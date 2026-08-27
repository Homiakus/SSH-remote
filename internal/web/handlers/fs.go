package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"sshpilot/internal/config"
	"sshpilot/internal/ssh"
)

// HandleFS routes all filesystem endpoints: /api/fs/list, /api/fs/read, /api/fs/write, etc.
func HandleFS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	serverName := r.URL.Query().Get("server")
	if serverName == "" && r.Method == http.MethodPost {
		serverName = r.FormValue("server")
	}
	if serverName == "" {
		http.Error(w, `{"error":"server query parameter required"}`, http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadServer(serverName)
	if err != nil {
		http.Error(w, `{"error":"server not found"}`, http.StatusNotFound)
		return
	}

	rfs, err := ssh.OpenRemoteFS(cfg)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	defer rfs.Close()

	action := strings.TrimPrefix(r.URL.Path, "/api/fs/")
	action = strings.TrimSpace(strings.Split(action, "/")[0])

	switch action {
	case "list":
		targetPath := r.URL.Query().Get("path")
		if targetPath == "" {
			targetPath = rfs.StartDir()
		}
		entries, err := rfs.ListDir(targetPath)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"path":    targetPath,
			"entries": entries,
			"count":   len(entries),
		})

	case "read":
		targetPath := r.URL.Query().Get("path")
		if targetPath == "" {
			http.Error(w, `{"error":"path required"}`, http.StatusBadRequest)
			return
		}
		preview, err := rfs.ReadFile(targetPath, ssh.FilePreviewLimit)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"file": preview,
		})

	case "write":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Server  string `json:"server"`
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
			http.Error(w, `{"error":"invalid json or path missing"}`, http.StatusBadRequest)
			return
		}
		if err := rfs.WriteFile(req.Path, []byte(req.Content)); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"path":    req.Path,
		})

	case "mkdir":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Server string `json:"server"`
			Path   string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
			http.Error(w, `{"error":"invalid json or path missing"}`, http.StatusBadRequest)
			return
		}
		if err := rfs.Mkdir(req.Path); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"path":    req.Path,
		})

	case "rename":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Server  string `json:"server"`
			OldPath string `json:"old_path"`
			NewPath string `json:"new_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OldPath == "" || req.NewPath == "" {
			http.Error(w, `{"error":"old_path and new_path are required"}`, http.StatusBadRequest)
			return
		}
		if err := rfs.Rename(req.OldPath, req.NewPath); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})

	case "delete":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Server string `json:"server"`
			Path   string `json:"path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
			http.Error(w, `{"error":"path missing"}`, http.StatusBadRequest)
			return
		}
		if err := rfs.Remove(req.Path); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"path":    req.Path,
		})

	case "chmod":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Server string `json:"server"`
			Path   string `json:"path"`
			Mode   string `json:"mode"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" || req.Mode == "" {
			http.Error(w, `{"error":"path and mode required"}`, http.StatusBadRequest)
			return
		}
		val, err := strconv.ParseUint(req.Mode, 8, 32)
		if err != nil {
			http.Error(w, `{"error":"invalid octal mode"}`, http.StatusBadRequest)
			return
		}
		if err := rfs.Chmod(req.Path, os.FileMode(val)); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
		})

	case "upload":
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, `{"error":"file required in form"}`, http.StatusBadRequest)
			return
		}
		defer file.Close()

		remoteDir := r.FormValue("dir")
		if remoteDir == "" {
			remoteDir = rfs.StartDir()
		}
		remotePath := strings.TrimRight(remoteDir, "/") + "/" + header.Filename

		data, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, `{"error":"failed to read upload payload"}`, http.StatusInternalServerError)
			return
		}
		if err := rfs.WriteFile(remotePath, data); err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"path":    remotePath,
		})

	case "download":
		targetPath := r.URL.Query().Get("path")
		if targetPath == "" {
			http.Error(w, `{"error":"path required"}`, http.StatusBadRequest)
			return
		}
		preview, err := rfs.ReadFile(targetPath, 50*1024*1024)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+filepath.Base(targetPath)+"\"")
		_, _ = w.Write([]byte(preview.Content))

	default:
		http.Error(w, `{"error":"unknown action"}`, http.StatusNotFound)
	}
}
