package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"time"

	"sshpilot/internal/config"
	"sshpilot/internal/scripts"
	"sshpilot/internal/ui"
	"sshpilot/internal/web"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	port := flag.String("port", "8080", "Web server port")
	host := flag.String("host", "127.0.0.1", "Web server host")
	noBrowser := flag.Bool("no-browser", false, "Do not automatically open browser")
	runTUI := flag.Bool("tui", false, "Run legacy TUI interface")
	flag.Parse()

	// Ensure directories exist
	if err := config.EnsureServersDir(); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating servers folder: %v\n", err)
		os.Exit(1)
	}
	if err := scripts.EnsureScriptsDir(); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating scripts folder: %v\n", err)
		os.Exit(1)
	}

	if *runTUI {
		app := ui.NewAppModel()
		p := tea.NewProgram(app, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	addr := fmt.Sprintf("%s:%s", *host, *port)
	url := fmt.Sprintf("http://%s", addr)

	fmt.Println("==================================================================")
	fmt.Println("  SSHPILOT // CONTROL PLANE (NEO-SWISS EDITORIAL SYSTEM)")
	fmt.Println("==================================================================")
	fmt.Printf("  • Local Web UI : %s\n", url)
	fmt.Println("  • Press Ctrl+C to terminate server")
	fmt.Println("==================================================================")

	if !*noBrowser {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(url)
		}()
	}

	if err := web.StartServer(addr); err != nil {
		fmt.Fprintf(os.Stderr, "Server error: %v\n", err)
		os.Exit(1)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}