package state

import (
	"path/filepath"
	"testing"

	"sshpilot/scripts/go/site-admin-tui/internal/domain"
)

func TestStoreSaveLoadAndCurrentRelease(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := NewStore(Paths{
		ConfigDir: filepath.Join(root, "etc"),
		DataDir:   filepath.Join(root, "var"),
		LogDir:    filepath.Join(root, "log"),
	})

	spec := domain.SiteSpec{
		Name:    "demo",
		Domain:  "demo.test",
		Runtime: domain.RuntimeStatic,
		Source:  domain.DeploySource{Kind: domain.SourceExistingDir, ExistingDir: "/srv/demo"},
	}
	if err := store.SaveSite(spec); err != nil {
		t.Fatalf("save site: %v", err)
	}

	loaded, err := store.LoadSite("demo")
	if err != nil {
		t.Fatalf("load site: %v", err)
	}
	if loaded.Domain != spec.Domain {
		t.Fatalf("expected domain %q, got %q", spec.Domain, loaded.Domain)
	}

	releaseDir, err := store.CreateReleaseDir("demo", "20260424-120000")
	if err != nil {
		t.Fatalf("create release dir: %v", err)
	}
	if err := store.SetCurrentRelease("demo", releaseDir); err != nil {
		t.Fatalf("set current release: %v", err)
	}

	current, err := store.CurrentReleasePath("demo")
	if err != nil {
		t.Fatalf("current release path: %v", err)
	}
	if current != releaseDir {
		t.Fatalf("expected current path %q, got %q", releaseDir, current)
	}

	nextReleaseDir, err := store.CreateReleaseDir("demo", "20260424-130000")
	if err != nil {
		t.Fatalf("create next release dir: %v", err)
	}
	if err := store.SetCurrentRelease("demo", nextReleaseDir); err != nil {
		t.Fatalf("replace current release: %v", err)
	}
	current, err = store.CurrentReleasePath("demo")
	if err != nil {
		t.Fatalf("current release path after replace: %v", err)
	}
	if current != nextReleaseDir {
		t.Fatalf("expected current path %q after replace, got %q", nextReleaseDir, current)
	}
}

func TestStoreLockAndHistory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store := NewStore(Paths{
		ConfigDir: filepath.Join(root, "etc"),
		DataDir:   filepath.Join(root, "var"),
		LogDir:    filepath.Join(root, "log"),
	})

	unlock, err := store.AcquireSiteLock("locked")
	if err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	if _, err := store.AcquireSiteLock("locked"); err == nil {
		t.Fatal("expected second lock acquisition to fail")
	}
	if err := unlock(); err != nil {
		t.Fatalf("unlock: %v", err)
	}

	record := domain.ReleaseRecord{
		ID:     "r1",
		Site:   "locked",
		Path:   "/tmp/release",
		Status: domain.ReleaseActive,
		Health: domain.HealthHealthy,
	}
	if err := store.AppendHistory("locked", record); err != nil {
		t.Fatalf("append history: %v", err)
	}
	history, err := store.LoadHistory("locked")
	if err != nil {
		t.Fatalf("load history: %v", err)
	}
	if len(history) != 1 || history[0].ID != "r1" {
		t.Fatalf("unexpected history: %+v", history)
	}
}
