package ssh

import (
	"fmt"
	"slices"

	gossh "golang.org/x/crypto/ssh"
)

type algorithmClass string

const (
	algorithmPreferred     algorithmClass = "preferred"
	algorithmCompatibility algorithmClass = "compatibility"
	algorithmForbidden     algorithmClass = "forbidden"
)

type algorithmKind string

const (
	algorithmHostKey     algorithmKind = "host-key"
	algorithmCipher      algorithmKind = "cipher"
	algorithmKeyExchange algorithmKind = "key-exchange"
	algorithmMAC         algorithmKind = "mac"
)

type cryptoAlgorithm struct {
	Kind      algorithmKind
	Name      string
	Class     algorithmClass
	Rationale string
}

// cryptoAlgorithmCatalog is the explicit SSH negotiation policy. New algorithms
// do not become enabled merely because x/crypto/ssh learns about them: they must
// first be classified here and covered by policy tests.
var cryptoAlgorithmCatalog = []cryptoAlgorithm{
	// Host-key algorithms.
	{algorithmHostKey, gossh.KeyAlgoED25519, algorithmPreferred, "modern compact host key with strong interoperability"},
	{algorithmHostKey, gossh.KeyAlgoECDSA256, algorithmCompatibility, "supported non-SHA1 host key for existing ECDSA deployments"},
	{algorithmHostKey, gossh.KeyAlgoECDSA384, algorithmCompatibility, "supported non-SHA1 host key for existing ECDSA deployments"},
	{algorithmHostKey, gossh.KeyAlgoECDSA521, algorithmCompatibility, "supported non-SHA1 host key for existing ECDSA deployments"},
	{algorithmHostKey, gossh.KeyAlgoRSASHA512, algorithmCompatibility, "RSA host keys are accepted only with SHA-2 signatures"},
	{algorithmHostKey, gossh.KeyAlgoRSASHA256, algorithmCompatibility, "RSA host keys are accepted only with SHA-2 signatures"},
	{algorithmHostKey, gossh.KeyAlgoRSA, algorithmForbidden, "ssh-rsa uses SHA-1 signatures and is insecure in x/crypto/ssh"},
	{algorithmHostKey, gossh.InsecureKeyAlgoDSA, algorithmForbidden, "DSA is restricted to insecure key sizes"},
	{algorithmHostKey, gossh.CertAlgoRSAv01, algorithmForbidden, "RSA SHA-1 certificate signature algorithm is not approved"},
	{algorithmHostKey, gossh.InsecureCertAlgoDSAv01, algorithmForbidden, "DSA certificate algorithm is not approved"},

	// Ciphers.
	{algorithmCipher, gossh.CipherChaCha20Poly1305, algorithmPreferred, "modern authenticated encryption"},
	{algorithmCipher, gossh.CipherAES128GCM, algorithmPreferred, "authenticated encryption"},
	{algorithmCipher, gossh.CipherAES256GCM, algorithmPreferred, "authenticated encryption"},
	{algorithmCipher, gossh.CipherAES128CTR, algorithmCompatibility, "supported non-CBC stream mode for existing peers"},
	{algorithmCipher, gossh.CipherAES192CTR, algorithmCompatibility, "supported non-CBC stream mode for existing peers"},
	{algorithmCipher, gossh.CipherAES256CTR, algorithmCompatibility, "supported non-CBC stream mode for existing peers"},
	{algorithmCipher, gossh.InsecureCipherAES128CBC, algorithmForbidden, "CBC is listed as insecure by x/crypto/ssh"},
	{algorithmCipher, "aes256-cbc", algorithmForbidden, "legacy CBC mode is outside the approved policy"},
	{algorithmCipher, gossh.InsecureCipherTripleDESCBC, algorithmForbidden, "3DES-CBC is listed as insecure by x/crypto/ssh"},
	{algorithmCipher, gossh.InsecureCipherRC4, algorithmForbidden, "RC4 is insecure"},
	{algorithmCipher, gossh.InsecureCipherRC4128, algorithmForbidden, "RC4 is insecure"},
	{algorithmCipher, gossh.InsecureCipherRC4256, algorithmForbidden, "RC4 is insecure"},

	// Key exchange.
	{algorithmKeyExchange, gossh.KeyExchangeMLKEM768X25519, algorithmPreferred, "hybrid post-quantum/X25519 key exchange supported by the pinned Go toolchain"},
	{algorithmKeyExchange, gossh.KeyExchangeCurve25519, algorithmPreferred, "modern elliptic-curve key exchange"},
	{algorithmKeyExchange, gossh.KeyExchangeECDHP256, algorithmCompatibility, "supported SHA-2 ECDH for existing peers"},
	{algorithmKeyExchange, gossh.KeyExchangeECDHP384, algorithmCompatibility, "supported SHA-2 ECDH for existing peers"},
	{algorithmKeyExchange, gossh.KeyExchangeECDHP521, algorithmCompatibility, "supported SHA-2 ECDH for existing peers"},
	{algorithmKeyExchange, gossh.KeyExchangeDH14SHA256, algorithmCompatibility, "finite-field compatibility KEX with SHA-2"},
	{algorithmKeyExchange, gossh.KeyExchangeDH16SHA512, algorithmCompatibility, "finite-field compatibility KEX with SHA-2"},
	{algorithmKeyExchange, gossh.KeyExchangeDHGEXSHA256, algorithmCompatibility, "group-exchange compatibility KEX with SHA-2"},
	{algorithmKeyExchange, "curve25519-sha256@libssh.org", algorithmForbidden, "deprecated alias is not configured explicitly; x/crypto handles compatibility for the canonical Curve25519 KEX"},
	{algorithmKeyExchange, gossh.InsecureKeyExchangeDH14SHA1, algorithmForbidden, "SHA-1 KEX is listed as insecure by x/crypto/ssh"},
	{algorithmKeyExchange, gossh.InsecureKeyExchangeDH1SHA1, algorithmForbidden, "weak finite-field group and SHA-1"},
	{algorithmKeyExchange, gossh.InsecureKeyExchangeDHGEXSHA1, algorithmForbidden, "SHA-1 group-exchange KEX is insecure"},

	// MACs.
	{algorithmMAC, gossh.HMACSHA256ETM, algorithmPreferred, "encrypt-then-MAC with SHA-2"},
	{algorithmMAC, gossh.HMACSHA512ETM, algorithmPreferred, "encrypt-then-MAC with SHA-2"},
	{algorithmMAC, gossh.HMACSHA256, algorithmCompatibility, "supported SHA-2 MAC for existing peers"},
	{algorithmMAC, gossh.HMACSHA512, algorithmCompatibility, "supported SHA-2 MAC for existing peers"},
	{algorithmMAC, gossh.HMACSHA1, algorithmCompatibility, "HMAC-SHA1 remains in x/crypto SupportedAlgorithms but is lower preference"},
	{algorithmMAC, gossh.InsecureHMACSHA196, algorithmForbidden, "truncated HMAC-SHA1 is listed as insecure by x/crypto/ssh"},
}

