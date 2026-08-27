package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"
)

// MigrateEnvToVault scans servers/ for legacy .env files and converts each
// to an encrypted .vault file. Successfully migrated .env files are removed.
// If a .env has an associated key file in servers/keys/, the key is embedded
// into the vault and the file is removed.
//
// Returns the number of migrated servers and any non-fatal errors encountered.
func MigrateEnvToVault(mk *MasterKey) (migrated int, errs []error) {
	entries, err := os.ReadDir(serversDir)
	if err != nil {
		return 0, []error{fmt.Errorf("не удалось прочитать папку servers: %w", err)}
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".env") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".env")
		if err := migrateOneServer(mk, name); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		} else {
			migrated++
		}
	}

	return migrated, errs
}

func migrateOneServer(mk *MasterKey, name string) error {
	envPath := filepath.Join(serversDir, name+".env")
	vaultPath := filepath.Join(serversDir, name+vaultExt)

	// Skip if vault already exists.
	if _, err := os.Stat(vaultPath); err == nil {
		return nil
	}

	envMap, err := godotenv.Read(envPath)
	if err != nil {
		return fmt.Errorf("не удалось прочитать .env: %w", err)
	}

	vault := &ServerVault{
		Name:        name,
		Host:        strings.TrimSpace(envMap["SSH_HOST"]),
		Port:        strings.TrimSpace(envMap["SSH_PORT"]),
		User:        strings.TrimSpace(envMap["SSH_USER"]),
		AuthMethod:  NormalizeAuthMethod(envMap["SSH_AUTH_METHOD"]),
		Password:    envMap["SSH_PASSWORD"],
		Passphrase:  envMap["SSH_KEY_PASSPHRASE"],
		Description: strings.TrimSpace(envMap["SSH_DESCRIPTION"]),
	}

	// Try to embed the SSH key if it exists.
	keyPath := strings.TrimSpace(envMap["SSH_KEY_PATH"])
	if keyPath != "" && vault.AuthMethod == AuthMethodKey {
		if embeddedKey, keyFile, err := tryReadKeyFile(keyPath); err == nil {
			vault.PrivateKey = embeddedKey
			// Try to read public key too.
			pubPath := keyFile + ".pub"
			if pubData, err := os.ReadFile(pubPath); err == nil {
				vault.PublicKey = strings.TrimSpace(string(pubData))
			}
		} else {
			// Key file not found — store external path for reference.
			vault.ExternalKeyPath = keyPath
		}
	}

	data, err := EncryptVault(mk, vault)
	if err != nil {
		return fmt.Errorf("не удалось зашифровать: %w", err)
	}

	if err := writeFileAtomic(vaultPath, data, 0600); err != nil {
		return fmt.Errorf("не удалось записать .vault: %w", err)
	}

	// Remove the legacy .env file.
	if err := os.Remove(envPath); err != nil {
		return fmt.Errorf("vault создан, но не удалось удалить .env: %w", err)
	}

	// Remove embedded key files if they were successfully embedded.
	if vault.PrivateKey != "" && keyPath != "" {
		cleanupKeyFiles(keyPath)
	}

	return nil
}

// tryReadKeyFile attempts to resolve and read a key file.
// Returns the PEM data as string and the resolved file path.
func tryReadKeyFile(keyPath string) (string, string, error) {
	resolved, err := ResolveKeyPath(keyPath)
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", "", err
	}
	return string(data), resolved, nil
}

// cleanupKeyFiles removes the private and public key files after embedding.
func cleanupKeyFiles(keyPath string) {
	resolved, err := ResolveKeyPath(keyPath)
	if err != nil {
		return
	}
	_ = os.Remove(resolved)
	_ = os.Remove(resolved + ".pub")
}
