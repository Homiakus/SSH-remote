package screens

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sshpilot/internal/config"
	"sshpilot/internal/scripts"
	sshclient "sshpilot/internal/ssh"

	tea "github.com/charmbracelet/bubbletea"
)

func TestConnectedGoLaunchHappyPath(t *testing.T) {
	var operations []string

	fs := newFakeRemoteFS("/home/test")
	fs.recordFn = func(operation string) {
		operations = append(operations, operation)
	}

	buildShell := newFakeTerminalSession()
	close(buildShell.outputCh)
	buildShell.runCommandFn = func(spec localCommandSpec) error {
		operations = append(operations, "run:"+spec.Display)
		buildShell.finishCommand(localCommandResult{Display: spec.Display})
		return nil
	}

	connection := &fakeSSHConnection{
		detectRemotePlatformFn: func() (sshclient.RemotePlatform, error) {
			operations = append(operations, "detect")
			return sshclient.RemotePlatform{GOOS: "linux", GOARCH: "arm64"}, nil
		},
		executeFn: func(content string) (string, error) {
			content = strings.TrimSpace(content)
			if strings.Contains(content, "find_pids()") {
				operations = append(operations, "stop")
				return "", nil
			}
			operations = append(operations, "exec:"+content)
			return "", nil
		},
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			operations = append(operations, "openfs")
			return fs, nil
		},
		nativeTerminalFn: func(command string, height, width int) tea.ExecCommand {
			operations = append(operations, "native:"+command)
			return &fakeExecCommand{}
		},
	}

	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: connection},
	)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.goBuild = func(script scripts.Script, platform sshclient.RemotePlatform) (scripts.GoBuildPlan, func(), error) {
		operations = append(operations, "build:"+platform.GOOS+"/"+platform.GOARCH)
		artifactDir := t.TempDir()
		artifactPath := filepath.Join(artifactDir, script.Name)
		if err := os.WriteFile(artifactPath, []byte("binary"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		return scripts.GoBuildPlan{
			WorkDir:      script.BuildPath,
			ArtifactPath: artifactPath,
			Env:          []string{"GOOS=linux", "GOARCH=arm64", "CGO_ENABLED=0"},
		}, func() {}, nil
	}
	m.localTerminal = func(workDir string, env []string) (trackedTerminalSession, error) {
		operations = append(operations, "local:"+workDir)
		return buildShell, nil
	}

	cmd := m.startExecution([]scripts.Script{{
		Name:      "site-admin-tui",
		Kind:      scripts.ScriptKindGoApp,
		Package:   "go",
		Path:      filepath.Join("scripts", "go", "site-admin-tui"),
		BuildPath: filepath.Join("scripts", "go", "site-admin-tui"),
	}})
	if cmd == nil {
		t.Fatal("expected go launch command")
	}

	m = runConnectedCmd(t, m, cmd)

	targetRemotePath := "/home/test/.sshpilot/runnables/go/site-admin-tui/site-admin-tui"
	wantOps := []string{
		"detect",
		"build:linux/arm64",
		"local:" + filepath.Join("scripts", "go", "site-admin-tui"),
		"run:" + buildCommandDisplay(buildArtifactPathFromSpec(t, buildShell.runSpecs[0])),
		"openfs",
		"exec:set -e\nmkdir -p -- '/home/test/.sshpilot/runnables/go/site-admin-tui'",
		"stop",
	}
	if len(operations) < len(wantOps) {
		t.Fatalf("operations = %#v, want prefix %#v", operations, wantOps)
	}
	for i, want := range wantOps {
		if operations[i] != want {
			t.Fatalf("operation[%d] = %q, want %q\nall=%#v", i, operations[i], want, operations)
		}
	}
	if len(operations) < len(wantOps)+5 {
		t.Fatalf("operations = %#v, want upload/chmod/rename/native after %#v", operations, wantOps)
	}
	uploadOp := operations[len(wantOps)]
	if !strings.HasPrefix(uploadOp, "upload:"+targetRemotePath+".sshpilot-upload-") {
		t.Fatalf("upload operation = %q, want temp upload for %s\nall=%#v", uploadOp, targetRemotePath, operations)
	}
	tempRemotePath := strings.TrimPrefix(uploadOp, "upload:")
	for offset, want := range []string{
		"write:" + tempRemotePath,
		"chmod:" + tempRemotePath,
		"rename:" + tempRemotePath + "->" + targetRemotePath,
		"native:" + shellQuote(targetRemotePath),
	} {
		i := len(wantOps) + 1 + offset
		if operations[i] != want {
			t.Fatalf("operation[%d] = %q, want %q\nall=%#v", i, operations[i], want, operations)
		}
	}

	if m.terminalTab(terminalTabBuild) == nil {
		t.Fatal("expected build tab to remain available")
	}
	if m.terminalTab(terminalTabBuild).buildState != buildTabReady {
		t.Fatalf("build tab state = %v, want ready", m.terminalTab(terminalTabBuild).buildState)
	}
	if len(buildShell.runSpecs) != 1 {
		t.Fatalf("run specs = %#v, want one build command", buildShell.runSpecs)
	}
	if buildShell.runSpecs[0].Name != "go" {
		t.Fatalf("build command = %#v, want go", buildShell.runSpecs[0])
	}
	if len(connection.nativeCommands) != 0 {
		t.Fatalf("native commands should be supplied by nativeTerminalFn in this test: %#v", connection.nativeCommands)
	}
	if operations[len(operations)-1] != "native:"+shellQuote(targetRemotePath) {
		t.Fatalf("launch operation = %q, want native command for %s\nall=%#v", operations[len(operations)-1], targetRemotePath, operations)
	}
	if m.executing {
		t.Fatal("expected go launch to leave executing state after native command exit")
	}

	buildLog := terminalTabText(m, terminalTabBuild)
	for _, want := range []string{
		"Локальная сборка Go-бинарника",
		"Целевая платформа: linux/arm64",
		"Подробный режим go build: включен (-x -v)",
		"Управление: ctrl+c - прервать фазу, esc - отменить весь запуск",
		"Build-сессия готова",
	} {
		if !strings.Contains(buildLog, want) {
			t.Fatalf("build tab does not contain %q:\n%s", want, buildLog)
		}
	}

	node, ok := fs.nodes[targetRemotePath]
	if !ok {
		t.Fatal("uploaded binary missing on remote fs")
	}
	if node.mode.Perm() != 0o755 {
		t.Fatalf("uploaded mode = %o, want 755", node.mode.Perm())
	}
}

func TestConnectedStartExecutionAllowsMixedRunnables(t *testing.T) {
	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: &fakeSSHConnection{}},
	)

	cmd := m.startExecution([]scripts.Script{
		{Name: "cleanup.sh", Kind: scripts.ScriptKindShell},
		{Name: "site-admin", Kind: scripts.ScriptKindGoApp},
		{Name: "remote-tool", Kind: scripts.ScriptKindBinary},
	})
	if cmd == nil {
		t.Fatal("expected mixed runnable selection to start unified runner queue")
	}
}

