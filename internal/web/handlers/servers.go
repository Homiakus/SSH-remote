package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"sshpilot/internal/config"
	"sshpilot/internal/ssh"
)

type ServerDTO struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	User        string `json:"user"`
	AuthMethod  string `json:"auth_method"`
	Password    string `json:"password,omitempty"`
	KeyPath     string `json:"key_path,omitempty"`
	Passphrase  string `json:"passphrase,omitempty"`
	Description string `json:"description"`
	Status      string `json:"status,omitempty"`
	LatencyMs   int64  `json:"latency_ms,omitempty"`
}

// HandleServers handles GET (list) and POST (create/update) for servers.
func HandleServers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		servers, err := config.ListServers()
		if err != nil && len(servers) == 0 {
			http.Error(w, `{"error":"failed to list servers"}`, http.StatusInternalServerError)
			return
		}

		dtos := make([]ServerDTO, 0, len(servers))
		for _, s := range servers {
			dtos = append(dtos, ServerDTO{
				Name:        s.Name,
				Host:        s.Host,
				Port:        s.Port,
				User:        s.User,
				AuthMethod:  s.AuthMethod,
				KeyPath:     s.KeyPath,
				Description: s.Description,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"servers": dtos,
			"count":   len(dtos),
		})

	case http.MethodPost:
		var req ServerDTO
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, `{"error":"invalid request json"}`, http.StatusBadRequest)
			return
		}

		if req.Name == "" || req.Host == "" {
			http.Error(w, `{"error":"name and host are required"}`, http.StatusBadRequest)
			return
		}

		if req.Port == "" {
			req.Port = "22"
		}
		if req.User == "" {
			req.User = "root"
		}
		if req.AuthMethod == "" {
			if req.KeyPath != "" {
				req.AuthMethod = "key"
			} else {
				req.AuthMethod = "password"
			}
		}

		cfg := &config.ServerConfig{
			Name:        req.Name,
			Host:        req.Host,
			Port:        req.Port,
			User:        req.User,
			AuthMethod:  req.AuthMethod,
			Password:    req.Password,
			KeyPath:     req.KeyPath,
			Passphrase:  req.Passphrase,
			Description: req.Description,
		}

		if err := config.SaveServer(req.Name, cfg); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusBadRequest)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"server":  req,
		})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// HandleServerDetail handles GET, DELETE for /api/servers/<name>
func HandleServerDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	name := strings.TrimPrefix(r.URL.Path, "/api/servers/")
	name = strings.TrimSpace(strings.Split(name, "/")[0])

	if name == "" {
		http.Error(w, `{"error":"server name missing"}`, http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		cfg, err := config.LoadServer(name)
		if err != nil {
			http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"server": ServerDTO{
				Name:        cfg.Name,
				Host:        cfg.Host,
				Port:        cfg.Port,
				User:        cfg.User,
				AuthMethod:  cfg.AuthMethod,
				KeyPath:     cfg.KeyPath,
				Description: cfg.Description,
			},
		})

	case http.MethodDelete:
		if err := config.DeleteServer(name); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"deleted": name,
		})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// HandleServerTest tests SSH connection and latency for /api/servers/<name>/test
func HandleServerTest(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/servers/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, `{"error":"server name missing"}`, http.StatusBadRequest)
		return
	}
	name := parts[0]

	cfg, err := config.LoadServer(name)
	if err != nil {
		http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
		return
	}

	start := time.Now()
	client, err := ssh.Connect(cfg)
	latency := time.Since(start).Milliseconds()

	if err != nil {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}
	defer client.Close()

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"latency_ms": latency,
		"status":     "online",
	})
}

// HandleServerDiagnostics collects system metrics via SSH for /api/servers/<name>/diagnostics
func HandleServerDiagnostics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/servers/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, `{"error":"server name missing"}`, http.StatusBadRequest)
		return
	}
	name := parts[0]

	cfg, err := config.LoadServer(name)
	if err != nil {
		http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
		return
	}

	report := ssh.DiagnoseConnection(cfg)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success":     report.Err == nil,
		"stage":       report.Stage,
		"banner":      report.Banner,
		"warnings":    report.Warnings,
		"target":      report.Target,
		"tcp_address": report.TCPAddress,
		"error": func() string {
			if report.Err != nil {
				return report.Err.Error()
			}
			return ""
		}(),
	})
}
