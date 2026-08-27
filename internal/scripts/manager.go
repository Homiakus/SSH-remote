package scripts

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	scriptsDir            = "scripts"
	defaultRemoteRootName = ".sshpilot"
	manifestName          = "script.yaml"
)

type ScriptKind string

const (
	ScriptKindSH     ScriptKind = "sh"
	ScriptKindGo     ScriptKind = "go"
	ScriptKindBinary ScriptKind = "binary"

	// Legacy aliases kept for compatibility with the existing UI/tests.
	ScriptKindShell ScriptKind = ScriptKindSH
	ScriptKindGoApp ScriptKind = ScriptKindGo
)

// Script описывает один runnable-элемент.
type Script struct {
	Name      string
	Path      string
	Package   string
	Kind      ScriptKind
	BuildPath string

	EntryPath      string
	RemotePath     string
	RemoteDir      string
	RemoteName     string
	Chmod          os.FileMode
	RunArgs        []string
	Env            []string
	BuildDir       string
	Interpreter    string
	TargetPlatform string
	ManifestPath   string
}

// ScriptPackage описывает пакет (папку) скриптов.
type ScriptPackage struct {
	Name    string
	Scripts []Script
}

type scriptManifest struct {
	Name           string       `yaml:"name"`
	Kind           ScriptKind   `yaml:"kind"`
	EntryPath      string       `yaml:"entry_path"`
	RemoteDir      string       `yaml:"remote_dir"`
	RemoteName     string       `yaml:"remote_name"`
	Chmod          manifestMode `yaml:"chmod"`
	RunArgs        []string     `yaml:"run_args"`
	Env            manifestEnv  `yaml:"env"`
	BuildDir       string       `yaml:"build_dir"`
	Interpreter    string       `yaml:"interpreter"`
	TargetPlatform string       `yaml:"target_platform"`
}

type manifestMode struct {
	value os.FileMode
	set   bool
}

type manifestEnv []string

func (m *manifestMode) UnmarshalYAML(node *yaml.Node) error {
	if node == nil {
		return nil
	}

	switch node.Kind {
	case yaml.ScalarNode:
		text := strings.TrimSpace(node.Value)
		if text == "" {
			return nil
		}
		base := 8
		if strings.HasPrefix(text, "0o") || strings.HasPrefix(text, "0O") {
			text = text[2:]
		}
		value, err := strconv.ParseUint(text, base, 32)
		if err != nil {
			return fmt.Errorf("chmod должен быть восьмеричным числом, получено %q", node.Value)
		}
		m.value = os.FileMode(value)
		m.set = true
		return nil
	default:
		return fmt.Errorf("chmod должен быть scalar-значением")
	}
}

func (m manifestMode) Value() os.FileMode {
	return m.value
}

func (m manifestMode) IsSet() bool {
	return m.set
}

func (e *manifestEnv) UnmarshalYAML(node *yaml.Node) error {
	if node == nil {
		return nil
	}

	switch node.Kind {
	case 0:
		return nil
	case yaml.SequenceNode:
		var values []string
		if err := node.Decode(&values); err != nil {
			return err
		}
		*e = append((*e)[:0], values...)
		return nil
	case yaml.MappingNode:
		if len(node.Content)%2 != 0 {
			return fmt.Errorf("env должен быть map или list")
		}
		type envEntry struct {
			key   string
			value string
		}
		pairs := make([]envEntry, 0, len(node.Content)/2)
		for i := 0; i < len(node.Content); i += 2 {
			key := strings.TrimSpace(node.Content[i].Value)
			value := strings.TrimSpace(node.Content[i+1].Value)
			if key == "" {
				return fmt.Errorf("env содержит пустой ключ")
			}
			pairs = append(pairs, envEntry{key: key, value: value})
		}
		sort.Slice(pairs, func(i, j int) bool {
			return pairs[i].key < pairs[j].key
		})
		values := make([]string, 0, len(pairs))
		for _, pair := range pairs {
			values = append(values, pair.key+"="+pair.value)
		}
		*e = values
		return nil
	default:
		return fmt.Errorf("env должен быть map или list")
	}
}

// EnsureScriptsDir создаёт папку scripts/ если она не существует.
func EnsureScriptsDir() error {
	return os.MkdirAll(scriptsDir, 0o755)
}

// ListPackages сканирует папку scripts/ и возвращает список пакетов runnable-элементов.
// Каждый пакет — директория, содержащая shell-скрипты, Go-приложения или бинарники.
// Пакеты без валидных скриптов игнорируются.
func ListPackages() ([]ScriptPackage, error) {
	if err := EnsureScriptsDir(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(scriptsDir)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать папку scripts: %w", err)
	}

	var packages []ScriptPackage
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		pkg, err := loadPackage(entry.Name())
		if err != nil {
			continue
		}
		if len(pkg.Scripts) > 0 {
			packages = append(packages, *pkg)
		}
	}

	sort.Slice(packages, func(i, j int) bool {
		return packages[i].Name < packages[j].Name
	})

	return packages, nil
}

