package screens

import (
	"fmt"

	"sshpilot/internal/config"
)

func formatServerTarget(server config.ServerConfig) string {
	target := fmt.Sprintf("%s@%s", server.User, server.Host)
	if server.Port == "" {
		return target
	}
	return fmt.Sprintf("%s:%s", target, server.Port)
}

func buildSSHArgs(server config.ServerConfig) []string {
	target := fmt.Sprintf("%s@%s", server.User, server.Host)
	if server.Port == "" {
		return []string{target}
	}
	return []string{"-p", server.Port, target}
}
