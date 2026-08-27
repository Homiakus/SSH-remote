package screens

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"sshpilot/internal/config"
	sshclient "sshpilot/internal/ssh"
)

type terminalSession interface {
	Close() error
	Done() <-chan error
	Interrupt() error
	Output() <-chan string
	Send(string) error
	SendLine(string) error
}

type sshConnection interface {
	Close() error
	DetectRemotePlatform() (sshclient.RemotePlatform, error)
	Diagnose() sshclient.DiagnosticReport
	ExecuteScript(string) (string, error)
	ExecuteScriptStream(context.Context, string, chan<- string, chan<- sshclient.ScriptResult) error
	NativeTerminalCommand(string, int, int) tea.ExecCommand
	OpenRemoteFS() (sshclient.RemoteFS, error)
	Reset() error
}

type sshRuntime interface {
	NewConnection(config.ServerConfig) sshConnection
}

type defaultSSHRuntime struct{}

type managedSSHConnection struct {
	manager *sshclient.Manager
}

func (defaultSSHRuntime) NewConnection(server config.ServerConfig) sshConnection {
	cfg := config.ServerConfig{
		Name:        server.Name,
		Host:        server.Host,
		Port:        server.Port,
		User:        server.User,
		AuthMethod:  server.AuthMethod,
		Password:    server.Password,
		KeyPath:     server.KeyPath,
		Passphrase:  server.Passphrase,
		Description: server.Description,
	}

	return managedSSHConnection{
		manager: sshclient.NewManager(&cfg),
	}
}

func (c managedSSHConnection) Diagnose() sshclient.DiagnosticReport {
	return sshclient.DiagnoseConnectionWithManager(c.manager)
}

func (c managedSSHConnection) DetectRemotePlatform() (sshclient.RemotePlatform, error) {
	if err := c.manager.Check(); err != nil {
		return sshclient.RemotePlatform{}, err
	}

	client, err := c.manager.Client()
	if err != nil {
		return sshclient.RemotePlatform{}, err
	}

	return sshclient.DetectRemotePlatform(client)
}

func (c managedSSHConnection) OpenRemoteFS() (sshclient.RemoteFS, error) {
	return sshclient.OpenRemoteFSWithManager(c.manager)
}

func (c managedSSHConnection) NativeTerminalCommand(command string, height, width int) tea.ExecCommand {
	return sshclient.NewNativeCommandWithManager(c.manager, command, height, width)
}

func (c managedSSHConnection) Reset() error {
	return c.manager.Reset()
}

func (c managedSSHConnection) ExecuteScript(scriptContent string) (string, error) {
	if err := c.manager.Check(); err != nil {
		return "", err
	}

	client, err := c.manager.Client()
	if err != nil {
		return "", err
	}

	return sshclient.ExecuteScript(client, scriptContent)
}

func (c managedSSHConnection) ExecuteScriptStream(
	ctx context.Context,
	scriptContent string,
	outputCh chan<- string,
	doneCh chan<- sshclient.ScriptResult,
) error {
	if err := c.manager.Check(); err != nil {
		return err
	}

	client, err := c.manager.Client()
	if err != nil {
		return err
	}

	sshclient.ExecuteScriptStream(ctx, client, scriptContent, outputCh, doneCh)
	return nil
}

func (c managedSSHConnection) Close() error {
	return c.manager.Close()
}
