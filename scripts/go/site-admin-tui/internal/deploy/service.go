package deploy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sshpilot/scripts/go/site-admin-tui/internal/domain"
	"sshpilot/scripts/go/site-admin-tui/internal/nginx"
	"sshpilot/scripts/go/site-admin-tui/internal/runtime"
	"sshpilot/scripts/go/site-admin-tui/internal/state"
	"sshpilot/scripts/go/site-admin-tui/internal/system"
)

type HealthChecker func(context.Context, domain.HealthCheck) (domain.HealthStatus, error)

type Service struct {
	store         *state.Store
	runner        system.Runner
	config        domain.AppConfig
	adapters      map[domain.RuntimeType]runtime.Adapter
	healthChecker HealthChecker
	now           func() time.Time
}

type DeployResult struct {
	Spec        domain.SiteSpec
	Release     domain.ReleaseRecord
	Plan        []string
	Messages    []string
	CurrentPath string
}

type SiteDetails struct {
	Spec        domain.SiteSpec
	CurrentPath string
	History     []domain.ReleaseRecord
}

func NewService(store *state.Store, runner system.Runner, cfg domain.AppConfig) *Service {
	if store == nil {
		store = state.NewStore(state.DefaultPaths())
	}
	if runner == nil {
		runner = system.ExecRunner{}
	}
	if cfg.NginxSitesAvailable == "" {
		cfg = domain.DefaultAppConfig()
	}

	svc := &Service{
		store:    store,
		runner:   runner,
		config:   cfg,
		adapters: runtime.NewRegistry(),
		now:      func() time.Time { return time.Now().UTC() },
	}
	svc.healthChecker = func(ctx context.Context, health domain.HealthCheck) (domain.HealthStatus, error) {
		return runtime.NewRegistry()[domain.RuntimeStatic].HealthCheck(ctx, domain.SiteSpec{Domain: health.Host, Health: health}, runtime.Activation{Health: health})
	}
	return svc
}

func (s *Service) Store() *state.Store {
	return s.store
}

func (s *Service) Preview(spec domain.SiteSpec) ([]string, error) {
	spec, err := s.normalizeSpec(spec)
	if err != nil {
		return nil, err
	}
	adapter, err := s.adapter(spec.Runtime)
	if err != nil {
		return nil, err
	}
	plan := []string{
		fmt.Sprintf("Site: %s (%s)", spec.Name, spec.Runtime),
		fmt.Sprintf("Domain: %s", spec.Domain),
		fmt.Sprintf("Source: %s", spec.Source.Kind),
		"Layout: atomic releases with current symlink and shared dirs",
	}
	plan = append(plan, adapter.Plan(spec)...)
	if spec.TLS.Enabled {
		plan = append(plan, "Issue/renew TLS certificate with certbot webroot after successful activation")
	}
	return plan, nil
}

