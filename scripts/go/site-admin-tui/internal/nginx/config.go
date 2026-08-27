package nginx

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sshpilot/scripts/go/site-admin-tui/internal/system"
)

type Mode string

const (
	ModeStatic  Mode = "static"
	ModeFastCGI Mode = "fastcgi"
	ModeProxy   Mode = "proxy"
)

type SiteConfig struct {
	Name        string
	Domain      string
	Mode        Mode
	Root        string
	ProxyPass   string
	FastCGIPass string
	EnableTLS   bool
	TLSCertPath string
	TLSKeyPath  string
	Webroot     string
}

func RenderSite(cfg SiteConfig) (string, error) {
	if err := validateSiteConfig(cfg); err != nil {
		return "", err
	}

	var location string
	switch cfg.Mode {
	case ModeStatic:
		location = strings.Join([]string{
			"location / {",
			"    try_files $uri $uri/ /index.html;",
			"}",
		}, "\n")
	case ModeFastCGI:
		location = strings.Join([]string{
			"location / {",
			"    try_files $uri $uri/ /index.php?$query_string;",
			"}",
			"location ~ \\.php$ {",
			"    include snippets/fastcgi-php.conf;",
			"    fastcgi_pass " + cfg.FastCGIPass + ";",
			"}",
		}, "\n")
	case ModeProxy:
		location = strings.Join([]string{
			"location / {",
			"    proxy_set_header Host $host;",
			"    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;",
			"    proxy_set_header X-Forwarded-Proto $scheme;",
			"    proxy_pass " + cfg.ProxyPass + ";",
			"}",
		}, "\n")
	default:
		return "", fmt.Errorf("unsupported nginx mode %q", cfg.Mode)
	}

	httpServer := []string{
		"server {",
		"    listen 80;",
		"    listen [::]:80;",
		"    server_name " + cfg.Domain + ";",
	}
	if cfg.Webroot != "" {
		httpServer = append(httpServer,
			"    location /.well-known/acme-challenge/ {",
			"        root "+nginxDirectiveValue(cfg.Webroot)+";",
			"    }")
	}
	httpServer = append(httpServer,
		"    root "+nginxDirectiveValue(cfg.Root)+";",
		indent(location, "    "),
		"}")

	parts := []string{strings.Join(httpServer, "\n")}
	if cfg.EnableTLS {
		parts = append(parts, strings.Join([]string{
			"server {",
			"    listen 443 ssl http2;",
			"    listen [::]:443 ssl http2;",
			"    server_name " + cfg.Domain + ";",
			"    root " + nginxDirectiveValue(cfg.Root) + ";",
			"    ssl_certificate " + nginxDirectiveValue(cfg.TLSCertPath) + ";",
			"    ssl_certificate_key " + nginxDirectiveValue(cfg.TLSKeyPath) + ";",
			indent(location, "    "),
			"}",
		}, "\n"))
	}

	return strings.Join(parts, "\n\n"), nil
}

func InstallSite(ctx context.Context, runner system.Runner, availableDir, enabledDir string, cfg SiteConfig, content string, backupDir string) error {
	if err := validateNginxName(cfg.Name); err != nil {
		return err
	}
	for _, dir := range []string{availableDir, enabledDir, backupDir} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	target := filepath.Join(availableDir, cfg.Name+".conf")
	if existing, err := os.ReadFile(target); err == nil && backupDir != "" {
		backup := filepath.Join(backupDir, fmt.Sprintf("%s-%s.conf", cfg.Name, time.Now().UTC().Format("20060102-150405")))
		if err := os.WriteFile(backup, existing, 0o644); err != nil {
			return err
		}
	}
	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return err
	}

	link := filepath.Join(enabledDir, cfg.Name+".conf")
	if _, err := os.Lstat(link); err == nil {
		_ = os.Remove(link)
	}
	if err := os.Symlink(target, link); err != nil && !os.IsExist(err) {
		return err
	}

	if _, err := runner.Run(ctx, system.Command{Name: "nginx", Args: []string{"-t"}}); err != nil {
		return err
	}
	_, err := runner.Run(ctx, system.Command{Name: "systemctl", Args: []string{"reload", "nginx"}})
	return err
}

func validateSiteConfig(cfg SiteConfig) error {
	if err := validateNginxName(cfg.Name); err != nil {
		return err
	}
	if err := validateNginxDomain(cfg.Domain); err != nil {
		return err
	}
	if err := validateNginxDirectiveValue("root", cfg.Root, true); err != nil {
		return err
	}
	if cfg.Webroot != "" {
		if err := validateNginxDirectiveValue("webroot", cfg.Webroot, true); err != nil {
			return err
		}
	}
	switch cfg.Mode {
	case ModeStatic:
	case ModeFastCGI:
		if err := validateNginxDirectiveValue("fastcgi_pass", cfg.FastCGIPass, true); err != nil {
			return err
		}
		if strings.ContainsAny(cfg.FastCGIPass, " \t") {
			return fmt.Errorf("fastcgi_pass must not contain whitespace: %q", cfg.FastCGIPass)
		}
	case ModeProxy:
		if err := validateProxyPass(cfg.ProxyPass); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported nginx mode %q", cfg.Mode)
	}
	if cfg.EnableTLS {
		if err := validateNginxDirectiveValue("tls cert path", cfg.TLSCertPath, true); err != nil {
			return err
		}
		if err := validateNginxDirectiveValue("tls key path", cfg.TLSKeyPath, true); err != nil {
			return err
		}
	}
	return nil
}

func validateNginxName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("nginx site name is required")
	}
	if value == "." || value == ".." || filepath.IsAbs(value) || strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("nginx site name must be a safe filename: %q", value)
	}
	if strings.ContainsRune(value, 0) || strings.ContainsAny(value, `<>:"|?*;{}`) {
		return fmt.Errorf("nginx site name contains invalid characters: %q", value)
	}
	return nil
}

func validateNginxDomain(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("nginx domain is required")
	}
	if strings.ContainsRune(value, 0) || strings.ContainsAny(value, " \t\r\n/\\;{}") {
		return fmt.Errorf("nginx domain contains invalid characters: %q", value)
	}
	return nil
}

func validateNginxDirectiveValue(field, value string, required bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	if strings.ContainsRune(value, 0) || strings.ContainsAny(value, "\r\n;{}") {
		return fmt.Errorf("%s contains characters unsafe for raw nginx directives: %q", field, value)
	}
	return nil
}

func validateProxyPass(value string) error {
	if err := validateNginxDirectiveValue("proxy_pass", value, true); err != nil {
		return err
	}
	if strings.ContainsAny(value, " \t") {
		return fmt.Errorf("proxy_pass must not contain whitespace: %q", value)
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return fmt.Errorf("proxy_pass is invalid: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("proxy_pass must use http or https: %q", value)
	}
	if parsed.Host == "" {
		return fmt.Errorf("proxy_pass host is required: %q", value)
	}
	return nil
}

func nginxDirectiveValue(value string) string {
	if !strings.ContainsAny(value, " \t\"") {
		return value
	}
	escaped := strings.ReplaceAll(value, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return `"` + escaped + `"`
}

func indent(value, prefix string) string {
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