func defaultCryptoAlgorithms() (gossh.Algorithms, error) {
	algorithms := cryptoAlgorithmsForClasses(algorithmPreferred, algorithmCompatibility)
	if err := validateCryptoAlgorithms(algorithms); err != nil {
		return gossh.Algorithms{}, err
	}
	return algorithms, nil
}

func preferredCryptoAlgorithms() (gossh.Algorithms, error) {
	algorithms := cryptoAlgorithmsForClasses(algorithmPreferred)
	if err := validateCryptoAlgorithms(algorithms); err != nil {
		return gossh.Algorithms{}, err
	}
	return algorithms, nil
}

func cryptoAlgorithmsForClasses(classes ...algorithmClass) gossh.Algorithms {
	allowed := make(map[algorithmClass]struct{}, len(classes))
	for _, class := range classes {
		allowed[class] = struct{}{}
	}

	var algorithms gossh.Algorithms
	for _, entry := range cryptoAlgorithmCatalog {
		if _, ok := allowed[entry.Class]; !ok {
			continue
		}
		switch entry.Kind {
		case algorithmHostKey:
			algorithms.HostKeys = append(algorithms.HostKeys, entry.Name)
		case algorithmCipher:
			algorithms.Ciphers = append(algorithms.Ciphers, entry.Name)
		case algorithmKeyExchange:
			algorithms.KeyExchanges = append(algorithms.KeyExchanges, entry.Name)
		case algorithmMAC:
			algorithms.MACs = append(algorithms.MACs, entry.Name)
		}
	}
	return algorithms
}

func classifyCryptoAlgorithm(kind algorithmKind, name string) (algorithmClass, bool) {
	for _, entry := range cryptoAlgorithmCatalog {
		if entry.Kind == kind && entry.Name == name {
			return entry.Class, true
		}
	}
	return algorithmForbidden, false
}

func validateCryptoAlgorithms(algorithms gossh.Algorithms) error {
	if len(algorithms.HostKeys) == 0 || len(algorithms.Ciphers) == 0 || len(algorithms.KeyExchanges) == 0 || len(algorithms.MACs) == 0 {
		return fmt.Errorf("SSH crypto policy must define host keys, ciphers, key exchanges and MACs")
	}

	supported := gossh.SupportedAlgorithms()
	checks := []struct {
		kind      algorithmKind
		values    []string
		supported []string
	}{
		{algorithmHostKey, algorithms.HostKeys, supported.HostKeys},
		{algorithmCipher, algorithms.Ciphers, supported.Ciphers},
		{algorithmKeyExchange, algorithms.KeyExchanges, supported.KeyExchanges},
		{algorithmMAC, algorithms.MACs, supported.MACs},
	}

	for _, check := range checks {
		seen := make(map[string]struct{}, len(check.values))
		for _, name := range check.values {
			if _, duplicate := seen[name]; duplicate {
				return fmt.Errorf("duplicate %s algorithm %q", check.kind, name)
			}
			seen[name] = struct{}{}

			class, known := classifyCryptoAlgorithm(check.kind, name)
			if !known {
				return fmt.Errorf("unknown %s algorithm %q is forbidden by policy", check.kind, name)
			}
			if class == algorithmForbidden {
				return fmt.Errorf("forbidden %s algorithm %q", check.kind, name)
			}
			if !slices.Contains(check.supported, name) {
				return fmt.Errorf("approved %s algorithm %q is not supported by this x/crypto/ssh version", check.kind, name)
			}
		}
	}
	return nil
}
