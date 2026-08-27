package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	keysDir         = "keys"
	generatedKeyExt = ".ed25519"
)

// ValidateServerName проверяет, что имя можно безопасно использовать как имя файла.
func ValidateServerName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("имя сервера не может быть пустым")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("имя сервера %q недопустимо", name)
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("имя сервера не должно содержать путь: %q", name)
	}
	if strings.ContainsRune(name, 0) || strings.ContainsAny(name, `<>:"|?*`) {
		return fmt.Errorf("имя сервера содержит недопустимые символы: %q", name)
	}
	if strings.HasSuffix(name, ".") {
		return fmt.Errorf("имя сервера не должно заканчиваться точкой: %q", name)
	}
	return nil
}

// EnsureKeysDir создаёт папку servers/keys для ключей приложения.
func EnsureKeysDir() error {
	if err := EnsureServersDir(); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(serversDir, keysDir), 0700)
}

// DefaultKeyPath возвращает путь к приватному ключу сервера относительно servers/.
func DefaultKeyPath(serverName string) (string, error) {
	serverName = strings.TrimSpace(serverName)
	if err := ValidateServerName(serverName); err != nil {
		return "", err
	}
	return filepath.ToSlash(filepath.Join(keysDir, serverName+generatedKeyExt)), nil
}

// DefaultPublicKeyPath возвращает путь к публичному ключу сервера относительно servers/.
func DefaultPublicKeyPath(serverName string) (string, error) {
	privatePath, err := DefaultKeyPath(serverName)
	if err != nil {
		return "", err
	}
	return privatePath + ".pub", nil
}

// ResolveKeyPath раскрывает SSH_KEY_PATH в локальный путь к приватному ключу.
//
// Абсолютные пути и пути от ~ используются как есть, остальные пути считаются
// относительными к папке servers/ и не могут выходить за её пределы.
func ResolveKeyPath(keyPath string) (string, error) {
	raw := strings.TrimSpace(keyPath)
	if raw == "" {
		return "", fmt.Errorf("не указан SSH_KEY_PATH")
	}

	if expanded, ok, err := expandHomePath(raw); ok || err != nil {
		if err != nil {
			return "", err
		}
		return filepath.Clean(expanded), nil
	}
	if filepath.IsAbs(raw) {
		return filepath.Clean(raw), nil
	}

	normalized := strings.ReplaceAll(raw, `\`, "/")
	clean := filepath.Clean(filepath.FromSlash(normalized))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("SSH_KEY_PATH не должен выходить за пределы папки servers: %q", keyPath)
	}

	return filepath.Join(serversDir, clean), nil
}

func expandHomePath(path string) (string, bool, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return "", false, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", true, fmt.Errorf("не удалось определить домашнюю директорию: %w", err)
	}
	if path == "~" {
		return home, true, nil
	}
	return filepath.Join(home, path[2:]), true, nil
}
