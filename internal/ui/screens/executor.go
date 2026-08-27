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

type ScriptOutputMsg struct{ Line string }
type ScriptDoneMsg struct {
	Index  int
	Output string
	Err    error
}
type AllDoneMsg struct{}
type ExecutionCancelledMsg struct{}

type scriptStatus int

const (
	statusQueued scriptStatus = iota
	statusBuilding
	statusUploading
	statusStarting
	statusAttached
	statusSuccess
	statusFailed
	statusCancelled

	// Legacy aliases kept while the old executor model still exists.
	statusPending = statusQueued
	statusRunning = statusStarting
	statusError   = statusFailed
)

type scriptState struct {
	script scripts.Script
	status scriptStatus
	output string
	dur    time.Duration
	err    error
}

type ExecutorModel struct {
	server    config.ServerConfig
	scripts   []scriptState
	current   int
	output    []string
	spinner   spinner.Model
	width     int
	height    int
	done      bool
	cancelled bool
	startTime time.Time
}

func NewExecutorModel(server config.ServerConfig, ss []scripts.Script) ExecutorModel {
	states := make([]scriptState, len(ss))
	for i, s := range ss {
		states[i] = scriptState{script: s, status: statusPending}
	}
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = theme.SpinnerStyle
	return ExecutorModel{
		server: server, scripts: states, spinner: sp, startTime: time.Now(),
	}
}

func (m ExecutorModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, m.runNext())
}

func (m *ExecutorModel) runNext() tea.Cmd {
	if m.current >= len(m.scripts) || m.cancelled {
		return func() tea.Msg { return AllDoneMsg{} }
	}
	idx := m.current
	m.scripts[idx].status = statusRunning
	srv := m.server
	sc := m.scripts[idx].script
	start := time.Now()

	return func() tea.Msg {
		content, err := scripts.ReadScript(sc)
		if err != nil {
			return ScriptDoneMsg{Index: idx, Err: err}
		}
		cfg := &config.ServerConfig{
			Name: srv.Name, Host: srv.Host, Port: srv.Port,
			User: srv.User, AuthMethod: srv.AuthMethod,
			Password: srv.Password, KeyPath: srv.KeyPath, Passphrase: srv.Passphrase,
		}
		client, err := sshclient.Connect(cfg)
		if err != nil {
			return ScriptDoneMsg{Index: idx, Err: err}
		}
		defer client.Close()
		output, err := sshclient.ExecuteScript(client, content)
		_ = time.Since(start)
		return ScriptDoneMsg{Index: idx, Output: output, Err: err}
	}
}

func (m ExecutorModel) Update(msg tea.Msg) (ExecutorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case ScriptDoneMsg:
		if msg.Index < len(m.scripts) {
			s := &m.scripts[msg.Index]
			s.dur = time.Since(m.startTime)
			if msg.Err != nil {
				s.status = statusError
				s.err = msg.Err
				s.output = msg.Output
				m.output = append(m.output, theme.ErrorStyle.Render(
					fmt.Sprintf("❌ %s: %v", s.script.Name, msg.Err)))
				if msg.Output != "" {
					m.output = append(m.output, msg.Output)
				}
				m.done = true
				return m, nil
			}
			s.status = statusSuccess
			s.output = msg.Output
			if msg.Output != "" {
				for _, l := range strings.Split(strings.TrimSpace(msg.Output), "\n") {
					m.output = append(m.output, "  "+l)
				}
			}
			m.current++
			if m.current < len(m.scripts) {
				return m, m.runNext()
			}
			m.done = true
		}
	case AllDoneMsg:
		m.done = true
	case tea.KeyMsg:
		if key.Matches(msg, theme.ExecutorKeys.Cancel) {
			if m.done {
				return m, func() tea.Msg { return ExecutionCancelledMsg{} }
			}
			m.cancelled = true
			m.done = true
		}
	}
	return m, nil
}

func (m ExecutorModel) View() string {
	var b strings.Builder
	b.WriteString(components.RenderHeader(
		[]string{"Серверы", m.server.Name, "Выполнение"}, m.width))
	b.WriteString("\n")

	for _, s := range m.scripts {
		var icon, dur string
		switch s.status {
		case statusPending:
			icon = theme.MutedStyle.Render(theme.IconPending)
		case statusRunning:
			icon = m.spinner.View()
		case statusSuccess:
			icon = theme.SuccessStyle.Render(theme.IconSuccess)
			dur = theme.MutedStyle.Render(fmt.Sprintf(" %.1fs", s.dur.Seconds()))
		case statusError:
			icon = theme.ErrorStyle.Render(theme.IconError)
			dur = theme.MutedStyle.Render(fmt.Sprintf(" %.1fs", s.dur.Seconds()))
		}
		b.WriteString(fmt.Sprintf("  %s  %s%s\n", icon, s.script.Name, dur))
	}

	if len(m.output) > 0 {
		b.WriteString("\n")
		sep := theme.MutedStyle.Render(strings.Repeat("─", min(50, m.width-4)))
		b.WriteString("  " + sep + "\n")
		start := 0
		if len(m.output) > 15 {
			start = len(m.output) - 15
		}
		for _, line := range m.output[start:] {
			b.WriteString("  " + line + "\n")
		}
	}

	if m.done {
		b.WriteString("\n")
		if m.cancelled {
			b.WriteString("  " + theme.WarningStyle.Render("⚠ Выполнение отменено") + "\n")
		} else {
			ok := true
			for _, s := range m.scripts {
				if s.status == statusError {
					ok = false
					break
				}
			}
			if ok {
				b.WriteString("  " + theme.SuccessStyle.Render("✅ Все скрипты выполнены") + "\n")
			} else {
				b.WriteString("  " + theme.ErrorStyle.Render("❌ Завершено с ошибками") + "\n")
			}
		}
	}

	label := "отмена"
	if m.done {
		label = "назад"
	}
	b.WriteString("\n" + components.RenderStatusBar([]components.StatusItem{
		{Key: "esc", Desc: label},
	}, m.width))
	return lipgloss.NewStyle().MaxWidth(m.width).Render(b.String())
}
