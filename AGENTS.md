# SSHPilot engineering execution contract

This repository is developed through a plan-driven, test-first, bounded auto-repair loop.

## 1. Source of truth

1. Read `docs/MASTER_CODEBASE_PLAN.md`.
2. Select exactly one `READY` work item whose dependencies are `DONE`.
3. Read the subsystem code, tests, invariants, API contracts, and relevant docs before changing code.
4. Do not invent side quests. New work discovered during implementation becomes a new plan item unless it is required to satisfy the current acceptance criteria.
5. Never mark an item `DONE` without reproducible evidence from the required gates.

`docs/MASTER_CODEBASE_PLAN.md` is the canonical implementation plan. `docs/ENGINEERING_CONTROL_LOOP.md` defines how every item is executed.

## 2. Mandatory cycle

For each atomic plan item run this state machine:

`SELECT -> MODEL -> TEST-DESIGN -> IMPLEMENT -> FAST-GATES -> EDGE-SPACE -> FUZZ/PROPERTY -> MUTATION -> SECURITY/PERF -> REVIEW -> COMMIT -> PLAN-CHECKPOINT`

If a gate fails:

`FAIL -> CLASSIFY -> MINIMAL FIX -> RE-RUN FAILED GATE -> RE-RUN DEPENDENT GATES`

Maximum automatic repair attempts for the same failure class: **3**. After the third failure, stop changing code, preserve evidence, and record a blocker in the plan.

## 3. Test-first requirement

Before production code changes, define the observable behavior and add or update tests that fail for the intended reason whenever practical. A task is incomplete if tests only execute code without asserting its contract.

Required layers, chosen by risk:

- table-driven unit tests for deterministic logic;
- contract tests at package boundaries;
- integration tests for SSH/SFTP/WebSocket/filesystem/process interactions;
- race tests for code with goroutines, channels, shared state, cancellation, pools, caches, files, or connection lifecycle;
- fuzz/property tests for parsers, protocol input, paths, config/vault bytes, terminal escape streams, API payloads, and other high-entropy inputs;
- end-to-end/smoke tests for user-visible critical paths;
- mutation testing as a test-of-tests gate.

## 4. Multidimensional edge-case space

Do not write edge cases as a flat list. Model each risky behavior as dimensions and values, then generate combinations deliberately.

Minimum strategy:

- each value appears at least once;
- pairwise coverage for ordinary cross-product spaces;
- 3-wise coverage where three factors can interact materially;
- exhaustive combinations for small security-critical state spaces;
- boundary values and just-inside/just-outside values for numeric/range logic;
- metamorphic/property assertions where exact expected outputs are expensive;
- fault injection at I/O boundaries.

Subsystem dimensions must include, where applicable:

- SSH: auth method, host-key state, key algorithm, cipher/KEX policy, IPv4/IPv6/hostname, port, latency, disconnect phase, retry phase, cancellation phase, server banner, remote OS/shell, concurrent sessions.
- Vault/config: absent/present/corrupt/truncated/future-version data, wrong key, permissions, atomic-write failure, migration version, concurrent access, crash between temp-write and rename, Unicode and maximal fields.
- SFTP/remote FS: file/dir/symlink, absolute/relative/parent traversal, permissions, Unicode, long paths, large/empty files, interrupted transfer, partial write, rename/delete races, remote disconnect.
- Terminal/PTY: escape sequence family, fragmented byte delivery, UTF-8/rune width, combining characters, alternate screen, cursor edges, resize timing, paste size, keyboard modifiers, slow/closed WebSocket, cancellation.
- HTTP/WebSocket: method, malformed/oversized payload, missing fields, duplicate requests, cancellation, client disconnect, concurrent operations, server shutdown, dangerous remote-action validation.
- Monitoring/diagnostics/load tests: missing `/proc` fields, locale variation, command failure, permission denial, huge process lists, counter rollover, time jumps, zero/one/max workers, RPS edges, cancellation and backpressure.

