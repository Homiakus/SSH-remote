package config

import "testing"

func TestValidateHostValidIP(t *testing.T) {
	for _, host := range []string{
		"192.168.1.1",
		"10.0.0.1",
		"185.72.144.39",
		"::1",
		"fe80::1",
	} {
		if err := ValidateHost(host); err != nil {
			t.Errorf("ValidateHost(%q) = %v, want nil", host, err)
		}
	}
}

func TestValidateHostValidDomain(t *testing.T) {
	for _, host := range []string{
		"example.com",
		"my-server.local",
		"sub.domain.example.co.uk",
	} {
		if err := ValidateHost(host); err != nil {
			t.Errorf("ValidateHost(%q) = %v, want nil", host, err)
		}
	}
}

func TestValidateHostInvalid(t *testing.T) {
	for _, host := range []string{
		"",
		"host with space",
		"-badprefix.com",
		"host;rm -rf /",
		"host|cat /etc/passwd",
	} {
		if err := ValidateHost(host); err == nil {
			t.Errorf("ValidateHost(%q) = nil, want error", host)
		}
	}
}
