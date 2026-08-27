package domain

import "time"

type RuntimeType string

const (
	RuntimeStatic        RuntimeType = "static"
	RuntimePHP           RuntimeType = "php"
	RuntimeNode          RuntimeType = "node"
	RuntimePython        RuntimeType = "python"
	RuntimeDockerCompose RuntimeType = "docker_compose"
)

func (r RuntimeType) Valid() bool {
	switch r {
	case RuntimeStatic, RuntimePHP, RuntimeNode, RuntimePython, RuntimeDockerCompose:
		return true
	default:
		return false
	}
}

func SupportedRuntimes() []RuntimeType {
	return []RuntimeType{
		RuntimeStatic,
		RuntimePHP,
		RuntimeNode,
		RuntimePython,
		RuntimeDockerCompose,
	}
}

type SourceKind string

const (
	SourceGit         SourceKind = "git"
	SourceExistingDir SourceKind = "existing_dir"
)

func (s SourceKind) Valid() bool {
	switch s {
	case SourceGit, SourceExistingDir:
		return true
	default:
		return false
	}
}

type DeploySource struct {
	Kind         SourceKind `yaml:"kind" json:"kind"`
	Repo         string     `yaml:"repo,omitempty" json:"repo,omitempty"`
	Branch       string     `yaml:"branch,omitempty" json:"branch,omitempty"`
	Ref          string     `yaml:"ref,omitempty" json:"ref,omitempty"`
	DeploySubdir string     `yaml:"deploy_subdir,omitempty" json:"deploy_subdir,omitempty"`
	ExistingDir  string     `yaml:"existing_dir,omitempty" json:"existing_dir,omitempty"`
}

type TLSSpec struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Email   string `yaml:"email,omitempty" json:"email,omitempty"`
	Webroot string `yaml:"webroot,omitempty" json:"webroot,omitempty"`
}

type ServiceSpec struct {
	Name          string            `yaml:"name,omitempty" json:"name,omitempty"`
	Command       []string          `yaml:"command,omitempty" json:"command,omitempty"`
	Port          int               `yaml:"port,omitempty" json:"port,omitempty"`
	ComposeFile   string            `yaml:"compose_file,omitempty" json:"compose_file,omitempty"`
	PHPFPMService string            `yaml:"php_fpm_service,omitempty" json:"php_fpm_service,omitempty"`
	Environment   map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
}

type HealthCheck struct {
	URL            string `yaml:"url,omitempty" json:"url,omitempty"`
	Host           string `yaml:"host,omitempty" json:"host,omitempty"`
	Path           string `yaml:"path,omitempty" json:"path,omitempty"`
	ExpectedStatus int    `yaml:"expected_status,omitempty" json:"expected_status,omitempty"`
	Attempts       int    `yaml:"attempts,omitempty" json:"attempts,omitempty"`
	IntervalSec    int    `yaml:"interval_sec,omitempty" json:"interval_sec,omitempty"`
	TimeoutSec     int    `yaml:"timeout_sec,omitempty" json:"timeout_sec,omitempty"`
}

func (h HealthCheck) WithDefaults() HealthCheck {
	if h.ExpectedStatus == 0 {
		h.ExpectedStatus = 200
	}
	if h.Attempts == 0 {
		h.Attempts = 5
	}
	if h.IntervalSec == 0 {
		h.IntervalSec = 2
	}
	if h.TimeoutSec == 0 {
		h.TimeoutSec = 5
	}
	if h.Path == "" {
		h.Path = "/"
	}
	return h
}

type SiteSpec struct {
	Name       string       `yaml:"name" json:"name"`
	Domain     string       `yaml:"domain" json:"domain"`
	Runtime    RuntimeType  `yaml:"runtime" json:"runtime"`
	Source     DeploySource `yaml:"source" json:"source"`
	RootDir    string       `yaml:"root_dir,omitempty" json:"root_dir,omitempty"`
	SharedDirs []string     `yaml:"shared_dirs,omitempty" json:"shared_dirs,omitempty"`
	EnvFile    string       `yaml:"env_file,omitempty" json:"env_file,omitempty"`
	TLS        TLSSpec      `yaml:"tls,omitempty" json:"tls,omitempty"`
	Service    ServiceSpec  `yaml:"service,omitempty" json:"service,omitempty"`
	Health     HealthCheck  `yaml:"healthcheck,omitempty" json:"healthcheck,omitempty"`
	CreatedAt  time.Time    `yaml:"created_at,omitempty" json:"created_at,omitempty"`
	UpdatedAt  time.Time    `yaml:"updated_at,omitempty" json:"updated_at,omitempty"`
}

type ReleaseStatus string

const (
	ReleasePending    ReleaseStatus = "pending"
	ReleasePrepared   ReleaseStatus = "prepared"
	ReleaseActive     ReleaseStatus = "active"
	ReleaseFailed     ReleaseStatus = "failed"
	ReleaseRolledBack ReleaseStatus = "rolled_back"
)

type HealthStatus string

const (
	HealthUnknown HealthStatus = "unknown"
	HealthHealthy HealthStatus = "healthy"
	HealthFailed  HealthStatus = "failed"
)

type ReleaseRecord struct {
	ID             string        `json:"id"`
	Site           string        `json:"site"`
	SourceRevision string        `json:"source_revision,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	Path           string        `json:"path"`
	Status         ReleaseStatus `json:"status"`
	Health         HealthStatus  `json:"health"`
	RollbackFrom   string        `json:"rollback_from,omitempty"`
	Message        string        `json:"message,omitempty"`
}

type AppConfig struct {
	NginxSitesAvailable string `yaml:"nginx_sites_available"`
	NginxSitesEnabled   string `yaml:"nginx_sites_enabled"`
	SystemdDir          string `yaml:"systemd_dir"`
	DefaultPHPFPM       string `yaml:"default_php_fpm"`
	DefaultWebrootRoot  string `yaml:"default_webroot_root"`
}

func DefaultAppConfig() AppConfig {
	return AppConfig{
		NginxSitesAvailable: "/etc/nginx/sites-available",
		NginxSitesEnabled:   "/etc/nginx/sites-enabled",
		SystemdDir:          "/etc/systemd/system",
		DefaultPHPFPM:       "php8.2-fpm",
		DefaultWebrootRoot:  "/var/www",
	}
}

type DoctorCheck struct {
	Name     string
	Required bool
	OK       bool
	Detail   string
}

type DoctorReport struct {
	Checks []DoctorCheck
}

func (r DoctorReport) Healthy() bool {
	for _, check := range r.Checks {
		if check.Required && !check.OK {
			return false
		}
	}
	return true
}
