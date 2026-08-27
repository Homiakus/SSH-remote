package ssh

import (
	"errors"
	"testing"

	gossh "golang.org/x/crypto/ssh"

	"sshpilot/internal/config"
)

func TestManagerReusesResetAndClosesSharedClient(t *testing.T) {
	clients := []*gossh.Client{{}, {}}
	dialCount := 0
	closeCount := map[*gossh.Client]int{}

	manager := newManagerWithDeps(
		&config.ServerConfig{Host: "example.com", User: "root"},
		func(*config.ServerConfig) (*gossh.Client, error) {
			client := clients[dialCount]
			dialCount++
			return client, nil
		},
		func(*gossh.Client) error { return nil },
		func(client *gossh.Client) error {
			closeCount[client]++
			return nil
		},
	)

	first, err := manager.Client()
	if err != nil {
		t.Fatalf("first client: %v", err)
	}
	second, err := manager.Client()
	if err != nil {
		t.Fatalf("second client: %v", err)
	}
	if first != second {
		t.Fatal("expected manager to reuse the first shared client")
	}
	if dialCount != 1 {
		t.Fatalf("dial count = %d, want 1", dialCount)
	}

	if err := manager.Reset(); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if closeCount[first] != 1 {
		t.Fatalf("first client close count = %d, want 1", closeCount[first])
	}

	third, err := manager.Client()
	if err != nil {
		t.Fatalf("third client: %v", err)
	}
	if third == first {
		t.Fatal("expected reset to force a fresh client")
	}
	if dialCount != 2 {
		t.Fatalf("dial count after reset = %d, want 2", dialCount)
	}

	if err := manager.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if closeCount[third] != 1 {
		t.Fatalf("third client close count = %d, want 1", closeCount[third])
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if closeCount[third] != 1 {
		t.Fatalf("third client close count after second close = %d, want 1", closeCount[third])
	}

	if _, err := manager.Client(); !errors.Is(err, ErrManagerClosed) {
		t.Fatalf("client after close error = %v, want ErrManagerClosed", err)
	}
}

func TestManagerCheckReconnectsStaleClientOnce(t *testing.T) {
	staleClient := &gossh.Client{}
	freshClient := &gossh.Client{}
	dialCount := 0
	closeCount := map[*gossh.Client]int{}
	checkCount := map[*gossh.Client]int{}

	manager := newManagerWithDeps(
		&config.ServerConfig{Host: "example.com", User: "root"},
		func(*config.ServerConfig) (*gossh.Client, error) {
			dialCount++
			if dialCount == 1 {
				return staleClient, nil
			}
			return freshClient, nil
		},
		func(client *gossh.Client) error {
			checkCount[client]++
			if client == staleClient {
				return errors.New("не удалось создать сессию: EOF")
			}
			return nil
		},
		func(client *gossh.Client) error {
			closeCount[client]++
			return nil
		},
	)

	if err := manager.Check(); err != nil {
		t.Fatalf("check: %v", err)
	}
	if dialCount != 2 {
		t.Fatalf("dial count = %d, want 2", dialCount)
	}
	if closeCount[staleClient] != 1 {
		t.Fatalf("stale client close count = %d, want 1", closeCount[staleClient])
	}
	if checkCount[staleClient] != 1 || checkCount[freshClient] != 1 {
		t.Fatalf("check counts = %#v, want 1 attempt per client", checkCount)
	}

	client, err := manager.Client()
	if err != nil {
		t.Fatalf("client after reconnect: %v", err)
	}
	if client != freshClient {
		t.Fatal("expected manager to keep the reconnected client")
	}
}
