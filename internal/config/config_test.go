package config

import (
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// setupVaultTestDir sets up a temp servers dir with an initialized master key.
func setupVaultTestDir(t *testing.T) (cleanup func()) {
	t.Helper()
	origDir := serversDir
	origKey := globalMasterKey

	tmpDir := t.TempDir()
	serversDir = filepath.Join(tmpDir, "servers")
	if err := os.MkdirAll(serversDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	mk := &MasterKey{}
	if _, err := io.ReadFull(rand.Reader, mk.raw[:]); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	globalMasterKey = mk

	return func() {
		serversDir = origDir
		globalMasterKey = origKey
	}
}

func TestSaveAndLoadServerKeepsEmptyPort(t *testing.T) {
	cleanup := setupVaultTestDir(t)
	defer cleanup()

	cfg := &ServerConfig{
		Name:       "empty",
		Host:       "185.72.144.39",
		Port:       "",
		User:       "root",
		AuthMethod: "password",
		Password:   "test",
	}
	if err := SaveServer("empty", cfg); err != nil {
		t.Fatalf("save server: %v", err)
	}

	loaded, err := LoadServer("empty")
	if err != nil {
		t.Fatalf("load server: %v", err)
	}
	if loaded.Port != "" {
		t.Fatalf("expected empty port, got %q", loaded.Port)
	}
}

func TestSaveAndLoadServerKeepsExplicitPort22(t *testing.T) {
	cleanup := setupVaultTestDir(t)
	defer cleanup()

	cfg := &ServerConfig{
		Name:       "explicit",
		Host:       "185.72.144.39",
		Port:       "22",
		User:       "root",
		AuthMethod: "password",
		Password:   "test",
	}
	if err := SaveServer("explicit", cfg); err != nil {
		t.Fatalf("save server: %v", err)
	}

	loaded, err := LoadServer("explicit")
	if err != nil {
		t.Fatalf("load server: %v", err)
	}
	if loaded.Port != "22" {
		t.Fatalf("expected port 22, got %q", loaded.Port)
	}
}

func TestListServersReturnsValidServersAndBrokenConfigError(t *testing.T) {
	cleanup := setupVaultTestDir(t)
	defer cleanup()

	// Save a valid server.
	if err := SaveServer("good", &ServerConfig{
		Name: "good", Host: "127.0.0.1", User: "root", AuthMethod: "password", Password: "x",
	}); err != nil {
		t.Fatalf("save good: %v", err)
	}

	// Write a broken vault file.
	brokenPath := filepath.Join(serversDir, "broken.vault")
	if err := os.WriteFile(brokenPath, []byte("corrupted data"), 0600); err != nil {
		t.Fatalf("write broken: %v", err)
	}

	servers, err := ListServers()
	if err == nil {
		t.Fatal("expected broken config error")
	}
	if len(servers) != 1 || servers[0].Name != "good" {
		t.Fatalf("servers = %+v, want only good server", servers)
	}
}

func TestDeleteServerRemovesVault(t *testing.T) {
	cleanup := setupVaultTestDir(t)
	defer cleanup()

	if err := SaveServer("del", &ServerConfig{
		Name: "del", Host: "1.2.3.4", User: "root", AuthMethod: "password", Password: "x",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}

	if !ServerExists("del") {
		t.Fatal("server should exist after save")
	}

	if err := DeleteServer("del"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if ServerExists("del") {
		t.Fatal("server should not exist after delete")
	}
}

func TestSaveAndLoadServerWithEmbeddedKey(t *testing.T) {
	cleanup := setupVaultTestDir(t)
	defer cleanup()

	cfg := &ServerConfig{
		Name:        "keysrv",
		Host:        "10.0.0.1",
		Port:        "22",
		User:        "deploy",
		AuthMethod:  "key",
		EmbeddedKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nfakedata\n-----END OPENSSH PRIVATE KEY-----",
		Passphrase:  "mypass",
		Description: "test key server",
	}
	if err := SaveServer("keysrv", cfg); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadServer("keysrv")
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.EmbeddedKey != cfg.EmbeddedKey {
		t.Fatalf("EmbeddedKey mismatch: got %q", loaded.EmbeddedKey)
	}
	if loaded.KeyPath != embeddedKeyPath {
		t.Fatalf("KeyPath = %q, want %q", loaded.KeyPath, embeddedKeyPath)
	}
	if loaded.Passphrase != "mypass" {
		t.Fatalf("Passphrase = %q, want %q", loaded.Passphrase, "mypass")
	}
}
