package ssh

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/term"

	"sshpilot/internal/config"
)

type shellClient interface {
	Close() error
	NewSession() (shellSession, error)
}

type shellSession interface {
	Close() error
	RequestPty(term string, h, w int, modes ssh.TerminalModes) error
	Run(cmd string) error
	Signal(sig ssh.Signal) error
	SetStderr(io.Writer)
	SetStdin(io.Reader)
	SetStdout(io.Writer)
	Shell() error
	Wait() error
}

type fdReader interface {
	Fd() uintptr
}

var enterTerminalRawMode = func(stdin io.Reader) (func(), error) {
	file, ok := stdin.(fdReader)
	if !ok {
		return func() {}, nil
	}

	fd := int(file.Fd())
	if !term.IsTerminal(fd) {
		return func() {}, nil
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	return func() {
		_ = term.Restore(fd, state)
	}, nil
}

type liveShellClient struct {
	client *ssh.Client
}

func (c liveShellClient) Close() error {
	return c.client.Close()
}

func (c liveShellClient) NewSession() (shellSession, error) {
	session, err := c.client.NewSession()
	if err != nil {
		return nil, err
	}
	return liveShellSession{session: session}, nil
}

type liveShellSession struct {
	session *ssh.Session
}

func (s liveShellSession) Close() error {
	return s.session.Close()
}

func (s liveShellSession) RequestPty(term string, h, w int, modes ssh.TerminalModes) error {
	return s.session.RequestPty(term, h, w, modes)
}

func (s liveShellSession) Run(cmd string) error {
	return s.session.Run(cmd)
}

func (s liveShellSession) Signal(sig ssh.Signal) error {
	return s.session.Signal(sig)
}

func (s liveShellSession) SetStderr(w io.Writer) {
	s.session.Stderr = w
}

func (s liveShellSession) SetStdin(r io.Reader) {
	s.session.Stdin = r
}

func (s liveShellSession) SetStdout(w io.Writer) {
	s.session.Stdout = w
}

func (s liveShellSession) Shell() error {
	return s.session.Shell()
}

func (s liveShellSession) Wait() error {
	return s.session.Wait()
}

// NativeTerminalCommand подключает удалённый PTY напрямую к текущему терминалу.
// Он совместим с tea.ExecCommand, но не импортирует Bubble Tea в SSH-слой.
//
// Выход из нативного терминала — Ctrl+Q (перехватывается локально, не передаётся на сервер).
type NativeTerminalCommand struct {
	client shellClient
	cmd    string
	height int
	width  int

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	// exitKey — байт, при получении которого терминал принудительно закрывается.
	// По умолчанию 0x11 (Ctrl+Q).
	exitKey byte
}

// NewNativeShellCommand создаёт команду для интерактивного login shell.
func NewNativeShellCommand(cfg *config.ServerConfig, height, width int) *NativeTerminalCommand {
	return &NativeTerminalCommand{
		client:  liveShellClientFactory{cfg: cfg},
		height:  height,
		width:   width,
		exitKey: 0x11, // Ctrl+Q
	}
}

// NewNativeCommandWithManager создаёт команду для shell или remote command через общий SSH manager.
func NewNativeCommandWithManager(manager *Manager, cmd string, height, width int) *NativeTerminalCommand {
	return &NativeTerminalCommand{
		client:  managedShellClientFactory{manager: manager},
		cmd:     cmd,
		height:  height,
		width:   width,
		exitKey: 0x11, // Ctrl+Q
	}
}

func (c *NativeTerminalCommand) SetStdin(r io.Reader) {
	c.stdin = r
}

func (c *NativeTerminalCommand) SetStdout(w io.Writer) {
	c.stdout = w
}

func (c *NativeTerminalCommand) SetStderr(w io.Writer) {
	c.stderr = w
}

func (c *NativeTerminalCommand) Run() error {
	if c == nil || c.client == nil {
		return fmt.Errorf("native SSH terminal is not configured")
	}

	session, err := c.client.NewSession()
	if err != nil {
		_ = c.client.Close()
		return fmt.Errorf("не удалось создать native SSH-сессию: %w", err)
	}
	defer c.client.Close()
	defer session.Close()

	height, width := normalizePTYSize(c.height, c.width)
	if err := session.RequestPty("xterm-256color", height, width, defaultTerminalModes()); err != nil {
		return fmt.Errorf("не удалось запросить native PTY: %w", err)
	}

	// Оборачиваем stdin для перехвата Ctrl+Q (выход из терминала)
	exitCh := make(chan struct{}, 1)
	exitReader := newExitKeyReader(c.stdin, exitCh, c.exitKey)
	session.SetStdin(exitReader)
	if c.stdout != nil {
		session.SetStdout(c.stdout)
	}
	if c.stderr != nil {
		session.SetStderr(c.stderr)
	}

	restoreTerminal, err := enterTerminalRawMode(c.stdin)
	if err != nil {
		return fmt.Errorf("не удалось перевести локальный терминал в raw mode: %w", err)
	}
	defer restoreTerminal()

	if c.cmd != "" {
		if err := session.Run(c.cmd); err != nil {
			return fmt.Errorf("native remote command failed: %w", err)
		}
		return nil
	}

	if err := session.Shell(); err != nil {
		return fmt.Errorf("не удалось запустить native shell: %w", err)
	}

	// Горутина для реакции на Ctrl+Q — закрывает сессию для выхода
	go func() {
		<-exitCh
		_ = session.Close()
	}()

	if err := session.Wait(); err != nil {
		// Не считаем закрытие по Ctrl+Q ошибкой
		if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "SSH") {
			return nil
		}
		return fmt.Errorf("native shell завершился с ошибкой: %w", err)
	}
	return nil
}