func (s *Service) Deploy(ctx context.Context, spec domain.SiteSpec) (_ DeployResult, retErr error) {
	spec, err := s.normalizeSpec(spec)
	if err != nil {
		return DeployResult{}, err
	}
	plan, err := s.Preview(spec)
	if err != nil {
		return DeployResult{}, err
	}
	adapter, err := s.adapter(spec.Runtime)
	if err != nil {
		return DeployResult{}, err
	}
	unlock, err := s.store.AcquireSiteLock(spec.Name)
	if err != nil {
		return DeployResult{}, err
	}
	defer func() {
		if unlockErr := unlock(); retErr == nil && unlockErr != nil {
			retErr = unlockErr
		}
	}()

	prevCurrent, _ := s.store.CurrentReleasePath(spec.Name)
	releaseID := s.now().Format("20060102-150405")
	releasePath, err := s.store.CreateReleaseDir(spec.Name, releaseID)
	if err != nil {
		return DeployResult{}, err
	}
	record := domain.ReleaseRecord{
		ID:        releaseID,
		Site:      spec.Name,
		Path:      releasePath,
		Status:    domain.ReleasePending,
		Health:    domain.HealthUnknown,
		CreatedAt: s.now(),
	}
	_ = s.store.AppendHistory(spec.Name, record)
	_ = s.store.Auditf("deploy start site=%s release=%s runtime=%s", spec.Name, releaseID, spec.Runtime)

	revision, err := s.populateRelease(ctx, spec, releasePath)
	if err != nil {
		record.Status = domain.ReleaseFailed
		record.Message = err.Error()
		record.SourceRevision = revision
		_ = s.updateHistory(spec.Name, record)
		return DeployResult{}, err
	}
	record.SourceRevision = revision

	if err := s.prepareShared(spec, releasePath); err != nil {
		record.Status = domain.ReleaseFailed
		record.Message = err.Error()
		_ = s.updateHistory(spec.Name, record)
		return DeployResult{}, err
	}

	activation, err := adapter.PrepareRelease(ctx, runtime.Dependencies{Runner: s.runner, Config: s.config}, spec, releasePath)
	if err != nil {
		record.Status = domain.ReleaseFailed
		record.Message = err.Error()
		_ = s.updateHistory(spec.Name, record)
		return DeployResult{}, err
	}

	record.Status = domain.ReleasePrepared
	_ = s.updateHistory(spec.Name, record)

	if err := s.store.SetCurrentRelease(spec.Name, releasePath); err != nil {
		record.Status = domain.ReleaseFailed
		record.Message = err.Error()
		_ = s.updateHistory(spec.Name, record)
		return DeployResult{}, err
	}
	currentLink := s.store.CurrentLink(spec.Name)

	rollback := func(cause error) error {
		if prevCurrent == "" {
			return cause
		}
		_ = s.store.SetCurrentRelease(spec.Name, prevCurrent)
		activation.Nginx.Root = runtimeRoot(spec, currentLink)
		_ = adapter.Rollback(ctx, runtime.Dependencies{Runner: s.runner, Config: s.config}, spec, currentLink, s.store.BackupDir(spec.Name), activation)
		record.Status = domain.ReleaseRolledBack
		record.Health = domain.HealthFailed
		record.RollbackFrom = prevCurrent
		record.Message = cause.Error()
		_ = s.updateHistory(spec.Name, record)
		_ = s.store.Auditf("deploy rollback site=%s release=%s err=%v", spec.Name, releaseID, cause)
		return cause
	}

	activation.Nginx.Root = runtimeRoot(spec, currentLink)
	if activation.Nginx.Name != "" {
		if err := s.installNginx(ctx, spec, activation.Nginx); err != nil {
			return DeployResult{}, rollback(err)
		}
	}

	if err := adapter.Activate(ctx, runtime.Dependencies{Runner: s.runner, Config: s.config}, spec, currentLink, s.store.BackupDir(spec.Name), activation); err != nil {
		return DeployResult{}, rollback(err)
	}

	if health, err := s.runHealthCheck(ctx, spec, activation); err != nil {
		return DeployResult{}, rollback(err)
	} else {
		record.Health = health
		if health != domain.HealthHealthy {
			return DeployResult{}, rollback(fmt.Errorf("healthcheck returned %s", health))
		}
	}

	if spec.TLS.Enabled {
		if err := s.issueTLS(ctx, spec, activation); err != nil {
			return DeployResult{}, rollback(err)
		}
		activation.Nginx.EnableTLS = true
		activation.Nginx.TLSCertPath = filepath.Join("/etc/letsencrypt/live", spec.Domain, "fullchain.pem")
		activation.Nginx.TLSKeyPath = filepath.Join("/etc/letsencrypt/live", spec.Domain, "privkey.pem")
		if err := s.installNginx(ctx, spec, activation.Nginx); err != nil {
			return DeployResult{}, rollback(err)
		}
	}

	if err := s.store.SaveSite(spec); err != nil {
		return DeployResult{}, rollback(err)
	}

	record.Status = domain.ReleaseActive
	record.Message = "deploy completed"
	_ = s.updateHistory(spec.Name, record)
	_ = s.store.Auditf("deploy success site=%s release=%s", spec.Name, releaseID)

	currentPath, _ := s.store.CurrentReleasePath(spec.Name)
	return DeployResult{
		Spec:        spec,
		Release:     record,
		Plan:        plan,
		Messages:    []string{"Release prepared", "Current symlink switched", "Health-check passed"},
		CurrentPath: currentPath,
	}, nil
}

