package screens

import (
	"fmt"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"sshpilot/internal/scripts"
	sshclient "sshpilot/internal/ssh"
	"sshpilot/internal/ui/theme"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type goBuildPlanner func(scripts.Script, sshclient.RemotePlatform) (scripts.GoBuildPlan, func(), error)

type runnerPreparedMsg struct {
	script       scripts.Script
	platform     sshclient.RemotePlatform
	plan         scripts.GoBuildPlan
	artifactPath string
	interpreter  string
	cleanup      func()
	err          error
}

type runnerUploadResultMsg struct {
	script     scripts.Script
	remotePath string
	logs       []string
	err        error
}

// Compatibility aliases for the existing update loop/tests.
type goBuildPreparedMsg = runnerPreparedMsg
type goUploadResultMsg = runnerUploadResultMsg

type executionPhase string

const (
	executionPhaseIdle                  executionPhase = "idle"
	executionPhaseResolving             executionPhase = "resolving"
	executionPhaseValidating            executionPhase = "validating"
	executionPhaseDetectingPlatform     executionPhase = "detecting_platform"
	executionPhasePreparingArtifact     executionPhase = "preparing_local_artifact"
	executionPhaseBuildingLocal         executionPhase = "building_local"
	executionPhaseArtifactReady         executionPhase = "artifact_ready"
	executionPhaseOpeningRemoteFS       executionPhase = "opening_remote_fs"
	executionPhaseEnsuringRemoteDir     executionPhase = "ensuring_remote_dir"
	executionPhaseStoppingExisting      executionPhase = "stopping_existing"
	executionPhaseUploading             executionPhase = "uploading"
	executionPhaseSettingPermissions    executionPhase = "setting_permissions"
	executionPhaseOpeningServerTerminal executionPhase = "opening_server_terminal"
	executionPhaseDispatchingCommand    executionPhase = "dispatching_command"
	executionPhaseAwaitingRemoteStart   executionPhase = "awaiting_remote_start"
	executionPhaseAttached              executionPhase = "attached"
	executionPhaseCompleted             executionPhase = "completed"
	executionPhaseFailed                executionPhase = "failed"
	executionPhaseCancelled             executionPhase = "cancelled"
)

var (
	goLaunchPlatformTimeout    = 30 * time.Second
	goLaunchOpenFSTimeout      = 15 * time.Second
	goLaunchRemoteDirTimeout   = 10 * time.Second
	goLaunchUploadTimeout      = 45 * time.Second
	goLaunchRemoteChmodTimeout = 10 * time.Second
	goLaunchOpenShellTimeout   = 15 * time.Second
	goLaunchRemoteStartTimeout = 15 * time.Second
)

type runnerState struct {
	script       scripts.Script
	platform     sshclient.RemotePlatform
	plan         scripts.GoBuildPlan
	artifactPath string
	interpreter  string
	cleanup      func()
	remotePath   string
	phase        executionPhase
	commandLine  string
	index        int
}

func defaultGoBuildPlanner(script scripts.Script, platform sshclient.RemotePlatform) (scripts.GoBuildPlan, func(), error) {
	return scripts.PrepareGoBuild(script, scripts.BuildOptions{
		GOOS:   platform.GOOS,
		GOARCH: platform.GOARCH,
	})
}

func (s runnerState) buildCommandSpec() localCommandSpec {
	return localCommandSpec{
		Display: buildCommandDisplay(s.plan.ArtifactPath),
		Name:    "go",
		Args:    buildCommandArgs(s.plan.ArtifactPath),
	}
}

func (m *ConnectedModel) startGoExecution(script scripts.Script) tea.Cmd {
	return m.startRunnerQueue([]scripts.Script{script})
}

func (m *ConnectedModel) startRunnerQueue(ss []scripts.Script) tea.Cmd {
	if len(ss) == 0 {
		return nil
	}

	states := make([]scriptState, len(ss))
	for i, s := range ss {
		states[i] = scriptState{script: s, status: statusQueued}
	}
	m.execScripts = states
	m.execCurrent = 0
	m.executing = true
	m.execDone = false
	m.cancelled = false
	m.startTime = time.Now()
	m.focus = panelConsole

	m.appendConsole(theme.SubtitleStyle.Render(fmt.Sprintf("─── Запуск %d runnable-элементов ───", len(ss))))
	m.appendConsole("")
	return m.startNextRunnable()
}

func prepareRunnableCmd(connection sshConnection, planner goBuildPlanner, script scripts.Script) tea.Cmd {
	return func() tea.Msg {
		msg := runnerPreparedMsg{script: script}
		kind := script.Kind
		if kind == "" {
			kind = scripts.ScriptKindSH
			msg.script.Kind = kind
		}

		switch kind {
		case scripts.ScriptKindGo:
			if planner == nil {
				msg.err = newRunnerError(executionPhasePreparingArtifact, false, "go builder не настроен", nil)
				return msg
			}

			platform, err := detectRemotePlatformWithRetry(connection)
			if err != nil {
				msg.err = newRunnerError(executionPhaseDetectingPlatform, true, "не удалось определить платформу сервера", err)
				return msg
			}
			plan, cleanup, err := planner(script, platform)
			if err != nil {
				msg.err = newRunnerError(executionPhasePreparingArtifact, false, "не удалось подготовить локальную сборку", err)
				return msg
			}
			msg.platform = platform
			msg.plan = plan
			msg.artifactPath = plan.ArtifactPath
			msg.cleanup = cleanup
			return msg

		case scripts.ScriptKindSH:
			interpreter, err := scripts.DetectScriptInterpreter(script)
			if err != nil {
				msg.err = newRunnerError(executionPhaseValidating, false, "не удалось определить интерпретатор shell-скрипта", err)
				return msg
			}
			entryPath := script.LocalEntryPath()
			if entryPath == "" {
				msg.err = newRunnerError(executionPhasePreparingArtifact, false, "для shell-скрипта не указан EntryPath", nil)
				return msg
			}
			msg.interpreter = interpreter
			msg.artifactPath = entryPath
			return msg

		case scripts.ScriptKindBinary:
			if script.IsRemoteOnly() {
				return msg
			}

			platform, err := detectRemotePlatformWithRetry(connection)
			if err != nil {
				msg.err = newRunnerError(executionPhaseDetectingPlatform, true, "не удалось определить платформу сервера", err)
				return msg
			}
			if err := scripts.ValidateBinaryTargetPlatform(script, platform.GOOS, platform.GOARCH); err != nil {
				msg.err = newRunnerError(executionPhaseValidating, false, "платформа бинарника не совпадает с сервером", err)
				return msg
			}
			artifactPath, cleanup, err := scripts.PrepareLocalBinaryArtifact(script)
			if err != nil {
				msg.err = newRunnerError(executionPhasePreparingArtifact, false, "не удалось подготовить локальный бинарник", err)
				return msg
			}
			msg.platform = platform
			msg.artifactPath = artifactPath
			msg.cleanup = cleanup
			return msg

		default:
			msg.err = newRunnerError(executionPhaseResolving, false, "неподдерживаемый тип runnable", fmt.Errorf("%s", script.Kind))
			return msg
		}
	}
}

func prepareGoBuildCmd(connection sshConnection, planner goBuildPlanner, script scripts.Script) tea.Cmd {
	return prepareRunnableCmd(connection, planner, script)
}

func continueRunnableUploadCmd(connection sshConnection, state runnerState) tea.Cmd {
	return func() tea.Msg {
		logs := make([]string, 0, 12)

		fs, err := runGoLaunchStepValue(connection, "Открытие SFTP-сессии", goLaunchOpenFSTimeout, func() (sshclient.RemoteFS, error) {
			return connection.OpenRemoteFS()
		})
		if err != nil {
			return runnerUploadResultMsg{
				script: state.script,
				logs:   append(logs, "Не удалось открыть удалённую файловую систему"),
				err:    newRunnerError(executionPhaseOpeningRemoteFS, true, "не удалось открыть SFTP-сессию", err),
			}
		}
		if fs == nil {
			return runnerUploadResultMsg{
				script: state.script,
				logs:   append(logs, "Удалённая файловая система недоступна"),
				err:    newRunnerError(executionPhaseOpeningRemoteFS, true, "SSH runtime вернул пустую SFTP-сессию", nil),
			}
		}
		defer fs.Close()
		logs = append(logs, "SFTP-сессия открыта")

		remotePath := state.script.ResolveRemotePath(fs.StartDir())
		remoteDir := path.Dir(remotePath)

		if !state.script.IsRemoteOnly() {
			if err := runGoLaunchStep(connection, "Подготовка удалённого каталога", goLaunchRemoteDirTimeout, func() error {
				return prepareRemoteDir(connection, fs, remoteDir)
			}); err != nil {
				return runnerUploadResultMsg{
					script: state.script,
					logs:   append(logs, "Не удалось подготовить удалённый каталог"),
					err:    newRunnerError(executionPhaseEnsuringRemoteDir, true, "не удалось подготовить удалённый каталог", err),
				}
			}
			logs = append(logs, "Удалённый каталог подготовлен: "+remoteDir)

			if err := runGoLaunchStep(connection, "Остановка старого runnable", goLaunchRemoteChmodTimeout, func() error {
				return stopExistingRemoteRunnable(connection, remotePath)
			}); err != nil {
				return runnerUploadResultMsg{
					script: state.script,
					logs:   append(logs, "Не удалось остановить старый runnable"),
					err:    newRunnerError(executionPhaseStoppingExisting, true, "не удалось остановить старый runnable перед заменой", err),
				}
			}
			logs = append(logs, "Старый runnable остановлен или не был запущен")

			tempRemotePath := remoteUploadTempPath(remotePath)
			if err := runGoLaunchStep(connection, "Загрузка runnable на сервер", goLaunchUploadTimeout, func() error {
				return fs.Upload(sshclient.TransferRequest{
					LocalPath:  state.artifactPath,
					RemotePath: tempRemotePath,
					Overwrite:  true,
				})
			}); err != nil {
				cleanupRemoteUploadTemp(fs, tempRemotePath)
				return runnerUploadResultMsg{
					script: state.script,
					logs:   append(logs, "Загрузка runnable завершилась ошибкой"),
					err:    newRunnerError(executionPhaseUploading, true, "не удалось загрузить runnable на сервер", err),
				}
			}

			mode := state.script.EffectiveChmod()
			if err := runGoLaunchStep(connection, "Выдача прав на выполнение", goLaunchRemoteChmodTimeout, func() error {
				return fs.Chmod(tempRemotePath, mode)
			}); err != nil {
				cleanupRemoteUploadTemp(fs, tempRemotePath)
				return runnerUploadResultMsg{
					script: state.script,
					logs:   append(logs, "Не удалось выдать права на выполнение"),
					err:    newRunnerError(executionPhaseSettingPermissions, true, "не удалось выдать права на выполнение", err),
				}
			}

			if err := runGoLaunchStep(connection, "Атомарная замена runnable", goLaunchRemoteChmodTimeout, func() error {
				return fs.Rename(tempRemotePath, remotePath)
			}); err != nil {
				cleanupRemoteUploadTemp(fs, tempRemotePath)
				return runnerUploadResultMsg{
					script: state.script,
					logs:   append(logs, "Не удалось заменить runnable"),
					err:    newRunnerError(executionPhaseUploading, true, "не удалось заменить runnable на сервере", err),
				}
			}

			logs = append(logs, "Runnable загружен: "+remotePath)
			logs = append(logs, fmt.Sprintf("Права обновлены: %04o", mode.Perm()))
		} else {
			if err := runGoLaunchStep(connection, "Проверка удалённого бинарника", goLaunchRemoteDirTimeout, func() error {
				_, statErr := fs.Stat(remotePath)
				return statErr
			}); err != nil {
				return runnerUploadResultMsg{
					script: state.script,
					logs:   append(logs, "Удалённый бинарник не найден"),
					err:    newRunnerError(executionPhaseValidating, false, "не удалось открыть удалённый бинарник", err),
				}
			}
			logs = append(logs, "Удалённый файл готов к запуску: "+remotePath)

			if state.script.Chmod != 0 {
				mode := state.script.EffectiveChmod()
				if err := runGoLaunchStep(connection, "Выдача прав на выполнение", goLaunchRemoteChmodTimeout, func() error {
					return fs.Chmod(remotePath, mode)
				}); err != nil {
					return runnerUploadResultMsg{
						script: state.script,
						logs:   append(logs, "Не удалось выдать права на выполнение"),
						err:    newRunnerError(executionPhaseSettingPermissions, true, "не удалось выдать права на выполнение", err),
					}
				}
				logs = append(logs, fmt.Sprintf("Права обновлены: %04o", mode.Perm()))
			}
		}

		return runnerUploadResultMsg{
			script:     state.script,
			remotePath: remotePath,
			logs:       logs,
		}
	}
}

func continueGoUploadCmd(connection sshConnection, script scripts.Script, artifactPath string) tea.Cmd {
	return continueRunnableUploadCmd(connection, runnerState{
		script:       script,
		artifactPath: artifactPath,
	})
}

func (m *ConnectedModel) startNextRunnable() tea.Cmd {
	if m.execCurrent >= len(m.execScripts) || m.cancelled {
		return func() tea.Msg { return AllDoneMsg{} }
	}

	current := m.execCurrent
	script := m.execScripts[current].script
	m.goLaunch = &runnerState{
		script: script,
		phase:  executionPhaseResolving,
		index:  current,
	}
	m.execScripts[current].status = statusQueued
	m.focus = panelConsole
	m.appendConsole(theme.SubtitleStyle.Render(fmt.Sprintf("─── %s: %s ───", strings.ToUpper(string(script.Kind)), script.Name)))
	m.appendConsole(theme.SpinnerStyle.Render("? Резолвлю runnable и проверяю окружение..."))
	return prepareRunnableCmd(m.connection, m.goBuild, script)
}

func (m ConnectedModel) handleGoBuildPrepared(msg goBuildPreparedMsg) (ConnectedModel, tea.Cmd) {
	return m.handleRunnerPrepared(runnerPreparedMsg(msg))
}

func (m ConnectedModel) handleRunnerPrepared(msg runnerPreparedMsg) (ConnectedModel, tea.Cmd) {
	if m.goLaunch == nil {
		return m, nil
	}

	if msg.err != nil {
		m.failCurrentRunner(msg.err, nil)
		return m, nil
	}

	m.goLaunch.platform = msg.platform
	m.goLaunch.plan = msg.plan
	m.goLaunch.artifactPath = msg.artifactPath
	m.goLaunch.interpreter = msg.interpreter
	m.goLaunch.cleanup = msg.cleanup
	m.goLaunch.phase = executionPhaseArtifactReady

	switch msg.script.Kind {
	case scripts.ScriptKindGo:
		m.goLaunch.phase = executionPhaseBuildingLocal
		m.execScripts[m.goLaunch.index].status = statusBuilding
		m.appendConsole(theme.MutedStyle.Render(fmt.Sprintf("  Целевая платформа: %s/%s", msg.platform.GOOS, msg.platform.GOARCH)))
		m.appendConsole(theme.MutedStyle.Render("  Рабочая директория сборки: " + filepath.Clean(msg.plan.WorkDir)))
		m.appendConsole(theme.MutedStyle.Render("  Артефакт: " + filepath.Clean(msg.plan.ArtifactPath)))
		m.appendConsole(theme.MutedStyle.Render("  Переменные окружения: " + strings.Join(msg.plan.Env, " ")))
		m.appendConsole(theme.MutedStyle.Render("  Подробный режим go build: включен (-x -v)"))
		m.appendConsole(theme.MutedStyle.Render("  Управление: ctrl+c - прервать текущую фазу, esc - отменить весь запуск"))

		m.prepareBuildTerminalTab(msg.script.Name)
		m.appendTerminalLine(terminalTabBuild, theme.MutedStyle.Render(fmt.Sprintf("Целевая платформа: %s/%s", msg.platform.GOOS, msg.platform.GOARCH)))
		m.appendTerminalLine(terminalTabBuild, theme.MutedStyle.Render("Рабочая директория: "+filepath.Clean(msg.plan.WorkDir)))
		m.appendTerminalLine(terminalTabBuild, theme.MutedStyle.Render("Артефакт: "+filepath.Clean(msg.plan.ArtifactPath)))
		m.appendTerminalLine(terminalTabBuild, theme.MutedStyle.Render("Переменные окружения: "+strings.Join(msg.plan.Env, " ")))
		m.appendTerminalLine(terminalTabBuild, theme.MutedStyle.Render("Подробный режим go build: включен (-x -v)"))
		m.appendTerminalLine(terminalTabBuild, theme.MutedStyle.Render("Управление: ctrl+c - прервать фазу, esc - отменить весь запуск"))
		return m, tea.Batch(
			m.focusTerminalTab(terminalTabBuild),
			textinput.Blink,
			startBuildTerminalCmd(m.localTerminal, msg.plan.WorkDir, msg.plan.Env),
		)

	case scripts.ScriptKindSH:
		m.appendConsole(theme.MutedStyle.Render("  Локальный entry: " + filepath.Clean(msg.artifactPath)))
		m.appendConsole(theme.MutedStyle.Render("  Интерпретатор: " + msg.interpreter))

	case scripts.ScriptKindBinary:
		if !msg.script.IsRemoteOnly() {
			m.appendConsole(theme.MutedStyle.Render(fmt.Sprintf("  Целевая платформа: %s/%s", msg.platform.GOOS, msg.platform.GOARCH)))
			m.appendConsole(theme.MutedStyle.Render("  Локальный бинарник: " + filepath.Clean(msg.artifactPath)))
		} else {
			m.appendConsole(theme.MutedStyle.Render("  Удалённый бинарник: " + msg.script.RemotePath))
		}
	}

	m.execScripts[m.goLaunch.index].status = statusUploading
	m.appendConsole(theme.SpinnerStyle.Render("? Подготавливаю файл на сервере..."))
	return m, continueRunnableUploadCmd(m.connection, *m.goLaunch)
}

func (m ConnectedModel) handleLocalCommandDone(msg localCommandDoneMsg) (ConnectedModel, tea.Cmd) {
	tab := m.findTerminalTabByShell(msg.shell)
	if tab == nil || !msg.ok {
		return m, nil
	}

	if msg.tab != terminalTabBuild || m.goLaunch == nil || m.goLaunch.phase != executionPhaseBuildingLocal {
		return m, nil
	}

	tab.connecting = false
	m.clearTerminalCommandState(msg.tab)
	if msg.result.Err != nil {
		tab.buildState = buildTabFailed
		if msg.result.Interrupted {
			m.cancelCurrentRunner(fmt.Errorf("локальная сборка прервана пользователем"), []string{"Локальная сборка прервана пользователем"})
			return m, nil
		}
		m.failCurrentRunner(newRunnerError(executionPhaseBuildingLocal, false, "локальная сборка завершилась ошибкой", msg.result.Err), nil)
		return m, nil
	}

	tab.buildState = buildTabReady
	m.goLaunch.phase = executionPhaseUploading
	m.execScripts[m.goLaunch.index].status = statusUploading
	m.focus = panelConsole
	m.appendConsole(theme.SuccessStyle.Render("  ? Локальная сборка завершена"))
	m.appendConsole(theme.SpinnerStyle.Render("? Подготавливаю runnable на сервере..."))

	return m, continueRunnableUploadCmd(m.connection, *m.goLaunch)
}

func (m ConnectedModel) handleGoUploadResult(msg goUploadResultMsg) (ConnectedModel, tea.Cmd) {
	return m.handleRunnerUploadResult(runnerUploadResultMsg(msg))
}

func (m ConnectedModel) handleRunnerUploadResult(msg runnerUploadResultMsg) (ConnectedModel, tea.Cmd) {
	if m.goLaunch == nil {
		return m, nil
	}

	if cleanup := m.goLaunch.cleanup; cleanup != nil {
		cleanup()
		m.goLaunch.cleanup = nil
	}

	if msg.err != nil {
		if tab := m.terminalTab(terminalTabBuild); tab != nil && m.goLaunch.script.Kind == scripts.ScriptKindGo {
			tab.buildState = buildTabFailed
		}
		m.failCurrentRunner(msg.err, msg.logs)
		return m, nil
	}

	for _, line := range msg.logs {
		m.appendConsole("  " + theme.MutedStyle.Render(line))
	}

	commandLine, err := buildRemoteRunnableCommand(m.goLaunch.script, msg.remotePath, m.goLaunch.interpreter)
	if err != nil {
		m.failCurrentRunner(newRunnerError(executionPhaseDispatchingCommand, false, "не удалось подготовить команду запуска", err), nil)
		return m, nil
	}

	m.goLaunch.phase = executionPhaseOpeningServerTerminal
	m.goLaunch.remotePath = msg.remotePath
	m.goLaunch.commandLine = commandLine

	m.goLaunch.phase = executionPhaseAttached
	m.execScripts[m.goLaunch.index].status = statusAttached
	m.focus = panelConsole
	m.appendConsole(theme.SuccessStyle.Render("  ✓ Открываю runnable в нативной консоли сервера"))
	m.appendConsole(theme.MutedStyle.Render("  Команда: " + commandLine))
	return m, m.nativeServerTerminalCmd(commandLine, false)
}

func (m *ConnectedModel) failCurrentRunner(err error, logs []string) {
	if m.goLaunch != nil && m.goLaunch.cleanup != nil {
		m.goLaunch.cleanup()
	}
	if m.goLaunch != nil {
		m.goLaunch.phase = executionPhaseFailed
	}
	if tab := m.terminalTab(terminalTabBuild); tab != nil && m.goLaunch != nil && m.goLaunch.script.Kind == scripts.ScriptKindGo {
		tab.buildState = buildTabFailed
	}

	if len(m.execScripts) > 0 && m.execCurrent < len(m.execScripts) {
		s := &m.execScripts[m.execCurrent]
		s.status = statusFailed
		s.err = err
		s.dur = time.Since(m.startTime)
	}
	m.executing = false
	m.execDone = true
	for _, line := range logs {
		m.appendConsole("  " + theme.MutedStyle.Render(line))
	}
	if err != nil && len(m.execScripts) > 0 && m.execCurrent < len(m.execScripts) {
		m.appendConsole(theme.ErrorStyle.Render(fmt.Sprintf("  ? %s: %v", m.execScripts[m.execCurrent].script.Name, err)))
	}
	m.appendConsole(theme.ErrorStyle.Render("─── Запуск завершился ошибкой ───"))
	m.appendConsole("")
	m.goLaunch = nil
}

func (m *ConnectedModel) cancelCurrentRunner(err error, logs []string) {
	if m.goLaunch != nil && m.goLaunch.cleanup != nil {
		m.goLaunch.cleanup()
	}
	if m.goLaunch != nil {
		m.goLaunch.phase = executionPhaseCancelled
	}

	if len(m.execScripts) > 0 && m.execCurrent < len(m.execScripts) {
		s := &m.execScripts[m.execCurrent]
		s.status = statusCancelled
		s.err = err
		s.dur = time.Since(m.startTime)
	}
	m.executing = false
	m.execDone = true
	for _, line := range logs {
		m.appendConsole("  " + theme.MutedStyle.Render(line))
	}
	if err != nil && len(m.execScripts) > 0 && m.execCurrent < len(m.execScripts) {
		m.appendConsole(theme.WarningStyle.Render(fmt.Sprintf("  ? %s: %v", m.execScripts[m.execCurrent].script.Name, err)))
	}
	m.appendConsole(theme.WarningStyle.Render("─── Запуск отменён ───"))
	m.appendConsole("")
	m.goLaunch = nil
}

func (m *ConnectedModel) completeCurrentRunnerSuccess() tea.Cmd {
	if m.goLaunch == nil {
		return nil
	}

	m.goLaunch.phase = executionPhaseCompleted
	if len(m.execScripts) > 0 && m.execCurrent < len(m.execScripts) {
		s := &m.execScripts[m.execCurrent]
		s.status = statusSuccess
		s.dur = time.Since(m.startTime)
		m.appendConsole(theme.SuccessStyle.Render(
			fmt.Sprintf("  ? %s завершён успешно (%.1fs)", s.script.Name, s.dur.Seconds())))
	}
	m.appendConsole(theme.SuccessStyle.Render("─── Runnable завершён ───"))
	m.appendConsole("")
	m.goLaunch = nil
	m.execCurrent++
	if m.execCurrent < len(m.execScripts) {
		return m.startNextRunnable()
	}
	m.executing = false
	m.execDone = true
	return nil
}

func stopExistingRemoteRunnable(connection sshConnection, remotePath string) error {
	if connection == nil {
		return fmt.Errorf("ssh-соединение недоступно")
	}

	remotePath = path.Clean(strings.TrimSpace(remotePath))
	if remotePath == "" || remotePath == "." || remotePath == "/" {
		return fmt.Errorf("некорректный путь runnable: %q", remotePath)
	}

	script := strings.Join([]string{
		"set -u",
		"target=" + shellQuote(remotePath),
		"self=$$",
		"parent=${PPID:-}",
		"find_pids() {",
		"  if command -v pgrep >/dev/null 2>&1; then",
		"    pgrep -f -- \"$target\" 2>/dev/null || true",
		"  else",
		"    ps -eo pid=,args= 2>/dev/null | awk -v target=\"$target\" 'index($0, target) { print $1 }'",
		"  fi",
		"}",
		"filter_pids() {",
		"  awk -v self=\"$self\" -v parent=\"$parent\" '$1 != self && $1 != parent && $1 != \"\" { print $1 }'",
		"}",
		"pids=$(find_pids | filter_pids | sort -u)",
		"if [ -z \"$pids\" ]; then",
		"  exit 0",
		"fi",
		"kill -TERM $pids 2>/dev/null || true",
		"i=0",
		"while [ \"$i\" -lt 5 ]; do",
		"  sleep 1",
		"  remaining=$(find_pids | filter_pids | sort -u)",
		"  if [ -z \"$remaining\" ]; then",
		"    exit 0",
		"  fi",
		"  i=$((i + 1))",
		"done",
		"remaining=$(find_pids | filter_pids | sort -u)",
		"if [ -n \"$remaining\" ]; then",
		"  kill -KILL $remaining 2>/dev/null || true",
		"  sleep 1",
		"fi",
		"remaining=$(find_pids | filter_pids | sort -u)",
		"if [ -n \"$remaining\" ]; then",
		"  echo \"failed to stop pids: $remaining\" >&2",
		"  exit 1",
		"fi",
	}, "\n")

	if _, err := connection.ExecuteScript(script); err != nil {
		return fmt.Errorf("не удалось остановить процессы %s: %w", remotePath, err)
	}
	return nil
}

func remoteUploadTempPath(remotePath string) string {
	remotePath = path.Clean(strings.TrimSpace(remotePath))
	return fmt.Sprintf("%s.sshpilot-upload-%d", remotePath, time.Now().UnixNano())
}

func cleanupRemoteUploadTemp(fs sshclient.RemoteFS, tempRemotePath string) {
	if fs == nil || strings.TrimSpace(tempRemotePath) == "" {
		return
	}
	_ = fs.Remove(tempRemotePath)
}

func prepareRemoteDir(connection sshConnection, fs sshclient.RemoteFS, dir string) error {
	dir = path.Clean(strings.TrimSpace(dir))
	if dir == "" || dir == "." || dir == "/" {
		return nil
	}

	var shellErr error
	if connection != nil {
		shellErr = ensureRemoteDirWithScript(connection, dir)
		if shellErr == nil {
			return nil
		}
	}

	if fs == nil {
		return shellErr
	}

	if err := ensureRemoteDir(fs, dir); err != nil {
		if shellErr != nil {
			return fmt.Errorf(
				"ssh mkdir завершился ошибкой (%v), а резервный SFTP-путь тоже не сработал: %w",
				shellErr,
				err,
			)
		}
		return err
	}

	return nil
}

func ensureRemoteDirWithScript(connection sshConnection, dir string) error {
	if connection == nil {
		return fmt.Errorf("ssh-соединение недоступно")
	}

	script := strings.Join([]string{
		"set -e",
		"mkdir -p -- " + shellQuote(dir),
	}, "\n")
	if _, err := connection.ExecuteScript(script); err != nil {
		return fmt.Errorf("не удалось создать директорию %s через SSH-команду: %w", dir, err)
	}
	return nil
}

func ensureRemoteDir(fs sshclient.RemoteFS, dir string) error {
	entry, err := fs.Stat(dir)
	if err == nil {
		if !entry.IsDir {
			return fmt.Errorf("%s существует, но не является директорией", dir)
		}
		return nil
	}

	exists, statErr := remoteEntryExists(fs, dir)
	if statErr != nil {
		return statErr
	}
	if exists {
		return nil
	}

	parent := path.Dir(dir)
	if parent != "." && parent != "/" && parent != dir {
		if err := ensureRemoteDir(fs, parent); err != nil {
			return err
		}
	}

	return fs.Mkdir(dir)
}

func buildRemoteRunnableCommand(script scripts.Script, remotePath, interpreter string) (string, error) {
	var parts []string
	kind := script.Kind
	if kind == "" {
		kind = scripts.ScriptKindSH
	}

	for _, item := range script.Env {
		key, value, ok := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return "", fmt.Errorf("переменная окружения %q должна быть в формате KEY=VALUE", item)
		}
		parts = append(parts, key+"="+shellQuote(value))
	}

	switch kind {
	case scripts.ScriptKindSH:
		words := strings.Fields(strings.TrimSpace(interpreter))
		if len(words) == 0 {
			return "", fmt.Errorf("для shell runnable не удалось определить интерпретатор")
		}
		for _, word := range words {
			parts = append(parts, shellQuote(word))
		}
		parts = append(parts, shellQuote(remotePath))

	case scripts.ScriptKindGo, scripts.ScriptKindBinary:
		parts = append(parts, shellQuote(remotePath))

	default:
		return "", fmt.Errorf("неподдерживаемый тип runnable: %s", script.Kind)
	}

	for _, arg := range script.RunArgs {
		parts = append(parts, shellQuote(arg))
	}

	return strings.Join(parts, " "), nil
}

