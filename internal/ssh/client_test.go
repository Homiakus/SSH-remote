package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"sshpilot/internal/config"

	gossh "golang.org/x/crypto/ssh"
)

func TestEffectivePort(t *testing.T) {
	tests := []struct {
		name string
		port string
		want string
	}{
		{name: "empty uses default", port: "", want: "22"},
		{name: "spaces use default", port: "   ", want: "22"},
		{name: "explicit stays", port: "2222", want: "2222"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := effectivePort(&config.ServerConfig{Port: tt.port}); got != tt.want {
				t.Fatalf("effectivePort() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConnectionTarget(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.ServerConfig
		want string
	}{
		{
			name: "empty port hides suffix",
			cfg:  config.ServerConfig{User: "root", Host: "185.72.144.39", Port: ""},
			want: "root@185.72.144.39",
		},
		{
			name: "explicit port shows suffix",
			cfg:  config.ServerConfig{User: "root", Host: "185.72.144.39", Port: "22"},
			want: "root@185.72.144.39:22",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := connectionTarget(&tt.cfg); got != tt.want {
				t.Fatalf("connectionTarget() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNetworkAddress(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.ServerConfig
		want string
	}{
		{
			name: "empty port omits suffix",
			cfg:  config.ServerConfig{Host: "185.72.144.39", Port: ""},
			want: "185.72.144.39",
		},
		{
			name: "explicit port shows suffix",
			cfg:  config.ServerConfig{Host: "185.72.144.39", Port: "22"},
			want: "185.72.144.39:22",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := networkAddress(&tt.cfg); got != tt.want {
				t.Fatalf("networkAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildPasswordAuth(t *testing.T) {
	auths, err := buildPasswordAuth("secret")
	if err != nil {
		t.Fatalf("buildPasswordAuth() error = %v", err)
	}

	if len(auths) != 2 {
		t.Fatalf("expected 2 auth methods, got %d", len(auths))
	}
}

func TestBuildPasswordAuthRequiresPassword(t *testing.T) {
	_, err := buildPasswordAuth("")
	if err == nil {
		t.Fatal("expected error for empty password")
	}
}

func TestTOFUHostKeyCallbackRequiresExplicitTrustForUnknownHostKey(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "known_hosts")
	callback := tofuHostKeyCallback(path)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}
	key := testPublicKey(t)

	err := callback("example.test:2222", addr, key)
	if err == nil {
		t.Fatal("expected unknown host key error")
	}
	var unknown UnknownHostKeyError
	if !errors.As(err, &unknown) {
		t.Fatalf("expected UnknownHostKeyError, got %T: %v", err, err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("known_hosts should not be created before explicit trust, stat err = %v", statErr)
	}

	if err := trustHostKey(path, "example.test:2222", key); err != nil {
		t.Fatalf("trust host key: %v", err)
	}
	if err := callback("example.test:2222", addr, key); err != nil {
		t.Fatalf("callback after explicit trust: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat known_hosts: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("known_hosts mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestTOFUHostKeyCallbackRejectsChangedHostKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	callback := tofuHostKeyCallback(path)
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}

	if err := trustHostKey(path, "example.test:2222", testPublicKey(t)); err != nil {
		t.Fatalf("trust host key: %v", err)
	}
	if err := callback("example.test:2222", addr, testPublicKey(t)); err == nil {
		t.Fatal("expected changed host key error")
	}
}

func testPublicKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("new signer: %v", err)
	}
	return signer.PublicKey()
}
