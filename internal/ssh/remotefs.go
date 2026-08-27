package ssh

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"

	"sshpilot/internal/config"

	"github.com/pkg/sftp"
)

const FilePreviewLimit = 256 * 1024

// RemoteEntry описывает файл или директорию на удалённом сервере.
type RemoteEntry struct {
	Name    string
	Path    string
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
	UID     int
	GID     int
	IsDir   bool
}

// FilePreview содержит содержимое файла и его метаданные для превью/редактирования.
type FilePreview struct {
	Path      string
	Content   string
	Size      int64
	Mode      os.FileMode
	ModTime   time.Time
	UID       int
	GID       int
	IsText    bool
	Truncated bool
	Editable  bool
}

// TransferRequest описывает передачу файла между локальной машиной и сервером.
type TransferRequest struct {
	LocalPath  string
	RemotePath string
	Overwrite  bool
}

// RemoteFS описывает файловые операции поверх SSH.
type RemoteFS interface {
	StartDir() string
	ListDir(path string) ([]RemoteEntry, error)
	Stat(path string) (RemoteEntry, error)
	ReadFile(path string, limit int64) (FilePreview, error)
	WriteFile(path string, content []byte) error
	Mkdir(path string) error
	Rename(oldPath, newPath string) error
	Remove(path string) error
	Chmod(path string, mode os.FileMode) error
	Chown(path string, uid, gid int) error
	Upload(req TransferRequest) error
	Download(req TransferRequest) error
	Close() error
}

type sftpRemoteFS struct {
	provider sftpProvider
}

type sftpProvider interface {
	Client() (*sftp.Client, error)
	StartDir() string
	Close() error
}

type staticSFTPProvider struct {
	sftp        *sftp.Client
	startDir    string
	closeSFTP   func() error
	closeClient func() error
}

type managedSFTPProvider struct {
	manager *Manager

	mu         sync.Mutex
	sftp       *sftp.Client
	generation uint64
	startDir   string

	openSFTP  func(*ssh.Client) (*sftp.Client, error)
	closeSFTP func(*sftp.Client) error
}

// OpenRemoteFS открывает файловую систему сервера через SFTP.
func OpenRemoteFS(cfg *config.ServerConfig) (RemoteFS, error) {
	client, err := Connect(cfg)
	if err != nil {
		return nil, err
	}

	return openRemoteFSWithClient(client, client.Close)
}

// OpenRemoteFSWithClient открывает SFTP поверх уже существующего SSH-клиента.
func OpenRemoteFSWithClient(client *ssh.Client) (RemoteFS, error) {
	return openRemoteFSWithClient(client, nil)
}

// OpenRemoteFSWithManager открывает SFTP через общий менеджер SSH-соединения.
func OpenRemoteFSWithManager(manager *Manager) (RemoteFS, error) {
	provider := &managedSFTPProvider{
		manager:   manager,
		openSFTP:  func(client *ssh.Client) (*sftp.Client, error) { return sftp.NewClient(client) },
		closeSFTP: func(client *sftp.Client) error { return client.Close() },
	}

	if _, err := provider.Client(); err != nil {
		return nil, err
	}

	return &sftpRemoteFS{provider: provider}, nil
}

func openRemoteFSWithClient(client *ssh.Client, closeClient func() error) (RemoteFS, error) {
	sftpClient, err := sftp.NewClient(client)
	if err != nil {
		if closeClient != nil {
			_ = closeClient()
		}
		return nil, fmt.Errorf("не удалось открыть SFTP-сессию: %w", err)
	}

	return &sftpRemoteFS{
		provider: &staticSFTPProvider{
			sftp:        sftpClient,
			startDir:    resolveRemoteStartDir(sftpClient.Getwd, func(p string) (string, error) { return sftpClient.RealPath(p) }),
			closeSFTP:   sftpClient.Close,
			closeClient: closeClient,
		},
	}, nil
}

func (fs *sftpRemoteFS) StartDir() string {
	if fs.provider == nil {
		return "/"
	}
	return fs.provider.StartDir()
}

