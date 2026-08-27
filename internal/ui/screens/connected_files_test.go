package screens

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"sshpilot/internal/config"
	sshclient "sshpilot/internal/ssh"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConnectedFileModeLoadsDirectoryAndPreview(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")
	fs.addFile("/home/test/readme.txt", "hello from server\n", 0644)

	m := newConnectedModelWithRemoteFS(config.ServerConfig{Name: "prod"}, func(config.ServerConfig) (sshclient.RemoteFS, error) {
		return fs, nil
	})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m, cmd := m.Update(keyRunes("f"))
	m = runConnectedCmd(t, m, cmd)

	if m.leftMode != leftModeFiles {
		t.Fatalf("expected files mode, got %v", m.leftMode)
	}
	if m.files.currentPath != "/home/test" {
		t.Fatalf("current path = %q, want /home/test", m.files.currentPath)
	}
	if len(m.files.entries) != 1 || m.files.entries[0].Name != "readme.txt" {
		t.Fatalf("unexpected entries: %#v", m.files.entries)
	}

	m, cmd = m.Update(keyEnter())
	m = runConnectedCmd(t, m, cmd)

	if m.files.preview == nil {
		t.Fatal("expected preview to be loaded")
	}
	if got := strings.TrimSpace(m.files.preview.Content); got != "hello from server" {
		t.Fatalf("preview content = %q", got)
	}

	view := m.View()
	for _, want := range []string{"📁 Файлы", "u/o", "backspace", "hello from server"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view does not contain %q:\n%s", want, view)
		}
	}
}

func TestConnectedFileEditorEscShowsDiscardDialog(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")
	fs.addFile("/home/test/readme.txt", "hello\n", 0644)

	m := loadedFileModeModel(t, fs)

	m, cmd := m.Update(keyRunes("e"))
	m = runConnectedCmd(t, m, cmd)
	if !m.files.editorActive {
		t.Fatal("expected editor to become active")
	}

	m.files.editor.SetValue("changed")

	m, cmd = m.Update(keyEsc())
	if cmd != nil {
		t.Fatal("expected esc in dirty editor to stay local")
	}
	if m.files.dialog == nil || m.files.dialog.kind != fileDialogConfirmDiscard {
		t.Fatalf("expected discard confirmation dialog, got %#v", m.files.dialog)
	}

	m, _ = m.Update(keyEnter())
	if m.files.editorActive {
		t.Fatal("expected editor to close after confirming discard")
	}
}

func TestConnectedFileDeleteRequiresConfirm(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")
	fs.addFile("/home/test/readme.txt", "hello\n", 0644)

	m := loadedFileModeModel(t, fs)

	m, cmd := m.Update(keyRunes("d"))
	if cmd != nil {
		t.Fatal("delete should wait for confirmation")
	}
	if m.files.dialog == nil || m.files.dialog.kind != fileDialogConfirmDelete {
		t.Fatalf("expected delete dialog, got %#v", m.files.dialog)
	}
	if _, ok := fs.nodes["/home/test/readme.txt"]; !ok {
		t.Fatal("file should still exist before confirmation")
	}

	m, cmd = m.Update(keyEnter())
	m = runConnectedCmd(t, m, cmd)

	if _, ok := fs.nodes["/home/test/readme.txt"]; ok {
		t.Fatal("file should be removed after confirmation")
	}
	if len(m.files.entries) != 0 {
		t.Fatalf("expected empty directory after delete, got %#v", m.files.entries)
	}
}

func TestConnectedFileBusyBlocksTerminal(t *testing.T) {
	m := newConnectedModelWithRemoteFS(config.ServerConfig{Name: "prod"}, func(config.ServerConfig) (sshclient.RemoteFS, error) {
		return newFakeRemoteFS("/home/test"), nil
	})
	m.leftMode = leftModeFiles
	m.files.busy = true

	m, cmd := m.Update(keyRunes("t"))
	if cmd != nil {
		t.Fatal("terminal should not open while file operation is busy")
	}
	if m.IsTerminalActive() {
		t.Fatal("terminal should remain inactive")
	}
}

