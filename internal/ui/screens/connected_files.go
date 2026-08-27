package screens

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"sshpilot/internal/config"
	"sshpilot/internal/scripts"
	sshclient "sshpilot/internal/ssh"
	"sshpilot/internal/ui/components"
	"sshpilot/internal/ui/theme"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type connectedLeftMode int

const (
	leftModeScripts connectedLeftMode = iota
	leftModeFiles
)

type fileDialogKind int

const (
	fileDialogCreateFile fileDialogKind = iota
	fileDialogCreateDir
	fileDialogRename
	fileDialogUpload
	fileDialogDownload
	fileDialogPermissions
	fileDialogConfirmDelete
	fileDialogConfirmOverwrite
	fileDialogConfirmDiscard
	fileDialogConfirmRun
)

type fileActionKind int

const (
	fileActionCreateFile fileActionKind = iota
	fileActionCreateDir
	fileActionSave
	fileActionRename
	fileActionDelete
	fileActionPermissions
	fileActionUpload
	fileActionDownload
	fileActionDiscardEditor
	fileActionRun
)

type fileBrowserState struct {
	fs           sshclient.RemoteFS
	initialized  bool
	initializing bool
	loading      bool
	busy         bool

	currentPath string
	entries     []sshclient.RemoteEntry
	cursor      int

	preview     *sshclient.FilePreview
	previewPath string

	editor         textarea.Model
	editorActive   bool
	editorPath     string
	editorOriginal string

	dialog *fileDialogState

	statusMessage string
	errorMessage  string
}

type fileDialogState struct {
	kind         fileDialogKind
	title        string
	message      string
	labels       []string
	inputs       []textinput.Model
	focusIndex   int
	confirmLabel string
	errorMessage string
	pending      filePendingAction
}

type filePendingAction struct {
	kind       fileActionKind
	path       string
	target     string
	content    string
	localPath  string
	remotePath string
	overwrite  bool
	mode       os.FileMode
	uid        int
	gid        int
	hasMode    bool
	hasOwner   bool
}

type fileFSReadyMsg struct {
	fs       sshclient.RemoteFS
	startDir string
	err      error
}

type fileDirLoadedMsg struct {
	path    string
	entries []sshclient.RemoteEntry
	err     error
}

type filePreviewLoadedMsg struct {
	preview sshclient.FilePreview
	err     error
}

type fileActionDoneMsg struct {
	action        fileActionKind
	status        string
	reloadDir     string
	reloadPreview string
	clearPreview  bool
	err           error
}

type fileOverwriteRequiredMsg struct {
	title   string
	message string
	pending filePendingAction
}

func newConnectedModelWithRuntime(server config.ServerConfig, runtime sshRuntime) ConnectedModel {
	if runtime == nil {
		runtime = defaultSSHRuntime{}
	}

	pkgs, _ := scripts.ListPackages()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = theme.SpinnerStyle

	editor := textarea.New()
	editor.Prompt = ""
	editor.ShowLineNumbers = true
	editor.CharLimit = 0
	editor.SetHeight(12)
	editor.SetWidth(60)

	m := ConnectedModel{
		server:        server,
		spinner:       sp,
		focus:         panelScripts,
		leftMode:      leftModeScripts,
		connChecking:  true,
		connection:    runtime.NewConnection(server),
		goBuild:       defaultGoBuildPlanner,
		localTerminal: startLocalTerminalSession,
		localPicker:   defaultLocalPathPicker{},
		files: fileBrowserState{
			editor: editor,
		},
	}

	for _, pkg := range pkgs {
		m.entries = append(m.entries, scriptEntry{isPackage: true, pkg: pkg})
	}
	if len(m.entries) > 0 {
		m.toggleExpand(0)
	}

	m.appendConsole(theme.SuccessStyle.Render(fmt.Sprintf("⚡ Подключено к: %s (%s)",
		server.Name, formatServerTarget(server))))
	m.appendConsole(theme.MutedStyle.Render("   Выберите скрипты слева и нажмите Enter для запуска"))
	m.appendConsole(theme.MutedStyle.Render("   F — переключить режим Скрипты/Файлы"))
	m.appendConsole(theme.MutedStyle.Render("   T — открыть native terminal сервера"))
	m.appendConsole("")

	return m
}

func (m ConnectedModel) IsBusy() bool {
	return m.executing || m.files.busy
}

func (m ConnectedModel) CapturesTextInput() bool {
	return m.files.dialog != nil || m.files.editorActive
}

func (m *ConnectedModel) Close() {
	if m.files.fs != nil {
		_ = m.files.fs.Close()
		m.files.fs = nil
	}
	m.closeTerminalTabsImmediate()
	if m.connection != nil {
		_ = m.connection.Close()
	}
}

func (m *ConnectedModel) updateFileEditorSize() {
	width := max(20, m.width/2-6)
	if m.isCompact() {
		width = max(20, m.width-8)
	}
	height := max(8, m.height-12)
	m.files.editor.SetWidth(width)
	m.files.editor.SetHeight(height)
}

func (m *ConnectedModel) ensureFileMode() tea.Cmd {
	m.leftMode = leftModeFiles
	m.files.errorMessage = ""
	m.files.statusMessage = ""
	if m.isCompact() {
		m.focus = panelScripts
	}
	if m.files.initialized || m.files.initializing {
		return nil
	}
	m.files.initializing = true
	return openRemoteFSCmd(m.connection)
}

func openRemoteFSCmd(connection sshConnection) tea.Cmd {
	return func() tea.Msg {
		fs, startDir, err := openRemoteFSWithRetry(connection)
		if err != nil {
			return fileFSReadyMsg{err: err}
		}
		return fileFSReadyMsg{fs: fs, startDir: startDir}
	}
}

