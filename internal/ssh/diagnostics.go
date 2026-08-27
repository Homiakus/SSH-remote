package ssh

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"sshpilot/internal/config"
)

type DiagnosticStage string

const (
	DiagnosticStageConfig    DiagnosticStage = "config"
	DiagnosticStageTCP       DiagnosticStage = "tcp"
	DiagnosticStageBanner    DiagnosticStage = "banner"
	DiagnosticStageAuth      DiagnosticStage = "auth"
	DiagnosticStageHandshake DiagnosticStage = "handshake"
	DiagnosticStageSession   DiagnosticStage = "session"
	DiagnosticStageSuccess   DiagnosticStage = "success"
)

// DiagnosticReport описывает результат пошаговой проверки SSH-подключения.
type DiagnosticReport struct {
	Target          string
	NetworkAddress  string
	TCPAddress      string
	EffectivePort   string
	UsedDefaultPort bool
	AuthMethod      string
	Warnings        []string
	AttemptedAuth   []string
	Banner          string
	Stage           DiagnosticStage
	Err             error
}

var attemptedMethodsPattern = regexp.MustCompile(`attempted methods \[([^\]]*)\]`)

// DiagnoseConnection выполняет пошаговую диагностику SSH-подключения:
// проверка конфига → TCP-соединение → SSH-banner → аутентификация → сессия.
// Возвращает DiagnosticReport с детальной информацией о каждом этапе.
func DiagnoseConnection(cfg *config.ServerConfig) DiagnosticReport {
	report := newDiagnosticReport(cfg)

	if err := validateConnectionConfig(cfg); err != nil {
		report.Stage = DiagnosticStageConfig
		report.Err = err
		return report
	}

	if err := checkTCP(report.TCPAddress); err != nil {
		report.Stage = DiagnosticStageTCP
		report.Err = err
		return report
	}

	banner, err := readSSHBanner(report.TCPAddress)
	report.Banner = banner
	if err != nil {
		report.Stage = DiagnosticStageBanner
		report.Err = err
		return report
	}

	client, err := Connect(cfg)
	if err != nil {
		report.Stage = classifyDiagnosticStage(err)
		report.Err = err
		report.AttemptedAuth = extractAttemptedAuthMethods(err)
		return report
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		report.Stage = DiagnosticStageSession
		report.Err = fmt.Errorf("не удалось создать сессию: %w", err)
		return report
	}
	defer session.Close()

	if _, err := session.Output("echo ok"); err != nil {
		report.Stage = DiagnosticStageSession
		report.Err = fmt.Errorf("не удалось выполнить тестовую команду: %w", err)
		return report
	}

	report.Stage = DiagnosticStageSuccess
	return report
}

// DiagnoseConnectionWithManager проверяет SSH-подключение через менеджер общего соединения.
// Использует существующее подключение Manager вместо создания нового.
func DiagnoseConnectionWithManager(manager *Manager) DiagnosticReport {
	if manager == nil {
		return DiagnosticReport{
			Stage: DiagnosticStageConfig,
			Err:   fmt.Errorf("shared ssh manager is nil"),
		}
	}

	report := newDiagnosticReport(&manager.cfg)

	if err := validateConnectionConfig(&manager.cfg); err != nil {
		report.Stage = DiagnosticStageConfig
		report.Err = err
		return report
	}

	if err := manager.Check(); err != nil {
		report.Stage = classifyDiagnosticStage(err)
		report.Err = err
		report.AttemptedAuth = extractAttemptedAuthMethods(err)
		return report
	}

	report.Stage = DiagnosticStageSuccess
	return report
}

func newDiagnosticReport(cfg *config.ServerConfig) DiagnosticReport {
	return DiagnosticReport{
		Target:          connectionTarget(cfg),
		NetworkAddress:  networkAddress(cfg),
		TCPAddress:      net.JoinHostPort(strings.TrimSpace(cfg.Host), effectivePort(cfg)),
		EffectivePort:   effectivePort(cfg),
		UsedDefaultPort: strings.TrimSpace(cfg.Port) == "",
		AuthMethod:      config.NormalizeAuthMethod(cfg.AuthMethod),
		Warnings:        config.SecretWarnings(cfg),
	}
}

