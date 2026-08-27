package domain

import "testing"

func TestValidateSiteName(t *testing.T) {
	valid := []string{"static-demo", "node_demo", "v1.site"}
	for _, name := range valid {
		t.Run("valid "+name, func(t *testing.T) {
			if err := ValidateSiteName(name); err != nil {
				t.Fatalf("ValidateSiteName(%q) error = %v", name, err)
			}
		})
	}

	invalid := []string{"", "../x", "a/b", `a\b`, ".", "..", "site.", "bad:name", "bad name"}
	for _, name := range invalid {
		t.Run("invalid "+name, func(t *testing.T) {
			if err := ValidateSiteName(name); err == nil {
				t.Fatalf("ValidateSiteName(%q) expected error", name)
			}
		})
	}
}

func TestValidateRelativePathRejectsTraversal(t *testing.T) {
	invalid := []string{"../shared", "/etc/passwd", `..\shared`, "."}
	for _, value := range invalid {
		t.Run(value, func(t *testing.T) {
			if err := ValidateRelativePath("shared_dirs", value, false); err == nil {
				t.Fatalf("expected %q to be rejected", value)
			}
		})
	}

	if err := ValidateRelativePath("root_dir", ".", true); err != nil {
		t.Fatalf("dot root should be allowed: %v", err)
	}
	if err := ValidateRelativePath("shared_dirs", "storage/uploads", false); err != nil {
		t.Fatalf("safe relative path rejected: %v", err)
	}
}

func TestValidateDomainRejectsNginxInjection(t *testing.T) {
	if err := ValidateDomain("demo.test"); err != nil {
		t.Fatalf("valid domain rejected: %v", err)
	}
	if err := ValidateDomain("demo.test;\nserver_name evil.test"); err == nil {
		t.Fatal("expected unsafe domain to be rejected")
	}
}

func TestValidateServiceUnitNameRejectsPathTraversal(t *testing.T) {
	if err := ValidateServiceUnitName("site-admin-demo"); err != nil {
		t.Fatalf("valid unit name rejected: %v", err)
	}
	if err := ValidateServiceUnitName("../evil"); err == nil {
		t.Fatal("expected unsafe unit name to be rejected")
	}
}
