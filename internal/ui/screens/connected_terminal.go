package screens

import (
	"fmt"
	"strings"
	"time"

	sshclient "sshpilot/internal/ssh"
	"sshpilot/internal/ui/components"
	"sshpilot/internal/ui/theme"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type terminalStartedMsg struct {
	tab   terminalTabKind
	shell terminalSession
	err   error
}

type terminalOutputMsg struct {
	tab   terminalTabKind
	shell terminalSession
	text  string
	ok    bool
}

type terminalDoneMsg struct {
	tab   terminalTabKind
	shell terminalSession
	err   error
}

type terminalCommandResultMsg struct {
	tab terminalTabKind
	err error
}

type terminalDiagnosisMsg struct {
	report string
}

type nativeTerminalFinishedMsg struct {
	command string
	manual  bool
	err     error
}

type immediateNativeExecCommand interface {
	tea.ExecCommand
	RunImmediatelyForTest() bool
}

type localCommandDoneMsg struct {
	tab    terminalTabKind
	shell  terminalSession
	result localCommandResult
	ok     bool
}

type terminalTabKind int

const (
	terminalTabBuild terminalTabKind = iota
)

const maxTerminalLines = 2000

type buildTabState int

const (
	buildTabIdle buildTabState = iota
	buildTabRunning
	buildTabReady
	buildTabFailed
)

type terminalTabState struct {
	kind       terminalTabKind
	input      textinput.Model
	shell      terminalSession
	lines      []string
	partial    string
	connecting bool
	closed     bool
	buildState buildTabState
	runningCmd string
	runningAt  time.Time
	outputAt   time.Time
}

func newTerminalInputModel() textinput.Model {
	input := textinput.New()
	input.Prompt = "> "
	input.Placeholder = "Введите команду и нажмите Enter"
	input.CharLimit = 4096
	return input
}

func newTerminalTabState(kind terminalTabKind) *terminalTabState {
	return &terminalTabState{
		kind:       kind,
		input:      newTerminalInputModel(),
		buildState: buildTabIdle,
	}
}

func (m *ConnectedModel) ensureTerminalWorkspace() {
	if m.terminalTabs == nil {
		m.terminalTabs = map[terminalTabKind]*terminalTabState{}
	}
	m.terminalWorkspaceActive = true
}

func (m *ConnectedModel) focusTerminalTab(kind terminalTabKind) tea.Cmd {
	tab := m.terminalTab(kind)
	if tab == nil {
		return nil
	}

	m.terminalWorkspaceActive = true
	m.focus = panelConsole
	m.activeTerminalTab = kind

	for _, otherKind := range m.terminalTabOrder {
		other := m.terminalTab(otherKind)
		if other == nil {
			continue
		}
		if otherKind == kind {
			continue
		}
		other.input.Blur()
	}

	return tab.input.Focus()
}

func (m *ConnectedModel) ensureTerminalTab(kind terminalTabKind) *terminalTabState {
	m.ensureTerminalWorkspace()

	if tab, ok := m.terminalTabs[kind]; ok {
		return tab
	}

	tab := newTerminalTabState(kind)
	m.terminalTabs[kind] = tab
	m.terminalTabOrder = append(m.terminalTabOrder, kind)
	m.updateTerminalInputsWidth()
	return tab
}

func (m ConnectedModel) terminalTab(kind terminalTabKind) *terminalTabState {
	if m.terminalTabs == nil {
		return nil
	}
	return m.terminalTabs[kind]
}

func (m ConnectedModel) activeTerminalTabState() *terminalTabState {
	if !m.terminalWorkspaceActive {
		return nil
	}
	return m.terminalTab(m.activeTerminalTab)
}

func (m ConnectedModel) findTerminalTabByShell(shell terminalSession) *terminalTabState {
	if shell == nil {
		return nil
	}
	for _, kind := range m.terminalTabOrder {
		tab := m.terminalTab(kind)
		if tab != nil && tab.shell == shell {
			return tab
		}
	}
	return nil
}

func (m ConnectedModel) hasTerminalTab(kind terminalTabKind) bool {
	return m.terminalTab(kind) != nil
}

func (m *ConnectedModel) updateTerminalInputsWidth() {
	width := max(20, m.width-8)
	for _, kind := range m.terminalTabOrder {
		tab := m.terminalTab(kind)
		if tab != nil {
			tab.input.Width = width
		}
	}
}

func (m *ConnectedModel) resetTerminalWorkspace() {
	if m.terminalTabs != nil {
		for _, tab := range m.terminalTabs {
			tab.input.Blur()
		}
	}
	m.terminalWorkspaceActive = false
	m.terminalTabs = nil
	m.terminalTabOrder = nil
	m.activeTerminalTab = terminalTabBuild
}

func (m *ConnectedModel) closeTerminalTabsImmediate() {
	for _, kind := range m.terminalTabOrder {
		tab := m.terminalTab(kind)
		if tab != nil && tab.shell != nil {
			_ = tab.shell.Close()
		}
	}
	m.resetTerminalWorkspace()
}

func (m *ConnectedModel) closeTerminalWorkspace() tea.Cmd {
	shells := make([]terminalSession, 0, len(m.terminalTabOrder))
	for _, kind := range m.terminalTabOrder {
		tab := m.terminalTab(kind)
		if tab != nil && tab.shell != nil {
			shells = append(shells, tab.shell)
		}
	}
	m.resetTerminalWorkspace()

	cmds := make([]tea.Cmd, 0, len(shells))
	for _, shell := range shells {
		cmds = append(cmds, closeTerminalSession(shell))
	}
	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

func (m *ConnectedModel) prepareBuildTerminalTab(scriptName string) {
	tab := m.ensureTerminalTab(terminalTabBuild)
	tab.connecting = true
	tab.closed = false
	tab.shell = nil
	tab.lines = nil
	tab.partial = ""
	tab.buildState = buildTabRunning
	tab.runningCmd = ""
	tab.runningAt = time.Time{}
	tab.outputAt = time.Time{}
	tab.input.SetValue("")
	tab.input.CursorEnd()
	m.appendTerminalLine(terminalTabBuild, theme.SubtitleStyle.Render("Локальная сборка Go-бинарника"))
	m.appendTerminalLine(terminalTabBuild, theme.MutedStyle.Render("Приложение: "+scriptName))
	m.appendTerminalLine(terminalTabBuild, theme.SpinnerStyle.Render("Подготовка локальной build-сессии..."))
}

func (m *ConnectedModel) attachTerminalTab(kind terminalTabKind, shell terminalSession, introLines []string) {
	tab := m.ensureTerminalTab(kind)
	tab.connecting = false
	tab.closed = false
	tab.shell = shell
	tab.runningCmd = ""
	tab.runningAt = time.Time{}
	tab.outputAt = time.Time{}
	if kind != terminalTabBuild {
		tab.lines = nil
		tab.partial = ""
	}
	tab.input.SetValue("")
	tab.input.CursorEnd()

	for _, line := range introLines {
		m.appendTerminalLine(kind, line)
	}
}

func (m *ConnectedModel) appendTerminalLine(kind terminalTabKind, line string) {
	tab := m.ensureTerminalTab(kind)
	tab.lines = append(tab.lines, line)
	trimTerminalLines(tab)
}

func (m *ConnectedModel) appendTerminalOutput(kind terminalTabKind, chunk string) {
	tab := m.ensureTerminalTab(kind)

	chunk = strings.ReplaceAll(chunk, "\r\n", "\n")
	chunk = strings.ReplaceAll(chunk, "\r", "\n")

	full := tab.partial + chunk
	parts := strings.Split(full, "\n")
	if len(parts) == 1 {
		tab.partial = parts[0]
		return
	}

	tab.lines = append(tab.lines, parts[:len(parts)-1]...)
	tab.partial = parts[len(parts)-1]
	trimTerminalLines(tab)
}

func trimTerminalLines(tab *terminalTabState) {
	if tab == nil || len(tab.lines) <= maxTerminalLines {
		return
	}
	copy(tab.lines, tab.lines[len(tab.lines)-maxTerminalLines:])
	tab.lines = tab.lines[:maxTerminalLines]
}

func (m *ConnectedModel) markTerminalCommandStarted(kind terminalTabKind, display string) {
	tab := m.ensureTerminalTab(kind)
	tab.runningCmd = display
	tab.runningAt = time.Now()
	tab.outputAt = tab.runningAt
}

func (m *ConnectedModel) markTerminalOutputActivity(kind terminalTabKind) {
	tab := m.terminalTab(kind)
	if tab == nil || tab.runningAt.IsZero() {
		return
	}
	tab.outputAt = time.Now()
}

func (m *ConnectedModel) clearTerminalCommandState(kind terminalTabKind) {
	tab := m.terminalTab(kind)
	if tab == nil {
		return
	}
	tab.runningCmd = ""
	tab.runningAt = time.Time{}
	tab.outputAt = time.Time{}
}

func (m ConnectedModel) openServerTerminalCmd() tea.Cmd {
	return m.nativeServerTerminalCmd("", true)
}

func (m ConnectedModel) nativeServerTerminalCmd(command string, manual bool) tea.Cmd {
	if m.connection == nil {
		return func() tea.Msg {
			return nativeTerminalFinishedMsg{
				command: command,
				manual:  manual,
				err:     fmt.Errorf("ssh-соединение недоступно"),
			}
		}
	}
	execCommand := m.connection.NativeTerminalCommand(command, m.height, m.width)
	if execCommand == nil {
		return func() tea.Msg {
			return nativeTerminalFinishedMsg{
				command: command,
				manual:  manual,
				err:     fmt.Errorf("native SSH-терминал не настроен"),
			}
		}
	}
	if immediate, ok := execCommand.(immediateNativeExecCommand); ok && immediate.RunImmediatelyForTest() {
		return func() tea.Msg {
			return nativeTerminalFinishedMsg{
				command: command,
				manual:  manual,
				err:     immediate.Run(),
			}
		}
	}
	return tea.Exec(execCommand, func(err error) tea.Msg {
		return nativeTerminalFinishedMsg{
			command: command,
			manual:  manual,
			err:     err,
		}
	})
}

func startBuildTerminalCmd(factory localTerminalFactory, workDir string, env []string) tea.Cmd {
	return func() tea.Msg {
		if factory == nil {
			return terminalStartedMsg{
				tab: terminalTabBuild,
				err: fmt.Errorf("локальный build-терминал не настроен"),
			}
		}
		shell, err := factory(workDir, env)
		return terminalStartedMsg{tab: terminalTabBuild, shell: shell, err: err}
	}
}

func waitForTerminalOutput(tab terminalTabKind, shell terminalSession) tea.Cmd {
	return func() tea.Msg {
		text, ok := <-shell.Output()
		return terminalOutputMsg{tab: tab, shell: shell, text: text, ok: ok}
	}
}

func waitForScriptOutput(index int, outputCh <-chan string) tea.Cmd {
	return func() tea.Msg {
		text, ok := <-outputCh
		return scriptStreamOutputMsg{index: index, outputCh: outputCh, text: text, ok: ok}
	}
}

func waitForScriptDone(index int, doneCh <-chan sshclient.ScriptResult) tea.Cmd {
	return func() tea.Msg {
		result, ok := <-doneCh
		return scriptStreamDoneMsg{index: index, result: result, ok: ok}
	}
}

func waitForTrackedCommandDone(tab terminalTabKind, shell trackedTerminalSession) tea.Cmd {
	return func() tea.Msg {
		result, ok := <-shell.CommandDone()
		return localCommandDoneMsg{tab: tab, shell: shell, result: result, ok: ok}
	}
}

func waitForTerminalDone(tab terminalTabKind, shell terminalSession) tea.Cmd {
	return func() tea.Msg {
		err, ok := <-shell.Done()
		if !ok {
			return terminalDoneMsg{tab: tab, shell: shell}
		}
		return terminalDoneMsg{tab: tab, shell: shell, err: err}
	}
}

func sendTerminalCommand(tab terminalTabKind, shell terminalSession, line string) tea.Cmd {
	return func() tea.Msg {
		return terminalCommandResultMsg{tab: tab, err: shell.SendLine(line)}
	}
}

func runTrackedTerminalCommand(tab terminalTabKind, shell trackedTerminalSession, spec localCommandSpec) tea.Cmd {
	return func() tea.Msg {
		return terminalCommandResultMsg{tab: tab, err: shell.RunCommand(spec)}
	}
}

func interruptTerminalCommand(tab terminalTabKind, shell terminalSession) tea.Cmd {
	return func() tea.Msg {
		return terminalCommandResultMsg{tab: tab, err: shell.Interrupt()}
	}
}

func closeTerminalSession(shell terminalSession) tea.Cmd {
	return func() tea.Msg {
		_ = shell.Close()
		return nil
	}
}

func formatTerminalDiagnosticLines(report string) []string {
	report = strings.TrimSpace(report)
	if report == "" {
		return nil
	}

	lines := strings.Split(report, "\n")
	rendered := make([]string, 0, len(lines))
	for i, line := range lines {
		switch i {
		case 0:
			rendered = append(rendered, theme.ErrorStyle.Render(line))
		default:
			rendered = append(rendered, theme.MutedStyle.Render(line))
		}
	}
	return rendered
}

func (m ConnectedModel) updateTerminalKeys(msg tea.KeyMsg) (ConnectedModel, tea.Cmd) {
	active := m.activeTerminalTabState()
	if active == nil {
		return m, nil
	}

	switch msg.String() {
	case "esc":
		if m.goLaunch != nil && m.executing {
			m.cancelCurrentRunner(fmt.Errorf("запуск отменён пользователем"), []string{"Запуск отменён пользователем"})
		}
		return m, m.closeTerminalWorkspace()

	case "ctrl+c":
		if active.shell != nil && !active.connecting && !active.closed && m.canInterruptTerminalTab(active.kind) {
			return m, interruptTerminalCommand(active.kind, active.shell)
		}
		return m, nil

	case "tab":
		return m.cycleTerminalTab(1)

	case "shift+tab":
		return m.cycleTerminalTab(-1)

	case "t":
		return m.openOrSelectServerTab()

	case "enter":
		if !m.canSendTerminalCommand(active.kind) {
			return m, nil
		}

		line := active.input.Value()
		m.appendTerminalLine(active.kind, theme.MutedStyle.Render("> "+line))
		active.input.SetValue("")
		if _, ok := active.shell.(trackedTerminalSession); ok && strings.TrimSpace(line) != "" {
			m.markTerminalCommandStarted(active.kind, strings.TrimSpace(line))
		}
		return m, sendTerminalCommand(active.kind, active.shell, line)
	}

	if !m.canEditTerminalInput(active.kind) {
		return m, nil
	}

	var cmd tea.Cmd
	active.input, cmd = active.input.Update(msg)
	return m, cmd
}

func (m ConnectedModel) canEditTerminalInput(kind terminalTabKind) bool {
	tab := m.terminalTab(kind)
	if tab == nil || tab.connecting || tab.closed || tab.shell == nil {
		return false
	}
	if tracked, ok := tab.shell.(trackedTerminalSession); ok && tracked.Running() {
		return false
	}
	return true
}

func (m ConnectedModel) canSendTerminalCommand(kind terminalTabKind) bool {
	return m.canEditTerminalInput(kind)
}

func (m ConnectedModel) canInterruptTerminalTab(kind terminalTabKind) bool {
	tab := m.terminalTab(kind)
	if tab == nil || tab.connecting || tab.closed || tab.shell == nil {
		return false
	}
	if tracked, ok := tab.shell.(trackedTerminalSession); ok {
		return tracked.Running()
	}
	return true
}

func (m ConnectedModel) cycleTerminalTab(delta int) (ConnectedModel, tea.Cmd) {
	if len(m.terminalTabOrder) <= 1 {
		return m, nil
	}

	current := 0
	for i, kind := range m.terminalTabOrder {
		if kind == m.activeTerminalTab {
			current = i
			break
		}
	}

	next := (current + delta + len(m.terminalTabOrder)) % len(m.terminalTabOrder)
	return m, tea.Batch(m.focusTerminalTab(m.terminalTabOrder[next]), textinput.Blink)
}

func (m ConnectedModel) openOrSelectServerTab() (ConnectedModel, tea.Cmd) {
	m.appendConsole(theme.MutedStyle.Render("Открываю нативную SSH-консоль сервера..."))
	m.appendConsole(theme.MutedStyle.Render("Для выхода из консоли нажмите Ctrl+Q или введите exit"))
	return m, m.openServerTerminalCmd()
}

func (m ConnectedModel) renderTerminalView() string {
	if m.width < 20 || m.height < 8 {
		return "Окно слишком маленькое"
	}

	header := components.RenderHeader([]string{"Серверы", m.server.Name, "Терминалы"}, m.width)
	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(theme.ColorSecondary).
		Width(max(20, m.width-2)).
		Height(max(8, m.height-7)).
		Render(m.renderTerminalPanel(max(20, m.width-4), max(8, m.height-9)))
	statusBar := components.RenderStatusBar(m.terminalStatusItems(), m.width)
	return lipgloss.JoinVertical(lipgloss.Left, header, "", panel, statusBar)
}

func (m ConnectedModel) renderTerminalPanel(w, h int) string {
	active := m.activeTerminalTabState()
	if active == nil {
		return theme.MutedStyle.Render("Терминальная рабочая область закрыта")
	}

	lines := append([]string{}, active.lines...)
	if active.partial != "" {
		lines = append(lines, active.partial)
	}
	if len(lines) == 0 {
		lines = append(lines, theme.MutedStyle.Render("Ожидание вывода..."))
	}

	outputHeight := h - 8
	if outputHeight < 4 {
		outputHeight = 4
	}
	start := max(0, len(lines)-outputHeight)

	var b strings.Builder
	b.WriteString(m.renderTerminalTabs() + "\n\n")
	b.WriteString(theme.SubtitleStyle.Render(m.terminalPanelTitle(active.kind)) + "\n\n")
	for _, line := range lines[start:] {
		b.WriteString(line + "\n")
	}

	b.WriteString("\n" + theme.LabelStyle.Render("Команда") + "\n")
	inputView := m.terminalInputView(active, w)
	b.WriteString(inputView)

	return strings.TrimRight(b.String(), "\n")
}

func (m ConnectedModel) renderTerminalTabs() string {
	parts := make([]string, 0, len(m.terminalTabOrder))
	for _, kind := range m.terminalTabOrder {
		label := m.terminalTabLabel(kind)
		style := lipgloss.NewStyle().
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.ColorBorder).
			Foreground(theme.ColorSubtext)
		if kind == m.activeTerminalTab {
			style = style.
				BorderForeground(theme.ColorSecondary).
				Foreground(theme.ColorText)
		}
		parts = append(parts, style.Render(label))
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, parts...)
}

func (m ConnectedModel) terminalTabLabel(kind terminalTabKind) string {
	tab := m.terminalTab(kind)
	if tab == nil {
		return ""
	}

	switch kind {
	case terminalTabBuild:
		switch tab.buildState {
		case buildTabRunning:
			if !tab.runningAt.IsZero() {
				return "Build • " + formatShortDuration(time.Since(tab.runningAt))
			}
			return "Build • running"
		case buildTabReady:
			return "Build • ready"
		case buildTabFailed:
			return "Build • failed"
		default:
			return "Build"
		}
	default:
		return "Terminal"
	}
}

func formatShortDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
	minutes := int(d / time.Minute)
	seconds := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm %02ds", minutes, seconds)
}