// FormatDiagnosticReport превращает результат диагностики в текст для UI.
func FormatDiagnosticReport(report DiagnosticReport) string {
	lines := []string{diagnosticSummary(report)}
	if portLine := diagnosticPortLine(report); portLine != "" {
		lines = append(lines, portLine)
	}

	switch report.Stage {
	case DiagnosticStageConfig:
		lines = appendDiagnosticWarnings(lines, report.Warnings)
		lines = append(lines, "Конфигурация: "+report.Err.Error())
		lines = append(lines, "Итог: заполните обязательные поля подключения.")

	case DiagnosticStageTCP:
		lines = appendDiagnosticWarnings(lines, report.Warnings)
		lines = append(lines, fmt.Sprintf("TCP: не удалось подключиться к %s", report.NetworkAddress))
		lines = append(lines, "Детали: "+report.Err.Error())
		lines = append(lines, "Итог: хост недоступен или SSH-порт закрыт.")

	case DiagnosticStageBanner:
		lines = append(lines, fmt.Sprintf("TCP: соединение с %s установлено", report.NetworkAddress))
		lines = appendDiagnosticWarnings(lines, report.Warnings)
		lines = append(lines, "SSH: не удалось получить корректный SSH-banner.")
		lines = append(lines, "Детали: "+report.Err.Error())
		lines = append(lines, "Итог: порт открыт, но ответ не похож на рабочий SSH-сервер.")

	case DiagnosticStageAuth:
		lines = append(lines, fmt.Sprintf("TCP: соединение с %s установлено", report.NetworkAddress))
		if report.Banner != "" {
			lines = append(lines, "SSH: сервер отвечает как "+report.Banner)
		}
		lines = appendDiagnosticWarnings(lines, report.Warnings)
		lines = appendDiagnosticAuthAttempt(lines, report)
		lines = append(lines, diagnosticAuthFailureLine(report))
		lines = append(lines, "Детали: "+report.Err.Error())
		lines = append(lines, diagnosticAuthFailureConclusion(report))

	case DiagnosticStageHandshake:
		lines = append(lines, fmt.Sprintf("TCP: соединение с %s установлено", report.NetworkAddress))
		if report.Banner != "" {
			lines = append(lines, "SSH: сервер отвечает как "+report.Banner)
		}
		lines = appendDiagnosticWarnings(lines, report.Warnings)
		var hostKeyErr UnknownHostKeyError
		if errors.As(report.Err, &hostKeyErr) {
			lines = append(lines, "SSH: host key сервера не доверен.")
			lines = append(lines, "Fingerprint: "+hostKeyErr.Fingerprint)
			lines = append(lines, "Детали: "+report.Err.Error())
			lines = append(lines, "Итог: сверьте fingerprint по независимому каналу и добавьте ключ в "+hostKeyErr.KnownHostsPath+".")
		} else {
			lines = append(lines, "SSH: рукопожатие не завершилось.")
			lines = append(lines, "Детали: "+report.Err.Error())
			lines = append(lines, "Итог: проверьте совместимость SSH-настроек или ограничений сервера.")
		}

	case DiagnosticStageSession:
		lines = append(lines, fmt.Sprintf("TCP: соединение с %s установлено", report.NetworkAddress))
		if report.Banner != "" {
			lines = append(lines, "SSH: сервер отвечает как "+report.Banner)
		}
		lines = appendDiagnosticWarnings(lines, report.Warnings)
		lines = append(lines, "Аутентификация: успешно.")
		lines = append(lines, "Сессия: не удалось открыть рабочую SSH-сессию.")
		lines = append(lines, "Детали: "+report.Err.Error())
		lines = append(lines, "Итог: сеть и логин работают, проблема уже после входа.")

	case DiagnosticStageSuccess:
		lines = append(lines, fmt.Sprintf("TCP: соединение с %s установлено", report.NetworkAddress))
		if report.Banner != "" {
			lines = append(lines, "SSH: сервер отвечает как "+report.Banner)
		}
		lines = appendDiagnosticWarnings(lines, report.Warnings)
		lines = append(lines, "Аутентификация: успешно.")
		lines = append(lines, "Итог: подключение работает.")
	}

	return strings.Join(lines, "\n")
}

func validateConnectionConfig(cfg *config.ServerConfig) error {
	if strings.TrimSpace(cfg.Host) == "" {
		return fmt.Errorf("не задан SSH_HOST")
	}
	if err := config.ValidateHost(cfg.Host); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.User) == "" {
		return fmt.Errorf("не задан SSH_USER")
	}

	switch config.NormalizeAuthMethod(cfg.AuthMethod) {
	case config.AuthMethodKey:
		if _, err := config.ResolveKeyPath(cfg.KeyPath); err != nil {
			return err
		}
	default:
		if cfg.Password == "" {
			return fmt.Errorf("не задан SSH_PASSWORD")
		}
	}

	return nil
}

func checkTCP(address string) error {
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return err
	}
	return conn.Close()
}

func readSSHBanner(address string) (string, error) {
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return "", err
	}

	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		return "", err
	}

	banner := strings.TrimSpace(line)
	if !strings.HasPrefix(banner, "SSH-") {
		return banner, fmt.Errorf("получен не-SSH ответ: %q", banner)
	}

	return banner, nil
}

