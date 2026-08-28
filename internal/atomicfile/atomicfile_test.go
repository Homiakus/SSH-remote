package atomicfile_test

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"sshpilot/internal/atomicfile"
	"sshpilot/internal/testkit/faultfs"
)

func TestWriteCreatesAndReplacesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	if err := atomicfile.Write(path, []byte("first"), 0o600); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := atomicfile.Write(path, []byte("second"), 0o600); err != nil {
		t.Fatalf("second write: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	if string(data) != "second" {
		t.Fatalf("content = %q, want second", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat result: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
		}
	}
}

func TestWriteWithOpsFailureMatrix(t *testing.T) {
	tests := []struct {
		name        string
		prepareDest bool
		configure   func(*faultfs.Ops, error)
		wantOld     bool
	}{
		{name: "mkdir", configure: func(ops *faultfs.Ops, injected error) { ops.FailAt(faultfs.OpMkdirAll, 1, injected) }},
		{name: "create temp", configure: func(ops *faultfs.Ops, injected error) { ops.FailAt(faultfs.OpCreateTemp, 1, injected) }},
		{name: "write temp", configure: func(ops *faultfs.Ops, injected error) { ops.FailAt(faultfs.OpWrite, 1, injected) }},
		{name: "chmod temp", configure: func(ops *faultfs.Ops, injected error) { ops.FailAt(faultfs.OpChmod, 1, injected) }},
		{name: "close temp", configure: func(ops *faultfs.Ops, injected error) { ops.FailAt(faultfs.OpClose, 1, injected) }},
		{name: "initial rename without destination", configure: func(ops *faultfs.Ops, injected error) { ops.FailAt(faultfs.OpRename, 1, injected) }},
		{name: "stat failure after initial rename", prepareDest: true, wantOld: true, configure: func(ops *faultfs.Ops, injected error) {
			ops.FailAt(faultfs.OpRename, 1, injected)
			ops.FailAt(faultfs.OpStat, 1, injected)
		}},
		{name: "backup rename", prepareDest: true, wantOld: true, configure: func(ops *faultfs.Ops, injected error) {
			ops.FailAt(faultfs.OpRename, 1, injected)
			ops.FailAt(faultfs.OpRename, 2, injected)
		}},
		{name: "replacement rename restores backup", prepareDest: true, wantOld: true, configure: func(ops *faultfs.Ops, injected error) {
			ops.FailAt(faultfs.OpRename, 1, injected)
			ops.FailAt(faultfs.OpRename, 3, injected)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "nested", "state.json")
			if tt.prepareDest {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
					t.Fatalf("prepare destination: %v", err)
				}
			}

			injected := errors.New("injected failure")
			ops := faultfs.New(atomicfile.OSOps{})
			tt.configure(ops, injected)

			err := atomicfile.WriteWithOps(path, []byte("new"), 0o600, ops)
			if err == nil || !strings.Contains(err.Error(), injected.Error()) {
				t.Fatalf("error = %v, want injected failure", err)
			}

			data, readErr := os.ReadFile(path)
			switch {
			case tt.wantOld:
				if readErr != nil || string(data) != "old" {
					t.Fatalf("destination after failure = %q, %v; want old", data, readErr)
				}
			case readErr == nil:
				t.Fatalf("destination unexpectedly exists with %q", data)
			}

			leftovers, globErr := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
			if globErr != nil {
				t.Fatalf("glob temp leftovers: %v", globErr)
			}
			if len(leftovers) != 0 {
				t.Fatalf("atomic write left temporary/backup files after failure: %v", leftovers)
			}
		})
	}
}

func TestWriteWithOpsFallbackReplacementSucceeds(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatalf("prepare destination: %v", err)
	}

	injected := errors.New("force fallback")
	ops := faultfs.New(atomicfile.OSOps{})
	ops.FailAt(faultfs.OpRename, 1, injected)

	if err := atomicfile.WriteWithOps(path, []byte("new"), 0o600, ops); err != nil {
		t.Fatalf("fallback write: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read destination: %v", err)
	}
	if string(data) != "new" {
		t.Fatalf("destination = %q, want new", data)
	}
	if got := ops.Calls(faultfs.OpRename); got != 3 {
		t.Fatalf("rename calls = %d, want 3", got)
	}
	if got := ops.Calls(faultfs.OpRemove); got == 0 {
		t.Fatal("expected backup cleanup remove call")
	}

	leftovers, globErr := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if globErr != nil {
		t.Fatalf("glob temp leftovers: %v", globErr)
	}
	if len(leftovers) != 0 {
		t.Fatalf("fallback write left temporary/backup files: %v", leftovers)
	}
}