// ReadScript считывает содержимое локального shell-скрипта.
func ReadScript(s Script) (string, error) {
	if s.Kind != "" && s.Kind != ScriptKindSH {
		return "", fmt.Errorf("скрипт %s не поддерживает чтение как shell-скрипт", s.Name)
	}

	entryPath := s.LocalEntryPath()
	if entryPath == "" {
		return "", fmt.Errorf("для %s не указан EntryPath", s.Name)
	}

	data, err := os.ReadFile(entryPath)
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать скрипт %s: %w", entryPath, err)
	}
	return string(data), nil
}

func (s Script) LocalEntryPath() string {
	if entryPath := strings.TrimSpace(s.EntryPath); entryPath != "" {
		return filepath.Clean(entryPath)
	}
	if s.Kind == ScriptKindGo {
		return ""
	}
	if p := strings.TrimSpace(s.Path); p != "" {
		return filepath.Clean(p)
	}
	return ""
}

func (s Script) LocalBuildDir() string {
	if buildDir := strings.TrimSpace(s.BuildDir); buildDir != "" {
		return filepath.Clean(buildDir)
	}
	if buildDir := strings.TrimSpace(s.BuildPath); buildDir != "" {
		return filepath.Clean(buildDir)
	}
	if s.Kind == ScriptKindGo && strings.TrimSpace(s.Path) != "" {
		return filepath.Clean(s.Path)
	}
	return ""
}

func (s Script) IsRemoteOnly() bool {
	return strings.TrimSpace(s.RemotePath) != "" && strings.TrimSpace(s.LocalEntryPath()) == ""
}

func (s Script) EffectiveRemoteName() string {
	if remoteName := strings.TrimSpace(s.RemoteName); remoteName != "" {
		return remoteName
	}
	if remotePath := strings.TrimSpace(s.RemotePath); remotePath != "" {
		return path.Base(remotePath)
	}
	if entryPath := s.LocalEntryPath(); entryPath != "" {
		return filepath.Base(entryPath)
	}
	return s.Name
}

func (s Script) ResolveRemoteDir(startDir string) string {
	if remotePath := strings.TrimSpace(s.RemotePath); remotePath != "" {
		return path.Dir(path.Clean(remotePath))
	}

	if remoteDir := strings.TrimSpace(s.RemoteDir); remoteDir != "" {
		if strings.HasPrefix(remoteDir, "/") {
			return path.Clean(remoteDir)
		}
		return path.Join(cleanRemoteBase(startDir), remoteDir)
	}

	return path.Join(
		cleanRemoteBase(startDir),
		defaultRemoteRootName,
		"runnables",
		sanitizeRemotePart(s.Package),
		sanitizeRemotePart(s.Name),
	)
}

func (s Script) ResolveRemotePath(startDir string) string {
	if remotePath := strings.TrimSpace(s.RemotePath); remotePath != "" {
		return path.Clean(remotePath)
	}
	return path.Join(s.ResolveRemoteDir(startDir), s.EffectiveRemoteName())
}

func (s Script) EffectiveChmod() os.FileMode {
	if s.Chmod != 0 {
		return s.Chmod
	}
	return 0o755
}

// DetectScriptInterpreter возвращает интерпретатор для shell runnable.
func DetectScriptInterpreter(s Script) (string, error) {
	if s.Kind != ScriptKindSH {
		return "", fmt.Errorf("%s не является shell runnable", s.Name)
	}
	if interpreter := strings.TrimSpace(s.Interpreter); interpreter != "" {
		return interpreter, nil
	}

	entryPath := s.LocalEntryPath()
	if entryPath == "" {
		return "/usr/bin/env bash", nil
	}

	data, err := os.ReadFile(entryPath)
	if err != nil {
		return "", fmt.Errorf("не удалось прочитать %s для определения shebang: %w", entryPath, err)
	}

	lines := strings.SplitN(string(data), "\n", 2)
	if len(lines) > 0 {
		first := strings.TrimSpace(lines[0])
		if strings.HasPrefix(first, "#!") {
			shebang := strings.TrimSpace(strings.TrimPrefix(first, "#!"))
			if shebang != "" {
				return shebang, nil
			}
		}
	}

	return "/usr/bin/env bash", nil
}

// loadPackage загружает пакет runnable-элементов из папки.
func loadPackage(name string) (*ScriptPackage, error) {
	dir := filepath.Join(scriptsDir, name)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	pkg := &ScriptPackage{Name: name}

	for _, entry := range entries {
		entryPath := filepath.Join(dir, entry.Name())

		if entry.IsDir() {
			manifestPath := filepath.Join(entryPath, manifestName)
			if _, statErr := os.Stat(manifestPath); statErr == nil {
				script, loadErr := loadManifestScript(name, entryPath, manifestPath)
				if loadErr == nil {
					pkg.Scripts = append(pkg.Scripts, script)
				}
				continue
			}

			if name == "go" {
				script, loadErr := loadLegacyGoScript(name, entryPath)
				if loadErr == nil {
					pkg.Scripts = append(pkg.Scripts, script)
				}
			}
			continue
		}

		if !isLegacyShellFile(entry.Name()) {
			continue
		}

		pkg.Scripts = append(pkg.Scripts, Script{
			Name:       entry.Name(),
			Path:       entryPath,
			EntryPath:  entryPath,
			Package:    name,
			Kind:       ScriptKindSH,
			RemoteName: entry.Name(),
		})
	}

	sort.Slice(pkg.Scripts, func(i, j int) bool {
		return pkg.Scripts[i].Name < pkg.Scripts[j].Name
	})

	return pkg, nil
}