func TestRunFileActionCmdUploadAndDownloadRequireOverwriteConfirm(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")
	fs.addFile("/home/test/existing.txt", "old", 0644)

	tempDir := t.TempDir()
	localUpload := path.Join(tempDir, "upload.txt")
	if err := os.WriteFile(localUpload, []byte("new upload"), 0644); err != nil {
		t.Fatalf("write upload file: %v", err)
	}

	msg := runFileActionCmd(fs, filePendingAction{
		kind:       fileActionUpload,
		localPath:  localUpload,
		remotePath: "/home/test/existing.txt",
	})()
	if _, ok := msg.(fileOverwriteRequiredMsg); !ok {
		t.Fatalf("expected overwrite confirmation for upload, got %T", msg)
	}

	done, ok := runFileActionCmd(fs, filePendingAction{
		kind:       fileActionUpload,
		localPath:  localUpload,
		remotePath: "/home/test/existing.txt",
		overwrite:  true,
	})().(fileActionDoneMsg)
	if !ok || done.err != nil {
		t.Fatalf("expected upload success, got %#v", done)
	}
	if got := string(fs.nodes["/home/test/existing.txt"].content); got != "new upload" {
		t.Fatalf("upload content = %q", got)
	}

	localDownload := path.Join(tempDir, "download.txt")
	if err := os.WriteFile(localDownload, []byte("already here"), 0644); err != nil {
		t.Fatalf("write local download file: %v", err)
	}

	msg = runFileActionCmd(fs, filePendingAction{
		kind:       fileActionDownload,
		localPath:  localDownload,
		remotePath: "/home/test/existing.txt",
	})()
	if _, ok := msg.(fileOverwriteRequiredMsg); !ok {
		t.Fatalf("expected overwrite confirmation for download, got %T", msg)
	}

	done, ok = runFileActionCmd(fs, filePendingAction{
		kind:       fileActionDownload,
		localPath:  localDownload,
		remotePath: "/home/test/existing.txt",
		overwrite:  true,
	})().(fileActionDoneMsg)
	if !ok || done.err != nil {
		t.Fatalf("expected download success, got %#v", done)
	}

	data, err := os.ReadFile(localDownload)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "new upload" {
		t.Fatalf("downloaded content = %q", string(data))
	}
}

func TestRunFileActionCmdUploadDirectoryRecursively(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")

	localDir := filepath.Join(t.TempDir(), "site")
	if err := os.MkdirAll(filepath.Join(localDir, "assets"), 0755); err != nil {
		t.Fatalf("create local dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "index.html"), []byte("<h1>ok</h1>"), 0644); err != nil {
		t.Fatalf("write index.html: %v", err)
	}
	if err := os.WriteFile(filepath.Join(localDir, "assets", "app.js"), []byte("console.log('ok')"), 0644); err != nil {
		t.Fatalf("write app.js: %v", err)
	}

	msg := runFileActionCmd(fs, filePendingAction{
		kind:       fileActionUpload,
		localPath:  localDir,
		remotePath: "/home/test/site",
	})()
	done, ok := msg.(fileActionDoneMsg)
	if !ok || done.err != nil {
		t.Fatalf("expected directory upload success, got %#v", msg)
	}
	if done.status != "Папка загружена на сервер" {
		t.Fatalf("status = %q, want folder upload status", done.status)
	}

	if node, ok := fs.nodes["/home/test/site"]; !ok || !node.isDir {
		t.Fatalf("expected remote site directory, got %#v", node)
	}
	if got := string(fs.nodes["/home/test/site/index.html"].content); got != "<h1>ok</h1>" {
		t.Fatalf("index content = %q", got)
	}
	if got := string(fs.nodes["/home/test/site/assets/app.js"].content); got != "console.log('ok')" {
		t.Fatalf("nested file content = %q", got)
	}
}

func TestConnectedUploadDialogBrowseFilePopulatesPaths(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")
	fs.addFile("/home/test/readme.txt", "hello\n", 0644)

	m := loadedFileModeModel(t, fs)

	localFile := filepath.Join(t.TempDir(), "deploy.tar.gz")
	if err := os.WriteFile(localFile, []byte("archive"), 0644); err != nil {
		t.Fatalf("write local file: %v", err)
	}

	picker := &fakeLocalPathPicker{filePath: localFile}
	m.localPicker = picker

	m, cmd := m.Update(keyRunes("u"))
	m = runConnectedCmd(t, m, cmd)
	m, cmd = m.Update(keyCtrlO())
	m = runConnectedCmd(t, m, cmd)

	if picker.fileCalls != 1 {
		t.Fatalf("file picker calls = %d, want 1", picker.fileCalls)
	}
	if m.files.dialog == nil || m.files.dialog.kind != fileDialogUpload {
		t.Fatalf("expected upload dialog, got %#v", m.files.dialog)
	}
	if got := m.files.dialog.inputs[0].Value(); got != localFile {
		t.Fatalf("local path = %q, want %q", got, localFile)
	}
	if got := m.files.dialog.inputs[1].Value(); got != "/home/test/deploy.tar.gz" {
		t.Fatalf("remote path = %q, want /home/test/deploy.tar.gz", got)
	}
	if m.files.dialog.focusIndex != 1 {
		t.Fatalf("focus index = %d, want 1", m.files.dialog.focusIndex)
	}
}

func TestConnectedUploadDialogBrowseFolderPopulatesPaths(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")
	fs.addFile("/home/test/readme.txt", "hello\n", 0644)

	m := loadedFileModeModel(t, fs)

	localDir := filepath.Join(t.TempDir(), "release")
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatalf("create local dir: %v", err)
	}

	picker := &fakeLocalPathPicker{folderPath: localDir}
	m.localPicker = picker

	m, cmd := m.Update(keyRunes("u"))
	m = runConnectedCmd(t, m, cmd)
	m, cmd = m.Update(keyCtrlG())
	m = runConnectedCmd(t, m, cmd)

	if picker.folderCalls != 1 {
		t.Fatalf("folder picker calls = %d, want 1", picker.folderCalls)
	}
	if m.files.dialog == nil || m.files.dialog.kind != fileDialogUpload {
		t.Fatalf("expected upload dialog, got %#v", m.files.dialog)
	}
	if got := m.files.dialog.inputs[0].Value(); got != localDir {
		t.Fatalf("local path = %q, want %q", got, localDir)
	}
	if got := m.files.dialog.inputs[1].Value(); got != "/home/test/release" {
		t.Fatalf("remote path = %q, want /home/test/release", got)
	}
}

