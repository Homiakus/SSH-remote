package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleServers(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/servers", nil)
	rec := httptest.NewRecorder()

	HandleServers(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response JSON: %v", err)
	}

	if _, ok := resp["servers"]; !ok {
		t.Fatalf("Expected 'servers' key in response, got %v", resp)
	}
}

func TestHandleKeys(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/keys", nil)
	rec := httptest.NewRecorder()

	HandleKeys(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode keys JSON: %v", err)
	}

	if _, ok := resp["keys"]; !ok {
		t.Fatalf("Expected 'keys' in response, got %v", resp)
	}
}

func TestHandleScripts(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/scripts", nil)
	rec := httptest.NewRecorder()

	HandleScripts(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode scripts JSON: %v", err)
	}

	if _, ok := resp["packages"]; !ok {
		t.Fatalf("Expected 'packages' in response, got %v", resp)
	}
}

func TestHandleGitHubSync(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/scripts/sync-github", bytes.NewReader([]byte("{}")))
	rec := httptest.NewRecorder()

	HandleGitHubSync(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var resp GitHubSyncStatus
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode sync response: %v", err)
	}

	if resp.Repo == "" {
		t.Fatalf("Expected non-empty repo in status")
	}
}
