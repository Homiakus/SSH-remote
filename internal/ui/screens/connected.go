package screens

import (
	"fmt"
	"strings"
	"time"

	"sshpilot/internal/config"
	"sshpilot/internal/scripts"
	sshclient "sshpilot/internal/ssh"
	"sshpilot/internal/ui/components"
	"sshpilot/internal/ui/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ──────────────── Messages ────────────────

// ConnectedBackMsg — возврат к списку серверов.
type ConnectedBackMsg struct{}

// connectionStatusMsg — результат проверки подключения.
type connectionStatusMsg struct {
	report sshclient.DiagnosticReport
}

type connectionCheckTickMsg struct{}

// ──────────────── Focus panel ────────────────

type panel int

const (
	panelScripts panel = iota
	panelConsole
)

// Порог ширины для скрытия левой панели
const compactWidthThreshold = 80

// ──────────────── Model ────────────────

// ConnectedModel — экран подключения к серверу с двумя панелями:
// левая — список скриптов, правая — консоль вывода.
type ConnectedModel struct {
	server        config.ServerConfig
	leftMode      connectedLeftMode
	goBuild       goBuildPlanner
	localTerminal localTerminalFactory
	localPicker   localPathPicker

	// Скрипты (левая панель)
	entries []scriptEntry
	cursor  int

	// Консоль (правая панель)
	consoleLines   []string
	consolePartial string
	consoleScroll  int

	// Выполнение
	execScripts []scriptState
	execCurrent int
	execStarted time.Time
	executing   bool
	execDone    bool
	cancelled   bool
	startTime   time.Time

	// Состояние подключения
	connReport    sshclient.DiagnosticReport
	connChecking  bool
	lastCheckTime time.Time
	connection    sshConnection

	// UI
	focus   panel
	spinner spinner.Model
	width   int
	height  int

	files fileBrowserState

	// Терминальная рабочая область
	terminalWorkspaceActive bool
	terminalTabs            map[terminalTabKind]*terminalTabState
	terminalTabOrder        []terminalTabKind
	activeTerminalTab       terminalTabKind

	// Go-launch пайплайн
	goLaunch *runnerState
}

func NewConnectedModel(server config.ServerConfig) ConnectedModel {
	return newConnectedModelWithRuntime(server, defaultSSHRuntime{})
}

func (m *ConnectedModel) appendConsole(line string) {
	m.flushConsolePartial()
	m.consoleLines = append(m.consoleLines, line)
	// Автопрокрутка вниз
	m.consoleScroll = max(0, len(m.consoleRenderLines())-m.consoleVisibleLines())
}

func (m *ConnectedModel) appendConsoleOutput(chunk string) {
	chunk = strings.ReplaceAll(chunk, "\r\n", "\n")
	chunk = strings.ReplaceAll(chunk, "\r", "\n")

	full := m.consolePartial + chunk
	parts := strings.Split(full, "\n")
	if len(parts) == 1 {
		m.consolePartial = parts[0]
		return
	}

	m.consoleLines = append(m.consoleLines, parts[:len(parts)-1]...)
	m.consolePartial = parts[len(parts)-1]
	m.consoleScroll = max(0, len(m.consoleRenderLines())-m.consoleVisibleLines())
}

func (m *ConnectedModel) flushConsolePartial() {
	if m.consolePartial == "" {
		return
	}

	m.consoleLines = append(m.consoleLines, m.consolePartial)
	m.consolePartial = ""
}

func (m ConnectedModel) consoleRenderLines() []string {
	lines := append([]string{}, m.consoleLines...)
	if m.consolePartial != "" {
		lines = append(lines, m.consolePartial)
	}
	return lines
}

func (m ConnectedModel) consoleVisibleLines() int {
	h := m.height - 6 // header + statusbar + borders
	if h < 3 {
		return 3
	}
	return h
}

// IsExecuting возвращает true, если скрипты сейчас выполняются.
func (m ConnectedModel) IsExecuting() bool {
	return m.executing || m.files.busy
}

// IsTerminalActive возвращает true, если открыт локальный build terminal.
func (m ConnectedModel) IsTerminalActive() bool {
	return m.terminalWorkspaceActive
}

// isCompact возвращает true, если ширина меньше порога — скрываем левую панель.
func (m ConnectedModel) isCompact() bool {
	return m.width < compactWidthThreshold
}

func (m ConnectedModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.checkConnection())
}

