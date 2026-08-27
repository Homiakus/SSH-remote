package screens

import (
	"fmt"
	"strings"

	"sshpilot/internal/config"
	"sshpilot/internal/ui/components"
	"sshpilot/internal/ui/theme"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ServerSavedMsg struct{ Name string }
type CancelEditMsg struct{}

const (
	fieldName = iota
	fieldHost
	fieldPort
	fieldUser
	fieldAuthMethod
	fieldPassword
	fieldKeyPath
	fieldPassphrase
	fieldDescription
	numFields
)

var fieldLabels = []string{
	"Имя сервера", "Хост (IP/домен)", "Порт", "Пользователь",
	"Метод авторизации", "Пароль", "Путь к ключу", "Пароль ключа", "Описание",
}

type ServerEditModel struct {
	inputs        []textinput.Model
	focusIndex    int
	isNew         bool
	originalName  string
	width, height int
	message       string
}

func NewServerEditModel(server *config.ServerConfig) ServerEditModel {
	inputs := make([]textinput.Model, numFields)
	placeholders := []string{
		"my-server", "192.168.1.100", "22 (по умолчанию)", "root",
		"password или key", "пароль", "keys/my-server.ed25519", "(опционально)", "Описание",
	}
	for i := range inputs {
		t := textinput.New()
		t.CharLimit = 256
		t.Width = 40
		t.Placeholder = placeholders[i]
		if i == fieldPassword || i == fieldPassphrase {
			t.EchoMode = textinput.EchoPassword
		}
		inputs[i] = t
	}

	m := ServerEditModel{inputs: inputs, isNew: server == nil}

	if server != nil {
		m.originalName = server.Name
		vals := []string{server.Name, server.Host, server.Port, server.User,
			server.AuthMethod, server.Password, server.KeyPath, server.Passphrase, server.Description}
		for i, v := range vals {
			inputs[i].SetValue(v)
		}
	} else {
		inputs[fieldAuthMethod].SetValue(config.AuthMethodPassword)
	}
	inputs[0].Focus()
	m.inputs = inputs
	return m
}

func (m ServerEditModel) Init() tea.Cmd { return textinput.Blink }

func (m ServerEditModel) Update(msg tea.Msg) (ServerEditModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, theme.FormKeys.Cancel):
			return m, func() tea.Msg { return CancelEditMsg{} }
		case key.Matches(msg, theme.FormKeys.Save):
			return m, m.save()
		case key.Matches(msg, theme.FormKeys.NextField):
			m.focusIndex = (m.focusIndex + 1) % numFields
			return m, m.updateFocus()
		case key.Matches(msg, theme.FormKeys.PrevField):
			m.focusIndex = (m.focusIndex - 1 + numFields) % numFields
			return m, m.updateFocus()
		}
	}
	var cmd tea.Cmd
	m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
	return m, cmd
}

func (m *ServerEditModel) updateFocus() tea.Cmd {
	cmds := make([]tea.Cmd, numFields)
	for i := range m.inputs {
		if i == m.focusIndex {
			cmds[i] = m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (m *ServerEditModel) save() tea.Cmd {
	name := strings.TrimSpace(m.inputs[fieldName].Value())
	host := strings.TrimSpace(m.inputs[fieldHost].Value())
	user := strings.TrimSpace(m.inputs[fieldUser].Value())

	if name == "" {
		m.message = theme.ErrorStyle.Render("? Имя сервера не может быть пустым")
		return nil
	}
	if err := config.ValidateServerName(name); err != nil {
		m.message = theme.ErrorStyle.Render(fmt.Sprintf("? %v", err))
		return nil
	}
	if host == "" {
		m.message = theme.ErrorStyle.Render("? Хост не может быть пустым")
		return nil
	}
	if user == "" {
		m.message = theme.ErrorStyle.Render("? Пользователь не может быть пустым")
		return nil
	}

	port := strings.TrimSpace(m.inputs[fieldPort].Value())
	auth := config.NormalizeAuthMethod(m.inputs[fieldAuthMethod].Value())

	cfg := &config.ServerConfig{
		Name: name, Host: host, Port: port, User: user, AuthMethod: auth,
		Password: m.inputs[fieldPassword].Value(), KeyPath: m.inputs[fieldKeyPath].Value(),
		Passphrase: m.inputs[fieldPassphrase].Value(), Description: m.inputs[fieldDescription].Value(),
	}

	if err := config.SaveServer(name, cfg); err != nil {
		m.message = theme.ErrorStyle.Render(fmt.Sprintf("? Ошибка: %v", err))
		return nil
	}
	if !m.isNew && m.originalName != "" && m.originalName != name {
		if err := config.DeleteServer(m.originalName); err != nil {
			m.message = theme.WarningStyle.Render(fmt.Sprintf("? Новый конфиг сохранён, но старый '%s' не удалён: %v", m.originalName, err))
			return nil
		}
	}
	n := name
	return func() tea.Msg { return ServerSavedMsg{Name: n} }
}

func (m ServerEditModel) View() string {
	var b strings.Builder
	title := "Новый сервер"
	if !m.isNew {
		title = "Редактирование: " + m.originalName
	}
	b.WriteString(components.RenderHeader([]string{"Серверы", title}, m.width))
	b.WriteString("\n")

	auth := config.NormalizeAuthMethod(m.inputs[fieldAuthMethod].Value())
	for i, input := range m.inputs {
		if auth == config.AuthMethodKey && i == fieldPassword {
			continue
		}
		if auth != config.AuthMethodKey && (i == fieldKeyPath || i == fieldPassphrase) {
			continue
		}
		label := theme.LabelStyle.Render(fieldLabels[i])
		cur := "  "
		if i == m.focusIndex {
			cur = theme.SelectedItemStyle.Render(theme.IconArrow + " ")
			label = lipgloss.NewStyle().Width(18).Render(theme.FocusedInputStyle.Render(fieldLabels[i]))
		}
		b.WriteString(fmt.Sprintf("%s%s  %s\n", cur, label, input.View()))
	}

	if m.message != "" {
		b.WriteString("\n" + m.message + "\n")
	}
	for _, warning := range m.passwordWarnings() {
		b.WriteString("\n" + theme.WarningStyle.Render("? "+warning) + "\n")
	}
	b.WriteString("\n" + components.RenderStatusBar([]components.StatusItem{
		{Key: "tab", Desc: "след. поле"}, {Key: "ctrl+s", Desc: "сохранить"}, {Key: "esc", Desc: "отмена"},
	}, m.width))
	return b.String()
}

func (m ServerEditModel) passwordWarnings() []string {
	if config.NormalizeAuthMethod(m.inputs[fieldAuthMethod].Value()) != config.AuthMethodPassword {
		return nil
	}

	return config.PasswordWarnings(m.inputs[fieldPassword].Value())
}
