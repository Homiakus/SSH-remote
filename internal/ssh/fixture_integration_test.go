package ssh

import (
	"net"
	"os"
	"strings"
	"testing"

	"sshpilot/internal/config"
	"sshpilot/internal/testkit/knownhostsfixture"
	"sshpilot/internal/testkit/sshfixture"
)

func TestConnectionWithDeterministicSSHFixture(t *testing.T) {
	t.Chdir(t.TempDir())
	server, err := sshfixture.Start(sshfixture.Options{
		User:     "pilot",
		Password: "secret",
		Commands: map[string]sshfixture.CommandResult{"echo ok": {Stdout: "ok\n"}},
	})
	if err != nil {
		t.Fatalf("start SSH fixture: %v", err)
	}
	defer server.Close()

	cfg := fixtureServerConfig(t, server, "pilot", "secret")
	if err := TestConnection(cfg); err != nil {
		t.Fatalf("TestConnection: %v", err)
	}
	data, err := os.ReadFile(config.KnownHostsPath())
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("known_hosts was not populated by TOFU")
	}
}

func TestConnectionFixtureFailurePhases(t *testing.T) {
	tests := []struct {
		name    string
		mode    sshfixture.Mode
		user    string
		pass    string
		wantErr string
	}{
		{name: "auth rejected", mode: sshfixture.RejectAuth, user: "pilot", pass: "secret", wantErr: "подключ"},
		{name: "wrong user", mode: sshfixture.AcceptAll, user: "other", pass: "secret", wantErr: "подключ"},
		{name: "wrong password", mode: sshfixture.AcceptAll, user: "pilot", pass: "wrong", wantErr: "подключ"},
		{name: "session rejected", mode: sshfixture.RejectSession, user: "pilot", pass: "secret", wantErr: "создать сессию"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			server, err := sshfixture.Start(sshfixture.Options{User: "pilot", Password: "secret", Mode: tt.mode})
			if err != nil {
				t.Fatalf("start SSH fixture: %v", err)
			}
			defer server.Close()

			cfg := fixtureServerConfig(t, server, tt.user, tt.pass)
			err = TestConnection(cfg)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("TestConnection error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestTOFUWithKnownHostsFixtureStates(t *testing.T) {
	server, err := sshfixture.Start(sshfixture.Options{User: "pilot", Password: "secret"})
	if err != nil {
		t.Fatalf("start SSH fixture: %v", err)
	}
	defer server.Close()

	addr, err := net.ResolveTCPAddr("tcp", server.Address())
	if err != nil {
		t.Fatalf("resolve fixture address: %v", err)
	}

	states := []struct {
		name    string
		state   knownhostsfixture.State
		wantErr bool
	}{
		{name: "missing auto trusts", state: knownhostsfixture.Missing},
		{name: "known same", state: knownhostsfixture.KnownSame},
		{name: "known changed", state: knownhostsfixture.KnownChanged, wantErr: true},
		{name: "corrupt", state: knownhostsfixture.Corrupt, wantErr: true},
	}
	for _, tt := range states {
		t.Run(tt.name, func(t *testing.T) {
			path := t.TempDir() + "/known_hosts"
			if err := knownhostsfixture.Prepare(path, server.Address(), server.PublicKey(), tt.state); err != nil {
				t.Fatalf("prepare known_hosts: %v", err)
			}
			err := tofuHostKeyCallback(path)(server.Address(), addr, server.PublicKey())
			if (err != nil) != tt.wantErr {
				t.Fatalf("callback error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.state == knownhostsfixture.Missing && err == nil {
				if info, statErr := os.Stat(path); statErr != nil || info.Size() == 0 {
					t.Fatalf("TOFU did not persist first key: info=%v err=%v", info, statErr)
				}
			}
		})
	}
}

func fixtureServerConfig(t *testing.T, server *sshfixture.Server, user, password string) *config.ServerConfig {
	t.Helper()
	host, port, err := net.SplitHostPort(server.Address())
	if err != nil {
		t.Fatalf("split fixture address: %v", err)
	}
	return &config.ServerConfig{
		Name:       "fixture",
		Host:       host,
		Port:       port,
		User:       user,
		AuthMethod: "password",
		Password:   password,
	}
}
