package monitoring

import "time"

// HostMetrics aggregates all real-time host telemetry.
type HostMetrics struct {
	ServerName string        `json:"server_name"`
	Timestamp  time.Time     `json:"timestamp"`
	OSInfo     SystemInfo    `json:"os_info"`
	CPU        CPUMetrics    `json:"cpu"`
	Memory     MemoryMetrics `json:"memory"`
	Disks      []DiskMetric  `json:"disks"`
	Network    NetworkMetric `json:"network"`
	Processes  []ProcessInfo `json:"processes"`
	Services   []ServiceInfo `json:"services"`
	RawError   string        `json:"raw_error,omitempty"`
}

// SystemInfo describes OS, kernel, hostname, and uptime.
type SystemInfo struct {
	OSName       string `json:"os_name"`
	Kernel       string `json:"kernel"`
	Arch         string `json:"arch"`
	Hostname     string `json:"hostname"`
	UptimeString string `json:"uptime_string"`
}

// CPUMetrics contains utilization, core counts, and load averages.
type CPUMetrics struct {
	UsagePercent float64 `json:"usage_percent"`
	Cores        int     `json:"cores"`
	Load1        float64 `json:"load_1"`
	Load5        float64 `json:"load_5"`
	Load15       float64 `json:"load_15"`
}

// MemoryMetrics contains RAM and Swap usage in bytes and percentages.
type MemoryMetrics struct {
	TotalBytes     uint64  `json:"total_bytes"`
	UsedBytes      uint64  `json:"used_bytes"`
	FreeBytes      uint64  `json:"free_bytes"`
	AvailableBytes uint64  `json:"available_bytes"`
	UsagePercent   float64 `json:"usage_percent"`
	SwapTotalBytes uint64  `json:"swap_total_bytes"`
	SwapUsedBytes  uint64  `json:"swap_used_bytes"`
	SwapPercent    float64 `json:"swap_percent"`
}

// DiskMetric describes a mount point partition.
type DiskMetric struct {
	Filesystem   string  `json:"filesystem"`
	MountPoint   string  `json:"mount_point"`
	TotalBytes   uint64  `json:"total_bytes"`
	UsedBytes    uint64  `json:"used_bytes"`
	FreeBytes    uint64  `json:"free_bytes"`
	UsagePercent float64 `json:"usage_percent"`
}

// NetworkMetric contains aggregated network traffic.
type NetworkMetric struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

// ProcessInfo describes a running OS process.
type ProcessInfo struct {
	PID     int     `json:"pid"`
	User    string  `json:"user"`
	CPU     float64 `json:"cpu_percent"`
	Memory  float64 `json:"mem_percent"`
	Command string  `json:"command"`
}

// ServiceInfo describes a systemd service status.
type ServiceInfo struct {
	Name   string `json:"name"`
	Status string `json:"status"` // active, inactive, failed, not-found
}
