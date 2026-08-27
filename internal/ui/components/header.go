package components

import (
	"strings"

	"sshpilot/internal/ui/theme"

	"github.com/charmbracelet/lipgloss"
)

// RenderHeader отрисовывает заголовок приложения с хлебными крошками.
func RenderHeader(breadcrumbs []string, width int) string {
	title := theme.TitleStyle.Render(" ⚡ SSHPilot ")

	var crumbs string
	if len(breadcrumbs) > 0 {
		var parts []string
		for i, b := range breadcrumbs {
			if i == len(breadcrumbs)-1 {
				parts = append(parts, theme.BreadcrumbActiveStyle.Render(b))
			} else {
				parts = append(parts, theme.BreadcrumbStyle.Render(b))
			}
		}
		sep := theme.MutedStyle.Render(" → ")
		crumbs = strings.Join(parts, sep)
	}

	header := lipgloss.JoinHorizontal(lipgloss.Center, title, "  ", crumbs)

	return lipgloss.NewStyle().
		Width(width).
		MarginBottom(1).
		Render(header)
}