func (m ConnectedModel) terminalRunningHint(tab *terminalTabState) string {
	if tab == nil || tab.runningAt.IsZero() {
		return ""
	}

	elapsed := formatShortDuration(time.Since(tab.runningAt))
	silence := elapsed
	command := ""
	if tab.runningCmd != "" {
		command = tab.runningCmd + ". "
	}
	if !tab.outputAt.IsZero() {
		silence = formatShortDuration(time.Since(tab.outputAt))
	}

	if tab.kind == terminalTabBuild {
		return fmt.Sprintf(
			"%sСборка выполняется %s, без нового вывода %s. ctrl+c - прервать build, esc - отменить весь запуск.",
			command,
			elapsed,
			silence,
		)
	}

	return fmt.Sprintf(
		"%sКоманда выполняется %s, без нового вывода %s. Используйте ctrl+c для прерывания.",
		command,
		elapsed,
		silence,
	)
}

func (m ConnectedModel) terminalPanelTitle(kind terminalTabKind) string {
	switch kind {
	case terminalTabBuild:
		return "Build Terminal"
	default:
		return "Terminal"
	}
}

func (m ConnectedModel) terminalInputView(tab *terminalTabState, w int) string {
	input := tab.input
	input.Width = max(16, w-4)

	switch {
	case tab.connecting && tab.kind == terminalTabBuild:
		return theme.MutedStyle.Render("Подготовка build-сессии...")
	case tab.connecting:
		return theme.MutedStyle.Render("Подключение...")
	case tab.closed:
		return theme.MutedStyle.Render("Сеанс завершен. Нажмите esc, чтобы закрыть окно.")
	case tab.shell == nil:
		return theme.MutedStyle.Render("Сеанс не инициализирован.")
	}

	if tracked, ok := tab.shell.(trackedTerminalSession); ok && tracked.Running() {
		hint := m.terminalRunningHint(tab)
		if hint == "" {
			hint = "Команда выполняется... используйте ctrl+c для прерывания."
		}
		return theme.MutedStyle.Render(hint)
	}
	return input.View()
}

