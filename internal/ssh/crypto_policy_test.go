package ssh

import (
	"slices"
	"strings"
	"testing"

	"sshpilot/internal/testkit/sshfixture"

	gossh "golang.org/x/crypto/ssh"
)

func TestCryptoAlgorithmClassification(t *testing.T) {
	tests := []struct {
		kind      algorithmKind
		name      string
		wantClass algorithmClass
		wantKnown bool
	}{
		{algorithmHostKey, gossh.KeyAlgoED25519, algorithmPreferred, true},
		{algorithmHostKey, gossh.KeyAlgoRSASHA256, algorithmCompatibility, true},
		{algorithmHostKey, gossh.KeyAlgoRSA, algorithmForbidden, true},
		{algorithmCipher, gossh.CipherAES128GCM, algorithmPreferred, true},
		{algorithmCipher, gossh.CipherAES256CTR, algorithmCompatibility, true},
		{algorithmCipher, gossh.InsecureCipherAES128CBC, algorithmForbidden, true},
		{algorithmCipher, "aes256-cbc", algorithmForbidden, true},
		{algorithmKeyExchange, gossh.KeyExchangeCurve25519, algorithmPreferred, true},
		{algorithmKeyExchange, gossh.KeyExchangeDH14SHA256, algorithmCompatibility, true},
		{algorithmKeyExchange, gossh.InsecureKeyExchangeDH14SHA1, algorithmForbidden, true},
		{algorithmKeyExchange, "curve25519-sha256@libssh.org", algorithmForbidden, true},
		{algorithmMAC, gossh.HMACSHA256ETM, algorithmPreferred, true},
		{algorithmMAC, gossh.HMACSHA1, algorithmCompatibility, true},
		{algorithmMAC, gossh.InsecureHMACSHA196, algorithmForbidden, true},
		{algorithmCipher, "future-unknown-cipher", algorithmForbidden, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind)+"/"+tt.name, func(t *testing.T) {
			gotClass, gotKnown := classifyCryptoAlgorithm(tt.kind, tt.name)
			if gotClass != tt.wantClass || gotKnown != tt.wantKnown {
				t.Fatalf("classify(%s, %q) = (%s, %v), want (%s, %v)", tt.kind, tt.name, gotClass, gotKnown, tt.wantClass, tt.wantKnown)
			}
		})
	}
}

func TestDefaultCryptoAlgorithmsAreSupportedAndExcludeInsecure(t *testing.T) {
	algorithms, err := defaultCryptoAlgorithms()
	if err != nil {
		t.Fatalf("defaultCryptoAlgorithms: %v", err)
	}

	supported := gossh.SupportedAlgorithms()
	insecure := gossh.InsecureAlgorithms()
	checks := []struct {
		name      string
		values    []string
		supported []string
		insecure  []string
	}{
		{"host keys", algorithms.HostKeys, supported.HostKeys, insecure.HostKeys},
		{"ciphers", algorithms.Ciphers, supported.Ciphers, insecure.Ciphers},
		{"key exchanges", algorithms.KeyExchanges, supported.KeyExchanges, insecure.KeyExchanges},
		{"MACs", algorithms.MACs, supported.MACs, insecure.MACs},
	}

	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if len(check.values) == 0 {
				t.Fatal("approved list is empty")
			}
			seen := map[string]bool{}
			for _, name := range check.values {
				if seen[name] {
					t.Fatalf("duplicate approved algorithm %q", name)
				}
				seen[name] = true
				if !slices.Contains(check.supported, name) {
					t.Fatalf("approved algorithm %q is not in ssh.SupportedAlgorithms()", name)
				}
				if slices.Contains(check.insecure, name) {
					t.Fatalf("approved algorithm %q is in ssh.InsecureAlgorithms()", name)
				}
			}
		})
	}
}

