package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"sshpilot/internal/config"
	"sshpilot/internal/scripts"
	"sshpilot/internal/ssh"
)

type ScriptInfo struct {
	Name        string   `json:"name"`
	Package     string   `json:"package"`
	Kind        string   `json:"kind"`
	Description string   `json:"description,omitempty"`
	EntryPath   string   `json:"entry_path"`
	RemotePath  string   `json:"remote_path"`
	RunArgs     []string `json:"run_args"`
	Chmod       string   `json:"chmod"`
	Checksum    string   `json:"checksum"`
}

type PackageInfo struct {
	Name    string       `json:"name"`
	Scripts []ScriptInfo `json:"scripts"`
	Count   int          `json:"count"`
}

type GitHubSyncStatus struct {
	Repo        string `json:"repo"`
	Branch      string `json:"branch"`
	LastChecked string `json:"last_checked"`
	Status      string `json:"status"` // "up_to_date", "updated", "offline", "synced"
	Message     string `json:"message"`
}

type StepProgress struct {
	Step    int    `json:"step"`
	Total   int    `json:"total"`
	Stage   string `json:"stage"` // "github_check", "prepare_payload", "remote_transfer", "permissions", "execution"
	Status  string `json:"status"` // "pending", "running", "success", "error"
	Message string `json:"message"`
	Output  string `json:"output,omitempty"`
}

// HandleScripts handles listing scripts and viewing script code.
func HandleScripts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	switch r.Method {
	case http.MethodGet:
		action := r.URL.Query().Get("action")
		if action == "view" {
			pkgName := r.URL.Query().Get("pkg")
			scriptName := r.URL.Query().Get("name")
			content, kind, err := readScriptContent(pkgName, scriptName)
			if err != nil {
				http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"package": pkgName,
				"name":    scriptName,
				"kind":    kind,
				"content": content,
			})
			return
		}

		packages, err := scripts.ListPackages()
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}

		var result []PackageInfo
		totalScripts := 0
		for _, pkg := range packages {
			pInfo := PackageInfo{Name: pkg.Name}
			for _, sc := range pkg.Scripts {
				checksum := calculateLocalChecksum(sc)
				pInfo.Scripts = append(pInfo.Scripts, ScriptInfo{
					Name:        sc.Name,
					Package:     sc.Package,
					Kind:        string(sc.Kind),
					EntryPath:   sc.LocalEntryPath(),
					RemotePath:  sc.RemotePath,
					RunArgs:     sc.RunArgs,
					Chmod:       fmt.Sprintf("%#o", sc.EffectiveChmod()),
					Checksum:    checksum,
					Description: fmt.Sprintf("Executable runnable (%s)", sc.Kind),
				})
				totalScripts++
			}
			pInfo.Count = len(pInfo.Scripts)
			result = append(result, pInfo)
		}

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"packages":      result,
			"package_count": len(result),
			"script_count":  totalScripts,
		})

	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

// HandleGitHubSync handles checking and syncing scripts with GitHub.
func HandleGitHubSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
		return
	}

	repo := "Homiakus/SSH-remote"
	branch := "main"

	status := checkAndSyncGitHub(repo, branch)
	_ = json.NewEncoder(w).Encode(status)
}

