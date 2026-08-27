package ssh

import (
	"bytes"
	"io"
	"strings"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func TestNativeTerminalCommandConnectsPTYDirectly(t *testing.T) {
	stdin := strings.NewReader("input")
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	session := &fakeShellSession{}
	client := &fakeShellClient{session: session}

	cmd := &NativeTerminalCommand{
		client: client,
		cmd:    "top",
		height: 40,
		width:  120,
	}
	cmd.SetStdin(stdin)
	cmd.SetStdout(stdout)
	cmd.SetStderr(stderr)

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !session.requestPtyCalled {
		t.Fatal("expected PTY request")
	}
	if session.ptyTerm != "xterm-256color" {
		t.Fatalf("pty term = %q, want xterm-256color", session.ptyTerm)
	}
	if session.ptyHeight != 40 || session.ptyWidth != 120 {
		t.Fatalf("pty size = %dx%d, want 40x120", session.ptyHeight, session.ptyWidth)
	}
	if session.stdinReader == nil {
		t.Fatal("stdin reader was not set on session")
	}
	// Проверяем, что stdin обёрнут в exitKeyReader
	if ekr, ok := session.stdinReader.(*exitKeyReader); !ok {
		t.Fatal("stdin reader is not an exitKeyReader")
	} else if ekr.reader != stdin {
		t.Fatalf("exitKeyReader wraps %v, want %v", ekr.reader, stdin)
	}
	if session.stdoutWriter != stdout {
		t.Fatal("stdout writer was not passed through directly")
	}
	if session.stderrWriter != stderr {
		t.Fatal("stderr writer was not passed through directly")
	}
	if session.runCommand != "top" {
		t.Fatalf("run command = %q, want top", session.runCommand)
	}
	if session.shellCalled {
		t.Fatal("shell should not start for command mode")
	}
}

func TestNativeTerminalCommandEntersRawMode(t *testing.T) {
	oldRawMode := enterTerminalRawMode
	defer func() {
		enterTerminalRawMode = oldRawMode
	}()

	rawEntered := false
	rawRestored := false
	stdin := strings.NewReader("input")
	enterTerminalRawMode = func(got io.Reader) (func(), error) {
		if got != stdin {
			t.Fatal("raw mode should be requested for command stdin")
		}
		rawEntered = true
		return func() {
			rawRestored = true
		}, nil
	}

	session := &fakeShellSession{}
	client := &fakeShellClient{session: session}
	cmd := &NativeTerminalCommand{
		client: client,
		cmd:    "top",
		stdin:  stdin,
	}

	if err := cmd.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !rawEntered {
		t.Fatal("expected terminal raw mode")
	}
	if !rawRestored {
		t.Fatal("expected terminal raw mode to be restored")
	}
}

func TestNativeTerminalCommandStartsShellWhenCommandEmpty(t *testing.T) {
	session := &fakeShellSession{}
	client := &fakeShellClient{session: session}

	cmd := &NativeTerminalCommand{client: client}
	if err := cmd.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !session.shellCalled {
		t.Fatal("expected shell mode")
	}
	if session.runCommand != "" {
		t.Fatalf("run command = %q, want empty", session.runCommand)
	}
	if session.ptyHeight != 24 || session.ptyWidth != 80 {
		t.Fatalf("default pty size = %dx%d, want 24x80", session.ptyHeight, session.ptyWidth)
	}
}

type fakeShellClient struct {
	session     shellSession
	newErr      error
	closeCalled bool
}

func (c *fakeShellClient) Close() error {
	c.closeCalled = true
	return nil
}

func (c *fakeShellClient) NewSession() (shellSession, error) {
	if c.newErr != nil {
		return nil, c.newErr
	}
	return c.session, nil
}

type fakeShellSession struct {
	waitCh        <-chan struct{}
	waitErr       error
	requestPtyErr error
	runErr        error
	shellErr      error

	requestPtyCalled bool
	shellCalled      bool
	closeCalled      bool
	runCommand       string
	ptyTerm          string
	ptyHeight        int
	ptyWidth         int
	ptyModes         gossh.TerminalModes
	stdinReader      io.Reader
	stdoutWriter     io.Writer
	stderrWriter     io.Writer
}

func (s *fakeShellSession) Close() error {
	s.closeCalled = true
	return nil
}

func (s *fakeShellSession) RequestPty(term string, h, w int, modes gossh.TerminalModes) error {
	s.requestPtyCalled = true
	s.ptyTerm = term
	s.ptyHeight = h
	s.ptyWidth = w
	s.ptyModes = modes
	return s.requestPtyErr
}

func (s *fakeShellSession) Run(cmd string) error {
	s.runCommand = cmd
	return s.runErr
}

func (s *fakeShellSession) Signal(sig gossh.Signal) error {
	return nil
}

func (s *fakeShellSession) SetStderr(w io.Writer) {
	s.stderrWriter = w
}

func (s *fakeShellSession) SetStdin(r io.Reader) {
	s.stdinReader = r
}

func (s *fakeShellSession) SetStdout(w io.Writer) {
	s.stdoutWriter = w
}

func (s *fakeShellSession) Shell() error {
	s.shellCalled = true
	return s.shellErr
}

func (s *fakeShellSession) Wait() error {
	if s.waitCh != nil {
		<-s.waitCh
	}
	return s.waitErr
}
