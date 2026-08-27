package domain

import (
	"fmt"
	"path/filepath"
	"strings"
)

func ValidateSiteName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("site name is required")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("site name %q is not allowed", name)
	}
	if filepath.IsAbs(name) || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("site name must not contain a path: %q", name)
	}
	if strings.ContainsRune(name, 0) || strings.ContainsAny(name, `<>:"|?*`) {
		return fmt.Errorf("site name contains invalid characters: %q", name)
	}
	if strings.HasSuffix(name, ".") {
		return fmt.Errorf("site name must not end with a dot: %q", name)
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			continue
		}
		return fmt.Errorf("site name must be an ASCII slug: %q", name)
	}
	return nil
}

func ValidateDomain(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("domain is required")
	}
	if len(value) > 253 {
		return fmt.Errorf("domain is too long: %q", value)
	}
	if hasControl(value) || strings.ContainsAny(value, " \t/\\;{}") {
		return fmt.Errorf("domain contains invalid characters: %q", value)
	}
	if strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") {
		return fmt.Errorf("domain has invalid label layout: %q", value)
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 {
			return fmt.Errorf("domain label has invalid length: %q", label)
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("domain label must not start or end with '-': %q", label)
		}
		for _, r := range label {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' {
				continue
			}
			return fmt.Errorf("domain must be an ASCII hostname: %q", value)
		}
	}
	return nil
}

func CleanRelativePath(value string) string {
	return filepath.Clean(strings.TrimSpace(value))
}

func ValidateRelativePath(field, value string, allowDot bool) error {
	raw := strings.TrimSpace(value)
	if raw == "" {
		return fmt.Errorf("%s is required", field)
	}
	if hasControl(raw) {
		return fmt.Errorf("%s contains control characters", field)
	}
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, `\`) || filepath.VolumeName(raw) != "" {
		return fmt.Errorf("%s must be relative: %q", field, value)
	}

	clean := CleanRelativePath(raw)
	if clean == "." {
		if allowDot {
			return nil
		}
		return fmt.Errorf("%s must not point to release root", field)
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, `..\`) {
		return fmt.Errorf("%s must not escape release directory: %q", field, value)
	}
	return nil
}

func ValidateServiceUnitName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	if name == "." || name == ".." || strings.HasSuffix(name, ".service") {
		return fmt.Errorf("service.name must be a unit basename without .service suffix: %q", name)
	}
	if len(name) > 128 || hasControl(name) || strings.ContainsAny(name, `/\ `+"\t") {
		return fmt.Errorf("service.name contains invalid characters: %q", name)
	}
	for _, r := range name {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' || r == '@' {
			continue
		}
		return fmt.Errorf("service.name must contain only ASCII unit-name characters: %q", name)
	}
	return nil
}

func ValidateSafeLine(field, value string) error {
	if strings.ContainsRune(value, 0) || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be a single line", field)
	}
	return nil
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
