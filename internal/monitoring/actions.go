package monitoring

import (
	"fmt"
	"strings"

	"sshpilot/internal/config"
	"sshpilot/internal/ssh"
)

// KillProcess sends SIGTERM or SIGKILL to a PID on the remote server.
func KillProcess(cfg *config.ServerConfig, pid int, force bool) error {
	if pid <= 1 {
		return fmt.Errorf("invalid PID %d: refusing to kill system root process", pid)
	}

	client, err := ssh.Connect(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", cfg.Name, err)
	}
	defer client.Close()

	sig := "-15"
	if force {
		sig = "-9"
	}
	cmd := fmt.Sprintf("kill %s %d", sig, pid)
	out, err := ssh.ExecuteCommand(client, cmd)
	if err != nil && !strings.Contains(out, "No such process") {
		return fmt.Errorf("kill failed: %w (output: %s)", err, out)
	}
	return nil
}

// ControlService runs systemctl start/stop/restart on a systemd service unit.
func ControlService(cfg *config.ServerConfig, serviceName string, action string) error {
	serviceName = strings.TrimSpace(serviceName)
	action = strings.ToLower(strings.TrimSpace(action))

	switch action {
	case "start", "stop", "restart", "reload", "status":
	default:
		return fmt.Errorf("unsupported service action: %s (supported: start, stop, restart, reload, status)", action)
	}

	// Basic safety sanitization
	if strings.ContainsAny(serviceName, ";|&$`\n") {
		return fmt.Errorf("invalid service name characters")
	}

	client, err := ssh.Connect(cfg)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %w", cfg.Name, err)
	}
	defer client.Close()

	cmd := fmt.Sprintf("systemctl %s %s 2>&1", action, serviceName)
	out, err := ssh.ExecuteCommand(client, cmd)
	if err != nil {
		return fmt.Errorf("systemctl %s %s failed: %w (output: %s)", action, serviceName, err, out)
	}
	return nil
}
