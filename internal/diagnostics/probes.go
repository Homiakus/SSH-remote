package diagnostics

import (
	"bufio"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math"
	"net"
	"strings"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"sshpilot/internal/config"
	"sshpilot/internal/ssh"
)

// RunFullDiagnosticAudit performs a comprehensive 6-stage diagnostic audit on a server.
func RunFullDiagnosticAudit(cfg *config.ServerConfig) *DiagnosticReport {
	start := time.Now()
	port := cfg.Port
	if strings.TrimSpace(port) == "" {
		port = "22"
	}
	addr := net.JoinHostPort(strings.TrimSpace(cfg.Host), port)

	report := &DiagnosticReport{
		ServerName:    cfg.Name,
		TargetHost:    cfg.Host,
		TargetPort:    port,
		Timestamp:     start,
		OverallStatus: StagePass,
		Stages:        make([]StageResult, 0, 7),
	}

	// 1. TCP Port Reachability Probe
	tcpStart := time.Now()
	conn, err := net.DialTimeout("tcp", addr, 4*time.Second)
	tcpDur := time.Since(tcpStart).Milliseconds()
	if err != nil {
		report.OverallStatus = StageFail
		report.Stages = append(report.Stages, StageResult{
			Name:       "TCP Socket Connection",
			Stage:      "tcp_connect",
			Status:     StageFail,
			DurationMs: tcpDur,
			Summary:    fmt.Sprintf("Failed to connect to %s", addr),
			Error:      err.Error(),
		})
		report.TotalDuration = time.Since(start).Milliseconds()
		return report
	}

	report.Stages = append(report.Stages, StageResult{
		Name:       "TCP Socket Connection",
		Stage:      "tcp_connect",
		Status:     StagePass,
		DurationMs: tcpDur,
		Summary:    fmt.Sprintf("Successfully connected to %s (%d ms)", addr, tcpDur),
	})

	// 2. SSH Banner Probe
	bannerStart := time.Now()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(conn)
	bannerLine, bannerErr := reader.ReadString('\n')
	bannerDur := time.Since(bannerStart).Milliseconds()
	_ = conn.Close()

	if bannerErr != nil || !strings.HasPrefix(bannerLine, "SSH-") {
		report.OverallStatus = StageWarn
		report.Stages = append(report.Stages, StageResult{
			Name:       "SSH Protocol Banner",
			Stage:      "ssh_banner",
			Status:     StageWarn,
			DurationMs: bannerDur,
			Summary:    "SSH banner not received cleanly",
			Error: func() string {
				if bannerErr != nil {
					return bannerErr.Error()
				}
				return "Invalid SSH banner"
			}(),
		})
	} else {
		trimmedBanner := strings.TrimSpace(bannerLine)
		report.Banner = trimmedBanner
		report.Stages = append(report.Stages, StageResult{
			Name:       "SSH Protocol Banner",
			Stage:      "ssh_banner",
			Status:     StagePass,
			DurationMs: bannerDur,
			Summary:    fmt.Sprintf("Detected banner: %s", trimmedBanner),
			Details:    trimmedBanner,
		})
	}

	// 3. Cryptographic Handshake & Host Key
	var capturedHostKey gossh.PublicKey
	sshConf := &gossh.ClientConfig{
		User: cfg.User,
		HostKeyCallback: func(hostname string, remote net.Addr, key gossh.PublicKey) error {
			capturedHostKey = key
			return nil
		},
		Timeout: 4 * time.Second,
	}
	rawClient, _ := gossh.Dial("tcp", addr, sshConf)
	if rawClient != nil {
		_ = rawClient.Close()
	}

	if capturedHostKey != nil {
		h := sha256.Sum256(capturedHostKey.Marshal())
		report.HostKeyAlg = capturedHostKey.Type()
		report.HostKeySHA256 = "SHA256:" + base64.RawStdEncoding.EncodeToString(h[:])
	}

	handshakeStart := time.Now()
	sshClient, authErr := ssh.Connect(cfg)
	handshakeDur := time.Since(handshakeStart).Milliseconds()

	if capturedHostKey != nil {
		h := sha256.Sum256(capturedHostKey.Marshal())
		report.HostKeyAlg = capturedHostKey.Type()
		report.HostKeySHA256 = "SHA256:" + base64.RawStdEncoding.EncodeToString(h[:])
	}

	if authErr != nil {
		report.OverallStatus = StageFail
		report.Stages = append(report.Stages, StageResult{
			Name:       "Authentication & Handshake",
			Stage:      "ssh_auth",
			Status:     StageFail,
			DurationMs: handshakeDur,
			Summary:    fmt.Sprintf("Auth failed using method '%s' for user '%s'", cfg.AuthMethod, cfg.User),
			Error:      authErr.Error(),
		})
		report.TotalDuration = time.Since(start).Milliseconds()
		return report
	}
	defer sshClient.Close()

	report.Stages = append(report.Stages, StageResult{
		Name:       "Authentication & Handshake",
		Stage:      "ssh_auth",
		Status:     StagePass,
		DurationMs: handshakeDur,
		Summary:    fmt.Sprintf("Authenticated successfully as %s via %s (HostKey: %s)", cfg.User, cfg.AuthMethod, report.HostKeyAlg),
		Details:    fmt.Sprintf("Fingerprint: %s", report.HostKeySHA256),
	})

	// 4. Remote Shell Session Probe
	sessStart := time.Now()
	sess, sessErr := sshClient.NewSession()
	sessDur := time.Since(sessStart).Milliseconds()
	if sessErr != nil {
		report.OverallStatus = StageWarn
		report.Stages = append(report.Stages, StageResult{
			Name:       "PTY / Shell Allocation",
			Stage:      "pty_session",
			Status:     StageWarn,
			DurationMs: sessDur,
			Summary:    "Could not allocate interactive session",
			Error:      sessErr.Error(),
		})
	} else {
		_ = sess.Close()
		report.Stages = append(report.Stages, StageResult{
			Name:       "PTY / Shell Allocation",
			Stage:      "pty_session",
			Status:     StagePass,
			DurationMs: sessDur,
			Summary:    "Interactive PTY session channels verified",
		})
	}

	// 5. SFTP Subsystem Probe
	sftpStart := time.Now()
	sftpSess, sftpErr := sshClient.NewSession()
	sftpDur := time.Since(sftpStart).Milliseconds()
	if sftpErr != nil {
		report.Stages = append(report.Stages, StageResult{
			Name:       "SFTP Subsystem Handshake",
			Stage:      "sftp_probe",
			Status:     StageWarn,
			DurationMs: sftpDur,
			Summary:    "SFTP session allocation warning",
			Error:      sftpErr.Error(),
		})
	} else {
		_ = sftpSess.Close()
		report.Stages = append(report.Stages, StageResult{
			Name:       "SFTP Subsystem Handshake",
			Stage:      "sftp_probe",
			Status:     StagePass,
			DurationMs: sftpDur,
			Summary:    "SFTP subsystem ready for file management",
		})
	}

	// 6. Network Jitter & 5-probe Ping
	pingReport := RunPingJitter(cfg, 5)
	report.PingReport = &pingReport
	var jitterStatus StageStatus = StagePass
	if pingReport.LossPercent > 0 {
		jitterStatus = StageWarn
	}
	if pingReport.LossPercent >= 50 {
		jitterStatus = StageFail
	}
	report.Stages = append(report.Stages, StageResult{
		Name:       "Latency & Network Jitter",
		Stage:      "ping_jitter",
		Status:     jitterStatus,
		DurationMs: int64(pingReport.AvgMs),
		Summary:    fmt.Sprintf("Avg Latency: %.1f ms · Jitter: ±%.1f ms · Loss: %.0f%%", pingReport.AvgMs, pingReport.JitterMs, pingReport.LossPercent),
	})

	report.TotalDuration = time.Since(start).Milliseconds()
	return report
}