func TestConnectedGoLaunchFailuresStayRecoverable(t *testing.T) {
	tests := []struct {
		name          string
		builderErr    error
		detectErr     error
		buildRunErr   error
		uploadErr     error
		chmodErr      error
		shellErr      error
		wantMessage   string
		wantBuildTab  bool
		wantBuildFail bool
	}{
		{name: "detect", detectErr: errors.New("uname failed"), wantMessage: "uname failed"},
		{name: "build prepare", builderErr: errors.New("build failed"), wantMessage: "build failed"},
		{name: "build run", buildRunErr: errors.New("exit status 1"), wantMessage: "exit status 1", wantBuildTab: true, wantBuildFail: true},
		{name: "upload", uploadErr: errors.New("upload failed"), wantMessage: "upload failed", wantBuildTab: true, wantBuildFail: true},
		{name: "chmod", chmodErr: errors.New("chmod failed"), wantMessage: "chmod failed", wantBuildTab: true, wantBuildFail: true},
		{name: "native", shellErr: errors.New("shell failed"), wantMessage: "shell failed", wantBuildTab: true, wantBuildFail: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeRemoteFS("/home/test")
			remoteFS := remoteFSFailureWrapper{
				RemoteFS:  fs,
				uploadErr: tt.uploadErr,
				chmodErr:  tt.chmodErr,
			}

			buildShell := newFakeTerminalSession()
			close(buildShell.outputCh)
			buildShell.runCommandFn = func(spec localCommandSpec) error {
				if tt.buildRunErr != nil {
					buildShell.finishCommand(localCommandResult{
						Display:  spec.Display,
						Err:      tt.buildRunErr,
						ExitCode: 1,
					})
					return nil
				}
				buildShell.finishCommand(localCommandResult{Display: spec.Display})
				return nil
			}

			connection := &fakeSSHConnection{
				detectRemotePlatformFn: func() (sshclient.RemotePlatform, error) {
					if tt.detectErr != nil {
						return sshclient.RemotePlatform{}, tt.detectErr
					}
					return sshclient.RemotePlatform{GOOS: "linux", GOARCH: "amd64"}, nil
				},
				openRemoteFSFn: func() (sshclient.RemoteFS, error) {
					return remoteFS, nil
				},
				nativeTerminalFn: func(command string, height, width int) tea.ExecCommand {
					return &fakeExecCommand{err: tt.shellErr}
				},
			}

			m := newConnectedModelWithRuntime(
				config.ServerConfig{Name: "prod"},
				fakeSSHRuntime{connection: connection},
			)
			m.goBuild = func(script scripts.Script, platform sshclient.RemotePlatform) (scripts.GoBuildPlan, func(), error) {
				if tt.builderErr != nil {
					return scripts.GoBuildPlan{}, nil, tt.builderErr
				}
				artifactDir := t.TempDir()
				artifactPath := filepath.Join(artifactDir, script.Name)
				if err := os.WriteFile(artifactPath, []byte("binary"), 0o644); err != nil {
					t.Fatalf("write artifact: %v", err)
				}
				return scripts.GoBuildPlan{
					WorkDir:      script.BuildPath,
					ArtifactPath: artifactPath,
					Env:          []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"},
				}, func() {}, nil
			}
			m.localTerminal = func(workDir string, env []string) (trackedTerminalSession, error) {
				return buildShell, nil
			}

			cmd := m.startExecution([]scripts.Script{{
				Name:      "site-admin-tui",
				Kind:      scripts.ScriptKindGoApp,
				Package:   "go",
				Path:      filepath.Join("scripts", "go", "site-admin-tui"),
				BuildPath: filepath.Join("scripts", "go", "site-admin-tui"),
			}})
			if cmd == nil {
				t.Fatal("expected go launch command")
			}

			m = runConnectedCmd(t, m, cmd)

			if m.executing {
				t.Fatal("model should leave executing state after failure")
			}
			buildTab := m.terminalTab(terminalTabBuild)
			if tt.wantBuildTab {
				if !m.IsTerminalActive() {
					t.Fatal("terminal workspace should remain available after failure")
				}
				if buildTab == nil {
					t.Fatal("expected build tab after failure")
				}
				if tt.wantBuildFail && buildTab.buildState != buildTabFailed {
					t.Fatalf("build tab state = %v, want failed", buildTab.buildState)
				}
			} else if buildTab != nil {
				t.Fatalf("unexpected build tab for early failure: %#v", buildTab)
			}
			rendered := strings.Join(m.consoleLines, "\n")
			if !strings.Contains(rendered, tt.wantMessage) {
				t.Fatalf("console does not contain %q:\n%s", tt.wantMessage, rendered)
			}
			if tt.detectErr != nil && buildTab != nil {
				buildLog := terminalTabText(m, terminalTabBuild)
				if !strings.Contains(buildLog, tt.wantMessage) {
					t.Fatalf("build tab does not contain %q:\n%s", tt.wantMessage, buildLog)
				}
			}
		})
	}
}

