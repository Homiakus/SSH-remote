package ssh

import (
	"strings"
	"testing"
)

func TestFormatDiagnosticReportForAuthFailure(t *testing.T) {
	report := DiagnosticReport{
		Target:          "root@185.72.144.39",
		NetworkAddress:  "185.72.144.39",
		TCPAddress:      "185.72.144.39:22",
		EffectivePort:   "22",
		UsedDefaultPort: true,
		AuthMethod:      "password",
		Warnings: []string{
			"Пароль заканчивается пробелом. Приложение отправит его буквально.",
		},
		AttemptedAuth: []string{"none", "password"},
		Banner:        "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.15",
		Stage:         DiagnosticStageAuth,
		Err:           errString("ssh: unable to authenticate"),
	}

	text := FormatDiagnosticReport(report)
	for _, want := range []string{
		"Сервер root@185.72.144.39 доступен, но не принимает логин/пароль",
		"Порт: не задан",
		"TCP: соединение с 185.72.144.39 установлено",
		"SSH: сервер отвечает как SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.15",
		"Локальная проверка: Пароль заканчивается пробелом. Приложение отправит его буквально.",
		"Клиент: попробовал методы аутентификации none, password.",
		"Клиент: пароль был отправлен серверу, но сервер его не принял.",
		"Итог: проверьте SSH_USER/SSH_PASSWORD",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report does not contain %q:\n%s", want, text)
		}
	}

	if strings.Index(text, "Локальная проверка:") > strings.Index(text, "Аутентификация:") {
		t.Fatalf("local warning should appear before auth failure:\n%s", text)
	}
	if strings.Index(text, "Клиент: пароль был отправлен серверу") > strings.Index(text, "Аутентификация:") {
		t.Fatalf("client auth outcome should appear before auth failure:\n%s", text)
	}
}

func TestFormatDiagnosticReportForKeyAuthFailure(t *testing.T) {
	report := DiagnosticReport{
		Target:          "root@185.72.144.39",
		NetworkAddress:  "185.72.144.39",
		TCPAddress:      "185.72.144.39:22",
		EffectivePort:   "22",
		UsedDefaultPort: true,
		AuthMethod:      "key",
		Warnings: []string{
			"Пароль ключа начинается с пробела. Приложение отправит его буквально.",
		},
		AttemptedAuth: []string{"none", "publickey"},
		Banner:        "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.15",
		Stage:         DiagnosticStageAuth,
		Err:           errString("ssh: unable to authenticate"),
	}

	text := FormatDiagnosticReport(report)
	for _, want := range []string{
		"Сервер root@185.72.144.39 доступен, но не принимает SSH-ключ",
		"Локальная проверка: Пароль ключа начинается с пробела. Приложение отправит его буквально.",
		"Клиент: попробовал методы аутентификации none, publickey.",
		"Клиент: SSH-ключ был отправлен серверу, но сервер его не принял.",
		"Итог: проверьте SSH_KEY_PATH/SSH_KEY_PASSPHRASE",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report does not contain %q:\n%s", want, text)
		}
	}

	if strings.Contains(text, "SSH_PASSWORD") {
		t.Fatalf("key auth report should not mention SSH_PASSWORD:\n%s", text)
	}
}

func TestFormatDiagnosticReportForSuccess(t *testing.T) {
	report := DiagnosticReport{
		Target:          "root@185.72.144.39",
		EffectivePort:   "2222",
		UsedDefaultPort: false,
		NetworkAddress:  "185.72.144.39:2222",
		Banner:          "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.15",
		Stage:           DiagnosticStageSuccess,
	}

	text := FormatDiagnosticReport(report)
	for _, want := range []string{
		"Подключение к root@185.72.144.39 работает",
		"Порт: 2222 (задан явно)",
		"Аутентификация: успешно.",
		"Итог: подключение работает.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report does not contain %q:\n%s", want, text)
		}
	}
}

func TestFormatDiagnosticReportForUnknownHostKey(t *testing.T) {
	report := DiagnosticReport{
		Target:         "root@example.test:2222",
		NetworkAddress: "example.test:2222",
		Banner:         "SSH-2.0-OpenSSH_9.6",
		Stage:          DiagnosticStageHandshake,
		Err: UnknownHostKeyError{
			Hostname:       "example.test:2222",
			KnownHostsPath: "servers/known_hosts",
			Fingerprint:    "SHA256:test",
		},
	}

	text := FormatDiagnosticReport(report)
	for _, want := range []string{
		"SSH: host key сервера не доверен.",
		"Fingerprint: SHA256:test",
		"сверьте fingerprint",
		"servers/known_hosts",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("report does not contain %q:\n%s", want, text)
		}
	}
}

type errString string

func (e errString) Error() string {
	return string(e)
}

func TestExtractAttemptedAuthMethods(t *testing.T) {
	err := errString("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password], no supported methods remain")

	got := extractAttemptedAuthMethods(err)
	want := []string{"none", "password"}
	if !slicesEqual(got, want) {
		t.Fatalf("extractAttemptedAuthMethods() = %#v, want %#v", got, want)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