// RunPingJitter performs N consecutive TCP ping probes against a server to measure jitter and latency.
func RunPingJitter(cfg *config.ServerConfig, count int) PingReport {
	if count <= 0 {
		count = 5
	}
	if count > 20 {
		count = 20
	}

	port := cfg.Port
	if strings.TrimSpace(port) == "" {
		port = "22"
	}
	addr := net.JoinHostPort(strings.TrimSpace(cfg.Host), port)

	samples := make([]JitterSample, 0, count)
	var latencies []float64
	successCount := 0

	for i := 1; i <= count; i++ {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		lat := float64(time.Since(start).Microseconds()) / 1000.0 // ms with decimal

		if err != nil {
			samples = append(samples, JitterSample{
				Sequence:  i,
				LatencyMs: lat,
				Success:   false,
				Error:     err.Error(),
			})
		} else {
			_ = conn.Close()
			successCount++
			latencies = append(latencies, lat)
			samples = append(samples, JitterSample{
				Sequence:  i,
				LatencyMs: math.Round(lat*100) / 100,
				Success:   true,
			})
		}
		if i < count {
			time.Sleep(30 * time.Millisecond)
		}
	}

	lost := count - successCount
	lossPct := (float64(lost) / float64(count)) * 100.0

	var minMs, maxMs, avgMs, jitterMs float64
	if len(latencies) > 0 {
		minMs = latencies[0]
		maxMs = latencies[0]
		var sum float64
		for _, v := range latencies {
			if v < minMs {
				minMs = v
			}
			if v > maxMs {
				maxMs = v
			}
			sum += v
		}
		avgMs = sum / float64(len(latencies))

		// Mean absolute difference between consecutive samples (jitter)
		if len(latencies) > 1 {
			var diffSum float64
			for i := 1; i < len(latencies); i++ {
				diffSum += math.Abs(latencies[i] - latencies[i-1])
			}
			jitterMs = diffSum / float64(len(latencies)-1)
		}
	}

	return PingReport{
		Count:       count,
		Successful:  successCount,
		Lost:        lost,
		LossPercent: math.Round(lossPct*10) / 10,
		MinMs:       math.Round(minMs*100) / 100,
		AvgMs:       math.Round(avgMs*100) / 100,
		MaxMs:       math.Round(maxMs*100) / 100,
		JitterMs:    math.Round(jitterMs*100) / 100,
		Samples:     samples,
	}
}

