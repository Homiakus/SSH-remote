# SSHPilot — Advanced Engineering Control Loop

## Purpose

This document defines the cyclic mechanism used to implement `docs/MASTER_CODEBASE_PLAN.md`. It turns each plan item into a small, evidence-backed change and constrains automatic repair so that a green pipeline cannot be achieved by weakening tests, security, or correctness.

The loop optimizes for four things simultaneously:

1. semantic correctness;
2. resistance to multidimensional edge cases;
3. quality of the tests themselves;
4. bounded, reversible progress through the master plan.

---

## 1. Core state machine

```text
PLAN_SELECT
    |
    v
CONTRACT_MODEL
    |
    v
TEST_DESIGN -----> EDGE_SPACE_MODEL
    |                    |
    +---------+----------+
              v
          IMPLEMENT
              |
              v
         FAST_GATES
              |
       +------+------+
       |             |
      PASS           FAIL
       |             |
       |        FAILURE_CLASSIFY
       |             |
       |        BOUNDED_AUTOFIX
       |             |
       |        retry <= 3
       |             |
       +-------------+
              |
              v
        PROPERTY/FUZZ
              |
              v
          MUTATION
              |
              v
       SECURITY/PERF
              |
              v
          REVIEW
              |
              v
          COMMIT
              |
              v
      PLAN_CHECKPOINT
```

Every transition is explicit. An item is never `DONE` merely because production code compiles.

---

## 2. Step A — Select one plan item

Read `docs/MASTER_CODEBASE_PLAN.md` and choose the item identified by `NEXT` or the first `READY` item whose dependencies are complete.

Create a working record containing:

```yaml
plan_id: MP-x.y
objective: one observable outcome
allowed_scope:
  - files/packages expected to change
forbidden_scope:
  - unrelated packages/contracts
invariants:
  - behavior that must remain true
acceptance:
  - measurable exit criteria
risk: low|medium|high|critical
```

If the item cannot be expressed as one observable outcome, split it before implementation.

---

## 3. Step B — Build the behavioral contract

Before editing code, identify:

- inputs and outputs;
- state transitions;
- externally observable errors;
- resource ownership;
- cancellation/deadline semantics;
- concurrency expectations;
- persistence/network side effects;
- security boundaries;
- compatibility constraints.

For stateful code, write the legal state graph. Tests should assert impossible transitions fail closed.

Examples:

### SSH connection

```text
NEW -> TCP_CONNECTED -> HOSTKEY_VERIFIED -> AUTHENTICATED -> ACTIVE -> CLOSED
  \         \                 \                \
   \-> FAIL  \-> FAIL          \-> FAIL         \-> FAIL
```

No retry may jump over host-key verification or authentication.

### Vault write

```text
VALID_OLD -> TEMP_WRITTEN -> TEMP_SYNCED -> RENAMED -> VALID_NEW
```

A crash/failure before `RENAMED` must not destroy `VALID_OLD`.

---

## 4. Step C — Model edge cases as a multidimensional space

A flat list hides interactions. Represent the input/environment as dimensions.

Example for SSH connect:

```text
A = auth: password | private-key | encrypted-key | invalid
H = host key: unseen | same | changed | malformed known_hosts
N = network: normal | slow | reset-before-banner | reset-during-handshake | timeout
D = address: hostname | IPv4 | IPv6
C = cancellation: none | before-dial | during-dial | during-handshake | after-auth
R = retry: first-attempt | retryable-failure | non-retryable-failure
```

The conceptual space is:

`S = A × H × N × D × C × R`

Do not brute-force huge spaces blindly. Use the following coverage policy:

### 4.1 Value coverage
Every dimension value appears at least once.

### 4.2 Pairwise
Use pairwise combinations for ordinary interactions.

### 4.3 3-wise
Use 3-wise combinations where defects plausibly require three factors, for example:

`changed host key × retryable network error × reconnect`.

### 4.4 Exhaustive security subspaces
Exhaustively enumerate compact state spaces where one missed combination can violate a trust boundary, for example:

`host-key state × retry path × user decision`.

### 4.5 Boundary lattice
For numeric/range inputs, test:

`min-1, min, min+1, nominal, max-1, max, max+1`.

For sizes also test zero, one, powers-of-two boundaries, chunk-size ±1 and practical maximums.

### 4.6 Sequence boundaries
For byte-stream parsers such as VT100/ANSI, split the same logical input at every possible byte boundary and assert equivalent final state.

### 4.7 Fault surfaces
Inject failures at every external operation that can fail:

- open/read/write/fsync/rename/close;
- dial/handshake/auth/session/read/write/close;
- goroutine/channel send/receive/cancel;
- JSON/YAML decode;
- subprocess start/wait/exit;
- WebSocket read/write/disconnect.

---

## 5. Step D — Design tests before implementation

