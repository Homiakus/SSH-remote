package ssh

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"sshpilot/internal/config"
)

func effectivePort(cfg *config.ServerConfig) string {
	port := strings.TrimSpace(cfg.Port)
	if port == "" {
		return "22"
	}
	return port
}

func networkAddress(cfg *config.ServerConfig) string {
	host := strings.TrimSpace(cfg.Host)
	port := strings.TrimSpace(cfg.Port)
	if port == "" {
		return host
	}
	return net.JoinHostPort(host, port)
}

func connectionTarget(cfg *config.ServerConfig) string {
	host := strings.TrimSpace(cfg.Host)
	user := strings.TrimSpace(cfg.User)
	port := strings.TrimSpace(cfg.Port)
	if port == "" {
		return fmt.Sprintf("%s@%s", user, host)
	}
	return fmt.Sprintf("%s@%s:%s", user, host, port)
}

// Connect устанавливает SSH-подключение к серверу по конфигурации.
// Использует TOFU (Trust On First Use) для проверки host key через
// собственный known_hosts-файл в servers/known_hosts.
// Криптографические алгоритмы задаются явной fail-closed политикой из
// crypto_policy.go; небезопасные алгоритмы x/crypto/ssh не разрешаются.
// Таймаут подключения: 20 секунд.
func Connect(cfg *config.ServerConfig) (*ssh.Client, error) {
	authMethods, err := buildAuthMethods(cfg)
	if err != nil {
		return nil, fmt.Errorf("ошибка аутентификации: %w", err)
	}

	algorithms, err := defaultCryptoAlgorithms()
	if err != nil {
		return nil, fmt.Errorf("ошибка криптографической политики SSH: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User:              strings.TrimSpace(cfg.User),
		Auth:              authMethods,
		HostKeyCallback:   tofuHostKeyCallback(config.KnownHostsPath()),
		Timeout:           20 * time.Second,
		HostKeyAlgorithms: algorithms.HostKeys,
		Config: ssh.Config{
			Ciphers:      algorithms.Ciphers,
			KeyExchanges: algorithms.KeyExchanges,
			MACs:         algorithms.MACs,
		},
	}

	addr := net.JoinHostPort(strings.TrimSpace(cfg.Host), effectivePort(cfg))
	dialer := net.Dialer{
		Timeout:   20 * time.Second,
		KeepAlive: 15 * time.Second,
	}

	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("не удалось установить TCP соединение с %s: %w", connectionTarget(cfg), err)
	}

	ncc, chans, reqs, err := ssh.NewClientConn(conn, addr, sshConfig)
	if err != nil {
		_ = conn.Close()
		// If transient EOF / connection reset on high-latency networks, retry once after short backoff
		if strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "reset") {
			time.Sleep(400 * time.Millisecond)
			connRetry, errRetry := dialer.Dial("tcp", addr)
			if errRetry == nil {
				ncc, chans, reqs, err = ssh.NewClientConn(connRetry, addr, sshConfig)
				if err != nil {
					_ = connRetry.Close()
				}
			}
		}
		if err != nil {
			return nil, fmt.Errorf("не удалось подключиться к %s — %w", connectionTarget(cfg), err)
		}
	}

	return ssh.NewClient(ncc, chans, reqs), nil
}

// TestConnection проверяет SSH-подключение: коннект + создание сессии + echo-тест.
// Возвращает nil если все три этапа успешны.
func TestConnection(cfg *config.ServerConfig) error {
	client, err := Connect(cfg)
	if err != nil {
		return err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("не удалось создать сессию: %w", err)
	}
	defer session.Close()

	_, err = session.Output("echo ok")
	if err != nil {
		return fmt.Errorf("не удалось выполнить тестовую команду: %w", err)
	}

	return nil
}

// ExecuteCommand выполняет одну команду на удалённом сервере через существующий SSH-клиент.
// Возвращает объединённый вывод stdout+stderr. При ошибке выполнения вывод всё равно возвращается.
func ExecuteCommand(client *ssh.Client, command string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("не удалось создать сессию: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput(command)
	if err != nil {
		return string(output), fmt.Errorf("ошибка выполнения команды: %w\nВывод: %s", err, string(output))
	}

	return string(output), nil
}

// ExecuteCommandContext выполняет команду с поддержкой отмены контекста.
func ExecuteCommandContext(ctx context.Context, client *ssh.Client, command string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("не удалось создать сессию: %w", err)
	}
	defer session.Close()

	var output []byte
	var runErr error
	done := make(chan struct{})
	go func() {
		output, runErr = session.CombinedOutput(command)
		close(done)
	}()

	select {
	case <-ctx.Done():
		_ = session.Close()
		<-done
		return string(output), ctx.Err()
	case <-done:
		if runErr != nil {
			return string(output), fmt.Errorf("ошибка выполнения команды: %w\nВывод: %s", runErr, string(output))
		}
		return string(output), nil
	}
}