func (fs *sftpRemoteFS) ListDir(dir string) ([]RemoteEntry, error) {
	sftpClient, err := fs.provider.Client()
	if err != nil {
		return nil, err
	}

	target := normalizeRemotePath(fs.StartDir(), dir)

	infos, err := sftpClient.ReadDir(target)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать директорию %s: %w", target, err)
	}

	entries := make([]RemoteEntry, 0, len(infos))
	for _, info := range infos {
		entry := remoteEntryFromInfo(info, path.Join(target, info.Name()))
		entries = append(entries, entry)
	}

	sortRemoteEntries(entries)
	return entries, nil
}

func (fs *sftpRemoteFS) Stat(name string) (RemoteEntry, error) {
	sftpClient, err := fs.provider.Client()
	if err != nil {
		return RemoteEntry{}, err
	}

	target := normalizeRemotePath(fs.StartDir(), name)

	info, err := sftpClient.Stat(target)
	if err != nil {
		return RemoteEntry{}, fmt.Errorf("не удалось получить информацию о %s: %w", target, err)
	}

	return remoteEntryFromInfo(info, target), nil
}

func (fs *sftpRemoteFS) ReadFile(name string, limit int64) (FilePreview, error) {
	if limit <= 0 {
		limit = FilePreviewLimit
	}

	sftpClient, err := fs.provider.Client()
	if err != nil {
		return FilePreview{}, err
	}

	target := normalizeRemotePath(fs.StartDir(), name)
	info, err := sftpClient.Stat(target)
	if err != nil {
		return FilePreview{}, fmt.Errorf("не удалось получить информацию о %s: %w", target, err)
	}
	if info.IsDir() {
		return FilePreview{}, fmt.Errorf("%s является директорией", target)
	}

	file, err := sftpClient.Open(target)
	if err != nil {
		return FilePreview{}, fmt.Errorf("не удалось открыть %s: %w", target, err)
	}
	defer file.Close()

	readLimit := info.Size()
	truncated := false
	if readLimit > limit {
		readLimit = limit
		truncated = true
	}

	data, err := io.ReadAll(io.LimitReader(file, readLimit))
	if err != nil {
		return FilePreview{}, fmt.Errorf("не удалось прочитать %s: %w", target, err)
	}

	preview := previewFromData(target, info, data)
	preview.Truncated = truncated
	preview.Editable = preview.IsText && !preview.Truncated

	return preview, nil
}

func (fs *sftpRemoteFS) WriteFile(name string, content []byte) error {
	sftpClient, err := fs.provider.Client()
	if err != nil {
		return err
	}

	target, err := normalizeRemoteMutationPath(fs.StartDir(), name)
	if err != nil {
		return err
	}
	file, err := sftpClient.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY)
	if err != nil {
		return fmt.Errorf("не удалось открыть %s для записи: %w", target, err)
	}
	defer file.Close()

	if _, err := io.Copy(file, bytes.NewReader(content)); err != nil {
		return fmt.Errorf("не удалось записать %s: %w", target, err)
	}

	return nil
}

func (fs *sftpRemoteFS) Mkdir(name string) error {
	sftpClient, err := fs.provider.Client()
	if err != nil {
		return err
	}

	target, err := normalizeRemoteMutationPath(fs.StartDir(), name)
	if err != nil {
		return err
	}
	if err := sftpClient.Mkdir(target); err != nil {
		return fmt.Errorf("не удалось создать директорию %s: %w", target, err)
	}
	return nil
}

func (fs *sftpRemoteFS) Rename(oldPath, newPath string) error {
	sftpClient, err := fs.provider.Client()
	if err != nil {
		return err
	}

	src, err := normalizeRemoteMutationPath(fs.StartDir(), oldPath)
	if err != nil {
		return err
	}
	dst, err := normalizeRemoteMutationPath(fs.StartDir(), newPath)
	if err != nil {
		return err
	}
	if err := sftpClient.PosixRename(src, dst); err != nil {
		return fmt.Errorf("не удалось переименовать %s в %s: %w", src, dst, err)
	}
	return nil
}

