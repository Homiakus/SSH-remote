package scripts

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListPackagesIncludesGoAppsAndShellScripts(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	tempDir := t.TempDir()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir temp dir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(origWD); err != nil {
			t.Fatalf("restore wd: %v", err)
		}
	})

	shellPath := filepath.Join(tempDir, "scripts", "maintenance", "01_cleanup.sh")
	if err := os.MkdirAll(filepath.Dir(shellPath), 0o755); err != nil {
		t.Fatalf("mkdir shell package: %v", err)
	}
	if err := os.WriteFile(shellPath, []byte("#!/usr/bin/env bash\necho ok\n"), 0o644); err != nil {
		t.Fatalf("write shell script: %v", err)
	}

	goMainPath := filepath.Join(tempDir, "scripts", "go", "demo-app", "main.go")
	if err := os.MkdirAll(filepath.Dir(goMainPath), 0o755); err != nil {
		t.Fatalf("mkdir go app: %v", err)
	}
	if err := os.WriteFile(goMainPath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write go app: %v", err)
	}

	goHelperPath := filepath.Join(tempDir, "scripts", "go", "helper-lib", "helper.go")
	if err := os.MkdirAll(filepath.Dir(goHelperPath), 0o755); err != nil {
		t.Fatalf("mkdir helper dir: %v", err)
	}
	if err := os.WriteFile(goHelperPath, []byte("package helper\n"), 0o644); err != nil {
		t.Fatalf("write helper package: %v", err)
	}

	packages, err := ListPackages()
	if err != nil {
		t.Fatalf("ListPackages() error = %v", err)
	}

	if len(packages) != 2 {
		t.Fatalf("packages count = %d, want 2", len(packages))
	}

	if packages[0].Name != "go" {
		t.Fatalf("first package = %q, want go", packages[0].Name)
	}
	if len(packages[0].Scripts) != 1 {
		t.Fatalf("go package scripts = %d, want 1", len(packages[0].Scripts))
	}
	if packages[0].Scripts[0].Kind != ScriptKindGoApp {
		t.Fatalf("go script kind = %q, want %q", packages[0].Scripts[0].Kind, ScriptKindGoApp)
	}
	if packages[0].Scripts[0].BuildPath == "" {
		t.Fatal("go script BuildPath should not be empty")
	}

	if packages[1].Name != "maintenance" {
		t.Fatalf("second package = %q, want maintenance", packages[1].Name)
	}
	if len(packages[1].Scripts) != 1 {
		t.Fatalf("maintenance scripts = %d, want 1", len(packages[1].Scripts))
	}
	if packages[1].Scripts[0].Kind != ScriptKindShell {
		t.Fatalf("shell script kind = %q, want %q", packages[1].Scripts[0].Kind, ScriptKindShell)
	}
}

func TestPrepareGoBuildCreatesTemporaryArtifactPath(t *testing.T) {
	tempDir := t.TempDir()

	goModPath := filepath.Join(tempDir, "go.mod")
	if err := os.WriteFile(goModPath, []byte("module example.com/test\n\ngo 1.26.0\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	appDir := filepath.Join(tempDir, "scripts", "go", "demo-app")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatalf("mkdir app dir: %v", err)
	}

	mainPath := filepath.Join(appDir, "main.go")
	if err := os.WriteFile(mainPath, []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	script := Script{
		Name:      "demo-app",
		Path:      appDir,
		Package:   "go",
		Kind:      ScriptKindGoApp,
		BuildPath: appDir,
	}

	plan, cleanup, err := PrepareGoBuild(script, BuildOptions{GOOS: "linux", GOARCH: "amd64"})
	if err != nil {
		t.Fatalf("PrepareGoBuild() error = %v", err)
	}
	if cleanup == nil {
		t.Fatal("cleanup should not be nil")
	}
	if plan.WorkDir != appDir {
		t.Fatalf("plan.WorkDir = %q, want %q", plan.WorkDir, appDir)
	}
	if got, want := plan.Env, []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"}; len(got) != len(want) {
		t.Fatalf("plan.Env len = %d, want %d (%v)", len(got), len(want), got)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("plan.Env[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	}

	tempBuildDir := filepath.Dir(plan.ArtifactPath)
	if filepath.Base(plan.ArtifactPath) != script.Name {
		t.Fatalf("artifact basename = %q, want %q", filepath.Base(plan.ArtifactPath), script.Name)
	}

	if _, err := os.Stat(tempBuildDir); err != nil {
		t.Fatalf("build dir stat error: %v", err)
	}

	cleanup()

	if _, err := os.Stat(tempBuildDir); !os.IsNotExist(err) {
		t.Fatalf("build dir should be removed after cleanup, stat err = %v", err)
	}
}
