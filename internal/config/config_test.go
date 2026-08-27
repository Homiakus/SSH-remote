package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadServerKeepsEmptyPort(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWD); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})

	content := "SSH_HOST=185.72.144.39\nSSH_PORT=\nSSH_USER=root\nSSH_AUTH_METHOD=password\n"
	path := filepath.Join(tempDir, "servers", "empty.env")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir servers: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	cfg, err := LoadServer("empty")
	if err != nil {
		t.Fatalf("load server: %v", err)
	}

	if cfg.Port != "" {
		t.Fatalf("expected empty port, got %q", cfg.Port)
	}
}

func TestLoadServerKeepsExplicitPort22(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWD); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})

	content := "SSH_HOST=185.72.144.39\nSSH_PORT=22\nSSH_USER=root\nSSH_AUTH_METHOD=password\n"
	path := filepath.Join(tempDir, "servers", "explicit.env")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir servers: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}

	cfg, err := LoadServer("explicit")
	if err != nil {
		t.Fatalf("load server: %v", err)
	}

	if cfg.Port != "22" {
		t.Fatalf("expected port 22, got %q", cfg.Port)
	}
}

func TestListServersReturnsValidServersAndBrokenConfigError(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWD); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})

	serversDir := filepath.Join(tempDir, "servers")
	if err := os.MkdirAll(serversDir, 0o755); err != nil {
		t.Fatalf("mkdir servers: %v", err)
	}
	if err := os.WriteFile(filepath.Join(serversDir, "good.env"), []byte("SSH_HOST=127.0.0.1\nSSH_USER=root\nSSH_AUTH_METHOD=password\n"), 0o600); err != nil {
		t.Fatalf("write good config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(serversDir, "broken.env"), []byte("SSH_HOST=\"unterminated\n"), 0o600); err != nil {
		t.Fatalf("write broken config: %v", err)
	}

	servers, err := ListServers()
	if err == nil {
		t.Fatal("expected broken config error")
	}
	if len(servers) != 1 || servers[0].Name != "good" {
		t.Fatalf("servers = %+v, want only good server", servers)
	}
}
