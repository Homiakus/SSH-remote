package screens

import (
	"fmt"
	"strings"

	"sshpilot/internal/config"
	"sshpilot/internal/ui/components"
	"sshpilot/internal/ui/theme"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ServerSelectedMsg struct{ Server config.ServerConfig }
type EditServerMsg struct{ Server *config.ServerConfig }
type RefreshServersMsg struct{}
type ServerDeletedMsg struct{}
type TestConnectionMsg struct{ Server config.ServerConfig }
type SetupServerKeyMsg struct{ Server config.ServerConfig }

type TestResultMsg struct {
	ServerName string
	Err        error
	Report     string
}

type ServerKeySetupResultMsg struct {
	ServerName string
	Err        error
}

type ServerListModel struct {
	servers    []config.ServerConfig
	cursor     int
	width      int
	height     int
	testStatus map[string]string
	message    string
}

func NewServerListModel() ServerListModel {
	servers, message := loadServersForList()
	return ServerListModel{servers: servers, testStatus: make(map[string]string), message: message}
}

func (m ServerListModel) Init() tea.Cmd { return nil }

func (m ServerListModel) Update(msg tea.Msg) (ServerListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height

	case RefreshServersMsg:
		var message string
		m.servers, message = loadServersForList()
		if m.cursor >= len(m.servers) {
			m.cursor = max(0, len(m.servers)-1)
		}
		m.message = message

	case TestResultMsg:
		if msg.Err != nil {
			m.testStatus[msg.ServerName] = "error"
			m.message = renderTestReport(msg.Report, false)
		} else {
			m.testStatus[msg.ServerName] = "ok"
			m.message = renderTestReport(msg.Report, true)
		}

	case ServerKeySetupResultMsg:
		if msg.Err != nil {
			m.testStatus[msg.ServerName] = "error"
			m.message = theme.ErrorStyle.Render(fmt.Sprintf("? SSH-ключ для %s не установлен: %v", msg.ServerName, msg.Err))
		} else {
			m.testStatus[msg.ServerName] = "ok"
			var message string
			m.servers, message = loadServersForList()
			m.message = theme.SuccessStyle.Render(fmt.Sprintf("? SSH-ключ для %s установлен и сохранён", msg.ServerName))
			if message != "" {
				m.message += "\n" + message
			}
		}

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, theme.ServerListKeys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, theme.ServerListKeys.Down):
			if m.cursor < len(m.servers)-1 {
				m.cursor++
			}
		case key.Matches(msg, theme.ServerListKeys.Enter):
			if len(m.servers) > 0 {
				s := m.servers[m.cursor]
				return m, func() tea.Msg { return ServerSelectedMsg{Server: s} }
			}
		case key.Matches(msg, theme.ServerListKeys.Add):
			return m, func() tea.Msg { return EditServerMsg{Server: nil} }
		case key.Matches(msg, theme.ServerListKeys.Edit):
			if len(m.servers) > 0 {
				s := m.servers[m.cursor]
				return m, func() tea.Msg { return EditServerMsg{Server: &s} }
			}
		case key.Matches(msg, theme.ServerListKeys.Delete):
			if len(m.servers) > 0 {
				name := m.servers[m.cursor].Name
				_ = config.DeleteServer(name)
				m.message = theme.WarningStyle.Render(fmt.Sprintf("Сервер '%s' удалён", name))
				return m, func() tea.Msg { return RefreshServersMsg{} }
			}
		case key.Matches(msg, theme.ServerListKeys.Test):
			if len(m.servers) > 0 {
				srv := m.servers[m.cursor]
				m.testStatus[srv.Name] = "testing"
				m.message = theme.SpinnerStyle.Render(fmt.Sprintf("? Диагностика %s...", srv.Name))
				return m, func() tea.Msg { return TestConnectionMsg{Server: srv} }
			}
		case key.Matches(msg, theme.ServerListKeys.SetupKey):
			if len(m.servers) > 0 {
				srv := m.servers[m.cursor]
				m.testStatus[srv.Name] = "testing"
				m.message = theme.SpinnerStyle.Render(fmt.Sprintf("? Устанавливаю SSH-ключ для %s...", srv.Name))
				return m, func() tea.Msg { return SetupServerKeyMsg{Server: srv} }
			}
		case key.Matches(msg, theme.ServerListKeys.Refresh):
			return m, func() tea.Msg { return RefreshServersMsg{} }
		}
	}
	return m, nil
}

func (m ServerListModel) View() string {
	var b strings.Builder
	b.WriteString(components.RenderHeader([]string{"Серверы"}, m.width))
	b.WriteString("\n")

	if len(m.servers) == 0 {
		b.WriteString(lipgloss.NewStyle().
			Foreground(theme.ColorMuted).Italic(true).Padding(2, 4).
			Render("Нет настроенных серверов. Нажмите 'a', чтобы добавить."))
	} else {
		for i, srv := range m.servers {
			cur := "  "
			style := theme.ItemStyle
			if i == m.cursor {
				cur = theme.SelectedItemStyle.Render(theme.IconArrow + " ")
				style = theme.SelectedItemStyle
			}
			statusIcon := theme.MutedStyle.Render(theme.IconOffline)
			if st, ok := m.testStatus[srv.Name]; ok {
				switch st {
				case "ok":
					statusIcon = theme.SuccessStyle.Render(theme.IconOnline)
				case "error":
					statusIcon = theme.ErrorStyle.Render(theme.IconOnline)
				case "testing":
					statusIcon = theme.SpinnerStyle.Render("?")
				}
			}
			name := style.Render(srv.Name)
			host := theme.MutedStyle.Render(formatServerTarget(srv))
			desc := ""
			if srv.Description != "" {
				desc = theme.MutedStyle.Render(" · " + srv.Description)
			}
			b.WriteString(fmt.Sprintf("%s%s %s  %s%s\n", cur, statusIcon, name, host, desc))
		}
	}

	if m.message != "" {
		b.WriteString("\n" + m.message)
	}

	b.WriteString("\n" + components.RenderStatusBar([]components.StatusItem{
		{Key: "enter", Desc: "подключиться"}, {Key: "a", Desc: "добавить"},
		{Key: "e", Desc: "редактировать"}, {Key: "d", Desc: "удалить"},
		{Key: "t", Desc: "диагностика"}, {Key: "ctrl+k", Desc: "ssh-ключ"}, {Key: "?", Desc: "помощь"},
	}, m.width))
	return b.String()
}

func loadServersForList() ([]config.ServerConfig, string) {
	servers, err := config.ListServers()
	if err == nil {
		return servers, ""
	}
	return servers, theme.WarningStyle.Render(fmt.Sprintf("? Некоторые конфиги серверов не загружены: %v", err))
}

func renderTestReport(report string, ok bool) string {
	report = strings.TrimSpace(report)
	if report == "" {
		if ok {
			return theme.SuccessStyle.Render("? Подключение успешно")
		}
		return theme.ErrorStyle.Render("? Подключение не удалось")
	}

	lines := strings.Split(report, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i > 0 {
			b.WriteString("\n")
		}

		switch {
		case i == 0 && ok:
			b.WriteString(theme.SuccessStyle.Render("? " + line))
		case i == 0 && !ok:
			b.WriteString(theme.ErrorStyle.Render("? " + line))
		default:
			b.WriteString(theme.MutedStyle.Render(line))
		}
	}

	return b.String()
}
