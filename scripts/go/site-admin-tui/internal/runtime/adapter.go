package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sshpilot/scripts/go/site-admin-tui/internal/domain"
	"sshpilot/scripts/go/site-admin-tui/internal/nginx"
	"sshpilot/scripts/go/site-admin-tui/internal/system"
)

type Dependencies struct {
	Runner system.Runner
	Config domain.AppConfig
}

type Activation struct {
	Nginx      nginx.SiteConfig
	Service    *system.ServiceUnit
	Health     domain.HealthCheck
	WorkingDir string
}

type Adapter interface {
	Runtime() domain.RuntimeType
	Plan(spec domain.SiteSpec) []string
	PrepareRelease(context.Context, Dependencies, domain.SiteSpec, string) (Activation, error)
	Activate(context.Context, Dependencies, domain.SiteSpec, string, string, Activation) error
	Restart(context.Context, Dependencies, domain.SiteSpec, string, Activation) error
	HealthCheck(context.Context, domain.SiteSpec, Activation) (domain.HealthStatus, error)
	Rollback(context.Context, Dependencies, domain.SiteSpec, string, string, Activation) error
}

func NewRegistry() map[domain.RuntimeType]Adapter {
	return map[domain.RuntimeType]Adapter{
		domain.RuntimeStatic:        staticAdapter{},
		domain.RuntimePHP:           phpAdapter{},
		domain.RuntimeNode:          nodeAdapter{},
		domain.RuntimePython:        pythonAdapter{},
		domain.RuntimeDockerCompose: dockerAdapter{},
	}
}

type staticAdapter struct{}
type phpAdapter struct{}
type nodeAdapter struct{}
type pythonAdapter struct{}
type dockerAdapter struct{}

func (staticAdapter) Runtime() domain.RuntimeType { return domain.RuntimeStatic }
func (phpAdapter) Runtime() domain.RuntimeType    { return domain.RuntimePHP }
func (nodeAdapter) Runtime() domain.RuntimeType   { return domain.RuntimeNode }
func (pythonAdapter) Runtime() domain.RuntimeType { return domain.RuntimePython }
func (dockerAdapter) Runtime() domain.RuntimeType { return domain.RuntimeDockerCompose }

func (staticAdapter) Plan(spec domain.SiteSpec) []string {
	return []string{
		"Copy release files into atomic release directory",
		"Switch nginx root to current release",
		"Run HTTP health-check via nginx",
	}
}

func (phpAdapter) Plan(spec domain.SiteSpec) []string {
	return []string{
		"Copy PHP application files into atomic release directory",
		"Generate nginx fastcgi config",
		"Reload nginx and validate PHP-FPM upstream",
	}
}

func (nodeAdapter) Plan(spec domain.SiteSpec) []string {
	return []string{
		"Install node dependencies inside release directory",
		"Optionally run npm build",
		"Install systemd unit and proxy traffic via nginx",
	}
}

func (pythonAdapter) Plan(spec domain.SiteSpec) []string {
	return []string{
		"Create Python virtualenv in release directory",
		"Install requirements into virtualenv",
		"Install gunicorn/systemd service and nginx reverse proxy",
	}
}

func (dockerAdapter) Plan(spec domain.SiteSpec) []string {
	return []string{
		"Validate docker compose configuration in release directory",
		"Start containers from current release",
		"Proxy public traffic through nginx if port is configured",
	}
}

func (staticAdapter) PrepareRelease(_ context.Context, _ Dependencies, spec domain.SiteSpec, releasePath string) (Activation, error) {
	root := resolveRuntimeRoot(spec, releasePath)
	return Activation{
		Nginx: nginx.SiteConfig{
			Name:    spec.Name,
			Domain:  spec.Domain,
			Mode:    nginx.ModeStatic,
			Root:    root,
			Webroot: spec.TLS.Webroot,
		},
		Health: defaultWebHealth(spec, 80),
	}, nil
}

