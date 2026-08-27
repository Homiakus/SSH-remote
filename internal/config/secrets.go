package config

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	AuthMethodPassword = "password"
	AuthMethodKey      = "key"
)

// NormalizeAuthMethod приводит метод аутентификации к поддерживаемому значению.
func NormalizeAuthMethod(method string) string {
	if strings.TrimSpace(method) == AuthMethodKey {
		return AuthMethodKey
	}
	return AuthMethodPassword
}

// PasswordWarnings возвращает предупреждения для SSH-пароля.
func PasswordWarnings(password string) []string {
	return secretWarnings("Пароль", password)
}

// PassphraseWarnings возвращает предупреждения для пароля ключа.
func PassphraseWarnings(passphrase string) []string {
	return secretWarnings("Пароль ключа", passphrase)
}

// SecretWarnings возвращает предупреждения для активного секрета конфигурации.
func SecretWarnings(cfg *ServerConfig) []string {
	if cfg == nil {
		return nil
	}

	switch NormalizeAuthMethod(cfg.AuthMethod) {
	case AuthMethodKey:
		return PassphraseWarnings(cfg.Passphrase)
	default:
		return PasswordWarnings(cfg.Password)
	}
}

func secretWarnings(label, value string) []string {
	if value == "" {
		return nil
	}

	if strings.TrimSpace(value) == "" {
		return []string{
			fmt.Sprintf("%s состоит только из пробелов. Приложение отправит его буквально.", label),
		}
	}

	warnings := make([]string, 0, 2)
	if hasLeadingWhitespace(value) {
		warnings = append(warnings, fmt.Sprintf("%s начинается с пробела. Приложение отправит его буквально.", label))
	}
	if hasTrailingWhitespace(value) {
		warnings = append(warnings, fmt.Sprintf("%s заканчивается пробелом. Приложение отправит его буквально.", label))
	}

	return warnings
}

func hasLeadingWhitespace(value string) bool {
	if value == "" {
		return false
	}

	runes := []rune(value)
	return unicode.IsSpace(runes[0])
}

func hasTrailingWhitespace(value string) bool {
	if value == "" {
		return false
	}

	runes := []rune(value)
	return unicode.IsSpace(runes[len(runes)-1])
}
