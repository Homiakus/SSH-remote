package wsfixture

import (
	"net/http"
	"strings"
	"testing"
)

func TestClientFramesRoundTripLengths(t *testing.T) {
	for _, size := range []int{0, 5, 125, 126, 65535, 65536} {
		t.Run(strings.Repeat("x", min(size, 8)), func(t *testing.T) {
			fixture := New()
			defer fixture.Close()

			payload := []byte(strings.Repeat("p", size))
			done := make(chan error, 1)
			go func() { done <- fixture.WriteClientFrame(2, payload) }()

			opcode, got, err := fixture.ReadClientFrame()
			if err != nil {
				t.Fatalf("read client frame: %v", err)
			}
			if opcode != 2 {
				t.Fatalf("opcode = %d, want 2", opcode)
			}
			if string(got) != string(payload) {
				t.Fatalf("payload length/content mismatch: got %d bytes, want %d", len(got), len(payload))
			}
			if err := <-done; err != nil {
				t.Fatalf("write client frame: %v", err)
			}
		})
	}
}

func TestResponseWriterSurface(t *testing.T) {
	fixture := New()
	defer fixture.Close()
	fixture.Header().Set("X-Test", "ok")
	if got := fixture.Header().Get("X-Test"); got != "ok" {
		t.Fatalf("header = %q, want ok", got)
	}
	fixture.WriteHeader(http.StatusSwitchingProtocols)
	if _, err := fixture.Write([]byte("unexpected")); err == nil {
		t.Fatal("expected ResponseWriter.Write to reject direct writes")
	}
	if fixture.ServerWriteStarted() == nil {
		t.Fatal("write-started channel is nil")
	}
}
