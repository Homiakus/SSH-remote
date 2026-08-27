package ui

import (
	"strings"
	"testing"

	"sshpilot/internal/config"
	"sshpilot/internal/ui/screens"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConnectedScreenInheritsCurrentWindowSize(t *testing.T) {
	model := AppModel{screen: ScreenServerList}

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	app := updated.(AppModel)

	updated, _ = app.Update(screens.ServerSelectedMsg{
		Server: config.ServerConfig{
			Name: "demo",
			Host: "127.0.0.1",
			Port: "22",
			User: "root",
		},
	})
	app = updated.(AppModel)

	if app.screen != ScreenConnected {
		t.Fatalf("expected connected screen, got %v", app.screen)
	}

	if view := app.View(); strings.Contains(view, "Окно слишком маленькое") {
		t.Fatalf("expected connected screen to reuse known window size, got view: %q", view)
	}
}
