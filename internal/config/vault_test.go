package config

import (
	"bytes"
	"crypto/rand"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	mk := generateTestKey(t)
	vault := &ServerVault{
		Name:       "test-server",
		Host:       "192.168.1.1",
		Port:       "22",
		User:       "root",
		AuthMethod: "password",
		Password:   "s3cret!@#$%^&*()",
	}

	data, err := EncryptVault(mk, vault)
	if err != nil {
		t.Fatalf("EncryptVault() error = %v", err)
	}

	got, err := DecryptVault(mk, data)
	if err != nil {
		t.Fatalf("DecryptVault() error = %v", err)
	}

	if got.Name != vault.Name || got.Host != vault.Host || got.Password != vault.Password {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", got, vault)
	}
}

func TestEncryptDecryptWithEmbeddedKey(t *testing.T) {
	mk := generateTestKey(t)
	vault := &ServerVault{
		Name:       "key-server",
		Host:       "10.0.0.1",
		Port:       "22",
		User:       "deploy",
		AuthMethod: "key",
		PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nfake-key-data\n-----END OPENSSH PRIVATE KEY-----",
		PublicKey:  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5 sshpilot-test",
		Passphrase: "keypass",
	}

	data, err := EncryptVault(mk, vault)
	if err != nil {
		t.Fatalf("EncryptVault() error = %v", err)
	}

	got, err := DecryptVault(mk, data)
	if err != nil {
		t.Fatalf("DecryptVault() error = %v", err)
	}

	if got.PrivateKey != vault.PrivateKey {
		t.Fatalf("private key mismatch: got %q", got.PrivateKey)
	}
	if got.PublicKey != vault.PublicKey {
		t.Fatalf("public key mismatch: got %q", got.PublicKey)
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	mk1 := generateTestKey(t)
	mk2 := generateTestKey(t)

	vault := &ServerVault{Name: "test", Host: "1.2.3.4", Password: "secret"}

	data, err := EncryptVault(mk1, vault)
	if err != nil {
		t.Fatalf("EncryptVault() error = %v", err)
	}

	if _, err := DecryptVault(mk2, data); err == nil {
		t.Fatal("DecryptVault() with wrong key should fail")
	}
}

func TestDecryptCorruptedData(t *testing.T) {
	mk := generateTestKey(t)
	vault := &ServerVault{Name: "test", Host: "1.2.3.4"}

	data, err := EncryptVault(mk, vault)
	if err != nil {
		t.Fatalf("EncryptVault() error = %v", err)
	}

	// Corrupt a byte in the ciphertext area.
	if len(data) > vaultHeaderLen+2 {
		data[vaultHeaderLen+1] ^= 0xFF
	}

	if _, err := DecryptVault(mk, data); err == nil {
		t.Fatal("DecryptVault() with corrupted data should fail")
	}
}

func TestDecryptTooShort(t *testing.T) {
	mk := generateTestKey(t)
	if _, err := DecryptVault(mk, []byte("short")); err == nil {
		t.Fatal("DecryptVault() with too-short data should fail")
	}
}

func TestDecryptBadMagic(t *testing.T) {
	mk := generateTestKey(t)
	data := make([]byte, vaultHeaderLen+32)
	copy(data, "XXXX") // wrong magic

	if _, err := DecryptVault(mk, data); err == nil {
		t.Fatal("DecryptVault() with bad magic should fail")
	}
}

func TestDecryptBadVersion(t *testing.T) {
	mk := generateTestKey(t)
	data := make([]byte, vaultHeaderLen+32)
	copy(data, vaultMagic)
	data[4] = 99 // unsupported version

	if _, err := DecryptVault(mk, data); err == nil {
		t.Fatal("DecryptVault() with bad version should fail")
	}
}

func TestVaultFileFormat(t *testing.T) {
	mk := generateTestKey(t)
	vault := &ServerVault{Name: "fmt-test", Host: "1.2.3.4"}

	data, err := EncryptVault(mk, vault)
	if err != nil {
		t.Fatalf("EncryptVault() error = %v", err)
	}

	if string(data[:4]) != vaultMagic {
		t.Fatalf("magic = %q, want %q", string(data[:4]), vaultMagic)
	}
	if data[4] != vaultVersion {
		t.Fatalf("version = %d, want %d", data[4], vaultVersion)
	}
	if len(data) < vaultHeaderLen+16 { // at least GCM tag (16 bytes)
		t.Fatalf("data too short: %d bytes", len(data))
	}
}

func TestEncryptDifferentNonces(t *testing.T) {
	mk := generateTestKey(t)
	vault := &ServerVault{Name: "nonce-test", Host: "1.2.3.4", Password: "same"}

	data1, _ := EncryptVault(mk, vault)
	data2, _ := EncryptVault(mk, vault)

	if bytes.Equal(data1, data2) {
		t.Fatal("two encryptions of the same vault should produce different ciphertexts")
	}
}

func TestVaultToConfigAndBack(t *testing.T) {
	original := &ServerVault{
		Name:       "roundtrip",
		Host:       "10.0.0.5",
		Port:       "2222",
		User:       "admin",
		AuthMethod: "key",
		PrivateKey: "-----BEGIN KEY-----\ndata\n-----END KEY-----",
		Passphrase: "p@ss",
	}

	cfg := vaultToConfig(original)
	if cfg.KeyPath != embeddedKeyPath {
		t.Fatalf("KeyPath = %q, want %q", cfg.KeyPath, embeddedKeyPath)
	}
	if cfg.EmbeddedKey != original.PrivateKey {
		t.Fatalf("EmbeddedKey mismatch")
	}

	back := configToVault(cfg)
	if back.PrivateKey != original.PrivateKey {
		t.Fatalf("PrivateKey lost in round-trip")
	}
	if back.ExternalKeyPath != "" {
		t.Fatalf("ExternalKeyPath should be empty for embedded key, got %q", back.ExternalKeyPath)
	}
}

func TestVaultToConfigExternalKey(t *testing.T) {
	v := &ServerVault{
		Name:            "ext-key",
		Host:            "1.2.3.4",
		AuthMethod:      "key",
		ExternalKeyPath: "keys/mykey.ed25519",
	}

	cfg := vaultToConfig(v)
	if cfg.KeyPath != "keys/mykey.ed25519" {
		t.Fatalf("KeyPath = %q, want %q", cfg.KeyPath, "keys/mykey.ed25519")
	}
	if cfg.EmbeddedKey != "" {
		t.Fatalf("EmbeddedKey should be empty for external key")
	}
}

func TestLoadOrCreateMasterKey(t *testing.T) {
	origDir := serversDir
	serversDir = filepath.Join(t.TempDir(), "servers")
	defer func() { serversDir = origDir }()

	mk1, err := LoadOrCreateMasterKey()
	if err != nil {
		t.Fatalf("LoadOrCreateMasterKey() first call error = %v", err)
	}

	mk2, err := LoadOrCreateMasterKey()
	if err != nil {
		t.Fatalf("LoadOrCreateMasterKey() second call error = %v", err)
	}

	if mk1.raw != mk2.raw {
		t.Fatal("LoadOrCreateMasterKey() returned different keys on second call")
	}
}

func TestLoadMasterKeyCorrupted(t *testing.T) {
	origDir := serversDir
	serversDir = filepath.Join(t.TempDir(), "servers")
	defer func() { serversDir = origDir }()

	if err := os.MkdirAll(serversDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Write a key with wrong length.
	if err := os.WriteFile(filepath.Join(serversDir, masterKeyFile), []byte("short"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadOrCreateMasterKey(); err == nil {
		t.Fatal("LoadOrCreateMasterKey() with corrupted key should fail")
	}
}

func TestEncryptNilInputs(t *testing.T) {
	mk := generateTestKey(t)

	if _, err := EncryptVault(nil, &ServerVault{}); err == nil {
		t.Fatal("EncryptVault(nil key) should fail")
	}
	if _, err := EncryptVault(mk, nil); err == nil {
		t.Fatal("EncryptVault(nil vault) should fail")
	}
}

func generateTestKey(t *testing.T) *MasterKey {
	t.Helper()
	mk := &MasterKey{}
	if _, err := io.ReadFull(rand.Reader, mk.raw[:]); err != nil {
		t.Fatalf("generateTestKey: %v", err)
	}
	return mk
}
