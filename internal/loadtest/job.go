package loadtest

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"sshpilot/internal/config"
	"sshpilot/internal/ssh"
)

// Job represents an active or completed load test execution.
type Job struct {
	mu             sync.RWMutex
	config         Config
	ctx            context.Context
	cancel         context.CancelFunc
	running        bool
	done           bool
	startTime      time.Time
	endTime        time.Time
	totalSent      int64
	totalSuccess   int64
	totalFailed    int64
	bytesTotal     int64
	statusCodes    map[string]int64
	errorBreakdown map[string]int64
	latencies      []float64
	recentWindow   []float64
	errorMsg       string
}

// NewJob creates and prepares a new Job instance.
func NewJob(parentCtx context.Context, cfg Config) *Job {
	if cfg.Concurrency <= 0 {
		cfg.Concurrency = 1
	}
	if cfg.Concurrency > 200 {
		cfg.Concurrency = 200
	}
	if cfg.TimeoutMs <= 0 {
		cfg.TimeoutMs = 10000
	}
	if cfg.TotalRequests <= 0 && cfg.DurationSec <= 0 {
		cfg.TotalRequests = 100
	}

	ctx, cancel := context.WithCancel(parentCtx)
	return &Job{
		config:         cfg,
		ctx:            ctx,
		cancel:         cancel,
		statusCodes:    make(map[string]int64),
		errorBreakdown: make(map[string]int64),
		latencies:      make([]float64, 0, 1024),
		recentWindow:   make([]float64, 0, 50),
	}
}

// Run starts the load test execution loop asynchronously.
func (j *Job) Run() {
	j.mu.Lock()
	j.running = true
	j.startTime = time.Now()
	j.mu.Unlock()

	defer func() {
		j.mu.Lock()
		j.running = false
		j.done = true
		j.endTime = time.Now()
		j.mu.Unlock()
	}()

	var durationLimit time.Duration
	if j.config.DurationSec > 0 {
		durationLimit = time.Duration(j.config.DurationSec) * time.Second
	}

	var rateTicker *time.Ticker
	var rateTokens chan struct{}
	if j.config.RPSLimit > 0 {
		rateTicker = time.NewTicker(time.Second / time.Duration(j.config.RPSLimit))
		defer rateTicker.Stop()
		rateTokens = make(chan struct{}, j.config.RPSLimit)
		go func() {
			for {
				select {
				case <-j.ctx.Done():
					return
				case <-rateTicker.C:
					select {
					case rateTokens <- struct{}{}:
					default:
					}
				}
			}
		}()
	}

	tr := &http.Transport{
		MaxIdleConns:        j.config.Concurrency * 2,
		MaxIdleConnsPerHost: j.config.Concurrency * 2,
		IdleConnTimeout:     30 * time.Second,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
	}
	httpClient := &http.Client{
		Transport: tr,
		Timeout:   time.Duration(j.config.TimeoutMs) * time.Millisecond,
	}

	var reqCounter int64
	var wg sync.WaitGroup

	for i := 0; i < j.config.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-j.ctx.Done():
					return
				default:
				}

				if durationLimit > 0 && time.Since(j.startTime) >= durationLimit {
					return
				}

				if j.config.TotalRequests > 0 {
					current := atomic.AddInt64(&reqCounter, 1)
					if current > int64(j.config.TotalRequests) {
						return
					}
				}

				if rateTokens != nil {
					select {
					case <-j.ctx.Done():
						return
					case <-rateTokens:
					}
				}

				res := j.executeSingle(httpClient)
				j.recordResult(res)
			}
		}()
	}

	wg.Wait()
}

// Stop cancels the running job.
func (j *Job) Stop() {
	if j.cancel != nil {
		j.cancel()
	}
}

func (j *Job) executeSingle(httpClient *http.Client) ResultRecord {
	start := time.Now()

	switch j.config.TargetType {
	case TargetSSHConnect:
		return j.executeSSHConnect(start)
	case TargetSSHCommand:
		return j.executeSSHCommand(start)
	case TargetHTTP:
		fallthrough
	default:
		return j.executeHTTP(httpClient, start)
	}
}

func (j *Job) executeHTTP(client *http.Client, start time.Time) ResultRecord {
	reqMethod := strings.ToUpper(j.config.Method)
	if reqMethod == "" {
		reqMethod = "GET"
	}
	url := j.config.URL
	if url == "" {
		url = "http://127.0.0.1:8080/api/servers"
	}

	var bodyReader io.Reader
	if len(j.config.Body) > 0 {
		bodyReader = strings.NewReader(j.config.Body)
	}

	req, err := http.NewRequestWithContext(j.ctx, reqMethod, url, bodyReader)
	if err != nil {
		dur := time.Since(start).Seconds() * 1000.0
		return ResultRecord{DurationMs: dur, Err: err}
	}

	for k, v := range j.config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	dur := time.Since(start).Seconds() * 1000.0
	if err != nil {
		return ResultRecord{DurationMs: dur, Err: err}
	}
	defer resp.Body.Close()

	n, _ := io.Copy(io.Discard, resp.Body)
	return ResultRecord{
		DurationMs: dur,
		StatusCode: resp.StatusCode,
		Bytes:      n,
	}
}

