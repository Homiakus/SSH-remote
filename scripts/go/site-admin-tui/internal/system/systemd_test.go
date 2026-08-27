package system

import (
	"context"
	"path/filepath"
	"testing"

	"sshpilot/scripts/go/site-admin-tui/internal/domain"
)

func TestRenderServiceUnitRejectsUnsafeUnitName(t *testing.T) {
	t.Parallel()

	_, err := RenderServiceUnit(domain.SiteSpec{
		Name:    "demo",
		Runtime: domain.RuntimeNode,
		Service: domain.ServiceSpec{
			Name:    "../evil",
			Command: []string{"npm start"},
			Port:    3000,
		},
	}, "/srv/demo/current")
	if err == nil {
		t.Fatal("expected unsafe unit name error")
	}
}

func TestInstallServiceUnitRejectsPathTraversalName(t *testing.T) {
	t.Parallel()

	err := InstallServiceUnit(context.Background(), &FakeRunner{}, t.TempDir(), ServiceUnit{
		Name:    "../evil",
		Content: "[Unit]\n",
	}, filepath.Join(t.TempDir(), "backup"))
	if err == nil {
		t.Fatal("expected unsafe unit name error")
	}
}