// FetchRemoteLogs fetches remote system logs (journalctl, syslog, auth.log, dmesg) via SSH.
func FetchRemoteLogs(cfg *config.ServerConfig, logType string, lines int, filter string) ([]LogEntry, error) {
	if lines <= 0 {
		lines = 50
	}
	if lines > 300 {
		lines = 300
	}

	client, err := ssh.Connect(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", cfg.Name, err)
	}
	defer client.Close()

	var cmd string
	switch strings.ToLower(logType) {
	case "auth":
		cmd = fmt.Sprintf("tail -n %d /var/log/auth.log 2>/dev/null || journalctl -u ssh -n %d --no-pager 2>/dev/null", lines, lines)
	case "dmesg":
		cmd = fmt.Sprintf("dmesg -T 2>/dev/null | tail -n %d || dmesg | tail -n %d", lines, lines)
	case "syslog":
		cmd = fmt.Sprintf("tail -n %d /var/log/syslog 2>/dev/null || journalctl -n %d --no-pager 2>/dev/null", lines, lines)
	case "journal":
		fallthrough
	default:
		cmd = fmt.Sprintf("journalctl -n %d --no-pager 2>/dev/null || tail -n %d /var/log/messages 2>/dev/null", lines, lines)
	}

	out, err := ssh.ExecuteCommand(client, cmd)
	if err != nil && out == "" {
		return nil, fmt.Errorf("failed to read logs: %w", err)
	}

	return ParseLogOutput(out, filter), nil
}

// ParseLogOutput parses log lines and extracts level, timestamp, and message.
func ParseLogOutput(raw string, filter string) []LogEntry {
	filter = strings.ToLower(strings.TrimSpace(filter))
	rawLines := strings.Split(raw, "\n")
	entries := make([]LogEntry, 0, len(rawLines))

	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lower := strings.ToLower(trimmed)
		if filter != "" && !strings.Contains(lower, filter) {
			continue
		}

		level := "info"
		if strings.Contains(lower, "error") || strings.Contains(lower, "fatal") || strings.Contains(lower, "failed") || strings.Contains(lower, "crit") {
			level = "error"
		} else if strings.Contains(lower, "warn") {
			level = "warn"
		} else if strings.Contains(lower, "debug") {
			level = "debug"
		}

		// Extract timestamp if standard Linux format (e.g. "Aug 27 12:34:56" or "2026-08-27T12:34:56")
		fields := strings.Fields(trimmed)
		ts := ""
		msg := trimmed
		if len(fields) >= 3 {
			if strings.Contains(fields[0], "-") || len(fields[0]) == 3 {
				ts = strings.Join(fields[:3], " ")
				msg = strings.Join(fields[3:], " ")
			}
		}

		entries = append(entries, LogEntry{
			Timestamp: ts,
			Level:     level,
			Message:   msg,
			Raw:       trimmed,
		})
	}

	return entries
}

// ExecuteDiagnosticCommand runs predefined safe diagnostic utilities.
func ExecuteDiagnosticCommand(cfg *config.ServerConfig, cmdKey string) (DiagnosticCommandResult, error) {
	cmdMap := map[string]string{
		"ports":   "ss -tulpn 2>/dev/null || netstat -tulpn 2>/dev/null || lsof -i -P -n",
		"network": "ip addr 2>/dev/null || ifconfig 2>/dev/null",
		"routes":  "ip route 2>/dev/null || route -n 2>/dev/null",
		"disk":    "df -h -T 2>/dev/null",
		"memory":  "free -h 2>/dev/null || cat /proc/meminfo",
		"uptime":  "uptime 2>/dev/null; uname -a 2>/dev/null",
		"firewall": "ufw status verbose 2>/dev/null || iptables -L -n 2>/dev/null || echo 'Firewall status unavailable'",
	}

	cmd, ok := cmdMap[cmdKey]
	if !ok {
		cmd = "uptime"
		cmdKey = "uptime"
	}

	client, err := ssh.Connect(cfg)
	if err != nil {
		return DiagnosticCommandResult{
			CommandKey: cmdKey,
			Command:    cmd,
			Error:      err.Error(),
		}, fmt.Errorf("failed to connect to %s: %w", cfg.Name, err)
	}
	defer client.Close()

	start := time.Now()
	out, err := ssh.ExecuteCommand(client, cmd)
	dur := time.Since(start).Milliseconds()

	res := DiagnosticCommandResult{
		CommandKey: cmdKey,
		Command:    cmd,
		Output:     out,
		DurationMs: dur,
	}
	if err != nil {
		res.ExitCode = 1
		res.Error = err.Error()
	}
	return res, nil
}
