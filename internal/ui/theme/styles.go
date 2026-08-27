package theme

import "github.com/charmbracelet/lipgloss"

// ──────────────────────── Цветовая палитра ────────────────────────

var (
	ColorPrimary   = lipgloss.Color("#7C3AED")
	ColorSecondary = lipgloss.Color("#06B6D4")
	ColorAccent    = lipgloss.Color("#F472B6")
	ColorSuccess   = lipgloss.Color("#34D399")
	ColorWarning   = lipgloss.Color("#FBBF24")
	ColorDanger    = lipgloss.Color("#F87171")
	ColorMuted     = lipgloss.Color("#6B7280")
	ColorText      = lipgloss.Color("#E5E7EB")
	ColorSubtext   = lipgloss.Color("#9CA3AF")
	ColorBg        = lipgloss.Color("#111827")
	ColorBgLight   = lipgloss.Color("#1F2937")
	ColorBorder    = lipgloss.Color("#374151")
)

// ──────────────────────── Стили заголовков ────────────────────────

var (
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(ColorPrimary).
			Padding(0, 2).
			MarginBottom(1)

	SubtitleStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true).
			MarginBottom(1)

	BreadcrumbStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	BreadcrumbActiveStyle = lipgloss.NewStyle().
				Foreground(ColorAccent).
				Bold(true)
)

// ──────────────────────── Стили списков ────────────────────────

var (
	ItemStyle = lipgloss.NewStyle().
			Foreground(ColorText).
			PaddingLeft(2)

	SelectedItemStyle = lipgloss.NewStyle().
				Foreground(ColorPrimary).
				Bold(true).
				PaddingLeft(2)

	ItemDescStyle = lipgloss.NewStyle().
			Foreground(ColorSubtext).
			PaddingLeft(4)
)

// ──────────────────────── Стили рамок ────────────────────────

var (
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Padding(1, 2)

	ActiveBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorPrimary).
			Padding(1, 2)

	OutputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorSecondary).
			Padding(0, 1)
)

// ──────────────────────── Стили статусов ────────────────────────

var (
	SuccessStyle = lipgloss.NewStyle().Foreground(ColorSuccess)
	ErrorStyle   = lipgloss.NewStyle().Foreground(ColorDanger)
	WarningStyle = lipgloss.NewStyle().Foreground(ColorWarning)
	SpinnerStyle = lipgloss.NewStyle().Foreground(ColorSecondary)
	MutedStyle   = lipgloss.NewStyle().Foreground(ColorMuted)
)

// ──────────────────────── Стили формы ────────────────────────

var (
	LabelStyle = lipgloss.NewStyle().
			Foreground(ColorSecondary).
			Bold(true).
			Width(18)

	InputStyle = lipgloss.NewStyle().
			Foreground(ColorText)

	FocusedInputStyle = lipgloss.NewStyle().
				Foreground(ColorPrimary).
				Bold(true)
)

// ──────────────────────── Стили статус-бара ────────────────────────

var (
	StatusBarStyle  = lipgloss.NewStyle().Foreground(ColorSubtext).MarginTop(1)
	StatusKeyStyle  = lipgloss.NewStyle().Foreground(ColorPrimary).Bold(true)
	StatusDescStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	StatusSepStyle  = lipgloss.NewStyle().Foreground(ColorBorder)
)

// ──────────────────────── Иконки ────────────────────────

const (
	IconServer    = "⚡"
	IconOnline    = "●"
	IconOffline   = "○"
	IconFolder    = "📁"
	IconScript    = "📄"
	IconSuccess   = "✅"
	IconError     = "❌"
	IconRunning   = "⏳"
	IconPending   = "⏸"
	IconArrow     = "▸"
	IconArrowDown = "▾"
	IconDot       = "·"
	IconBranch    = "├──"
	IconBranchEnd = "└──"
)
