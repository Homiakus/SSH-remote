package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/joho/godotenv"
)

const serversDir = "servers"
const knownHostsName = "known_hosts"

// ServerConfig хранит настройки SSH-подключения к серверу.
type ServerConfig struct {
	Name        string // Имя сервера (= имя файла без .env)
	Host        string // SSH_HOST
	Port        string // SSH_PORT
	User        string // SSH_USER
	AuthMethod  string // SSH_AUTH_METHOD: "password" или "key"
	Password    string // SSH_PASSWORD (если AuthMethod = "password")
	KeyPath     string // SSH_KEY_PATH (если AuthMethod = "key")
	Passphrase  string // SSH_KEY_PASSPHRASE (опционально, для ключа)
	Description string // SSH_DESCRIPTION
}

// EnsureServersDir создаёт папку servers/ если она не существует.
func EnsureServersDir() error {
	return os.MkdirAll(serversDir, 0755)
}

// KnownHostsPath возвращает путь к app-managed known_hosts файлу.
func KnownHostsPath() string {
	return filepath.Join(serversDir, knownHostsName)
}

// ListServers сканирует папку servers/ и возвращает список конфигов.
func ListServers() ([]ServerConfig, error) {
	if err := EnsureServersDir(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(serversDir)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать папку servers: %w", err)
	}

	var servers []ServerConfig
	var loadErrs []error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".env") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".env")
		cfg, err := LoadServer(name)
		if err != nil {
			loadErrs = append(loadErrs, fmt.Errorf("%s: %w", entry.Name(), err))
			continue
		}
		servers = append(servers, *cfg)
	}

	sort.Slice(servers, func(i, j int) bool {
		return servers[i].Name < servers[j].Name
	})

	return servers, errors.Join(loadErrs...)
}

// LoadServer загружает конфигурацию сервера из файла servers/<name>.env.
func LoadServer(name string) (*ServerConfig, error) {
	name = strings.TrimSpace(name)
	if err := ValidateServerName(name); err != nil {
		return nil, err
	}

	path := filepath.Join(serversDir, name+".env")

	envMap, err := godotenv.Read(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось загрузить %s: %w", path, err)
	}

	cfg := &ServerConfig{
		Name:        name,
		Host:        strings.TrimSpace(envMap["SSH_HOST"]),
		Port:        strings.TrimSpace(envMap["SSH_PORT"]),
		User:        strings.TrimSpace(envMap["SSH_USER"]),
		AuthMethod:  NormalizeAuthMethod(envMap["SSH_AUTH_METHOD"]),
		Password:    envMap["SSH_PASSWORD"],
		KeyPath:     strings.TrimSpace(envMap["SSH_KEY_PATH"]),
		Passphrase:  envMap["SSH_KEY_PASSPHRASE"],
		Description: strings.TrimSpace(envMap["SSH_DESCRIPTION"]),
	}

	if err := ValidateHost(cfg.Host); err != nil {
		return nil, fmt.Errorf("невалидный хост в %s: %w", path, err)
	}

	return cfg, nil
}

// SaveServer сохраняет конфигурацию сервера в файл servers/<name>.env.
func SaveServer(name string, cfg *ServerConfig) error {
	if err := EnsureServersDir(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if err := ValidateServerName(name); err != nil {
		return err
	}

	authMethod := NormalizeAuthMethod(cfg.AuthMethod)
	envMap := map[string]string{
		"SSH_HOST":        strings.TrimSpace(cfg.Host),
		"SSH_PORT":        strings.TrimSpace(cfg.Port),
		"SSH_USER":        strings.TrimSpace(cfg.User),
		"SSH_AUTH_METHOD": authMethod,
		"SSH_DESCRIPTION": cfg.Description,
	}

	if authMethod == AuthMethodPassword {
		envMap["SSH_PASSWORD"] = cfg.Password
	} else {
		envMap["SSH_KEY_PATH"] = cfg.KeyPath
		if cfg.Passphrase != "" {
			envMap["SSH_KEY_PASSPHRASE"] = cfg.Passphrase
		}
	}

	content, err := godotenv.Marshal(envMap)
	if err != nil {
		return fmt.Errorf("не удалось сериализовать конфиг: %w", err)
	}

	path := filepath.Join(serversDir, name+".env")
	return writeFileAtomic(path, []byte(content), 0600)
}

// DeleteServer удаляет файл конфигурации сервера.
func DeleteServer(name string) error {
	name = strings.TrimSpace(name)
	if err := ValidateServerName(name); err != nil {
		return err
	}
	path := filepath.Join(serversDir, name+".env")
	return os.Remove(path)
}

// ServerExists проверяет существование конфигурации сервера.
func ServerExists(name string) bool {
	name = strings.TrimSpace(name)
	if err := ValidateServerName(name); err != nil {
		return false
	}
	path := filepath.Join(serversDir, name+".env")
	_, err := os.Stat(path)
	return err == nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	temp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("не удалось создать временный файл для %s: %w", path, err)
	}
	tempName := temp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tempName)
		}
	}()

	if _, err = temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("не удалось записать временный файл %s: %w", tempName, err)
	}
	if err = temp.Chmod(perm); err != nil {
		_ = temp.Close()
		return fmt.Errorf("не удалось выставить права на временный файл %s: %w", tempName, err)
	}
	if err = temp.Close(); err != nil {
		return fmt.Errorf("не удалось закрыть временный файл %s: %w", tempName, err)
	}

	if err = os.Rename(tempName, path); err == nil {
		return nil
	}
	renameErr := err
	if _, statErr := os.Stat(path); statErr != nil {
		return fmt.Errorf("не удалось заменить %s: %w", path, renameErr)
	}
	backup := tempName + ".old"
	if backupErr := os.Rename(path, backup); backupErr != nil {
		return fmt.Errorf("не удалось заменить %s: rename: %v; backup: %w", path, renameErr, backupErr)
	}
	if err = os.Rename(tempName, path); err != nil {
		_ = os.Rename(backup, path)
		return fmt.Errorf("не удалось заменить %s после backup: %w", path, err)
	}
	_ = os.Remove(backup)
	return nil
}