// checkConnection — запускает фоновую проверку SSH-соединения.
func (m ConnectedModel) checkConnection() tea.Cmd {
	return func() tea.Msg {
		return connectionStatusMsg{report: m.connection.Diagnose()}
	}
}

// scheduleNextCheck — запланировать следующую проверку через 30 секунд.
func (m ConnectedModel) scheduleNextCheck() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return connectionCheckTickMsg{}
	})
}

// ──────────── Expand / Select логика скриптов ────────────

func (m *ConnectedModel) toggleExpand(idx int) {
	if idx < 0 || idx >= len(m.entries) || !m.entries[idx].isPackage {
		return
	}
	m.entries[idx].expanded = !m.entries[idx].expanded
	pkg := m.entries[idx].pkg
	if m.entries[idx].expanded {
		ins := make([]scriptEntry, len(pkg.Scripts))
		for i, s := range pkg.Scripts {
			ins[i] = scriptEntry{script: s}
		}
		tail := append(ins, m.entries[idx+1:]...)
		m.entries = append(m.entries[:idx+1], tail...)
	} else {
		end := idx + 1
		for end < len(m.entries) && !m.entries[end].isPackage {
			end++
		}
		m.entries = append(m.entries[:idx+1], m.entries[end:]...)
	}
}

func (m ConnectedModel) getSelected() []scripts.Script {
	var r []scripts.Script
	for _, e := range m.entries {
		if !e.isPackage && e.selected {
			r = append(r, e.script)
		}
	}
	return r
}

// ──────────── Выполнение скриптов ────────────

func (m *ConnectedModel) startExecution(ss []scripts.Script) tea.Cmd {
	runnables, err := classifySelectedScripts(ss)
	if err != nil {
		m.appendConsole(theme.ErrorStyle.Render("❌ " + err.Error()))
		m.appendConsole("")
		return nil
	}
	return m.startRunnerQueue(runnables)
}

func (m *ConnectedModel) runNextScript() tea.Cmd {
	return m.startNextRunnable()
}

// ──────────── Update ────────────

