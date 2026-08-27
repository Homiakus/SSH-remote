package main

import (
	"os"

	"sshpilot/scripts/go/site-admin-tui/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