func (m ConnectedModel) terminalStatusItems() []components.StatusItem {
	active := m.activeTerminalTabState()
	if active == nil {
		return []components.StatusItem{{Key: "esc", Desc: "закрыть"}}
	}

	items := make([]components.StatusItem, 0, 6)
	if len(m.terminalTabOrder) > 1 {
		items = append(items,
			components.StatusItem{Key: "tab", Desc: "след. вкладка"},
			components.StatusItem{Key: "shift+tab", Desc: "пред. вкладка"},
		)
	}

	if active.kind == terminalTabBuild {
		items = append(items, components.StatusItem{Key: "t", Desc: "сервер"})
	}

	if m.canSendTerminalCommand(active.kind) {
		items = append(items, components.StatusItem{Key: "enter", Desc: "выполнить"})
	}
	if m.canInterruptTerminalTab(active.kind) {
		desc := "прервать"
		if active.kind == terminalTabBuild {
			desc = "стоп build"
		}
		items = append(items, components.StatusItem{Key: "ctrl+c", Desc: desc})
	}

	escDesc := "закрыть"
	if m.goLaunch != nil && m.executing {
		escDesc = "отменить запуск"
	}
	items = append(items, components.StatusItem{Key: "esc", Desc: escDesc})
	return items
}

func (m ConnectedModel) handleTerminalStarted(msg terminalStartedMsg) (ConnectedModel, tea.Cmd) {
	tab := m.ensureTerminalTab(msg.tab)
	if msg.err != nil {
		tab.connecting = false
		tab.closed = true
		tab.shell = nil
		if msg.tab == terminalTabBuild {
			tab.buildState = buildTabFailed
			m.failCurrentRunner(msg.err, []string{"Не удалось открыть локальную build-сессию"})
			return m, nil
		}

		m.appendTerminalLine(msg.tab, theme.ErrorStyle.Render(fmt.Sprintf("❌ Не удалось открыть терминал: %v", msg.err)))
		if cmd := m.connectionFailureDiagnosisCmd(); cmd != nil {
			return m, cmd
		}
		m.appendTerminalLine(msg.tab, theme.MutedStyle.Render("Последняя SSH-диагностика: TCP и логин в порядке, проблема на этапе интерактивной shell-сессии."))
		return m, nil
	}

	m.attachTerminalTab(msg.tab, msg.shell, nil)
	if msg.tab == terminalTabBuild {
		if m.goLaunch == nil {
			return m, nil
		}

		tracked, ok := msg.shell.(trackedTerminalSession)
		if !ok {
			m.failCurrentRunner(fmt.Errorf("build-терминал не поддерживает отслеживание команд"), []string{"Локальная build-сессия не поддерживает отслеживание команд"})
			return m, nil
		}

		buildCmd := m.goLaunch.buildCommandSpec()
		m.markTerminalCommandStarted(msg.tab, buildCmd.Display)
		m.appendTerminalLine(msg.tab, theme.SuccessStyle.Render("Build-сессия готова"))
		m.appendTerminalLine(msg.tab, theme.MutedStyle.Render("Запускаю go build с подробным трассировочным выводом (-x -v)..."))
		m.appendTerminalLine(msg.tab, theme.MutedStyle.Render("> "+buildCmd.Display))
		tab.buildState = buildTabRunning
		return m, tea.Batch(
			waitForTerminalOutput(msg.tab, msg.shell),
			runTrackedTerminalCommand(msg.tab, tracked, buildCmd),
			waitForTrackedCommandDone(msg.tab, tracked),
		)
	}

	return m, nil
}