func TestDefaultCryptoAlgorithmsPreferPreferredBeforeCompatibility(t *testing.T) {
	algorithms, err := defaultCryptoAlgorithms()
	if err != nil {
		t.Fatalf("defaultCryptoAlgorithms: %v", err)
	}

	checks := []struct {
		kind   algorithmKind
		values []string
	}{
		{algorithmHostKey, algorithms.HostKeys},
		{algorithmCipher, algorithms.Ciphers},
		{algorithmKeyExchange, algorithms.KeyExchanges},
		{algorithmMAC, algorithms.MACs},
	}
	for _, check := range checks {
		seenCompatibility := false
		for _, name := range check.values {
			class, known := classifyCryptoAlgorithm(check.kind, name)
			if !known {
				t.Fatalf("approved %s algorithm %q not classified", check.kind, name)
			}
			switch class {
			case algorithmPreferred:
				if seenCompatibility {
					t.Fatalf("preferred %s algorithm %q appears after compatibility fallback", check.kind, name)
				}
			case algorithmCompatibility:
				seenCompatibility = true
			default:
				t.Fatalf("approved %s algorithm %q has class %s", check.kind, name, class)
			}
		}
	}
}

func TestValidateCryptoAlgorithmsFailsClosed(t *testing.T) {
	base, err := defaultCryptoAlgorithms()
	if err != nil {
		t.Fatalf("defaultCryptoAlgorithms: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*gossh.Algorithms)
		want   string
	}{
		{name: "empty host keys", mutate: func(a *gossh.Algorithms) { a.HostKeys = nil }, want: "must define"},
		{name: "unknown cipher", mutate: func(a *gossh.Algorithms) { a.Ciphers = append(a.Ciphers, "future-unknown-cipher") }, want: "unknown cipher"},
		{name: "forbidden host key", mutate: func(a *gossh.Algorithms) { a.HostKeys = append(a.HostKeys, gossh.KeyAlgoRSA) }, want: "forbidden host-key"},
		{name: "forbidden KEX", mutate: func(a *gossh.Algorithms) { a.KeyExchanges = append(a.KeyExchanges, gossh.InsecureKeyExchangeDH14SHA1) }, want: "forbidden key-exchange"},
		{name: "duplicate MAC", mutate: func(a *gossh.Algorithms) { a.MACs = append(a.MACs, a.MACs[0]) }, want: "duplicate mac"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := cloneAlgorithms(base)
			tt.mutate(&candidate)
			err := validateCryptoAlgorithms(candidate)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validate error = %v, want substring %q", err, tt.want)
			}
		})
	}
}

func TestPreferredCryptoAlgorithmsContainNoCompatibilityFallbacks(t *testing.T) {
	algorithms, err := preferredCryptoAlgorithms()
	if err != nil {
		t.Fatalf("preferredCryptoAlgorithms: %v", err)
	}
	checks := []struct {
		kind   algorithmKind
		values []string
	}{
		{algorithmHostKey, algorithms.HostKeys},
		{algorithmCipher, algorithms.Ciphers},
		{algorithmKeyExchange, algorithms.KeyExchanges},
		{algorithmMAC, algorithms.MACs},
	}
	for _, check := range checks {
		for _, name := range check.values {
			class, known := classifyCryptoAlgorithm(check.kind, name)
			if !known || class != algorithmPreferred {
				t.Fatalf("preferred policy contains %s %q classified as %s known=%v", check.kind, name, class, known)
			}
		}
	}
}

func TestConnectNegotiatesPreferredAlgorithms(t *testing.T) {
	server := startCryptoFixture(t, sshfixture.Options{
		User:     "pilot",
		Password: "secret",
		Algorithms: &gossh.Algorithms{
			KeyExchanges: []string{gossh.KeyExchangeCurve25519},
			Ciphers:      []string{gossh.CipherAES128GCM},
			MACs:         []string{gossh.HMACSHA256ETM},
		},
	})
	client, err := Connect(fixtureServerConfig(t, server, "pilot", "secret"))
	if err != nil {
		t.Fatalf("Connect preferred-only fixture: %v", err)
	}
	defer client.Close()
	assertNegotiated(t, server, gossh.KeyExchangeCurve25519, gossh.KeyAlgoED25519, gossh.CipherAES128GCM, gossh.HMACSHA256ETM)
}

