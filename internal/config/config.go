package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"sshpilot/internal/atomicfile"
)

var serversDir = "servers"

const knownHostsName = "known_hosts"

// masterKeyOnce holds the lazily-initialised master key for vault operations.
var globalMasterKey *MasterKey

// InitMasterKey loads or creates the master key. Must be called before
// any vault operations (LoadServer, SaveServer, etc.).
func InitMasterKey() error {
	mk, err := LoadOrCreateMasterKey()
	if err != nil {
		return err
	}
	globalMasterKey = mk
	return nil
}

func ensureMasterKey() (*MasterKey, error) {
	if globalMasterKey != nil {
		return globalMasterKey, nil
	}
	return nil, fmt.Errorf("master key не инициализирован: вызовите config.InitMasterKey()")
}

// ServerConfig хранит настройки SSH-подключения к серверу.
type ServerConfig struct {
	Name        string // Имя сервера (= имя файла без .vault)
	Host        string // SSH_HOST
	Port        string // SSH_PORT
	User        string // SSH_USER
	AuthMethod  string // SSH_AUTH_METHOD: "password" или "key"
	Password    string // SSH_PASSWORD (если AuthMethod = "password")
	KeyPath     string // SSH_KEY_PATH (если AuthMethod = "key")
	Passphrase  string // SSH_KEY_PASSPHRASE (опционально, для ключа)
	Description string // SSH_DESCRIPTION
	EmbeddedKey string // PEM-encoded private key stored inside vault
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
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), vaultExt) {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), vaultExt)
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

// LoadServer загружает конфигурацию сервера из зашифрованного файла servers/<name>.vault.
func LoadServer(name string) (*ServerConfig, error) {
	name = strings.TrimSpace(name)
	if err := ValidateServerName(name); err != nil {
		return nil, err
	}

	mk, err := ensureMasterKey()
	if err != nil {
		return nil, err
	}

	path := filepath.Join(serversDir, name+vaultExt)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось загрузить %s: %w", path, err)
	}

	vault, err := DecryptVault(mk, data)
	if err != nil {
		return nil, fmt.Errorf("не удалось расшифровать %s: %w", path, err)
	}

	vault.Name = name
	cfg := vaultToConfig(vault)

	if err := ValidateHost(cfg.Host); err != nil {
		return nil, fmt.Errorf("невалидный хост в %s: %w", path, err)
	}

	return cfg, nil
}

// SaveServer сохраняет конфигурацию сервера в зашифрованный файл servers/<name>.vault.
func SaveServer(name string, cfg *ServerConfig) error {
	if err := EnsureServersDir(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if err := ValidateServerName(name); err != nil {
		return err
	}

	mk, err := ensureMasterKey()
	if err != nil {
		return err
	}

	cfg.AuthMethod = NormalizeAuthMethod(cfg.AuthMethod)
	cfg.Name = name
	vault := configToVault(cfg)

	data, err := EncryptVault(mk, vault)
	if err != nil {
		return fmt.Errorf("не удалось зашифровать конфиг: %w", err)
	}

	path := filepath.Join(serversDir, name+vaultExt)
	return writeFileAtomic(path, data, 0600)
}

// DeleteServer удаляет файл конфигурации сервера.
func DeleteServer(name string) error {
	name = strings.TrimSpace(name)
	if err := ValidateServerName(name); err != nil {
		return err
	}
	path := filepath.Join(serversDir, name+vaultExt)
	return os.Remove(path)
}

// ServerExists проверяет существование конфигурации сервера.
func ServerExists(name string) bool {
	name = strings.TrimSpace(name)
	if err := ValidateServerName(name); err != nil {
		return false
	}
	path := filepath.Join(serversDir, name+vaultExt)
	_, err := os.Stat(path)
	return err == nil
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	return atomicfile.Write(path, data, perm)
}
