package screens

import (
	"fmt"
	"strings"

	"sshpilot/internal/scripts"
	"sshpilot/internal/ui/theme"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ──────────────── Messages ────────────────

type scriptConfirmedMsg struct {
	scripts []scripts.Script
}

type scriptCancelledMsg struct{}

// ──────────────── Model ────────────────

// scriptConfirmModel — экран подтверждения перед выполнением скриптов.
type scriptConfirmModel struct {
	scripts  []scripts.Script
	content  string
	server   string
	width    int
	height   int
}

func newScriptConfirmModel(server string, ss []scripts.Script, content string) scriptConfirmModel {
	return scriptConfirmModel{
		server:  server,
		scripts: ss,
		content: content,
	}
}

func (m scriptConfirmModel) Init() tea.Cmd { return nil }

func (m scriptConfirmModel) Update(msg tea.Msg) (scriptConfirmModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			return m, func() tea.Msg { return scriptConfirmedMsg{scripts: m.scripts} }
		case "n", "N", "esc":
			return m, func() tea.Msg { return scriptCancelledMsg{} }
		}
	}
	return m, nil
}

func (m scriptConfirmModel) View() string {
	var b strings.Builder

	// Заголовок
	title := theme.WarningStyle.Render("⚠ Подтверждение выполнения скриптов")
	b.WriteString(title + "\n\n")

	// Сервер
	b.WriteString(fmt.Sprintf("Сервер: %s\n", theme.BreadcrumbActiveStyle.Render(m.server)))

	// Список скриптов
	b.WriteString("\nСкрипты:\n")
	for _, s := range m.scripts {
		kind := string(s.Kind)
		if kind == "" {
			kind = "sh"
		}
		b.WriteString(fmt.Sprintf("  • %s [%s]\n", s.Name, kind))
	}

	// Содержимое первого скрипта для предпросмотра
	if m.content != "" {
		b.WriteString("\n" + theme.MutedStyle.Render(strings.Repeat("─", 40)) + "\n")
		preview := m.content
		if len(preview) > 500 {
			preview = preview[:500] + "\n... (обрезано)"
		}
		b.WriteString(theme.MutedStyle.Render(preview))
		b.WriteString("\n" + theme.MutedStyle.Render(strings.Repeat("─", 40)) + "\n")
	}

	// Подсказка
	b.WriteString("\n" + theme.MutedStyle.Render("Y — подтвердить, N / Esc — отмена"))

	return lipgloss.NewStyle().
		Width(min(m.width-4, 80)).
		Padding(1, 2).
		Render(b.String())
}

// waitForConfirmCmd возвращает команду, показывающую диалог подтверждения.
// Используется для интеграции в существующий Flow без переписывания всей логики.
func waitForConfirmCmd(server string, ss []scripts.Script, content string) tea.Cmd {
	return func() tea.Msg {
		return scriptConfirmedMsg{scripts: ss}
	}
}
