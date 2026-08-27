package ssh

import (
	"os"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func TestEnsureServerKeyPairGeneratesParseableKey(t *testing.T) {
	withTempWorkingDir(t)

	pair, err := EnsureServerKeyPair("prod")
	if err != nil {
		t.Fatalf("EnsureServerKeyPair() error = %v", err)
	}
	if pair.RelativePrivateKeyPath != "keys/prod.ed25519" {
		t.Fatalf("relative private key path = %q", pair.RelativePrivateKeyPath)
	}

	privateData, err := os.ReadFile(pair.PrivateKeyPath)
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	if _, err := gossh.ParsePrivateKey(privateData); err != nil {
		t.Fatalf("generated private key does not parse: %v", err)
	}
	if _, err := buildKeyAuth(pair.RelativePrivateKeyPath, ""); err != nil {
		t.Fatalf("buildKeyAuth() with generated key error = %v", err)
	}

	publicData, err := os.ReadFile(pair.PublicKeyPath)
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	_, comment, _, _, err := gossh.ParseAuthorizedKey(publicData)
	if err != nil {
		t.Fatalf("generated public key does not parse: %v", err)
	}
	if comment != "sshpilot-prod" {
		t.Fatalf("public key comment = %q, want %q", comment, "sshpilot-prod")
	}
}

func TestEnsureServerKeyPairReusesExistingPrivateKey(t *testing.T) {
	withTempWorkingDir(t)

	first, err := EnsureServerKeyPair("prod")
	if err != nil {
		t.Fatalf("first EnsureServerKeyPair() error = %v", err)
	}
	before, err := os.ReadFile(first.PrivateKeyPath)
	if err != nil {
		t.Fatalf("read first private key: %v", err)
	}

	second, err := EnsureServerKeyPair("prod")
	if err != nil {
		t.Fatalf("second EnsureServerKeyPair() error = %v", err)
	}
	after, err := os.ReadFile(second.PrivateKeyPath)
	if err != nil {
		t.Fatalf("read second private key: %v", err)
	}
	if string(before) != string(after) {
		t.Fatal("expected existing private key to be reused")
	}
}

func TestAuthorizedKeyInstallScriptIsIdempotent(t *testing.T) {
	script := authorizedKeyInstallScript("ssh-ed25519 AAAATEST sshpilot-prod")

	if !strings.Contains(script, `grep -qxF "$key" "$HOME/.ssh/authorized_keys"`) {
		t.Fatalf("script does not guard against duplicate keys:\n%s", script)
	}
	if !strings.Contains(script, `printf '%s\n' "$key" >> "$HOME/.ssh/authorized_keys"`) {
		t.Fatalf("script does not append key safely:\n%s", script)
	}
}

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	got := shellQuote("ssh-ed25519 AAAA comment's")
	want := `'ssh-ed25519 AAAA comment'"'"'s'`
	if got != want {
		t.Fatalf("shellQuote() = %q, want %q", got, want)
	}
}

func withTempWorkingDir(t *testing.T) {
	t.Helper()

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
}
