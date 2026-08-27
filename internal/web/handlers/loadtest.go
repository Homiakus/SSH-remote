package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"sshpilot/internal/loadtest"
)

// HandleLoadTest routes /api/loadtest/* endpoints.
func HandleLoadTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	engine := loadtest.GetDefaultEngine()
	path := strings.TrimPrefix(r.URL.Path, "/api/loadtest")
	path = strings.TrimPrefix(path, "/")

	switch path {
	case "start":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		var cfg loadtest.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, `{"error":"invalid request json"}`, http.StatusBadRequest)
			return
		}
		report, err := engine.StartJob(cfg)
		if err != nil {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"error": err.Error(),
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"job":     report,
		})

	case "status":
		report, ok := engine.GetCurrentStatus()
		if !ok {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"has_job": false,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"has_job": true,
			"job":     report,
		})

	case "stop":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		stopped := engine.StopCurrentJob()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": stopped,
		})

	case "history":
		history := engine.GetHistory()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"history": history,
			"count":   len(history),
		})

	default:
		http.Error(w, `{"error":"endpoint not found"}`, http.StatusNotFound)
	}
}
