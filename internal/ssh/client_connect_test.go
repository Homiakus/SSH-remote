package ssh

import (
	"testing"

	"sshpilot/internal/config"
)

func TestBuildPasswordAuthEmptyPassword(t *testing.T) {
	_, err := buildPasswordAuth("")
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestBuildPasswordAuthValid(t *testing.T) {
	methods, err := buildPasswordAuth("test123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(methods) != 2 {
		t.Fatalf("expected 2 auth methods (password + keyboard-interactive), got %d", len(methods))
	}
}

func TestNormalizeAuthMethodDefaultsToPassword(t *testing.T) {
	result := config.NormalizeAuthMethod("unknown")
	if result != config.AuthMethodPassword {
		t.Fatalf("expected password for unknown method, got %s", result)
	}
}

func TestNormalizeAuthMethodKey(t *testing.T) {
	result := config.NormalizeAuthMethod("key")
	if result != config.AuthMethodKey {
		t.Fatalf("expected key method, got %s", result)
	}
}

func TestEffectivePortDefault(t *testing.T) {
	cfg := &config.ServerConfig{Host: "localhost", User: "root"}
	port := effectivePort(cfg)
	if port != "22" {
		t.Fatalf("expected default port 22, got %s", port)
	}
}

func TestEffectivePortExplicit(t *testing.T) {
	cfg := &config.ServerConfig{Host: "localhost", Port: "2222"}
	port := effectivePort(cfg)
	if port != "2222" {
		t.Fatalf("expected port 2222, got %s", port)
	}
}

func TestNetworkAddressNoPort(t *testing.T) {
	cfg := &config.ServerConfig{Host: "example.com"}
	addr := networkAddress(cfg)
	if addr != "example.com" {
		t.Fatalf("expected 'example.com', got %s", addr)
	}
}

func TestNetworkAddressWithPort(t *testing.T) {
	cfg := &config.ServerConfig{Host: "example.com", Port: "2222"}
	addr := networkAddress(cfg)
	expected := "example.com:2222"
	if addr != expected {
		t.Fatalf("expected %s, got %s", expected, addr)
	}
}
