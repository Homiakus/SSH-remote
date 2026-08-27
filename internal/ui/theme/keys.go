package theme

import "github.com/charmbracelet/bubbles/key"

type GlobalKeyMap struct {
	Quit key.Binding
	Help key.Binding
	Back key.Binding
}

var GlobalKeys = GlobalKeyMap{
	Quit: key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "выход")),
	Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "помощь")),
	Back: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "назад")),
}

type ServerListKeyMap struct {
	Up, Down, Enter, Add, Edit, Delete, Test, SetupKey, Refresh key.Binding
}

var ServerListKeys = ServerListKeyMap{
	Up:       key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("up/k", "вверх")),
	Down:     key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("down/j", "вниз")),
	Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "подключиться")),
	Add:      key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "добавить")),
	Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "редактировать")),
	Delete:   key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "удалить")),
	Test:     key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "диагностика")),
	SetupKey: key.NewBinding(key.WithKeys("ctrl+k"), key.WithHelp("ctrl+k", "ssh-ключ")),
	Refresh:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "обновить")),
}

type FormKeyMap struct {
	NextField, PrevField, Save, Cancel key.Binding
}

var FormKeys = FormKeyMap{
	NextField: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "след. поле")),
	PrevField: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "пред. поле")),
	Save:      key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("ctrl+s", "сохранить")),
	Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "отмена")),
}

type ScriptListKeyMap struct {
	Up, Down, Enter, Toggle, Expand, RunAll key.Binding
}

var ScriptListKeys = ScriptListKeyMap{
	Up:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("up/k", "вверх")),
	Down:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("down/j", "вниз")),
	Enter:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "запустить пакет")),
	Toggle: key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "выбрать/снять")),
	Expand: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "развернуть/свернуть")),
	RunAll: key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "запустить выбранные")),
}

type ExecutorKeyMap struct {
	Cancel, ScrollUp, ScrollDown key.Binding
}

var ExecutorKeys = ExecutorKeyMap{
	Cancel:     key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "отменить")),
	ScrollUp:   key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("up/k", "вверх")),
	ScrollDown: key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("down/j", "вниз")),
}