func TestConnectedGoLaunchNativeTerminalKeepsBuildLog(t *testing.T) {
	buildShell := newFakeTerminalSession()
	close(buildShell.outputCh)
	buildShell.runCommandFn = func(spec localCommandSpec) error {
		buildShell.finishCommand(localCommandResult{Display: spec.Display})
		return nil
	}

	connection := &fakeSSHConnection{
		detectRemotePlatformFn: func() (sshclient.RemotePlatform, error) {
			return sshclient.RemotePlatform{GOOS: "linux", GOARCH: "amd64"}, nil
		},
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			return newFakeRemoteFS("/home/test"), nil
		},
		nativeTerminalFn: func(command string, height, width int) tea.ExecCommand {
			if strings.Contains(command, "__SSHPILOT_") {
				t.Fatalf("native command should not contain tracking markers: %q", command)
			}
			return &fakeExecCommand{}
		},
	}

	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: connection},
	)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.goBuild = func(script scripts.Script, platform sshclient.RemotePlatform) (scripts.GoBuildPlan, func(), error) {
		artifactPath := filepath.Join(t.TempDir(), script.Name)
		if err := os.WriteFile(artifactPath, []byte("binary"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		return scripts.GoBuildPlan{
			WorkDir:      script.BuildPath,
			ArtifactPath: artifactPath,
			Env:          []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"},
		}, func() {}, nil
	}
	m.localTerminal = func(workDir string, env []string) (trackedTerminalSession, error) {
		return buildShell, nil
	}

	cmd := m.startExecution([]scripts.Script{{
		Name:      "site-admin-tui",
		Kind:      scripts.ScriptKindGoApp,
		Package:   "go",
		Path:      filepath.Join("scripts", "go", "site-admin-tui"),
		BuildPath: filepath.Join("scripts", "go", "site-admin-tui"),
	}})
	m = runConnectedCmd(t, m, cmd)

	buildBefore := terminalTabText(m, terminalTabBuild)
	if m.activeTerminalTab != terminalTabBuild {
		t.Fatalf("active terminal tab = %v, want build", m.activeTerminalTab)
	}

	m, cmd = m.Update(keyRunes("t"))
	m = runConnectedCmd(t, m, cmd)

	if m.activeTerminalTab != terminalTabBuild {
		t.Fatalf("active terminal tab = %v, want build", m.activeTerminalTab)
	}
	if got := terminalTabText(m, terminalTabBuild); got != buildBefore {
		t.Fatalf("build tab log changed after opening native terminal\nbefore:\n%s\n\nafter:\n%s", buildBefore, got)
	}
}

func TestConnectedGoLaunchKeepsBuildTabVisibleAfterBuild(t *testing.T) {
	script := scripts.Script{
		Name:      "site-admin-tui",
		Kind:      scripts.ScriptKindGoApp,
		Package:   "go",
		Path:      filepath.Join("scripts", "go", "site-admin-tui"),
		BuildPath: filepath.Join("scripts", "go", "site-admin-tui"),
	}

	buildShell := newFakeTerminalSession()
	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: &fakeSSHConnection{}},
	)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.execScripts = []scriptState{{script: script, status: statusBuilding}}
	m.executing = true
	m.goLaunch = &runnerState{
		script:       script,
		phase:        executionPhaseBuildingLocal,
		index:        0,
		artifactPath: filepath.Join(t.TempDir(), script.Name),
		plan: scripts.GoBuildPlan{
			WorkDir:      script.BuildPath,
			ArtifactPath: filepath.Join(t.TempDir(), script.Name),
		},
	}
	m.prepareBuildTerminalTab(script.Name)
	m.attachTerminalTab(terminalTabBuild, buildShell, nil)
	m.activeTerminalTab = terminalTabBuild

	m, cmd := m.handleLocalCommandDone(localCommandDoneMsg{
		tab:    terminalTabBuild,
		shell:  buildShell,
		result: localCommandResult{},
		ok:     true,
	})

	if cmd == nil {
		t.Fatal("expected upload/open-server command batch")
	}
	if m.activeTerminalTab != terminalTabBuild {
		t.Fatalf("active terminal tab = %v, want build after build", m.activeTerminalTab)
	}
	if !m.terminalWorkspaceActive {
		t.Fatal("build terminal workspace should remain active after build")
	}
	if m.focus != panelConsole {
		t.Fatalf("focus = %v, want console/terminal panel", m.focus)
	}
	if view := m.View(); !strings.Contains(view, "Build Terminal") {
		t.Fatalf("view should keep rendering build terminal after local build:\n%s", view)
	}
	if m.terminalTab(terminalTabBuild).buildState != buildTabReady {
		t.Fatalf("build tab state = %v, want ready", m.terminalTab(terminalTabBuild).buildState)
	}
}