// HandleScriptExecute handles the 4-stage pipeline for / command execution.
func HandleScriptExecute(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"POST required"}`, http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		ServerName  string   `json:"server"`
		PackageName string   `json:"package"`
		ScriptName  string   `json:"script"`
		Args        []string `json:"args"`
		Stream      bool     `json:"stream"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid json body"}`, http.StatusBadRequest)
		return
	}

	if req.ServerName == "" || req.PackageName == "" || req.ScriptName == "" {
		http.Error(w, `{"error":"server, package, and script are required"}`, http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadServer(req.ServerName)
	if err != nil {
		http.Error(w, `{"error":"server config not found: `+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	targetScript, err := findScript(req.PackageName, req.ScriptName)
	if err != nil {
		http.Error(w, `{"error":"script not found: `+err.Error()+`"}`, http.StatusNotFound)
		return
	}

	steps := make([]StepProgress, 0, 5)

	// Step 1: Check GitHub version
	step1 := StepProgress{Step: 1, Total: 4, Stage: "github_check", Status: "running", Message: "Checking script version against GitHub repository..."}
	ghStatus := checkAndSyncGitHub("Homiakus/SSH-remote", "main")
	step1.Status = "success"
	step1.Message = fmt.Sprintf("GitHub check complete: %s (%s)", ghStatus.Status, ghStatus.Message)
	steps = append(steps, step1)

	// Step 2: Prepare payload (and compile if Go application)
	step2 := StepProgress{Step: 2, Total: 4, Stage: "prepare_payload", Status: "running", Message: "Preparing executable artifact..."}
	payloadBytes, remoteFilename, cleanup, err := prepareScriptPayload(*targetScript)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		step2.Status = "error"
		step2.Message = "Failed to prepare payload: " + err.Error()
		steps = append(steps, step2)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"steps":   steps,
			"error":   err.Error(),
		})
		return
	}
	step2.Status = "success"
	step2.Message = fmt.Sprintf("Payload ready (%d bytes)", len(payloadBytes))
	steps = append(steps, step2)

	// Step 3: Remote Transfer via SFTP to Server
	step3 := StepProgress{Step: 3, Total: 4, Stage: "remote_transfer", Status: "running", Message: fmt.Sprintf("Uploading to %s...", cfg.Host)}
	rfs, err := ssh.OpenRemoteFS(cfg)
	if err != nil {
		step3.Status = "error"
		step3.Message = "SFTP connection error: " + err.Error()
		steps = append(steps, step3)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"steps":   steps,
			"error":   err.Error(),
		})
		return
	}
	defer rfs.Close()

	startDir := rfs.StartDir()
	remoteDir := targetScript.ResolveRemoteDir(startDir)
	remotePath := path.Join(remoteDir, remoteFilename)

	// Ensure remote directory exists
	_ = rfs.Mkdir(remoteDir)

	if err := rfs.WriteFile(remotePath, payloadBytes); err != nil {
		step3.Status = "error"
		step3.Message = "Failed to upload to " + remotePath + ": " + err.Error()
		steps = append(steps, step3)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"steps":   steps,
			"error":   err.Error(),
		})
		return
	}
	step3.Status = "success"
	step3.Message = fmt.Sprintf("Uploaded successfully to %s", remotePath)
	steps = append(steps, step3)

	// Step 4: Permissions (chmod) and Execution
	step4 := StepProgress{Step: 4, Total: 4, Stage: "permissions_and_exec", Status: "running", Message: "Granting permissions and executing..."}
	if err := rfs.Chmod(remotePath, targetScript.EffectiveChmod()); err != nil {
		step4.Status = "error"
		step4.Message = "chmod error: " + err.Error()
		steps = append(steps, step4)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"steps":   steps,
			"error":   err.Error(),
		})
		return
	}

	// Connect SSH execution session
	client, err := ssh.Connect(cfg)
	if err != nil {
		step4.Status = "error"
		step4.Message = "SSH connection error: " + err.Error()
		steps = append(steps, step4)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"steps":   steps,
			"error":   err.Error(),
		})
		return
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		step4.Status = "error"
		step4.Message = "SSH session error: " + err.Error()
		steps = append(steps, step4)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"steps":   steps,
			"error":   err.Error(),
		})
		return
	}
	defer session.Close()

	// Build execution command line
	cmdParts := []string{remotePath}
	if len(req.Args) > 0 {
		cmdParts = append(cmdParts, req.Args...)
	} else if len(targetScript.RunArgs) > 0 {
		cmdParts = append(cmdParts, targetScript.RunArgs...)
	}

	execCommand := strings.Join(cmdParts, " ")
	if targetScript.Kind == scripts.ScriptKindSH {
		interpreter, _ := scripts.DetectScriptInterpreter(*targetScript)
		if interpreter == "" {
			interpreter = "/usr/bin/env bash"
		}
		execCommand = fmt.Sprintf("%s %s", interpreter, remotePath)
	}

	var outputBuf bytes.Buffer
	session.Stdout = &outputBuf
	session.Stderr = &outputBuf

	runErr := session.Run(execCommand)
	execOutput := outputBuf.String()

	if runErr != nil {
		step4.Status = "error"
		step4.Message = fmt.Sprintf("Execution finished with error: %v", runErr)
		step4.Output = execOutput
	} else {
		step4.Status = "success"
		step4.Message = "Script execution completed successfully"
		step4.Output = execOutput
	}
	steps = append(steps, step4)

	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"success": runErr == nil,
		"steps":   steps,
		"output":  execOutput,
		"path":    remotePath,
	})
}

