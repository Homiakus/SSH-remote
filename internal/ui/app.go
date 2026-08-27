package ui

import (
	"fmt"

	"sshpilot/internal/config"
	"sshpilot/internal/ssh"
	"sshpilot/internal/ui/screens"
	"sshpilot/internal/ui/theme"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Screen определяет текущий экран.
type Screen int

const (
	ScreenServerList Screen = iota
	ScreenServerEdit
	ScreenConnected
	ScreenHelp
)

// AppModel — корневая модель приложения.
type AppModel struct {
	screen     Screen
	prevScreen Screen
	width      int
	height     int

	serverList screens.ServerListModel
	serverEdit screens.ServerEditModel
	connected  screens.ConnectedModel
	help       screens.HelpModel
}

// NewAppModel создаёт корневую модель.
func NewAppModel() AppModel {
	return AppModel{
		screen:     ScreenServerList,
		serverList: screens.NewServerListModel(),
	}
}

func (m AppModel) windowSizeMsg() tea.WindowSizeMsg {
	return tea.WindowSizeMsg{Width: m.width, Height: m.height}
}

func (m *AppModel) syncServerListSize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.serverList, _ = m.serverList.Update(m.windowSizeMsg())
}

func (m *AppModel) syncServerEditSize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.serverEdit, _ = m.serverEdit.Update(m.windowSizeMsg())
}

func (m *AppModel) syncConnectedSize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.connected, _ = m.connected.Update(m.windowSizeMsg())
}

func (m *AppModel) syncHelpSize() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	m.help, _ = m.help.Update(m.windowSizeMsg())
}

func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		tea.SetWindowTitle("⚡ SSHPilot"),
	)
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if m.screen == ScreenConnected && m.connected.IsTerminalActive() {
			return m.updateCurrentScreen(msg)
		}

		// Глобальные клавиши
		if key.Matches(msg, theme.GlobalKeys.Quit) && m.screen == ScreenServerList {
			return m, tea.Quit
		}
		if key.Matches(msg, theme.GlobalKeys.Help) && m.screen == ScreenConnected && m.connected.CapturesTextInput() {
			return m.updateCurrentScreen(msg)
		}
		if key.Matches(msg, theme.GlobalKeys.Help) && m.screen != ScreenServerEdit && m.screen != ScreenHelp {
			m.prevScreen = m.screen
			m.screen = ScreenHelp
			m.help = screens.NewHelpModel("")
			m.syncHelpSize()
			return m, nil
		}
		if key.Matches(msg, theme.GlobalKeys.Back) {
			if m.screen == ScreenConnected {
				return m.updateCurrentScreen(msg)
			}
			return m.handleBack(msg)
		}

	// ──────────── Навигация между экранами ────────────

	case screens.ServerSelectedMsg:
		m.screen = ScreenConnected
		m.connected = screens.NewConnectedModel(msg.Server)
		m.syncConnectedSize()
		return m, m.connected.Init()

	case screens.EditServerMsg:
		m.screen = ScreenServerEdit
		m.serverEdit = screens.NewServerEditModel(msg.Server)
		m.syncServerEditSize()
		return m, m.serverEdit.Init()

	case screens.ServerSavedMsg:
		m.screen = ScreenServerList
		m.serverList = screens.NewServerListModel()
		m.syncServerListSize()
		return m, nil

	case screens.CancelEditMsg:
		m.screen = ScreenServerList
		m.syncServerListSize()
		return m, nil

	case screens.ConnectedBackMsg:
		m.connected.Close()
		m.screen = ScreenServerList
		m.serverList = screens.NewServerListModel()
		m.syncServerListSize()
		return m, nil

	case screens.CloseHelpMsg:
		m.screen = m.prevScreen
		return m, nil

	case screens.TestConnectionMsg:
		srv := msg.Server
		return m, func() tea.Msg {
			report := ssh.DiagnoseConnection(&config.ServerConfig{
				Name: srv.Name, Host: srv.Host, Port: srv.Port,
				User: srv.User, AuthMethod: srv.AuthMethod,
				Password: srv.Password, KeyPath: srv.KeyPath,
				Passphrase: srv.Passphrase,
			})
			return screens.TestResultMsg{
				ServerName: srv.Name,
				Err:        report.Err,
				Report:     ssh.FormatDiagnosticReport(report),
			}
		}

	case screens.SetupServerKeyMsg:
		srv := msg.Server
		return m, func() tea.Msg {
			cfg := &config.ServerConfig{
				Name: srv.Name, Host: srv.Host, Port: srv.Port,
				User: srv.User, AuthMethod: srv.AuthMethod,
				Password: srv.Password, KeyPath: srv.KeyPath,
				Passphrase: srv.Passphrase, Description: srv.Description,
			}
			keyCfg, err := ssh.SetupGeneratedKeyAuth(cfg)
			if err == nil && keyCfg == nil {
				err = fmt.Errorf("подготовка SSH-ключа вернула пустую конфигурацию")
			}
			if err == nil {
				err = config.SaveServer(srv.Name, keyCfg)
			}
			return screens.ServerKeySetupResultMsg{
				ServerName: srv.Name,
				Err:        err,
			}
		}
	}

	// Делегируем обновление текущему экрану
	return m.updateCurrentScreen(msg)
}

func (m AppModel) handleBack(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.screen {
	case ScreenConnected:
		m.connected.Close()
		m.screen = ScreenServerList
		m.serverList = screens.NewServerListModel()
		return m, nil
	case ScreenHelp:
		m.screen = m.prevScreen
		return m, nil
	default:
		return m.updateCurrentScreen(msg)
	}
}

func (m AppModel) updateCurrentScreen(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.screen {
	case ScreenServerList:
		m.serverList, cmd = m.serverList.Update(msg)
	case ScreenServerEdit:
		m.serverEdit, cmd = m.serverEdit.Update(msg)
	case ScreenConnected:
		m.connected, cmd = m.connected.Update(msg)
	case ScreenHelp:
		m.help, cmd = m.help.Update(msg)
	}
	return m, cmd
}

func (m AppModel) View() string {
	switch m.screen {
	case ScreenServerList:
		return m.serverList.View()
	case ScreenServerEdit:
		return m.serverEdit.View()
	case ScreenConnected:
		return m.connected.View()
	case ScreenHelp:
		return m.help.View()
	default:
		return "Unknown screen"
	}
}