func (s *Service) Import(ctx context.Context, spec domain.SiteSpec) (DeployResult, error) {
	spec.Source.Kind = domain.SourceExistingDir
	return s.Deploy(ctx, spec)
}

func (s *Service) Redeploy(ctx context.Context, site string) (DeployResult, error) {
	spec, err := s.store.LoadSite(site)
	if err != nil {
		return DeployResult{}, err
	}
	return s.Deploy(ctx, spec)
}

func (s *Service) Restart(ctx context.Context, site string) error {
	spec, err := s.store.LoadSite(site)
	if err != nil {
		return err
	}
	adapter, err := s.adapter(spec.Runtime)
	if err != nil {
		return err
	}
	current, err := s.store.CurrentReleasePath(site)
	if err != nil {
		return err
	}
	return adapter.Restart(ctx, runtime.Dependencies{Runner: s.runner, Config: s.config}, spec, current, runtime.Activation{})
}

func (s *Service) Rollback(ctx context.Context, site string) (DeployResult, error) {
	spec, err := s.store.LoadSite(site)
	if err != nil {
		return DeployResult{}, err
	}
	history, err := s.store.LoadHistory(site)
	if err != nil {
		return DeployResult{}, err
	}
	var current *domain.ReleaseRecord
	var previous *domain.ReleaseRecord
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Status == domain.ReleaseActive {
			if current == nil {
				current = &history[i]
				continue
			}
			previous = &history[i]
			break
		}
	}
	if previous == nil {
		return DeployResult{}, fmt.Errorf("no previous successful release for %s", site)
	}
	if err := s.store.SetCurrentRelease(site, previous.Path); err != nil {
		return DeployResult{}, err
	}
	adapter, err := s.adapter(spec.Runtime)
	if err != nil {
		return DeployResult{}, err
	}
	currentLink := s.store.CurrentLink(site)
	activation, err := adapter.PrepareRelease(ctx, runtime.Dependencies{Runner: s.runner, Config: s.config}, spec, previous.Path)
	if err != nil {
		return DeployResult{}, err
	}
	activation.Nginx.Root = runtimeRoot(spec, currentLink)
	if activation.Nginx.Name != "" {
		if err := s.installNginx(ctx, spec, activation.Nginx); err != nil {
			return DeployResult{}, err
		}
	}
	if err := adapter.Rollback(ctx, runtime.Dependencies{Runner: s.runner, Config: s.config}, spec, currentLink, s.store.BackupDir(site), activation); err != nil {
		return DeployResult{}, err
	}
	record := domain.ReleaseRecord{
		ID:           s.now().Format("20060102-150405"),
		Site:         site,
		Path:         previous.Path,
		Status:       domain.ReleaseActive,
		Health:       domain.HealthHealthy,
		CreatedAt:    s.now(),
		RollbackFrom: current.Path,
		Message:      "manual rollback",
	}
	_ = s.store.AppendHistory(site, record)
	_ = s.store.Auditf("manual rollback site=%s to=%s", site, previous.Path)
	return DeployResult{Spec: spec, Release: record, CurrentPath: previous.Path}, nil
}