func openRemoteFSWithRetry(connection sshConnection) (sshclient.RemoteFS, string, error) {
	open := func() (sshclient.RemoteFS, string, error) {
		fs, err := runGoLaunchStepValue(connection, "Открытие SFTP-сессии", goLaunchOpenFSTimeout, func() (sshclient.RemoteFS, error) {
			fs, openErr := connection.OpenRemoteFS()
			if openErr != nil {
				return nil, openErr
			}
			if fs == nil {
				return nil, fmt.Errorf("ssh runtime вернул пустую SFTP-сессию")
			}
			return fs, nil
		})
		if err != nil {
			return nil, "", err
		}
		return fs, fs.StartDir(), nil
	}

	fs, startDir, err := open()
	if err == nil {
		return fs, startDir, nil
	}

	firstErr := err
	if connection != nil {
		_ = connection.Reset()
	}

	fs, startDir, retryErr := open()
	if retryErr == nil {
		return fs, startDir, nil
	}

	return nil, "", fmt.Errorf(
		"не удалось открыть файловую систему сервера после повторной попытки: сначала %v, затем %w",
		firstErr,
		retryErr,
	)
}

func listRemoteDirCmd(fs sshclient.RemoteFS, dir string) tea.Cmd {
	return func() tea.Msg {
		entries, err := fs.ListDir(dir)
		return fileDirLoadedMsg{path: dir, entries: entries, err: err}
	}
}

func loadRemotePreviewCmd(fs sshclient.RemoteFS, name string) tea.Cmd {
	return func() tea.Msg {
		preview, err := fs.ReadFile(name, sshclient.FilePreviewLimit)
		return filePreviewLoadedMsg{preview: preview, err: err}
	}
}

func runFileActionCmd(fs sshclient.RemoteFS, action filePendingAction) tea.Cmd {
	return func() tea.Msg {
		switch action.kind {
		case fileActionCreateFile:
			exists, err := remoteEntryExists(fs, action.path)
			if err != nil {
				return fileActionDoneMsg{action: action.kind, err: err}
			}
			if exists {
				return fileActionDoneMsg{action: action.kind, err: fmt.Errorf("файл %s уже существует", action.path)}
			}
			if err := fs.WriteFile(action.path, []byte{}); err != nil {
				return fileActionDoneMsg{action: action.kind, err: err}
			}
			return fileActionDoneMsg{
				action:        action.kind,
				status:        "Файл создан",
				reloadDir:     remoteParentPath(action.path),
				reloadPreview: action.path,
			}

		case fileActionCreateDir:
			exists, err := remoteEntryExists(fs, action.path)
			if err != nil {
				return fileActionDoneMsg{action: action.kind, err: err}
			}
			if exists {
				return fileActionDoneMsg{action: action.kind, err: fmt.Errorf("директория %s уже существует", action.path)}
			}
			if err := fs.Mkdir(action.path); err != nil {
				return fileActionDoneMsg{action: action.kind, err: err}
			}
			return fileActionDoneMsg{
				action:    action.kind,
				status:    "Директория создана",
				reloadDir: remoteParentPath(action.path),
			}

		case fileActionSave:
			if err := fs.WriteFile(action.path, []byte(action.content)); err != nil {
				return fileActionDoneMsg{action: action.kind, err: err}
			}
			return fileActionDoneMsg{
				action:        action.kind,
				status:        "Файл сохранён",
				reloadDir:     remoteParentPath(action.path),
				reloadPreview: action.path,
			}

		case fileActionRename:
			exists, err := remoteEntryExists(fs, action.target)
			if err != nil {
				return fileActionDoneMsg{action: action.kind, err: err}
			}
			if exists && !action.overwrite {
				return fileOverwriteRequiredMsg{
					title:   "Перезаписать цель?",
					message: fmt.Sprintf("Удалить %s и переименовать %s?", action.target, action.path),
					pending: action,
				}
			}
			if exists && action.overwrite {
				if err := fs.Remove(action.target); err != nil {
					return fileActionDoneMsg{action: action.kind, err: err}
				}
			}
			if err := fs.Rename(action.path, action.target); err != nil {
				return fileActionDoneMsg{action: action.kind, err: err}
			}
			return fileActionDoneMsg{
				action:        action.kind,
				status:        "Имя обновлено",
				reloadDir:     remoteParentPath(action.target),
				reloadPreview: action.target,
			}

		case fileActionDelete:
			if err := fs.Remove(action.path); err != nil {
				return fileActionDoneMsg{action: action.kind, err: err}
			}
			return fileActionDoneMsg{
				action:       action.kind,
				status:       "Элемент удалён",
				reloadDir:    remoteParentPath(action.path),
				clearPreview: true,
			}

		case fileActionPermissions:
			if action.hasMode {
				if err := fs.Chmod(action.path, action.mode); err != nil {
					return fileActionDoneMsg{action: action.kind, err: err}
				}
			}
			if action.hasOwner {
				if err := fs.Chown(action.path, action.uid, action.gid); err != nil {
					return fileActionDoneMsg{action: action.kind, err: err}
				}
			}
			return fileActionDoneMsg{
				action:        action.kind,
				status:        "Права и владелец обновлены",
				reloadDir:     remoteParentPath(action.path),
				reloadPreview: action.path,
			}

		case fileActionUpload:
			localInfo, err := localPathInfo(action.localPath)
			if err != nil {
				return fileActionDoneMsg{
					action: action.kind,
					err:    fmt.Errorf("не удалось прочитать локальный путь %s: %w", action.localPath, err),
				}
			}
			exists, err := remoteEntryExists(fs, action.remotePath)
			if err != nil {
				return fileActionDoneMsg{action: action.kind, err: err}
			}
			if exists && !action.overwrite {
				title := "Перезаписать удалённый файл?"
				message := fmt.Sprintf("Файл %s уже существует. Перезаписать его загрузкой?", action.remotePath)
				if localInfo.IsDir() {
					title = "Обновить удалённую папку?"
					message = fmt.Sprintf("Папка %s уже существует. Обновить её содержимое загрузкой?", action.remotePath)
				}
				return fileOverwriteRequiredMsg{
					title:   title,
					message: message,
					pending: action,
				}
			}
			if err := fs.Upload(sshclient.TransferRequest{
				LocalPath:  action.localPath,
				RemotePath: action.remotePath,
				Overwrite:  action.overwrite,
			}); err != nil {
				return fileActionDoneMsg{action: action.kind, err: err}
			}
			status := "Файл загружен на сервер"
			reloadPreview := action.remotePath
			if localInfo.IsDir() {
				status = "Папка загружена на сервер"
				reloadPreview = ""
			}
			return fileActionDoneMsg{
				action:        action.kind,
				status:        status,
				reloadDir:     remoteParentPath(action.remotePath),
				reloadPreview: reloadPreview,
			}

		case fileActionDownload:
			if localPathExists(action.localPath) && !action.overwrite {
				return fileOverwriteRequiredMsg{
					title:   "Перезаписать локальный файл?",
					message: fmt.Sprintf("Файл %s уже существует локально. Перезаписать его скачиванием?", action.localPath),
					pending: action,
				}
			}
			if err := fs.Download(sshclient.TransferRequest{
				LocalPath:  action.localPath,
				RemotePath: action.remotePath,
				Overwrite:  action.overwrite,
			}); err != nil {
				return fileActionDoneMsg{action: action.kind, err: err}
			}
			return fileActionDoneMsg{
				action:        action.kind,
				status:        "Файл скачан локально",
				reloadPreview: action.remotePath,
			}
		}

		return fileActionDoneMsg{action: action.kind, err: fmt.Errorf("неизвестное файловое действие")}
	}
}

