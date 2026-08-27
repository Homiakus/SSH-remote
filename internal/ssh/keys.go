package ssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	gossh "golang.org/x/crypto/ssh"

	"sshpilot/internal/config"
)

// GeneratedKeyPair описывает пару ключей, созданную для конкретного сервера.
type GeneratedKeyPair struct {
	RelativePrivateKeyPath string
	PrivateKeyPath         string
	PublicKeyPath          string
	PublicAuthorizedKey    string
	EmbeddedPrivateKey     string // PEM data for vault embedding
}

// EnsureServerKeyPair создаёт или переиспользует ED25519 ключ для указанного сервера.
// При использовании vault-хранилища ключ встраивается прямо в vault.
// При первом вызове генерирует новую пару, при повторном — возвращает существующую.
func EnsureServerKeyPair(serverName string) (GeneratedKeyPair, error) {
	// Check if server already has an embedded key in vault.
	cfg, loadErr := config.LoadServer(serverName)
	if loadErr == nil && cfg.EmbeddedKey != "" {
		publicKey, err := authorizedPublicKeyFromPrivate([]byte(cfg.EmbeddedKey), serverName)
		if err != nil {
			return GeneratedKeyPair{}, fmt.Errorf("не удалось получить публичный ключ из embedded key: %w", err)
		}
		return GeneratedKeyPair{
			RelativePrivateKeyPath: config.EmbeddedKeyPathValue(),
			PrivateKeyPath:         "",
			PublicKeyPath:          "",
			PublicAuthorizedKey:    publicKey,
			EmbeddedPrivateKey:     cfg.EmbeddedKey,
		}, nil
	}

	// Also check for legacy file-based key.
	relativePrivatePath, err := config.DefaultKeyPath(serverName)
	if err != nil {
		return GeneratedKeyPair{}, err
	}
	relativePublicPath, err := config.DefaultPublicKeyPath(serverName)
	if err != nil {
		return GeneratedKeyPair{}, err
	}

	privatePath, err := config.ResolveKeyPath(relativePrivatePath)
	if err != nil {
		return GeneratedKeyPair{}, err
	}
	publicPath, err := config.ResolveKeyPath(relativePublicPath)
	if err != nil {
		return GeneratedKeyPair{}, err
	}

	var privateData []byte
	privateData, err = os.ReadFile(privatePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return GeneratedKeyPair{}, fmt.Errorf("не удалось прочитать приватный ключ %s: %w", privatePath, err)
		}

		// Generate new key pair
		privateData, err = generateOpenSSHPrivateKey(serverName)
		if err != nil {
			return GeneratedKeyPair{}, err
		}
		if err := config.EnsureKeysDir(); err == nil {
			_ = os.WriteFile(privatePath, privateData, 0600)
			pubKey, pubErr := authorizedPublicKeyFromPrivate(privateData, serverName)
			if pubErr == nil {
				_ = os.WriteFile(publicPath, []byte(pubKey), 0644)
			}
		}
	}

	publicKey, err := authorizedPublicKeyFromPrivate(privateData, serverName)
	if err != nil {
		return GeneratedKeyPair{}, fmt.Errorf("не удалось получить публичный ключ: %w", err)
	}

	return GeneratedKeyPair{
		RelativePrivateKeyPath: relativePrivatePath,
		PrivateKeyPath:         privatePath,
		PublicKeyPath:          publicPath,
		PublicAuthorizedKey:    publicKey,
		EmbeddedPrivateKey:     string(privateData),
	}, nil
}