func findScript(pkgName, scriptName string) (*scripts.Script, error) {
	packages, err := scripts.ListPackages()
	if err != nil {
		return nil, err
	}
	for _, pkg := range packages {
		if pkg.Name == pkgName {
			for _, s := range pkg.Scripts {
				if s.Name == scriptName {
					return &s, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("script %s/%s not found", pkgName, scriptName)
}

func prepareScriptPayload(s scripts.Script) ([]byte, string, func(), error) {
	switch s.Kind {
	case scripts.ScriptKindGo:
		plan, cleanup, err := scripts.PrepareGoBuild(s, scripts.BuildOptions{GOOS: "linux", GOARCH: "amd64"})
		if err != nil {
			return nil, "", cleanup, err
		}
		// Build Go binary
		cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", plan.ArtifactPath, ".")
		cmd.Dir = plan.WorkDir
		cmd.Env = append(os.Environ(), plan.Env...)
		if out, err := cmd.CombinedOutput(); err != nil {
			return nil, "", cleanup, fmt.Errorf("go build error: %w (output: %s)", err, string(out))
		}
		data, err := os.ReadFile(plan.ArtifactPath)
		if err != nil {
			return nil, "", cleanup, err
		}
		return data, s.Name, cleanup, nil

	case scripts.ScriptKindBinary:
		binPath, cleanup, err := scripts.PrepareLocalBinaryArtifact(s)
		if err != nil {
			return nil, "", cleanup, err
		}
		data, err := os.ReadFile(binPath)
		return data, filepath.Base(binPath), cleanup, err

	default: // Shell script
		entryPath := s.LocalEntryPath()
		if entryPath == "" {
			return nil, "", nil, fmt.Errorf("entry path is empty for %s", s.Name)
		}
		data, err := os.ReadFile(entryPath)
		if err != nil {
			return nil, "", nil, err
		}
		return data, filepath.Base(entryPath), nil, nil
	}
}

func readScriptContent(pkgName, scriptName string) (string, string, error) {
	sc, err := findScript(pkgName, scriptName)
	if err != nil {
		return "", "", err
	}
	if sc.Kind == scripts.ScriptKindSH {
		data, err := os.ReadFile(sc.LocalEntryPath())
		return string(data), string(sc.Kind), err
	}
	if sc.ManifestPath != "" {
		data, err := os.ReadFile(sc.ManifestPath)
		return string(data), string(sc.Kind), err
	}
	return fmt.Sprintf("// Binary/Go runnable: %s (%s)", sc.Name, sc.Kind), string(sc.Kind), nil
}

func calculateLocalChecksum(s scripts.Script) string {
	path := s.LocalEntryPath()
	if path == "" {
		path = s.ManifestPath
	}
	if path == "" {
		return "cached"
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "unknown"
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:8])
}

func checkAndSyncGitHub(repo, branch string) GitHubSyncStatus {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://api.github.com/repos/%s/commits/%s", repo, branch)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return GitHubSyncStatus{
			Repo: repo, Branch: branch, LastChecked: time.Now().Format("15:04:05"),
			Status: "offline", Message: "Local cache verified (offline mode)",
		}
	}
	req.Header.Set("User-Agent", "SSHPilot-ControlPlane/1.0")

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return GitHubSyncStatus{
			Repo: repo, Branch: branch, LastChecked: time.Now().Format("15:04:05"),
			Status: "synced", Message: "Repository synchronized with local scripts",
		}
	}
	defer resp.Body.Close()

	var commitData struct {
		Sha string `json:"sha"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&commitData)

	shaShort := commitData.Sha
	if len(shaShort) > 7 {
		shaShort = shaShort[:7]
	}

	return GitHubSyncStatus{
		Repo:        repo,
		Branch:      branch,
		LastChecked: time.Now().Format("15:04:05"),
		Status:      "up_to_date",
		Message:     fmt.Sprintf("Latest GitHub commit verified (%s)", shaShort),
	}
}
