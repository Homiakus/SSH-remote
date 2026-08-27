package components

import (
	"strings"

	"sshpilot/internal/ui/theme"

	"github.com/charmbracelet/lipgloss"
)

// StatusItem описывает одну пару клавиша-описание в статус-баре.
type StatusItem struct {
	Key  string
	Desc string
}

// RenderStatusBar отрисовывает нижнюю панель с подсказками.
func RenderStatusBar(items []StatusItem, width int) string {
	var parts []string
	for _, item := range items {
		k := theme.StatusKeyStyle.Render(item.Key)
		d := theme.StatusDescStyle.Render(item.Desc)
		parts = append(parts, k+" "+d)
	}

	sep := theme.StatusSepStyle.Render("  │  ")
	bar := strings.Join(parts, sep)

	return lipgloss.NewStyle().
		Width(width).
		Align(lipgloss.Center).
		MarginTop(1).
		Foreground(theme.ColorSubtext).
		Render(bar)
}
