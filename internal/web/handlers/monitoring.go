package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"sshpilot/internal/config"
	"sshpilot/internal/monitoring"
)

// HandleServerMetrics handles GET /api/servers/{name}/metrics
func HandleServerMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	name := extractServerName(r.URL.Path, "/metrics")
	if name == "" {
		http.Error(w, `{"error":"server name missing"}`, http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadServer(name)
	if err != nil {
		http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
		return
	}

	metrics, err := monitoring.CollectServerMetrics(cfg)
	if err != nil && metrics.RawError != "" {
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
			"metrics": metrics,
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"metrics": metrics,
	})
}

// HandleProcessKill handles POST /api/servers/{name}/process/kill
func HandleProcessKill(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	name := extractServerName(r.URL.Path, "/process/kill")
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
		PID   int  `json:"pid"`
		Force bool `json:"force"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request json"}`, http.StatusBadRequest)
		return
	}

	if err := monitoring.KillProcess(cfg, req.PID, req.Force); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"pid":     req.PID,
	})
}

// HandleServiceAction handles POST /api/servers/{name}/service/action
func HandleServiceAction(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	name := extractServerName(r.URL.Path, "/service/action")
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
		Service string `json:"service"`
		Action  string `json:"action"` // start, stop, restart
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request json"}`, http.StatusBadRequest)
		return
	}

	if err := monitoring.ControlService(cfg, req.Service, req.Action); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"service": req.Service,
		"action":  req.Action,
	})
}

func extractServerName(path string, suffix string) string {
	cleaned := strings.TrimPrefix(path, "/api/servers/")
	cleaned = strings.TrimSuffix(cleaned, suffix)
	parts := strings.Split(cleaned, "/")
	if len(parts) > 0 {
		return strings.TrimSpace(parts[0])
	}
	return ""
}
