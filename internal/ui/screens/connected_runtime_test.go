package screens

import (
	"testing"

	"sshpilot/internal/config"
	sshclient "sshpilot/internal/ssh"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConnectedReusesSharedTransportAcrossTerminalAndFiles(t *testing.T) {
	dialCount := 0
	transportReady := false
	fs := newFakeRemoteFS("/home/test")

	connection := &fakeSSHConnection{
		diagnoseFn: func() sshclient.DiagnosticReport {
			return sshclient.DiagnosticReport{Stage: sshclient.DiagnosticStageSuccess}
		},
		nativeTerminalFn: func(string, int, int) tea.ExecCommand {
			if !transportReady {
				transportReady = true
				dialCount++
			}
			return &fakeExecCommand{}
		},
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			if !transportReady {
				transportReady = true
				dialCount++
			}
			return fs, nil
		},
	}

	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: connection},
	)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	var cmd tea.Cmd
	m, cmd = m.Update(keyRunes("t"))
	m = runConnectedCmd(t, m, cmd)
	if dialCount != 1 {
		t.Fatalf("dial count after terminal = %d, want 1", dialCount)
	}

	m, cmd = m.Update(keyEsc())
	m = runConnectedCmd(t, m, cmd)
	m, cmd = m.Update(keyRunes("f"))
	m = runConnectedCmd(t, m, cmd)

	if dialCount != 1 {
		t.Fatalf("dial count after files = %d, want 1", dialCount)
	}
	if connection.nativeTerminalCalls != 1 {
		t.Fatalf("native terminal calls = %d, want 1", connection.nativeTerminalCalls)
	}
	if connection.openRemoteFSCalls != 1 {
		t.Fatalf("open fs calls = %d, want 1", connection.openRemoteFSCalls)
	}
}

func TestConnectionCheckTickUsesExistingSharedTransport(t *testing.T) {
	dialCount := 1
	transportReady := true
	connection := &fakeSSHConnection{
		diagnoseFn: func() sshclient.DiagnosticReport {
			if !transportReady {
				transportReady = true
				dialCount++
			}
			return sshclient.DiagnosticReport{Stage: sshclient.DiagnosticStageSuccess}
		},
	}

	m := newConnectedModelWithRuntime(
		testServerConfig(),
		fakeSSHRuntime{connection: connection},
	)
	m.connChecking = false

	next, cmd := m.Update(connectionCheckTickMsg{})
	if !next.connChecking {
		t.Fatal("expected connection check to start after tick")
	}
	msg := cmd()
	reportMsg, ok := msg.(connectionStatusMsg)
	if !ok {
		t.Fatalf("expected connectionStatusMsg, got %T", msg)
	}
	if reportMsg.report.Stage != sshclient.DiagnosticStageSuccess {
		t.Fatalf("diagnostic stage = %q, want success", reportMsg.report.Stage)
	}
	if dialCount != 1 {
		t.Fatalf("dial count = %d, want 1", dialCount)
	}
}

func TestConnectionCheckTickReconnectsStaleTransportOnce(t *testing.T) {
	dialCount := 1
	stale := true
	transportReady := true
	connection := &fakeSSHConnection{
		diagnoseFn: func() sshclient.DiagnosticReport {
			if stale {
				transportReady = false
				stale = false
			}
			if !transportReady {
				transportReady = true
				dialCount++
			}
			return sshclient.DiagnosticReport{Stage: sshclient.DiagnosticStageSuccess}
		},
	}

	m := newConnectedModelWithRuntime(
		testServerConfig(),
		fakeSSHRuntime{connection: connection},
	)
	m.connChecking = false

	next, cmd := m.Update(connectionCheckTickMsg{})
	msg := cmd()
	reportMsg, ok := msg.(connectionStatusMsg)
	if !ok {
		t.Fatalf("expected connectionStatusMsg, got %T", msg)
	}
	if reportMsg.report.Stage != sshclient.DiagnosticStageSuccess {
		t.Fatalf("diagnostic stage = %q, want success", reportMsg.report.Stage)
	}
	if dialCount != 2 {
		t.Fatalf("dial count after reconnect = %d, want 2", dialCount)
	}

	next, _ = next.Update(reportMsg)
	next, cmd = next.Update(connectionCheckTickMsg{})
	msg = cmd()
	reportMsg, ok = msg.(connectionStatusMsg)
	if !ok {
		t.Fatalf("expected connectionStatusMsg on second tick, got %T", msg)
	}
	if reportMsg.report.Stage != sshclient.DiagnosticStageSuccess {
		t.Fatalf("second diagnostic stage = %q, want success", reportMsg.report.Stage)
	}
	if dialCount != 2 {
		t.Fatalf("dial count after second tick = %d, want 2", dialCount)
	}
}