func (fs *sftpRemoteFS) Remove(name string) error {
	sftpClient, err := fs.provider.Client()
	if err != nil {
		return err
	}

	target, err := validateRemoteDestructivePath(fs.StartDir(), name)
	if err != nil {
		return err
	}

	info, err := sftpClient.Stat(target)
	if err != nil {
		return fmt.Errorf("не удалось получить информацию о %s: %w", target, err)
	}

	if info.IsDir() {
		if err := sftpClient.RemoveAll(target); err != nil {
			return fmt.Errorf("не удалось удалить директорию %s: %w", target, err)
		}
		return nil
	}

	if err := sftpClient.Remove(target); err != nil {
		return fmt.Errorf("не удалось удалить файл %s: %w", target, err)
	}
	return nil
}

func (fs *sftpRemoteFS) Chmod(name string, mode os.FileMode) error {
	sftpClient, err := fs.provider.Client()
	if err != nil {
		return err
	}

	target, err := normalizeRemoteMutationPath(fs.StartDir(), name)
	if err != nil {
		return err
	}
	if err := sftpClient.Chmod(target, mode); err != nil {
		return fmt.Errorf("не удалось изменить права для %s: %w", target, err)
	}
	return nil
}

func (fs *sftpRemoteFS) Chown(name string, uid, gid int) error {
	sftpClient, err := fs.provider.Client()
	if err != nil {
		return err
	}

	target, err := normalizeRemoteMutationPath(fs.StartDir(), name)
	if err != nil {
		return err
	}
	if err := sftpClient.Chown(target, uid, gid); err != nil {
		return fmt.Errorf("не удалось изменить владельца для %s: %w", target, err)
	}
	return nil
}

func (fs *sftpRemoteFS) Upload(req TransferRequest) error {
	localPath := filepath.Clean(strings.TrimSpace(req.LocalPath))
	sftpClient, err := fs.provider.Client()
	if err != nil {
		return err
	}

	remotePath, err := normalizeRemoteMutationPath(fs.StartDir(), req.RemotePath)
	if err != nil {
		return err
	}
	localInfo, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("не удалось открыть локальный путь %s: %w", localPath, err)
	}

	if localInfo.IsDir() {
		return uploadLocalDirectory(sftpClient, localPath, remotePath, req.Overwrite)
	}

	return uploadLocalFile(sftpClient, localPath, remotePath, req.Overwrite)
}

func (fs *sftpRemoteFS) Download(req TransferRequest) error {
	localPath := filepath.Clean(strings.TrimSpace(req.LocalPath))
	sftpClient, err := fs.provider.Client()
	if err != nil {
		return err
	}

	remotePath := normalizeRemotePath(fs.StartDir(), req.RemotePath)

	src, err := sftpClient.Open(remotePath)
	if err != nil {
		return fmt.Errorf("не удалось открыть удалённый файл %s: %w", remotePath, err)
	}
	defer src.Close()

	flags := os.O_CREATE | os.O_WRONLY
	if req.Overwrite {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}

	dst, err := os.OpenFile(localPath, flags, 0644)
	if err != nil {
		return fmt.Errorf("не удалось открыть локальный файл %s: %w", localPath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("не удалось скачать %s в %s: %w", remotePath, localPath, err)
	}

	return nil
}

func (fs *sftpRemoteFS) Close() error {
	if fs.provider == nil {
		return nil
	}
	return fs.provider.Close()
}

func (p *staticSFTPProvider) Client() (*sftp.Client, error) {
	if p.sftp == nil {
		return nil, fmt.Errorf("sftp client closed")
	}
	return p.sftp, nil
}

func (p *staticSFTPProvider) StartDir() string {
	if strings.TrimSpace(p.startDir) == "" {
		return "/"
	}
	return p.startDir
}

func (p *staticSFTPProvider) Close() error {
	var err error
	if p.closeSFTP != nil {
		err = p.closeSFTP()
	}
	if p.closeClient != nil {
		clientErr := p.closeClient()
		if err == nil {
			err = clientErr
		}
	}

	p.sftp = nil
	p.closeSFTP = nil
	p.closeClient = nil
	return err
}

func (p *managedSFTPProvider) Client() (*sftp.Client, error) {
	client, generation, err := p.manager.checkedClient()
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.sftp != nil && p.generation == generation {
		return p.sftp, nil
	}
	if err := p.closeLocked(); err != nil {
		return nil, err
	}

	openFn := p.openSFTP
	if openFn == nil {
		openFn = func(client *ssh.Client) (*sftp.Client, error) { return sftp.NewClient(client) }
	}

	sftpClient, err := openFn(client)
	if err != nil {
		return nil, fmt.Errorf("не удалось открыть SFTP-сессию: %w", err)
	}

	p.sftp = sftpClient
	p.generation = generation
	p.startDir = resolveRemoteStartDir(
		sftpClient.Getwd,
		func(name string) (string, error) { return sftpClient.RealPath(name) },
	)

	return p.sftp, nil
}

func (p *managedSFTPProvider) StartDir() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if strings.TrimSpace(p.startDir) == "" {
		return "/"
	}
	return p.startDir
}