func remoteEntryExists(fs sshclient.RemoteFS, name string) (bool, error) {
	_, err := fs.Stat(name)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "no such file") || strings.Contains(msg, "not exist") {
		return false, nil
	}
	return false, err
}

func localPathExists(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	_, err := os.Stat(filepath.Clean(name))
	return err == nil
}

func localPathInfo(name string) (os.FileInfo, error) {
	if strings.TrimSpace(name) == "" {
		return nil, os.ErrNotExist
	}
	return os.Stat(filepath.Clean(name))
}

func remoteParentPath(name string) string {
	clean := path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(name), "/"))
	if clean == "/" {
		return "/"
	}
	parent := path.Dir(clean)
	if parent == "." || parent == "" {
		return "/"
	}
	return parent
}

func joinRemoteName(dir, name string) string {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return dir
	}
	if strings.HasPrefix(cleanName, "/") {
		return path.Clean(cleanName)
	}
	return path.Join(dir, cleanName)
}

func suggestRemoteUploadPath(currentPath, localPath string) string {
	cleanLocalPath := strings.TrimSpace(localPath)
	if cleanLocalPath == "" {
		return currentPath
	}

	baseName := filepath.Base(filepath.Clean(cleanLocalPath))
	if baseName == "." || baseName == string(os.PathSeparator) || baseName == "" {
		return currentPath
	}

	return joinRemoteName(currentPath, baseName)
}

func (m *ConnectedModel) handleFileMessage(msg tea.Msg) (ConnectedModel, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case fileFSReadyMsg:
		m.files.initializing = false
		if msg.err != nil {
			m.files.errorMessage = fmt.Sprintf("Не удалось открыть файловый менеджер: %v", msg.err)
			return *m, nil, true
		}
		m.files.fs = msg.fs
		m.files.initialized = true
		m.files.currentPath = msg.startDir
		m.files.statusMessage = "Файловый режим подключён"
		m.files.loading = true
		return *m, listRemoteDirCmd(msg.fs, msg.startDir), true

	case fileDirLoadedMsg:
		m.files.loading = false
		if msg.err != nil {
			m.files.errorMessage = msg.err.Error()
			return *m, nil, true
		}

		prevSelected := ""
		if entry := m.currentFileEntry(); entry != nil {
			prevSelected = entry.Path
		}

		m.files.currentPath = msg.path
		m.files.entries = msg.entries
		if len(m.files.entries) == 0 {
			m.files.cursor = 0
		} else {
			if prevSelected != "" {
				for i := range m.files.entries {
					if m.files.entries[i].Path == prevSelected {
						m.files.cursor = i
						break
					}
				}
			}
			if m.files.cursor >= len(m.files.entries) {
				m.files.cursor = len(m.files.entries) - 1
			}
			if m.files.cursor < 0 {
				m.files.cursor = 0
			}
		}

		if m.files.preview != nil && m.files.preview.Path != m.currentFilePreviewTarget() {
			m.files.preview = nil
			m.files.previewPath = ""
		}
		return *m, nil, true

	case filePreviewLoadedMsg:
		m.files.loading = false
		if msg.err != nil {
			m.files.errorMessage = msg.err.Error()
			return *m, nil, true
		}
		m.files.preview = &msg.preview
		m.files.previewPath = msg.preview.Path
		if msg.preview.Truncated {
			m.files.statusMessage = "Показано только начало файла (до 256 KiB)"
		} else if !msg.preview.IsText {
			m.files.statusMessage = "Бинарный файл: доступно только мета-превью"
		} else {
			m.files.statusMessage = ""
		}
		return *m, nil, true

	case fileOverwriteRequiredMsg:
		m.files.busy = false
		m.files.dialog = newConfirmDialog(fileDialogConfirmOverwrite, msg.title, msg.message, "перезаписать", msg.pending)
		return *m, nil, true

	case fileLocalPathPickedMsg:
		if m.files.dialog == nil || m.files.dialog.kind != fileDialogUpload {
			return *m, nil, true
		}
		if msg.err != nil {
			m.files.dialog.errorMessage = msg.err.Error()
			return *m, nil, true
		}
		if strings.TrimSpace(msg.path) == "" {
			return *m, nil, true
		}

		dialog := m.files.dialog
		previousLocalPath := dialog.inputs[0].Value()
		previousSuggestedRemotePath := suggestRemoteUploadPath(m.files.currentPath, previousLocalPath)
		currentRemotePath := strings.TrimSpace(dialog.inputs[1].Value())

		dialog.inputs[0].SetValue(msg.path)
		if currentRemotePath == "" || currentRemotePath == previousSuggestedRemotePath {
			dialog.inputs[1].SetValue(suggestRemoteUploadPath(m.files.currentPath, msg.path))
		}
		dialog.errorMessage = ""
		dialog.focusIndex = min(1, len(dialog.inputs)-1)
		return *m, focusDialogInput(dialog), true

	case fileActionDoneMsg:
		m.files.busy = false
		if msg.err != nil {
			m.files.errorMessage = msg.err.Error()
			return *m, nil, true
		}
		m.files.statusMessage = msg.status
		m.files.errorMessage = ""
		if msg.action == fileActionSave {
			m.files.editorOriginal = m.files.editor.Value()
		}
		if msg.clearPreview {
			m.files.preview = nil
			m.files.previewPath = ""
		}

		var cmds []tea.Cmd
		if msg.reloadDir != "" && m.files.fs != nil {
			m.files.loading = true
			cmds = append(cmds, listRemoteDirCmd(m.files.fs, msg.reloadDir))
		}
		if msg.reloadPreview != "" && m.files.fs != nil {
			m.files.loading = true
			cmds = append(cmds, loadRemotePreviewCmd(m.files.fs, msg.reloadPreview))
			if msg.action == fileActionRename {
				m.files.editorPath = msg.reloadPreview
			}
		}
		return *m, tea.Batch(cmds...), true
	}

	return *m, nil, false
}