func TestRunFileActionCmdSaveRenameAndPermissions(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")
	fs.addFile("/home/test/notes.txt", "old", 0644)

	msg := runFileActionCmd(fs, filePendingAction{
		kind:    fileActionSave,
		path:    "/home/test/notes.txt",
		content: "new content",
	})()
	done, ok := msg.(fileActionDoneMsg)
	if !ok || done.err != nil {
		t.Fatalf("expected save success, got %#v", msg)
	}
	if got := string(fs.nodes["/home/test/notes.txt"].content); got != "new content" {
		t.Fatalf("saved content = %q", got)
	}

	msg = runFileActionCmd(fs, filePendingAction{
		kind:   fileActionRename,
		path:   "/home/test/notes.txt",
		target: "/home/test/renamed.txt",
	})()
	done, ok = msg.(fileActionDoneMsg)
	if !ok || done.err != nil {
		t.Fatalf("expected rename success, got %#v", msg)
	}
	if _, ok := fs.nodes["/home/test/notes.txt"]; ok {
		t.Fatal("old file should be renamed away")
	}
	if _, ok := fs.nodes["/home/test/renamed.txt"]; !ok {
		t.Fatal("renamed file missing")
	}

	msg = runFileActionCmd(fs, filePendingAction{
		kind:     fileActionPermissions,
		path:     "/home/test/renamed.txt",
		mode:     0600,
		hasMode:  true,
		uid:      1001,
		gid:      1002,
		hasOwner: true,
	})()
	done, ok = msg.(fileActionDoneMsg)
	if !ok || done.err != nil {
		t.Fatalf("expected chmod/chown success, got %#v", msg)
	}

	node := fs.nodes["/home/test/renamed.txt"]
	if node.mode.Perm() != 0600 {
		t.Fatalf("mode = %o, want 600", node.mode.Perm())
	}
	if node.uid != 1001 || node.gid != 1002 {
		t.Fatalf("owner = %d:%d, want 1001:1002", node.uid, node.gid)
	}
}

