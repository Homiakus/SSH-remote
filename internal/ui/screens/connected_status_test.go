package screens

import (
	"strings"
	"testing"

	"sshpilot/internal/config"
	sshclient "sshpilot/internal/ssh"
)

func TestConnectedStatusUpdateStoresDiagnosticReport(t *testing.T) {
	m := newConnectedModelWithRemoteFS(testServerConfig(), nilRemoteFSOpener)

	report := sshclient.DiagnosticReport{
		Stage: sshclient.DiagnosticStageTCP,
		Err:   errString("dial tcp 185.228.72.253:22: i/o timeout"),
	}

	next, cmd := m.Update(connectionStatusMsg{report: report})
	if cmd == nil {
		t.Fatal("expected next connection check to be scheduled")
	}
	if next.connChecking {
		t.Fatal("connection check should finish after receiving report")
	}
	if next.connReport.Stage != sshclient.DiagnosticStageTCP {
		t.Fatalf("stored stage = %q, want %q", next.connReport.Stage, sshclient.DiagnosticStageTCP)
	}
}

func TestRenderHeaderWithStatusDistinguishesStages(t *testing.T) {
	tests := []struct {
		name   string
		report sshclient.DiagnosticReport
		want   string
	}{
		{
			name:   "tcp timeout",
			report: sshclient.DiagnosticReport{Stage: sshclient.DiagnosticStageTCP, Err: errString("dial tcp 185.228.72.253:22: i/o timeout")},
			want:   "tcp timeout",
		},
		{
			name:   "auth failure",
			report: sshclient.DiagnosticReport{Stage: sshclient.DiagnosticStageAuth},
			want:   "пароль отклонён",
		},
		{
			name:   "session failure",
			report: sshclient.DiagnosticReport{Stage: sshclient.DiagnosticStageSession},
			want:   "сессия недоступна",
		},
		{
			name:   "success",
			report: sshclient.DiagnosticReport{Stage: sshclient.DiagnosticStageSuccess},
			want:   "подключено",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newConnectedModelWithRemoteFS(testServerConfig(), nilRemoteFSOpener)
			m.width = 120
			m.connChecking = false
			m.connReport = tt.report

			header := m.renderHeaderWithStatus()
			if !strings.Contains(header, tt.want) {
				t.Fatalf("header does not contain %q:\n%s", tt.want, header)
			}
		})
	}
}

func TestNativeTerminalFailureUsesStoredConnectionDiagnosis(t *testing.T) {
	m := newConnectedModelWithRemoteFS(testServerConfig(), nilRemoteFSOpener)
	m.width = 120
	m.height = 40
	m.connChecking = false
	m.connReport = sshclient.DiagnosticReport{
		Target:         "root@185.228.72.253:22",
		NetworkAddress: "185.228.72.253:22",
		EffectivePort:  "22",
		Stage:          sshclient.DiagnosticStageTCP,
		Err:            errString("dial tcp 185.228.72.253:22: i/o timeout"),
	}

	next, cmd := m.Update(nativeTerminalFinishedMsg{manual: true, err: errString("pty denied")})
	next = runConnectedCmd(t, next, cmd)

	rendered := strings.Join(next.consoleRenderLines(), "\n")
	for _, want := range []string{
		"Нативная SSH-консоль завершилась с ошибкой: pty denied",
		"Не удается открыть TCP-соединение",
		"i/o timeout",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("terminal output does not contain %q:\n%s", want, rendered)
		}
	}
}

func TestConnectionCheckTickStartsNextDiagnosticRun(t *testing.T) {
	m := newConnectedModelWithRemoteFS(testServerConfig(), nilRemoteFSOpener)
	m.connChecking = false

	next, cmd := m.Update(connectionCheckTickMsg{})
	if !next.connChecking {
		t.Fatal("expected connection check to start after tick")
	}
	if cmd == nil {
		t.Fatal("expected diagnostic command after tick")
	}

	msg := cmd()
	reportMsg, ok := msg.(connectionStatusMsg)
	if !ok {
		t.Fatalf("expected connectionStatusMsg, got %T", msg)
	}
	if reportMsg.report.Stage != sshclient.DiagnosticStageConfig {
		t.Fatalf("diagnostic stage = %q, want config", reportMsg.report.Stage)
	}
}

func nilRemoteFSOpener(config.ServerConfig) (sshclient.RemoteFS, error) {
	return nil, nil
}

func testServerConfig() config.ServerConfig {
	return config.ServerConfig{Name: "weecare", Host: "185.228.72.253", Port: "22", User: "root"}
}

type errString string

func (e errString) Error() string {
	return string(e)
}
