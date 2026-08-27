# SSHPilot — Master Codebase Plan

Status values: `BACKLOG`, `READY`, `IN_PROGRESS`, `BLOCKED`, `DONE`.

This file is the single source of truth for implementation order. The execution protocol is defined in `docs/ENGINEERING_CONTROL_LOOP.md`; agent rules are in `/AGENTS.md`.

## Plan invariants

- Execute only one atomic `READY` item at a time.
- A child item cannot start before all listed dependencies are `DONE`.
- `DONE` requires evidence: tests, edge-space coverage, mutation result when Go changed, and relevant security/performance checks.
- Discoveries do not silently expand scope: add a new item with an explicit dependency.
- No test/security threshold is lowered to make an item pass.
- No automated process directly merges to `main`.

---

## MP-0 — Engineering control plane

### MP-0.1 — Plan-driven execution contract — `IN_PROGRESS`
Dependencies: none.

Deliverables:
- canonical master plan;
- cyclic coding/testing/controlled-repair protocol;
- agent execution contract;
- CI quality gate;
- mutation-testing gate.

Exit:
- repository contains the artifacts above;
- CI validates Go formatting, vet, tests, race detector, vulnerability scan, builds and PR mutation testing;
- mutation testing is differential on PRs to keep feedback bounded.

### MP-0.2 — Baseline quality ledger — `READY`
Dependencies: MP-0.1.

Create a machine-readable baseline for each relevant package:
- statement coverage;
- mutation efficacy;
- mutant coverage;
- test duration;
- race status;
- package build status;
- known fuzz targets and corpus size.

Use the baseline as a ratchet: new code must not reduce the current metric and critical packages receive progressively higher targets.

### MP-0.3 — Deterministic test fixtures — `READY`
Dependencies: MP-0.1.

Build reusable test fixtures/fakes for:
- SSH server handshake/auth/session failures;
- known_hosts/TOFU states;
- SFTP-like filesystem semantics;
- WebSocket connect/disconnect/backpressure;
- command runner/stdout/stderr/cancellation;
- filesystem atomic-write failures.

Prefer in-process deterministic fixtures over tests that depend on public networks or wall-clock sleeps.

---

## MP-1 — Security and trust boundaries

### MP-1.1 — SSH cryptographic policy audit — `READY`
Dependencies: MP-0.1.

Audit `internal/ssh` against an explicit compatibility/security policy. Classify every host-key algorithm, cipher, KEX and MAC as `preferred`, `compatibility`, or `forbidden`. Do not silently broaden crypto compatibility.

Tests must cover negotiation failure/success across policy classes, downgrade attempts, unknown algorithms and host-key mismatch.

### MP-1.2 — TOFU/known_hosts state machine — `BACKLOG`
Dependencies: MP-1.1, MP-0.3.

Specify and test states:
`unknown -> first-trust -> known-same -> known-changed -> explicit-resolution`.

Cover concurrent first connection, corrupted file, permission failures, multiple algorithms, hostname/IP aliases and key rotation policy.

### MP-1.3 — Vault key lifecycle and durable persistence — `BACKLOG`
Dependencies: MP-0.3.

Audit master-key creation, file permissions, backup/restore, corruption handling, versioning, migration, atomic writes and future KDF/version evolution.

Properties:
- authenticated corruption always fails closed;
- wrong key never returns plausible plaintext;
- interrupted writes preserve the last valid vault;
- secret material never appears in logs/errors.

### MP-1.4 — Remote command/action boundary — `BACKLOG`
Dependencies: MP-0.3.

Inventory all code paths that execute remote commands, manage services/processes or write remote files. Add typed validation and explicit dangerous-action policies where raw strings cross a trust boundary.

---

## MP-2 — SSH transport reliability

### MP-2.1 — Connection lifecycle model — `BACKLOG`
Dependencies: MP-1.1, MP-1.2.

Define dial, handshake, authentication, session, reconnect and close states. Replace error-string-driven transient classification with typed/causal classification where possible.

Edge dimensions: failure phase × retryability × cancellation timing × address family × auth method × host-key state.

### MP-2.2 — Cancellation, deadlines and bounded retry — `BACKLOG`
Dependencies: MP-2.1.

Every long-running transport operation must have a bounded deadline/cancellation path. Retries must have an explicit budget, eligible error classes and testable backoff strategy.

### MP-2.3 — Session concurrency and leak resistance — `BACKLOG`
Dependencies: MP-2.1, MP-0.3.

Stress session create/close, streaming stdout/stderr, cancellation, client close while active, goroutine termination and channel ownership under the race detector.

---

