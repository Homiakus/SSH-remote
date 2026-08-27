package web

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

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
		switch {
		case strings.HasSuffix(path, "/test"):
			handlers.HandleServerTest(w, r)
		case strings.HasSuffix(path, "/metrics"):
			handlers.HandleServerMetrics(w, r)
		case strings.HasSuffix(path, "/process/kill"):
			handlers.HandleProcessKill(w, r)
		case strings.HasSuffix(path, "/service/action"):
			handlers.HandleServiceAction(w, r)
		case strings.HasSuffix(path, "/diagnostics/run"), strings.HasSuffix(path, "/diagnostics"):
			handlers.HandleServerDiagnosticsAudit(w, r)
		case strings.HasSuffix(path, "/diagnostics/ping"):
			handlers.HandleServerPingJitter(w, r)
		case strings.HasSuffix(path, "/diagnostics/logs"):
			handlers.HandleServerLogs(w, r)
		case strings.HasSuffix(path, "/diagnostics/exec"):
			handlers.HandleServerDiagnosticExec(w, r)
		default:
			handlers.HandleServerDetail(w, r)
		}
	})

	mux.HandleFunc("/api/loadtest", handlers.HandleLoadTest)
	mux.HandleFunc("/api/loadtest/", handlers.HandleLoadTest)

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

// StartServerWithGracefulShutdown binds to the preferred port or automatically finds the next available port,
// invokes onStarted with the actual URL, and listens for OS interrupt signals for graceful cleanup.
func StartServerWithGracefulShutdown(host string, preferredPort string, onStarted func(url string)) error {
	router := NewRouter()

	ln, actualPort, err := ListenAvailable(host, preferredPort)
	if err != nil {
		return fmt.Errorf("failed to bind any available port: %w", err)
	}

	actualAddr := fmt.Sprintf("%s:%d", host, actualPort)
	actualURL := fmt.Sprintf("http://%s", actualAddr)

	server := &http.Server{
		Handler: router,
	}

	if onStarted != nil {
		onStarted(actualURL)
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.Serve(ln)
	}()

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)

	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
	case sig := <-shutdown:
		fmt.Printf("\n\x1b[1;33m[!] Received signal %v, shutting down SSHPILOT cleanly...\x1b[0m\n", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			_ = server.Close()
			return fmt.Errorf("could not stop server gracefully: %w", err)
		}
		fmt.Println("\x1b[1;32m✓ SSHPILOT server stopped gracefully.\x1b[0m")
	}

	return nil
}

// ListenAvailable tries the preferred port first, then searches subsequent ports, and falls back to dynamic port.
func ListenAvailable(host string, preferredPortStr string) (net.Listener, int, error) {
	prefPort, err := strconv.Atoi(preferredPortStr)
	if err != nil || prefPort <= 0 {
		prefPort = 8080
	}

	// 1. Try preferred port first
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, prefPort))
	if err == nil {
		return ln, prefPort, nil
	}

	// 2. Try next 50 consecutive ports
	for port := prefPort + 1; port <= prefPort+50; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
		if err == nil {
			return ln, port, nil
		}
	}

	// 3. Fallback: OS ephemeral dynamic port
	ln, err = net.Listen("tcp", fmt.Sprintf("%s:0", host))
	if err != nil {
		return nil, 0, err
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return ln, 0, nil
	}
	return ln, tcpAddr.Port, nil
}

// StartServer starts the Web UI server on the given address (legacy synchronous wrapper).
func StartServer(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host = "127.0.0.1"
		port = "8080"
	}
	return StartServerWithGracefulShutdown(host, port, func(url string) {
		fmt.Printf("\n\x1b[1;32m✓ SSHPILOT Web UI is running on %s\x1b[0m\n", url)
	})
}
