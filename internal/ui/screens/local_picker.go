package screens

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type localPickerKind int

const (
	localPickFile localPickerKind = iota
	localPickFolder
)

type localPathPicker interface {
	PickFile(initialDir string) (string, error)
	PickFolder(initialDir string) (string, error)
}

type defaultLocalPathPicker struct{}

type fileLocalPathPickedMsg struct {
	kind localPickerKind
	path string
	err  error
}

func (defaultLocalPathPicker) PickFile(initialDir string) (string, error) {
	return openNativeLocalPathPicker(localPickFile, initialDir)
}

func (defaultLocalPathPicker) PickFolder(initialDir string) (string, error) {
	return openNativeLocalPathPicker(localPickFolder, initialDir)
}

func pickLocalPathCmd(picker localPathPicker, kind localPickerKind, initialValue string) tea.Cmd {
	return func() tea.Msg {
		if picker == nil {
			return fileLocalPathPickedMsg{
				kind: kind,
				err:  fmt.Errorf("локальный выбор пути недоступен"),
			}
		}

		initialDir := resolveLocalPickerInitialDir(initialValue)

		var (
			selectedPath string
			err          error
		)

		switch kind {
		case localPickFolder:
			selectedPath, err = picker.PickFolder(initialDir)
		default:
			selectedPath, err = picker.PickFile(initialDir)
		}

		return fileLocalPathPickedMsg{
			kind: kind,
			path: strings.TrimSpace(selectedPath),
			err:  err,
		}
	}
}

func resolveLocalPickerInitialDir(currentValue string) string {
	cleanValue := strings.TrimSpace(currentValue)
	if cleanValue != "" {
		cleanPath := filepath.Clean(cleanValue)
		if info, err := os.Stat(cleanPath); err == nil {
			if info.IsDir() {
				return cleanPath
			}
			return filepath.Dir(cleanPath)
		}

		parentDir := filepath.Dir(cleanPath)
		if parentDir != "." && parentDir != "" {
			if info, err := os.Stat(parentDir); err == nil && info.IsDir() {
				return parentDir
			}
		}
	}

	homeDir, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(homeDir) != "" {
		return homeDir
	}
	return "."
}