## MP-3 — Remote filesystem and script execution

### MP-3.1 — Remote path semantics — `BACKLOG`
Dependencies: MP-0.3, MP-1.4.

Specify normalization and policy for absolute paths, `..`, symlinks, roots, Unicode, empty paths and platform-specific remote semantics.

### MP-3.2 — Transfer atomicity and partial failure — `BACKLOG`
Dependencies: MP-3.1.

Cover empty/large files, short writes, disconnect mid-transfer, rename failure, permission denial, concurrent rename/delete and temp-file cleanup.

### MP-3.3 — Script execution contract — `BACKLOG`
Dependencies: MP-1.4, MP-2.2.

Specify shell selection, encoding, stdout/stderr ordering guarantees, exit-code representation, cancellation semantics and maximum output/backpressure behavior.

---

## MP-4 — Monitoring, diagnostics and load testing

### MP-4.1 — Parser robustness — `BACKLOG`
Dependencies: MP-0.3.

Fuzz and property-test parsing of `/proc`, `df`, `ps`, service status and diagnostics outputs. Cover missing fields, extra whitespace, locale variation, huge values, counter rollover and malformed lines.

### MP-4.2 — Action safety — `BACKLOG`
Dependencies: MP-1.4.

Systemd/process actions must use allowlisted actions and validated identifiers; destructive actions require explicit user intent at the UI/API boundary.

### MP-4.3 — Load-test engine correctness — `BACKLOG`
Dependencies: MP-2.2.

Prove bounded workers, cancellation, rate control, metric accounting and percentile correctness under zero/one/max worker states, timeout storms and target disconnects.

Use deterministic clock injection for timing-sensitive logic where practical.

---

## MP-5 — Terminal, TUI and Web UI correctness

### MP-5.1 — VT100/ANSI parser test model — `BACKLOG`
Dependencies: MP-0.1.

Create golden/property tests for escape-sequence parsing, including fragmentation at every byte boundary, malformed/incomplete sequences, alternate-screen transitions, cursor boundaries and erase modes.

### MP-5.2 — Unicode and cell-width correctness — `BACKLOG`
Dependencies: MP-5.1.

Cover UTF-8 split boundaries, wide runes, combining marks, emoji sequences, zero-width code points and cursor calculations.

### MP-5.3 — Input/focus/WebSocket state machine — `BACKLOG`
Dependencies: MP-5.1.

Model focus, modal state, mobile input, paste, keyboard shortcuts, reconnect, slow consumer, resize and terminal close as explicit states/transitions.

### MP-5.4 — TUI/Web behavior parity — `BACKLOG`
Dependencies: MP-3.3, MP-4.2, MP-5.3.

For shared capabilities, define one behavioral contract and verify both front ends expose consistent safety rules and error semantics.

---

## MP-6 — Deployment/admin tooling

### MP-6.1 — `site-admin-tui` contract boundary — `BACKLOG`
Dependencies: MP-0.1.

Treat `scripts/go/site-admin-tui` as a first-class package set: validation, state persistence, nginx/systemd generation and deployment operations require the same gates as the main application.

### MP-6.2 — Generated configuration safety — `BACKLOG`
Dependencies: MP-6.1.

Use golden tests plus semantic validation for nginx/systemd output; prevent unsafe path/unit/domain interpolation and require rollback evidence for failed deploys.

---

## MP-7 — Release engineering and operational confidence

### MP-7.1 — Cross-platform build matrix — `BACKLOG`
Dependencies: MP-0.1.

Continuously compile supported Windows/Linux/macOS amd64/arm64 targets. Platform-specific behavior gets targeted tests where compile-only checks are insufficient.

### MP-7.2 — Performance budgets — `BACKLOG`
Dependencies: MP-2.3, MP-4.3, MP-5.1.

Add benchmarks and budgets for hot paths: terminal parsing/rendering, metrics parsing, load-test aggregation, config/vault operations and connection/session orchestration.

Track allocations where they affect sustained throughput.

### MP-7.3 — Release evidence bundle — `BACKLOG`
Dependencies: MP-1 through MP-7 relevant items.

A release candidate must have a reproducible evidence bundle containing:
- commit SHA;
- build matrix result;
- unit/integration/race results;
- vulnerability result;
- mutation scores;
- fuzz campaigns/corpus replay result;
- benchmark deltas;
- known limitations and migration notes.

---

## Current execution pointer

`NEXT = MP-0.1`

After MP-0.1 is merged and its CI is green, set MP-0.1 to `DONE`, set `NEXT = MP-0.2`, and execute the next atomic item through `docs/ENGINEERING_CONTROL_LOOP.md`.