func TestConnectedGoLaunchDetectPlatformRetriesAfterReset(t *testing.T) {
	detectCalls := 0
	connection := &fakeSSHConnection{
		detectRemotePlatformFn: func() (sshclient.RemotePlatform, error) {
			detectCalls++
			if detectCalls == 1 {
				return sshclient.RemotePlatform{}, errors.New("session EOF")
			}
			return sshclient.RemotePlatform{GOOS: "linux", GOARCH: "amd64"}, nil
		},
		resetFn: func() error { return nil },
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			return newFakeRemoteFS("/home/test"), nil
		},
		nativeTerminalFn: func(command string, height, width int) tea.ExecCommand {
			return &fakeExecCommand{}
		},
	}

	buildShell := newFakeTerminalSession()
	close(buildShell.outputCh)
	buildShell.runCommandFn = func(spec localCommandSpec) error {
		buildShell.finishCommand(localCommandResult{Display: spec.Display})
		return nil
	}

	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: connection},
	)
	m.goBuild = func(script scripts.Script, platform sshclient.RemotePlatform) (scripts.GoBuildPlan, func(), error) {
		artifactPath := filepath.Join(t.TempDir(), script.Name)
		if err := os.WriteFile(artifactPath, []byte("binary"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		return scripts.GoBuildPlan{
			WorkDir:      script.BuildPath,
			ArtifactPath: artifactPath,
			Env:          []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"},
		}, func() {}, nil
	}
	m.localTerminal = func(workDir string, env []string) (trackedTerminalSession, error) {
		return buildShell, nil
	}

	cmd := m.startExecution([]scripts.Script{{
		Name:      "site-admin-tui",
		Kind:      scripts.ScriptKindGoApp,
		Package:   "go",
		Path:      filepath.Join("scripts", "go", "site-admin-tui"),
		BuildPath: filepath.Join("scripts", "go", "site-admin-tui"),
	}})
	m = runConnectedCmd(t, m, cmd)

	if detectCalls != 2 {
		t.Fatalf("detect calls = %d, want 2", detectCalls)
	}
	if connection.resetCalls != 1 {
		t.Fatalf("reset calls = %d, want 1", connection.resetCalls)
	}
	buildTab := m.terminalTab(terminalTabBuild)
	if buildTab == nil {
		t.Fatal("expected build tab after successful retry")
	}
	if buildTab.buildState != buildTabReady {
		t.Fatalf("build tab state = %v, want ready", buildTab.buildState)
	}
	if m.executing {
		t.Fatal("expected native run to complete without timeout")
	}
}

func TestConnectedGoLaunchInterruptReportsCancellation(t *testing.T) {
	script := scripts.Script{
		Name:      "site-admin-tui",
		Kind:      scripts.ScriptKindGoApp,
		Package:   "go",
		Path:      filepath.Join("scripts", "go", "site-admin-tui"),
		BuildPath: filepath.Join("scripts", "go", "site-admin-tui"),
	}

	buildShell := newFakeTerminalSession()
	close(buildShell.outputCh)
	buildShell.runCommandFn = func(spec localCommandSpec) error { return nil }
	buildShell.interruptFn = func() error {
		buildShell.finishCommand(localCommandResult{
			Display:     buildShell.runSpecs[len(buildShell.runSpecs)-1].Display,
			Err:         errors.New("команда прервана"),
			ExitCode:    130,
			Interrupted: true,
		})
		return nil
	}

	connection := &fakeSSHConnection{
		detectRemotePlatformFn: func() (sshclient.RemotePlatform, error) {
			return sshclient.RemotePlatform{GOOS: "linux", GOARCH: "amd64"}, nil
		},
	}

	m := newConnectedModelWithRuntime(
		config.ServerConfig{Name: "prod"},
		fakeSSHRuntime{connection: connection},
	)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.goBuild = func(script scripts.Script, platform sshclient.RemotePlatform) (scripts.GoBuildPlan, func(), error) {
		artifactPath := filepath.Join(t.TempDir(), script.Name)
		if err := os.WriteFile(artifactPath, []byte("binary"), 0o644); err != nil {
			t.Fatalf("write artifact: %v", err)
		}
		return scripts.GoBuildPlan{
			WorkDir:      script.BuildPath,
			ArtifactPath: artifactPath,
			Env:          []string{"GOOS=linux", "GOARCH=amd64", "CGO_ENABLED=0"},
		}, func() {}, nil
	}
	m.localTerminal = func(workDir string, env []string) (trackedTerminalSession, error) {
		return buildShell, nil
	}

	cmd := m.startExecution([]scripts.Script{script})
	if cmd == nil {
		t.Fatal("expected go launch command")
	}

	prepared, ok := prepareGoBuildCmd(m.connection, m.goBuild, script)().(goBuildPreparedMsg)
	if !ok {
		t.Fatal("expected goBuildPreparedMsg")
	}
	m, _ = m.Update(prepared)

	started, ok := startBuildTerminalCmd(m.localTerminal, prepared.plan.WorkDir, prepared.plan.Env)().(terminalStartedMsg)
	if !ok {
		t.Fatal("expected terminalStartedMsg")
	}
	m, _ = m.Update(started)
	if err := buildShell.RunCommand(m.goLaunch.buildCommandSpec()); err != nil {
		t.Fatalf("run command: %v", err)
	}

	if !m.canInterruptTerminalTab(terminalTabBuild) {
		t.Fatal("expected build tab to become interruptible after start")
	}
	if got := m.terminalInputView(m.terminalTab(terminalTabBuild), 120); !strings.Contains(got, "ctrl+c") || !strings.Contains(got, "esc") {
		t.Fatalf("running hint does not advertise interruption:\n%s", got)
	}

	m, cmd = m.Update(keyCtrlC())
	m = runConnectedCmd(t, m, cmd)
	result := <-buildShell.commandDoneCh
	m, _ = m.Update(localCommandDoneMsg{
		tab:    terminalTabBuild,
		shell:  buildShell,
		result: result,
		ok:     true,
	})

	if m.executing {
		t.Fatal("expected go launch to stop after interruption")
	}
	rendered := strings.Join(m.consoleLines, "\n")
	if !strings.Contains(rendered, "Локальная сборка прервана пользователем") {
		t.Fatalf("console does not contain interruption message:\n%s", rendered)
	}
}

func TestContinueGoUploadCmdTimesOutAndResetsConnection(t *testing.T) {
	oldTimeout := goLaunchOpenFSTimeout
	goLaunchOpenFSTimeout = 10 * time.Millisecond
	defer func() {
		goLaunchOpenFSTimeout = oldTimeout
	}()

	release := make(chan struct{})
	connection := &fakeSSHConnection{
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			<-release
			return nil, errors.New("sftp reset")
		},
		resetFn: func() error {
			close(release)
			return nil
		},
	}

	artifactPath := filepath.Join(t.TempDir(), "site-admin-tui")
	if err := os.WriteFile(artifactPath, []byte("binary"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	msg, ok := continueGoUploadCmd(connection, scripts.Script{Name: "site-admin-tui"}, artifactPath)().(goUploadResultMsg)
	if !ok {
		t.Fatal("expected goUploadResultMsg")
	}
	if msg.err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(msg.err.Error(), "таймаут") {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	if connection.resetCalls != 1 {
		t.Fatalf("reset calls = %d, want 1", connection.resetCalls)
	}
}

func TestContinueGoUploadCmdFallsBackToSFTPDirPrepWhenSSHMkdirFails(t *testing.T) {
	var operations []string

	fs := newFakeRemoteFS("/home/test")
	fs.recordFn = func(operation string) {
		operations = append(operations, operation)
	}

	connection := &fakeSSHConnection{
		executeFn: func(content string) (string, error) {
			content = strings.TrimSpace(content)
			if strings.Contains(content, "find_pids()") {
				operations = append(operations, "stop")
				return "", nil
			}
			operations = append(operations, "exec:"+content)
			return "", errors.New("bash unavailable")
		},
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			operations = append(operations, "openfs")
			return fs, nil
		},
	}

	artifactPath := filepath.Join(t.TempDir(), "site-admin-tui")
	if err := os.WriteFile(artifactPath, []byte("binary"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	msg, ok := continueGoUploadCmd(connection, scripts.Script{
		Name:    "site-admin-tui",
		Kind:    scripts.ScriptKindGoApp,
		Package: "go",
	}, artifactPath)().(goUploadResultMsg)
	if !ok {
		t.Fatal("expected goUploadResultMsg")
	}
	if msg.err != nil {
		t.Fatalf("unexpected error: %v", msg.err)
	}

	targetRemotePath := "/home/test/.sshpilot/runnables/go/site-admin-tui/site-admin-tui"
	wantOps := []string{
		"openfs",
		"exec:set -e\nmkdir -p -- '/home/test/.sshpilot/runnables/go/site-admin-tui'",
		"mkdir:/home/test/.sshpilot",
		"mkdir:/home/test/.sshpilot/runnables",
		"mkdir:/home/test/.sshpilot/runnables/go",
		"mkdir:/home/test/.sshpilot/runnables/go/site-admin-tui",
		"stop",
	}
	if len(operations) < len(wantOps)+4 {
		t.Fatalf("operations = %#v, want fallback prefix plus temp upload", operations)
	}
	for i, want := range wantOps {
		if operations[i] != want {
			t.Fatalf("operation[%d] = %q, want %q\nall=%#v", i, operations[i], want, operations)
		}
	}
	uploadOp := operations[len(wantOps)]
	if !strings.HasPrefix(uploadOp, "upload:"+targetRemotePath+".sshpilot-upload-") {
		t.Fatalf("upload operation = %q, want temp upload for %s\nall=%#v", uploadOp, targetRemotePath, operations)
	}
	tempRemotePath := strings.TrimPrefix(uploadOp, "upload:")
	for offset, want := range []string{
		"write:" + tempRemotePath,
		"chmod:" + tempRemotePath,
		"rename:" + tempRemotePath + "->" + targetRemotePath,
	} {
		i := len(wantOps) + 1 + offset
		if operations[i] != want {
			t.Fatalf("operation[%d] = %q, want %q\nall=%#v", i, operations[i], want, operations)
		}
	}
}

func TestContinueGoUploadCmdFailsBeforeUploadWhenOldRunnableCannotStop(t *testing.T) {
	fs := newFakeRemoteFS("/home/test")
	connection := &fakeSSHConnection{
		executeFn: func(content string) (string, error) {
			if strings.Contains(content, "find_pids()") {
				return "", errors.New("permission denied")
			}
			return "", nil
		},
		openRemoteFSFn: func() (sshclient.RemoteFS, error) {
			return fs, nil
		},
	}

	artifactPath := filepath.Join(t.TempDir(), "site-admin-tui")
	if err := os.WriteFile(artifactPath, []byte("binary"), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	msg, ok := continueGoUploadCmd(connection, scripts.Script{
		Name:    "site-admin-tui",
		Kind:    scripts.ScriptKindGoApp,
		Package: "go",
	}, artifactPath)().(goUploadResultMsg)
	if !ok {
		t.Fatal("expected goUploadResultMsg")
	}
	if msg.err == nil {
		t.Fatal("expected stop error")
	}
	if !strings.Contains(msg.err.Error(), "permission denied") {
		t.Fatalf("unexpected error: %v", msg.err)
	}
	for _, operation := range fs.operations {
		if strings.HasPrefix(operation, "upload:") {
			t.Fatalf("upload should not run after stop failure: %#v", fs.operations)
		}
	}
}

func TestContinueGoUploadCmdCleansTempArtifactOnDeployFailure(t *testing.T) {
	tests := []struct {
		name      string
		uploadErr error
		chmodErr  error
		renameErr error
	}{
		{name: "upload", uploadErr: errors.New("upload failed")},
		{name: "chmod", chmodErr: errors.New("chmod failed")},
		{name: "rename", renameErr: errors.New("rename failed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := newFakeRemoteFS("/home/test")
			connection := &fakeSSHConnection{
				executeFn: func(string) (string, error) {
					return "", nil
				},
				openRemoteFSFn: func() (sshclient.RemoteFS, error) {
					return remoteFSFailureWrapper{
						RemoteFS:  fs,
						uploadErr: tt.uploadErr,
						chmodErr:  tt.chmodErr,
						renameErr: tt.renameErr,
					}, nil
				},
				nativeTerminalFn: func(command string, height, width int) tea.ExecCommand {
					t.Fatal("native terminal should not open after deploy failure")
					return nil
				},
			}

			artifactPath := filepath.Join(t.TempDir(), "site-admin-tui")
			if err := os.WriteFile(artifactPath, []byte("binary"), 0o644); err != nil {
				t.Fatalf("write artifact: %v", err)
			}

			msg, ok := continueGoUploadCmd(connection, scripts.Script{
				Name:    "site-admin-tui",
				Kind:    scripts.ScriptKindGoApp,
				Package: "go",
			}, artifactPath)().(goUploadResultMsg)
			if !ok {
				t.Fatal("expected goUploadResultMsg")
			}
			if msg.err == nil {
				t.Fatal("expected deploy error")
			}

			targetRemotePath := "/home/test/.sshpilot/runnables/go/site-admin-tui/site-admin-tui"
			var tempRemotePath string
			for _, operation := range fs.operations {
				if strings.HasPrefix(operation, "upload:"+targetRemotePath+".sshpilot-upload-") {
					tempRemotePath = strings.TrimPrefix(operation, "upload:")
					break
				}
			}
			if tempRemotePath == "" && tt.uploadErr == nil {
				t.Fatalf("temp upload missing from operations: %#v", fs.operations)
			}
			if tempRemotePath == "" {
				for _, operation := range fs.operations {
					if strings.HasPrefix(operation, "remove:"+targetRemotePath+".sshpilot-upload-") {
						tempRemotePath = strings.TrimPrefix(operation, "remove:")
						break
					}
				}
			}
			if tempRemotePath == "" {
				t.Fatalf("temp path missing from operations: %#v", fs.operations)
			}
			if _, exists := fs.nodes[tempRemotePath]; exists {
				t.Fatalf("temp artifact still exists at %s; operations=%#v", tempRemotePath, fs.operations)
			}
			for _, operation := range fs.operations {
				if operation == "rename:"+tempRemotePath+"->"+targetRemotePath && tt.renameErr != nil {
					t.Fatalf("failing rename should not be recorded as successful: %#v", fs.operations)
				}
			}
		})
	}
}

func buildArtifactPathFromSpec(t *testing.T, spec localCommandSpec) string {
	t.Helper()

	for i := 0; i < len(spec.Args)-1; i++ {
		if spec.Args[i] == "-o" {
			return spec.Args[i+1]
		}
	}
	t.Fatalf("build spec does not contain -o flag: %#v", spec)
	return ""
}

type remoteFSFailureWrapper struct {
	sshclient.RemoteFS
	uploadErr error
	chmodErr  error
	renameErr error
}

func (fs remoteFSFailureWrapper) Upload(req sshclient.TransferRequest) error {
	if fs.uploadErr != nil {
		return fs.uploadErr
	}
	return fs.RemoteFS.Upload(req)
}

func (fs remoteFSFailureWrapper) Chmod(name string, mode os.FileMode) error {
	if fs.chmodErr != nil {
		return fs.chmodErr
	}
	return fs.RemoteFS.Chmod(name, mode)
}

func (fs remoteFSFailureWrapper) Rename(oldPath, newPath string) error {
	if fs.renameErr != nil {
		return fs.renameErr
	}
	return fs.RemoteFS.Rename(oldPath, newPath)
}