func TestConnectedFileRunActionStartsRemoteBinary(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")
	fs.addFile("/home/test/tool", "binary", 0o755)

	connection := &fakeSSHConnection{
		openRemoteFSFn: func() (sshclient.RemoteFS, error) { return fs, nil },
	}

	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: connection},
	)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m, cmd := m.Update(keyRunes("f"))
	m = runConnectedCmd(t, m, cmd)
	m, cmd = m.Update(keyRunes("x"))
	m = runConnectedCmd(t, m, cmd)

	if m.executing {
		t.Fatal("expected remote file run to complete")
	}
	if len(connection.nativeCommands) != 1 {
		t.Fatalf("native commands = %#v, want one run command", connection.nativeCommands)
	}
	if strings.Contains(connection.nativeCommands[0], "__SSHPILOT_") {
		t.Fatalf("native run command should not contain tracking markers: %q", connection.nativeCommands[0])
	}
	if !strings.Contains(connection.nativeCommands[0], "/home/test/tool") {
		t.Fatalf("run command = %q", connection.nativeCommands[0])
	}
	if fs.nodes["/home/test/tool"].mode.Perm() != 0o755 {
		t.Fatalf("mode = %o, want 755", fs.nodes["/home/test/tool"].mode.Perm())
	}
}

func TestConnectedFileRunActionConfirmsChmodForNonExecutable(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")
	fs.addFile("/home/test/tool", "binary", 0o644)

	connection := &fakeSSHConnection{
		openRemoteFSFn: func() (sshclient.RemoteFS, error) { return fs, nil },
	}

	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: connection},
	)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m, cmd := m.Update(keyRunes("f"))
	m = runConnectedCmd(t, m, cmd)
	m, cmd = m.Update(keyRunes("x"))
	if cmd != nil {
		t.Fatal("non-executable run should wait for confirmation")
	}
	if m.files.dialog == nil || m.files.dialog.kind != fileDialogConfirmRun {
		t.Fatalf("expected chmod+run confirmation dialog, got %#v", m.files.dialog)
	}

	m, cmd = m.Update(keyEnter())
	m = runConnectedCmd(t, m, cmd)

	if m.executing {
		t.Fatal("expected chmod+run flow to complete")
	}
	if fs.nodes["/home/test/tool"].mode.Perm() != 0o755 {
		t.Fatalf("mode = %o, want 755", fs.nodes["/home/test/tool"].mode.Perm())
	}
}

func TestHandleFileMessageStoresOperationErrors(t *testing.T) {
	m := newConnectedModelWithRemoteFS(config.ServerConfig{Name: "prod"}, func(config.ServerConfig) (sshclient.RemoteFS, error) {
		return newFakeRemoteFS("/home/test"), nil
	})

	next, _, handled := m.handleFileMessage(fileActionDoneMsg{
		action: fileActionSave,
		err:    errors.New("permission denied"),
	})
	if !handled {
		t.Fatal("expected message to be handled")
	}
	if !strings.Contains(next.files.errorMessage, "permission denied") {
		t.Fatalf("error message = %q", next.files.errorMessage)
	}
}

func TestConnectedFileModeRetriesOpenRemoteFSAfterReset(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")
	fs.addFile("/home/test/readme.txt", "hello from server\n", 0o644)

	openCalls := 0
	connection := &fakeSSHConnection{
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			openCalls++
			if openCalls == 1 {
				return nil, errors.New("sftp EOF")
			}
			return fs, nil
		},
		resetFn: func() error { return nil },
	}

	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: connection},
	)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	m, cmd := m.Update(keyRunes("f"))
	m = runConnectedCmd(t, m, cmd)

	if !m.files.initialized {
		t.Fatal("expected file mode to initialize after retry")
	}
	if m.files.currentPath != "/home/test" {
		t.Fatalf("current path = %q, want /home/test", m.files.currentPath)
	}
	if len(m.files.entries) != 1 || m.files.entries[0].Name != "readme.txt" {
		t.Fatalf("unexpected entries: %#v", m.files.entries)
	}
	if connection.openRemoteFSCalls != 2 {
		t.Fatalf("open remote fs calls = %d, want 2", connection.openRemoteFSCalls)
	}
	if connection.resetCalls != 1 {
		t.Fatalf("reset calls = %d, want 1", connection.resetCalls)
	}
}