func (phpAdapter) PrepareRelease(_ context.Context, deps Dependencies, spec domain.SiteSpec, releasePath string) (Activation, error) {
	root := resolveRuntimeRoot(spec, releasePath)
	phpService := spec.Service.PHPFPMService
	if phpService == "" {
		phpService = deps.Config.DefaultPHPFPM
	}
	socket := phpService
	if !strings.HasPrefix(socket, "unix:") && !strings.Contains(socket, ".sock") {
		socket = "unix:/run/php/" + phpService + ".sock"
	}
	return Activation{
		Nginx: nginx.SiteConfig{
			Name:        spec.Name,
			Domain:      spec.Domain,
			Mode:        nginx.ModeFastCGI,
			Root:        root,
			FastCGIPass: socket,
			Webroot:     spec.TLS.Webroot,
		},
		Health: defaultWebHealth(spec, 80),
	}, nil
}

func (nodeAdapter) PrepareRelease(ctx context.Context, deps Dependencies, spec domain.SiteSpec, releasePath string) (Activation, error) {
	appDir := resolveRuntimeRoot(spec, releasePath)
	if _, err := os.Stat(filepath.Join(appDir, "package.json")); err == nil {
		if _, err := deps.Runner.Run(ctx, system.Command{Name: "npm", Args: []string{"install", "--omit=dev"}, Dir: appDir}); err != nil {
			return Activation{}, err
		}
		hasBuild, err := packageHasScript(filepath.Join(appDir, "package.json"), "build")
		if err != nil {
			return Activation{}, err
		}
		if hasBuild {
			if _, err := deps.Runner.Run(ctx, system.Command{Name: "npm", Args: []string{"run", "build"}, Dir: appDir}); err != nil {
				return Activation{}, err
			}
		}
	}

	return Activation{
		Nginx: nginx.SiteConfig{
			Name:      spec.Name,
			Domain:    spec.Domain,
			Mode:      nginx.ModeProxy,
			Root:      appDir,
			ProxyPass: fmt.Sprintf("http://127.0.0.1:%d", spec.Service.Port),
			Webroot:   spec.TLS.Webroot,
		},
		Health:     defaultLoopbackHealth(spec, spec.Service.Port),
		WorkingDir: appDir,
	}, nil
}

func (pythonAdapter) PrepareRelease(ctx context.Context, deps Dependencies, spec domain.SiteSpec, releasePath string) (Activation, error) {
	appDir := resolveRuntimeRoot(spec, releasePath)
	if _, err := deps.Runner.Run(ctx, system.Command{Name: "python3", Args: []string{"-m", "venv", ".venv"}, Dir: appDir}); err != nil {
		return Activation{}, err
	}
	if _, err := os.Stat(filepath.Join(appDir, "requirements.txt")); err == nil {
		if _, err := deps.Runner.Run(ctx, system.Command{
			Name: "bash",
			Args: []string{"-lc", ". .venv/bin/activate && pip install -r requirements.txt"},
			Dir:  appDir,
		}); err != nil {
			return Activation{}, err
		}
	}
	return Activation{
		Nginx: nginx.SiteConfig{
			Name:      spec.Name,
			Domain:    spec.Domain,
			Mode:      nginx.ModeProxy,
			Root:      appDir,
			ProxyPass: fmt.Sprintf("http://127.0.0.1:%d", spec.Service.Port),
			Webroot:   spec.TLS.Webroot,
		},
		Health:     defaultLoopbackHealth(spec, spec.Service.Port),
		WorkingDir: appDir,
	}, nil
}

func (dockerAdapter) PrepareRelease(ctx context.Context, deps Dependencies, spec domain.SiteSpec, releasePath string) (Activation, error) {
	appDir := resolveRuntimeRoot(spec, releasePath)
	composeFile := spec.Service.ComposeFile
	if composeFile == "" {
		for _, candidate := range []string{"docker-compose.yml", "compose.yml"} {
			if _, err := os.Stat(filepath.Join(appDir, candidate)); err == nil {
				composeFile = candidate
				break
			}
		}
	}
	if composeFile == "" {
		return Activation{}, fmt.Errorf("docker compose file not found")
	}
	if _, err := deps.Runner.Run(ctx, system.Command{
		Name: "docker",
		Args: []string{"compose", "-f", composeFile, "config", "-q"},
		Dir:  appDir,
	}); err != nil {
		return Activation{}, err
	}

	activation := Activation{
		WorkingDir: appDir,
		Health:     defaultLoopbackHealth(spec, spec.Service.Port),
	}
	if spec.Service.Port > 0 {
		activation.Nginx = nginx.SiteConfig{
			Name:      spec.Name,
			Domain:    spec.Domain,
			Mode:      nginx.ModeProxy,
			Root:      appDir,
			ProxyPass: fmt.Sprintf("http://127.0.0.1:%d", spec.Service.Port),
			Webroot:   spec.TLS.Webroot,
		}
	}
	return activation, nil
}