func newRunnerError(phase executionPhase, retryable bool, userMessage string, err error) error {
	return sshclient.OperationError{
		Phase:       string(phase),
		Retryable:   retryable,
		UserMessage: userMessage,
		Err:         err,
	}
}

func shellQuote(value string) string {
	escaped := strings.ReplaceAll(value, "'", `'"'"'`)
	return "'" + escaped + "'"
}

func buildCommandDisplay(artifactPath string) string {
	args := buildCommandArgs(filepath.Clean(artifactPath))
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, "go")
	if runtime.GOOS == "windows" {
		for _, arg := range args {
			parts = append(parts, powershellQuote(arg))
		}
		return strings.Join(parts, " ")
	}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func buildCommandArgs(artifactPath string) []string {
	return []string{"build", "-x", "-v", "-o", filepath.Clean(artifactPath), "."}
}

func detectRemotePlatformWithRetry(connection sshConnection) (sshclient.RemotePlatform, error) {
	detect := func() (sshclient.RemotePlatform, error) {
		return runGoLaunchStepValue(connection, "Определение платформы сервера", goLaunchPlatformTimeout, func() (sshclient.RemotePlatform, error) {
			return connection.DetectRemotePlatform()
		})
	}

	if platform, err := detect(); err == nil {
		return platform, nil
	} else {
		firstErr := err
		if connection != nil {
			_ = connection.Reset()
		}
		platform, retryErr := detect()
		if retryErr == nil {
			return platform, nil
		}
		return sshclient.RemotePlatform{}, fmt.Errorf(
			"не удалось определить платформу сервера после повторной попытки: сначала %v, затем %w",
			firstErr,
			retryErr,
		)
	}
}

func runGoLaunchStep(connection sshConnection, action string, timeout time.Duration, fn func() error) error {
	if fn == nil {
		return nil
	}
	_, err := runGoLaunchStepValue(connection, action, timeout, func() (struct{}, error) {
		return struct{}{}, fn()
	})
	return err
}

type goLaunchStepResult[T any] struct {
	value T
	err   error
}

func runGoLaunchStepValue[T any](connection sshConnection, action string, timeout time.Duration, fn func() (T, error)) (T, error) {
	var zero T
	if fn == nil {
		return zero, nil
	}
	if timeout <= 0 {
		return fn()
	}

	done := make(chan goLaunchStepResult[T], 1)
	abandoned := make(chan struct{})
	go func() {
		value, err := fn()
		select {
		case done <- goLaunchStepResult[T]{value: value, err: err}:
		case <-abandoned:
		}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case result := <-done:
		return result.value, result.err
	case <-timer.C:
		if connection != nil {
			_ = connection.Reset()
		}
		close(abandoned)
		return zero, fmt.Errorf("%s превысило таймаут %s", action, timeout.Round(time.Second))
	}
}

func powershellQuote(value string) string {
	escaped := strings.ReplaceAll(value, "'", "''")
	return "'" + escaped + "'"
}
