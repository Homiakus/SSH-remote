package config

import (
	"testing"
)

func TestPasswordWarnings(t *testing.T) {
	warnings := PasswordWarnings(" secret ")

	for _, want := range []string{
		"Пароль начинается с пробела. Приложение отправит его буквально.",
		"Пароль заканчивается пробелом. Приложение отправит его буквально.",
	} {
		if !containsString(warnings, want) {
			t.Fatalf("warnings %v do not contain %q", warnings, want)
		}
	}
}

func TestPasswordWarningsWhitespaceOnly(t *testing.T) {
	warnings := PasswordWarnings("   ")

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if warnings[0] != "Пароль состоит только из пробелов. Приложение отправит его буквально." {
		t.Fatalf("unexpected warning: %q", warnings[0])
	}
}

func TestSecretWarningsUsesPassphraseForKeyAuth(t *testing.T) {
	warnings := SecretWarnings(&ServerConfig{
		AuthMethod: " key ",
		Passphrase: " secret",
	})

	want := "Пароль ключа начинается с пробела. Приложение отправит его буквально."
	if !containsString(warnings, want) {
		t.Fatalf("warnings %v do not contain %q", warnings, want)
	}
}

func TestSaveAndLoadServerPreservesQuotedPasswordWhitespace(t *testing.T) {
	cleanup := setupVaultTestDir(t)
	defer cleanup()

	cfg := &ServerConfig{
		Host:       "185.72.144.39",
		User:       "root",
		AuthMethod: AuthMethodPassword,
		Password:   "secret ",
	}

	if err := SaveServer("quoted", cfg); err != nil {
		t.Fatalf("save server: %v", err)
	}

	loaded, err := LoadServer("quoted")
	if err != nil {
		t.Fatalf("load server: %v", err)
	}

	if loaded.Password != "secret " {
		t.Fatalf("expected password with trailing space, got %q", loaded.Password)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