func (m ConnectedModel) currentFileEntry() *sshclient.RemoteEntry {
	if len(m.files.entries) == 0 || m.files.cursor < 0 || m.files.cursor >= len(m.files.entries) {
		return nil
	}
	return &m.files.entries[m.files.cursor]
}

func (m ConnectedModel) currentFilePreviewTarget() string {
	if m.files.editorActive && m.files.editorPath != "" {
		return m.files.editorPath
	}
	if entry := m.currentFileEntry(); entry != nil && !entry.IsDir {
		return entry.Path
	}
	return ""
}

func fileEntryExecutable(entry sshclient.RemoteEntry) bool {
	return entry.Mode.Perm()&0o111 != 0
}

func (m *ConnectedModel) startFileRunnable(entry sshclient.RemoteEntry, ensureExec bool) tea.Cmd {
	script := scripts.Script{
		Name:       entry.Name,
		Package:    "files",
		Kind:       scripts.ScriptKindBinary,
		RemotePath: entry.Path,
	}
	if ensureExec {
		script.Chmod = 0o755
	}
	m.files.errorMessage = ""
	m.files.statusMessage = ""
	return m.startRunnerQueue([]scripts.Script{script})
}

func (m *ConnectedModel) clearPreviewIfSelectionChanged() {
	if m.files.editorActive {
		return
	}
	target := m.currentFilePreviewTarget()
	if target == "" || target != m.files.previewPath {
		m.files.preview = nil
		m.files.previewPath = ""
	}
}

