package remotefs

import (
	"errors"
	"os"
	"reflect"
	"testing"

	sshcore "sshpilot/internal/ssh"
)

func TestFakeRecordsAndDelegatesEveryOperation(t *testing.T) {
	injected := errors.New("injected")
	entry := sshcore.RemoteEntry{Path: "/root/a"}
	preview := sshcore.FilePreview{Path: "/root/a", Content: "body"}
	transfer := sshcore.TransferRequest{LocalPath: "local", RemotePath: "/remote", Overwrite: true}

	fake := &Fake{
		StartDirValue: "/root",
		ListDirFunc: func(path string) ([]sshcore.RemoteEntry, error) {
			if path != "/root" {
				t.Fatalf("ListDir path = %q", path)
			}
			return []sshcore.RemoteEntry{entry}, nil
		},
		StatFunc: func(path string) (sshcore.RemoteEntry, error) { return entry, nil },
		ReadFileFunc: func(path string, limit int64) (sshcore.FilePreview, error) {
			if limit != 42 {
				t.Fatalf("ReadFile limit = %d", limit)
			}
			return preview, nil
		},
		WriteFileFunc: func(path string, data []byte) error {
			if path != "/root/a" || string(data) != "data" {
				t.Fatalf("WriteFile args = %q %q", path, data)
			}
			return injected
		},
		MkdirFunc: func(path string) error { return injected },
		RenameFunc: func(oldPath, newPath string) error {
			if oldPath != "old" || newPath != "new" {
				t.Fatalf("Rename args = %q %q", oldPath, newPath)
			}
			return injected
		},
		RemoveFunc: func(path string) error { return injected },
		ChmodFunc: func(path string, mode os.FileMode) error {
			if mode != 0o640 {
				t.Fatalf("mode = %v", mode)
			}
			return injected
		},
		ChownFunc: func(path string, uid, gid int) error {
			if uid != 12 || gid != 34 {
				t.Fatalf("owner = %d:%d", uid, gid)
			}
			return injected
		},
		UploadFunc: func(req sshcore.TransferRequest) error {
			if !reflect.DeepEqual(req, transfer) {
				t.Fatalf("upload = %#v", req)
			}
			return injected
		},
		DownloadFunc: func(req sshcore.TransferRequest) error {
			if !reflect.DeepEqual(req, transfer) {
				t.Fatalf("download = %#v", req)
			}
			return injected
		},
		CloseFunc: func() error { return injected },
	}

	if fake.StartDir() != "/root" {
		t.Fatalf("StartDir = %q", fake.StartDir())
	}
	if got, err := fake.ListDir("/root"); err != nil || !reflect.DeepEqual(got, []sshcore.RemoteEntry{entry}) {
		t.Fatalf("ListDir = %#v, %v", got, err)
	}
	if got, err := fake.Stat("/root/a"); err != nil || got.Path != entry.Path {
		t.Fatalf("Stat = %#v, %v", got, err)
	}
	if got, err := fake.ReadFile("/root/a", 42); err != nil || got.Content != "body" {
		t.Fatalf("ReadFile = %#v, %v", got, err)
	}
	if err := fake.WriteFile("/root/a", []byte("data")); !errors.Is(err, injected) {
		t.Fatalf("WriteFile error = %v", err)
	}
	if err := fake.Mkdir("dir"); !errors.Is(err, injected) {
		t.Fatalf("Mkdir error = %v", err)
	}
	if err := fake.Rename("old", "new"); !errors.Is(err, injected) {
		t.Fatalf("Rename error = %v", err)
	}
	if err := fake.Remove("gone"); !errors.Is(err, injected) {
		t.Fatalf("Remove error = %v", err)
	}
	if err := fake.Chmod("mode", 0o640); !errors.Is(err, injected) {
		t.Fatalf("Chmod error = %v", err)
	}
	if err := fake.Chown("owner", 12, 34); !errors.Is(err, injected) {
		t.Fatalf("Chown error = %v", err)
	}
	if err := fake.Upload(transfer); !errors.Is(err, injected) {
		t.Fatalf("Upload error = %v", err)
	}
	if err := fake.Download(transfer); !errors.Is(err, injected) {
		t.Fatalf("Download error = %v", err)
	}
	if err := fake.Close(); !errors.Is(err, injected) {
		t.Fatalf("Close error = %v", err)
	}

	calls := fake.Calls()
	wantOps := []Operation{OpListDir, OpStat, OpReadFile, OpWrite, OpMkdir, OpRename, OpRemove, OpChmod, OpChown, OpUpload, OpDownload, OpClose}
	if len(calls) != len(wantOps) {
		t.Fatalf("calls = %d, want %d", len(calls), len(wantOps))
	}
	for i, want := range wantOps {
		if calls[i].Operation != want {
			t.Fatalf("call[%d] = %q, want %q", i, calls[i].Operation, want)
		}
	}
	if string(calls[3].Data) != "data" {
		t.Fatalf("recorded data = %q", calls[3].Data)
	}
	if calls[2].Limit != 42 || calls[7].Mode != 0o640 || calls[8].UID != 12 || calls[8].GID != 34 {
		t.Fatalf("recorded scalar arguments incorrect: %#v", calls)
	}
	if !reflect.DeepEqual(calls[9].Transfer, transfer) || !reflect.DeepEqual(calls[10].Transfer, transfer) {
		t.Fatalf("recorded transfer incorrect")
	}
}

func TestFakeDefaultsAndDefensiveCopies(t *testing.T) {
	fake := &Fake{}
	if fake.StartDir() != "/" {
		t.Fatalf("default StartDir = %q", fake.StartDir())
	}
	if got, err := fake.ListDir("/"); err != nil || got != nil {
		t.Fatalf("default ListDir = %#v, %v", got, err)
	}
	if got, err := fake.Stat("/x"); err != nil || got != (sshcore.RemoteEntry{}) {
		t.Fatalf("default Stat = %#v, %v", got, err)
	}
	if got, err := fake.ReadFile("/x", 1); err != nil || got != (sshcore.FilePreview{}) {
		t.Fatalf("default ReadFile = %#v, %v", got, err)
	}
	if err := fake.WriteFile("/x", []byte("original")); err != nil {
		t.Fatalf("default WriteFile: %v", err)
	}
	for name, err := range map[string]error{
		"mkdir": fake.Mkdir("/d"), "rename": fake.Rename("a", "b"), "remove": fake.Remove("x"),
		"chmod": fake.Chmod("x", 0o600), "chown": fake.Chown("x", 1, 2),
		"upload": fake.Upload(sshcore.TransferRequest{}), "download": fake.Download(sshcore.TransferRequest{}), "close": fake.Close(),
	} {
		if err != nil {
			t.Fatalf("default %s: %v", name, err)
		}
	}

	calls := fake.Calls()
	calls[3].Data[0] = 'X'
	calls2 := fake.Calls()
	if string(calls2[3].Data) != "original" {
		t.Fatalf("Calls leaked mutable data: %q", calls2[3].Data)
	}
}
