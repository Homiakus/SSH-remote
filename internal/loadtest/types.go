package loadtest

import (
	"sync"
	"time"
)

// TargetType defines the type of stress/load benchmark.
type TargetType string

const (
	TargetHTTP       TargetType = "http"
	TargetSSHConnect TargetType = "ssh_connect"
	TargetSSHCommand TargetType = "ssh_command"
)

// Config configures a load test execution.
type Config struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	TargetType    TargetType        `json:"target_type"`
	URL           string            `json:"url,omitempty"`
	Method        string            `json:"method,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          string            `json:"body,omitempty"`
	ServerName    string            `json:"server_name,omitempty"`
	Command       string            `json:"command,omitempty"`
	Concurrency   int               `json:"concurrency"`
	TotalRequests int               `json:"total_requests"`
	DurationSec   int               `json:"duration_sec"`
	RPSLimit      int               `json:"rps_limit"`
	TimeoutMs     int               `json:"timeout_ms"`
}

// Percentiles contains latency distribution.
type Percentiles struct {
	MinMs  float64 `json:"min_ms"`
	AvgMs  float64 `json:"avg_ms"`
	P50Ms  float64 `json:"p50_ms"`
	P90Ms  float64 `json:"p90_ms"`
	P95Ms  float64 `json:"p95_ms"`
	P99Ms  float64 `json:"p99_ms"`
	MaxMs  float64 `json:"max_ms"`
}

// StatusReport contains the real-time and final statistics of a load test.
type StatusReport struct {
	ID             string            `json:"id"`
	Config         Config            `json:"config"`
	Running        bool              `json:"running"`
	Done           bool              `json:"done"`
	ProgressPct    float64           `json:"progress_pct"`
	StartTime      time.Time         `json:"start_time"`
	DurationSec    float64           `json:"duration_sec"`
	TotalSent      int64             `json:"total_sent"`
	TotalSuccess   int64             `json:"total_success"`
	TotalFailed    int64             `json:"total_failed"`
	CurrentRPS     float64           `json:"current_rps"`
	BytesTotal     int64             `json:"bytes_total"`
	StatusCodes    map[string]int64  `json:"status_codes"`
	ErrorBreakdown map[string]int64  `json:"error_breakdown"`
	Latency        Percentiles       `json:"latency"`
	RecentLatencies []float64        `json:"recent_latencies,omitempty"`
	ErrorMessage   string            `json:"error_message,omitempty"`
}

// ResultRecord holds individual request execution metric.
type ResultRecord struct {
	DurationMs float64
	StatusCode int
	Bytes      int64
	Err        error
}

// Engine manages load testing runs and history.
type Engine struct {
	mu           sync.RWMutex
	currentJob   *Job
	history      []StatusReport
	maxHistory   int
}