func TestConnectNegotiatesCompatibilityAlgorithmsWithoutWeakFallback(t *testing.T) {
	server := startCryptoFixture(t, sshfixture.Options{
		User:             "pilot",
		Password:         "secret",
		HostKeyAlgorithm: gossh.KeyAlgoECDSA256,
		Algorithms: &gossh.Algorithms{
			KeyExchanges: []string{gossh.KeyExchangeDH14SHA256},
			Ciphers:      []string{gossh.CipherAES128CTR},
			MACs:         []string{gossh.HMACSHA1},
		},
	})
	client, err := Connect(fixtureServerConfig(t, server, "pilot", "secret"))
	if err != nil {
		t.Fatalf("Connect compatibility-only fixture: %v", err)
	}
	defer client.Close()
	assertNegotiated(t, server, gossh.KeyExchangeDH14SHA256, gossh.KeyAlgoECDSA256, gossh.CipherAES128CTR, gossh.HMACSHA1)
}

func TestConnectRejectsForbiddenOnlyNegotiation(t *testing.T) {
	server := startCryptoFixture(t, sshfixture.Options{
		User:     "pilot",
		Password: "secret",
		Algorithms: &gossh.Algorithms{
			KeyExchanges: []string{gossh.InsecureKeyExchangeDH14SHA1},
			Ciphers:      []string{gossh.CipherAES128GCM},
			MACs:         []string{gossh.HMACSHA256ETM},
		},
	})
	client, err := Connect(fixtureServerConfig(t, server, "pilot", "secret"))
	if client != nil {
		_ = client.Close()
		t.Fatal("forbidden-only fixture unexpectedly connected")
	}
	if err == nil || !strings.Contains(err.Error(), "no common algorithm") {
		t.Fatalf("Connect forbidden-only error = %v, want negotiation failure", err)
	}
}

func TestConnectResistsDowngradeOffer(t *testing.T) {
	server := startCryptoFixture(t, sshfixture.Options{
		User:     "pilot",
		Password: "secret",
		Algorithms: &gossh.Algorithms{
			KeyExchanges: []string{gossh.InsecureKeyExchangeDH14SHA1, gossh.KeyExchangeCurve25519},
			Ciphers:      []string{gossh.InsecureCipherAES128CBC, gossh.CipherAES128GCM},
			MACs:         []string{gossh.InsecureHMACSHA196, gossh.HMACSHA256ETM},
		},
	})
	client, err := Connect(fixtureServerConfig(t, server, "pilot", "secret"))
	if err != nil {
		t.Fatalf("Connect downgrade fixture: %v", err)
	}
	defer client.Close()
	assertNegotiated(t, server, gossh.KeyExchangeCurve25519, gossh.KeyAlgoED25519, gossh.CipherAES128GCM, gossh.HMACSHA256ETM)
}

func startCryptoFixture(t *testing.T, options sshfixture.Options) *sshfixture.Server {
	t.Helper()
	t.Chdir(t.TempDir())
	server, err := sshfixture.Start(options)
	if err != nil {
		t.Fatalf("start SSH crypto fixture: %v", err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server
}

func assertNegotiated(t *testing.T, server *sshfixture.Server, kex, hostKey, cipher, mac string) {
	t.Helper()
	algorithms, ok := server.NegotiatedAlgorithms()
	if !ok {
		t.Fatal("fixture did not record negotiated algorithms")
	}
	if algorithms.KeyExchange != kex {
		t.Fatalf("negotiated KEX = %q, want %q", algorithms.KeyExchange, kex)
	}
	if algorithms.HostKey != hostKey {
		t.Fatalf("negotiated host key = %q, want %q", algorithms.HostKey, hostKey)
	}
	if algorithms.Read.Cipher != cipher || algorithms.Write.Cipher != cipher {
		t.Fatalf("negotiated cipher read/write = %q/%q, want %q", algorithms.Read.Cipher, algorithms.Write.Cipher, cipher)
	}
	if algorithms.Read.MAC != mac || algorithms.Write.MAC != mac {
		t.Fatalf("negotiated MAC read/write = %q/%q, want %q", algorithms.Read.MAC, algorithms.Write.MAC, mac)
	}
}

func cloneAlgorithms(src gossh.Algorithms) gossh.Algorithms {
	return gossh.Algorithms{
		HostKeys:     append([]string(nil), src.HostKeys...),
		Ciphers:      append([]string(nil), src.Ciphers...),
		KeyExchanges: append([]string(nil), src.KeyExchanges...),
		MACs:         append([]string(nil), src.MACs...),
	}
}