func (j *Job) executeSSHConnect(start time.Time) ResultRecord {
	cfg, err := config.LoadServer(j.config.ServerName)
	if err != nil {
		dur := time.Since(start).Seconds() * 1000.0
		return ResultRecord{DurationMs: dur, Err: err}
	}

	client, err := ssh.Connect(cfg)
	dur := time.Since(start).Seconds() * 1000.0
	if err != nil {
		return ResultRecord{DurationMs: dur, Err: err}
	}
	_ = client.Close()

	return ResultRecord{
		DurationMs: dur,
		StatusCode: 200,
		Bytes:      128,
	}
}

func (j *Job) executeSSHCommand(start time.Time) ResultRecord {
	cfg, err := config.LoadServer(j.config.ServerName)
	if err != nil {
		dur := time.Since(start).Seconds() * 1000.0
		return ResultRecord{DurationMs: dur, Err: err}
	}

	client, err := ssh.Connect(cfg)
	if err != nil {
		dur := time.Since(start).Seconds() * 1000.0
		return ResultRecord{DurationMs: dur, Err: err}
	}
	defer client.Close()

	cmd := j.config.Command
	if cmd == "" {
		cmd = "echo ok"
	}
	out, err := ssh.ExecuteCommand(client, cmd)
	dur := time.Since(start).Seconds() * 1000.0
	if err != nil {
		return ResultRecord{DurationMs: dur, Err: err}
	}

	return ResultRecord{
		DurationMs: dur,
		StatusCode: 200,
		Bytes:      int64(len(out)),
	}
}

func (j *Job) recordResult(res ResultRecord) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.totalSent++
	j.latencies = append(j.latencies, res.DurationMs)

	if len(j.recentWindow) >= 50 {
		j.recentWindow = j.recentWindow[1:]
	}
	j.recentWindow = append(j.recentWindow, res.DurationMs)

	if res.Err != nil {
		j.totalFailed++
		errStr := simplifyError(res.Err)
		j.errorBreakdown[errStr]++
	} else if res.StatusCode >= 400 {
		j.totalFailed++
		scStr := strconv.Itoa(res.StatusCode)
		j.statusCodes[scStr]++
	} else {
		j.totalSuccess++
		scStr := strconv.Itoa(res.StatusCode)
		j.statusCodes[scStr]++
	}

	j.bytesTotal += res.Bytes
}

func (j *Job) GetReport() StatusReport {
	j.mu.RLock()
	defer j.mu.RUnlock()

	now := time.Now()
	if j.done && !j.endTime.IsZero() {
		now = j.endTime
	}

	elapsed := now.Sub(j.startTime).Seconds()
	if elapsed <= 0 {
		elapsed = 0.001
	}

	rps := float64(j.totalSent) / elapsed
	progress := 0.0

	if j.config.TotalRequests > 0 {
		progress = (float64(j.totalSent) / float64(j.config.TotalRequests)) * 100.0
		if progress > 100.0 {
			progress = 100.0
		}
	} else if j.config.DurationSec > 0 {
		progress = (elapsed / float64(j.config.DurationSec)) * 100.0
		if progress > 100.0 {
			progress = 100.0
		}
	}

	statusMap := make(map[string]int64, len(j.statusCodes))
	for k, v := range j.statusCodes {
		statusMap[k] = v
	}
	errMap := make(map[string]int64, len(j.errorBreakdown))
	for k, v := range j.errorBreakdown {
		errMap[k] = v
	}

	recent := make([]float64, len(j.recentWindow))
	copy(recent, j.recentWindow)

	return StatusReport{
		ID:             j.config.ID,
		Config:         j.config,
		Running:        j.running,
		Done:           j.done,
		ProgressPct:    round2(progress),
		StartTime:      j.startTime,
		DurationSec:    round2(elapsed),
		TotalSent:      j.totalSent,
		TotalSuccess:   j.totalSuccess,
		TotalFailed:    j.totalFailed,
		CurrentRPS:     round2(rps),
		BytesTotal:     j.bytesTotal,
		StatusCodes:    statusMap,
		ErrorBreakdown: errMap,
		Latency:        CalculatePercentiles(j.latencies),
		RecentLatencies: recent,
		ErrorMessage:   j.errorMsg,
	}
}

func simplifyError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if strings.Contains(s, "context canceled") {
		return "Canceled"
	}
	if strings.Contains(s, "timeout") || strings.Contains(s, "Client.Timeout") {
		return "Timeout"
	}
	if strings.Contains(s, "connection refused") || strings.Contains(s, "connect: connection refused") {
		return "Connection Refused"
	}
	if strings.Contains(s, "no such host") {
		return "DNS Resolution Failed"
	}
	if len(s) > 60 {
		return s[:57] + "..."
	}
	return s
}
