package knownhostsfixture

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh/knownhosts"
)

func TestPrepareStates(t *testing.T) {
	current, err := PublicKey(0x11)
	if err != nil {
		t.Fatalf("current key: %v", err)
	}
	host := "fixture.test:2222"
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2222}

	t.Run("missing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "known_hosts")
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatalf("seed path: %v", err)
		}
		if err := Prepare(path, host, current, Missing); err != nil {
			t.Fatalf("prepare missing: %v", err)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("stat missing = %v, want not exist", err)
		}
	})

	t.Run("known same", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nested", "known_hosts")
		if err := Prepare(path, host, current, KnownSame); err != nil {
			t.Fatalf("prepare known same: %v", err)
		}
		callback, err := knownhosts.New(path)
		if err != nil {
			t.Fatalf("parse known_hosts: %v", err)
		}
		if err := callback(host, addr, current); err != nil {
			t.Fatalf("same key rejected: %v", err)
		}
	})

	t.Run("known changed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "known_hosts")
		if err := Prepare(path, host, current, KnownChanged); err != nil {
			t.Fatalf("prepare known changed: %v", err)
		}
		callback, err := knownhosts.New(path)
		if err != nil {
			t.Fatalf("parse known_hosts: %v", err)
		}
		if err := callback(host, addr, current); err == nil {
			t.Fatal("changed key unexpectedly accepted")
		}
	})

	t.Run("known changed avoids identical deterministic key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "known_hosts")
		keyA5, err := PublicKey(0xA5)
		if err != nil {
			t.Fatalf("key A5: %v", err)
		}
		if err := Prepare(path, host, keyA5, KnownChanged); err != nil {
			t.Fatalf("prepare changed from A5: %v", err)
		}
		callback, err := knownhosts.New(path)
		if err != nil {
			t.Fatalf("parse known_hosts: %v", err)
		}
		if err := callback(host, addr, keyA5); err == nil {
			t.Fatal("fallback changed key unexpectedly matches current")
		}
	})

	t.Run("corrupt", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "nested", "known_hosts")
		if err := Prepare(path, host, current, Corrupt); err != nil {
			t.Fatalf("prepare corrupt: %v", err)
		}
		if _, err := knownhosts.New(path); err == nil {
			t.Fatal("corrupt known_hosts unexpectedly parsed")
		}
	})
}

func TestPrepareRejectsInvalidInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "known_hosts")
	if err := Prepare(path, "fixture.test", nil, KnownSame); err == nil {
		t.Fatal("expected nil key error")
	}
	if err := Prepare(path, "fixture.test", nil, KnownChanged); err == nil {
		t.Fatal("expected nil changed key error")
	}
	key, err := PublicKey(1)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if err := Prepare(path, "fixture.test", key, State(255)); err == nil {
		t.Fatal("expected unsupported state error")
	}
}
