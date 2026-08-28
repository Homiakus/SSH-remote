# SSH cryptographic policy

Status: MP-1.1 policy specification.

SSHPilot uses an explicit allowlist for SSH transport negotiation. A dependency upgrade must not silently enable a newly implemented algorithm: every algorithm must first be classified in `internal/ssh/crypto_policy.go` and covered by tests.

## Classes

- **preferred** — modern algorithms offered first.
- **compatibility** — algorithms still returned by `golang.org/x/crypto/ssh.SupportedAlgorithms()` and retained for interoperability, but ordered after preferred choices.
- **forbidden** — algorithms with known security issues, deprecated aliases that should not be configured directly, unsupported legacy values, and any unknown/unclassified value. Negotiation fails closed instead of auto-enabling them.

The production client permits `preferred + compatibility`; it never includes `forbidden` algorithms. There is no automatic legacy retry.

## Host-key algorithms

| Class | Algorithms |
| --- | --- |
| preferred | `ssh-ed25519` |
| compatibility | `ecdsa-sha2-nistp256`, `ecdsa-sha2-nistp384`, `ecdsa-sha2-nistp521`, `rsa-sha2-512`, `rsa-sha2-256` |
| forbidden | `ssh-rsa`, `ssh-dss`, SHA-1 RSA certificate algorithm, DSA certificate algorithm, unknown algorithms |

`ssh-rsa` here means the SHA-1 signature algorithm, not the RSA key format. RSA host keys remain usable through `rsa-sha2-256` or `rsa-sha2-512`.

## Ciphers

| Class | Algorithms |
| --- | --- |
| preferred | `chacha20-poly1305@openssh.com`, `aes128-gcm@openssh.com`, `aes256-gcm@openssh.com` |
| compatibility | `aes128-ctr`, `aes192-ctr`, `aes256-ctr` |
| forbidden | AES-CBC, 3DES-CBC, RC4/arcfour variants, unknown algorithms |

## Key exchange

| Class | Algorithms |
| --- | --- |
| preferred | `mlkem768x25519-sha256`, `curve25519-sha256` |
| compatibility | NIST ECDH P-256/P-384/P-521, `diffie-hellman-group14-sha256`, `diffie-hellman-group16-sha512`, `diffie-hellman-group-exchange-sha256` |
| forbidden | SHA-1 DH variants, `curve25519-sha256@libssh.org` as an explicit configured alias, unknown algorithms |

The deprecated libssh Curve25519 name is not added manually; the Go SSH implementation handles compatibility from the canonical Curve25519 KEX.

## MACs

| Class | Algorithms |
| --- | --- |
| preferred | `hmac-sha2-256-etm@openssh.com`, `hmac-sha2-512-etm@openssh.com` |
| compatibility | `hmac-sha2-256`, `hmac-sha2-512`, `hmac-sha1` |
| forbidden | `hmac-sha1-96`, unknown algorithms |

`hmac-sha1` remains a compatibility fallback only because the pinned `x/crypto/ssh` version includes it in `SupportedAlgorithms()` rather than `InsecureAlgorithms()`. It is never preferred.

## Fail-closed invariants

1. Every production algorithm is present in the local catalog.
2. Every allowed algorithm must also be present in `ssh.SupportedAlgorithms()` for the pinned dependency version.
3. No allowed algorithm may occur in `ssh.InsecureAlgorithms()`.
4. Duplicate algorithm entries are rejected.
5. Empty host-key/cipher/KEX/MAC sets are rejected.
6. Unknown names are forbidden, not silently ignored.
7. The client never retries a failed negotiation by adding forbidden algorithms.
8. Preferred algorithms are ordered before compatibility algorithms.
9. Host-key verification remains TOFU/known_hosts based; crypto negotiation never bypasses a host-key mismatch.

## Test matrix

The deterministic in-process SSH fixture constrains the server side and records negotiated algorithms. MP-1.1 tests cover:

- preferred-only peer -> connection succeeds and the exact preferred algorithms are negotiated;
- compatibility-only but approved peer -> connection succeeds using the exact compatibility choices;
- forbidden-only KEX peer -> negotiation fails;
- downgrade offer containing forbidden + preferred choices -> forbidden choices are ignored and preferred choices win;
- unknown and forbidden values -> policy validation fails closed;
- dependency drift -> approved entries must still be members of `SupportedAlgorithms()` and must not enter `InsecureAlgorithms()`;
- host-key mismatch -> existing TOFU state-machine tests continue to fail closed.

## Upstream basis

- Go `x/crypto/ssh`: `SupportedAlgorithms()` explicitly excludes algorithms with security issues; `InsecureAlgorithms()` identifies the implemented insecure set. The package documentation recommends using `SupportedAlgorithms()` when SHA-1/insecure algorithms must be excluded.
- OpenSSH legacy guidance: weak legacy algorithms are disabled by default and should only be re-enabled narrowly and temporarily when an old peer cannot be upgraded.

References:
- https://pkg.go.dev/golang.org/x/crypto/ssh
- https://www.openssh.org/legacy.html
