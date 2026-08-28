package remotefs

import (
	"os"
	"sync"

	sshcore "sshpilot/internal/ssh"
)

// Operation identifies a RemoteFS method invocation.
type Operation string

const (
	OpListDir  Operation = "list_dir"
	OpStat     Operation = "stat"
	OpReadFile Operation = "read_file"
	OpWrite    Operation = "write_file"
	OpMkdir    Operation = "mkdir"
	OpRename   Operation = "rename"
	OpRemove   Operation = "remove"
	OpChmod    Operation = "chmod"
	OpChown    Operation = "chown"
	OpUpload   Operation = "upload"
	OpDownload Operation = "download"
	OpClose    Operation = "close"
)

// Call is an immutable snapshot of one fake filesystem invocation.
type Call struct {
	Operation Operation
	Path      string
	Path2     string
	Data      []byte
	Limit     int64
	Mode      os.FileMode
	UID       int
	GID       int
	Transfer  sshcore.TransferRequest
}

// Fake implements ssh.RemoteFS with function fields and deterministic call recording.
type Fake struct {
	StartDirValue string

	ListDirFunc   func(string) ([]sshcore.RemoteEntry, error)
	StatFunc      func(string) (sshcore.RemoteEntry, error)
	ReadFileFunc  func(string, int64) (sshcore.FilePreview, error)
	WriteFileFunc func(string, []byte) error
	MkdirFunc     func(string) error
	RenameFunc    func(string, string) error
	RemoveFunc    func(string) error
	ChmodFunc     func(string, os.FileMode) error
	ChownFunc     func(string, int, int) error
	UploadFunc    func(sshcore.TransferRequest) error
	DownloadFunc  func(sshcore.TransferRequest) error
	CloseFunc     func() error

	mu    sync.Mutex
	calls []Call
}

func (f *Fake) StartDir() string {
	if f.StartDirValue == "" {
		return "/"
	}
	return f.StartDirValue
}

func (f *Fake) ListDir(path string) ([]sshcore.RemoteEntry, error) {
	f.record(Call{Operation: OpListDir, Path: path})
	if f.ListDirFunc != nil {
		return f.ListDirFunc(path)
	}
	return nil, nil
}

func (f *Fake) Stat(path string) (sshcore.RemoteEntry, error) {
	f.record(Call{Operation: OpStat, Path: path})
	if f.StatFunc != nil {
		return f.StatFunc(path)
	}
	return sshcore.RemoteEntry{}, nil
}

func (f *Fake) ReadFile(path string, limit int64) (sshcore.FilePreview, error) {
	f.record(Call{Operation: OpReadFile, Path: path, Limit: limit})
	if f.ReadFileFunc != nil {
		return f.ReadFileFunc(path, limit)
	}
	return sshcore.FilePreview{}, nil
}

func (f *Fake) WriteFile(path string, data []byte) error {
	copyData := append([]byte(nil), data...)
	f.record(Call{Operation: OpWrite, Path: path, Data: copyData})
	if f.WriteFileFunc != nil {
		return f.WriteFileFunc(path, data)
	}
	return nil
}

func (f *Fake) Mkdir(path string) error {
	f.record(Call{Operation: OpMkdir, Path: path})
	if f.MkdirFunc != nil {
		return f.MkdirFunc(path)
	}
	return nil
}

func (f *Fake) Rename(oldPath, newPath string) error {
	f.record(Call{Operation: OpRename, Path: oldPath, Path2: newPath})
	if f.RenameFunc != nil {
		return f.RenameFunc(oldPath, newPath)
	}
	return nil
}

func (f *Fake) Remove(path string) error {
	f.record(Call{Operation: OpRemove, Path: path})
	if f.RemoveFunc != nil {
		return f.RemoveFunc(path)
	}
	return nil
}

func (f *Fake) Chmod(path string, mode os.FileMode) error {
	f.record(Call{Operation: OpChmod, Path: path, Mode: mode})
	if f.ChmodFunc != nil {
		return f.ChmodFunc(path, mode)
	}
	return nil
}

func (f *Fake) Chown(path string, uid, gid int) error {
	f.record(Call{Operation: OpChown, Path: path, UID: uid, GID: gid})
	if f.ChownFunc != nil {
		return f.ChownFunc(path, uid, gid)
	}
	return nil
}

func (f *Fake) Upload(req sshcore.TransferRequest) error {
	f.record(Call{Operation: OpUpload, Transfer: req})
	if f.UploadFunc != nil {
		return f.UploadFunc(req)
	}
	return nil
}

func (f *Fake) Download(req sshcore.TransferRequest) error {
	f.record(Call{Operation: OpDownload, Transfer: req})
	if f.DownloadFunc != nil {
		return f.DownloadFunc(req)
	}
	return nil
}

func (f *Fake) Close() error {
	f.record(Call{Operation: OpClose})
	if f.CloseFunc != nil {
		return f.CloseFunc()
	}
	return nil
}

// Calls returns a defensive snapshot of recorded operations.
func (f *Fake) Calls() []Call {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]Call, len(f.calls))
	copy(out, f.calls)
	for i := range out {
		out[i].Data = append([]byte(nil), out[i].Data...)
	}
	return out
}

func (f *Fake) record(call Call) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}

var _ sshcore.RemoteFS = (*Fake)(nil)
