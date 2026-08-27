// Package log предоставляет журналирование аудита выполненных скриптов.
package log

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AuditEntry описывает одну запись аудита о выполненном скрипте.
type AuditEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	ServerName string    `json:"server_name"`
	ServerHost string    `json:"server_host"`
	ScriptName string    `json:"script_name"`
	ScriptKind string    `json:"script_kind"`
	Success    bool      `json:"success"`
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"duration_ms"`
}

const auditLogDir = ".sshpilot"

func logPath(serverName string) string {
	if err := os.MkdirAll(auditLogDir, 0700); err != nil {
		return ""
	}
	return filepath.Join(auditLogDir, serverName+".log")
}

// AppendAudit добавляет запись о выполнении скрипта в лог сервера.
func AppendAudit(serverName, serverHost, scriptName, scriptKind string, success bool, errStr string, dur time.Duration) {
	path := logPath(serverName)
	if path == "" {
		return
	}

	entry := AuditEntry{
		Timestamp:  time.Now(),
		ServerName: serverName,
		ServerHost: serverHost,
		ScriptName: scriptName,
		ScriptKind: scriptKind,
		Success:    success,
		Error:      errStr,
		DurationMs: dur.Milliseconds(),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, string(data))
}
