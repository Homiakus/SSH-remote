package monitoring

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"sshpilot/internal/config"
	"sshpilot/internal/ssh"
)

const collectionScript = `
echo "===SYS==="
uname -srm 2>/dev/null || uname -a
uptime -p 2>/dev/null || uptime 2>/dev/null || true
cat /proc/loadavg 2>/dev/null || true
hostname 2>/dev/null || true

echo "===CPU==="
nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo "1"
cat /proc/stat 2>/dev/null | head -n 1 || true

echo "===MEM==="
free -b 2>/dev/null || cat /proc/meminfo 2>/dev/null || true

echo "===DISK==="
df -k -P 2>/dev/null || true

echo "===NET==="
cat /proc/net/dev 2>/dev/null || true

echo "===PROC==="
ps -eo pid,user,%cpu,%mem,comm --sort=-%cpu 2>/dev/null | head -n 16 || ps aux 2>/dev/null | head -n 16 || true

echo "===SVC==="
for s in ssh sshd nginx caddy docker podman redis redis-server postgresql mysql mariadb ufw fail2ban cron; do
  if systemctl is-active --quiet "$s" 2>/dev/null; then
    echo "$s:active"
  elif systemctl status "$s" >/dev/null 2>&1; then
    echo "$s:inactive"
  fi
done
`

// CollectServerMetrics connects to the remote server, executes the collection script, and parses the output.
func CollectServerMetrics(cfg *config.ServerConfig) (HostMetrics, error) {
	client, err := ssh.Connect(cfg)
	if err != nil {
		return HostMetrics{
			ServerName: cfg.Name,
			Timestamp:  time.Now(),
			RawError:   err.Error(),
		}, fmt.Errorf("failed to connect to %s: %w", cfg.Name, err)
	}
	defer client.Close()

	output, err := ssh.ExecuteScript(client, collectionScript)
	if err != nil && output == "" {
		return HostMetrics{
			ServerName: cfg.Name,
			Timestamp:  time.Now(),
			RawError:   err.Error(),
		}, fmt.Errorf("failed to execute metrics collection: %w", err)
	}

	metrics := ParseCollectionOutput(output)
	metrics.ServerName = cfg.Name
	metrics.Timestamp = time.Now()
	return metrics, nil
}

// ParseCollectionOutput parses raw multi-section telemetry script output into HostMetrics.
func ParseCollectionOutput(raw string) HostMetrics {
	sections := make(map[string]string)
	currentKey := ""
	var currentLines []string

	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "===") && strings.HasSuffix(trimmed, "===") {
			if currentKey != "" {
				sections[currentKey] = strings.Join(currentLines, "\n")
			}
			currentKey = strings.Trim(trimmed, "=")
			currentLines = nil
		} else if currentKey != "" {
			currentLines = append(currentLines, line)
		}
	}
	if currentKey != "" {
		sections[currentKey] = strings.Join(currentLines, "\n")
	}

	metrics := HostMetrics{
		Timestamp: time.Now(),
		Disks:     make([]DiskMetric, 0),
		Processes: make([]ProcessInfo, 0),
		Services:  make([]ServiceInfo, 0),
	}

	parseSysSection(sections["SYS"], &metrics)
	parseCPUSection(sections["CPU"], &metrics)
	parseMemSection(sections["MEM"], &metrics)
	parseDiskSection(sections["DISK"], &metrics)
	parseNetSection(sections["NET"], &metrics)
	parseProcSection(sections["PROC"], &metrics)
	parseSvcSection(sections["SVC"], &metrics)

	return metrics
}

func parseSysSection(content string, m *HostMetrics) {
	lines := cleanLines(content)
	if len(lines) > 0 {
		parts := strings.Fields(lines[0])
		if len(parts) >= 1 {
			m.OSInfo.OSName = parts[0]
		}
		if len(parts) >= 2 {
			m.OSInfo.Kernel = parts[1]
		}
		if len(parts) >= 3 {
			m.OSInfo.Arch = parts[2]
		}
	}
	for _, l := range lines {
		if strings.HasPrefix(l, "up ") || strings.Contains(l, "up ") {
			m.OSInfo.UptimeString = l
		}
		// Load avg from /proc/loadavg (e.g. 0.12 0.08 0.05 1/324 12345)
		fields := strings.Fields(l)
		if len(fields) >= 3 {
			if l1, err := strconv.ParseFloat(fields[0], 64); err == nil {
				if l5, err := strconv.ParseFloat(fields[1], 64); err == nil {
					if l15, err := strconv.ParseFloat(fields[2], 64); err == nil {
						m.CPU.Load1 = l1
						m.CPU.Load5 = l5
						m.CPU.Load15 = l15
					}
				}
			}
		}
	}
	if len(lines) >= 4 {
		m.OSInfo.Hostname = strings.TrimSpace(lines[len(lines)-1])
	}
}

