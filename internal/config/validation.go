package config

import (
	"fmt"
	"net"
	"regexp"
	"strings"
)

var hostnamePattern = regexp.MustCompile(`^([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])(\.([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]{0,61}[a-zA-Z0-9]))*$`)

// ValidateHost проверяет, что строка является валидным IP-адресом или доменным именем.
// Возвращает ошибку для пустых значений, строк с пробелами и опасными символами.
func ValidateHost(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("хост не может быть пустым")
	}

	// Проверяем как IP-адрес (IPv4 или IPv6)
	if ip := net.ParseIP(host); ip != nil {
		return nil
	}

	// Проверяем на опасные символы shell-инъекций
	if strings.ContainsAny(host, `"'`+"`"+`\$;&|<>`) {
		return fmt.Errorf("хост содержит недопустимые символы: %q", host)
	}

	// Хост не может начинаться с дефиса (может быть воспринят как флаг в некоторых контекстах)
	if strings.HasPrefix(host, "-") {
		return fmt.Errorf("хост не может начинаться с дефиса: %q", host)
	}

	// Проверяем как доменное имя
	if hostnamePattern.MatchString(host) {
		return nil
	}

	return fmt.Errorf("невалидный хост: %q (ожидается IP-адрес или доменное имя)", host)
}