func (staticAdapter) Activate(context.Context, Dependencies, domain.SiteSpec, string, string, Activation) error {
	return nil
}

func (phpAdapter) Activate(context.Context, Dependencies, domain.SiteSpec, string, string, Activation) error {
	return nil
}

func (nodeAdapter) Activate(ctx context.Context, deps Dependencies, spec domain.SiteSpec, currentLink string, backupDir string, _ Activation) error {
	unit, err := system.RenderServiceUnit(spec, resolveRuntimeRoot(spec, currentLink))
	if err != nil {
		return err
	}
	if err := system.InstallServiceUnit(ctx, deps.Runner, deps.Config.SystemdDir, unit, backupDir); err != nil {
		return err
	}
	return system.RestartService(ctx, deps.Runner, unit.Name)
}

func (pythonAdapter) Activate(ctx context.Context, deps Dependencies, spec domain.SiteSpec, currentLink string, backupDir string, _ Activation) error {
	if len(spec.Service.Command) == 0 {
		spec.Service.Command = []string{
			". .venv/bin/activate && gunicorn app:app --bind 127.0.0.1:" + fmt.Sprintf("%d", spec.Service.Port),
		}
	}
	unit, err := system.RenderServiceUnit(spec, resolveRuntimeRoot(spec, currentLink))
	if err != nil {
		return err
	}
	if err := system.InstallServiceUnit(ctx, deps.Runner, deps.Config.SystemdDir, unit, backupDir); err != nil {
		return err
	}
	return system.RestartService(ctx, deps.Runner, unit.Name)
}

func (dockerAdapter) Activate(ctx context.Context, deps Dependencies, spec domain.SiteSpec, currentLink string, _ string, _ Activation) error {
	composeFile := spec.Service.ComposeFile
	if composeFile == "" {
		composeFile = defaultComposeFile(resolveRuntimeRoot(spec, currentLink))
	}
	_, err := deps.Runner.Run(ctx, system.Command{
		Name: "docker",
		Args: []string{"compose", "-f", composeFile, "up", "-d"},
		Dir:  resolveRuntimeRoot(spec, currentLink),
	})
	return err
}

func (staticAdapter) Restart(context.Context, Dependencies, domain.SiteSpec, string, Activation) error {
	return nil
}
func (phpAdapter) Restart(context.Context, Dependencies, domain.SiteSpec, string, Activation) error {
	return nil
}

func (nodeAdapter) Restart(ctx context.Context, deps Dependencies, spec domain.SiteSpec, _ string, _ Activation) error {
	unitName := spec.Service.Name
	if unitName == "" {
		unitName = "site-admin-" + spec.Name
	}
	return system.RestartService(ctx, deps.Runner, unitName)
}

func (pythonAdapter) Restart(ctx context.Context, deps Dependencies, spec domain.SiteSpec, _ string, _ Activation) error {
	unitName := spec.Service.Name
	if unitName == "" {
		unitName = "site-admin-" + spec.Name
	}
	return system.RestartService(ctx, deps.Runner, unitName)
}

func (dockerAdapter) Restart(ctx context.Context, deps Dependencies, spec domain.SiteSpec, currentLink string, _ Activation) error {
	composeFile := spec.Service.ComposeFile
	if composeFile == "" {
		composeFile = defaultComposeFile(resolveRuntimeRoot(spec, currentLink))
	}
	_, err := deps.Runner.Run(ctx, system.Command{
		Name: "docker",
		Args: []string{"compose", "-f", composeFile, "up", "-d"},
		Dir:  resolveRuntimeRoot(spec, currentLink),
	})
	return err
}

func (staticAdapter) HealthCheck(ctx context.Context, spec domain.SiteSpec, activation Activation) (domain.HealthStatus, error) {
	return checkHTTP(ctx, defaultHealth(activation.Health, spec))
}

func (phpAdapter) HealthCheck(ctx context.Context, spec domain.SiteSpec, activation Activation) (domain.HealthStatus, error) {
	return checkHTTP(ctx, defaultHealth(activation.Health, spec))
}