func (m ConnectedModel) handleTerminalOutput(msg terminalOutputMsg) (ConnectedModel, tea.Cmd) {
	tab := m.findTerminalTabByShell(msg.shell)
	if tab == nil || !msg.ok {
		return m, nil
	}

	m.markTerminalOutputActivity(msg.tab)
	m.appendTerminalOutput(msg.tab, msg.text)
	return m, waitForTerminalOutput(msg.tab, msg.shell)
}

func (m ConnectedModel) handleTerminalDone(msg terminalDoneMsg) (ConnectedModel, tea.Cmd) {
	tab := m.findTerminalTabByShell(msg.shell)
	if tab == nil {
		return m, nil
	}

	m.clearTerminalCommandState(msg.tab)
	tab.shell = nil
	tab.connecting = false
	tab.closed = true

	if msg.err != nil {
		m.appendTerminalLine(msg.tab, theme.WarningStyle.Render(fmt.Sprintf("⚠ Сеанс завершен: %v", msg.err)))
	} else {
		m.appendTerminalLine(msg.tab, theme.MutedStyle.Render("Сеанс завершен"))
	}

	return m, nil
}

func (m ConnectedModel) handleTerminalCommandResult(msg terminalCommandResultMsg) (ConnectedModel, tea.Cmd) {
	if msg.err == nil {
		return m, nil
	}

	tab := m.terminalTab(msg.tab)
	if tab != nil {
		m.clearTerminalCommandState(msg.tab)
		m.appendTerminalLine(msg.tab, theme.ErrorStyle.Render(fmt.Sprintf("❌ Ошибка терминала: %v", msg.err)))
		if msg.tab == terminalTabBuild {
			tab.buildState = buildTabFailed
		}
	}

	if msg.tab == terminalTabBuild && m.goLaunch != nil && m.goLaunch.phase == executionPhaseBuildingLocal {
		m.failCurrentRunner(msg.err, []string{"Не удалось запустить локальную сборку"})
	}
	return m, nil
}

