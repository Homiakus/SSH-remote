package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultKeyPath(t *testing.T) {
	got, err := DefaultKeyPath("prod")
	if err != nil {
		t.Fatalf("DefaultKeyPath() error = %v", err)
	}
	if got != "keys/prod.ed25519" {
		t.Fatalf("DefaultKeyPath() = %q, want %q", got, "keys/prod.ed25519")
	}
}

func TestDefaultKeyPathRejectsUnsafeServerNames(t *testing.T) {
	tests := []string{
		"",
		"../prod",
		`prod\root`,
		"prod/root",
		"bad:name",
		"..",
	}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DefaultKeyPath(name); err == nil {
				t.Fatalf("expected error for %q", name)
			}
		})
	}
}

func TestResolveKeyPathRelativeToServers(t *testing.T) {
	got, err := ResolveKeyPath("keys/prod.ed25519")
	if err != nil {
		t.Fatalf("ResolveKeyPath() error = %v", err)
	}
	want := filepath.Join("servers", "keys", "prod.ed25519")
	if got != want {
		t.Fatalf("ResolveKeyPath() = %q, want %q", got, want)
	}
}

func TestResolveKeyPathDirectFileInServers(t *testing.T) {
	got, err := ResolveKeyPath("prod.key")
	if err != nil {
		t.Fatalf("ResolveKeyPath() error = %v", err)
	}
	want := filepath.Join("servers", "prod.key")
	if got != want {
		t.Fatalf("ResolveKeyPath() = %q, want %q", got, want)
	}
}

func TestResolveKeyPathKeepsAbsolutePath(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "id_ed25519")
	got, err := ResolveKeyPath(abs)
	if err != nil {
		t.Fatalf("ResolveKeyPath() error = %v", err)
	}
	if got != filepath.Clean(abs) {
		t.Fatalf("ResolveKeyPath() = %q, want %q", got, filepath.Clean(abs))
	}
}

func TestResolveKeyPathExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir() error = %v", err)
	}

	got, err := ResolveKeyPath("~/.ssh/id_ed25519")
	if err != nil {
		t.Fatalf("ResolveKeyPath() error = %v", err)
	}
	want := filepath.Join(home, ".ssh", "id_ed25519")
	if got != want {
		t.Fatalf("ResolveKeyPath() = %q, want %q", got, want)
	}
}

func TestResolveKeyPathRejectsTraversal(t *testing.T) {
	if _, err := ResolveKeyPath("../id_ed25519"); err == nil {
		t.Fatal("expected traversal error")
	}
	if _, err := ResolveKeyPath(`..\id_ed25519`); err == nil {
		t.Fatal("expected backslash traversal error")
	}
}
