# Deterministic test fixtures

`MP-0.3` provides small in-process fixtures for external boundaries that otherwise make tests slow, flaky, or hard to fault-inject. They are intentionally protocol- or interface-level helpers rather than a global mocking framework.

## Rules

- Prefer deterministic state transitions and explicit fault scripts over `time.Sleep`, public networks, or scheduler assumptions.
- Keep each fixture focused on one boundary. Compose fixtures in the test that owns the behavioral contract.
- Record calls when order/arguments are part of the contract; return defensive copies for mutable data.
- Failure injection is ordinal (`fail the Nth operation`) so multi-step recovery/rollback paths are reproducible.
- Fixtures must not weaken production verification. For example, the SSH fixture is paired with the real TOFU callback in integration tests; `InsecureIgnoreHostKey` is used only inside the fixture's self-test client.
- A new production seam introduced for testability must keep the default production behavior unchanged.
- `internal/testkit/**` is test infrastructure, not production behavior. It is excluded from mutation-threshold numerator/denominator, but its own tests, race checks, and consumers remain mandatory. Changes under `internal/testkit/**` are treated as test-suite changes by the mutation baseline ratchet.
- Production seams exercised primarily through cross-package fixture tests must use integration-aware differential mutation testing so mutants are checked against the complete test suite.

## Catalog

### `internal/testkit/sshfixture`

In-process SSH server with a deterministic Ed25519 host key and scripted phases:

- successful password authentication and `exec` responses;
- handshake rejection;
- authentication rejection;
- session-channel rejection;
- stdout/stderr/non-zero exit status.

Use it for transport/auth/session contracts without external SSH hosts.

### `internal/testkit/knownhostsfixture`

Materializes deterministic `known_hosts` states:

- missing;
- known/same key;
- known/changed key;
- corrupt file.

The helper uses deterministic keys, so host-key state-machine tests are reproducible across runs.

### `internal/testkit/wsfixture`

`http.Hijacker` fixture backed by `net.Pipe` with client-frame/server-frame helpers. A server-write signal fires immediately before the underlying write, allowing backpressure assertions without sleeps. It supports short, 16-bit extended, and 64-bit extended payload lengths.

### `internal/testkit/remotefs`

Function-field implementation of `internal/ssh.RemoteFS` with immutable call snapshots. Consumers can script errors/returns per method and assert operation order, paths, permissions, ownership, transfer requests, and payloads.

### `scripts/go/site-admin-tui/internal/system.FakeRunner`

The existing command fake now accepts a context-aware handler and a programmable `LookPath` handler while preserving its older handler/map behavior. This supports deterministic stdout/stderr, cancellation, lookup failures, and argument assertions.

### `internal/testkit/faultfs` + `internal/atomicfile`

`internal/atomicfile` contains the production atomic-write transaction behind an injectable filesystem-operations interface. `faultfs` can fail the Nth mkdir/create/write/chmod/close/rename/stat/remove operation.

The matrix tests cover pre-commit failures, rename fallback, backup creation, restoration after replacement failure, and cleanup. `internal/config` delegates its vault/config atomic writes to this transaction.

The Mutation Gate also uses the atomic-write success path as an execution canary: in an isolated source copy it changes the `MkdirAll` success predicate to its logical opposite and verifies the focused atomic-file test fails. If that deliberately fatal mutant survives, the mutation engine is considered untrustworthy and the gate fails before Gremlins thresholds are evaluated.

## Edge-space use

Fixtures are building blocks for the multidimensional matrices in `docs/ENGINEERING_CONTROL_LOOP.md`. Examples:

- SSH: auth method × host-key state × handshake/session phase × command exit state.
- Persistence: destination present/absent × failure operation × failure ordinal × rollback outcome.
- WebSocket: opcode × payload-length class × peer state × backpressure/disconnect point.
- Remote FS: path class × operation × return/error × permission/ownership × transfer overwrite mode.
- Runner: command × stdout/stderr state × cancellation timing × lookup result.

Tests should use pairwise coverage for ordinary combinations and exhaustive small subspaces for trust, rollback, destructive operations, and other security-critical behavior.