func (s *Service) SiteDetails(name string) (SiteDetails, error) {
	spec, err := s.store.LoadSite(name)
	if err != nil {
		return SiteDetails{}, err
	}
	currentPath, _ := s.store.CurrentReleasePath(name)
	history, err := s.store.LoadHistory(name)
	if err != nil {
		return SiteDetails{}, err
	}
	sort.Slice(history, func(i, j int) bool {
		return history[i].CreatedAt.After(history[j].CreatedAt)
	})
	return SiteDetails{Spec: spec, CurrentPath: currentPath, History: history}, nil
}

func (s *Service) ListSites() ([]domain.SiteSpec, error) {
	return s.store.ListSites()
}

func (s *Service) AuditLines(limit int) ([]string, error) {
	return s.store.ReadAuditLines(limit)
}

func (s *Service) Doctor() (domain.DoctorReport, error) {
	return system.RunDoctor(s.runner, s.store)
}

func (s *Service) normalizeSpec(spec domain.SiteSpec) (domain.SiteSpec, error) {
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Domain = strings.TrimSpace(spec.Domain)
	spec.Service.Name = strings.TrimSpace(spec.Service.Name)
	spec.Service.ComposeFile = strings.TrimSpace(spec.Service.ComposeFile)
	spec.Source.DeploySubdir = strings.TrimSpace(spec.Source.DeploySubdir)
	spec.EnvFile = strings.TrimSpace(spec.EnvFile)
	if err := domain.ValidateSiteName(spec.Name); err != nil {
		return spec, err
	}
	if err := domain.ValidateDomain(spec.Domain); err != nil {
		return spec, err
	}
	if !spec.Runtime.Valid() {
		return spec, fmt.Errorf("unsupported runtime %q", spec.Runtime)
	}
	if !spec.Source.Kind.Valid() {
		return spec, fmt.Errorf("unsupported source %q", spec.Source.Kind)
	}
	if spec.Source.Kind == domain.SourceGit && spec.Source.Repo == "" {
		return spec, fmt.Errorf("git repo is required")
	}
	if spec.Source.Kind == domain.SourceExistingDir && spec.Source.ExistingDir == "" {
		return spec, fmt.Errorf("existing_dir path is required")
	}
	if spec.Source.Kind == domain.SourceGit && spec.Source.Branch == "" && spec.Source.Ref == "" {
		spec.Source.Branch = "main"
	}
	if spec.RootDir == "" {
		spec.RootDir = "."
	}
	if err := domain.ValidateRelativePath("root_dir", spec.RootDir, true); err != nil {
		return spec, err
	}
	spec.RootDir = domain.CleanRelativePath(spec.RootDir)
	if spec.Source.DeploySubdir != "" {
		if err := domain.ValidateRelativePath("source.deploy_subdir", spec.Source.DeploySubdir, false); err != nil {
			return spec, err
		}
		spec.Source.DeploySubdir = domain.CleanRelativePath(spec.Source.DeploySubdir)
	}
	if len(spec.SharedDirs) > 0 {
		cleaned := make([]string, 0, len(spec.SharedDirs))
		for _, dir := range spec.SharedDirs {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				continue
			}
			if err := domain.ValidateRelativePath("shared_dirs", dir, false); err != nil {
				return spec, err
			}
			cleaned = append(cleaned, domain.CleanRelativePath(dir))
		}
		spec.SharedDirs = cleaned
	}
	if spec.EnvFile != "" {
		if err := domain.ValidateRelativePath("env_file", spec.EnvFile, false); err != nil {
			return spec, err
		}
		spec.EnvFile = domain.CleanRelativePath(spec.EnvFile)
	}
	if err := domain.ValidateServiceUnitName(spec.Service.Name); err != nil {
		return spec, err
	}
	if spec.Service.ComposeFile != "" {
		if err := domain.ValidateRelativePath("service.compose_file", spec.Service.ComposeFile, false); err != nil {
			return spec, err
		}
		spec.Service.ComposeFile = domain.CleanRelativePath(spec.Service.ComposeFile)
	}
	if spec.Service.Port < 0 || spec.Service.Port > 65535 {
		return spec, fmt.Errorf("service.port must be between 0 and 65535")
	}
	for _, command := range spec.Service.Command {
		if err := domain.ValidateSafeLine("service.command", command); err != nil {
			return spec, err
		}
	}
	if spec.TLS.Enabled && spec.TLS.Webroot == "" {
		spec.TLS.Webroot = filepath.Join(s.store.SharedDir(spec.Name), "acme")
	}
	if err := domain.ValidateSafeLine("tls.webroot", spec.TLS.Webroot); err != nil {
		return spec, err
	}
	if err := domain.ValidateSafeLine("tls.email", spec.TLS.Email); err != nil {
		return spec, err
	}
	switch spec.Runtime {
	case domain.RuntimeNode:
		if spec.Service.Port == 0 {
			spec.Service.Port = 3000
		}
		if len(spec.Service.Command) == 0 {
			spec.Service.Command = []string{"npm start"}
		}
	case domain.RuntimePython:
		if spec.Service.Port == 0 {
			spec.Service.Port = 8000
		}
	case domain.RuntimeDockerCompose:
		if spec.Service.Port == 0 {
			spec.Service.Port = 8080
		}
	case domain.RuntimePHP:
		if spec.Service.PHPFPMService == "" {
			spec.Service.PHPFPMService = s.config.DefaultPHPFPM
		}
	}
	spec.Health = spec.Health.WithDefaults()
	return spec, nil
}