func TestOpenRemoteFSCmdReturnsErrorForNilSessionInsteadOfPanicking(t *testing.T) {
	connection := &fakeSSHConnection{
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			return nil, nil
		},
		resetFn: func() error { return nil },
	}

	msg, ok := openRemoteFSCmd(connection)().(fileFSReadyMsg)
	if !ok {
		t.Fatal("expected fileFSReadyMsg")
	}
	if msg.err == nil {
		t.Fatal("expected error for nil remote fs")
	}
	if !strings.Contains(msg.err.Error(), "пустую SFTP-сессию") {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	if connection.openRemoteFSCalls != 2 {
		t.Fatalf("open remote fs calls = %d, want 2", connection.openRemoteFSCalls)
	}
	if connection.resetCalls != 1 {
		t.Fatalf("reset calls = %d, want 1", connection.resetCalls)
	}
}

func TestOpenRemoteFSCmdRetriesAfterTimeout(t *testing.T) {
	oldTimeout := goLaunchOpenFSTimeout
	goLaunchOpenFSTimeout = 10 * time.Millisecond
	defer func() {
		goLaunchOpenFSTimeout = oldTimeout
	}()

	release := make(chan struct{})
	released := false
	fs := newFakeRemoteFS("/home/test")
	var connection *fakeSSHConnection
	connection = &fakeSSHConnection{
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			if connection.ResetCallCount() == 0 {
				<-release
				return nil, errors.New("stale sftp session")
			}
			return fs, nil
		},
		resetFn: func() error {
			if !released {
				close(release)
				released = true
			}
			return nil
		},
	}

	msg, ok := openRemoteFSCmd(connection)().(fileFSReadyMsg)
	if !ok {
		t.Fatal("expected fileFSReadyMsg")
	}
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	if msg.fs == nil {
		t.Fatal("expected remote fs after retry")
	}
	if msg.startDir != "/home/test" {
		t.Fatalf("start dir = %q, want /home/test", msg.startDir)
	}
	if connection.openRemoteFSCalls != 2 {
		t.Fatalf("open remote fs calls = %d, want 2", connection.openRemoteFSCalls)
	}
	if connection.resetCalls != 2 {
		t.Fatalf("reset calls = %d, want 2", connection.resetCalls)
	}
}

func loadedFileModeModel(t *testing.T, fs *fakeRemoteFS) ConnectedModel {
	t.Helper()

	m := newConnectedModelWithRemoteFS(config.ServerConfig{Name: "prod"}, func(config.ServerConfig) (sshclient.RemoteFS, error) {
		return fs, nil
	})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	var cmd tea.Cmd
	m, cmd = m.Update(keyRunes("f"))
	m = runConnectedCmd(t, m, cmd)
	m, cmd = m.Update(keyEnter())
	m = runConnectedCmd(t, m, cmd)
	return m
}

func runConnectedCmd(t *testing.T, m ConnectedModel, cmd tea.Cmd) ConnectedModel {
	t.Helper()

	if cmd == nil {
		return m
	}

	msg, ok := tryRunTeaCmd(cmd, 5*time.Millisecond)
	if !ok {
		return m
	}
	return runConnectedMsg(t, m, msg)
}

func runConnectedMsg(t *testing.T, m ConnectedModel, msg tea.Msg) ConnectedModel {
	t.Helper()

	switch msg := msg.(type) {
	case nil:
		return m
	case tea.BatchMsg:
		pending := append([]tea.Cmd{}, msg...)
		for len(pending) > 0 {
			progressed := false
			remaining := pending[:0]
			for _, nested := range pending {
				nestedMsg, ok := tryRunTeaCmd(nested, 5*time.Millisecond)
				if !ok {
					remaining = append(remaining, nested)
					continue
				}
				progressed = true
				m = runConnectedMsg(t, m, nestedMsg)
			}
			if !progressed {
				return m
			}
			pending = remaining
		}
		return m
	default:
		var next tea.Cmd
		m, next = m.Update(msg)
		return runConnectedCmd(t, m, next)
	}
}

func tryRunTeaCmd(cmd tea.Cmd, timeout time.Duration) (tea.Msg, bool) {
	if cmd == nil {
		return nil, true
	}

	ch := make(chan tea.Msg, 1)
	go func() {
		ch <- cmd()
	}()

	select {
	case msg := <-ch:
		return msg, true
	case <-time.After(timeout):
		return nil, false
	}
}

