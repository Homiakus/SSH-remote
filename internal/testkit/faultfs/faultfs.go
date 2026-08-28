package faultfs

import (
	"io/fs"
	"os"
	"sync"

	"sshpilot/internal/atomicfile"
)

// Operation identifies one filesystem boundary that can fail.
type Operation string

const (
	OpMkdirAll   Operation = "mkdir_all"
	OpCreateTemp Operation = "create_temp"
	OpWrite      Operation = "write"
	OpChmod      Operation = "chmod"
	OpClose      Operation = "close"
	OpRename     Operation = "rename"
	OpStat       Operation = "stat"
	OpRemove     Operation = "remove"
)

type failureKey struct {
	op   Operation
	call int
}

// Ops decorates atomicfile.Ops with deterministic nth-call fault injection.
type Ops struct {
	Base atomicfile.Ops

	mu       sync.Mutex
	failures map[failureKey]error
	calls    map[Operation]int
}

// New creates a fault-injecting filesystem over base.
func New(base atomicfile.Ops) *Ops {
	return &Ops{
		Base:     base,
		failures: make(map[failureKey]error),
		calls:    make(map[Operation]int),
	}
}

// FailAt makes the nth call of op return err. Call numbers start at 1.
func (o *Ops) FailAt(op Operation, call int, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.failures[failureKey{op: op, call: call}] = err
}

// Calls returns how many times op has been observed.
func (o *Ops) Calls(op Operation) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.calls[op]
}

func (o *Ops) next(op Operation) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls[op]++
	return o.failures[failureKey{op: op, call: o.calls[op]}]
}

func (o *Ops) MkdirAll(path string, perm os.FileMode) error {
	if err := o.next(OpMkdirAll); err != nil {
		return err
	}
	return o.Base.MkdirAll(path, perm)
}

func (o *Ops) CreateTemp(dir, pattern string) (atomicfile.File, error) {
	if err := o.next(OpCreateTemp); err != nil {
		return nil, err
	}
	file, err := o.Base.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	return &fileWrapper{File: file, owner: o}, nil
}

func (o *Ops) Rename(oldPath, newPath string) error {
	if err := o.next(OpRename); err != nil {
		return err
	}
	return o.Base.Rename(oldPath, newPath)
}

func (o *Ops) Stat(path string) (fs.FileInfo, error) {
	if err := o.next(OpStat); err != nil {
		return nil, err
	}
	return o.Base.Stat(path)
}

func (o *Ops) Remove(path string) error {
	if err := o.next(OpRemove); err != nil {
		return err
	}
	return o.Base.Remove(path)
}

type fileWrapper struct {
	atomicfile.File
	owner *Ops
}

func (f *fileWrapper) Write(data []byte) (int, error) {
	if err := f.owner.next(OpWrite); err != nil {
		return 0, err
	}
	return f.File.Write(data)
}

func (f *fileWrapper) Chmod(mode os.FileMode) error {
	if err := f.owner.next(OpChmod); err != nil {
		return err
	}
	return f.File.Chmod(mode)
}

func (f *fileWrapper) Close() error {
	if err := f.owner.next(OpClose); err != nil {
		_ = f.File.Close()
		return err
	}
	return f.File.Close()
}