func (s *Service) adapter(runtimeType domain.RuntimeType) (runtime.Adapter, error) {
	adapter, ok := s.adapters[runtimeType]
	if !ok {
		return nil, fmt.Errorf("adapter for %s is not registered", runtimeType)
	}
	return adapter, nil
}

func (s *Service) populateRelease(ctx context.Context, spec domain.SiteSpec, releasePath string) (string, error) {
	switch spec.Source.Kind {
	case domain.SourceGit:
		return s.cloneGitSource(ctx, spec, releasePath)
	case domain.SourceExistingDir:
		return s.copyExistingSource(spec, releasePath)
	default:
		return "", fmt.Errorf("unsupported source %q", spec.Source.Kind)
	}
}

func (s *Service) cloneGitSource(ctx context.Context, spec domain.SiteSpec, releasePath string) (string, error) {
	tempDir, err := os.MkdirTemp("", "site-admin-git-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tempDir)

	args := []string{"clone", "--depth", "1"}
	if spec.Source.Branch != "" {
		args = append(args, "--branch", spec.Source.Branch)
	}
	args = append(args, spec.Source.Repo, tempDir)
	if _, err := s.runner.Run(ctx, system.Command{Name: "git", Args: args}); err != nil {
		return "", err
	}
	if spec.Source.Ref != "" {
		if _, err := s.runner.Run(ctx, system.Command{Name: "git", Args: []string{"checkout", spec.Source.Ref}, Dir: tempDir}); err != nil {
			return "", err
		}
	}
	result, err := s.runner.Run(ctx, system.Command{Name: "git", Args: []string{"rev-parse", "HEAD"}, Dir: tempDir})
	if err != nil {
		return "", err
	}
	sourceRoot := tempDir
	if spec.Source.DeploySubdir != "" {
		sourceRoot, err = safeJoin(tempDir, spec.Source.DeploySubdir, "source.deploy_subdir")
		if err != nil {
			return "", err
		}
	}
	if err := copyDir(sourceRoot, releasePath); err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Stdout), nil
}

func (s *Service) copyExistingSource(spec domain.SiteSpec, releasePath string) (string, error) {
	if err := copyDir(spec.Source.ExistingDir, releasePath); err != nil {
		return "", err
	}
	return "existing-dir:" + filepath.Clean(spec.Source.ExistingDir), nil
}

