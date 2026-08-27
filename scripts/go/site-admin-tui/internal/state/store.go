package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"sshpilot/scripts/go/site-admin-tui/internal/domain"

	"gopkg.in/yaml.v3"
)

var ErrSiteNotFound = errors.New("site not found")

type Paths struct {
	ConfigDir  string
	DataDir    string
	LogDir     string
	ConfigFile string
	SitesDir   string
	AuditLog   string
}

func DefaultPaths() Paths {
	configDir := "/etc/site-admin-tui"
	dataDir := "/var/lib/site-admin-tui"
	logDir := "/var/log/site-admin-tui"
	return Paths{
		ConfigDir:  configDir,
		DataDir:    dataDir,
		LogDir:     logDir,
		ConfigFile: filepath.Join(configDir, "config.yaml"),
		SitesDir:   filepath.Join(configDir, "sites"),
		AuditLog:   filepath.Join(logDir, "audit.log"),
	}
}

func (p Paths) normalize() Paths {
	def := DefaultPaths()
	if p.ConfigDir == "" {
		p.ConfigDir = def.ConfigDir
	}
	if p.DataDir == "" {
		p.DataDir = def.DataDir
	}
	if p.LogDir == "" {
		p.LogDir = def.LogDir
	}
	if p.ConfigFile == "" {
		p.ConfigFile = filepath.Join(p.ConfigDir, "config.yaml")
	}
	if p.SitesDir == "" {
		p.SitesDir = filepath.Join(p.ConfigDir, "sites")
	}
	if p.AuditLog == "" {
		p.AuditLog = filepath.Join(p.LogDir, "audit.log")
	}
	return p
}

type Store struct {
	paths Paths
}

func NewStore(paths Paths) *Store {
	return &Store{paths: paths.normalize()}
}

func (s *Store) Paths() Paths {
	return s.paths
}

func (s *Store) EnsureLayout() error {
	for _, dir := range []string{
		s.paths.ConfigDir,
		s.paths.SitesDir,
		s.paths.DataDir,
		s.paths.LogDir,
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) LoadConfig() (domain.AppConfig, error) {
	if err := s.EnsureLayout(); err != nil {
		return domain.AppConfig{}, err
	}

	cfg := domain.DefaultAppConfig()
	data, err := os.ReadFile(s.paths.ConfigFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return domain.AppConfig{}, err
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return domain.AppConfig{}, fmt.Errorf("parse config: %w", err)
	}
	return cfg, nil
}

func (s *Store) SaveConfig(cfg domain.AppConfig) error {
	if err := s.EnsureLayout(); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.paths.ConfigFile, data, 0o644)
}

func (s *Store) SaveSite(spec domain.SiteSpec) error {
	if err := s.EnsureLayout(); err != nil {
		return err
	}
	now := time.Now().UTC()
	if spec.CreatedAt.IsZero() {
		spec.CreatedAt = now
	}
	spec.UpdatedAt = now
	data, err := yaml.Marshal(spec)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.SiteSpecPath(spec.Name), data, 0o644)
}

func (s *Store) LoadSite(name string) (domain.SiteSpec, error) {
	data, err := os.ReadFile(s.SiteSpecPath(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return domain.SiteSpec{}, ErrSiteNotFound
		}
		return domain.SiteSpec{}, err
	}
	var spec domain.SiteSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return domain.SiteSpec{}, err
	}
	return spec, nil
}