Tests are selected by the behavioral risk, not by a quota.

### Unit tests
Use for pure logic and local state transitions.

### Contract tests
Use where packages depend on a stable interface or error semantic.

### Integration tests
Use for real protocol/resource boundaries where fakes could mask ordering or lifecycle bugs.

### Property tests
Good properties include:

- parse(render(x)) preserves normalized state;
- encrypt/decrypt round-trip preserves vault data;
- corrupted authenticated ciphertext never decrypts successfully;
- split(stream, any-boundary) yields the same terminal state as unsplit stream;
- cancel eventually terminates all owned goroutines;
- retry count never exceeds policy;
- percentile outputs are monotonic and bounded by min/max sample.

### Fuzz tests
Prioritize:

- config and vault binary decoding;
- known_hosts parsing/input;
- terminal escape streams;
- API payloads;
- remote metric/log parsers;
- path normalization;
- script metadata and generated configuration.

Seed corpora must contain real protocol examples plus malformed/truncated variants.

### Race tests
Treat the race detector as mandatory for changes touching:

- connection/session managers;
- monitoring collectors;
- load-test workers;
- streaming command output;
- shared configuration state;
- WebSockets;
- TUI asynchronous messages.

---

## 6. Step E — Implement the smallest sufficient patch

Rules:

- keep one dominant behavioral purpose;
- preserve public behavior outside acceptance criteria;
- prefer typed errors over parsing error strings;
- prefer injectable clocks/runners/filesystems at hard-to-test boundaries;
- do not add generic abstractions before a second real use case;
- do not hide failures behind retries;
- ensure every goroutine has an owner and termination path;
- ensure every opened resource has a close path;
- use context/deadline semantics for potentially unbounded operations.

---

## 7. Step F — Fast gates

Run in this order:

```sh
# formatting check
files="$(gofmt -l .)"; test -z "$files" || { echo "$files"; exit 1; }

# static checks
go vet ./...

# deterministic suite
go test -count=1 ./...

# concurrency suite
go test -race -count=1 ./...

# build all packages
go build ./...
```

When only one package is under active work, run its focused tests before the full suite for faster diagnosis.

---

## 8. Step G — Failure classification before fixing

Never repair a red gate before classifying it.

Failure classes:

### F1 — Production defect
The new/old implementation violates the intended contract.

Action: fix implementation, preserve the test.

### F2 — Missing contract / ambiguous requirement
Multiple behaviors could be valid.

Action: stop automatic repair; clarify by updating the plan/contract first.

### F3 — Test defect
The test contradicts the explicit contract, has nondeterministic setup, or asserts an implementation detail unnecessarily.

Action: repair the test only with a written reason tied to the contract.

### F4 — Flake/timing/environment
The result varies without a semantic code change.

Action: make synchronization/state deterministic. Do not add arbitrary sleeps as the primary fix.

### F5 — Tooling/infrastructure
Compiler/tool/test harness failure unrelated to behavior.

Action: repair infrastructure without weakening gates.

### F6 — Security-policy conflict
A passing functional fix would weaken trust/crypto/path/auth boundaries.

Action: stop automation. Create/activate an explicit master-plan item with security acceptance criteria.

### F7 — Scope explosion
A local change exposes an architecture issue requiring broad unrelated edits.

Action: checkpoint current evidence, create a dependent plan item, and stop the current patch from sprawling.

---

## 9. Step H — Bounded auto-repair

For F1/F4/F5 and narrowly proven F3 failures, automatic repair is allowed with these constraints:

1. produce the smallest candidate patch;
2. re-run the failing focused test;
3. if green, re-run all dependent gates;
4. increment repair counter;
5. stop after 3 failed attempts for the same failure class.

Each attempt records:

```yaml
attempt: 1
failure_class: F1
hypothesis: "reader goroutine survives cancellation"
changed_files:
  - internal/ssh/client.go
verification:
  - go test ./internal/ssh -run Test...
result: pass|fail
```

The repair loop is not permitted to mutate acceptance criteria.

---

## 10. Step I — Test-of-tests with mutation testing

Coverage answers whether code executed. Mutation testing asks whether the tests notice a semantic defect.

SSHPilot uses Gremlins because it supports differential mutation testing and explicit efficacy/mutant-coverage thresholds. The project pins the tool version in CI because Gremlins is still pre-1.0 and configuration can change between minor releases.

PR command:

```sh
gremlins unleash \
  --diff "origin/$GITHUB_BASE_REF" \
  --coverpkg "./..." \
  --threshold-efficacy 80 \
  --threshold-mcover 70 \
  --output-statuses lctv
```

Metric meanings:

- efficacy = killed / (killed + lived);
- mutant coverage = (killed + lived) / (killed + lived + not-covered).

### Survivor handling

For every `LIVED` mutant:

