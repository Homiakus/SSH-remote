package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Vault file format constants.
const (
	vaultMagic      = "SSHV"
	vaultVersion    = 1
	vaultExt        = ".vault"
	masterKeyFile   = ".master_key"
	masterKeyLen    = 32 // AES-256
	vaultSaltLen    = 32
	vaultNonceLen   = 12 // AES-GCM standard nonce
	vaultHeaderLen  = len(vaultMagic) + 1 + vaultSaltLen + vaultNonceLen // 4+1+32+12 = 49
	embeddedKeyPath = "__embedded__"
)

// ServerVault holds all server data that gets encrypted into a .vault file.
type ServerVault struct {
	Name        string `json:"name"`
	Host        string `json:"host"`
	Port        string `json:"port"`
	User        string `json:"user"`
	AuthMethod  string `json:"auth_method"`
	Description string `json:"description"`

	// Secrets
	Password   string `json:"password,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`

	// Embedded SSH key material (PEM-encoded private key, authorized_keys public key)
	PrivateKey string `json:"private_key,omitempty"`
	PublicKey  string `json:"public_key,omitempty"`

	// External key path (legacy / user-provided key outside vault)
	ExternalKeyPath string `json:"external_key_path,omitempty"`
}

// MasterKey wraps a 32-byte AES-256 key used to encrypt/decrypt vault files.
type MasterKey struct {
	raw [masterKeyLen]byte
}

// LoadOrCreateMasterKey loads the master key from servers/.master_key,
// or generates a new one if the file does not exist.
func LoadOrCreateMasterKey() (*MasterKey, error) {
	if err := EnsureServersDir(); err != nil {
		return nil, fmt.Errorf("не удалось создать папку servers: %w", err)
	}

	path := filepath.Join(serversDir, masterKeyFile)
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != masterKeyLen {
			return nil, fmt.Errorf("повреждён файл master key %s: ожидалось %d байт, получено %d", path, masterKeyLen, len(data))
		}
		mk := &MasterKey{}
		copy(mk.raw[:], data)
		return mk, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("не удалось прочитать master key %s: %w", path, err)
	}

	// Generate new master key.
	mk := &MasterKey{}
	if _, err := io.ReadFull(rand.Reader, mk.raw[:]); err != nil {
		return nil, fmt.Errorf("не удалось сгенерировать master key: %w", err)
	}

	if err := writeFileAtomic(path, mk.raw[:], 0600); err != nil {
		return nil, fmt.Errorf("не удалось сохранить master key %s: %w", path, err)
	}

	return mk, nil
}

// EncryptVault serialises a ServerVault to JSON, then encrypts it with
// AES-256-GCM and returns a self-contained binary blob with header.
func EncryptVault(key *MasterKey, vault *ServerVault) ([]byte, error) {
	if key == nil {
		return nil, errors.New("master key не задан")
	}
	if vault == nil {
		return nil, errors.New("vault не задан")
	}

	plaintext, err := json.Marshal(vault)
	if err != nil {
		return nil, fmt.Errorf("не удалось сериализовать vault: %w", err)
	}

	// Generate salt (reserved for future KDF use; currently written but not
	// used for key derivation since we use the raw master key directly).
	salt := make([]byte, vaultSaltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("не удалось сгенерировать salt: %w", err)
	}

	block, err := aes.NewCipher(key.raw[:])
	if err != nil {
		return nil, fmt.Errorf("не удалось создать AES-cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать AES-GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("не удалось сгенерировать nonce: %w", err)
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	// Assemble: magic(4) | version(1) | salt(32) | nonce(12) | ciphertext+tag(variable)
	buf := make([]byte, 0, vaultHeaderLen+len(ciphertext))
	buf = append(buf, []byte(vaultMagic)...)
	buf = append(buf, byte(vaultVersion))
	buf = append(buf, salt...)
	buf = append(buf, nonce...)
	buf = append(buf, ciphertext...)

	return buf, nil
}

// DecryptVault parses a vault binary blob, decrypts the payload with
// AES-256-GCM, and returns the deserialised ServerVault.
func DecryptVault(key *MasterKey, data []byte) (*ServerVault, error) {
	if key == nil {
		return nil, errors.New("master key не задан")
	}
	if len(data) < vaultHeaderLen {
		return nil, fmt.Errorf("файл vault слишком мал: %d байт", len(data))
	}

	// Parse header.
	magic := string(data[:4])
	if magic != vaultMagic {
		return nil, fmt.Errorf("неверный формат vault: magic=%q, ожидалось %q", magic, vaultMagic)
	}
	version := data[4]
	if version != vaultVersion {
		return nil, fmt.Errorf("неподдерживаемая версия vault: %d", version)
	}

	// salt is data[5:37] — reserved for future KDF use
	nonce := data[37:49]
	ciphertext := data[49:]

	if len(ciphertext) == 0 {
		return nil, errors.New("файл vault не содержит зашифрованных данных")
	}

	block, err := aes.NewCipher(key.raw[:])
	if err != nil {
		return nil, fmt.Errorf("не удалось создать AES-cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("не удалось создать AES-GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("не удалось расшифровать vault (неверный ключ или повреждённые данные): %w", err)
	}

	vault := &ServerVault{}
	if err := json.Unmarshal(plaintext, vault); err != nil {
		return nil, fmt.Errorf("не удалось десериализовать vault: %w", err)
	}

	return vault, nil
}

// vaultToConfig converts a decrypted ServerVault into a ServerConfig.
func vaultToConfig(v *ServerVault) *ServerConfig {
	cfg := &ServerConfig{
		Name:        v.Name,
		Host:        v.Host,
		Port:        v.Port,
		User:        v.User,
		AuthMethod:  NormalizeAuthMethod(v.AuthMethod),
		Password:    v.Password,
		Passphrase:  v.Passphrase,
		Description: v.Description,
	}

	if v.PrivateKey != "" {
		cfg.KeyPath = embeddedKeyPath
		cfg.EmbeddedKey = v.PrivateKey
	} else if v.ExternalKeyPath != "" {
		cfg.KeyPath = v.ExternalKeyPath
	}

	return cfg
}

// configToVault converts a ServerConfig into a ServerVault for encryption.
func configToVault(cfg *ServerConfig) *ServerVault {
	v := &ServerVault{
		Name:        cfg.Name,
		Host:        cfg.Host,
		Port:        cfg.Port,
		User:        cfg.User,
		AuthMethod:  NormalizeAuthMethod(cfg.AuthMethod),
		Password:    cfg.Password,
		Passphrase:  cfg.Passphrase,
		Description: cfg.Description,
	}

	if cfg.EmbeddedKey != "" {
		v.PrivateKey = cfg.EmbeddedKey
	} else if cfg.KeyPath != "" && cfg.KeyPath != embeddedKeyPath {
		v.ExternalKeyPath = cfg.KeyPath
	}

	return v
}

// IsEmbeddedKey reports whether the key path refers to a vault-embedded key.
func IsEmbeddedKey(keyPath string) bool {
	return keyPath == embeddedKeyPath
}

// EmbeddedKeyPathValue returns the sentinel path value used for vault-embedded keys.
func EmbeddedKeyPathValue() string {
	return embeddedKeyPath
}
