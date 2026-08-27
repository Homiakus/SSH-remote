package tui

import (
	"testing"

	"sshpilot/scripts/go/site-admin-tui/internal/domain"
)

func TestWizardBuildsGitNodeSpec(t *testing.T) {
	t.Parallel()

	w := newWizard()
	w.fields[fieldName].Input.SetValue("demo")
	w.fields[fieldDomain].Input.SetValue("demo.test")
	w.fields[fieldRuntime].Index = 2 // node
	w.fields[fieldSource].Index = 0  // git
	w.fields[fieldGitRepo].Input.SetValue("https://example.com/demo.git")
	w.fields[fieldPort].Input.SetValue("3000")
	w.fields[fieldStartCommand].Input.SetValue("node server.js")
	w.fields[fieldTLS].Index = 1
	w.fields[fieldTLSEmail].Input.SetValue("ops@example.com")

	spec, err := w.spec()
	if err != nil {
		t.Fatalf("build spec: %v", err)
	}
	if spec.Runtime != domain.RuntimeNode {
		t.Fatalf("expected node runtime, got %s", spec.Runtime)
	}
	if spec.Source.Kind != domain.SourceGit {
		t.Fatalf("expected git source, got %s", spec.Source.Kind)
	}
	if got := spec.Service.Command[0]; got != "node server.js" {
		t.Fatalf("unexpected start command %q", got)
	}
	if !spec.TLS.Enabled {
		t.Fatal("expected TLS enabled")
	}
}

func TestWizardVisibleFieldsForExistingDirDocker(t *testing.T) {
	t.Parallel()

	w := newWizard()
	w.fields[fieldRuntime].Index = 4 // docker_compose
	w.fields[fieldSource].Index = 1  // existing_dir

	indices := w.visibleIndices()
	if !contains(indices, fieldExistingDir) {
		t.Fatal("expected existing_dir field to be visible")
	}
	if !contains(indices, fieldComposeFile) {
		t.Fatal("expected compose_file field to be visible")
	}
	if contains(indices, fieldGitRepo) {
		t.Fatal("expected git fields to be hidden")
	}
}

func contains(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
