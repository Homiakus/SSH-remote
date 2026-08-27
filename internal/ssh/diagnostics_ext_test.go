package ssh

import (
	"fmt"
	"testing"

	"sshpilot/internal/config"
)

func TestValidateConnectionConfigEmptyHost(t *testing.T) {
	cfg := &config.ServerConfig{User: "root"}
	err := validateConnectionConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty host")
	}
}

func TestValidateConnectionConfigEmptyUser(t *testing.T) {
	cfg := &config.ServerConfig{Host: "example.com"}
	err := validateConnectionConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty user")
	}
}

func TestValidateConnectionConfigEmptyPassword(t *testing.T) {
	cfg := &config.ServerConfig{
		Host:       "example.com",
		User:       "root",
		AuthMethod: config.AuthMethodPassword,
	}
	err := validateConnectionConfig(cfg)
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestClassifyDiagnosticStageAuth(t *testing.T) {
	stage := classifyDiagnosticStage(fmt.Errorf("ssh: unable to authenticate"))
	if stage != DiagnosticStageAuth {
		t.Fatalf("expected auth stage, got %s", stage)
	}
}

func TestClassifyDiagnosticStageAuthPermissionDenied(t *testing.T) {
	stage := classifyDiagnosticStage(fmt.Errorf("ssh: permission denied"))
	if stage != DiagnosticStageAuth {
		t.Fatalf("expected auth stage, got %s", stage)
	}
}

func TestClassifyDiagnosticStageTCP(t *testing.T) {
	stage := classifyDiagnosticStage(fmt.Errorf("dial tcp 1.2.3.4:22: connection refused"))
	if stage != DiagnosticStageTCP {
		t.Fatalf("expected tcp stage, got %s", stage)
	}
}

func TestClassifyDiagnosticStageTCPTimeout(t *testing.T) {
	stage := classifyDiagnosticStage(fmt.Errorf("dial tcp 1.2.3.4:22: i/o timeout"))
	if stage != DiagnosticStageTCP {
		t.Fatalf("expected tcp stage for timeout, got %s", stage)
	}
}

func TestClassifyDiagnosticStageTCPNoRoute(t *testing.T) {
	stage := classifyDiagnosticStage(fmt.Errorf("dial tcp: no route to host"))
	if stage != DiagnosticStageTCP {
		t.Fatalf("expected tcp stage for no route, got %s", stage)
	}
}

func TestClassifyDiagnosticStageDefaultToHandshake(t *testing.T) {
	stage := classifyDiagnosticStage(fmt.Errorf("unknown error"))
	if stage != DiagnosticStageHandshake {
		t.Fatalf("expected handshake stage for unknown, got %s", stage)
	}
}

func TestExtractAttemptedAuthMethodsNoMatch(t *testing.T) {
	err := fmt.Errorf("some other error without auth methods")
	methods := extractAttemptedAuthMethods(err)
	if len(methods) != 0 {
		t.Fatalf("expected empty, got %v", methods)
	}
}

func TestContainsAuthMethod(t *testing.T) {
	methods := []string{"publickey", "password", "keyboard-interactive"}
	if !containsAuthMethod(methods, "publickey") {
		t.Fatal("expected to find publickey")
	}
	if containsAuthMethod(methods, "gssapi") {
		t.Fatal("did not expect to find gssapi")
	}
}

func TestNewDiagnosticReportDefaults(t *testing.T) {
	cfg := &config.ServerConfig{
		Host: "example.com",
		User: "root",
	}
	report := newDiagnosticReport(cfg)
	if report.Target != "root@example.com" {
		t.Fatalf("expected root@example.com, got %s", report.Target)
	}
	if report.EffectivePort != "22" {
		t.Fatalf("expected default port 22, got %s", report.EffectivePort)
	}
	if !report.UsedDefaultPort {
		t.Fatal("expected UsedDefaultPort to be true")
	}
}
