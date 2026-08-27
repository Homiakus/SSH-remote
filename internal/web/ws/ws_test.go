package ws

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSocketEcho(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := Upgrade(w, r)
		if err != nil {
			t.Errorf("Upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		for {
			op, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if op == OpText {
				_ = conn.WriteText("echo:" + string(msg))
			}
		}
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	// Connect via TCP to test handshake and framing
	conn, err := net.Dial("tcp", server.Listener.Addr().String())
	if err != nil {
		t.Fatalf("Dial failed: %v", err)
	}
	defer conn.Close()

	key := "dGhlIHNhbXBsZSBub25jZQ=="
	req := fmt.Sprintf("GET /ws HTTP/1.1\r\n"+
		"Host: %s\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\n"+
		"Sec-WebSocket-Version: 13\r\n\r\n", server.Listener.Addr().String(), key)

	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("Write upgrade request failed: %v", err)
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("Read upgrade response failed: %v", err)
	}
	resp := string(buf[:n])
	if !strings.Contains(resp, "101 Switching Protocols") {
		t.Fatalf("Expected 101 Switching Protocols, got:\n%s", resp)
	}

	h := sha1.New()
	h.Write([]byte(key + wsMagicGUID))
	expectedAccept := base64.StdEncoding.EncodeToString(h.Sum(nil))
	if !strings.Contains(resp, expectedAccept) {
		t.Fatalf("Expected accept key %s in response", expectedAccept)
	}

	// Send masked client frame: "hello"
	payload := []byte("hello")
	mask := []byte{1, 2, 3, 4}
	maskedPayload := make([]byte, len(payload))
	for i, b := range payload {
		maskedPayload[i] = b ^ mask[i%4]
	}

	frame := []byte{0x81, byte(0x80 | len(payload))}
	frame = append(frame, mask...)
	frame = append(frame, maskedPayload...)

	if _, err := conn.Write(frame); err != nil {
		t.Fatalf("Write frame failed: %v", err)
	}

	// Read server response frame header
	respHeader := make([]byte, 2)
	if _, err := io.ReadFull(conn, respHeader); err != nil {
		t.Fatalf("Read echo frame header failed: %v", err)
	}

	payloadLen := int(respHeader[1] & 0x7F)
	respPayload := make([]byte, payloadLen)
	if _, err := io.ReadFull(conn, respPayload); err != nil {
		t.Fatalf("Read echo frame payload failed: %v", err)
	}

	serverMsg := string(respPayload)
	if serverMsg != "echo:hello" {
		t.Fatalf("Expected echo:hello, got %q", serverMsg)
	}
}