func (p *managedSFTPProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeLocked()
}

func (p *managedSFTPProvider) closeLocked() error {
	if p.sftp == nil {
		return nil
	}

	closeFn := p.closeSFTP
	if closeFn == nil {
		closeFn = func(client *sftp.Client) error { return client.Close() }
	}

	err := closeFn(p.sftp)
	p.sftp = nil
	return err
}

func resolveRemoteStartDir(
	getwd func() (string, error),
	realPath func(string) (string, error),
) string {
	if getwd != nil {
		if cwd, err := getwd(); err == nil {
			cwd = strings.TrimSpace(cwd)
			if cwd != "" {
				return normalizeRemotePath("/", cwd)
			}
		}
	}

	if realPath != nil {
		if cwd, err := realPath("."); err == nil {
			cwd = strings.TrimSpace(cwd)
			if cwd != "" {
				return normalizeRemotePath("/", cwd)
			}
		}
	}

	return "/"
}

func normalizeRemotePath(startDir, name string) string {
	base := strings.TrimSpace(startDir)
	if base == "" {
		base = "/"
	}
	base = path.Clean("/" + strings.TrimPrefix(base, "/"))

	target := strings.TrimSpace(name)
	if target == "" || target == "." {
		return base
	}

	if !strings.HasPrefix(target, "/") {
		target = path.Join(base, target)
	}

	return path.Clean("/" + strings.TrimPrefix(target, "/"))
}

func normalizeRemoteMutationPath(startDir, name string) (string, error) {
	raw := strings.TrimSpace(name)
	if raw == "" || raw == "." {
		return "", fmt.Errorf("remote path is empty")
	}

	target := normalizeRemotePath(startDir, raw)
	if !strings.HasPrefix(raw, "/") {
		base := normalizeRemotePath("/", startDir)
		if !remotePathWithin(base, target) {
			return "", fmt.Errorf("remote path %q escapes start directory %s", name, base)
		}
	}
	return target, nil
}

func validateRemoteDestructivePath(startDir, name string) (string, error) {
	target, err := normalizeRemoteMutationPath(startDir, name)
	if err != nil {
		return "", err
	}
	base := normalizeRemotePath("/", startDir)
	switch {
	case target == "/":
		return "", fmt.Errorf("refusing to remove remote root")
	case target == base:
		return "", fmt.Errorf("refusing to remove start directory %s", base)
	case path.Dir(target) == "/":
		return "", fmt.Errorf("refusing to remove top-level remote path %s", target)
	default:
		return target, nil
	}
}

func remotePathWithin(base, target string) bool {
	base = normalizeRemotePath("/", base)
	target = normalizeRemotePath("/", target)
	if base == "/" {
		return strings.HasPrefix(target, "/")
	}
	return target == base || strings.HasPrefix(target, strings.TrimRight(base, "/")+"/")
}

func parentRemotePath(name string) string {
	current := normalizeRemotePath("/", name)
	if current == "/" {
		return "/"
	}

	parent := path.Dir(current)
	if parent == "." || parent == "" {
		return "/"
	}
	return parent
}