func loadManifestScript(pkgName, baseDir, manifestPath string) (Script, error) {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return Script{}, err
	}

	var manifest scriptManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return Script{}, fmt.Errorf("не удалось разобрать %s: %w", manifestPath, err)
	}

	script := Script{
		Name:           strings.TrimSpace(manifest.Name),
		Package:        pkgName,
		Kind:           manifest.Kind,
		RemoteDir:      strings.TrimSpace(manifest.RemoteDir),
		RemoteName:     strings.TrimSpace(manifest.RemoteName),
		RunArgs:        append([]string{}, manifest.RunArgs...),
		Env:            append([]string{}, manifest.Env...),
		Interpreter:    strings.TrimSpace(manifest.Interpreter),
		TargetPlatform: strings.TrimSpace(manifest.TargetPlatform),
		ManifestPath:   manifestPath,
	}

	if script.Name == "" {
		script.Name = filepath.Base(baseDir)
	}
	if manifest.Chmod.IsSet() {
		script.Chmod = manifest.Chmod.Value()
	}

	switch manifest.Kind {
	case ScriptKindGo:
		buildDir := strings.TrimSpace(manifest.BuildDir)
		if buildDir == "" {
			buildDir = baseDir
		}
		buildDir = resolveLocalPath(baseDir, buildDir)
		hasMain, err := hasMainPackage(buildDir)
		if err != nil {
			return Script{}, err
		}
		if !hasMain {
			return Script{}, fmt.Errorf("%s не содержит package main", buildDir)
		}
		script.Path = buildDir
		script.BuildPath = buildDir
		script.BuildDir = buildDir

	case ScriptKindSH, ScriptKindBinary:
		entryPath := strings.TrimSpace(manifest.EntryPath)
		if entryPath == "" {
			return Script{}, fmt.Errorf("в %s требуется entry_path для kind=%s", manifestPath, manifest.Kind)
		}
		entryPath = resolveLocalPath(baseDir, entryPath)
		info, err := os.Stat(entryPath)
		if err != nil {
			return Script{}, fmt.Errorf("не удалось открыть entry_path %s: %w", entryPath, err)
		}
		if info.IsDir() {
			return Script{}, fmt.Errorf("entry_path %s должен быть файлом", entryPath)
		}
		script.Path = entryPath
		script.EntryPath = entryPath
		if manifest.Kind == ScriptKindBinary && script.TargetPlatform == "" {
			return Script{}, fmt.Errorf("в %s требуется target_platform для binary", manifestPath)
		}

	default:
		return Script{}, fmt.Errorf("неподдерживаемый kind в %s: %s", manifestPath, manifest.Kind)
	}

	if script.RemoteName == "" {
		script.RemoteName = script.EffectiveRemoteName()
	}

	return script, nil
}

func loadLegacyGoScript(pkgName, appDir string) (Script, error) {
	hasMain, err := hasMainPackage(appDir)
	if err != nil {
		return Script{}, err
	}
	if !hasMain {
		return Script{}, fmt.Errorf("%s не содержит package main", appDir)
	}

	return Script{
		Name:      filepath.Base(appDir),
		Path:      appDir,
		Package:   pkgName,
		Kind:      ScriptKindGo,
		BuildPath: appDir,
		BuildDir:  appDir,
	}, nil
}

func hasMainPackage(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}

	fileSet := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".go" {
			continue
		}

		filePath := filepath.Join(dir, entry.Name())
		file, err := parser.ParseFile(fileSet, filePath, nil, parser.PackageClauseOnly)
		if err != nil {
			return false, fmt.Errorf("не удалось разобрать %s: %w", filePath, err)
		}
		if file.Name != nil && file.Name.Name == "main" {
			return true, nil
		}
	}

	return false, nil
}

func resolveLocalPath(baseDir, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(baseDir, value))
}

func isLegacyShellFile(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".sh":
		return true
	default:
		return false
	}
}

func cleanRemoteBase(startDir string) string {
	base := strings.TrimSpace(startDir)
	if base == "" {
		return "/"
	}
	return path.Clean("/" + strings.TrimPrefix(base, "/"))
}

func sanitizeRemotePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	value = strings.ReplaceAll(value, "\\", "-")
	value = strings.ReplaceAll(value, "/", "-")
	value = strings.ReplaceAll(value, " ", "-")
	return value
}
