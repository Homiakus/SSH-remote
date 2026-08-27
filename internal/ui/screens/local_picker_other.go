//go:build !windows

package screens

import "fmt"

func openNativeLocalPathPicker(_ localPickerKind, _ string) (string, error) {
	return "", fmt.Errorf("окно выбора пути поддерживается только на Windows")
}