func parseCPUSection(content string, m *HostMetrics) {
	lines := cleanLines(content)
	if len(lines) > 0 {
		if cores, err := strconv.Atoi(strings.TrimSpace(lines[0])); err == nil && cores > 0 {
			m.CPU.Cores = cores
		}
	}
	if m.CPU.Cores == 0 {
		m.CPU.Cores = 1
	}

	for _, l := range lines {
		if strings.HasPrefix(l, "cpu ") {
			// cpu user nice system idle iowait irq softirq steal guest guest_nice
			fields := strings.Fields(l)
			if len(fields) >= 5 {
				var total, idle float64
				for i := 1; i < len(fields); i++ {
					v, _ := strconv.ParseFloat(fields[i], 64)
					total += v
					if i == 4 { // idle
						idle = v
					}
				}
				if total > 0 {
					usage := ((total - idle) / total) * 100.0
					m.CPU.UsagePercent = math.Round(usage*100) / 100
				}
			}
		}
	}
}

func parseMemSection(content string, m *HostMetrics) {
	lines := cleanLines(content)
	for _, l := range lines {
		fields := strings.Fields(l)
		if strings.HasPrefix(l, "Mem:") && len(fields) >= 7 {
			// Mem: total used free shared buff/cache available
			total, _ := strconv.ParseUint(fields[1], 10, 64)
			used, _ := strconv.ParseUint(fields[2], 10, 64)
			free, _ := strconv.ParseUint(fields[3], 10, 64)
			avail, _ := strconv.ParseUint(fields[6], 10, 64)
			m.Memory.TotalBytes = total
			m.Memory.UsedBytes = used
			m.Memory.FreeBytes = free
			m.Memory.AvailableBytes = avail
			if total > 0 {
				m.Memory.UsagePercent = math.Round((float64(used)/float64(total))*10000) / 100
			}
		} else if strings.HasPrefix(l, "Swap:") && len(fields) >= 4 {
			total, _ := strconv.ParseUint(fields[1], 10, 64)
			used, _ := strconv.ParseUint(fields[2], 10, 64)
			m.Memory.SwapTotalBytes = total
			m.Memory.SwapUsedBytes = used
			if total > 0 {
				m.Memory.SwapPercent = math.Round((float64(used)/float64(total))*10000) / 100
			}
		}
	}
}

func parseDiskSection(content string, m *HostMetrics) {
	lines := cleanLines(content)
	for i, l := range lines {
		if i == 0 || strings.HasPrefix(l, "Filesystem") {
			continue
		}
		fields := strings.Fields(l)
		if len(fields) >= 6 {
			// Filesystem 1024-blocks Used Available Capacity Mounted on
			fsName := fields[0]
			if strings.HasPrefix(fsName, "tmpfs") || strings.HasPrefix(fsName, "udev") || strings.HasPrefix(fsName, "none") {
				continue
			}
			totalK, _ := strconv.ParseUint(fields[1], 10, 64)
			usedK, _ := strconv.ParseUint(fields[2], 10, 64)
			availK, _ := strconv.ParseUint(fields[3], 10, 64)
			mount := fields[5]

			totalBytes := totalK * 1024
			usedBytes := usedK * 1024
			availBytes := availK * 1024

			var pct float64
			if totalBytes > 0 {
				pct = math.Round((float64(usedBytes)/float64(totalBytes))*10000) / 100
			}

			m.Disks = append(m.Disks, DiskMetric{
				Filesystem:   fsName,
				MountPoint:   mount,
				TotalBytes:   totalBytes,
				UsedBytes:    usedBytes,
				FreeBytes:    availBytes,
				UsagePercent: pct,
			})
		}
	}
}

func parseNetSection(content string, m *HostMetrics) {
	lines := cleanLines(content)
	var totalRx, totalTx uint64
	for _, l := range lines {
		if !strings.Contains(l, ":") {
			continue
		}
		parts := strings.Split(l, ":")
		if len(parts) != 2 {
			continue
		}
		iface := strings.TrimSpace(parts[0])
		if iface == "lo" {
			continue
		}
		fields := strings.Fields(parts[1])
		if len(fields) >= 9 {
			rx, _ := strconv.ParseUint(fields[0], 10, 64)
			tx, _ := strconv.ParseUint(fields[8], 10, 64)
			totalRx += rx
			totalTx += tx
		}
	}
	m.Network.RxBytes = totalRx
	m.Network.TxBytes = totalTx
}

func parseProcSection(content string, m *HostMetrics) {
	lines := cleanLines(content)
	for i, l := range lines {
		if i == 0 || strings.Contains(strings.ToLower(l), "pid") {
			continue
		}
		fields := strings.Fields(l)
		if len(fields) >= 5 {
			pid, err := strconv.Atoi(fields[0])
			if err != nil {
				continue
			}
			user := fields[1]
			cpu, _ := strconv.ParseFloat(fields[2], 64)
			mem, _ := strconv.ParseFloat(fields[3], 64)
			cmd := strings.Join(fields[4:], " ")

			m.Processes = append(m.Processes, ProcessInfo{
				PID:     pid,
				User:    user,
				CPU:     cpu,
				Memory:  mem,
				Command: cmd,
			})
		}
	}
}

func parseSvcSection(content string, m *HostMetrics) {
	lines := cleanLines(content)
	for _, l := range lines {
		parts := strings.Split(strings.TrimSpace(l), ":")
		if len(parts) == 2 {
			m.Services = append(m.Services, ServiceInfo{
				Name:   parts[0],
				Status: parts[1],
			})
		}
	}
}

func cleanLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}
