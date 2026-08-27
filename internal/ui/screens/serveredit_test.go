package screens

import (
	"os"
	"strings"
	"testing"

	"sshpilot/internal/config"
)

func TestNewServerEditModelDefaultsToPassword(t *testing.T) {
	m := NewServerEditModel(nil)

	if got := m.inputs[fieldAuthMethod].Value(); got != config.AuthMethodPassword {
		t.Fatalf("auth method = %q, want %q", got, config.AuthMethodPassword)
	}
}

func TestServerEditViewShowsPasswordWhitespaceWarning(t *testing.T) {
	m := NewServerEditModel(&config.ServerConfig{
		Name:       "prod",
		Host:       "185.72.144.39",
		User:       "root",
		AuthMethod: config.AuthMethodPassword,
		Password:   "secret ",
	})
	m.width = 100

	view := m.View()
	want := "Пароль заканчивается пробелом. Приложение отправит его буквально."
	if !strings.Contains(view, want) {
		t.Fatalf("view does not contain %q:\n%s", want, view)
	}
}

func TestServerEditSaveAllowsPasswordWarning(t *testing.T) {
	withTempScreenWorkingDir(t)

	m := NewServerEditModel(nil)
	m.inputs[fieldName].SetValue("prod")
	m.inputs[fieldHost].SetValue("185.72.144.39")
	m.inputs[fieldUser].SetValue("root")
	m.inputs[fieldAuthMethod].SetValue("password")
	m.inputs[fieldPassword].SetValue("secret ")

	cmd := m.save()
	if cmd == nil {
		t.Fatal("expected save command")
	}

	msg, ok := cmd().(ServerSavedMsg)
	if !ok {
		t.Fatalf("expected ServerSavedMsg, got %T", cmd())
	}
	if msg.Name != "prod" {
		t.Fatalf("expected saved name prod, got %q", msg.Name)
	}

	cfg, err := config.LoadServer("prod")
	if err != nil {
		t.Fatalf("load saved server: %v", err)
	}
	if cfg.Password != "secret " {
		t.Fatalf("expected trailing-space password to be preserved, got %q", cfg.Password)
	}
}

func TestServerEditRenameSavesNewConfigBeforeDeletingOld(t *testing.T) {
	withTempScreenWorkingDir(t)

	if err := config.SaveServer("old", &config.ServerConfig{
		Name:       "old",
		Host:       "127.0.0.1",
		User:       "root",
		AuthMethod: config.AuthMethodPassword,
		Password:   "secret",
	}); err != nil {
		t.Fatalf("save old server: %v", err)
	}

	m := NewServerEditModel(&config.ServerConfig{
		Name:       "old",
		Host:       "127.0.0.1",
		User:       "root",
		AuthMethod: config.AuthMethodPassword,
		Password:   "secret",
	})
	m.inputs[fieldName].SetValue("new")

	cmd := m.save()
	if cmd == nil {
		t.Fatalf("expected save command, message=%q", m.message)
	}
	if _, err := config.LoadServer("new"); err != nil {
		t.Fatalf("new config was not saved: %v", err)
	}
	if config.ServerExists("old") {
		t.Fatal("old config should be removed after successful new save")
	}
}

func withTempScreenWorkingDir(t *testing.T) {
	t.Helper()

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	if err := config.InitMasterKey(); err != nil {
		t.Fatalf("InitMasterKey: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWD); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})
}
