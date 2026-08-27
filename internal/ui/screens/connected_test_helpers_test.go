package screens

import (
	"context"
	"io"
	"strings"
	"sync"

	"sshpilot/internal/config"
	sshclient "sshpilot/internal/ssh"

	tea "github.com/charmbracelet/bubbletea"
)

func newConnectedModelWithRemoteFS(
	server config.ServerConfig,
	opener func(config.ServerConfig) (sshclient.RemoteFS, error),
) ConnectedModel {
	connection := &fakeSSHConnection{
		server: server,
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			return opener(server)
		},
	}

	return newConnectedModelWithRuntime(server, fakeSSHRuntime{connection: connection})
}

type fakeSSHRuntime struct {
	connection *fakeSSHConnection
}

func (r fakeSSHRuntime) NewConnection(server config.ServerConfig) sshConnection {
	if r.connection == nil {
		return &fakeSSHConnection{}
	}
	if r.connection.server.Name == "" && r.connection.server.Host == "" {
		r.connection.server = server
	}
	return r.connection
}

type fakeSSHConnection struct {
	mu sync.Mutex

	server config.ServerConfig

	detectRemotePlatformFn func() (sshclient.RemotePlatform, error)
	diagnoseFn             func() sshclient.DiagnosticReport
	openRemoteFSFn         func() (sshclient.RemoteFS, error)
	nativeTerminalFn       func(string, int, int) tea.ExecCommand
	executeFn              func(string) (string, error)
	executeStreamFn        func(string, chan<- string, chan<- sshclient.ScriptResult) error
	resetFn                func() error
	closeFn                func() error

	detectRemotePlatformCalls int
	diagnoseCalls             int
	openRemoteFSCalls         int
	resetCalls                int
	nativeTerminalCalls       int
	executeCalls              int
	executeStreamCalls        int
	closeCalls                int
	nativeCommands            []string
}

func (c *fakeSSHConnection) DetectRemotePlatform() (sshclient.RemotePlatform, error) {
	c.mu.Lock()
	c.detectRemotePlatformCalls++
	c.mu.Unlock()
	if c.detectRemotePlatformFn != nil {
		return c.detectRemotePlatformFn()
	}
	return sshclient.RemotePlatform{GOOS: "linux", GOARCH: "amd64"}, nil
}

func (c *fakeSSHConnection) Diagnose() sshclient.DiagnosticReport {
	c.mu.Lock()
	c.diagnoseCalls++
	c.mu.Unlock()
	if c.diagnoseFn != nil {
		return c.diagnoseFn()
	}
	return sshclient.DiagnoseConnection(&config.ServerConfig{
		Name:       c.server.Name,
		Host:       c.server.Host,
		Port:       c.server.Port,
		User:       c.server.User,
		AuthMethod: c.server.AuthMethod,
		Password:   c.server.Password,
		KeyPath:    c.server.KeyPath,
		Passphrase: c.server.Passphrase,
	})
}

func (c *fakeSSHConnection) OpenRemoteFS() (sshclient.RemoteFS, error) {
	c.mu.Lock()
	c.openRemoteFSCalls++
	c.mu.Unlock()
	if c.openRemoteFSFn != nil {
		return c.openRemoteFSFn()
	}
	return nil, nil
}

func (c *fakeSSHConnection) NativeTerminalCommand(command string, height, width int) tea.ExecCommand {
	c.mu.Lock()
	c.nativeTerminalCalls++
	c.mu.Unlock()
	if c.nativeTerminalFn != nil {
		return c.nativeTerminalFn(command, height, width)
	}
	return &fakeExecCommand{runFn: func() error {
		c.mu.Lock()
		c.nativeCommands = append(c.nativeCommands, command)
		c.mu.Unlock()
		return nil
	}}
}

func (c *fakeSSHConnection) Reset() error {
	c.mu.Lock()
	c.resetCalls++
	c.mu.Unlock()
	if c.resetFn != nil {
		return c.resetFn()
	}
	return nil
}

func (c *fakeSSHConnection) ExecuteScript(content string) (string, error) {
	c.mu.Lock()
	c.executeCalls++
	c.mu.Unlock()
	if c.executeFn != nil {
		return c.executeFn(content)
	}
	return "", nil
}

