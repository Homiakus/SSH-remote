package screens

import (
	"fmt"
	"strings"

	"sshpilot/internal/config"
	"sshpilot/internal/scripts"
	"sshpilot/internal/ui/components"
	"sshpilot/internal/ui/theme"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

type RunScriptsMsg struct {
	Server  config.ServerConfig
	Scripts []scripts.Script
}

type scriptEntry struct {
	isPackage bool
	pkg       scripts.ScriptPackage
	script    scripts.Script
	selected  bool
	expanded  bool
}

type ScriptListModel struct {
	server  config.ServerConfig
	entries []scriptEntry
	cursor  int
	width   int
	height  int
}

func NewScriptListModel(server config.ServerConfig) ScriptListModel {
	pkgs, _ := scripts.ListPackages()
	m := ScriptListModel{server: server}
	for _, pkg := range pkgs {
		m.entries = append(m.entries, scriptEntry{isPackage: true, pkg: pkg})
	}
	if len(m.entries) > 0 {
		m.toggleExpand(0)
	}
	return m
}

func (m *ScriptListModel) toggleExpand(idx int) {
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

func (m ScriptListModel) getSelected() []scripts.Script {
	var r []scripts.Script
	for _, e := range m.entries {
		if !e.isPackage && e.selected {
			r = append(r, e.script)
		}
	}
	return r
}

func (m ScriptListModel) Init() tea.Cmd { return nil }

func (m ScriptListModel) Update(msg tea.Msg) (ScriptListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, theme.ScriptListKeys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, theme.ScriptListKeys.Down):
			if m.cursor < len(m.entries)-1 {
				m.cursor++
			}
		case key.Matches(msg, theme.ScriptListKeys.Expand):
			if m.cursor < len(m.entries) && m.entries[m.cursor].isPackage {
				m.toggleExpand(m.cursor)
			}
		case key.Matches(msg, theme.ScriptListKeys.Toggle):
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
		case key.Matches(msg, theme.ScriptListKeys.Enter):
			sel := m.getSelected()
			if len(sel) == 0 && m.cursor < len(m.entries) && m.entries[m.cursor].isPackage {
				sel = m.entries[m.cursor].pkg.Scripts
			}
			if len(sel) > 0 {
				srv, ss := m.server, sel
				return m, func() tea.Msg { return RunScriptsMsg{Server: srv, Scripts: ss} }
			}
		case key.Matches(msg, theme.ScriptListKeys.RunAll):
			if sel := m.getSelected(); len(sel) > 0 {
				srv, ss := m.server, sel
				return m, func() tea.Msg { return RunScriptsMsg{Server: srv, Scripts: ss} }
			}
		}
	}
	return m, nil
}

func (m ScriptListModel) View() string {
	var b strings.Builder
	b.WriteString(components.RenderHeader([]string{"Серверы", m.server.Name, "Скрипты"}, m.width))
	b.WriteString("\n")

	if len(m.entries) == 0 {
		b.WriteString(theme.MutedStyle.Render("  Нет скриптов. Создайте папки в scripts/"))
	} else {
		for i, e := range m.entries {
			cur := "  "
			if i == m.cursor {
				cur = theme.SelectedItemStyle.Render(theme.IconArrow + " ")
			}
			if e.isPackage {
				icon := theme.IconArrow
				if e.expanded {
					icon = theme.IconArrowDown
				}
				st := theme.ItemStyle
				if i == m.cursor {
					st = theme.SelectedItemStyle
				}
				b.WriteString(fmt.Sprintf("%s%s %s %s  %s\n", cur,
					theme.MutedStyle.Render(icon), theme.IconFolder,
					st.Render(e.pkg.Name),
					theme.MutedStyle.Render(fmt.Sprintf("(%d)", len(e.pkg.Scripts)))))
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
				if i == m.cursor {
					st = theme.SelectedItemStyle
				}
				label := e.script.Name
				if e.script.Kind == scripts.ScriptKindGoApp {
					label += " [go]"
				}
				b.WriteString(fmt.Sprintf("%s    %s %s %s\n", cur,
					theme.MutedStyle.Render(pre), sel, st.Render(label)))
			}
		}
	}

	if s := m.getSelected(); len(s) > 0 {
		b.WriteString("\n" + theme.SuccessStyle.Render(fmt.Sprintf("  Выбрано: %d", len(s))))
	}
	b.WriteString("\n" + components.RenderStatusBar([]components.StatusItem{
		{Key: "enter", Desc: "запустить"}, {Key: "space", Desc: "выбрать"},
		{Key: "tab", Desc: "развернуть"}, {Key: "esc", Desc: "назад"},
	}, m.width))
	return b.String()
}
