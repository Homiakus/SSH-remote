package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"sshpilot/internal/config"
	"sshpilot/internal/diagnostics"
)

// HandleServerDiagnosticsAudit handles POST /api/servers/{name}/diagnostics/run
func HandleServerDiagnosticsAudit(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	name := extractServerName(r.URL.Path, "/diagnostics/run")
	if name == "" {
		name = extractServerName(r.URL.Path, "/diagnostics")
	}
	if name == "" {
		http.Error(w, `{"error":"server name missing"}`, http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadServer(name)
	if err != nil {
		http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
		return
	}

	report := diagnostics.RunFullDiagnosticAudit(cfg)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": report.OverallStatus != diagnostics.StageFail,
		"report":  report,
	})
}

// HandleServerPingJitter handles GET /api/servers/{name}/diagnostics/ping
func HandleServerPingJitter(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	name := extractServerName(r.URL.Path, "/diagnostics/ping")
	if name == "" {
		http.Error(w, `{"error":"server name missing"}`, http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadServer(name)
	if err != nil {
		http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
		return
	}

	count := 5
	if cStr := r.URL.Query().Get("count"); cStr != "" {
		if c, err := strconv.Atoi(cStr); err == nil && c > 0 {
			count = c
		}
	}

	pingReport := diagnostics.RunPingJitter(cfg, count)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"ping":    pingReport,
	})
}

// HandleServerLogs handles GET /api/servers/{name}/diagnostics/logs
func HandleServerLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	name := extractServerName(r.URL.Path, "/diagnostics/logs")
	if name == "" {
		http.Error(w, `{"error":"server name missing"}`, http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadServer(name)
	if err != nil {
		http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
		return
	}

	logType := r.URL.Query().Get("type")
	filter := r.URL.Query().Get("filter")
	lines := 60
	if lStr := r.URL.Query().Get("lines"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			lines = l
		}
	}

	entries, err := diagnostics.FetchRemoteLogs(cfg, logType, lines, filter)
	if err != nil && len(entries) == 0 {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"entries": entries,
		"count":   len(entries),
	})
}

// HandleServerDiagnosticExec handles POST /api/servers/{name}/diagnostics/exec
func HandleServerDiagnosticExec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	name := extractServerName(r.URL.Path, "/diagnostics/exec")
	if name == "" {
		http.Error(w, `{"error":"server name missing"}`, http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadServer(name)
	if err != nil {
		http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
		return
	}

	var req struct {
		CommandKey string `json:"command_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request json"}`, http.StatusBadRequest)
		return
	}

	result, err := diagnostics.ExecuteDiagnosticCommand(cfg, req.CommandKey)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": err == nil,
		"result":  result,
	})
}
