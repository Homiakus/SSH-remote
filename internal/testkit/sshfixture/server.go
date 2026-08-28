package sshfixture

import (
	"crypto/ed25519"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"

	gossh "golang.org/x/crypto/ssh"
)

// Mode selects the protocol phase that the fixture rejects.
type Mode uint8

const (
	AcceptAll Mode = iota
	RejectHandshake
	RejectAuth
	RejectSession
)

// CommandResult controls one exec request returned by the fixture.
type CommandResult struct {
	Stdout     string
	Stderr     string
	ExitStatus uint32
}

// Options configures a deterministic in-process SSH server.
type Options struct {
	User     string
	Password string
	Mode     Mode
	Commands map[string]CommandResult
}

// Server is an in-process SSH protocol fixture with deterministic failure modes.
type Server struct {
	listener net.Listener
	config   *gossh.ServerConfig
	mode     Mode
	signer   gossh.Signer
	commands map[string]CommandResult

	mu        sync.Mutex
	conns     map[net.Conn]struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// Start listens on an ephemeral loopback port and starts the fixture.
func Start(opts Options) (*Server, error) {
	if strings.TrimSpace(opts.User) == "" {
		return nil, fmt.Errorf("fixture user is required")
	}
	if opts.Password == "" {
		return nil, fmt.Errorf("fixture password is required")
	}

	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		return nil, fmt.Errorf("create fixture signer: %w", err)
	}

	serverConfig := &gossh.ServerConfig{
		PasswordCallback: func(meta gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
			if opts.Mode == RejectAuth || meta.User() != opts.User || string(password) != opts.Password {
				return nil, fmt.Errorf("fixture authentication rejected")
			}
			return nil, nil
		},
	}
	serverConfig.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for SSH fixture: %w", err)
	}

	commands := make(map[string]CommandResult, len(opts.Commands))
	for command, result := range opts.Commands {
		commands[command] = result
	}

	server := &Server{
		listener: listener,
		config:   serverConfig,
		mode:     opts.Mode,
		signer:   signer,
		commands: commands,
		conns:    make(map[net.Conn]struct{}),
	}
	server.wg.Add(1)
	go server.serve()
	return server, nil
}

// Address returns the loopback host:port used by the fixture.
func (s *Server) Address() string {
	return s.listener.Addr().String()
}

// PublicKey returns the deterministic host key advertised by the fixture.
func (s *Server) PublicKey() gossh.PublicKey {
	return s.signer.PublicKey()
}

// Close terminates the listener and all accepted connections and waits for handlers to exit.
func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		closeErr = s.listener.Close()
		s.mu.Lock()
		for conn := range s.conns {
			_ = conn.Close()
		}
		s.mu.Unlock()
	})
	s.wg.Wait()
	return closeErr
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return
		}
		s.track(conn, true)
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer s.track(conn, false)
	defer conn.Close()

	if s.mode == RejectHandshake {
		_, _ = io.WriteString(conn, "NOT-SSH fixture\r\n")
		return
	}

	serverConn, channels, requests, err := gossh.NewServerConn(conn, s.config)
	if err != nil {
		return
	}
	defer serverConn.Close()
	go gossh.DiscardRequests(requests)

	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			_ = newChannel.Reject(gossh.UnknownChannelType, "fixture only supports session channels")
			continue
		}
		if s.mode == RejectSession {
			_ = newChannel.Reject(gossh.Prohibited, "fixture session rejected")
			continue
		}

		channel, sessionRequests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		s.wg.Add(1)
		go s.handleSession(channel, sessionRequests)
	}
}

func (s *Server) handleSession(channel gossh.Channel, requests <-chan *gossh.Request) {
	defer s.wg.Done()
	defer channel.Close()

	for request := range requests {
		if request.Type != "exec" {
			_ = request.Reply(false, nil)
			continue
		}

		var payload struct{ Command string }
		if err := gossh.Unmarshal(request.Payload, &payload); err != nil {
			_ = request.Reply(false, nil)
			return
		}
		_ = request.Reply(true, nil)

		result := s.commands[payload.Command]
		_, _ = io.WriteString(channel, result.Stdout)
		_, _ = io.WriteString(channel.Stderr(), result.Stderr)
		_, _ = channel.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{Status: result.ExitStatus}))
		return
	}
}

func (s *Server) track(conn net.Conn, add bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if add {
		s.conns[conn] = struct{}{}
		return
	}
	delete(s.conns, conn)
}