// StreamCommand выполняет команду и передаёт stdout/stderr в указанные writer'ы.
func StreamCommand(client *ssh.Client, command string, stdout, stderr io.Writer) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("не удалось создать сессию: %w", err)
	}
	defer session.Close()

	session.Stdout = stdout
	session.Stderr = stderr
	if err := session.Run(command); err != nil {
		return fmt.Errorf("ошибка выполнения команды: %w", err)
	}
	return nil
}

// StreamCommandContext выполняет потоковую команду и завершает сессию при отмене.
func StreamCommandContext(ctx context.Context, client *ssh.Client, command string, stdout, stderr io.Writer) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("не удалось создать сессию: %w", err)
	}
	defer session.Close()

	session.Stdout = stdout
	session.Stderr = stderr
	done := make(chan error, 1)
	go func() { done <- session.Run(command) }()

	select {
	case <-ctx.Done():
		_ = session.Close()
		<-done
		return ctx.Err()
	case err := <-done:
		if err != nil {
			return fmt.Errorf("ошибка выполнения команды: %w", err)
		}
		return nil
	}
}

// OpenShell запускает интерактивный PTY shell.
func OpenShell(client *ssh.Client, stdin io.Reader, stdout, stderr io.Writer, term string, rows, cols int) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("не удалось создать сессию: %w", err)
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty(term, rows, cols, modes); err != nil {
		return fmt.Errorf("не удалось запросить PTY: %w", err)
	}

	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr
	if err := session.Shell(); err != nil {
		return fmt.Errorf("не удалось запустить shell: %w", err)
	}
	return session.Wait()
}

// OpenShellContext запускает shell и закрывает сессию при отмене контекста.
func OpenShellContext(ctx context.Context, client *ssh.Client, stdin io.Reader, stdout, stderr io.Writer, term string, rows, cols int) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("не удалось создать сессию: %w", err)
	}
	defer session.Close()

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := session.RequestPty(term, rows, cols, modes); err != nil {
		return fmt.Errorf("не удалось запросить PTY: %w", err)
	}

	session.Stdin = stdin
	session.Stdout = stdout
	session.Stderr = stderr
	if err := session.Shell(); err != nil {
		return fmt.Errorf("не удалось запустить shell: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- session.Wait() }()
	select {
	case <-ctx.Done():
		_ = session.Close()
		<-done
		return ctx.Err()
	case err := <-done:
		return err
	}
}

// ResizePTY обновляет размер терминала для активной SSH-сессии.
func ResizePTY(session *ssh.Session, rows, cols int) error {
	return session.WindowChange(rows, cols)
}

// RunCommandConcurrent безопасно собирает stdout/stderr параллельно.
func RunCommandConcurrent(client *ssh.Client, command string) (stdout, stderr string, err error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("не удалось создать сессию: %w", err)
	}
	defer session.Close()

	stdoutPipe, err := session.StdoutPipe()
	if err != nil {
		return "", "", err
	}
	stderrPipe, err := session.StderrPipe()
	if err != nil {
		return "", "", err
	}
	if err := session.Start(command); err != nil {
		return "", "", err
	}

	var stdoutBytes, stderrBytes []byte
	var stdoutErr, stderrErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		stdoutBytes, stdoutErr = io.ReadAll(stdoutPipe)
	}()
	go func() {
		defer wg.Done()
		stderrBytes, stderrErr = io.ReadAll(stderrPipe)
	}()
	waitErr := session.Wait()
	wg.Wait()

	if stdoutErr != nil {
		return string(stdoutBytes), string(stderrBytes), stdoutErr
	}
	if stderrErr != nil {
		return string(stdoutBytes), string(stderrBytes), stderrErr
	}
	if waitErr != nil {
		return string(stdoutBytes), string(stderrBytes), waitErr
	}
	return string(stdoutBytes), string(stderrBytes), nil
}

// PrivateKeyPermissionsTooOpen reports whether a private key file is too permissive.
func PrivateKeyPermissionsTooOpen(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().Perm()&0o077 != 0
}