func (m ConnectedModel) Update(msg tea.Msg) (ConnectedModel, tea.Cmd) {
	if next, cmd, handled := m.handleFileMessage(msg); handled {
		return next, cmd
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.updateTerminalInputsWidth()
		m.updateFileEditorSize()
		// При переключении в компактный режим — фокус на консоль
		if m.isCompact() && m.focus == panelScripts && m.leftMode == leftModeScripts {
			m.focus = panelConsole
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case connectionStatusMsg:
		m.connReport = msg.report
		m.connChecking = false
		m.lastCheckTime = time.Now()
		return m, m.scheduleNextCheck()

	case connectionCheckTickMsg:
		m.connChecking = true
		return m, m.checkConnection()

	case terminalStartedMsg:
		return m.handleTerminalStarted(msg)

	case terminalOutputMsg:
		return m.handleTerminalOutput(msg)

	case terminalDoneMsg:
		return m.handleTerminalDone(msg)

	case terminalCommandResultMsg:
		return m.handleTerminalCommandResult(msg)

	case terminalDiagnosisMsg:
		return m.handleTerminalDiagnosis(msg)

	case nativeTerminalFinishedMsg:
		return m.handleNativeTerminalFinished(msg)

	case localCommandDoneMsg:
		return m.handleLocalCommandDone(msg)

	case scriptStreamStartedMsg:
		return m, tea.Batch(
			waitForScriptOutput(msg.index, msg.outputCh),
			waitForScriptDone(msg.index, msg.doneCh),
		)

	case scriptStreamOutputMsg:
		if !msg.ok {
			return m, nil
		}
		if msg.index >= 0 && msg.index < len(m.execScripts) {
			m.execScripts[msg.index].output += msg.text
		}
		m.appendConsoleOutput(msg.text)
		return m, waitForScriptOutput(msg.index, msg.outputCh)

	case scriptStreamDoneMsg:
		if !msg.ok {
			return m, nil
		}
		if msg.index < len(m.execScripts) {
			s := &m.execScripts[msg.index]
			m.flushConsolePartial()
			s.dur = time.Since(m.execStarted)
			s.output = msg.result.Output
			if msg.result.Err != nil {
				s.status = statusError
				s.err = msg.result.Err
				m.appendConsole(theme.ErrorStyle.Render(
					fmt.Sprintf("  ❌ %s: %v", s.script.Name, msg.result.Err)))
				m.execDone = true
				m.executing = false
				m.appendConsole(theme.ErrorStyle.Render("─── Завершено с ошибками ───"))
				m.appendConsole("")
				return m, nil
			}
			s.status = statusSuccess
			m.appendConsole(theme.SuccessStyle.Render(
				fmt.Sprintf("  ✅ %s (%.1fs)", s.script.Name, s.dur.Seconds())))
			m.execCurrent++
			if m.execCurrent < len(m.execScripts) {
				m.appendConsole("")
				return m, m.runNextScript()
			}
			m.execDone = true
			m.executing = false
			m.appendConsole(theme.SuccessStyle.Render("─── Все скрипты выполнены ───"))
			m.appendConsole("")
		}
		return m, nil

	case ScriptDoneMsg:
		if msg.Index < len(m.execScripts) {
			s := &m.execScripts[msg.Index]
			s.dur = time.Since(m.execStarted)
			if msg.Err != nil {
				s.status = statusError
				s.err = msg.Err
				s.output = msg.Output
				if msg.Output != "" {
					m.appendConsoleOutput(msg.Output)
					m.flushConsolePartial()
				}
				m.appendConsole(theme.ErrorStyle.Render(
					fmt.Sprintf("  ❌ %s: %v", s.script.Name, msg.Err)))
				m.execDone = true
				m.executing = false
				m.appendConsole(theme.ErrorStyle.Render("─── Завершено с ошибками ───"))
				m.appendConsole("")
				return m, nil
			}
			s.status = statusSuccess
			s.output = msg.Output
			if msg.Output != "" {
				m.appendConsoleOutput(msg.Output)
				m.flushConsolePartial()
			}
			m.appendConsole(theme.SuccessStyle.Render(
				fmt.Sprintf("  ✅ %s (%.1fs)", s.script.Name, s.dur.Seconds())))
			m.execCurrent++
			if m.execCurrent < len(m.execScripts) {
				m.appendConsole("")
				return m, m.runNextScript()
			}
			m.execDone = true
			m.executing = false
			m.appendConsole(theme.SuccessStyle.Render("─── Все скрипты выполнены ───"))
			m.appendConsole("")
		}

	case goBuildPreparedMsg:
		return m.handleGoBuildPrepared(msg)

	case goUploadResultMsg:
		return m.handleGoUploadResult(msg)

	case AllDoneMsg:
		m.execDone = true
		m.executing = false

	case tea.KeyMsg:
		if m.terminalWorkspaceActive {
			return m.updateTerminalKeys(msg)
		}

		if msg.String() == "f" && !m.executing && !m.files.busy && !m.CapturesTextInput() {
			if m.leftMode == leftModeFiles {
				m.leftMode = leftModeScripts
				m.focus = panelScripts
				return m, nil
			}
			return m, m.ensureFileMode()
		}

		if m.leftMode == leftModeFiles {
			return m.updateFileKeys(msg)
		}

		if msg.String() == "esc" {
			if m.executing {
				return m, nil
			}
			return m, func() tea.Msg { return ConnectedBackMsg{} }
		}

		// T — открыть терминал
		if msg.String() == "t" && !m.executing && !m.files.busy {
			return m.openOrSelectServerTab()
		}

		// Tab — переключение панелей (только если не компактный режим)
		if msg.String() == "tab" && !m.executing {
			if !m.isCompact() {
				if m.focus == panelScripts {
					m.focus = panelConsole
				} else {
					m.focus = panelScripts
				}
			}
			return m, nil
		}

		if m.focus == panelConsole {
			return m.updateConsoleKeys(msg)
		}
		return m.updateScriptKeys(msg)
	}
	return m, nil
}

func (m ConnectedModel) updateScriptKeys(msg tea.KeyMsg) (ConnectedModel, tea.Cmd) {
	if m.executing {
		return m, nil // Блокируем навигацию скриптов во время выполнения
	}

	switch {
	case key.Matches(msg, theme.ScriptListKeys.Up):
		if m.cursor > 0 {
			m.cursor--
		}
	case key.Matches(msg, theme.ScriptListKeys.Down):
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
	case msg.String() == " ": // space - toggle
		if m.cursor < len(m.entries) {
			if m.entries[m.cursor].isPackage {
				pkg := m.entries[m.cursor].pkg
				toggle := true
				for _, e := range m.entries {
					if !e.isPackage && e.script.Package == pkg.Name && e.selected {
						toggle = false
						break
					}
				}
				for i := range m.entries {
					if !m.entries[i].isPackage && m.entries[i].script.Package == pkg.Name {
						m.entries[i].selected = toggle
					}
				}
			} else {
				m.entries[m.cursor].selected = !m.entries[m.cursor].selected
			}
		}
	case msg.String() == "e": // expand
		if m.cursor < len(m.entries) && m.entries[m.cursor].isPackage {
			m.toggleExpand(m.cursor)
		}
	case key.Matches(msg, theme.ScriptListKeys.Enter):
		sel := m.getSelected()
		if len(sel) == 0 && m.cursor < len(m.entries) && m.entries[m.cursor].isPackage {
			sel = m.entries[m.cursor].pkg.Scripts
		}
		if len(sel) > 0 {
			return m, m.startExecution(sel)
		}
	}
	return m, nil
}

func (m ConnectedModel) updateConsoleKeys(msg tea.KeyMsg) (ConnectedModel, tea.Cmd) {
	vis := m.consoleVisibleLines()
	switch {
	case key.Matches(msg, theme.ExecutorKeys.ScrollUp):
		if m.consoleScroll > 0 {
			m.consoleScroll--
		}
	case key.Matches(msg, theme.ExecutorKeys.ScrollDown):
		maxScroll := max(0, len(m.consoleRenderLines())-vis)
		if m.consoleScroll < maxScroll {
			m.consoleScroll++
		}
	}
	return m, nil
}

// ──────────── View ────────────

func (m ConnectedModel) View() string {
	if m.width < 20 || m.height < 5 {
		return "Окно слишком маленькое"
	}

	header := m.renderHeaderWithStatus()

	compact := m.isCompact()
	panelH := m.height - 5 // header + statusbar

	var panels string

	if compact {
		singleW := m.width - 2
		if singleW < 15 {
			singleW = 15
		}

		content := m.renderConsolePanel(singleW, panelH)
		color := theme.ColorSecondary
		if m.terminalWorkspaceActive {
			content = m.renderTerminalPanel(singleW, panelH)
		} else if m.leftMode == leftModeFiles {
			if m.focus == panelScripts {
				content = m.renderFilesPanel(singleW, panelH)
				color = theme.ColorPrimary
			} else {
				content = m.renderFileDetailPanel(singleW, panelH)
				color = theme.ColorSecondary
			}
		}

		panels = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(color).
			Width(singleW).
			Height(panelH).
			Render(content)
	} else {
		leftW := m.width*2/5 - 2
		rightW := m.width - leftW - 3
		if leftW < 15 {
			leftW = 15
		}
		if rightW < 15 {
			rightW = 15
		}

		leftContent := m.renderScriptsPanel(leftW, panelH)
		rightContent := m.renderConsolePanel(rightW, panelH)
		if m.terminalWorkspaceActive {
			if m.leftMode == leftModeFiles {
				leftContent = m.renderFilesPanel(leftW, panelH)
			}
			rightContent = m.renderTerminalPanel(rightW, panelH)
		} else if m.leftMode == leftModeFiles {
			leftContent = m.renderFilesPanel(leftW, panelH)
			rightContent = m.renderFileDetailPanel(rightW, panelH)
		}

		leftBorder := lipgloss.RoundedBorder()
		rightBorder := lipgloss.RoundedBorder()
		leftColor := theme.ColorBorder
		rightColor := theme.ColorBorder

		if m.terminalWorkspaceActive {
			rightColor = theme.ColorSecondary
		} else if m.focus == panelScripts {
			leftColor = theme.ColorPrimary
		} else {
			rightColor = theme.ColorSecondary
		}

		leftBox := lipgloss.NewStyle().
			Border(leftBorder).
			BorderForeground(leftColor).
			Width(leftW).
			Height(panelH).
			Render(leftContent)

		rightBox := lipgloss.NewStyle().
			Border(rightBorder).
			BorderForeground(rightColor).
			Width(rightW).
			Height(panelH).
			Render(rightContent)

		panels = lipgloss.JoinHorizontal(lipgloss.Top, leftBox, " ", rightBox)
	}

	items := m.scriptStatusItems(compact)
	if m.terminalWorkspaceActive {
		items = m.terminalStatusItems()
	} else if m.leftMode == leftModeFiles {
		items = m.fileStatusItems(compact)
	}
	statusBar := components.RenderStatusBar(items, m.width)

	if dialog := m.renderFileDialog(); dialog != "" {
		return lipgloss.JoinVertical(lipgloss.Left, header, panels, dialog, statusBar)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, panels, statusBar)
}

// renderHeaderWithStatus — заголовок с индикатором подключения.
func (m ConnectedModel) renderHeaderWithStatus() string {
	title := theme.TitleStyle.Render(" ⚡ SSHPilot ")

	// Breadcrumbs
	sep := theme.MutedStyle.Render(" → ")
	crumbs := theme.BreadcrumbStyle.Render("Серверы") + sep +
		theme.BreadcrumbActiveStyle.Render(m.server.Name)

	// Статус подключения
	var statusIndicator string
	if m.connChecking {
		statusIndicator = theme.SpinnerStyle.Render("◌ диагностика...")
	} else {
		statusIndicator = m.connectionStatusIndicator()
	}

	left := lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", crumbs)
	// Размещаем статус справа
	statusW := lipgloss.Width(statusIndicator)
	leftW := lipgloss.Width(left)
	gap := m.width - leftW - statusW - 2
	if gap < 1 {
		gap = 1
	}
	header := left + strings.Repeat(" ", gap) + statusIndicator

	return lipgloss.NewStyle().
		Width(m.width).
		MarginBottom(1).
		Render(header)
}

func (m ConnectedModel) connectionStatusIndicator() string {
	switch m.connReport.Stage {
	case sshclient.DiagnosticStageSuccess:
		return theme.SuccessStyle.Render("● подключено")
	case sshclient.DiagnosticStageAuth:
		return theme.ErrorStyle.Render("● пароль отклонён")
	case sshclient.DiagnosticStageSession:
		return theme.WarningStyle.Render("● сессия недоступна")
	case sshclient.DiagnosticStageConfig:
		return theme.WarningStyle.Render("● конфиг неполный")
	case sshclient.DiagnosticStageBanner:
		return theme.WarningStyle.Render("● не-SSH ответ")
	case sshclient.DiagnosticStageHandshake:
		return theme.WarningStyle.Render("● ssh несовместим")
	case sshclient.DiagnosticStageTCP:
		return theme.ErrorStyle.Render("● " + connectionTCPStatusLabel(m.connReport.Err))
	default:
		return theme.MutedStyle.Render("● статус неизвестен")
	}
}

func connectionTCPStatusLabel(err error) string {
	if err == nil {
		return "сеть недоступна"
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "i/o timeout"):
		return "tcp timeout"
	case strings.Contains(msg, "refused"):
		return "порт закрыт"
	default:
		return "сеть недоступна"
	}
}