func sortRemoteEntries(entries []RemoteEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

func remoteEntryFromInfo(info os.FileInfo, fullPath string) RemoteEntry {
	entry := RemoteEntry{
		Name:    info.Name(),
		Path:    fullPath,
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
		IsDir:   info.IsDir(),
	}

	if stat, ok := info.Sys().(*sftp.FileStat); ok {
		entry.UID = int(stat.UID)
		entry.GID = int(stat.GID)
	}

	return entry
}

func previewFromData(name string, info os.FileInfo, data []byte) FilePreview {
	preview := FilePreview{
		Path:    name,
		Size:    info.Size(),
		Mode:    info.Mode(),
		ModTime: info.ModTime(),
	}

	if stat, ok := info.Sys().(*sftp.FileStat); ok {
		preview.UID = int(stat.UID)
		preview.GID = int(stat.GID)
	}

	if isTextPreview(data) {
		preview.IsText = true
		preview.Content = string(data)
	}

	return preview
}

func isTextPreview(data []byte) bool {
	if len(data) == 0 {
		return true
	}

	if bytesContainZero(data) {
		return false
	}

	return utf8.Valid(data)
}

func bytesContainZero(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// ErrRemotePathExists удобно использовать в предикатах проверок при UI-операциях.
var ErrRemotePathExists = errors.New("remote path exists")

func uploadLocalDirectory(sftpClient *sftp.Client, localRoot, remoteRoot string, overwrite bool) error {
	if err := ensureRemoteDirectoryTarget(sftpClient, remoteRoot, overwrite); err != nil {
		return err
	}

	return filepath.WalkDir(localRoot, func(currentLocalPath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relativePath, err := filepath.Rel(localRoot, currentLocalPath)
		if err != nil {
			return fmt.Errorf("не удалось вычислить относительный путь для %s: %w", currentLocalPath, err)
		}
		if relativePath == "." {
			return nil
		}

		remoteTargetPath := path.Join(remoteRoot, filepath.ToSlash(relativePath))
		if entry.IsDir() {
			if err := sftpClient.MkdirAll(remoteTargetPath); err != nil {
				return fmt.Errorf("не удалось создать удалённую директорию %s: %w", remoteTargetPath, err)
			}
			return nil
		}

		return uploadLocalFile(sftpClient, currentLocalPath, remoteTargetPath, overwrite)
	})
}

func uploadLocalFile(sftpClient *sftp.Client, localPath, remotePath string, overwrite bool) error {
	if info, err := sftpClient.Stat(remotePath); err == nil && info.IsDir() {
		return fmt.Errorf("удалённый путь %s является директорией", remotePath)
	} else if err != nil && !isRemoteNotExistError(err) {
		return fmt.Errorf("не удалось получить информацию о %s: %w", remotePath, err)
	}

	parentDir := parentRemotePath(remotePath)
	if err := sftpClient.MkdirAll(parentDir); err != nil {
		return fmt.Errorf("не удалось создать удалённую директорию %s: %w", parentDir, err)
	}

	src, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("не удалось открыть локальный файл %s: %w", localPath, err)
	}
	defer src.Close()

	flags := os.O_CREATE | os.O_WRONLY
	if overwrite {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}

	dst, err := sftpClient.OpenFile(remotePath, flags)
	if err != nil {
		return fmt.Errorf("не удалось открыть удалённый файл %s: %w", remotePath, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("не удалось загрузить %s в %s: %w", localPath, remotePath, err)
	}

	return nil
}

func ensureRemoteDirectoryTarget(sftpClient *sftp.Client, remotePath string, overwrite bool) error {
	info, err := sftpClient.Stat(remotePath)
	if err == nil {
		if info.IsDir() {
			if !overwrite {
				return ErrRemotePathExists
			}
			return nil
		}
		if !overwrite {
			return ErrRemotePathExists
		}
		if err := sftpClient.Remove(remotePath); err != nil {
			return fmt.Errorf("не удалось заменить файл %s директорией: %w", remotePath, err)
		}
	} else if !isRemoteNotExistError(err) {
		return fmt.Errorf("не удалось получить информацию о %s: %w", remotePath, err)
	}

	if err := sftpClient.MkdirAll(remotePath); err != nil {
		return fmt.Errorf("не удалось создать удалённую директорию %s: %w", remotePath, err)
	}
	return nil
}

func isRemoteNotExistError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "no such file") || strings.Contains(message, "not exist")
}
