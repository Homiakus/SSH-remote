package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"sshpilot/scripts/go/site-admin-tui/internal/deploy"
	"sshpilot/scripts/go/site-admin-tui/internal/domain"
	"sshpilot/scripts/go/site-admin-tui/internal/state"
	"sshpilot/scripts/go/site-admin-tui/internal/system"
	"sshpilot/scripts/go/site-admin-tui/internal/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func Run(args []string, stdout, stderr io.Writer) int {
	store := state.NewStore(state.DefaultPaths())
	cfg, err := store.LoadConfig()
	if err != nil {
		fmt.Fprintf(stderr, "load config: %v\n", err)
		return 1
	}
	service := deploy.NewService(store, system.ExecRunner{}, cfg)

	if len(args) == 0 {
		program := tea.NewProgram(tui.NewApp(service), tea.WithAltScreen())
		if _, err := program.Run(); err != nil {
			fmt.Fprintf(stderr, "run tui: %v\n", err)
			return 1
		}
		return 0
	}

	switch args[0] {
	case "doctor":
		report, err := service.Doctor()
		if err != nil {
			fmt.Fprintf(stderr, "doctor: %v\n", err)
			return 1
		}
		for _, check := range report.Checks {
			status := "OK"
			if !check.OK {
				status = "FAIL"
			}
			level := "optional"
			if check.Required {
				level = "required"
			}
			fmt.Fprintf(stdout, "%-8s %-9s %-18s %s\n", status, level, check.Name, check.Detail)
		}
		if !report.Healthy() {
			return 1
		}
		return 0
	case "import":
		return runImport(context.Background(), service, args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n%s", args[0], usage())
		return 1
	}
}

func runImport(ctx context.Context, service *deploy.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		site        = fs.String("site", "", "site name")
		path        = fs.String("path", "", "existing directory path")
		runtimeType = fs.String("runtime", "", "runtime: static|php|node|python|docker_compose")
		domainName  = fs.String("domain", "", "public domain")
		rootDir     = fs.String("root-dir", ".", "relative root dir inside release")
		port        = fs.Int("port", 0, "application port for proxy runtimes")
		command     = fs.String("start-command", "", "start command for node/python")
		composeFile = fs.String("compose-file", "", "compose file for docker compose")
		tlsEnabled  = fs.Bool("tls", false, "enable TLS")
		tlsEmail    = fs.String("tls-email", "", "email for certbot")
		sharedDirs  = fs.String("shared-dirs", "", "comma separated shared dirs")
		envFile     = fs.String("env-file", "", "relative env file path")
	)
	if err := fs.Parse(args); err != nil {
		return 1
	}

	spec := domain.SiteSpec{
		Name:    strings.TrimSpace(*site),
		Domain:  strings.TrimSpace(*domainName),
		Runtime: domain.RuntimeType(strings.TrimSpace(*runtimeType)),
		Source: domain.DeploySource{
			Kind:        domain.SourceExistingDir,
			ExistingDir: strings.TrimSpace(*path),
		},
		RootDir: *rootDir,
		EnvFile: strings.TrimSpace(*envFile),
		TLS: domain.TLSSpec{
			Enabled: *tlsEnabled,
			Email:   strings.TrimSpace(*tlsEmail),
		},
		Service: domain.ServiceSpec{
			Port:        *port,
			ComposeFile: strings.TrimSpace(*composeFile),
		},
	}
	if *command != "" {
		spec.Service.Command = []string{*command}
	}
	if *sharedDirs != "" {
		spec.SharedDirs = splitCSV(*sharedDirs)
	}

	result, err := service.Import(ctx, spec)
	if err != nil {
		fmt.Fprintf(stderr, "import failed: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "Imported %s -> %s (%s)\n", result.Spec.Name, result.CurrentPath, result.Release.ID)
	return 0
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			result = append(result, part)
		}
	}
	return result
}

func usage() string {
	return "Usage:\n  site-admin-tui\n  site-admin-tui doctor\n  site-admin-tui import --site <name> --path <dir> --runtime <type> --domain <host>\n"
}
