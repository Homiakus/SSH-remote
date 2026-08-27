package web

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"sshpilot/internal/web/handlers"
)

//go:embed static/*
var staticFS embed.FS

// NewRouter sets up all HTTP routes for the web application.
func NewRouter() http.Handler {
	mux := http.NewServeMux()

	// API Endpoints
	mux.HandleFunc("/api/servers", handlers.HandleServers)
	mux.HandleFunc("/api/servers/", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasSuffix(path, "/test") {
			handlers.HandleServerTest(w, r)
		} else if strings.HasSuffix(path, "/diagnostics") {
			handlers.HandleServerDiagnostics(w, r)
		} else {
			handlers.HandleServerDetail(w, r)
		}
	})

	mux.HandleFunc("/api/keys", handlers.HandleKeys)
	mux.HandleFunc("/api/fs/", handlers.HandleFS)

	mux.HandleFunc("/api/scripts", handlers.HandleScripts)
	mux.HandleFunc("/api/scripts/sync-github", handlers.HandleGitHubSync)
	mux.HandleFunc("/api/scripts/execute", handlers.HandleScriptExecute)

	// WebSocket Terminal
	mux.HandleFunc("/ws/terminal", handlers.HandleTerminalWebSocket)

	// Embedded Static Assets
	subFS, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatalf("failed to create sub filesystem: %v", err)
	}
	fileServer := http.FileServer(http.FS(subFS))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Prevent caching for live development
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		fileServer.ServeHTTP(w, r)
	})

	return mux
}

// StartServer starts the Web UI server on the given address.
func StartServer(addr string) error {
	handler := NewRouter()
	fmt.Printf("\n\x1b[1;32m✓ SSHPILOT Web UI is running on http://%s\x1b[0m\n", addr)
	return http.ListenAndServe(addr, handler)
}
