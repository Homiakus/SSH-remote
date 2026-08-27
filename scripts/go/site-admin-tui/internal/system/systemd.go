package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sshpilot/scripts/go/site-admin-tui/internal/domain"
)

type ServiceUnit struct {
	Name    string
	Content string
}

func RenderServiceUnit(spec domain.SiteSpec, workingDir string) (ServiceUnit, error) {
	unitName := spec.Service.Name
	if unitName == "" {
		unitName = "site-admin-" + spec.Name
	}
	if err := validateUnitName(unitName); err != nil {
		return ServiceUnit{}, err
	}
	if err := domain.ValidateSiteName(spec.Name); err != nil {
		return ServiceUnit{}, err
	}
	if err := validateSystemdLine("working directory", workingDir); err != nil {
		return ServiceUnit{}, err
	}

	var execStart string
	switch spec.Runtime {
	case domain.RuntimeNode, domain.RuntimePython:
		if len(spec.Service.Command) == 0 {
			return ServiceUnit{}, fmt.Errorf("service command is required for %s", spec.Runtime)
		}
		for _, command := range spec.Service.Command {
			if err := validateSystemdLine("service command", command); err != nil {
				return ServiceUnit{}, err
			}
		}
		execStart = strings.Join(spec.Service.Command, " ")
	default:
		return ServiceUnit{}, fmt.Errorf("systemd unit is not supported for runtime %s", spec.Runtime)
	}

	content := strings.Join([]string{
		"[Unit]",
		"Description=site-admin managed site " + spec.Name,
		"After=network.target",
		"",
		"[Service]",
		"Type=simple",
		"WorkingDirectory=" + workingDir,
		"ExecStart=/usr/bin/env bash -lc '" + shellEscape(execStart) + "'",
		"Restart=always",
		"RestartSec=3",
		fmt.Sprintf("Environment=PORT=%d", spec.Service.Port),
		"",
		"[Install]",
		"WantedBy=multi-user.target",
		"",
	}, "\n")

	return ServiceUnit{Name: unitName, Content: content}, nil
}

func InstallServiceUnit(ctx context.Context, runner Runner, systemdDir string, unit ServiceUnit, backupDir string) error {
	if err := validateUnitName(unit.Name); err != nil {
		return err
	}
	if err := validateSystemdLine("systemd dir", systemdDir); err != nil {
		return err
	}
	if err := os.MkdirAll(systemdDir, 0o755); err != nil {
		return err
	}
	if backupDir != "" {
		if err := os.MkdirAll(backupDir, 0o755); err != nil {
			return err
		}
	}

	target := filepath.Join(systemdDir, unit.Name+".service")
	if !pathWithin(systemdDir, target) {
		return fmt.Errorf("unit path escapes systemd dir: %s", unit.Name)
	}
	if data, err := os.ReadFile(target); err == nil && backupDir != "" {
		backup := filepath.Join(backupDir, unit.Name+"-"+timestampSlug()+".service")
		if err := os.WriteFile(backup, data, 0o644); err != nil {
			return err
		}
	}

	if err := os.WriteFile(target, []byte(unit.Content), 0o644); err != nil {
		return err
	}

	if _, err := runner.Run(ctx, Command{Name: "systemctl", Args: []string{"daemon-reload"}}); err != nil {
		return err
	}
	if _, err := runner.Run(ctx, Command{Name: "systemctl", Args: []string{"enable", unit.Name + ".service"}}); err != nil {
		return err
	}
	return nil
}

func RestartService(ctx context.Context, runner Runner, unitName string) error {
	if unitName == "" {
		return fmt.Errorf("empty unit name")
	}
	if err := validateUnitName(unitName); err != nil {
		return err
	}
	_, err := runner.Run(ctx, Command{Name: "systemctl", Args: []string{"restart", unitName + ".service"}})
	return err
}

func shellEscape(value string) string {
	return strings.ReplaceAll(value, "'", `'"'"'`)
}

func timestampSlug() string {
	return time.Now().UTC().Format("20060102-150405")
}

func validateUnitName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("empty unit name")
	}
	if name == "." || name == ".." || strings.HasSuffix(name, ".service") {
		return fmt.Errorf("unit name must be a basename without .service suffix: %q", name)
	}
	if len(name) > 128 || strings.ContainsRune(name, 0) || strings.ContainsAny(name, `/\ `+"\t\r\n") {
		return fmt.Errorf("unit name contains invalid characters: %q", name)
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '@' {
			continue
		}
		return fmt.Errorf("unit name must contain only ASCII unit-name characters: %q", name)
	}
	return nil
}

func validateSystemdLine(field, value string) error {
	if strings.ContainsRune(value, 0) || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be a single line", field)
	}
	return nil
}

func pathWithin(base, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