func (m *ConnectedModel) updateFileKeys(msg tea.KeyMsg) (ConnectedModel, tea.Cmd) {
	if handled, cmd := m.updateFileDialogKeys(msg); handled {
		return *m, cmd
	}

	if m.files.editorActive && m.focus == panelConsole {
		switch msg.String() {
		case "ctrl+s":
			m.files.dialog = newConfirmDialog(
				fileDialogConfirmOverwrite,
				"Сохранить изменения?",
				fmt.Sprintf("Перезаписать %s содержимым редактора?", m.files.editorPath),
				"сохранить",
				filePendingAction{
					kind:    fileActionSave,
					path:    m.files.editorPath,
					content: m.files.editor.Value(),
				},
			)
			return *m, nil
		case "esc":
			if m.files.editor.Value() != m.files.editorOriginal {
				m.files.dialog = newConfirmDialog(
					fileDialogConfirmDiscard,
					"Отменить изменения?",
					"Есть несохранённые изменения. Закрыть редактор без сохранения?",
					"закрыть",
					filePendingAction{kind: fileActionDiscardEditor},
				)
				return *m, nil
			}
			m.closeFileEditor()
			return *m, nil
		}
	}

	if m.files.busy || m.files.loading || m.files.initializing {
		if msg.String() == "esc" {
			m.files.statusMessage = "Дождитесь завершения файловой операции"
			return *m, nil
		}
		return *m, nil
	}

	if m.files.editorActive && m.focus == panelConsole {
		if msg.String() == "tab" {
			m.focus = panelScripts
			m.files.editor.Blur()
			return *m, nil
		}
		var cmd tea.Cmd
		m.files.editor, cmd = m.files.editor.Update(msg)
		return *m, cmd
	}

	switch msg.String() {
	case "esc":
		if m.files.editorActive {
			if m.files.editor.Value() != m.files.editorOriginal {
				m.files.dialog = newConfirmDialog(
					fileDialogConfirmDiscard,
					"Отменить изменения?",
					"Есть несохранённые изменения. Закрыть редактор без сохранения?",
					"закрыть",
					filePendingAction{kind: fileActionDiscardEditor},
				)
				return *m, nil
			}
			m.closeFileEditor()
			return *m, nil
		}
		return *m, func() tea.Msg { return ConnectedBackMsg{} }

	case "tab":
		if !m.isCompact() || m.leftMode == leftModeFiles {
			if m.focus == panelScripts {
				m.focus = panelConsole
				if m.files.editorActive {
					return *m, m.files.editor.Focus()
				}
			} else {
				m.focus = panelScripts
				m.files.editor.Blur()
			}
		}
		return *m, nil

	case "backspace":
		if m.focus == panelScripts {
			parent := remoteParentPath(m.files.currentPath)
			if parent != m.files.currentPath && m.files.fs != nil {
				m.files.loading = true
				m.files.preview = nil
				m.files.previewPath = ""
				m.files.currentPath = parent
				return *m, listRemoteDirCmd(m.files.fs, parent)
			}
		}
		return *m, nil

	case "f":
		m.leftMode = leftModeScripts
		m.focus = panelScripts
		m.files.editor.Blur()
		return *m, nil

	case "t":
		next, cmd := m.openOrSelectServerTab()
		return next, cmd

	case "enter":
		if m.focus != panelScripts {
			if m.files.preview != nil && m.files.preview.Editable {
				return m.startFileEditor()
			}
			return *m, nil
		}
		entry := m.currentFileEntry()
		if entry == nil || m.files.fs == nil {
			return *m, nil
		}
		if entry.IsDir {
			m.files.loading = true
			m.files.currentPath = entry.Path
			m.files.preview = nil
			m.files.previewPath = ""
			return *m, listRemoteDirCmd(m.files.fs, entry.Path)
		}
		m.files.loading = true
		return *m, loadRemotePreviewCmd(m.files.fs, entry.Path)

	case "e":
		if m.files.preview != nil && m.files.preview.Editable {
			return m.startFileEditor()
		}
		m.files.errorMessage = "Редактирование доступно только для текстовых файлов до 256 KiB"
		return *m, nil

	case "n":
		m.files.dialog = newSingleInputDialog(
			fileDialogCreateFile,
			"Создать файл",
			"Имя или путь нового файла",
			"например notes.txt",
			"",
			"создать",
		)
		return *m, m.files.dialog.inputs[0].Focus()

	case "N":
		m.files.dialog = newSingleInputDialog(
			fileDialogCreateDir,
			"Создать директорию",
			"Имя или путь новой директории",
			"например logs",
			"",
			"создать",
		)
		return *m, m.files.dialog.inputs[0].Focus()

	case "r":
		entry := m.currentFileEntry()
		if entry == nil {
			return *m, nil
		}
		m.files.dialog = newSingleInputDialog(
			fileDialogRename,
			"Переименовать",
			fmt.Sprintf("Новое имя для %s", entry.Name),
			"новое имя",
			entry.Name,
			"применить",
		)
		m.files.dialog.pending = filePendingAction{kind: fileActionRename, path: entry.Path}
		return *m, m.files.dialog.inputs[0].Focus()

	case "d":
		entry := m.currentFileEntry()
		if entry == nil {
			return *m, nil
		}
		m.files.dialog = newConfirmDialog(
			fileDialogConfirmDelete,
			"Подтвердите удаление",
			fmt.Sprintf("Удалить %s?", entry.Path),
			"удалить",
			filePendingAction{kind: fileActionDelete, path: entry.Path},
		)
		return *m, nil

	case "p":
		entry := m.currentFileEntry()
		if entry == nil {
			return *m, nil
		}
		m.files.dialog = newPermissionsDialog(*entry)
		return *m, m.files.dialog.inputs[0].Focus()

	case "u":
		m.files.dialog = newUploadDialog(m.files.currentPath)
		return *m, m.files.dialog.inputs[0].Focus()

	case "o":
		entry := m.currentFileEntry()
		if entry == nil || entry.IsDir {
			m.files.errorMessage = "Скачивание доступно только для файлов"
			return *m, nil
		}
		m.files.dialog = newSingleInputDialog(
			fileDialogDownload,
			"Скачать файл",
			"Локальный путь для сохранения",
			"например C:\\temp\\"+entry.Name,
			entry.Name,
			"скачать",
		)
		m.files.dialog.pending = filePendingAction{kind: fileActionDownload, remotePath: entry.Path}
		return *m, m.files.dialog.inputs[0].Focus()

	case "x":
		entry := m.currentFileEntry()
		if entry == nil || entry.IsDir {
			m.files.errorMessage = "Запуск доступен только для файлов"
			return *m, nil
		}
		if fileEntryExecutable(*entry) {
			return *m, m.startFileRunnable(*entry, false)
		}
		m.files.dialog = newConfirmDialog(
			fileDialogConfirmRun,
			"Выдать права и запустить?",
			fmt.Sprintf("Файл %s не отмечен как executable. Выдать mode 755 и запустить его?", entry.Path),
			"chmod+run",
			filePendingAction{
				kind:    fileActionRun,
				path:    entry.Path,
				mode:    0o755,
				hasMode: true,
			},
		)
		return *m, nil

	case "up", "k":
		if m.focus == panelScripts && m.files.cursor > 0 {
			m.files.cursor--
			m.clearPreviewIfSelectionChanged()
		}
		return *m, nil

	case "down", "j":
		if m.focus == panelScripts && m.files.cursor < len(m.files.entries)-1 {
			m.files.cursor++
			m.clearPreviewIfSelectionChanged()
		}
		return *m, nil
	}

	return *m, nil
}

