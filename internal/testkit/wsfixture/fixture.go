package wsfixture

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
)

// Fixture is an in-memory HTTP hijack/WebSocket transport built on net.Pipe.
// Writes made by the server side are observable before they block, allowing
// tests to assert backpressure without sleeps or scheduler guesses.
type Fixture struct {
	client net.Conn
	server net.Conn
	reader *bufio.Reader
	header http.Header

	writeStarted chan struct{}
	closeOnce    sync.Once
}

// New creates a connected client/server transport suitable for http.Hijacker.
func New() *Fixture {
	client, server := net.Pipe()
	return &Fixture{
		client:       client,
		server:       server,
		reader:       bufio.NewReader(client),
		header:       make(http.Header),
		writeStarted: make(chan struct{}, 16),
	}
}

// Header implements http.ResponseWriter.
func (f *Fixture) Header() http.Header { return f.header }

// Write implements http.ResponseWriter. Upgrade paths should write through the hijacked connection.
func (f *Fixture) Write([]byte) (int, error) {
	return 0, fmt.Errorf("wsfixture: ResponseWriter.Write is not supported after hijack")
}

// WriteHeader implements http.ResponseWriter.
func (f *Fixture) WriteHeader(int) {}

// Hijack implements http.Hijacker.
func (f *Fixture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	observed := &observedConn{Conn: f.server, started: f.writeStarted}
	return observed, bufio.NewReadWriter(bufio.NewReader(observed), bufio.NewWriter(observed)), nil
}

// ServerWriteStarted is signaled immediately before each server-side net.Conn.Write.
// The signal happens before the write can block on the in-memory peer.
func (f *Fixture) ServerWriteStarted() <-chan struct{} { return f.writeStarted }

// ReadHTTPResponse reads an HTTP response from the client side.
func (f *Fixture) ReadHTTPResponse(req *http.Request) (*http.Response, error) {
	return http.ReadResponse(f.reader, req)
}

// WriteClientFrame writes one masked client frame.
func (f *Fixture) WriteClientFrame(opcode int, payload []byte) error {
	const maskBit = byte(0x80)
	mask := [4]byte{1, 2, 3, 4}
	length := len(payload)

	header := []byte{0x80 | byte(opcode&0x0f)}
	switch {
	case length <= 125:
		header = append(header, maskBit|byte(length))
	case length <= 65535:
		header = append(header, maskBit|126, 0, 0)
		binary.BigEndian.PutUint16(header[2:], uint16(length))
	default:
		header = append(header, maskBit|127, 0, 0, 0, 0, 0, 0, 0, 0)
		binary.BigEndian.PutUint64(header[2:], uint64(length))
	}
	header = append(header, mask[:]...)

	masked := make([]byte, length)
	for i, b := range payload {
		masked[i] = b ^ mask[i%len(mask)]
	}
	frame := append(header, masked...)
	_, err := f.client.Write(frame)
	return err
}

// ReadClientFrame reads one masked client frame from the server side.
func (f *Fixture) ReadClientFrame() (opcode int, payload []byte, err error) {
	reader := bufio.NewReader(f.server)
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return 0, nil, err
	}
	opcode = int(header[0] & 0x0f)
	masked := header[1]&0x80 != 0
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		extended := make([]byte, 2)
		if _, err := io.ReadFull(reader, extended); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(extended))
	case 127:
		extended := make([]byte, 8)
		if _, err := io.ReadFull(reader, extended); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(extended)
	}
	if !masked {
		return 0, nil, fmt.Errorf("wsfixture: client frame is not masked")
	}
	mask := make([]byte, 4)
	if _, err := io.ReadFull(reader, mask); err != nil {
		return 0, nil, err
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%len(mask)]
	}
	return opcode, payload, nil
}

// ReadServerFrame reads one unmasked server frame.
func (f *Fixture) ReadServerFrame() (opcode int, payload []byte, err error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(f.reader, header); err != nil {
		return 0, nil, err
	}
	opcode = int(header[0] & 0x0f)
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		extended := make([]byte, 2)
		if _, err := io.ReadFull(f.reader, extended); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(extended))
	case 127:
		extended := make([]byte, 8)
		if _, err := io.ReadFull(f.reader, extended); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(extended)
	}
	payload = make([]byte, length)
	if _, err := io.ReadFull(f.reader, payload); err != nil {
		return 0, nil, err
	}
	return opcode, payload, nil
}

// CloseClient disconnects the client peer while leaving the server endpoint to observe EOF.
func (f *Fixture) CloseClient() error { return f.client.Close() }

// Close terminates both ends of the transport.
func (f *Fixture) Close() error {
	var first error
	f.closeOnce.Do(func() {
		first = f.client.Close()
		if err := f.server.Close(); first == nil {
			first = err
		}
	})
	return first
}

type observedConn struct {
	net.Conn
	started chan<- struct{}
}

func (c *observedConn) Write(p []byte) (int, error) {
	c.started <- struct{}{}
	return c.Conn.Write(p)
}
