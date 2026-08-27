package screens

import (
	"strings"

	"sshpilot/internal/ui/components"
	"sshpilot/internal/ui/theme"

	tea "github.com/charmbracelet/bubbletea"
)

type CloseHelpMsg struct{}

type HelpModel struct {
	width, height int
}

func NewHelpModel(screen string) HelpModel { return HelpModel{} }

func (m HelpModel) Init() tea.Cmd { return nil }

func (m HelpModel) Update(msg tea.Msg) (HelpModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if msg.String() == "esc" || msg.String() == "?" || msg.String() == "q" {
			return m, func() tea.Msg { return CloseHelpMsg{} }
		}
	}
	return m, nil
}

func (m HelpModel) View() string {
	var b strings.Builder
	b.WriteString(components.RenderHeader([]string{"Помощь"}, m.width))
	b.WriteString("\n")

	type entry struct{ key, desc string }
	sections := []struct {
		title   string
		entries []entry
	}{
		{"Глобальные", []entry{
			{"q / ctrl+c", "Выход"}, {"?", "Помощь"}, {"esc", "Назад"},
		}},
		{"Список серверов", []entry{
			{"up/down или k/j", "Навигация"}, {"enter", "Подключиться"},
			{"a", "Добавить"}, {"e", "Редактировать"},
			{"d", "Удалить"}, {"t", "Диагностика"}, {"ctrl+k", "Установить SSH-ключ"}, {"r", "Обновить"},
		}},
		{"Форма", []entry{
			{"tab", "Следующее поле"}, {"shift+tab", "Предыдущее"},
			{"ctrl+s", "Сохранить"},
		}},
		{"Скрипты", []entry{
			{"enter", "Запустить"}, {"space", "Выбрать"},
			{"tab", "Развернуть"}, {"ctrl+r", "Запустить выбранные"},
			{"f", "Переключиться в файлы"}, {"t", "Server-вкладка"},
		}},
		{"Файлы", []entry{
			{"f", "Переключиться в скрипты"},
			{"enter", "Открыть папку / превью файла"},
			{"backspace", "На уровень выше"},
			{"e", "Редактировать текстовый файл"},
			{"ctrl+s", "Сохранить в редакторе"},
			{"n / N", "Новый файл / папка"},
			{"r / d", "Переименовать / удалить"},
			{"p", "Изменить права и владельца"},
			{"u / o", "Загрузить / скачать"},
		}},
		{"Терминал", []entry{
			{"enter", "Выполнить команду"},
			{"ctrl+c", "Прервать команду"},
			{"tab / shift+tab", "Переключить вкладку"},
			{"t", "Открыть / выбрать Server"},
			{"esc", "Закрыть терминал / отменить запуск"},
		}},
	}

	for _, sec := range sections {
		b.WriteString("  " + theme.SubtitleStyle.Render(sec.title) + "\n")
		for _, e := range sec.entries {
			k := theme.StatusKeyStyle.Render(pad(e.key, 16))
			d := theme.MutedStyle.Render(e.desc)
			b.WriteString("    " + k + "  " + d + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString(components.RenderStatusBar([]components.StatusItem{
		{Key: "esc", Desc: "закрыть"},
	}, m.width))
	return b.String()
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