func (m *ConnectedModel) updateFileDialogKeys(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.files.dialog == nil {
		return false, nil
	}

	dialog := m.files.dialog

	switch msg.String() {
	case "esc":
		m.files.dialog = nil
		return true, nil
	case "ctrl+o":
		if dialog.kind == fileDialogUpload {
			dialog.errorMessage = ""
			return true, pickLocalPathCmd(m.localPicker, localPickFile, dialog.inputs[0].Value())
		}
	case "ctrl+g":
		if dialog.kind == fileDialogUpload {
			dialog.errorMessage = ""
			return true, pickLocalPathCmd(m.localPicker, localPickFolder, dialog.inputs[0].Value())
		}
	case "tab":
		if len(dialog.inputs) > 1 {
			dialog.focusIndex = (dialog.focusIndex + 1) % len(dialog.inputs)
			return true, focusDialogInput(dialog)
		}
		return true, nil
	case "shift+tab":
		if len(dialog.inputs) > 1 {
			dialog.focusIndex = (dialog.focusIndex - 1 + len(dialog.inputs)) % len(dialog.inputs)
			return true, focusDialogInput(dialog)
		}
		return true, nil
	case "enter":
		return true, m.confirmFileDialog()
	}

	if len(dialog.inputs) == 0 {
		return true, nil
	}

	var cmd tea.Cmd
	dialog.inputs[dialog.focusIndex], cmd = dialog.inputs[dialog.focusIndex].Update(msg)
	return true, cmd
}

