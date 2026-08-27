package ws

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
)

const (
	OpContinuation = 0x0
	OpText         = 0x1
	OpBinary       = 0x2
	OpClose        = 0x8
	OpPing         = 0x9
	OpPong         = 0xA

	wsMagicGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
)

// Conn represents an RFC 6455 WebSocket connection.
type Conn struct {
	rwc    net.Conn
	br     *bufio.Reader
	bw     *bufio.Writer
	mu     sync.Mutex
	closed bool
}

// Upgrade upgrades an HTTP connection to a WebSocket connection.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return nil, errors.New("missing or invalid Upgrade header")
	}
	if !strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		return nil, errors.New("missing or invalid Connection header")
	}

	clientKey := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Key"))
	if clientKey == "" {
		return nil, errors.New("missing Sec-WebSocket-Key header")
	}

	h := sha1.New()
	h.Write([]byte(clientKey + wsMagicGUID))
	acceptKey := base64.StdEncoding.EncodeToString(h.Sum(nil))

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("http.ResponseWriter does not support hijacking")
	}

	netConn, brw, err := hijacker.Hijack()
	if err != nil {
		return nil, fmt.Errorf("hijack failed: %w", err)
	}

	// Send HTTP 101 Switching Protocols response
	res := fmt.Sprintf("HTTP/1.1 101 Switching Protocols\r\n"+
		"Upgrade: websocket\r\n"+
		"Connection: Upgrade\r\n"+
		"Sec-WebSocket-Accept: %s\r\n\r\n", acceptKey)

	if _, err := netConn.Write([]byte(res)); err != nil {
		netConn.Close()
		return nil, fmt.Errorf("failed to write upgrade response: %w", err)
	}

	return &Conn{
		rwc: netConn,
		br:  brw.Reader,
		bw:  brw.Writer,
	}, nil
}

// ReadMessage reads a complete data frame or handles control frames.
func (c *Conn) ReadMessage() (messageType int, p []byte, err error) {
	for {
		header := make([]byte, 2)
		if _, err := io.ReadFull(c.br, header); err != nil {
			return 0, nil, err
		}

		fin := (header[0] & 0x80) != 0
		opcode := int(header[0] & 0x0F)
		masked := (header[1] & 0x80) != 0
		payloadLen := uint64(header[1] & 0x7F)

		if payloadLen == 126 {
			extended := make([]byte, 2)
			if _, err := io.ReadFull(c.br, extended); err != nil {
				return 0, nil, err
			}
			payloadLen = uint64(binary.BigEndian.Uint16(extended))
		} else if payloadLen == 127 {
			extended := make([]byte, 8)
			if _, err := io.ReadFull(c.br, extended); err != nil {
				return 0, nil, err
			}
			payloadLen = binary.BigEndian.Uint64(extended)
		}

		var maskKey []byte
		if masked {
			maskKey = make([]byte, 4)
			if _, err := io.ReadFull(c.br, maskKey); err != nil {
				return 0, nil, err
			}
		}

		payload := make([]byte, payloadLen)
		if payloadLen > 0 {
			if _, err := io.ReadFull(c.br, payload); err != nil {
				return 0, nil, err
			}
			if masked {
				for i := uint64(0); i < payloadLen; i++ {
					payload[i] ^= maskKey[i%4]
				}
			}
		}

		switch opcode {
		case OpPing:
			_ = c.WriteMessage(OpPong, payload)
			continue
		case OpPong:
			continue
		case OpClose:
			_ = c.WriteMessage(OpClose, nil)
			c.Close()
			return OpClose, payload, io.EOF
		case OpText, OpBinary, OpContinuation:
			if !fin {
				return opcode, payload, nil
			}
			return opcode, payload, nil
		default:
			return opcode, payload, nil
		}
	}
}

// WriteMessage sends a frame to the client.
func (c *Conn) WriteMessage(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return errors.New("websocket connection is closed")
	}

	length := len(data)
	var header []byte

	b0 := byte(0x80) | byte(messageType&0x0F) // FIN = 1

	if length <= 125 {
		header = []byte{b0, byte(length)}
	} else if length <= 65535 {
		header = []byte{b0, 126, 0, 0}
		binary.BigEndian.PutUint16(header[2:], uint16(length))
	} else {
		header = make([]byte, 10)
		header[0] = b0
		header[1] = 127
		binary.BigEndian.PutUint64(header[2:], uint64(length))
	}

	frame := make([]byte, len(header)+len(data))
	copy(frame, header)
	copy(frame[len(header):], data)

	if _, err := c.rwc.Write(frame); err != nil {
		return err
	}
	return nil
}

// WriteText sends a text message.
func (c *Conn) WriteText(text string) error {
	return c.WriteMessage(OpText, []byte(text))
}

// WriteBinary sends a binary message.
func (c *Conn) WriteBinary(data []byte) error {
	return c.WriteMessage(OpBinary, data)
}

// Close closes the WebSocket connection.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.rwc.Close()
}