func keyRunes(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

func keyEnter() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEnter}
}

func keyEsc() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyEsc}
}

func keyCtrlC() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyCtrlC}
}

func keyShiftTab() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyShiftTab}
}

func keyCtrlO() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyCtrlO}
}

func keyCtrlG() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyCtrlG}
}

type fakeLocalPathPicker struct {
	filePath    string
	folderPath  string
	fileErr     error
	folderErr   error
	fileCalls   int
	folderCalls int
}

func (p *fakeLocalPathPicker) PickFile(_ string) (string, error) {
	p.fileCalls++
	return p.filePath, p.fileErr
}

func (p *fakeLocalPathPicker) PickFolder(_ string) (string, error) {
	p.folderCalls++
	return p.folderPath, p.folderErr
}

type fakeRemoteFS struct {
	startDir   string
	nodes      map[string]*fakeRemoteNode
	operations []string
	recordFn   func(string)
}

type fakeRemoteNode struct {
	isDir   bool
	content []byte
	mode    os.FileMode
	modTime time.Time
	uid     int
	gid     int
}

func newFakeRemoteFS(startDir string) *fakeRemoteFS {
	fs := &fakeRemoteFS{
		startDir: cleanRemote(startDir),
		nodes:    map[string]*fakeRemoteNode{},
	}
	fs.addDir(fs.startDir)
	return fs
}

func (fs *fakeRemoteFS) StartDir() string { return fs.startDir }

