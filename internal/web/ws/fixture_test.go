package ws_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sshpilot/internal/testkit/wsfixture"
	"sshpilot/internal/web/ws"
)

type upgradeResult struct {
	conn *ws.Conn
	err  error
}

func upgradedFixture(t *testing.T) (*ws.Conn, *wsfixture.Fixture) {
	t.Helper()
	fixture := wsfixture.New()
	req := httptest.NewRequest(http.MethodGet, "http://fixture.test/ws", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "keep-alive, Upgrade")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")

	done := make(chan upgradeResult, 1)
	go func() {
		conn, err := ws.Upgrade(fixture, req)
		done <- upgradeResult{conn: conn, err: err}
	}()

	<-fixture.ServerWriteStarted()
	resp, err := fixture.ReadHTTPResponse(req)
	if err != nil {
		fixture.Close()
		t.Fatalf("read upgrade response: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		fixture.Close()
		t.Fatalf("status = %d, want 101", resp.StatusCode)
	}
	result := <-done
	if result.err != nil {
		fixture.Close()
		t.Fatalf("Upgrade error = %v", result.err)
	}
	return result.conn, fixture
}

func TestFixtureWriteMessageLengthBoundaries(t *testing.T) {
	for _, size := range []int{0, 1, 125, 126, 65535, 65536} {
		t.Run(strings.Repeat("x", min(size, 8)), func(t *testing.T) {
			conn, fixture := upgradedFixture(t)
			defer fixture.Close()
			defer conn.Close()

			payload := []byte(strings.Repeat("p", size))
			done := make(chan error, 1)
			go func() { done <- conn.WriteBinary(payload) }()
			<-fixture.ServerWriteStarted()

			opcode, got, err := fixture.ReadServerFrame()
			if err != nil {
				t.Fatalf("ReadServerFrame: %v", err)
			}
			if opcode != ws.OpBinary {
				t.Fatalf("opcode = %d, want %d", opcode, ws.OpBinary)
			}
			if string(got) != string(payload) {
				t.Fatalf("payload mismatch: got %d bytes, want %d", len(got), len(payload))
			}
			if err := <-done; err != nil {
				t.Fatalf("WriteBinary: %v", err)
			}
		})
	}
}

func TestFixtureBackpressureAndDisconnect(t *testing.T) {
	conn, fixture := upgradedFixture(t)
	defer fixture.Close()

	writeDone := make(chan error, 1)
	go func() { writeDone <- conn.WriteText("blocked") }()
	<-fixture.ServerWriteStarted()
	select {
	case err := <-writeDone:
		t.Fatalf("write completed before peer read: %v", err)
	default:
	}

	opcode, payload, err := fixture.ReadServerFrame()
	if err != nil {
		t.Fatalf("read blocked frame: %v", err)
	}
	if opcode != ws.OpText || string(payload) != "blocked" {
		t.Fatalf("frame = opcode %d payload %q", opcode, payload)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write after peer read: %v", err)
	}

	readDone := make(chan error, 1)
	go func() {
		_, _, err := conn.ReadMessage()
		readDone <- err
	}()
	if err := fixture.CloseClient(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	if err := <-readDone; err == nil {
		t.Fatal("ReadMessage returned nil after disconnect")
	}
	if err := conn.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("close server conn: %v", err)
	}
}

func TestFixturePingPongThenText(t *testing.T) {
	conn, fixture := upgradedFixture(t)
	defer fixture.Close()
	defer conn.Close()

	readDone := make(chan struct {
		opcode int
		data   []byte
		err    error
	}, 1)
	go func() {
		opcode, data, err := conn.ReadMessage()
		readDone <- struct {
			opcode int
			data   []byte
			err    error
		}{opcode: opcode, data: data, err: err}
	}()

	pingDone := make(chan error, 1)
	go func() { pingDone <- fixture.WriteClientFrame(ws.OpPing, []byte("p")) }()
	<-fixture.ServerWriteStarted()
	pongOp, pong, err := fixture.ReadServerFrame()
	if err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pongOp != ws.OpPong || string(pong) != "p" {
		t.Fatalf("pong = opcode %d payload %q", pongOp, pong)
	}
	if err := <-pingDone; err != nil {
		t.Fatalf("write ping: %v", err)
	}

	textDone := make(chan error, 1)
	go func() { textDone <- fixture.WriteClientFrame(ws.OpText, []byte("hello")) }()
	result := <-readDone
	if result.err != nil || result.opcode != ws.OpText || string(result.data) != "hello" {
		t.Fatalf("ReadMessage = opcode %d payload %q err %v", result.opcode, result.data, result.err)
	}
	if err := <-textDone; err != nil {
		t.Fatalf("write text: %v", err)
	}
}

func TestUpgradeRejectsInvalidHandshakeDeterministically(t *testing.T) {
	tests := []struct {
		name    string
		headers map[string]string
	}{
		{name: "missing upgrade", headers: map[string]string{"Connection": "Upgrade", "Sec-WebSocket-Key": "key"}},
		{name: "missing connection", headers: map[string]string{"Upgrade": "websocket", "Sec-WebSocket-Key": "key"}},
		{name: "missing key", headers: map[string]string{"Upgrade": "websocket", "Connection": "Upgrade"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://fixture/ws", nil)
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}
			if _, err := ws.Upgrade(httptest.NewRecorder(), req); err == nil {
				t.Fatal("Upgrade unexpectedly succeeded")
			}
		})
	}
}