func (nodeAdapter) HealthCheck(ctx context.Context, spec domain.SiteSpec, activation Activation) (domain.HealthStatus, error) {
	return checkHTTP(ctx, defaultHealth(activation.Health, spec))
}

func (pythonAdapter) HealthCheck(ctx context.Context, spec domain.SiteSpec, activation Activation) (domain.HealthStatus, error) {
	return checkHTTP(ctx, defaultHealth(activation.Health, spec))
}

func (dockerAdapter) HealthCheck(ctx context.Context, spec domain.SiteSpec, activation Activation) (domain.HealthStatus, error) {
	return checkHTTP(ctx, defaultHealth(activation.Health, spec))
}

func (staticAdapter) Rollback(ctx context.Context, deps Dependencies, spec domain.SiteSpec, currentLink string, backupDir string, activation Activation) error {
	return staticAdapter{}.Restart(ctx, deps, spec, currentLink, activation)
}

func (phpAdapter) Rollback(ctx context.Context, deps Dependencies, spec domain.SiteSpec, currentLink string, backupDir string, activation Activation) error {
	return phpAdapter{}.Restart(ctx, deps, spec, currentLink, activation)
}

func (nodeAdapter) Rollback(ctx context.Context, deps Dependencies, spec domain.SiteSpec, currentLink string, backupDir string, activation Activation) error {
	return nodeAdapter{}.Restart(ctx, deps, spec, currentLink, activation)
}

func (pythonAdapter) Rollback(ctx context.Context, deps Dependencies, spec domain.SiteSpec, currentLink string, backupDir string, activation Activation) error {
	return pythonAdapter{}.Restart(ctx, deps, spec, currentLink, activation)
}

func (dockerAdapter) Rollback(ctx context.Context, deps Dependencies, spec domain.SiteSpec, currentLink string, backupDir string, activation Activation) error {
	return dockerAdapter{}.Restart(ctx, deps, spec, currentLink, activation)
}

func resolveRuntimeRoot(spec domain.SiteSpec, releasePath string) string {
	if spec.RootDir == "" || spec.RootDir == "." {
		return releasePath
	}
	return filepath.Join(releasePath, spec.RootDir)
}

func defaultWebHealth(spec domain.SiteSpec, port int) domain.HealthCheck {
	return defaultHealth(spec.Health, domain.SiteSpec{
		Domain: spec.Domain,
	})
}

func defaultLoopbackHealth(spec domain.SiteSpec, port int) domain.HealthCheck {
	health := spec.Health.WithDefaults()
	if health.URL == "" {
		health.URL = fmt.Sprintf("http://127.0.0.1:%d%s", port, health.Path)
	}
	return health
}

func defaultHealth(health domain.HealthCheck, spec domain.SiteSpec) domain.HealthCheck {
	health = health.WithDefaults()
	if health.URL == "" {
		health.URL = "http://127.0.0.1" + health.Path
	}
	if health.Host == "" {
		health.Host = spec.Domain
	}
	return health
}

func checkHTTP(ctx context.Context, health domain.HealthCheck) (domain.HealthStatus, error) {
	health = health.WithDefaults()
	client := &http.Client{Timeout: time.Duration(health.TimeoutSec) * time.Second}
	var lastErr error
	for attempt := 0; attempt < health.Attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, health.URL, nil)
		if err != nil {
			return domain.HealthFailed, err
		}
		if health.Host != "" {
			req.Host = health.Host
		}
		resp, err := client.Do(req)
		if err == nil && resp != nil {
			resp.Body.Close()
			if resp.StatusCode == health.ExpectedStatus {
				return domain.HealthHealthy, nil
			}
			lastErr = fmt.Errorf("unexpected status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(time.Duration(health.IntervalSec) * time.Second)
	}
	return domain.HealthFailed, lastErr
}

func packageHasScript(path, name string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	var parsed struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return false, err
	}
	_, ok := parsed.Scripts[name]
	return ok, nil
}

func defaultComposeFile(dir string) string {
	for _, candidate := range []string{"docker-compose.yml", "compose.yml"} {
		if _, err := os.Stat(filepath.Join(dir, candidate)); err == nil {
			return candidate
		}
	}
	return "docker-compose.yml"
}
