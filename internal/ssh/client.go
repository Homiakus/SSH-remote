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
// Таймаут подключения: 20 секунд.
func Connect(cfg *config.ServerConfig) (*ssh.Client, error) {
	authMethods, err := buildAuthMethods(cfg)
	if err != nil {
		return nil, fmt.Errorf("ошибка аутентификации: %w", err)
	}

	sshConfig := &ssh.ClientConfig{
		User:            strings.TrimSpace(cfg.User),
		Auth:            authMethods,
		HostKeyCallback: tofuHostKeyCallback(config.KnownHostsPath()),
		Timeout:         20 * time.Second,
		HostKeyAlgorithms: []string{
			ssh.KeyAlgoED25519,
			ssh.KeyAlgoECDSA256,
			ssh.KeyAlgoECDSA384,
			ssh.KeyAlgoECDSA521,
			ssh.KeyAlgoRSASHA512,
			ssh.KeyAlgoRSASHA256,
			ssh.KeyAlgoRSA,
		},
		Config: ssh.Config{
			Ciphers: []string{
				"chacha20-poly1305@openssh.com",
				"aes128-gcm@openssh.com",
				"aes256-gcm@openssh.com",
				"aes128-ctr",
				"aes192-ctr",
				"aes256-ctr",
				"aes128-cbc",
				"aes256-cbc",
			},
			KeyExchanges: []string{
				"curve25519-sha256",
				"curve25519-sha256@libssh.org",
				"ecdh-sha2-nistp256",
				"ecdh-sha2-nistp384",
				"ecdh-sha2-nistp521",
				"diffie-hellman-group14-sha256",
				"diffie-hellman-group14-sha1",
				"diffie-hellman-group-exchange-sha256",
			},
			MACs: []string{
				"hmac-sha2-256-etm@openssh.com",
				"hmac-sha2-512-etm@openssh.com",
				"hmac-sha2-256",
				"hmac-sha2-512",
				"hmac-sha1",
			},
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

// ExecuteScript выполняет скрипт на удалённом сервере и возвращает вывод.
func ExecuteScript(client *ssh.Client, scriptContent string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("не удалось создать сессию: %w", err)
	}
	defer session.Close()

	session.Stdin = strings.NewReader(scriptContent)

	output, err := session.CombinedOutput("bash -s")
	if err != nil {
		return string(output), fmt.Errorf("ошибка выполнения скрипта: %w\nВывод: %s", err, string(output))
	}

	return string(output), nil
}

// ScriptResult описывает результат потокового выполнения скрипта.
type ScriptResult struct {
	Output string
	Err    error
}

// ExecuteScriptStream выполняет скрипт с потоковой передачей вывода.
// ctx используется для отмены выполнения.
func ExecuteScriptStream(ctx context.Context, client *ssh.Client, scriptContent string, outputCh chan<- string, doneCh chan<- ScriptResult) {
	go func() {
		defer close(outputCh)
		defer close(doneCh)

		session, err := client.NewSession()
		if err != nil {
			doneCh <- ScriptResult{Err: fmt.Errorf("не удалось создать сессию: %w", err)}
			return
		}
		defer session.Close()

		session.Stdin = strings.NewReader(scriptContent)

		stdout, err := session.StdoutPipe()
		if err != nil {
			doneCh <- ScriptResult{Err: fmt.Errorf("не удалось получить stdout: %w", err)}
			return
		}

		stderr, err := session.StderrPipe()
		if err != nil {
			doneCh <- ScriptResult{Err: fmt.Errorf("не удалось получить stderr: %w", err)}
			return
		}

		if err := session.Start("bash -s"); err != nil {
			doneCh <- ScriptResult{Err: fmt.Errorf("не удалось запустить скрипт: %w", err)}
			return
		}

		// Читаем stdout и stderr в отдельных горутинах
		var fullOutput strings.Builder
		var fullOutputMu sync.Mutex
		readDone := make(chan struct{}, 2)

		readPipe := func(pipe io.Reader) {
			defer func() { readDone <- struct{}{} }()
			buf := make([]byte, 1024)
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				n, readErr := pipe.Read(buf)
				if n > 0 {
					line := string(buf[:n])
					fullOutputMu.Lock()
					fullOutput.WriteString(line)
					fullOutputMu.Unlock()
					select {
					case outputCh <- line:
					case <-ctx.Done():
						return
					}
				}
				if readErr != nil {
					return
				}
			}
		}

		go readPipe(stdout)
		go readPipe(stderr)

		// Ждём завершения обоих чтений
		<-readDone
		<-readDone

		err = session.Wait()
		fullOutputMu.Lock()
		output := fullOutput.String()
		fullOutputMu.Unlock()
		doneCh <- ScriptResult{
			Output: output,
			Err:    err,
		}
	}()
}

// buildAuthMethods формирует методы аутентификации для SSH-клиента.
func buildAuthMethods(cfg *config.ServerConfig) ([]ssh.AuthMethod, error) {
	switch config.NormalizeAuthMethod(cfg.AuthMethod) {
	case config.AuthMethodKey:
		return buildKeyAuth(cfg)
	default:
		return buildPasswordAuth(cfg.Password)
	}
}

func buildPasswordAuth(password string) ([]ssh.AuthMethod, error) {
	if password == "" {
		return nil, fmt.Errorf("не указан SSH_PASSWORD")
	}

	passwordAuth := ssh.Password(password)
	keyboardInteractive := ssh.KeyboardInteractive(
		func(user, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = password
			}
			return answers, nil
		},
	)

	return []ssh.AuthMethod{passwordAuth, keyboardInteractive}, nil
}

// buildKeyAuth создаёт аутентификацию по приватному ключу.
// Поддерживает как vault-embedded ключи, так и файловые.
func buildKeyAuth(cfg *config.ServerConfig) ([]ssh.AuthMethod, error) {
	var keyData []byte
	var err error

	if config.IsEmbeddedKey(cfg.KeyPath) && cfg.EmbeddedKey != "" {
		// Key is stored inside the encrypted vault.
		keyData = []byte(cfg.EmbeddedKey)
	} else {
		resolvedPath, pathErr := config.ResolveKeyPath(cfg.KeyPath)
		if pathErr != nil {
			return nil, pathErr
		}
		keyData, err = os.ReadFile(resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("не удалось прочитать ключ %s: %w", resolvedPath, err)
		}
	}

	var signer ssh.Signer
	if cfg.Passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(cfg.Passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey(keyData)
	}
	if err != nil {
		return nil, fmt.Errorf("не удалось распарсить ключ: %w", err)
	}

	return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
}
