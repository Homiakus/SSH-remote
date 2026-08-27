package ssh

import (
	"errors"
	"fmt"
	"sync"

	gossh "golang.org/x/crypto/ssh"

	"sshpilot/internal/config"
)

var ErrManagerClosed = errors.New("shared ssh manager is closed")

type dialClientFunc func(*config.ServerConfig) (*gossh.Client, error)
type checkClientFunc func(*gossh.Client) error
type closeClientFunc func(*gossh.Client) error

// Manager переиспользует одно SSH-соединение между разными подсистемами UI.
type Manager struct {
	cfg config.ServerConfig

	mu         sync.Mutex
	client     *gossh.Client
	generation uint64
	closed     bool

	dialClient  dialClientFunc
	checkClient checkClientFunc
	closeClient closeClientFunc
}

// NewManager создаёт менеджер общего SSH-подключения.
func NewManager(cfg *config.ServerConfig) *Manager {
	return newManagerWithDeps(cfg, Connect, healthCheckClient, closeSSHClient)
}

func newManagerWithDeps(
	cfg *config.ServerConfig,
	dial dialClientFunc,
	check checkClientFunc,
	closeFn closeClientFunc,
) *Manager {
	if dial == nil {
		dial = Connect
	}
	if check == nil {
		check = healthCheckClient
	}
	if closeFn == nil {
		closeFn = closeSSHClient
	}

	return &Manager{
		cfg:         cloneServerConfig(cfg),
		dialClient:  dial,
		checkClient: check,
		closeClient: closeFn,
	}
}

// Client возвращает общее SSH-соединение, создавая его при необходимости.
func (m *Manager) Client() (*gossh.Client, error) {
	client, _, err := m.clientWithGeneration()
	return client, err
}

// Check проверяет, что общее SSH-соединение живо.
// Если существующий транспорт протух, менеджер закрывает его и один раз
// пытается поднять новое соединение автоматически.
func (m *Manager) Check() error {
	_, _, err := m.checkedClient()
	return err
}

// Reset сбрасывает текущее общее соединение, оставляя менеджер пригодным к
// следующему ленивому подключению.
func (m *Manager) Reset() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.invalidateLocked()
}

// Close закрывает текущее соединение и больше не позволяет переоткрыть его.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true
	return m.invalidateLocked()
}

func (m *Manager) checkedClient() (*gossh.Client, uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.checkedClientLocked(true)
}

func (m *Manager) checkedClientLocked(allowReconnect bool) (*gossh.Client, uint64, error) {
	client, generation, err := m.ensureClientLocked()
	if err != nil {
		return nil, generation, err
	}

	if err := m.checkClient(client); err == nil {
		return client, generation, nil
	} else {
		_ = m.invalidateLocked()
		if !allowReconnect {
			return nil, m.generation, err
		}
	}

	client, generation, err = m.ensureClientLocked()
	if err != nil {
		return nil, generation, err
	}

	if err := m.checkClient(client); err != nil {
		_ = m.invalidateLocked()
		return nil, m.generation, err
	}

	return client, generation, nil
}

func (m *Manager) clientWithGeneration() (*gossh.Client, uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.ensureClientLocked()
}

func (m *Manager) ensureClientLocked() (*gossh.Client, uint64, error) {
	if m.closed {
		return nil, m.generation, ErrManagerClosed
	}
	if m.client != nil {
		return m.client, m.generation, nil
	}

	client, err := m.dialClient(&m.cfg)
	if err != nil {
		return nil, m.generation, err
	}

	m.client = client
	m.generation++
	return m.client, m.generation, nil
}

func (m *Manager) invalidateLocked() error {
	if m.client == nil {
		return nil
	}

	err := m.closeClient(m.client)
	m.client = nil
	m.generation++
	return err
}

func healthCheckClient(client *gossh.Client) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("не удалось создать сессию: %w", err)
	}
	defer session.Close()

	if _, err := session.Output("echo ok"); err != nil {
		return fmt.Errorf("не удалось выполнить тестовую команду: %w", err)
	}

	return nil
}

func closeSSHClient(client *gossh.Client) error {
	if client == nil {
		return nil
	}
	return client.Close()
}

func cloneServerConfig(cfg *config.ServerConfig) config.ServerConfig {
	if cfg == nil {
		return config.ServerConfig{}
	}

	return config.ServerConfig{
		Name:        cfg.Name,
		Host:        cfg.Host,
		Port:        cfg.Port,
		User:        cfg.User,
		AuthMethod:  cfg.AuthMethod,
		Password:    cfg.Password,
		KeyPath:     cfg.KeyPath,
		Passphrase:  cfg.Passphrase,
		Description: cfg.Description,
	}
}
