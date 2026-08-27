package ssh

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"sshpilot/internal/config"
)

var knownHostsMu sync.Mutex

type UnknownHostKeyError struct {
	Hostname       string
	KnownHostsPath string
	Fingerprint    string
}

func (e UnknownHostKeyError) Error() string {
	return fmt.Sprintf(
		"SSH host key для %s не найден в %s; проверьте fingerprint %s и добавьте ключ в known_hosts",
		e.Hostname,
		e.KnownHostsPath,
		e.Fingerprint,
	)
}

func tofuHostKeyCallback(path string) gossh.HostKeyCallback {
	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		knownHostsMu.Lock()
		defer knownHostsMu.Unlock()

		callback, err := knownhosts.New(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("не удалось прочитать known_hosts %s: %w", path, err)
			}
			return newUnknownHostKeyError(path, hostname, key)
		}

		if err := callback(hostname, remote, key); err == nil {
			return nil
		} else {
			var keyErr *knownhosts.KeyError
			if errors.As(err, &keyErr) && len(keyErr.Want) == 0 {
				return newUnknownHostKeyError(path, hostname, key)
			}
			return fmt.Errorf("SSH host key для %s не совпадает с %s: %w", hostname, path, err)
		}
	}
}

func newUnknownHostKeyError(path, hostname string, key gossh.PublicKey) UnknownHostKeyError {
	return UnknownHostKeyError{
		Hostname:       hostname,
		KnownHostsPath: path,
		Fingerprint:    gossh.FingerprintSHA256(key),
	}
}

func trustHostKey(path, hostname string, key gossh.PublicKey) error {
	knownHostsMu.Lock()
	defer knownHostsMu.Unlock()
	return trustHostKeyLocked(path, hostname, key)
}

func trustHostKeyLocked(path, hostname string, key gossh.PublicKey) error {
	if err := config.EnsureServersDir(); err != nil {
		return err
	}

	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key) + "\n"
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("не удалось открыть known_hosts %s: %w", path, err)
	}
	defer file.Close()

	if _, err := file.WriteString(line); err != nil {
		return fmt.Errorf("не удалось записать known_hosts %s: %w", path, err)
	}
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("не удалось выставить права на known_hosts %s: %w", path, err)
	}
	return nil
}