func normalizePTYSize(height, width int) (int, int) {
	if height < 1 {
		height = 24
	}
	if width < 1 {
		width = 80
	}
	return height, width
}

type liveShellClientFactory struct {
	cfg *config.ServerConfig
}

func (c liveShellClientFactory) Close() error {
	return nil
}

func (c liveShellClientFactory) NewSession() (shellSession, error) {
	client, err := Connect(c.cfg)
	if err != nil {
		return nil, err
	}
	session, err := liveShellClient{client: client}.NewSession()
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	return ownedShellSession{
		shellSession: session,
		client:       liveShellClient{client: client},
	}, nil
}

type managedShellClientFactory struct {
	manager *Manager
}

func (c managedShellClientFactory) Close() error {
	return nil
}

func (c managedShellClientFactory) NewSession() (shellSession, error) {
	if c.manager == nil {
		return nil, fmt.Errorf("shared ssh manager is nil")
	}
	client, _, err := c.manager.checkedClient()
	if err != nil {
		return nil, err
	}
	return liveShellClient{client: client}.NewSession()
}

type ownedShellSession struct {
	shellSession
	client shellClient
}

func (s ownedShellSession) Close() error {
	sessionErr := s.shellSession.Close()
	clientErr := s.client.Close()
	if sessionErr != nil {
		return sessionErr
	}
	return clientErr
}

func defaultTerminalModes() ssh.TerminalModes {
	return ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
}

// exitKeyReader оборачивает io.Reader и посылает сигнал в exitCh при обнаружении
// заданного байта. Перехваченный байт НЕ передаётся дальше — он используется
// как управляющая последовательность (например, Ctrl+Q = 0x11 для выхода из терминала).
type exitKeyReader struct {
	reader  io.Reader
	exitCh  chan<- struct{}
	exitKey byte
}

func newExitKeyReader(reader io.Reader, exitCh chan<- struct{}, exitKey byte) *exitKeyReader {
	if reader == nil {
		reader = io.NopCloser(strings.NewReader(""))
	}
	return &exitKeyReader{reader: reader, exitCh: exitCh, exitKey: exitKey}
}

func (r *exitKeyReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		// Ищем exit-байт и удаляем его из потока
		j := 0
		for i := 0; i < n; i++ {
			if p[i] == r.exitKey {
				// Сигнализируем о выходе (неблокирующая отправка)
				select {
				case r.exitCh <- struct{}{}:
				default:
				}
				// Не копируем этот байт
			} else {
				if i != j {
					p[j] = p[i]
				}
				j++
			}
		}
		n = j
	}
	return n, err
}