func (m ConnectedModel) connectionFailureDiagnosisCmd() tea.Cmd {
	if m.connReport.Stage == "" || m.connReport.Stage == sshclient.DiagnosticStageSuccess {
		return nil
	}

	report := sshclient.FormatDiagnosticReport(m.connReport)
	return func() tea.Msg {
		return terminalDiagnosisMsg{report: report}
	}
}

func (m ConnectedModel) renderScriptsPanel(_, _ int) string {
	var b strings.Builder

	// Заголовок панели
	title := theme.SubtitleStyle.Render("📁 Скрипты")
	b.WriteString(title + "\n\n")

	if len(m.entries) == 0 {
		b.WriteString(theme.MutedStyle.Render("Нет скриптов"))
		return b.String()
	}

	for i, e := range m.entries {
		cur := " "
		if i == m.cursor && m.focus == panelScripts {
			cur = theme.SelectedItemStyle.Render(theme.IconArrow)
		}
		if e.isPackage {
			icon := theme.IconArrow
			if e.expanded {
				icon = theme.IconArrowDown
			}
			st := theme.ItemStyle
			if i == m.cursor && m.focus == panelScripts {
				st = theme.SelectedItemStyle
			}
			b.WriteString(fmt.Sprintf("%s %s %s %s\n", cur,
				theme.MutedStyle.Render(icon),
				theme.IconFolder,
				st.Render(e.pkg.Name)))
		} else {
			pre := theme.IconBranch
			if i+1 >= len(m.entries) || m.entries[i+1].isPackage {
				pre = theme.IconBranchEnd
			}
			sel := "○"
			if e.selected {
				sel = theme.SuccessStyle.Render("●")
			}
			st := theme.MutedStyle
			if i == m.cursor && m.focus == panelScripts {
				st = theme.SelectedItemStyle
			}
			label := e.script.Name
			switch e.script.Kind {
			case scripts.ScriptKindGo:
				label += " [go]"
			case scripts.ScriptKindBinary:
				label += " [bin]"
			}
			b.WriteString(fmt.Sprintf("%s   %s %s %s\n", cur,
				theme.MutedStyle.Render(pre), sel, st.Render(label)))
		}
	}

	// Текущий статус выполнения
	if m.executing && len(m.execScripts) > 0 {
		b.WriteString("\n")
		b.WriteString(theme.SpinnerStyle.Render("⏳ Выполняется...") + "\n")
		for _, s := range m.execScripts {
			var icon string
			switch s.status {
			case statusQueued:
				icon = theme.MutedStyle.Render("○")
			case statusBuilding, statusUploading, statusStarting:
				icon = m.spinner.View()
			case statusAttached:
				icon = theme.SuccessStyle.Render(">")
			case statusSuccess:
				icon = theme.SuccessStyle.Render("●")
			case statusFailed:
				icon = theme.ErrorStyle.Render("●")
			}
			b.WriteString(fmt.Sprintf(" %s %s\n", icon, s.script.Name))
		}
	}

	if sel := m.getSelected(); len(sel) > 0 && !m.executing {
		b.WriteString("\n" + theme.SuccessStyle.Render(fmt.Sprintf("Выбрано: %d", len(sel))))
	}

	return b.String()
}