func (fs *fakeRemoteFS) ListDir(name string) ([]sshclient.RemoteEntry, error) {
	target := cleanRemote(name)
	node, ok := fs.nodes[target]
	if !ok {
		return nil, os.ErrNotExist
	}
	if !node.isDir {
		return nil, errors.New("not a directory")
	}

	entries := []sshclient.RemoteEntry{}
	for fullPath, child := range fs.nodes {
		if fullPath == target {
			continue
		}
		if path.Dir(fullPath) != target {
			continue
		}
		entries = append(entries, fs.entry(fullPath, child))
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

func (fs *fakeRemoteFS) Stat(name string) (sshclient.RemoteEntry, error) {
	target := cleanRemote(name)
	node, ok := fs.nodes[target]
	if !ok {
		return sshclient.RemoteEntry{}, os.ErrNotExist
	}
	return fs.entry(target, node), nil
}

func (fs *fakeRemoteFS) ReadFile(name string, limit int64) (sshclient.FilePreview, error) {
	target := cleanRemote(name)
	node, ok := fs.nodes[target]
	if !ok {
		return sshclient.FilePreview{}, os.ErrNotExist
	}
	if node.isDir {
		return sshclient.FilePreview{}, errors.New("is a directory")
	}

	content := node.content
	truncated := false
	if limit > 0 && int64(len(content)) > limit {
		content = content[:limit]
		truncated = true
	}

	return sshclient.FilePreview{
		Path:      target,
		Content:   string(content),
		Size:      int64(len(node.content)),
		Mode:      node.mode,
		ModTime:   node.modTime,
		UID:       node.uid,
		GID:       node.gid,
		IsText:    !strings.ContainsRune(string(content), '\x00'),
		Truncated: truncated,
		Editable:  !truncated,
	}, nil
}

func (fs *fakeRemoteFS) WriteFile(name string, content []byte) error {
	target := cleanRemote(name)
	fs.record("write:" + target)
	fs.nodes[target] = &fakeRemoteNode{
		content: append([]byte(nil), content...),
		mode:    0644,
		modTime: time.Now(),
	}
	return nil
}

func (fs *fakeRemoteFS) Mkdir(name string) error {
	fs.record("mkdir:" + cleanRemote(name))
	fs.addDir(name)
	return nil
}

func (fs *fakeRemoteFS) Rename(oldPath, newPath string) error {
	oldPath = cleanRemote(oldPath)
	newPath = cleanRemote(newPath)
	fs.record("rename:" + oldPath + "->" + newPath)

	node, ok := fs.nodes[oldPath]
	if !ok {
		return os.ErrNotExist
	}

	delete(fs.nodes, oldPath)
	fs.nodes[newPath] = node

	if node.isDir {
		for existingPath, child := range cloneNodeMap(fs.nodes) {
			if !strings.HasPrefix(existingPath, oldPath+"/") {
				continue
			}
			delete(fs.nodes, existingPath)
			fs.nodes[strings.Replace(existingPath, oldPath, newPath, 1)] = child
		}
	}
	return nil
}

func (fs *fakeRemoteFS) Remove(name string) error {
	target := cleanRemote(name)
	fs.record("remove:" + target)
	if _, ok := fs.nodes[target]; !ok {
		return os.ErrNotExist
	}
	for existingPath := range cloneNodeMap(fs.nodes) {
		if existingPath == target || strings.HasPrefix(existingPath, target+"/") {
			delete(fs.nodes, existingPath)
		}
	}
	return nil
}

func (fs *fakeRemoteFS) Chmod(name string, mode os.FileMode) error {
	target := cleanRemote(name)
	fs.record("chmod:" + target)
	node, ok := fs.nodes[target]
	if !ok {
		return os.ErrNotExist
	}
	node.mode = (node.mode & os.ModeDir) | mode
	return nil
}

func (fs *fakeRemoteFS) Chown(name string, uid, gid int) error {
	target := cleanRemote(name)
	node, ok := fs.nodes[target]
	if !ok {
		return os.ErrNotExist
	}
	node.uid = uid
	node.gid = gid
	return nil
}

func (fs *fakeRemoteFS) Upload(req sshclient.TransferRequest) error {
	fs.record("upload:" + cleanRemote(req.RemotePath))
	info, err := os.Stat(req.LocalPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		fs.addDir(req.RemotePath)
		return filepath.Walk(req.LocalPath, func(currentLocalPath string, _ os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			relativePath, err := filepath.Rel(req.LocalPath, currentLocalPath)
			if err != nil {
				return err
			}
			if relativePath == "." {
				return nil
			}

			remoteTarget := cleanRemote(path.Join(req.RemotePath, filepath.ToSlash(relativePath)))
			currentInfo, err := os.Stat(currentLocalPath)
			if err != nil {
				return err
			}
			if currentInfo.IsDir() {
				fs.addDir(remoteTarget)
				return nil
			}

			data, err := os.ReadFile(currentLocalPath)
			if err != nil {
				return err
			}
			return fs.WriteFile(remoteTarget, data)
		})
	}

	data, err := os.ReadFile(req.LocalPath)
	if err != nil {
		return err
	}
	return fs.WriteFile(req.RemotePath, data)
}

func (fs *fakeRemoteFS) Download(req sshclient.TransferRequest) error {
	target := cleanRemote(req.RemotePath)
	node, ok := fs.nodes[target]
	if !ok {
		return os.ErrNotExist
	}
	return os.WriteFile(req.LocalPath, node.content, 0644)
}

func (fs *fakeRemoteFS) Close() error { return nil }

func (fs *fakeRemoteFS) addDir(name string) {
	fs.nodes[cleanRemote(name)] = &fakeRemoteNode{
		isDir:   true,
		mode:    os.ModeDir | 0755,
		modTime: time.Now(),
	}
}

func (fs *fakeRemoteFS) addFile(name, content string, mode os.FileMode) {
	fs.nodes[cleanRemote(name)] = &fakeRemoteNode{
		content: []byte(content),
		mode:    mode,
		modTime: time.Now(),
	}
}

func (fs *fakeRemoteFS) record(operation string) {
	fs.operations = append(fs.operations, operation)
	if fs.recordFn != nil {
		fs.recordFn(operation)
	}
}

func (fs *fakeRemoteFS) entry(fullPath string, node *fakeRemoteNode) sshclient.RemoteEntry {
	return sshclient.RemoteEntry{
		Name:    path.Base(fullPath),
		Path:    fullPath,
		Size:    int64(len(node.content)),
		Mode:    node.mode,
		ModTime: node.modTime,
		UID:     node.uid,
		GID:     node.gid,
		IsDir:   node.isDir,
	}
}

func cleanRemote(name string) string {
	return path.Clean("/" + strings.TrimPrefix(name, "/"))
}

func cloneNodeMap(src map[string]*fakeRemoteNode) map[string]*fakeRemoteNode {
	clone := make(map[string]*fakeRemoteNode, len(src))
	for key, value := range src {
		clone[key] = value
	}
	return clone
}
