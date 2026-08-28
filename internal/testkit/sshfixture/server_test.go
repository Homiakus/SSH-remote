package sshfixture

import (
	"errors"
	"net"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

const (
	testUser     = "fixture-user"
	testPassword = "fixture-password"
)

func TestStartRequiresCredentials(t *testing.T) {
	if _, err := Start(Options{Password: testPassword}); err == nil {
		t.Fatal("expected missing user error")
	}
	if _, err := Start(Options{User: testUser}); err == nil {
		t.Fatal("expected missing password error")
	}
}

func TestServerExecSuccessAndFailure(t *testing.T) {
	server, err := Start(Options{
		User:     testUser,
		Password: testPassword,
		Commands: map[string]CommandResult{
			"echo ok": {Stdout: "ok\n"},
			"fail":    {Stdout: "partial\n", Stderr: "boom\n", ExitStatus: 17},
		},
	})
	if err != nil {
		t.Fatalf("start fixture: %v", err)
	}
	defer server.Close()

	client, err := dial(server.Address(), testUser, testPassword)
	if err != nil {
		t.Fatalf("dial fixture: %v", err)
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("new success session: %v", err)
	}
	output, err := session.Output("echo ok")
	if err != nil {
		t.Fatalf("success output: %v", err)
	}
	if string(output) != "ok\n" {
		t.Fatalf("success output = %q, want ok", output)
	}

	failureSession, err := client.NewSession()
	if err != nil {
		t.Fatalf("new failure session: %v", err)
	}
	combined, err := failureSession.CombinedOutput("fail")
	if err == nil {
		t.Fatal("expected non-zero exit status")
	}
	var exitErr *gossh.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitStatus() != 17 {
		t.Fatalf("failure error = %v, want exit status 17", err)
	}
	if got := string(combined); !strings.Contains(got, "partial") || !strings.Contains(got, "boom") {
		t.Fatalf("combined output = %q", got)
	}
}

func TestServerRejectsAuthentication(t *testing.T) {
	server, err := Start(Options{User: testUser, Password: testPassword, Mode: RejectAuth})
	if err != nil {
		t.Fatalf("start fixture: %v", err)
	}
	defer server.Close()

	if _, err := dial(server.Address(), testUser, testPassword); err == nil {
		t.Fatal("expected authentication rejection")
	}
	if _, err := dial(server.Address(), "wrong-user", testPassword); err == nil {
		t.Fatal("expected user mismatch rejection")
	}
	if _, err := dial(server.Address(), testUser, "wrong-password"); err == nil {
		t.Fatal("expected password mismatch rejection")
	}
}

func TestServerRejectsSession(t *testing.T) {
	server, err := Start(Options{User: testUser, Password: testPassword, Mode: RejectSession})
	if err != nil {
		t.Fatalf("start fixture: %v", err)
	}
	defer server.Close()

	client, err := dial(server.Address(), testUser, testPassword)
	if err != nil {
		t.Fatalf("dial fixture: %v", err)
	}
	defer client.Close()
	if _, err := client.NewSession(); err == nil {
		t.Fatal("expected session rejection")
	}
}

func TestServerRejectsHandshake(t *testing.T) {
	server, err := Start(Options{User: testUser, Password: testPassword, Mode: RejectHandshake})
	if err != nil {
		t.Fatalf("start fixture: %v", err)
	}
	defer server.Close()

	conn, err := net.Dial("tcp", server.Address())
	if err != nil {
		t.Fatalf("dial TCP: %v", err)
	}
	defer conn.Close()

	config := &gossh.ClientConfig{
		User:            testUser,
		Auth:            []gossh.AuthMethod{gossh.Password(testPassword)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	}
	if _, _, _, err := gossh.NewClientConn(conn, server.Address(), config); err == nil {
		t.Fatal("expected handshake rejection")
	}
}

func TestServerPublicKeyIsStable(t *testing.T) {
	first, err := Start(Options{User: testUser, Password: testPassword})
	if err != nil {
		t.Fatalf("start first fixture: %v", err)
	}
	defer first.Close()
	second, err := Start(Options{User: testUser, Password: testPassword})
	if err != nil {
		t.Fatalf("start second fixture: %v", err)
	}
	defer second.Close()

	if string(first.PublicKey().Marshal()) != string(second.PublicKey().Marshal()) {
		t.Fatal("fixture host key must be deterministic")
	}
}

func dial(address, user, password string) (*gossh.Client, error) {
	return gossh.Dial("tcp", address, &gossh.ClientConfig{
		User:            user,
		Auth:            []gossh.AuthMethod{gossh.Password(password)},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	})
}
