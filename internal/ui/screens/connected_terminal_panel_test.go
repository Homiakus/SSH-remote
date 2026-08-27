package screens

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sshpilot/internal/config"
	"sshpilot/internal/scripts"
	sshclient "sshpilot/internal/ssh"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConnectedShellExecutionRunsInNativeTerminal(t *testing.T) {
	scriptPath := filepath.Join(t.TempDir(), "deploy.sh")
	if err := os.WriteFile(scriptPath, []byte("echo one\necho two\n"), 0o644); err != nil {
		t.Fatalf("write script: %v", err)
	}

	fs := newFakeRemoteFS("/home/test")
	connection := &fakeSSHConnection{
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			return fs, nil
		},
	}

	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: connection},
	)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	cmd := m.startExecution([]scripts.Script{{
		Name:    "deploy.sh",
		Path:    scriptPath,
		Package: "tmp",
		Kind:    scripts.ScriptKindShell,
	}})
	m = runConnectedCmd(t, m, cmd)

	renderedLog := strings.Join(m.consoleRenderLines(), "\n")
	for _, want := range []string{"Открываю runnable в нативной консоли сервера", "Runnable завершён"} {
		if !strings.Contains(renderedLog, want) {
			t.Fatalf("console log does not contain %q:\n%s", want, renderedLog)
		}
	}
	if len(connection.nativeCommands) != 1 {
		t.Fatalf("native commands = %#v, want one command", connection.nativeCommands)
	}
	if strings.Contains(connection.nativeCommands[0], "__SSHPILOT_") {
		t.Fatalf("native command should not contain tracking markers: %q", connection.nativeCommands[0])
	}
	if m.executing {
		t.Fatal("expected execution to finish")
	}
	if connection.executeStreamCalls != 0 {
		t.Fatalf("execute stream calls = %d, want 0", connection.executeStreamCalls)
	}
	if connection.openRemoteFSCalls != 1 {
		t.Fatalf("open remote fs calls = %d, want 1", connection.openRemoteFSCalls)
	}
	if connection.nativeTerminalCalls != 1 {
		t.Fatalf("native terminal calls = %d, want 1", connection.nativeTerminalCalls)
	}

	view := m.View()
	for _, want := range []string{"Скрипты", "Терминал выполнения"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
	}
}

func TestConnectedServerTerminalRunsNativelyAndReturnsToConsole(t *testing.T) {
	connection := &fakeSSHConnection{}
	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: connection},
	)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m, cmd := m.Update(keyRunes("t"))
	m = runConnectedCmd(t, m, cmd)

	if m.IsTerminalActive() {
		t.Fatal("native terminal should return to the regular console after exit")
	}
	if connection.nativeTerminalCalls != 1 {
		t.Fatalf("native terminal calls = %d, want 1", connection.nativeTerminalCalls)
	}
	if len(connection.nativeCommands) != 1 || connection.nativeCommands[0] != "" {
		t.Fatalf("native shell command = %#v, want empty shell command", connection.nativeCommands)
	}

	view := m.View()
	for _, want := range []string{"Скрипты", "Нативная SSH-консоль закрыта"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
	}
}

func TestConnectedServerTerminalDoesNotUseEmbeddedLineMode(t *testing.T) {
	connection := &fakeSSHConnection{}
	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: connection},
	)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	var cmd tea.Cmd
	m, cmd = m.Update(keyRunes("t"))
	m = runConnectedCmd(t, m, cmd)

	if m.IsTerminalActive() {
		t.Fatal("native server terminal should not create embedded terminal workspace")
	}
	if len(connection.nativeCommands) != 1 || connection.nativeCommands[0] != "" {
		t.Fatalf("native commands = %#v, want one shell command", connection.nativeCommands)
	}
}

func TestConnectedTerminalLinesAreBounded(t *testing.T) {
	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: &fakeSSHConnection{}},
	)

	for i := 0; i < maxTerminalLines+25; i++ {
		m.appendTerminalLine(terminalTabBuild, "line")
	}

	tab := m.terminalTab(terminalTabBuild)
	if tab == nil {
		t.Fatal("expected terminal tab")
	}
	if len(tab.lines) != maxTerminalLines {
		t.Fatalf("terminal lines = %d, want %d", len(tab.lines), maxTerminalLines)
	}
}
