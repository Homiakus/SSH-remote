package system

import (
	"fmt"
	"os"
	"runtime"

	"sshpilot/scripts/go/site-admin-tui/internal/domain"
	"sshpilot/scripts/go/site-admin-tui/internal/state"
)

func RunDoctor(runner Runner, store *state.Store) (domain.DoctorReport, error) {
	if runner == nil {
		runner = ExecRunner{}
	}
	if store == nil {
		store = state.NewStore(state.DefaultPaths())
	}

	report := domain.DoctorReport{}
	report.Checks = append(report.Checks, domain.DoctorCheck{
		Name:     "os",
		Required: true,
		OK:       runtime.GOOS == "linux",
		Detail:   fmt.Sprintf("runtime=%s", runtime.GOOS),
	})
	report.Checks = append(report.Checks, domain.DoctorCheck{
		Name:     "root",
		Required: true,
		OK:       currentEUID() == 0,
		Detail:   fmt.Sprintf("euid=%d", currentEUID()),
	})

	if _, err := os.Stat("/etc/debian_version"); err == nil {
		report.Checks = append(report.Checks, domain.DoctorCheck{
			Name:     "debian_family",
			Required: true,
			OK:       true,
			Detail:   "/etc/debian_version found",
		})
	} else {
		report.Checks = append(report.Checks, domain.DoctorCheck{
			Name:     "debian_family",
			Required: true,
			OK:       false,
			Detail:   err.Error(),
		})
	}

	if err := store.EnsureLayout(); err != nil {
		report.Checks = append(report.Checks, domain.DoctorCheck{
			Name:     "layout",
			Required: true,
			OK:       false,
			Detail:   err.Error(),
		})
	} else {
		report.Checks = append(report.Checks, domain.DoctorCheck{
			Name:     "layout",
			Required: true,
			OK:       true,
			Detail:   store.Paths().ConfigDir + ", " + store.Paths().DataDir,
		})
	}

	for _, dep := range []struct {
		Name     string
		Required bool
	}{
		{Name: "nginx", Required: true},
		{Name: "systemctl", Required: true},
		{Name: "git", Required: true},
		{Name: "certbot", Required: false},
		{Name: "docker", Required: false},
		{Name: "node", Required: false},
		{Name: "npm", Required: false},
		{Name: "python3", Required: false},
		{Name: "pip3", Required: false},
		{Name: "php", Required: false},
	} {
		path, err := runner.LookPath(dep.Name)
		report.Checks = append(report.Checks, domain.DoctorCheck{
			Name:     dep.Name,
			Required: dep.Required,
			OK:       err == nil,
			Detail:   detailOrErr(path, err),
		})
	}

	return report, nil
}

func detailOrErr(detail string, err error) string {
	if err != nil {
		return err.Error()
	}
	return detail
}
