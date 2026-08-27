package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleLoadTestLifecycle(t *testing.T) {
	// 1. Status with no job
	req := httptest.NewRequest(http.MethodGet, "/api/loadtest/status", nil)
	w := httptest.NewRecorder()
	HandleLoadTest(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// 2. Start a test
	dummyServer := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(`{"ok":true}`))
	}))
	defer dummyServer.Close()

	payload, _ := json.Marshal(map[string]interface{}{
		"target_type":    "http",
		"url":            dummyServer.URL,
		"method":         "GET",
		"concurrency":    2,
		"total_requests": 20,
		"timeout_ms":     1000,
	})

	startReq := httptest.NewRequest(http.MethodPost, "/api/loadtest/start", bytes.NewReader(payload))
	startW := httptest.NewRecorder()
	HandleLoadTest(startW, startReq)
	if startW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d (body: %s)", startW.Code, startW.Body.String())
	}

	// 3. Check history
	histReq := httptest.NewRequest(http.MethodGet, "/api/loadtest/history", nil)
	histW := httptest.NewRecorder()
	HandleLoadTest(histW, histReq)
	if histW.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", histW.Code)
	}
}
