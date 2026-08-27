package diagnostics

import "time"

// StageStatus defines the diagnostic probe outcome.
type StageStatus string

const (
	StagePass StageStatus = "pass"
	StageWarn StageStatus = "warn"
	StageFail StageStatus = "fail"
	StageSkip StageStatus = "skip"
)

// StageResult describes a single audit stage result.
type StageResult struct {
	Name        string      `json:"name"`
	Stage       string      `json:"stage"`
	Status      StageStatus `json:"status"`
	DurationMs  int64       `json:"duration_ms"`
	Summary     string      `json:"summary"`
	Details     string      `json:"details,omitempty"`
	Error       string      `json:"error,omitempty"`
}

// DiagnosticReport is the full comprehensive diagnostic report.
type DiagnosticReport struct {
	ServerName     string        `json:"server_name"`
	TargetHost     string        `json:"target_host"`
	TargetPort     string        `json:"target_port"`
	Timestamp      time.Time     `json:"timestamp"`
	OverallStatus  StageStatus   `json:"overall_status"`
	TotalDuration  int64         `json:"total_duration_ms"`
	Stages         []StageResult `json:"stages"`
	Banner         string        `json:"banner,omitempty"`
	KexAlgorithms  []string      `json:"kex_algorithms,omitempty"`
	Ciphers        []string      `json:"ciphers,omitempty"`
	HostKeyAlg     string        `json:"host_key_alg,omitempty"`
	HostKeySHA256  string        `json:"host_key_sha256,omitempty"`
	PingReport     *PingReport   `json:"ping_report,omitempty"`
}

// JitterSample represents one latency probe.
type JitterSample struct {
	Sequence   int     `json:"seq"`
	LatencyMs  float64 `json:"latency_ms"`
	Success    bool    `json:"success"`
	Error      string  `json:"error,omitempty"`
}

// PingReport contains 10-packet ping & jitter metrics.
type PingReport struct {
	Count       int            `json:"count"`
	Successful  int            `json:"successful"`
	Lost        int            `json:"lost"`
	LossPercent float64        `json:"loss_percent"`
	MinMs       float64        `json:"min_ms"`
	AvgMs       float64        `json:"avg_ms"`
	MaxMs       float64        `json:"max_ms"`
	JitterMs    float64        `json:"jitter_ms"`
	Samples     []JitterSample `json:"samples"`
}

// LogEntry represents a single parsed remote system log line.
type LogEntry struct {
	Timestamp string `json:"timestamp"`
	Facility  string `json:"facility,omitempty"`
	Level     string `json:"level"` // error, warn, info, debug
	Message   string `json:"message"`
	Raw       string `json:"raw"`
}

// DiagnosticCommandResult contains output of predefined diagnostic tools.
type DiagnosticCommandResult struct {
	CommandKey  string `json:"command_key"`
	Command     string `json:"command"`
	ExitCode    int    `json:"exit_code"`
	Output      string `json:"output"`
	DurationMs  int64  `json:"duration_ms"`
	Error       string `json:"error,omitempty"`
}
