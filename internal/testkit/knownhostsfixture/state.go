package knownhostsfixture

import (
	"crypto/ed25519"
	"fmt"
	"os"
	"path/filepath"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// State describes the known_hosts condition prepared by the fixture.
type State uint8

const (
	Missing State = iota
	KnownSame
	KnownChanged
	Corrupt
)

// PublicKey creates a deterministic Ed25519 public key from seedByte.
func PublicKey(seedByte byte) (gossh.PublicKey, error) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = seedByte
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("create known_hosts fixture signer: %w", err)
	}
	return signer.PublicKey(), nil
}

// Prepare materializes one known_hosts state for hostname and currentKey.
func Prepare(path, hostname string, currentKey gossh.PublicKey, state State) error {
	if (state == KnownSame || state == KnownChanged) && currentKey == nil {
		return fmt.Errorf("known_hosts fixture key is nil")
	}
	switch state {
	case Missing:
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	case KnownSame:
		return writeLine(path, hostname, currentKey)
	case KnownChanged:
		changedKey, err := PublicKey(0xA5)
		if err != nil {
			return err
		}
		if string(changedKey.Marshal()) == string(currentKey.Marshal()) {
			changedKey, err = PublicKey(0x5A)
			if err != nil {
				return err
			}
		}
		return writeLine(path, hostname, changedKey)
	case Corrupt:
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("corrupt known_hosts record\x00\n"), 0o600)
	default:
		return fmt.Errorf("unsupported known_hosts fixture state %d", state)
	}
}

func writeLine(path, hostname string, key gossh.PublicKey) error {
	if key == nil {
		return fmt.Errorf("known_hosts fixture key is nil")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key) + "\n"
	return os.WriteFile(path, []byte(line), 0o600)
}
