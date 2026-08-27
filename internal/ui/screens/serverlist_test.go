package screens

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sshpilot/internal/config"

	tea "github.com/charmbracelet/bubbletea"
)

func TestServerListSetupKeyActionEmitsMessage(t *testing.T) {
	m := ServerListModel{
		servers: []config.ServerConfig{
			{Name: "prod", Host: "127.0.0.1", User: "root", AuthMethod: config.AuthMethodPassword, Password: "secret"},
		},
		testStatus: make(map[string]string),
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if cmd == nil {
		t.Fatal("expected setup key command")
	}
	if next.testStatus["prod"] != "testing" {
		t.Fatalf("status = %q, want testing", next.testStatus["prod"])
	}

	msg, ok := cmd().(SetupServerKeyMsg)
	if !ok {
		t.Fatalf("expected SetupServerKeyMsg, got %T", cmd())
	}
	if msg.Server.Name != "prod" {
		t.Fatalf("server name = %q, want prod", msg.Server.Name)
	}
}

func TestServerListKeySetupResultRefreshesServer(t *testing.T) {
	m := ServerListModel{
		servers:    []config.ServerConfig{{Name: "prod"}},
		testStatus: make(map[string]string),
	}

	next, _ := m.Update(ServerKeySetupResultMsg{ServerName: "prod"})
	if next.testStatus["prod"] != "ok" {
		t.Fatalf("status = %q, want ok", next.testStatus["prod"])
	}
}

func TestServerListShowsBrokenConfigWarning(t *testing.T) {
	withTempScreenWorkingDir(t)

	if err := config.SaveServer("good", &config.ServerConfig{
		Name:       "good",
		Host:       "127.0.0.1",
		User:       "root",
		AuthMethod: config.AuthMethodPassword,
		Password:   "secret",
	}); err != nil {
		t.Fatalf("save good server: %v", err)
	}
	if err := os.WriteFile(filepath.Join("servers", "broken.vault"), []byte("not valid vault data"), 0o600); err != nil {
		t.Fatalf("write broken config: %v", err)
	}

	m := NewServerListModel()
	if len(m.servers) != 1 || m.servers[0].Name != "good" {
		t.Fatalf("servers = %+v, want only good server", m.servers)
	}
	if !strings.Contains(m.message, "broken.vault") {
		t.Fatalf("expected broken config warning, got %q", m.message)
	}
}
