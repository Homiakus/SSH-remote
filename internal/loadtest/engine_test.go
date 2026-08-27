package loadtest

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCalculatePercentiles(t *testing.T) {
	latencies := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	p := CalculatePercentiles(latencies)

	if p.MinMs != 10.0 {
		t.Fatalf("MinMs = %v, want 10", p.MinMs)
	}
	if p.MaxMs != 100.0 {
		t.Fatalf("MaxMs = %v, want 100", p.MaxMs)
	}
	if p.AvgMs != 55.0 {
		t.Fatalf("AvgMs = %v, want 55", p.AvgMs)
	}
	if p.P50Ms < 50.0 || p.P50Ms > 60.0 {
		t.Fatalf("P50Ms = %v, want ~55", p.P50Ms)
	}
}

func TestCalculatePercentilesEmpty(t *testing.T) {
	p := CalculatePercentiles(nil)
	if p.MinMs != 0 || p.MaxMs != 0 {
		t.Fatalf("expected zeroes for empty latencies, got %+v", p)
	}
}

func TestHTTPLoadTestExecution(t *testing.T) {
	var requestCount int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&requestCount, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	engine := NewEngine(5)
	cfg := Config{
		TargetType:    TargetHTTP,
		URL:           server.URL,
		Method:        "GET",
		Concurrency:   4,
		TotalRequests: 50,
		TimeoutMs:     2000,
	}

	report, err := engine.StartJob(cfg)
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}
	if report.ID == "" {
		t.Fatal("expected non-empty job ID")
	}

	// Wait for job completion
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		status, ok := engine.GetCurrentStatus()
		if ok && status.Done {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	finalStatus, ok := engine.GetCurrentStatus()
	if !ok {
		t.Fatal("expected to get current status")
	}
	if !finalStatus.Done {
		t.Fatal("expected job to be done")
	}
	if finalStatus.TotalSuccess != 50 {
		t.Fatalf("total success = %d, want 50", finalStatus.TotalSuccess)
	}
	if finalStatus.StatusCodes["200"] != 50 {
		t.Fatalf("200 status count = %d, want 50", finalStatus.StatusCodes["200"])
	}
	if finalStatus.Latency.AvgMs <= 0 {
		t.Fatalf("avg latency should be > 0, got %v", finalStatus.Latency.AvgMs)
	}
}

func TestStopCurrentJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	engine := NewEngine(5)
	cfg := Config{
		TargetType:    TargetHTTP,
		URL:           server.URL,
		Concurrency:   2,
		TotalRequests: 500,
		TimeoutMs:     5000,
	}

	_, err := engine.StartJob(cfg)
	if err != nil {
		t.Fatalf("StartJob failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)
	stopped := engine.StopCurrentJob()
	if !stopped {
		t.Fatal("expected StopCurrentJob to return true")
	}

	time.Sleep(200 * time.Millisecond)
	status, _ := engine.GetCurrentStatus()
	if status.TotalSent >= 500 {
		t.Fatalf("job sent %d requests, expected premature stop", status.TotalSent)
	}
}
