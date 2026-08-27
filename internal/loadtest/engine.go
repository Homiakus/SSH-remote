package loadtest

import (
	"context"
	"fmt"
	"sync"
	"time"
)

var (
	defaultEngine *Engine
	engineOnce    sync.Once
)

// GetDefaultEngine returns the global singleton Engine instance.
func GetDefaultEngine() *Engine {
	engineOnce.Do(func() {
		defaultEngine = NewEngine(20)
	})
	return defaultEngine
}

// NewEngine creates a new Engine instance with max history limit.
func NewEngine(maxHistory int) *Engine {
	if maxHistory <= 0 {
		maxHistory = 20
	}
	return &Engine{
		history:    make([]StatusReport, 0, maxHistory),
		maxHistory: maxHistory,
	}
}

// StartJob creates and launches a new load test asynchronously.
func (e *Engine) StartJob(cfg Config) (StatusReport, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.currentJob != nil {
		rep := e.currentJob.GetReport()
		if rep.Running {
			return StatusReport{}, fmt.Errorf("a load test is already actively running (ID: %s)", rep.ID)
		}
		// Archive previous run
		e.archiveCurrentJobLocked()
	}

	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("bench_%d", time.Now().UnixNano())
	}
	if cfg.Name == "" {
		cfg.Name = fmt.Sprintf("Load Test %s", time.Now().Format("15:04:05"))
	}

	job := NewJob(context.Background(), cfg)
	e.currentJob = job

	go func() {
		job.Run()
		e.mu.Lock()
		defer e.mu.Unlock()
		if e.currentJob == job {
			e.archiveCurrentJobLocked()
		}
	}()

	return job.GetReport(), nil
}

// StopCurrentJob aborts any actively running load test.
func (e *Engine) StopCurrentJob() bool {
	e.mu.RLock()
	job := e.currentJob
	e.mu.RUnlock()

	if job != nil {
		job.Stop()
		return true
	}
	return false
}

// GetCurrentStatus returns the status of the current or latest job.
func (e *Engine) GetCurrentStatus() (StatusReport, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.currentJob == nil {
		if len(e.history) > 0 {
			return e.history[0], true
		}
		return StatusReport{}, false
	}

	return e.currentJob.GetReport(), true
}

// GetHistory returns list of previous load test reports (newest first).
func (e *Engine) GetHistory() []StatusReport {
	e.mu.RLock()
	defer e.mu.RUnlock()

	out := make([]StatusReport, len(e.history))
	copy(out, e.history)
	return out
}

func (e *Engine) archiveCurrentJobLocked() {
	if e.currentJob == nil {
		return
	}
	rep := e.currentJob.GetReport()
	if rep.TotalSent == 0 && !rep.Done {
		return
	}

	// Check if already in history
	for _, h := range e.history {
		if h.ID == rep.ID {
			return
		}
	}

	// Prepend to history
	e.history = append([]StatusReport{rep}, e.history...)
	if len(e.history) > e.maxHistory {
		e.history = e.history[:e.maxHistory]
	}
}