func (s *Service) prepareShared(spec domain.SiteSpec, releasePath string) error {
	for _, dir := range spec.SharedDirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		target, err := safeJoin(s.store.SharedDir(spec.Name), dir, "shared_dirs")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			return err
		}
		link, err := safeJoin(releasePath, dir, "shared_dirs")
		if err != nil {
			return err
		}
		if err := os.RemoveAll(link); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return err
		}
		if err := symlinkOrFallback(target, link, true); err != nil {
			return err
		}
	}

	if spec.EnvFile != "" {
		envTarget := filepath.Join(s.store.SharedDir(spec.Name), filepath.Base(spec.EnvFile))
		if _, err := os.Stat(envTarget); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(envTarget, []byte{}, 0o644); err != nil {
				return err
			}
		}
		link, err := safeJoin(releasePath, spec.EnvFile, "env_file")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			return err
		}
		_ = os.Remove(link)
		if err := symlinkOrFallback(envTarget, link, false); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) installNginx(ctx context.Context, spec domain.SiteSpec, cfg nginx.SiteConfig) error {
	content, err := nginx.RenderSite(cfg)
	if err != nil {
		return err
	}
	return nginx.InstallSite(ctx, s.runner, s.config.NginxSitesAvailable, s.config.NginxSitesEnabled, cfg, content, filepath.Join(s.store.BackupDir(spec.Name), "nginx"))
}

func (s *Service) runHealthCheck(ctx context.Context, spec domain.SiteSpec, activation runtime.Activation) (domain.HealthStatus, error) {
	health := spec.Health.WithDefaults()
	if activation.Health.URL != "" {
		health = activation.Health.WithDefaults()
	}
	return s.healthChecker(ctx, health)
}

func (s *Service) issueTLS(ctx context.Context, spec domain.SiteSpec, activation runtime.Activation) error {
	if _, err := net.LookupHost(spec.Domain); err != nil {
		return fmt.Errorf("dns preflight failed for %s: %w", spec.Domain, err)
	}
	if err := os.MkdirAll(spec.TLS.Webroot, 0o755); err != nil {
		return err
	}
	args := []string{
		"certonly",
		"--webroot",
		"-w", spec.TLS.Webroot,
		"-d", spec.Domain,
		"--non-interactive",
		"--agree-tos",
		"--keep-until-expiring",
	}
	if spec.TLS.Email != "" {
		args = append(args, "--email", spec.TLS.Email)
	} else {
		args = append(args, "--register-unsafely-without-email")
	}
	_, err := s.runner.Run(ctx, system.Command{Name: "certbot", Args: args})
	return err
}

func (s *Service) updateHistory(site string, record domain.ReleaseRecord) error {
	history, err := s.store.LoadHistory(site)
	if err != nil {
		return err
	}
	for i := range history {
		if history[i].ID == record.ID {
			history[i] = record
			return s.store.SaveHistory(site, history)
		}
	}
	history = append(history, record)
	return s.store.SaveHistory(site, history)
}

func copyDir(src, dst string) error {
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

func symlinkOrFallback(target, link string, dir bool) error {
	if err := os.Symlink(target, link); err == nil {
		return nil
	}
	if dir {
		return os.MkdirAll(link, 0o755)
	}
	return os.WriteFile(link, []byte(target+"\n"), 0o644)
}

func safeJoin(base, rel, field string) (string, error) {
	if err := domain.ValidateRelativePath(field, rel, false); err != nil {
		return "", err
	}
	base = filepath.Clean(base)
	target := filepath.Join(base, domain.CleanRelativePath(rel))
	if !pathWithin(base, target) {
		return "", fmt.Errorf("%s escapes base directory: %q", field, rel)
	}
	return target, nil
}

func pathWithin(base, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func runtimeRoot(spec domain.SiteSpec, currentLink string) string {
	if spec.RootDir == "" || spec.RootDir == "." {
		return currentLink
	}
	return filepath.Join(currentLink, spec.RootDir)
}
