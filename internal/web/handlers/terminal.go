package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"

	"golang.org/x/crypto/ssh"

	"sshpilot/internal/config"
	sshinternal "sshpilot/internal/ssh"
	"sshpilot/internal/web/ws"
)

type TermResizeMessage struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
	Data string `json:"data,omitempty"`
}

// HandleTerminalWebSocket manages the interactive bidirectional SSH session over WebSocket.
func HandleTerminalWebSocket(w http.ResponseWriter, r *http.Request) {
	serverName := r.URL.Query().Get("server")
	if serverName == "" {
		http.Error(w, "server parameter is required", http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadServer(serverName)
	if err != nil {
		http.Error(w, "server configuration not found: "+err.Error(), http.StatusNotFound)
		return
	}

	cols := 100
	rows := 30
	if c, err := strconv.Atoi(r.URL.Query().Get("cols")); err == nil && c > 0 {
		cols = c
	}
	if rw, err := strconv.Atoi(r.URL.Query().Get("rows")); err == nil && rw > 0 {
		rows = rw
	}

	conn, err := ws.Upgrade(w, r)
	if err != nil {
		http.Error(w, "websocket upgrade failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer conn.Close()

	// Connect SSH Client
	client, err := sshinternal.Connect(cfg)
	if err != nil {
		_ = conn.WriteText(fmt.Sprintf("\r\n\x1b[31m[SSHPilot] SSH Connection failed: %v\x1b[0m\r\n", err))
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		_ = conn.WriteText(fmt.Sprintf("\r\n\x1b[31m[SSHPilot] Failed to create session: %v\x1b[0m\r\n", err))
		return
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}

	if err := session.RequestPty("xterm-256color", rows, cols, modes); err != nil {
		_ = conn.WriteText(fmt.Sprintf("\r\n\x1b[31m[SSHPilot] Request PTY failed: %v\x1b[0m\r\n", err))
		return
	}

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		_ = conn.WriteText(fmt.Sprintf("\r\n\x1b[31m[SSHPilot] Stdin pipe error: %v\x1b[0m\r\n", err))
		return
	}
	defer stdinPipe.Close()

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		_ = conn.WriteText(fmt.Sprintf("\r\n\x1b[31m[SSHPilot] Stdout pipe error: %v\x1b[0m\r\n", err))
		return
	}

	stderrPipe, err := session.StderrPipe()
	if err != nil {
		_ = conn.WriteText(fmt.Sprintf("\r\n\x1b[31m[SSHPilot] Stderr pipe error: %v\x1b[0m\r\n", err))
		return
	}

	if err := session.Shell(); err != nil {
		_ = conn.WriteText(fmt.Sprintf("\r\n\x1b[31m[SSHPilot] Shell start error: %v\x1b[0m\r\n", err))
		return
	}

	var once sync.Once
	done := make(chan struct{})

	closeAll := func() {
		once.Do(func() {
			close(done)
			_ = session.Close()
			_ = conn.Close()
		})
	}

	// Reader goroutine: SSH Stdout -> WebSocket
	go func() {
		defer closeAll()
		buf := make([]byte, 4096)
		for {
			select {
			case <-done:
				return
			default:
				n, err := stdoutPipe.Read(buf)
				if n > 0 {
					if wErr := conn.WriteBinary(buf[:n]); wErr != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}
	}()

	// Reader goroutine: SSH Stderr -> WebSocket
	go func() {
		defer closeAll()
		buf := make([]byte, 4096)
		for {
			select {
			case <-done:
				return
			default:
				n, err := stderrPipe.Read(buf)
				if n > 0 {
					if wErr := conn.WriteBinary(buf[:n]); wErr != nil {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}
	}()

	// Reader loop: WebSocket Client -> SSH Stdin / WindowChange
	for {
		op, msg, err := conn.ReadMessage()
		if err != nil {
			closeAll()
			break
		}

		if op == ws.OpClose {
			closeAll()
			break
		}

		if len(msg) == 0 {
			continue
		}

		// Check if message is JSON control message (e.g. resize)
		if msg[0] == '{' {
			var ctrl TermResizeMessage
			if err := json.Unmarshal(msg, &ctrl); err == nil {
				switch ctrl.Type {
				case "resize":
					if ctrl.Cols > 0 && ctrl.Rows > 0 {
						_ = session.WindowChange(ctrl.Rows, ctrl.Cols)
					}
					continue
				case "input":
					if len(ctrl.Data) > 0 {
						_, _ = io.WriteString(stdinPipe, ctrl.Data)
					}
					continue
				case "ping":
					_ = conn.WriteText(`{"type":"pong"}`)
					continue
				}
			}
		}

		// Raw data from terminal emulator
		_, _ = stdinPipe.Write(msg)
	}
}
