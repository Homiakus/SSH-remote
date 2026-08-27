package diagnostics

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"sshpilot/internal/config"
)

func TestParseLogOutput(t *testing.T) {
	rawLogs := `Aug 27 12:00:01 server systemd[1]: Starting Daily apt upgrade...
Aug 27 12:00:02 server sshd[1234]: Failed password for root from 1.2.3.4 port 2222
Aug 27 12:00:03 server kernel: [123.456] warning: low memory buffer
Aug 27 12:00:04 server app[999]: normal informational log line`

	entries := ParseLogOutput(rawLogs, "")
	if len(entries) != 4 {
		t.Fatalf("entries count = %d, want 4", len(entries))
	}

	if entries[1].Level != "error" {
		t.Fatalf("entries[1].Level = %q, want error", entries[1].Level)
	}
	if entries[2].Level != "warn" {
		t.Fatalf("entries[2].Level = %q, want warn", entries[2].Level)
	}
	if entries[3].Level != "info" {
		t.Fatalf("entries[3].Level = %q, want info", entries[3].Level)
	}

	filtered := ParseLogOutput(rawLogs, "failed")
	if len(filtered) != 1 {
		t.Fatalf("filtered count = %d, want 1", len(filtered))
	}
}

func TestRunPingJitter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	host, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	cfg := &config.ServerConfig{
		Host: host,
		Port: strconv.Itoa(port),
	}

	res := RunPingJitter(cfg, 5)
	if res.Count != 5 {
		t.Fatalf("count = %d, want 5", res.Count)
	}
	if res.Successful != 5 {
		t.Fatalf("successful = %d, want 5", res.Successful)
	}
	if res.LossPercent != 0.0 {
		t.Fatalf("loss percent = %v, want 0", res.LossPercent)
	}
	if len(res.Samples) != 5 {
		t.Fatalf("samples count = %d, want 5", len(res.Samples))
	}
}
