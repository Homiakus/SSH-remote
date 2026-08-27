package ssh

import (
	"os"
	"testing"

	"sshpilot/internal/config"
)

func TestGenerateOpenSSHPrivateKey(t *testing.T) {
	data, err := generateOpenSSHPrivateKey("test-server")
	if err != nil {
		t.Fatalf("generate failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("empty key data")
	}
	// Проверяем, что ключ содержит PEM-заголовок
	if string(data[:10]) != "-----BEGIN" {
		t.Fatal("not a valid PEM key")
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "'hello'"},
		{"it's", "'it'\"'\"'s'"},
		{"", "''"},
	}
	for _, tc := range tests {
		result := shellQuote(tc.input)
		if result != tc.expected {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

func TestSafeAuthorizedKeyComment(t *testing.T) {
	result := safeAuthorizedKeyComment("my server  ")
	if result != "my_server" {
		t.Errorf("expected 'my_server', got %q", result)
	}
}

func TestSafeAuthorizedKeyCommentTab(t *testing.T) {
	result := safeAuthorizedKeyComment("my\tserver")
	if result != "my_server" {
		t.Errorf("expected 'my_server', got %q", result)
	}
}

func TestAuthorizedPublicKeyFromPrivate(t *testing.T) {
	privateData, err := generateOpenSSHPrivateKey("test-server")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	pub, err := authorizedPublicKeyFromPrivate(privateData, "test-server")
	if err != nil {
		t.Fatalf("extract public: %v", err)
	}
	if pub == "" {
		t.Fatal("empty public key")
	}
}

func TestAuthorizedKeyInstallScript(t *testing.T) {
	script := authorizedKeyInstallScript("ssh-ed25519 AAAAtest sshpilot-test")
	if script == "" {
		t.Fatal("empty script")
	}
	// Проверяем наличие ключевых команд
	if !contains(script, "set -e") {
		t.Error("missing set -e")
	}
	if !contains(script, "mkdir -p") {
		t.Error("missing mkdir -p")
	}
	if !contains(script, "authorized_keys") {
		t.Error("missing authorized_keys")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestCloneConfigForAuth(t *testing.T) {
	cfg := &config.ServerConfig{
		Name:       "test",
		Host:       "example.com",
		User:       "root",
		AuthMethod: config.AuthMethodKey,
		KeyPath:    "keys/test.ed25519",
	}
	cloned := cloneConfigForAuth(cfg)
	if cloned.Name != cfg.Name {
		t.Fatal("name mismatch")
	}
	if cloned.Host != cfg.Host {
		t.Fatal("host mismatch")
	}
	// Проверяем, что это копия, а не ссылка
	cloned.Host = "modified.com"
	if cfg.Host == "modified.com" {
		t.Fatal("clone is not a copy")
	}
}

func TestEnsureServerKeyPairCreatesKeys(t *testing.T) {
	origWD, _ := os.Getwd()
	tempDir := t.TempDir()
	os.Chdir(tempDir)
	t.Cleanup(func() { os.Chdir(origWD) })

	if err := config.EnsureKeysDir(); err != nil {
		t.Fatalf("ensure keys dir: %v", err)
	}

	pair, err := EnsureServerKeyPair("test-server")
	if err != nil {
		t.Fatalf("ensure key pair: %v", err)
	}

	// Проверить что файлы созданы
	if _, err := os.Stat(pair.PrivateKeyPath); os.IsNotExist(err) {
		t.Fatal("private key file not created")
	}
	if _, err := os.Stat(pair.PublicKeyPath); os.IsNotExist(err) {
		t.Fatal("public key file not created")
	}

	// Проверить, что публичный ключ не пуст
	if pair.PublicAuthorizedKey == "" {
		t.Fatal("public authorized key is empty")
	}

	// Повторный вызов должен переиспользовать существующие ключи
	pair2, err := EnsureServerKeyPair("test-server")
	if err != nil {
		t.Fatalf("second ensure key pair: %v", err)
	}
	if pair2.PublicAuthorizedKey != pair.PublicAuthorizedKey {
		t.Fatal("second call returned different public key")
	}
}