1. inspect the semantic mutation;
2. identify the missing assertion/invariant;
3. add or strengthen the smallest meaningful test;
4. rerun focused mutation testing;
5. only exclude if the mutation is truly equivalent or a documented tool limitation.

Forbidden responses to a survivor:

- reducing mutation thresholds;
- excluding a whole risky package;
- adding assertions unrelated to the mutant's semantic effect;
- changing production behavior solely to make the mutator easier to kill.

### Ratchet targets

Initial global PR floors:

- efficacy >= 80%;
- mutant coverage >= 70%.

Strategic targets:

- `internal/ssh`: efficacy >= 90%;
- `internal/config`: efficacy >= 90%;
- remote filesystem/command boundaries: efficacy >= 90%;
- general packages: ratchet toward >= 85%.

Do not lower a package's established baseline without an explicit reviewed exception.

---

## 11. Step J — Security and vulnerability gates

For SSH-remote, security gates are first-class because the application stores credentials and executes commands on remote systems.

Run:

```sh
govulncheck ./...
```

Additional manual/automated assertions must ensure:

- host-key mismatch fails closed;
- no insecure host-key callback exists;
- secrets are not emitted in logs/errors;
- file permissions for local secret material are restrictive where the OS supports them;
- remote command/action inputs cannot silently broaden privilege or target;
- path operations have explicit traversal/symlink semantics;
- retry paths do not bypass verification;
- crypto compatibility changes are explicit, reviewed plan items.

A security gate failure is never auto-fixed by widening accepted algorithms, disabling verification, or suppressing the finding.

---

## 12. Step K — Performance and leak gates

Use benchmarks only where sustained hot paths exist. Record both time and allocations.

Candidate benchmark families:

- terminal stream parsing and rendering;
- load-test percentile aggregation;
- monitoring text parsing;
- vault encode/decode;
- large directory listing transforms;
- command stream fan-out/backpressure.

Performance regressions are evaluated relative to an established baseline and task intent; micro-optimizations must not weaken clarity or safety.

For concurrency/resource-heavy code, add repeated stress runs and verify termination after cancellation.

---

## 13. Step L — Cross-platform validation

The supported platform set is:

- windows/amd64;
- windows/arm64;
- linux/amd64;
- linux/arm64;
- darwin/amd64;
- darwin/arm64.

For platform-neutral changes, compile matrix is sufficient. For platform-specific files or behavior, add targeted tests on the corresponding runner where feasible.

---

## 14. Step M — Review the patch as an adversary

Before commit, ask:

- Can a cancellation occur one instruction earlier/later?
- Can the network disconnect between any two protocol phases?
- Can a file be truncated/corrupt/replaced concurrently?
- Can Unicode or zero-length input change semantics?
- Can an attacker force an unbounded allocation, output, retry, goroutine, or worker count?
- Can the same user action happen twice?
- Can a retry bypass a security check?
- Can a partial failure leave persistent state inconsistent?
- Can tests still pass after a plausible semantic mutation?

Any material new scenario is added to the edge-space model and tests before completion.

---

## 15. Step N — Commit and plan checkpoint

A successful atomic item updates the master plan with:

```yaml
plan_id: MP-x.y
status: DONE
commit: <sha>
evidence:
  tests: <commands/results>
  race: pass
  mutation:
    efficacy: <value>
    mutant_coverage: <value>
  fuzz: <targets/corpus or N/A>
  security: <result or N/A>
  performance: <result or N/A>
edge_dimensions:
  - <dimension/value summary>
followups:
  - MP-x.z
```

Then move `NEXT` to the next dependency-ready item.

The implementation loop must never infer `DONE` from a commit existing. Only evidence closes work.

---

## 16. CI topology

The repository uses two feedback layers:

### Fast quality gate
Runs formatting, vet, deterministic tests, race tests, vulnerability analysis and build checks. It should fail quickly and provide diagnostic output.

### Mutation gate
Runs on PRs that modify Go source/module files. It uses Gremlins diff mode against the PR base, so only mutants in changed code are evaluated. Full-module mutation campaigns may be scheduled/manual because they are much more expensive.

This keeps the common loop fast while preserving test-of-tests pressure on every behavioral Go change.

---

## 17. Definition of done

A code task is done only when all relevant statements are true:

- the selected master-plan item is satisfied;
- behavior is explicitly specified;
- tests fail for the intended defect before the fix when practical;
- multidimensional edge cases are represented deliberately;
- deterministic tests pass;
- race detector passes where relevant;
- fuzz/property corpus replays pass where relevant;
- mutation gate meets the required threshold;
- security checks pass;
- build/platform requirements pass;
- no gate was weakened to achieve green;
- auto-repair stayed inside its budget and scope;
- evidence is written back to the master plan.

That is the complete control loop.