# SSHPilot — Quality Baseline Ledger

Captured: **2026-08-27**. This document summarizes the machine-readable baseline in `.quality/baseline.json`.

## Module baseline

| Metric | Baseline | Policy |
|---|---:|---|
| Go toolchain | 1.26.7 | patch-level security updates required |
| Statement coverage | 37.3% | monotonic non-regression |
| Mutation efficacy | 8.31% | full-campaign floor; new-code differential gate is 80% |
| Mutant coverage | 50.75% | full-campaign floor; new-code differential gate is 70% |
| Race detector | PASS | must remain pass |
| `go vet` | PASS | must remain pass |
| `govulncheck` | PASS | zero reachable known vulnerabilities |
| Cross-build | 6/6 PASS | windows/linux/darwin × amd64/arm64 |
| Fuzz targets | 0 | debt; MP-0.3 introduces first deterministic/fuzz fixtures |

## Package statement coverage

| Package | Coverage | Test time |
|---|---:|---:|
| `sshpilot` | 0.0% | n/a |
| `sshpilot/internal/config` | 62.3% | 0.011s |
| `sshpilot/internal/diagnostics` | 33.5% | 0.128s |
| `sshpilot/internal/loadtest` | 62.7% | 0.412s |
| `sshpilot/internal/log` | 0.0% | n/a |
| `sshpilot/internal/monitoring` | 77.4% | 0.004s |
| `sshpilot/internal/scripts` | 23.8% | 0.005s |
| `sshpilot/internal/ssh` | 35.4% | 0.015s |
| `sshpilot/internal/ui` | 18.7% | 0.011s |
| `sshpilot/internal/ui/components` | 0.0% | n/a |
| `sshpilot/internal/ui/screens` | 45.6% | 0.190s |
| `sshpilot/internal/ui/theme` | n/a | n/a |
| `sshpilot/internal/web` | 0.0% | n/a |
| `sshpilot/internal/web/handlers` | 8.9% | 0.154s |
| `sshpilot/internal/web/ws` | 60.9% | 0.005s |
| `sshpilot/scripts/go/site-admin-tui` | 0.0% | n/a |
| `sshpilot/scripts/go/site-admin-tui/internal/cli` | 0.0% | n/a |
| `sshpilot/scripts/go/site-admin-tui/internal/deploy` | 46.7% | 0.021s |
| `sshpilot/scripts/go/site-admin-tui/internal/domain` | 60.2% | 0.009s |
| `sshpilot/scripts/go/site-admin-tui/internal/nginx` | 53.3% | 0.007s |
| `sshpilot/scripts/go/site-admin-tui/internal/runtime` | 0.0% | n/a |
| `sshpilot/scripts/go/site-admin-tui/internal/state` | 46.9% | 0.009s |
| `sshpilot/scripts/go/site-admin-tui/internal/system` | 9.6% | 0.009s |
| `sshpilot/scripts/go/site-admin-tui/internal/tui` | 12.1% | 0.012s |

## Ratchet semantics

- A baseline is a floor, not a target. Existing debt is made visible without granting permission for new debt.
- Every package with a numeric statement-coverage baseline must remain at or above that value (0.05 percentage-point rounding tolerance).
- Differential mutation testing for changed Go source uses a stronger new-code gate: efficacy ≥80% and mutant coverage ≥70%.
- Full mutation campaigns are compared with module and package baselines; security-critical packages have a progressive target of ≥90% efficacy.
- Race/build/vulnerability regressions are hard failures; they are never ratcheted downward.

## Known baseline debt

- 8 pre-existing Go files are not `gofmt` clean; the CI formatting ratchet blocks new formatting debt without silently expanding MP-0.2 into unrelated rewrites.
- No Go fuzz targets exist yet. MP-0.3 owns deterministic fixtures/fakes and starts the fuzz/property corpus.
- Coverage is especially weak in web handlers, site-admin system/TUI, and SSH behavior; the master plan prioritizes safety-critical boundaries rather than chasing a global percentage.

## Mutation baseline highlights

The first full campaign evaluated 1,600 runnable mutants: 133 were killed and 1,467 lived; another 1,553 mutants were not covered. The result is a deliberately low but objective starting floor, not an acceptance target.

| Package | Efficacy | Mutant coverage | Killed / Lived / Not covered |
|---|---:|---:|---:|
| `sshpilot` | n/a | 0.00% | 0 / 0 / 8 |
| `sshpilot/internal/config` | 15.24% | 75.00% | 16 / 89 / 35 |
| `sshpilot/internal/diagnostics` | 0.00% | 65.66% | 0 / 65 / 34 |
| `sshpilot/internal/loadtest` | 5.71% | 78.95% | 6 / 99 / 28 |
| `sshpilot/internal/log` | n/a | 0.00% | 0 / 0 / 7 |
| `sshpilot/internal/monitoring` | 6.86% | 88.70% | 7 / 95 / 13 |
| `sshpilot/internal/scripts` | 7.55% | 47.75% | 4 / 49 / 58 |
| `sshpilot/internal/ssh` | 17.93% | 43.41% | 26 / 119 / 189 |
| `sshpilot/internal/ui` | 0.00% | 13.16% | 0 / 5 / 33 |
| `sshpilot/internal/ui/components` | 28.57% | 100.00% | 2 / 5 / 0 |
| `sshpilot/internal/ui/screens` | 12.05% | 47.71% | 64 / 467 / 582 |
| `sshpilot/internal/web` | n/a | 0.00% | 0 / 0 / 22 |
| `sshpilot/internal/web/handlers` | 13.64% | 7.36% | 3 / 19 / 277 |
| `sshpilot/internal/web/ws` | 15.15% | 84.62% | 5 / 28 / 6 |
| `sshpilot/scripts/go/site-admin-tui/internal/cli` | n/a | 0.00% | 0 / 0 / 9 |
| `sshpilot/scripts/go/site-admin-tui/internal/deploy` | 0.00% | 60.27% | 0 / 88 / 58 |
| `sshpilot/scripts/go/site-admin-tui/internal/domain` | 0.00% | 99.16% | 0 / 118 / 1 |
| `sshpilot/scripts/go/site-admin-tui/internal/nginx` | 0.00% | 93.75% | 0 / 60 / 4 |
| `sshpilot/scripts/go/site-admin-tui/internal/runtime` | 0.00% | 41.51% | 0 / 22 / 31 |
| `sshpilot/scripts/go/site-admin-tui/internal/state` | 0.00% | 55.06% | 0 / 49 / 40 |
| `sshpilot/scripts/go/site-admin-tui/internal/system` | 0.00% | 73.20% | 0 / 71 / 26 |
| `sshpilot/scripts/go/site-admin-tui/internal/tui` | 0.00% | 17.12% | 0 / 19 / 92 |