func (m ConnectedModel) renderConsolePanel(w, _ int) string {
	var b strings.Builder

	// Заголовок
	title := theme.SubtitleStyle.Render("🖥 Консоль")
	scrollInfo := ""
	titleText := "Терминал"
	if m.executing || len(m.execScripts) > 0 {
		titleText = "Терминал выполнения"
	}
	title = theme.SubtitleStyle.Render(">> " + titleText)
	lines := m.consoleRenderLines()
	if len(lines) > m.consoleVisibleLines() {
		total := len(lines)
		pos := m.consoleScroll + m.consoleVisibleLines()
		if pos > total {
			pos = total
		}
		scrollInfo = theme.MutedStyle.Render(fmt.Sprintf(" [%d/%d]", pos, total))
	}
	b.WriteString(title + scrollInfo + "\n\n")

	// Выводим строки консоли
	vis := m.consoleVisibleLines()
	start := m.consoleScroll
	end := start + vis
	if end > len(lines) {
		end = len(lines)
	}

	if len(lines) == 0 {
		if m.executing {
			b.WriteString(theme.MutedStyle.Render("Подключение к потоку вывода..."))
		} else {
			b.WriteString(theme.MutedStyle.Render("Живой вывод скриптов появится здесь"))
		}
	} else {
		lineW := w - 2
		if lineW < 10 {
			lineW = 10
		}
		for _, line := range lines[start:end] {
			// Обрезаем длинные строки
			rendered := line
			if lipgloss.Width(rendered) > lineW {
				rendered = rendered[:lineW-1] + "…"
			}
			b.WriteString(rendered + "\n")
		}
	}

	return b.String()
}
