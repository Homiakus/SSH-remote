package handlers

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"sshpilot/internal/config"
	"sshpilot/internal/ssh"
)

type KeyItem struct {
	Name         string `json:"name"`
	Filename     string `json:"filename"`
	RelativePath string `json:"relative_path"`
	PublicKey    string `json:"public_key,omitempty"`
	HasPublic    bool   `json:"has_public"`
}

// HandleKeys handles GET (list keys) and POST (generate server key).
func HandleKeys(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if err := config.EnsureKeysDir(); err != nil {
		http.Error(w, `{"error":"failed to access keys directory"}`, http.StatusInternalServerError)
		return
	}

	keysPath := filepath.Join("servers", "keys")

	switch r.Method {
	case http.MethodGet:
		entries, err := os.ReadDir(keysPath)
		if err != nil {
			http.Error(w, `{"error":"failed to read keys"}`, http.StatusInternalServerError)
			return
		}

		var keys []KeyItem
		for _, e := range entries {
			if e.IsDir() || strings.HasSuffix(e.Name(), ".pub") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".ed25519")
			pubFile := filepath.Join(keysPath, e.Name()+".pub")
			var pubKey string
			hasPub := false
			if pubData, err := os.ReadFile(pubFile); err == nil {
				pubKey = strings.TrimSpace(string(pubData))
				hasPub = true
			}
			keys = append(keys, KeyItem{
				Name:         name,
				Filename:     e.Name(),
				RelativePath: "keys/" + e.Name(),
				PublicKey:    pubKey,
				HasPublic:    hasPub,
			})
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"keys":  keys,
			"count": len(keys),
		})

	case http.MethodPost:
		var req struct {
			ServerName string `json:"server_name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ServerName == "" {
			http.Error(w, `{"error":"server_name is required"}`, http.StatusBadRequest)
			return
		}

		kp, err := ssh.EnsureServerKeyPair(req.ServerName)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"key_path":   kp.RelativePrivateKeyPath,
			"public_key": kp.PublicAuthorizedKey,
		})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}