func focusDialogInput(dialog *fileDialogState) tea.Cmd {
	cmds := make([]tea.Cmd, len(dialog.inputs))
	for i := range dialog.inputs {
		if i == dialog.focusIndex {
			cmds[i] = dialog.inputs[i].Focus()
		} else {
			dialog.inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (m *ConnectedModel) confirmFileDialog() tea.Cmd {
	if m.files.dialog == nil {
		return nil
	}

	dialog := m.files.dialog
	pending := dialog.pending

	switch dialog.kind {
	case fileDialogCreateFile:
		name := strings.TrimSpace(dialog.inputs[0].Value())
		if name == "" {
			dialog.errorMessage = "Укажите имя файла"
			return nil
		}
		pending = filePendingAction{kind: fileActionCreateFile, path: joinRemoteName(m.files.currentPath, name)}

	case fileDialogCreateDir:
		name := strings.TrimSpace(dialog.inputs[0].Value())
		if name == "" {
			dialog.errorMessage = "Укажите имя директории"
			return nil
		}
		pending = filePendingAction{kind: fileActionCreateDir, path: joinRemoteName(m.files.currentPath, name)}

	case fileDialogRename:
		name := strings.TrimSpace(dialog.inputs[0].Value())
		if name == "" {
			dialog.errorMessage = "Укажите новое имя"
			return nil
		}
		pending.target = joinRemoteName(remoteParentPath(dialog.pending.path), name)
		if pending.target == dialog.pending.path {
			m.files.dialog = nil
			m.files.statusMessage = "Имя не изменилось"
			return nil
		}

	case fileDialogUpload:
		localPath := strings.TrimSpace(dialog.inputs[0].Value())
		remotePath := strings.TrimSpace(dialog.inputs[1].Value())
		if localPath == "" {
			dialog.errorMessage = "Укажите локальный путь"
			return nil
		}
		if _, err := localPathInfo(localPath); err != nil {
			dialog.errorMessage = fmt.Sprintf("Локальный путь недоступен: %v", err)
			return nil
		}
		if remotePath == "" {
			remotePath = suggestRemoteUploadPath(m.files.currentPath, localPath)
		}
		pending = filePendingAction{
			kind:       fileActionUpload,
			localPath:  localPath,
			remotePath: remotePath,
			overwrite:  dialog.pending.overwrite,
		}

	case fileDialogDownload:
		localPath := strings.TrimSpace(dialog.inputs[0].Value())
		if localPath == "" {
			dialog.errorMessage = "Укажите локальный путь"
			return nil
		}
		pending.localPath = localPath

	case fileDialogPermissions:
		modeValue := strings.TrimSpace(dialog.inputs[0].Value())
		uidValue := strings.TrimSpace(dialog.inputs[1].Value())
		gidValue := strings.TrimSpace(dialog.inputs[2].Value())
		if modeValue != "" {
			parsed, err := strconv.ParseUint(modeValue, 8, 32)
			if err != nil {
				dialog.errorMessage = "Mode нужно указывать в восьмеричном виде, например 644"
				return nil
			}
			pending.mode = os.FileMode(parsed)
			pending.hasMode = true
		}
		if uidValue != "" || gidValue != "" {
			uid, err := strconv.Atoi(defaultString(uidValue, strconv.Itoa(dialog.pending.uid)))
			if err != nil {
				dialog.errorMessage = "UID должен быть числом"
				return nil
			}
			gid, err := strconv.Atoi(defaultString(gidValue, strconv.Itoa(dialog.pending.gid)))
			if err != nil {
				dialog.errorMessage = "GID должен быть числом"
				return nil
			}
			pending.uid = uid
			pending.gid = gid
			pending.hasOwner = true
		}
		if !pending.hasMode && !pending.hasOwner {
			dialog.errorMessage = "Укажите хотя бы mode или owner/group"
			return nil
		}

	case fileDialogConfirmDelete, fileDialogConfirmOverwrite:
		pending.overwrite = true

	case fileDialogConfirmDiscard:
		m.files.dialog = nil
		m.closeFileEditor()
		m.files.statusMessage = "Редактор закрыт без сохранения"
		return nil

	case fileDialogConfirmRun:
		m.files.dialog = nil
		return m.startFileRunnable(sshclient.RemoteEntry{
			Name: path.Base(pending.path),
			Path: pending.path,
			Mode: pending.mode,
		}, pending.hasMode)
	}

	m.files.dialog = nil
	m.files.errorMessage = ""
	m.files.statusMessage = ""
	if m.files.fs == nil {
		m.files.errorMessage = "Файловое соединение не инициализировано"
		return nil
	}
	m.files.busy = true
	return runFileActionCmd(m.files.fs, pending)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (m *ConnectedModel) startFileEditor() (ConnectedModel, tea.Cmd) {
	if m.files.preview == nil || !m.files.preview.Editable {
		m.files.errorMessage = "Редактирование доступно только для текстовых файлов до 256 KiB"
		return *m, nil
	}

	m.files.editor.SetValue(m.files.preview.Content)
	m.files.editor.CursorEnd()
	m.files.editorOriginal = m.files.preview.Content
	m.files.editorPath = m.files.preview.Path
	m.files.editorActive = true
	m.focus = panelConsole
	return *m, m.files.editor.Focus()
}

func (m *ConnectedModel) closeFileEditor() {
	m.files.editorActive = false
	m.files.editorPath = ""
	m.files.editor.Blur()
}

func newTextInput(placeholder, value string) textinput.Model {
	input := textinput.New()
	input.CharLimit = 1024
	input.Width = 48
	input.Placeholder = placeholder
	input.SetValue(value)
	return input
}

func newSingleInputDialog(kind fileDialogKind, title, message, placeholder, value, confirm string) *fileDialogState {
	return &fileDialogState{
		kind:         kind,
		title:        title,
		message:      message,
		labels:       []string{"Значение"},
		inputs:       []textinput.Model{newTextInput(placeholder, value)},
		confirmLabel: confirm,
	}
}

func newConfirmDialog(kind fileDialogKind, title, message, confirm string, pending filePendingAction) *fileDialogState {
	return &fileDialogState{
		kind:         kind,
		title:        title,
		message:      message,
		confirmLabel: confirm,
		pending:      pending,
	}
}

func newUploadDialog(currentPath string) *fileDialogState {
	return &fileDialogState{
		kind:         fileDialogUpload,
		title:        "Загрузить файл или папку",
		message:      "Укажите локальный путь и целевой путь на сервере. Ctrl+O — выбрать файл, Ctrl+G — выбрать папку. Папки загружаются рекурсивно.",
		labels:       []string{"Локальный путь", "Удалённый путь"},
		inputs:       []textinput.Model{newTextInput("C:\\temp\\file.txt", ""), newTextInput(path.Join(currentPath, "file.txt"), "")},
		confirmLabel: "загрузить",
	}
}

func newPermissionsDialog(entry sshclient.RemoteEntry) *fileDialogState {
	modeValue := fmt.Sprintf("%o", entry.Mode.Perm())
	return &fileDialogState{
		kind:    fileDialogPermissions,
		title:   "Права и владелец",
		message: fmt.Sprintf("Изменить mode/owner/group для %s", entry.Path),
		labels:  []string{"Mode (octal)", "UID", "GID"},
		inputs: []textinput.Model{
			newTextInput("644", modeValue),
			newTextInput("0", strconv.Itoa(entry.UID)),
			newTextInput("0", strconv.Itoa(entry.GID)),
		},
		confirmLabel: "применить",
		pending: filePendingAction{
			kind: fileActionPermissions,
			path: entry.Path,
			uid:  entry.UID,
			gid:  entry.GID,
		},
	}
}

func (m ConnectedModel) renderFilesPanel(w, h int) string {
	var b strings.Builder

	title := theme.SubtitleStyle.Render("📁 Файлы")
	location := theme.MutedStyle.Render("  " + m.files.currentPath)
	b.WriteString(title + location + "\n\n")

	if m.files.initializing {
		b.WriteString(theme.SpinnerStyle.Render(m.spinner.View() + " Подключение к файловой системе..."))
		return b.String()
	}

	if m.files.loading {
		b.WriteString(theme.SpinnerStyle.Render(m.spinner.View()+" Загрузка содержимого...") + "\n\n")
	}

	if len(m.files.entries) == 0 {
		b.WriteString(theme.MutedStyle.Render("Папка пуста"))
		return b.String()
	}

	visibleLines := max(5, h-6)
	start := 0
	if m.files.cursor >= visibleLines {
		start = m.files.cursor - visibleLines + 1
	}
	end := min(len(m.files.entries), start+visibleLines)

	for i := start; i < end; i++ {
		entry := m.files.entries[i]
		cur := " "
		if i == m.files.cursor && m.focus == panelScripts {
			cur = theme.SelectedItemStyle.Render(theme.IconArrow)
		}

		icon := theme.IconScript
		style := theme.MutedStyle
		sizeText := theme.MutedStyle.Render(humanSize(entry.Size))
		if entry.IsDir {
			icon = theme.IconFolder
			sizeText = theme.MutedStyle.Render("dir")
			style = theme.ItemStyle
		}
		if i == m.files.cursor && m.focus == panelScripts {
			style = theme.SelectedItemStyle
		}

		b.WriteString(fmt.Sprintf("%s %s %-8s %s\n", cur, icon, sizeText, style.Render(entry.Name)))
	}

	return b.String()
}

func (m ConnectedModel) renderFileDetailPanel(w, _ int) string {
	var b strings.Builder

	title := "📄 Превью"
	if m.files.editorActive {
		title = "✏ Редактор"
	}
	b.WriteString(theme.SubtitleStyle.Render(title) + "\n\n")

	if m.files.errorMessage != "" {
		b.WriteString(theme.ErrorStyle.Render("❌ "+m.files.errorMessage) + "\n\n")
	}
	if m.files.statusMessage != "" {
		b.WriteString(theme.SuccessStyle.Render("ℹ "+m.files.statusMessage) + "\n\n")
	}

	if m.files.editorActive {
		b.WriteString(theme.MutedStyle.Render(m.files.editorPath) + "\n\n")
		b.WriteString(m.files.editor.View())
		return b.String()
	}

	entry := m.currentFileEntry()
	if entry == nil {
		b.WriteString(theme.MutedStyle.Render("Выберите файл или директорию слева"))
		return b.String()
	}

	b.WriteString(theme.MutedStyle.Render(entry.Path) + "\n")
	b.WriteString(theme.MutedStyle.Render(fmt.Sprintf(
		"mode %s  uid:%d gid:%d  изменён %s  размер %s",
		entry.Mode.String(), entry.UID, entry.GID, entry.ModTime.Format("2006-01-02 15:04"), humanSize(entry.Size),
	)) + "\n\n")

	if entry.IsDir {
		b.WriteString(theme.MutedStyle.Render("Enter - открыть директорию\nBackspace - подняться на уровень выше"))
		return b.String()
	}

	if m.files.preview == nil || m.files.preview.Path != entry.Path {
		b.WriteString(theme.MutedStyle.Render("Enter - загрузить превью файла\nE - открыть редактор, если файл текстовый\nX - запустить файл на сервере"))
		return b.String()
	}

	if !m.files.preview.IsText {
		b.WriteString(theme.WarningStyle.Render("Бинарный или неподдерживаемый текстовый файл.\nДоступны метаданные, запуск, скачивание, rename/delete, chmod/chown."))
		return b.String()
	}

	lineWidth := max(16, w-4)
	lines := strings.Split(m.files.preview.Content, "\n")
	for _, line := range lines {
		b.WriteString(truncateLine(line, lineWidth) + "\n")
	}
	if m.files.preview.Truncated {
		b.WriteString("\n" + theme.WarningStyle.Render("Файл слишком большой для полного превью"))
	}

	return strings.TrimRight(b.String(), "\n")
}

func humanSize(size int64) string {
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(size)/float64(div), "KMGTPE"[exp])
}

func truncateLine(line string, width int) string {
	if width <= 1 || lipgloss.Width(line) <= width {
		return line
	}
	runes := []rune(line)
	if len(runes) <= width {
		return line
	}
	return string(runes[:width-1]) + "…"
}

func (m ConnectedModel) renderFileDialog() string {
	if m.files.dialog == nil {
		return ""
	}

	dialog := m.files.dialog
	var b strings.Builder
	b.WriteString(theme.SubtitleStyle.Render(dialog.title) + "\n\n")
	b.WriteString(dialog.message + "\n")

	for i := range dialog.inputs {
		label := "Поле"
		if i < len(dialog.labels) {
			label = dialog.labels[i]
		}
		b.WriteString("\n" + theme.LabelStyle.Render(label) + "\n")
		b.WriteString(dialog.inputs[i].View() + "\n")
	}

	if dialog.errorMessage != "" {
		b.WriteString("\n" + theme.ErrorStyle.Render("❌ "+dialog.errorMessage))
	}

	footerItems := []components.StatusItem{
		{Key: "enter", Desc: dialog.confirmLabel},
		{Key: "tab", Desc: "след. поле"},
	}
	if dialog.kind == fileDialogUpload {
		footerItems = append(footerItems,
			components.StatusItem{Key: "ctrl+o", Desc: "файл"},
			components.StatusItem{Key: "ctrl+g", Desc: "папка"},
		)
	}
	footerItems = append(footerItems, components.StatusItem{Key: "esc", Desc: "отмена"})

	footer := components.RenderStatusBar(footerItems, max(40, m.width-6))

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorAccent).
		Padding(1, 2).
		Width(max(40, m.width-6)).
		Render(b.String() + "\n\n" + footer)
}

func (m ConnectedModel) scriptStatusItems(compact bool) []components.StatusItem {
	items := []components.StatusItem{}
	if !compact {
		items = append(items, components.StatusItem{Key: "tab", Desc: "панель"})
	}
	items = append(items, components.StatusItem{Key: "↑↓", Desc: "навигация"})

	if m.focus == panelScripts && !m.executing && !compact {
		items = append(items,
			components.StatusItem{Key: "space", Desc: "выбрать"},
			components.StatusItem{Key: "e", Desc: "развернуть"},
			components.StatusItem{Key: "enter", Desc: "запустить"},
		)
	}
	if m.focus == panelConsole {
		items = append(items, components.StatusItem{Key: "↑↓", Desc: "прокрутка"})
	}
	items = append(items,
		components.StatusItem{Key: "f", Desc: "файлы"},
		components.StatusItem{Key: "t", Desc: "терминал"},
		components.StatusItem{Key: "esc", Desc: "назад"},
	)
	return items
}

func (m ConnectedModel) fileStatusItems(compact bool) []components.StatusItem {
	if m.files.dialog != nil {
		return []components.StatusItem{
			{Key: "enter", Desc: m.files.dialog.confirmLabel},
			{Key: "tab", Desc: "след. поле"},
			{Key: "esc", Desc: "отмена"},
		}
	}

	items := []components.StatusItem{}
	if !compact {
		items = append(items, components.StatusItem{Key: "tab", Desc: "панель"})
	}

	if m.files.editorActive && m.focus == panelConsole {
		items = append(items,
			components.StatusItem{Key: "ctrl+s", Desc: "сохранить"},
			components.StatusItem{Key: "tab", Desc: "к списку"},
			components.StatusItem{Key: "f", Desc: "скрипты"},
			components.StatusItem{Key: "esc", Desc: "закрыть"},
		)
		return items
	}

	items = append(items,
		components.StatusItem{Key: "↑↓", Desc: "навигация"},
		components.StatusItem{Key: "enter", Desc: "открыть"},
		components.StatusItem{Key: "backspace", Desc: "вверх"},
		components.StatusItem{Key: "e", Desc: "редактор"},
		components.StatusItem{Key: "n/N", Desc: "файл/папка"},
		components.StatusItem{Key: "r", Desc: "rename"},
		components.StatusItem{Key: "d", Desc: "удалить"},
		components.StatusItem{Key: "p", Desc: "права"},
		components.StatusItem{Key: "x", Desc: "run"},
		components.StatusItem{Key: "u/o", Desc: "upload/download"},
		components.StatusItem{Key: "f", Desc: "скрипты"},
		components.StatusItem{Key: "t", Desc: "терминал"},
		components.StatusItem{Key: "esc", Desc: "назад"},
	)
	return items
}