func (m ConnectedModel) handleTerminalDiagnosis(msg terminalDiagnosisMsg) (ConnectedModel, tea.Cmd) {
	if !m.terminalWorkspaceActive {
		for _, line := range formatTerminalDiagnosticLines(msg.report) {
			m.appendConsole(line)
		}
		return m, nil
	}

	target := m.activeTerminalTab
	if !m.hasTerminalTab(target) {
		target = terminalTabBuild
	}
	for _, line := range formatTerminalDiagnosticLines(msg.report) {
		m.appendTerminalLine(target, line)
	}
	return m, nil
}

func (m ConnectedModel) handleNativeTerminalFinished(msg nativeTerminalFinishedMsg) (ConnectedModel, tea.Cmd) {
	if msg.manual {
		if msg.err != nil {
			m.appendConsole(theme.ErrorStyle.Render(fmt.Sprintf("❌ Нативная SSH-консоль завершилась с ошибкой: %v", msg.err)))
			if cmd := m.connectionFailureDiagnosisCmd(); cmd != nil {
				return m, cmd
			}
			return m, nil
		}
		m.appendConsole(theme.MutedStyle.Render("Нативная SSH-консоль закрыта (Ctrl+Q или exit)"))
		return m, nil
	}

	if msg.err != nil {
		m.failCurrentRunner(
			newRunnerError(executionPhaseCompleted, false, "runnable завершился на сервере с ошибкой", msg.err),
			nil,
		)
		return m, nil
	}

	return m, m.completeCurrentRunnerSuccess()
}