func classifyDiagnosticStage(err error) DiagnosticStage {
	msg := strings.ToLower(err.Error())

	switch {
	case strings.Contains(msg, "unable to authenticate"),
		strings.Contains(msg, "permission denied"):
		return DiagnosticStageAuth

	case strings.Contains(msg, "не удалось создать сессию"),
		strings.Contains(msg, "не удалось выполнить тестовую команду"):
		return DiagnosticStageSession

	case strings.Contains(msg, "dial tcp"),
		strings.Contains(msg, "connectex"),
		strings.Contains(msg, "no route to host"),
		strings.Contains(msg, "refused"),
		strings.Contains(msg, "timeout"):
		return DiagnosticStageTCP

	default:
		return DiagnosticStageHandshake
	}
}

func diagnosticSummary(report DiagnosticReport) string {
	switch report.Stage {
	case DiagnosticStageConfig:
		return fmt.Sprintf("Конфигурация %s неполная", report.Target)
	case DiagnosticStageTCP:
		return fmt.Sprintf("Не удается открыть TCP-соединение к %s", report.Target)
	case DiagnosticStageBanner:
		return fmt.Sprintf("Порт для %s открыт, но SSH не отвечает корректно", report.Target)
	case DiagnosticStageAuth:
		if report.AuthMethod == config.AuthMethodKey {
			return fmt.Sprintf("Сервер %s доступен, но не принимает SSH-ключ", report.Target)
		}
		return fmt.Sprintf("Сервер %s доступен, но не принимает логин/пароль", report.Target)
	case DiagnosticStageHandshake:
		return fmt.Sprintf("SSH-рукопожатие с %s не завершилось", report.Target)
	case DiagnosticStageSession:
		return fmt.Sprintf("Подключение к %s создано, но сессия не открылась", report.Target)
	default:
		return fmt.Sprintf("Подключение к %s работает", report.Target)
	}
}

func diagnosticPortLine(report DiagnosticReport) string {
	if report.UsedDefaultPort {
		return "Порт: не задан"
	}
	return fmt.Sprintf("Порт: %s (задан явно)", report.EffectivePort)
}

func appendDiagnosticWarnings(lines, warnings []string) []string {
	for _, warning := range warnings {
		lines = append(lines, "Локальная проверка: "+warning)
	}
	return lines
}

func diagnosticAuthFailureLine(report DiagnosticReport) string {
	if report.AuthMethod == config.AuthMethodKey {
		return "Аутентификация: сервер отклонил SSH-ключ или пароль ключа."
	}
	return "Аутентификация: сервер отклонил указанные учетные данные."
}

func diagnosticAuthFailureConclusion(report DiagnosticReport) string {
	if report.AuthMethod == config.AuthMethodKey {
		return "Итог: проверьте SSH_KEY_PATH/SSH_KEY_PASSPHRASE или настройки PubkeyAuthentication и PermitRootLogin на сервере."
	}
	return "Итог: проверьте SSH_USER/SSH_PASSWORD или настройки PasswordAuthentication и PermitRootLogin на сервере."
}

func appendDiagnosticAuthAttempt(lines []string, report DiagnosticReport) []string {
	if methods := diagnosticAttemptedAuthLine(report.AttemptedAuth); methods != "" {
		lines = append(lines, methods)
	}
	if outcome := diagnosticConfiguredAuthOutcome(report); outcome != "" {
		lines = append(lines, outcome)
	}
	return lines
}

func diagnosticAttemptedAuthLine(methods []string) string {
	if len(methods) == 0 {
		return ""
	}
	return fmt.Sprintf("Клиент: попробовал методы аутентификации %s.", strings.Join(methods, ", "))
}

func diagnosticConfiguredAuthOutcome(report DiagnosticReport) string {
	switch report.AuthMethod {
	case config.AuthMethodKey:
		if containsAuthMethod(report.AttemptedAuth, "publickey") {
			return "Клиент: SSH-ключ был отправлен серверу, но сервер его не принял."
		}
	case config.AuthMethodPassword:
		if containsAuthMethod(report.AttemptedAuth, "password") {
			return "Клиент: пароль был отправлен серверу, но сервер его не принял."
		}
		if containsAuthMethod(report.AttemptedAuth, "keyboard-interactive") {
			return "Клиент: пароль был отправлен через keyboard-interactive, но сервер его не принял."
		}
	}

	return ""
}

func extractAttemptedAuthMethods(err error) []string {
	if err == nil {
		return nil
	}

	matches := attemptedMethodsPattern.FindStringSubmatch(err.Error())
	if len(matches) != 2 {
		return nil
	}

	fields := strings.Fields(matches[1])
	if len(fields) == 0 {
		return nil
	}

	methods := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			methods = append(methods, field)
		}
	}

	return methods
}

func containsAuthMethod(methods []string, want string) bool {
	for _, method := range methods {
		if method == want {
			return true
		}
	}
	return false
}