func (c *fakeSSHConnection) ExecuteScriptStream(
	_ context.Context,
	content string,
	outputCh chan<- string,
	doneCh chan<- sshclient.ScriptResult,
) error {
	c.mu.Lock()
	c.executeStreamCalls++
	c.mu.Unlock()
	if c.executeStreamFn != nil {
		return c.executeStreamFn(content, outputCh, doneCh)
	}

	var (
		output string
		err    error
	)
	if c.executeFn != nil {
		c.mu.Lock()
		c.executeCalls++
		c.mu.Unlock()
		output, err = c.executeFn(content)
	}

	doneCh <- sshclient.ScriptResult{Output: output, Err: err}
	close(outputCh)
	close(doneCh)
	return nil
}

func (c *fakeSSHConnection) Close() error {
	c.mu.Lock()
	c.closeCalls++
	c.mu.Unlock()
	if c.closeFn != nil {
		return c.closeFn()
	}
	return nil
}

func (c *fakeSSHConnection) ResetCallCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.resetCalls
}

type fakeTerminalSession struct {
	outputCh      chan string
	doneCh        chan error
	commandDoneCh chan localCommandResult

	sendErr       error
	sendDataErr   error
	runCommandErr error
	interruptErr  error
	closeErr      error
	sendFn        func(string) error
	sendLineFn    func(string) error
	runCommandFn  func(localCommandSpec) error
	interruptFn   func() error

	sentLines      []string
	sentData       []string
	runSpecs       []localCommandSpec
	closeCalls     int
	interruptCalls int
	running        bool
}

func newFakeTerminalSession() *fakeTerminalSession {
	return &fakeTerminalSession{
		outputCh:      make(chan string, 8),
		doneCh:        make(chan error, 1),
		commandDoneCh: make(chan localCommandResult, 8),
	}
}

func (s *fakeTerminalSession) Close() error {
	s.closeCalls++
	return s.closeErr
}

func (s *fakeTerminalSession) Done() <-chan error {
	return s.doneCh
}

func (s *fakeTerminalSession) Interrupt() error {
	s.interruptCalls++
	if s.interruptFn != nil {
		return s.interruptFn()
	}
	return s.interruptErr
}

func (s *fakeTerminalSession) Output() <-chan string {
	return s.outputCh
}

func (s *fakeTerminalSession) Send(data string) error {
	s.sentData = append(s.sentData, data)
	if s.sendFn != nil {
		return s.sendFn(data)
	}
	return s.sendDataErr
}

func (s *fakeTerminalSession) SendLine(line string) error {
	s.sentLines = append(s.sentLines, line)
	if s.sendLineFn != nil {
		return s.sendLineFn(line)
	}
	return s.sendErr
}

func (s *fakeTerminalSession) CommandDone() <-chan localCommandResult {
	return s.commandDoneCh
}

func (s *fakeTerminalSession) RunCommand(spec localCommandSpec) error {
	s.runSpecs = append(s.runSpecs, spec)
	s.running = true
	if s.runCommandFn != nil {
		return s.runCommandFn(spec)
	}
	return s.runCommandErr
}

func (s *fakeTerminalSession) Running() bool {
	return s.running
}

func (s *fakeTerminalSession) finishCommand(result localCommandResult) {
	s.running = false
	s.commandDoneCh <- result
}

func terminalTabText(m ConnectedModel, kind terminalTabKind) string {
	tab := m.terminalTab(kind)
	if tab == nil {
		return ""
	}

	lines := append([]string{}, tab.lines...)
	if tab.partial != "" {
		lines = append(lines, tab.partial)
	}
	return strings.Join(lines, "\n")
}

type fakeExecCommand struct {
	err       error
	runFn     func() error
	stdinSet  bool
	stdoutSet bool
	stderrSet bool
}

func (c *fakeExecCommand) Run() error {
	if c.runFn != nil {
		return c.runFn()
	}
	return c.err
}

func (c *fakeExecCommand) RunImmediatelyForTest() bool {
	return true
}

func (c *fakeExecCommand) SetStdin(_ io.Reader) {
	c.stdinSet = true
}

func (c *fakeExecCommand) SetStdout(_ io.Writer) {
	c.stdoutSet = true
}

func (c *fakeExecCommand) SetStderr(_ io.Writer) {
	c.stderrSet = true
}