// SetupGeneratedKeyAuth устанавливает сгенерированный публичный ключ на сервер,
// используя временный парольный доступ, и возвращает конфиг с SSH_AUTH_METHOD=key
// для сохранения. Автоматически проверяет вход по ключу после установки.
func SetupGeneratedKeyAuth(cfg *config.ServerConfig) (*config.ServerConfig, error) {
	if cfg == nil {
		return nil, fmt.Errorf("конфигурация сервера не задана")
	}
	if err := config.ValidateServerName(cfg.Name); err != nil {
		return nil, err
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("не указан временный пароль для установки SSH-ключа")
	}

	keyPair, err := EnsureServerKeyPair(cfg.Name)
	if err != nil {
		return nil, err
	}

	passwordCfg := cloneConfigForAuth(cfg)
	passwordCfg.AuthMethod = config.AuthMethodPassword
	client, err := Connect(passwordCfg)
	if err != nil {
		return nil, fmt.Errorf("не удалось подключиться по паролю для установки ключа: %w", err)
	}
	if err := InstallAuthorizedKey(client, keyPair.PublicAuthorizedKey); err != nil {
		_ = client.Close()
		return nil, err
	}
	if err := client.Close(); err != nil {
		return nil, fmt.Errorf("не удалось закрыть парольное SSH-соединение: %w", err)
	}

	keyCfg := cloneConfigForAuth(cfg)
	keyCfg.AuthMethod = config.AuthMethodKey
	keyCfg.Password = ""
	keyCfg.KeyPath = keyPair.RelativePrivateKeyPath
	keyCfg.EmbeddedKey = keyPair.EmbeddedPrivateKey
	keyCfg.Passphrase = ""
	if err := TestConnection(keyCfg); err != nil {
		return nil, fmt.Errorf("ключ установлен, но проверка входа по ключу не прошла: %w", err)
	}

	return keyCfg, nil
}

// InstallAuthorizedKey идемпотентно добавляет public key в ~/.ssh/authorized_keys.
func InstallAuthorizedKey(client *gossh.Client, publicKey string) error {
	publicKey = strings.TrimSpace(publicKey)
	if client == nil {
		return fmt.Errorf("SSH-клиент не задан")
	}
	if publicKey == "" {
		return fmt.Errorf("публичный ключ не задан")
	}

	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("не удалось создать сессию для установки ключа: %w", err)
	}
	defer session.Close()

	session.Stdin = strings.NewReader(authorizedKeyInstallScript(publicKey))
	output, err := session.CombinedOutput("bash -s")
	if err != nil {
		return fmt.Errorf("не удалось установить публичный ключ: %w\nВывод: %s", err, string(output))
	}
	return nil
}

func generateOpenSSHPrivateKey(serverName string) ([]byte, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("не удалось сгенерировать ED25519 ключ: %w", err)
	}

	block, err := gossh.MarshalPrivateKey(privateKey, "sshpilot-"+serverName)
	if err != nil {
		return nil, fmt.Errorf("не удалось сериализовать приватный ключ: %w", err)
	}
	return pem.EncodeToMemory(block), nil
}

func authorizedPublicKeyFromPrivate(privateData []byte, serverName string) (string, error) {
	signer, err := gossh.ParsePrivateKey(privateData)
	if err != nil {
		return "", err
	}

	key := strings.TrimSpace(string(gossh.MarshalAuthorizedKey(signer.PublicKey())))
	if key == "" {
		return "", fmt.Errorf("публичный ключ пуст")
	}
	return key + " sshpilot-" + safeAuthorizedKeyComment(serverName), nil
}

func safeAuthorizedKeyComment(serverName string) string {
	replacer := strings.NewReplacer(" ", "_", "\t", "_", "\r", "_", "\n", "_")
	return replacer.Replace(strings.TrimSpace(serverName))
}

func authorizedKeyInstallScript(publicKey string) string {
	quotedKey := shellQuote(strings.TrimSpace(publicKey))
	return strings.Join([]string{
		"set -e",
		"umask 077",
		`mkdir -p "$HOME/.ssh"`,
		`touch "$HOME/.ssh/authorized_keys"`,
		`chmod 700 "$HOME/.ssh"`,
		`chmod 600 "$HOME/.ssh/authorized_keys"`,
		"key=" + quotedKey,
		`if ! grep -qxF "$key" "$HOME/.ssh/authorized_keys"; then`,
		`  printf '%s\n' "$key" >> "$HOME/.ssh/authorized_keys"`,
		"fi",
		"",
	}, "\n")
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func cloneConfigForAuth(cfg *config.ServerConfig) *config.ServerConfig {
	return &config.ServerConfig{
		Name:        cfg.Name,
		Host:        cfg.Host,
		Port:        cfg.Port,
		User:        cfg.User,
		AuthMethod:  cfg.AuthMethod,
		Password:    cfg.Password,
		KeyPath:     cfg.KeyPath,
		EmbeddedKey: cfg.EmbeddedKey,
		Passphrase:  cfg.Passphrase,
		Description: cfg.Description,
	}
}
