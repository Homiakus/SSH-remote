package screens

import (
	"fmt"

	"sshpilot/internal/scripts"
	sshclient "sshpilot/internal/ssh"
)

// ──────────────── Stream execution messages ────────────────

type scriptStreamStartedMsg struct {
	index    int
	outputCh <-chan string
	doneCh   <-chan sshclient.ScriptResult
}

type scriptStreamOutputMsg struct {
	index    int
	outputCh <-chan string
	text     string
	ok       bool
}

type scriptStreamDoneMsg struct {
	index  int
	result sshclient.ScriptResult
	ok     bool
}

// ──────────────── Script classification ────────────────

// classifySelectedScripts проверяет, что все выбранные скрипты имеют поддерживаемый тип.
func classifySelectedScripts(ss []scripts.Script) ([]scripts.Script, error) {
	runnables := make([]scripts.Script, 0, len(ss))
	for _, script := range ss {
		switch script.Kind {
		case "", scripts.ScriptKindSH, scripts.ScriptKindGo, scripts.ScriptKindBinary:
			runnables = append(runnables, script)
		default:
			return nil, fmt.Errorf("неподдерживаемый тип скрипта: %s", script.Kind)
		}
	}
	return runnables, nil
}
