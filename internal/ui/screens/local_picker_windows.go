//go:build windows

package screens

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func openNativeLocalPathPicker(kind localPickerKind, initialDir string) (string, error) {
	powerShellPath, err := findPowerShellExecutable()
	if err != nil {
		return "", err
	}

	cmd := exec.Command(powerShellPath, "-NoProfile", "-STA", "-Command", buildNativeLocalPickerScript(kind, initialDir))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	output, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(output))
	if err != nil {
		if result != "" {
			return "", fmt.Errorf("не удалось открыть окно выбора: %s", result)
		}
		return "", fmt.Errorf("не удалось открыть окно выбора: %w", err)
	}

	return result, nil
}

func findPowerShellExecutable() (string, error) {
	for _, candidate := range []string{"powershell.exe", "pwsh.exe", "pwsh"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("PowerShell не найден")
}

func buildNativeLocalPickerScript(kind localPickerKind, initialDir string) string {
	initialDir = quotePowerShellLiteral(initialDir)

	switch kind {
	case localPickFolder:
		return fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$OutputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::UTF8
$dialog = New-Object System.Windows.Forms.FolderBrowserDialog
$dialog.Description = 'Выберите папку для загрузки'
$dialog.ShowNewFolderButton = $false
$initial = '%s'
if ($initial -and (Test-Path -LiteralPath $initial)) {
    $dialog.SelectedPath = $initial
}
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
    [Console]::Write($dialog.SelectedPath)
}
`, initialDir)
	default:
		return fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$OutputEncoding = [Console]::OutputEncoding = [System.Text.UTF8Encoding]::UTF8
$dialog = New-Object System.Windows.Forms.OpenFileDialog
$dialog.Title = 'Выберите файл для загрузки'
$dialog.CheckFileExists = $true
$dialog.Multiselect = $false
$dialog.RestoreDirectory = $true
$initial = '%s'
if ($initial -and (Test-Path -LiteralPath $initial)) {
    $dialog.InitialDirectory = $initial
}
if ($dialog.ShowDialog() -eq [System.Windows.Forms.DialogResult]::OK) {
    [Console]::Write($dialog.FileName)
}
`, initialDir)
	}
}

func quotePowerShellLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}