func (s *Store) ListSites() ([]domain.SiteSpec, error) {
	if err := s.EnsureLayout(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(s.paths.SitesDir)
	if err != nil {
		return nil, err
	}
	sites := make([]domain.SiteSpec, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		spec, err := s.LoadSite(strings.TrimSuffix(entry.Name(), ".yaml"))
		if err != nil {
			continue
		}
		sites = append(sites, spec)
	}
	sort.Slice(sites, func(i, j int) bool {
		return sites[i].Name < sites[j].Name
	})
	return sites, nil
}

func (s *Store) SiteSpecPath(name string) string {
	return filepath.Join(s.paths.SitesDir, name+".yaml")
}

func (s *Store) SiteRoot(name string) string {
	return filepath.Join(s.paths.DataDir, "sites", name)
}

func (s *Store) ReleasesDir(name string) string {
	return filepath.Join(s.SiteRoot(name), "releases")
}

func (s *Store) SharedDir(name string) string {
	return filepath.Join(s.SiteRoot(name), "shared")
}

func (s *Store) CurrentLink(name string) string {
	return filepath.Join(s.SiteRoot(name), "current")
}

func (s *Store) HistoryPath(name string) string {
	return filepath.Join(s.SiteRoot(name), "history.json")
}

func (s *Store) LockPath(name string) string {
	return filepath.Join(s.SiteRoot(name), "lock")
}

func (s *Store) BackupDir(name string) string {
	return filepath.Join(s.SiteRoot(name), "backups")
}

func (s *Store) EnsureSiteLayout(name string) error {
	for _, dir := range []string{
		s.SiteRoot(name),
		s.ReleasesDir(name),
		s.SharedDir(name),
		s.BackupDir(name),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) AcquireSiteLock(name string) (func() error, error) {
	if err := s.EnsureSiteLayout(name); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(s.LockPath(name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("site %s is already locked", name)
		}
		return nil, err
	}
	_, _ = fmt.Fprintf(file, "pid=%d\ncreated_at=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
	if err := file.Close(); err != nil {
		return nil, err
	}
	return func() error {
		err := os.Remove(s.LockPath(name))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}, nil
}

func (s *Store) CreateReleaseDir(name, releaseID string) (string, error) {
	if err := s.EnsureSiteLayout(name); err != nil {
		return "", err
	}
	path := filepath.Join(s.ReleasesDir(name), releaseID)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", err
	}
	return path, nil
}

func (s *Store) CurrentReleasePath(name string) (string, error) {
	target, err := os.Readlink(s.CurrentLink(name))
	if err != nil {
		if data, readErr := os.ReadFile(s.CurrentLink(name)); readErr == nil {
			return strings.TrimSpace(string(data)), nil
		}
		return "", err
	}
	if filepath.IsAbs(target) {
		return target, nil
	}
	return filepath.Clean(filepath.Join(filepath.Dir(s.CurrentLink(name)), target)), nil
}

func (s *Store) SetCurrentRelease(name, releasePath string) error {
	if err := s.EnsureSiteLayout(name); err != nil {
		return err
	}
	releasePath = strings.TrimSpace(releasePath)
	if releasePath == "" {
		return fmt.Errorf("release path is required")
	}
	link := s.CurrentLink(name)
	temp := fmt.Sprintf("%s.tmp.%d", link, time.Now().UTC().UnixNano())
	_ = os.Remove(temp)
	if err := os.Symlink(releasePath, temp); err != nil {
		if writeErr := writeFileAtomic(temp, []byte(releasePath+"\n"), 0o644); writeErr != nil {
			return fmt.Errorf("create current release link: symlink: %v; fallback file: %w", err, writeErr)
		}
	}
	if err := replacePath(temp, link); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

func (s *Store) LoadHistory(name string) ([]domain.ReleaseRecord, error) {
	data, err := os.ReadFile(s.HistoryPath(name))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var history []domain.ReleaseRecord
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, err
	}
	return history, nil
}

func (s *Store) SaveHistory(name string, history []domain.ReleaseRecord) error {
	if err := s.EnsureSiteLayout(name); err != nil {
		return err
	}
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(s.HistoryPath(name), data, 0o644)
}

func (s *Store) AppendHistory(name string, record domain.ReleaseRecord) error {
	history, err := s.LoadHistory(name)
	if err != nil {
		return err
	}
	history = append(history, record)
	return s.SaveHistory(name, history)
}

func (s *Store) LastSuccessfulRelease(name string) (*domain.ReleaseRecord, error) {
	history, err := s.LoadHistory(name)
	if err != nil {
		return nil, err
	}
	for i := len(history) - 1; i >= 0; i-- {
		record := history[i]
		if record.Status == domain.ReleaseActive {
			return &record, nil
		}
	}
	return nil, nil
}

func (s *Store) Auditf(format string, args ...any) error {
	if err := s.EnsureLayout(); err != nil {
		return err
	}
	file, err := os.OpenFile(s.paths.AuditLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "%s %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
	return err
}

func (s *Store) ReadAuditLines(limit int) ([]string, error) {
	data, err := os.ReadFile(s.paths.AuditLog)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	return lines, nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tempName)
		}
	}()

	if _, err = temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err = temp.Chmod(perm); err != nil {
		_ = temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	return replacePath(tempName, path)
}

func replacePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else {
		renameErr := err
		if _, statErr := os.Lstat(dst); statErr != nil {
			return renameErr
		}
		backup := src + ".old"
		if backupErr := os.Rename(dst, backup); backupErr != nil {
			return fmt.Errorf("replace %s: rename: %v; backup: %w", dst, renameErr, backupErr)
		}
		if err := os.Rename(src, dst); err != nil {
			_ = os.Rename(backup, dst)
			return fmt.Errorf("replace %s after backup: %w", dst, err)
		}
		_ = os.Remove(backup)
	}
	return nil
}
