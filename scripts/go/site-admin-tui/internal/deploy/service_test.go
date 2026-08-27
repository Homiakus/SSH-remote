package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sshpilot/scripts/go/site-admin-tui/internal/domain"
	"sshpilot/scripts/go/site-admin-tui/internal/state"
	"sshpilot/scripts/go/site-admin-tui/internal/system"
)

func TestDeployGitStatic(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo-static")
	mustWriteFile(t, filepath.Join(repo, "index.html"), "hello")

	service, runner := newTestService(t, root, func(_ context.Context, _ domain.HealthCheck) (domain.HealthStatus, error) {
		return domain.HealthHealthy, nil
	})
	runner.Handler = gitAwareHandler(t)

	result, err := service.Deploy(context.Background(), domain.SiteSpec{
		Name:    "static-demo",
		Domain:  "static.test",
		Runtime: domain.RuntimeStatic,
		Source: domain.DeploySource{
			Kind: domain.SourceGit,
			Repo: repo,
		},
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}
	if result.Release.Status != domain.ReleaseActive {
		t.Fatalf("expected active release, got %s", result.Release.Status)
	}
	if _, err := os.Stat(filepath.Join(result.CurrentPath, "index.html")); err != nil {
		t.Fatalf("expected deployed file: %v", err)
	}
	assertCallContains(t, runner.Calls, "git clone")
	assertCallContains(t, runner.Calls, "nginx -t")
}

func TestDeployGitNodeRunsBuild(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := filepath.Join(root, "repo-node")
	mustWriteFile(t, filepath.Join(repo, "package.json"), `{"scripts":{"build":"echo build"}}`)

	service, runner := newTestService(t, root, func(_ context.Context, _ domain.HealthCheck) (domain.HealthStatus, error) {
		return domain.HealthHealthy, nil
	})
	runner.Handler = gitAwareHandler(t)

	_, err := service.Deploy(context.Background(), domain.SiteSpec{
		Name:    "node-demo",
		Domain:  "node.test",
		Runtime: domain.RuntimeNode,
		Source: domain.DeploySource{
			Kind: domain.SourceGit,
			Repo: repo,
		},
		Service: domain.ServiceSpec{
			Port:    3100,
			Command: []string{"node server.js"},
		},
	})
	if err != nil {
		t.Fatalf("deploy: %v", err)
	}

	assertCallContains(t, runner.Calls, "npm install --omit=dev")
	assertCallContains(t, runner.Calls, "npm run build")
	assertCallContains(t, runner.Calls, "systemctl restart site-admin-node-demo.service")
}

func TestImportExistingDir(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "source")
	mustWriteFile(t, filepath.Join(sourceDir, "app.txt"), "import-me")

	service, _ := newTestService(t, root, func(_ context.Context, _ domain.HealthCheck) (domain.HealthStatus, error) {
		return domain.HealthHealthy, nil
	})

	result, err := service.Import(context.Background(), domain.SiteSpec{
		Name:    "import-demo",
		Domain:  "import.test",
		Runtime: domain.RuntimeStatic,
		Source: domain.DeploySource{
			ExistingDir: sourceDir,
		},
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if _, err := os.Stat(filepath.Join(result.CurrentPath, "app.txt")); err != nil {
		t.Fatalf("expected imported file: %v", err)
	}
}

func TestRollbackAfterFailedHealthCheck(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceA := filepath.Join(root, "source-a")
	sourceB := filepath.Join(root, "source-b")
	mustWriteFile(t, filepath.Join(sourceA, "index.html"), "v1")
	mustWriteFile(t, filepath.Join(sourceB, "index.html"), "v2")

	service, _ := newTestService(t, root, func(_ context.Context, _ domain.HealthCheck) (domain.HealthStatus, error) {
		return domain.HealthHealthy, nil
	})

	initial, err := service.Import(context.Background(), domain.SiteSpec{
		Name:    "rollback-demo",
		Domain:  "rollback.test",
		Runtime: domain.RuntimeStatic,
		Source: domain.DeploySource{
			ExistingDir: sourceA,
		},
	})
	if err != nil {
		t.Fatalf("initial import: %v", err)
	}

	service.healthChecker = func(_ context.Context, _ domain.HealthCheck) (domain.HealthStatus, error) {
		return domain.HealthFailed, nil
	}
	_, err = service.Import(context.Background(), domain.SiteSpec{
		Name:    "rollback-demo",
		Domain:  "rollback.test",
		Runtime: domain.RuntimeStatic,
		Source: domain.DeploySource{
			ExistingDir: sourceB,
		},
	})
	if err == nil {
		t.Fatal("expected second deploy to fail")
	}

	current, err := service.Store().CurrentReleasePath("rollback-demo")
	if err != nil {
		t.Fatalf("current release: %v", err)
	}
	if current != initial.CurrentPath {
		t.Fatalf("expected rollback to restore %q, got %q", initial.CurrentPath, current)
	}
}

func TestDockerRedeploy(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "compose-app")
	mustWriteFile(t, filepath.Join(sourceDir, "docker-compose.yml"), "services: {}\n")

	service, runner := newTestService(t, root, func(_ context.Context, _ domain.HealthCheck) (domain.HealthStatus, error) {
		return domain.HealthHealthy, nil
	})

	spec := domain.SiteSpec{
		Name:    "compose-demo",
		Domain:  "compose.test",
		Runtime: domain.RuntimeDockerCompose,
		Source: domain.DeploySource{
			ExistingDir: sourceDir,
		},
		Service: domain.ServiceSpec{
			Port:        8088,
			ComposeFile: "docker-compose.yml",
		},
	}
	if _, err := service.Import(context.Background(), spec); err != nil {
		t.Fatalf("initial import: %v", err)
	}
	if _, err := service.Redeploy(context.Background(), "compose-demo"); err != nil {
		t.Fatalf("redeploy: %v", err)
	}

	assertCallContains(t, runner.Calls, "docker compose -f docker-compose.yml up -d")
}

func TestDeployBlockedByExistingLock(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sourceDir := filepath.Join(root, "locked-source")
	mustWriteFile(t, filepath.Join(sourceDir, "index.html"), "locked")

	service, _ := newTestService(t, root, func(_ context.Context, _ domain.HealthCheck) (domain.HealthStatus, error) {
		return domain.HealthHealthy, nil
	})

	unlock, err := service.Store().AcquireSiteLock("locked-demo")
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	defer unlock()

	_, err = service.Import(context.Background(), domain.SiteSpec{
		Name:    "locked-demo",
		Domain:  "locked.test",
		Runtime: domain.RuntimeStatic,
		Source: domain.DeploySource{
			ExistingDir: sourceDir,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "already locked") {
		t.Fatalf("expected lock error, got %v", err)
	}
}

func TestPreviewRejectsTraversalInReleaseRelativePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	service, _ := newTestService(t, root, func(_ context.Context, _ domain.HealthCheck) (domain.HealthStatus, error) {
		return domain.HealthHealthy, nil
	})

	_, err := service.Preview(domain.SiteSpec{
		Name:    "unsafe-demo",
		Domain:  "unsafe.test",
		Runtime: domain.RuntimeStatic,
		Source: domain.DeploySource{
			Kind:        domain.SourceExistingDir,
			ExistingDir: filepath.Join(root, "source"),
		},
		SharedDirs: []string{"../outside"},
	})
	if err == nil {
		t.Fatal("expected traversal in shared_dirs to be rejected")
	}
}

func TestPreviewRejectsUnsafeServiceName(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	service, _ := newTestService(t, root, func(_ context.Context, _ domain.HealthCheck) (domain.HealthStatus, error) {
		return domain.HealthHealthy, nil
	})

	_, err := service.Preview(domain.SiteSpec{
		Name:    "unsafe-service",
		Domain:  "unsafe-service.test",
		Runtime: domain.RuntimeNode,
		Source: domain.DeploySource{
			Kind:        domain.SourceExistingDir,
			ExistingDir: filepath.Join(root, "source"),
		},
		Service: domain.ServiceSpec{
			Name: "../evil",
		},
	})
	if err == nil {
		t.Fatal("expected unsafe service name to be rejected")
	}
}

func newTestService(t *testing.T, root string, health func(context.Context, domain.HealthCheck) (domain.HealthStatus, error)) (*Service, *system.FakeRunner) {
	t.Helper()

	runner := &system.FakeRunner{}
	runner.Handler = func(cmd system.Command) (system.Result, error) {
		switch {
		case cmd.Name == "systemctl":
			return system.Result{}, nil
		case cmd.Name == "nginx":
			return system.Result{}, nil
		case cmd.Name == "docker":
			return system.Result{}, nil
		case cmd.Name == "npm":
			return system.Result{}, nil
		case cmd.Name == "python3":
			if len(cmd.Args) >= 3 && cmd.Args[0] == "-m" && cmd.Args[1] == "venv" {
				if err := os.MkdirAll(filepath.Join(cmd.Dir, ".venv", "bin"), 0o755); err != nil {
					return system.Result{}, err
				}
			}
			return system.Result{}, nil
		case cmd.Name == "bash":
			return system.Result{}, nil
		default:
			return system.Result{}, nil
		}
	}

	store := state.NewStore(state.Paths{
		ConfigDir: filepath.Join(root, "etc"),
		DataDir:   filepath.Join(root, "var"),
		LogDir:    filepath.Join(root, "log"),
	})
	cfg := domain.DefaultAppConfig()
	cfg.NginxSitesAvailable = filepath.Join(root, "nginx", "sites-available")
	cfg.NginxSitesEnabled = filepath.Join(root, "nginx", "sites-enabled")
	cfg.SystemdDir = filepath.Join(root, "systemd")

	service := NewService(store, runner, cfg)
	service.healthChecker = health
	return service, runner
}

func gitAwareHandler(t *testing.T) func(system.Command) (system.Result, error) {
	t.Helper()
	return func(cmd system.Command) (system.Result, error) {
		switch cmd.Name {
		case "git":
			if len(cmd.Args) > 0 && cmd.Args[0] == "clone" {
				repo := cmd.Args[len(cmd.Args)-2]
				dest := cmd.Args[len(cmd.Args)-1]
				return system.Result{}, copyDir(repo, dest)
			}
			if len(cmd.Args) > 0 && cmd.Args[0] == "rev-parse" {
				return system.Result{Stdout: "deadbeef\n"}, nil
			}
			return system.Result{}, nil
		case "npm", "systemctl", "nginx", "docker", "bash", "python3":
			return system.Result{}, nil
		default:
			return system.Result{}, nil
		}
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
}

func assertCallContains(t *testing.T, calls []system.Command, expected string) {
	t.Helper()
	for _, call := range calls {
		line := call.Name
		if len(call.Args) > 0 {
			line += " " + strings.Join(call.Args, " ")
		}
		if strings.Contains(line, expected) {
			return
		}
	}
	t.Fatalf("expected call containing %q, got %+v", expected, calls)
}
