package atomicfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// File is the minimal file surface needed by an atomic write transaction.
type File interface {
	Name() string
	Write([]byte) (int, error)
	Chmod(os.FileMode) error
	Close() error
}

// Ops isolates filesystem side effects so failure paths can be exercised deterministically.
type Ops interface {
	MkdirAll(string, os.FileMode) error
	CreateTemp(string, string) (File, error)
	Rename(string, string) error
	Stat(string) (fs.FileInfo, error)
	Remove(string) error
}

// OSOps delegates Ops to the host operating system.
type OSOps struct{}

func (OSOps) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (OSOps) CreateTemp(dir, pattern string) (File, error) { return os.CreateTemp(dir, pattern) }
func (OSOps) Rename(oldPath, newPath string) error         { return os.Rename(oldPath, newPath) }
func (OSOps) Stat(path string) (fs.FileInfo, error)        { return os.Stat(path) }
func (OSOps) Remove(path string) error                     { return os.Remove(path) }

// Write replaces path atomically where the platform allows it, preserving the previous file
// through a backup/restore fallback when an in-place rename is not available.
func Write(path string, data []byte, perm os.FileMode) error {
	return WriteWithOps(path, data, perm, OSOps{})
}

// WriteWithOps is Write with injectable filesystem operations for deterministic fault testing.
func WriteWithOps(path string, data []byte, perm os.FileMode, ops Ops) (err error) {
	dir := filepath.Dir(path)
	if err := ops.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	temp, err := ops.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("не удалось создать временный файл для %s: %w", path, err)
	}
	tempName := temp.Name()
	defer func() {
		if err != nil {
			_ = ops.Remove(tempName)
		}
	}()

	if _, err = temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("не удалось записать временный файл %s: %w", tempName, err)
	}
	if err = temp.Chmod(perm); err != nil {
		_ = temp.Close()
		return fmt.Errorf("не удалось выставить права на временный файл %s: %w", tempName, err)
	}
	if err = temp.Close(); err != nil {
		return fmt.Errorf("не удалось закрыть временный файл %s: %w", tempName, err)
	}

	if err = ops.Rename(tempName, path); err == nil {
		return nil
	}
	renameErr := err
	if _, statErr := ops.Stat(path); statErr != nil {
		return fmt.Errorf("не удалось заменить %s: %w", path, renameErr)
	}

	backup := tempName + ".old"
	if backupErr := ops.Rename(path, backup); backupErr != nil {
		return fmt.Errorf("не удалось заменить %s: rename: %v; backup: %w", path, renameErr, backupErr)
	}
	if err = ops.Rename(tempName, path); err != nil {
		_ = ops.Rename(backup, path)
		return fmt.Errorf("не удалось заменить %s после backup: %w", path, err)
	}
	_ = ops.Remove(backup)
	return nil
}
