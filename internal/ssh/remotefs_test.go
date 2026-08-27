package ssh

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestSortRemoteEntriesDirsFirst(t *testing.T) {
	entries := []RemoteEntry{
		{Name: "zeta.log", IsDir: false},
		{Name: "alpha", IsDir: true},
		{Name: "beta.txt", IsDir: false},
		{Name: "docs", IsDir: true},
	}

	sortRemoteEntries(entries)

	got := []string{entries[0].Name, entries[1].Name, entries[2].Name, entries[3].Name}
	want := []string{"alpha", "docs", "beta.txt", "zeta.log"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sortRemoteEntries() order = %#v, want %#v", got, want)
		}
	}
}

func TestNormalizeRemotePath(t *testing.T) {
	tests := []struct {
		name     string
		startDir string
		input    string
		want     string
	}{
		{name: "empty uses start dir", startDir: "/home/root", input: "", want: "/home/root"},
		{name: "dot uses start dir", startDir: "/home/root", input: ".", want: "/home/root"},
		{name: "relative joins start dir", startDir: "/home/root", input: "logs/app.log", want: "/home/root/logs/app.log"},
		{name: "absolute stays absolute", startDir: "/home/root", input: "/etc/hosts", want: "/etc/hosts"},
		{name: "clean path traversals", startDir: "/home/root", input: "../tmp/../app", want: "/home/app"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeRemotePath(tt.startDir, tt.input); got != tt.want {
				t.Fatalf("normalizeRemotePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeRemoteMutationPathBlocksRelativeEscape(t *testing.T) {
	if _, err := normalizeRemoteMutationPath("/home/root", "../app"); err == nil {
		t.Fatal("expected relative traversal to be rejected")
	}

	got, err := normalizeRemoteMutationPath("/home/root", "logs/app.log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/home/root/logs/app.log" {
		t.Fatalf("normalizeRemoteMutationPath() = %q, want /home/root/logs/app.log", got)
	}
}

func TestValidateRemoteDestructivePathBlocksBroadTargets(t *testing.T) {
	tests := []string{"/", "/tmp", ".", "../app"}
	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			if _, err := validateRemoteDestructivePath("/home/root", input); err == nil {
				t.Fatalf("expected destructive path %q to be rejected", input)
			}
		})
	}

	got, err := validateRemoteDestructivePath("/home/root", "logs/app.log")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/home/root/logs/app.log" {
		t.Fatalf("validateRemoteDestructivePath() = %q, want /home/root/logs/app.log", got)
	}
}

func TestParentRemotePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "root stays root", path: "/", want: "/"},
		{name: "file returns containing dir", path: "/home/root/app.log", want: "/home/root"},
		{name: "dir returns parent", path: "/home/root/logs", want: "/home/root"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parentRemotePath(tt.path); got != tt.want {
				t.Fatalf("parentRemotePath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveRemoteStartDirFallback(t *testing.T) {
	got := resolveRemoteStartDir(
		func() (string, error) { return "", errors.New("no cwd") },
		func(_ string) (string, error) { return "", errors.New("no real path") },
	)

	if got != "/" {
		t.Fatalf("resolveRemoteStartDir() = %q, want /", got)
	}
}

func TestResolveRemoteStartDirUsesRealPathFallback(t *testing.T) {
	got := resolveRemoteStartDir(
		func() (string, error) { return "", errors.New("no cwd") },
		func(_ string) (string, error) { return "/srv/app", nil },
	)

	if got != "/srv/app" {
		t.Fatalf("resolveRemoteStartDir() = %q, want /srv/app", got)
	}
}

func TestPreviewFromDataDetectsBinaryAndEditable(t *testing.T) {
	info := fakeFileInfo{
		name: "notes.txt",
		size: 4,
		mode: 0644,
		mod:  time.Unix(10, 0),
	}

	textPreview := previewFromData("/tmp/notes.txt", info, []byte("test"))
	if !textPreview.IsText {
		t.Fatal("expected text preview to be text")
	}

	binaryPreview := previewFromData("/tmp/blob.bin", info, []byte{0x00, 0x10})
	if binaryPreview.IsText {
		t.Fatal("expected binary preview to be non-text")
	}
}

func TestStaticSFTPProviderCloseSkipsSharedClientCloser(t *testing.T) {
	sftpClosed := 0
	clientClosed := 0
	fs := &sftpRemoteFS{
		provider: &staticSFTPProvider{
			startDir:  "/home/test",
			closeSFTP: func() error { sftpClosed++; return nil },
		},
	}

	if err := fs.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if sftpClosed != 1 {
		t.Fatalf("sftp close count = %d, want 1", sftpClosed)
	}
	if clientClosed != 0 {
		t.Fatalf("client close count = %d, want 0", clientClosed)
	}
}

func TestStaticSFTPProviderCloseClosesOwnedClient(t *testing.T) {
	sftpClosed := 0
	clientClosed := 0
	fs := &sftpRemoteFS{
		provider: &staticSFTPProvider{
			startDir:    "/home/test",
			closeSFTP:   func() error { sftpClosed++; return nil },
			closeClient: func() error { clientClosed++; return nil },
		},
	}

	if err := fs.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if sftpClosed != 1 {
		t.Fatalf("sftp close count = %d, want 1", sftpClosed)
	}
	if clientClosed != 1 {
		t.Fatalf("client close count = %d, want 1", clientClosed)
	}
}

type fakeFileInfo struct {
	name string
	size int64
	mode os.FileMode
	mod  time.Time
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return f.mod }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }
