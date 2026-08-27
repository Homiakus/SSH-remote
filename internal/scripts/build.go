package scripts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type BuildOptions struct {
	GOOS   string
	GOARCH string
}

type GoBuildPlan struct {
	WorkDir      string
	ArtifactPath string
	Env          []string
}

// PrepareGoBuild подготавливает окружение локальной сборки Go-приложения.
func PrepareGoBuild(script Script, opts BuildOptions) (GoBuildPlan, func(), error) {
	if script.Kind != ScriptKindGo {
		return GoBuildPlan{}, nil, fmt.Errorf("script %s не является Go-приложением", script.Name)
	}
	buildDir := script.LocalBuildDir()
	if buildDir == "" {
		return GoBuildPlan{}, nil, fmt.Errorf("для %s не указан BuildDir", script.Name)
	}
	if opts.GOOS == "" || opts.GOARCH == "" {
		return GoBuildPlan{}, nil, fmt.Errorf("для сборки %s требуется GOOS и GOARCH", script.Name)
	}

	tempDir, err := os.MkdirTemp("", "sshpilot-go-build-*")
	if err != nil {
		return GoBuildPlan{}, nil, fmt.Errorf("не удалось создать временную директорию сборки: %w", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(tempDir)
	}

	outputPath := filepath.Join(tempDir, script.Name)

	return GoBuildPlan{
		WorkDir:      buildDir,
		ArtifactPath: outputPath,
		Env: []string{
			"GOOS=" + opts.GOOS,
			"GOARCH=" + opts.GOARCH,
			"CGO_ENABLED=0",
		},
	}, cleanup, nil
}

// PrepareLocalBinaryArtifact проверяет локальный бинарник и возвращает путь к артефакту.
func PrepareLocalBinaryArtifact(script Script) (string, func(), error) {
	if script.Kind != ScriptKindBinary {
		return "", nil, fmt.Errorf("script %s не является бинарником", script.Name)
	}

	entryPath := script.LocalEntryPath()
	if entryPath == "" {
		return "", nil, fmt.Errorf("для %s не указан EntryPath", script.Name)
	}

	info, err := os.Stat(entryPath)
	if err != nil {
		return "", nil, fmt.Errorf("не удалось открыть бинарник %s: %w", entryPath, err)
	}
	if info.IsDir() {
		return "", nil, fmt.Errorf("%s должен быть файлом", entryPath)
	}

	return entryPath, nil, nil
}

// ValidateBinaryTargetPlatform проверяет, что таргет бинарника совпадает с сервером.
func ValidateBinaryTargetPlatform(script Script, goos, goarch string) error {
	if script.Kind != ScriptKindBinary {
		return fmt.Errorf("%s не является binary runnable", script.Name)
	}

	target := strings.TrimSpace(script.TargetPlatform)
	if target == "" {
		return fmt.Errorf("для бинарника %s не указан target_platform", script.Name)
	}

	actual := strings.TrimSpace(goos) + "/" + strings.TrimSpace(goarch)
	if target != actual {
		return fmt.Errorf("бинарник %s собран для %s, а сервер использует %s", script.Name, target, actual)
	}

	return nil
}