For every task, record the dimensions actually exercised by its tests.

## 5. Mutation testing policy

Use Gremlins for Go mutation testing. A passing coverage percentage is not sufficient.

For changed production Go code on a PR, run the differential campaign in integration mode so a mutant is checked against the complete suite, including package-boundary integration tests:

```sh
gremlins unleash --integration --diff "origin/$GITHUB_BASE_REF" --coverpkg "./..." --threshold-efficacy 80 --threshold-mcover 70
```

Pure test infrastructure under `internal/testkit/` is excluded from the mutation numerator because it is not shipped behavior. Changes there are still covered by normal/race tests and are classified as test-suite changes for the full mutation-baseline ratchet.

Before trusting Gremlins on CI, the Mutation Gate runs an independent execution canary: it introduces a known-fatal conditional mutation into the atomic-write transaction in an isolated copy and proves the focused test rejects it. If the canary survives, mutation results are invalid and the gate fails before threshold evaluation.

Interpret survivors as missing test semantics first, not as permission to weaken mutation settings. Security-critical code (`internal/ssh`, `internal/config`, remote command/filesystem boundaries) targets **>= 90% efficacy** over time; the global floor is ratcheted upward and never lowered merely to make CI green.

A survived mutant may be suppressed/excluded only with a written rationale proving equivalence or tool limitation. Test-support exclusions must be path-specific and must never hide production behavior that can execute in a release binary.

## 6. Controlled auto-fix policy

Automatic fixes MAY:

- format code and imports;
- fix local compile/type errors created by the current patch;
- make the smallest implementation correction required by a failing relevant test;
- add assertions or missing tests that expose the defect;
- remove dead code introduced by the current task;
- repair deterministic races/resource leaks within the task boundary.

Automatic fixes MUST NOT:

- delete, skip, weaken, or broadly rewrite a failing test to make it pass;
- reduce coverage/mutation/security thresholds;
- add blanket `nolint`, ignored errors, empty recover blocks, broad retries, arbitrary sleeps, or inflated timeouts as a substitute for correctness;
- weaken SSH host-key verification or enable insecure host-key callbacks;
- broaden cryptographic algorithms/ciphers/KEX/MACs without an explicit plan item and security review;
- weaken path validation, command validation, permission checks, or secret handling;
- change a persistence format, public API, migration semantics, auth/security policy, or destructive behavior without explicit plan acceptance criteria;
- force-push or directly auto-merge to `main`.

## 7. Gate order

Run cheap, diagnostic gates first and expensive gates later:

1. `gofmt` cleanliness.
2. `go vet ./...`.
3. focused package tests for the touched area.
4. `go test -count=1 ./...`.
5. `go test -race -count=1 ./...` for concurrency-relevant changes (default for CI on Linux).
6. edge-space/property/fuzz seed tests.
7. `govulncheck ./...` for dependency/security-sensitive changes.
8. mutation execution canary, then integration-aware differential mutation testing on the PR.
9. relevant integration/E2E/performance gates.
10. cross-platform build when platform-sensitive code changes.

Never start auto-repair from an expensive gate if a cheaper prerequisite is already red.

## 8. Patch containment

Prefer one behavioral change per commit. Keep fixes inside the selected plan item. If a repair crosses an architecture boundary or changes more than one public contract, split the work or create a dependent plan item.

Every commit message must identify the plan item, for example:

`feat(ssh): MP-2.3 harden host-key rotation handling`

## 9. Completion evidence

A plan item may be marked `DONE` only with:

- changed files/functions;
- tests added/changed;
- edge-space dimensions covered;
- commands/gates executed and results;
- mutation result for changed Go code, or an explicit `N/A` reason;
- security/performance evidence when required;
- remaining known limitations linked to follow-up plan IDs.

The goal is not maximum automatic activity. The goal is small, explainable, reversible, evidence-backed progress through the master plan.
