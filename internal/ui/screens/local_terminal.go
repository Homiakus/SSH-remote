package screens

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type localCommandSpec struct {
	Display  string
	Name     string
	Args     []string
	UseShell bool
	Line     string
}

type localCommandResult struct {
	Display     string
	ExitCode    int
	Err         error
	Interrupted bool
}

type trackedTerminalSession interface {
	terminalSession
	CommandDone() <-chan localCommandResult
	RunCommand(localCommandSpec) error
	Running() bool
}

type localTerminalFactory func(workDir string, env []string) (trackedTerminalSession, error)

type localTerminalSession struct {
	workDir string
	env     []string

	output      chan string
	done        chan error
	commandDone chan localCommandResult

	mu        sync.Mutex
	active    *localActiveCommand
	closed    bool
	closeOnce sync.Once
	commands  sync.WaitGroup
}

type localActiveCommand struct {
	cmd         *exec.Cmd
	display     string
	interrupted bool
}

func startLocalTerminalSession(workDir string, env []string) (trackedTerminalSession, error) {
	if workDir == "" {
		return nil, fmt.Errorf("рабочая директория локального терминала не задана")
	}
	if info, err := os.Stat(workDir); err != nil {
		return nil, fmt.Errorf("не удалось открыть рабочую директорию %s: %w", workDir, err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("%s не является директорией", workDir)
	}

	return &localTerminalSession{
		workDir:     workDir,
		env:         append([]string{}, env...),
		output:      make(chan string, 64),
		done:        make(chan error, 1),
		commandDone: make(chan localCommandResult, 8),
	}, nil
}

func (s *localTerminalSession) Output() <-chan string {
	return s.output
}

func (s *localTerminalSession) Done() <-chan error {
	return s.done
}

func (s *localTerminalSession) CommandDone() <-chan localCommandResult {
	return s.commandDone
}

func (s *localTerminalSession) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.active != nil
}

func (s *localTerminalSession) Send(data string) error {
	return s.SendLine(data)
}

func (s *localTerminalSession) SendLine(line string) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	return s.RunCommand(localCommandSpec{
		Display:  line,
		UseShell: true,
		Line:     line,
	})
}

func (s *localTerminalSession) RunCommand(spec localCommandSpec) error {
	cmd, display, err := buildLocalCommand(spec)
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("не удалось подключить stdout локальной команды: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("не удалось подключить stderr локальной команды: %w", err)
	}

	cmd.Dir = s.workDir
	cmd.Env = append(os.Environ(), s.env...)

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("локальный терминал уже закрыт")
	}
	if s.active != nil {
		s.mu.Unlock()
		return fmt.Errorf("локальная команда уже выполняется")
	}
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("не удалось запустить локальную команду: %w", err)
	}

	active := &localActiveCommand{
		cmd:     cmd,
		display: display,
	}
	s.active = active
	s.commands.Add(1)
	s.mu.Unlock()

	s.emitStatusLine(fmt.Sprintf("[local] PID %d запущен: %s", cmd.Process.Pid, display))

	var readers sync.WaitGroup
	readers.Add(2)
	go s.readOutput(stdout, &readers)
	go s.readOutput(stderr, &readers)

	go func() {
		err := cmd.Wait()
		readers.Wait()
		s.finishCommand(active, err)
	}()

	return nil
}

func (s *localTerminalSession) Interrupt() error {
	s.mu.Lock()
	active := s.active
	if active == nil {
		s.mu.Unlock()
		return nil
	}
	active.interrupted = true
	s.mu.Unlock()

	if active.cmd != nil && active.cmd.Process != nil {
		s.emitStatusLine(fmt.Sprintf("[local] Отправляю прерывание PID %d...", active.cmd.Process.Pid))
	} else {
		s.emitStatusLine("[local] Отправляю прерывание активной команде...")
	}

	if err := killProcessTree(active.cmd); err != nil {
		return fmt.Errorf("не удалось прервать локальную команду: %w", err)
	}
	return nil
}

func (s *localTerminalSession) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		active := s.active
		s.mu.Unlock()

		if active != nil {
			active.interrupted = true
			_ = killProcessTree(active.cmd)
		}

		go func() {
			s.commands.Wait()
			close(s.output)
			close(s.commandDone)
			s.done <- nil
			close(s.done)
		}()
	})

	return nil
}

func (s *localTerminalSession) readOutput(reader io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()

	buf := make([]byte, 1024)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			s.output <- string(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (s *localTerminalSession) emitStatusLine(line string) {
	if line == "" {
		return
	}
	s.output <- line + "\n"
}

func (s *localTerminalSession) finishCommand(active *localActiveCommand, err error) {
	defer s.commands.Done()

	exitCode := exitCodeFromError(err)
	resultErr := err

	s.mu.Lock()
	if s.active == active {
		s.active = nil
	}
	interrupted := active.interrupted
	s.mu.Unlock()

	if interrupted {
		exitCode = 130
		resultErr = errors.New("команда прервана")
	}

	switch {
	case interrupted:
		s.emitStatusLine(fmt.Sprintf("[local] Команда прервана (код %d)", exitCode))
	case resultErr != nil:
		s.emitStatusLine(fmt.Sprintf("[local] Команда завершилась с ошибкой (код %d): %v", exitCode, resultErr))
	default:
		s.emitStatusLine(fmt.Sprintf("[local] Команда завершена успешно (код %d)", exitCode))
	}

	s.commandDone <- localCommandResult{
		Display:     active.display,
		ExitCode:    exitCode,
		Err:         resultErr,
		Interrupted: interrupted,
	}
}

func buildLocalCommand(spec localCommandSpec) (*exec.Cmd, string, error) {
	if spec.UseShell {
		line := strings.TrimSpace(spec.Line)
		if line == "" {
			return nil, "", fmt.Errorf("локальная команда пуста")
		}

		shell, args, err := resolveLocalShell(line)
		if err != nil {
			return nil, "", err
		}
		display := spec.Display
		if display == "" {
			display = line
		}
		return exec.Command(shell, args...), display, nil
	}

	if spec.Name == "" {
		return nil, "", fmt.Errorf("локальная команда не задана")
	}

	display := spec.Display
	if display == "" {
		display = strings.TrimSpace(strings.Join(append([]string{spec.Name}, spec.Args...), " "))
	}
	return exec.Command(spec.Name, spec.Args...), display, nil
}

func resolveLocalShell(line string) (string, []string, error) {
	if runtime.GOOS == "windows" {
		for _, candidate := range []string{"pwsh", "powershell"} {
			path, err := exec.LookPath(candidate)
			if err == nil {
				return path, []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", line}, nil
			}
		}
		return "", nil, fmt.Errorf("не найден PowerShell для локального терминала")
	}

	path, err := exec.LookPath("sh")
	if err != nil {
		return "", nil, fmt.Errorf("не найден shell для локального терминала: %w", err)
	}
	return path, []string{"-lc", line}, nil
}

func killProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	if runtime.GOOS == "windows" {
		kill := exec.Command("taskkill", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
		if err := kill.Run(); err != nil && !processAlreadyExited(err) {
			return err
		}
		return nil
	}

	if err := cmd.Process.Kill(); err != nil && !processAlreadyExited(err) {
		return err
	}
	return nil
}

func processAlreadyExited(err error) bool {
	if err == nil {
		return false
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already finished") || strings.Contains(msg, "not found") || strings.Contains(msg, "not running")
}

func exitCodeFromError(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
