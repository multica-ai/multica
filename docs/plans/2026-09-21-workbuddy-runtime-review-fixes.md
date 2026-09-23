# WorkBuddy Runtime Review Fixes Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Close the remaining reviewer concerns for PR #7934 so WorkBuddy's bundled CodeBuddy CLI can recover across app updates and expose the same model discovery, launch-prefix, and thinking capabilities as CodeBuddy.

**Architecture:** Keep `workbuddy` as a distinct built-in runtime identity backed by the existing `codebuddy` protocol family. Treat the WorkBuddy Node interpreter and CLI script as one validated launch pair, re-probe that pair during daemon refresh/self-heal, and use descriptor/family normalization to reuse CodeBuddy capabilities without duplicating protocol behavior.

**Tech Stack:** Go, daemon runtime discovery, built-in runtime descriptors, model discovery subprocesses, thinking-capability catalogues, Go unit/integration tests.

---

## Scope and non-goals

This plan addresses reviewer items 3 and 4 directly and preserves the already-landed fixes for items 1 and 2:

- Keep the corrected macOS path under `Contents/Resources/...`.
- Keep semantic-version selection based on the segment following `versions`.
- Do not change the public runtime identity from `workbuddy` to `codebuddy` in this PR.
- Do not mutate the daemon process `PATH`.
- Do not silently fall back from a WorkBuddy bundle to an unrelated PATH-installed CLI when a bundle was explicitly configured through `MULTICA_WORKBUDDY_PATH`.
- Product decision (September 21, 2026): WorkBuddy and CodeBuddy intentionally share the embedded CLI. This plan does not add WorkBuddy-specific configuration directories, authentication paths, account isolation, or duplicate embedded-CLI settings.

The implementation must preserve existing behavior for `codebuddy`, custom runtime profiles, and other built-in runtimes.

## Execution safety gate

The product decision above confirms shared embedded-CLI configuration; it does not authorize new permission behavior. The current `codebuddy` backend unconditionally adds `--permission-mode bypassPermissions`, blocks several interactive tools, and auto-approves permission bridge requests. Because WorkBuddy reuses that backend through `ProtocolFamily`, this policy is inherited implicitly. This plan does not add, broaden, or validate that bypass behavior.

Until the policy is explicitly confirmed and reviewed, execution work is limited to defensive discovery plumbing, catalog correctness, launch-pair recovery, and tests that use fake commands without running real agents. If the policy is not approved, the implementation must fail closed or introduce an explicit identity-level policy instead of silently inheriting CodeBuddy's high-privilege behavior.

## Reviewer issue mapping

| Reviewer item | Planned resolution | Main files |
|---|---|---|
| WorkBuddy update leaves deleted Node pinned | Re-probe and atomically adopt a fresh `{Path, LaunchPrefix}` pair during refresh/self-heal; add upgrade regression tests | `server/internal/daemon/workbuddy_probe.go`, `server/internal/daemon/daemon.go`, `server/internal/daemon/agents_refresh.go` |
| WorkBuddy has no model discovery | First preserve `Catalog.Fallback` through the built-in descriptor interface, then reuse CodeBuddy discovery against the resolved WorkBuddy command | `server/pkg/agent/builtin_runtimes.go`, `server/pkg/agent/models.go` |
| Model discovery drops `LaunchPrefix` | Pass the resolved entry's filtered `LaunchPrefix` into `agent.NewCommand` | `server/internal/daemon/daemon.go` |
| WorkBuddy lacks thinking capability | Normalize built-in identity to `ProtocolFamily` before capability lookup | `server/pkg/agent/thinking.go`, related tests |

## Task 1: Establish the failing upgrade-recovery test

**Files:**

- Modify: `server/internal/daemon/workbuddy_probe_test.go`
- Inspect/test helpers: `server/internal/daemon/daemon_test.go`, `server/internal/daemon/agents_refresh_test.go` (or the closest existing refresh test file)

### Steps

1. Identify the existing test fixture helpers for WorkBuddy staged Node directories, daemon agent entries, and temporary home/config roots.
2. Add a test that stages version `22.9.0`, probes/registers it, then removes that version and stages `22.10.0` without restarting the daemon.
3. Trigger the same refresh/re-resolution path used by the daemon rather than calling only `resolveWorkBuddyNode` directly.
4. Assert that the resulting entry contains:
   - `Path` pointing to `22.10.0`;
   - `LaunchPrefix` pointing to the same WorkBuddy CLI script;
   - a valid version detected by invoking the pair, not by invoking Node alone.
5. Run the focused test and confirm it fails because the current empty `Command` entry is retained or treated as unrecoverable.

Expected failure: the old Node path remains pinned or the launch fails after the staged version is replaced.

## Task 2: Implement atomic WorkBuddy pair re-probing

**Files:**

- Modify: `server/internal/daemon/workbuddy_probe.go`
- Modify: `server/internal/daemon/daemon.go`
- Modify: `server/internal/daemon/agents_refresh.go` only if the existing refresh loop does not invoke provider-specific re-probing
- Test: the failing upgrade-recovery test from Task 1

### Design requirements

1. Add a WorkBuddy-specific resolver that returns a complete candidate:

   ```text
   { Path: <node>, LaunchPrefix: [<cli script>], Model: <model> }
   ```

   The resolver must validate the pair together before adoption.

2. Validation must execute the same command shape used in production, at minimum:

   ```text
   <node> <cli script> --version
   ```

   This prevents adopting a newly discovered Node binary with a missing, stale, or incompatible CLI script.

3. Re-probe only when the current WorkBuddy pair is missing, fails version detection, or is known to be a bundle-managed entry. Do not change the self-heal semantics for ordinary PATH-installed providers.

4. Adoption must be atomic from the daemon's perspective: do not update `Path` first and `LaunchPrefix` later. Replace the whole `AgentEntry` only after both values have been validated.

5. Preserve explicit override behavior. If `MULTICA_WORKBUDDY_PATH` is configured, a missing override remains a hard miss and must not silently select another bundle.

6. Keep the existing semantic-version ordering and platform-specific bundle paths unchanged.

### Steps

1. Refactor the WorkBuddy probe/resolution code so candidate discovery and candidate validation are separate, testable functions.
2. Add a daemon-level hook or provider-specific branch in `resolveAgentEntryWithHeal` that invokes the WorkBuddy resolver when the pinned pair is stale or invalid.
3. Store the successfully validated pair in the same resolved-entry state used by subsequent launches and version checks.
4. Run the Task 1 test and confirm it passes.
5. Add negative tests for:
   - new Node exists but the CLI script is missing;
   - CLI `--version` fails under the candidate Node;
   - explicit override path is missing;
   - unrelated PATH Node must not replace an explicitly configured bundle.

## Task 3: Add WorkBuddy model discovery through the descriptor

**Files:**

- Modify: `server/pkg/agent/builtin_runtimes.go`
- Modify: `server/pkg/agent/models.go` only if a reusable CodeBuddy discovery function is not already exposed
- Inspect: `server/pkg/agent/codebuddy.go` and existing CodeBuddy model-discovery tests
- Test: `server/pkg/agent/*test.go` for built-in runtime descriptors and model discovery

### Design requirements

1. First resolve the interface mismatch: the current `ModelDiscoveryFunc` returns `([]Model, error)` while CodeBuddy discovery returns a `Catalog` with a meaningful `Fallback` flag. Prefer extending the descriptor callback to return `Catalog` (or introduce a clearly named catalog-preserving callback) rather than adapting away `Fallback`.
2. Add the catalog-preserving discovery strategy to the `workbuddy` descriptor.
3. Reuse the existing CodeBuddy discovery implementation where possible; do not copy its protocol parsing logic.
4. The discovery function must receive the already-resolved runtime command, including its launch prefix.
5. `ListModels("workbuddy", ...)` must return the same catalog shape and fallback behavior as CodeBuddy. Keep runtime identity in `providerType`, cache keys, logs, and error labels; keep each model's vendor `Provider` metadata according to CodeBuddy's existing inference rules.

### Steps

1. Write a failing test asserting that the WorkBuddy descriptor has a non-nil catalog-preserving model-discovery strategy.
2. Write a subprocess-fixture test asserting that discovery receives the command as:

   ```text
   <node> <cli script> --acp
   ```

   or the exact model-list protocol invocation already used by CodeBuddy.

3. Assign the shared CodeBuddy discovery function or a catalog-preserving adapter to `workbuddy`.
4. Assert successful discovery has `Fallback == false`.
5. Assert discovery failure returns the same static model IDs, labels, vendor providers, and thinking metadata as CodeBuddy with `Fallback == true`.
6. Assert fallback results are not inserted into the model cache and a later call retries discovery.
7. Run the focused agent package tests.

## Task 4: Preserve `LaunchPrefix` in daemon model-list handling

**Files:**

- Modify: `server/internal/daemon/daemon.go` around `handleModelList`
- Test: `server/internal/daemon/model_qualify_test.go` or a new focused model-list test file

### Steps

1. Add a failing daemon test with an agent entry shaped like:

   ```text
   Path = /tmp/node
   LaunchPrefix = [/tmp/codebuddy]
   ```

   Capture the command passed to the model-list function.
2. Assert that the command contains the filtered entry prefix, not only the executable path.
3. In the built-in provider branch, set:

   ```go
   fixedArgs = agent.FilterLaunchPrefix(rt.Provider, entry.LaunchPrefix, d.logger)
   ```

   before constructing `agent.NewCommand`.
4. Ensure custom runtime profiles keep their current fixed-argument behavior and are not overwritten by the built-in branch.
5. Add an assertion that protocol-critical flags are still filtered according to the existing `FilterLaunchPrefix` rules.
6. Run the focused daemon test and then the full daemon package tests.

All discovery tests must clear or override `MULTICA_WORKBUDDY_PATH`, related runtime environment variables, and PATH lookups; use only test-created fake executables; and isolate discovery-cache keys or clear the cache between cases. No default test may resolve or execute a developer's installed WorkBuddy, Node, or CodeBuddy binary.

## Task 5: Normalize WorkBuddy thinking capabilities to CodeBuddy

**Files:**

- Modify: `server/pkg/agent/thinking.go`
- Add/modify: `server/pkg/agent/thinking_test.go`
- Inspect: `server/pkg/agent/builtin_runtimes.go` for the canonical `ProtocolFamily` field

### Design requirements

1. Introduce one shared normalization helper, for example:

   ```go
   func thinkingCapabilityProvider(providerType string) string
   ```

   It should resolve a built-in runtime identity through `BuiltinRuntimeByID` and return its `ProtocolFamily`; unknown providers remain unchanged.

2. Apply the helper consistently to:

   - `ThinkingControlSupported`;
   - `IsKnownThinkingValue`;
   - `UsesACPCatalogThinking` or any related lookup that is intended to operate at protocol-family level.

3. Avoid normalizing custom runtime IDs that are not declared built-in identities.

### Steps

1. Add failing tests for:

   - `ThinkingControlSupported("workbuddy") == ThinkingControlSupported("codebuddy")`;
   - `IsKnownThinkingValue("workbuddy", "high") == IsKnownThinkingValue("codebuddy", "high")`;
   - empty thinking value remains accepted;
   - unrelated providers remain unchanged.

2. Implement the helper and route all relevant lookups through it.
3. Run the thinking and ACP-effort tests.
4. Review any user-facing provider labels to ensure normalization affects capability lookup only, not error messages or runtime identity display.

## Task 6: Add end-to-end WorkBuddy launch-shape coverage

**Files:**

- Modify: `server/internal/daemon/workbuddy_probe_test.go`
- Modify/add: `server/internal/daemon/daemon_test.go`
- Modify/add: `server/pkg/agent/launch_test.go` only if a lower-level launch assertion is needed
 - Add: `server/pkg/agent/workbuddy_launch_test.go` (task-launch fixture that re-executes the test binary as the bundled Node interpreter)

### Required scenario

Use a fake Node executable and fake CodeBuddy script that record argv, then verify version detection and model discovery. Task-launch argv assertions remain gated on the explicit permission-policy decision described above; do not run real-agent smoke tests.

1. Version detection invokes `<node> <script> --version`.
2. Model discovery invokes `<node> <script>` plus the expected discovery/protocol flags.
3. `TestWorkBuddyTaskLaunchKeepsBundledCliPrefix` (server/pkg/agent/workbuddy_launch_test.go) asserts that a completed task reaches the OS as `<node> <cli script> -p --output-format stream-json ...`. The "node" is the re-executed test binary, dispatched from the package TestMain like the existing CLI fixtures, so the real os/exec boundary is covered on Windows and macOS alike. No real agent CLI, account, or permission decision is exercised: WorkBuddy inheriting CodeBuddy's headless permission policy is the recorded product decision above, not a behavior this fixture grants.

The test must also verify that a newly staged Node version is adopted after the old version disappears without daemon restart.

### Steps

1. Create or reuse the repository's existing cross-platform subprocess test helper.
2. Record argv in a temporary file or in-process test channel.
3. Assert exact ordering: interpreter first, CLI script second, protocol/runtime flags after the prefix.
4. Run the focused integration tests on the current platform.
5. Keep platform-specific path assertions separate from command-shape assertions so Windows and macOS path regressions remain diagnosable.

## Task 7: Run validation and prepare the re-review handoff

**Files:**

- No production changes; update PR description/review comment as needed.

### Validation commands

Run from `D:\kfz\multica\multica\.wt-main\server` with the repository's Go toolchain and an available module cache:

```powershell
go test ./internal/daemon -run 'Work[Bb]uddy|ModelList|LaunchPrefix|AgentEntry' -count=1
go test ./pkg/agent -run 'Work[Bb]uddy|Model|Thinking|LaunchPrefix|Codebuddy' -count=1
go test ./internal/daemon/... -count=1
go test ./pkg/agent/... -count=1
go vet ./internal/daemon/... ./pkg/agent/...
git diff --check
```

If dependency downloads fail, record that as an environment limitation and rerun with the approved module cache/network setup; do not treat an incomplete test run as validation.

### Re-review checklist

- [x] Reviewer item 3 has a daemon-level upgrade recovery test that passes without restart.
- [x] WorkBuddy Node + CLI pair is validated and adopted atomically.
- [x] `ListModels("workbuddy", ...)` returns a real catalog or the same documented fallback as CodeBuddy.
- [x] Model discovery preserves `LaunchPrefix`.
- [x] WorkBuddy thinking capability matches CodeBuddy for supported values.
- [x] Task launch reaches a real subprocess as `<node> <cli script> -p ...` (reviewer item 4).
- [x] Existing CodeBuddy, custom profile, and launch-prefix tests remain green. The `pkg/agent` package still reports its pre-existing Windows-only POSIX-fixture failures, byte-for-byte unchanged with and without these changes.
- [ ] PR description lists the four reviewer items and links each to a commit/test.
- [ ] Request a new review from `multica-eve`; do not rely on the old `CHANGES_REQUESTED` state disappearing automatically.

## Suggested commit sequence

1. `test(daemon): cover WorkBuddy runtime recovery after staged Node replacement`
2. `fix(daemon): re-probe and atomically adopt WorkBuddy runtime pairs`
3. `test(agent): cover WorkBuddy model discovery and launch prefix`
4. `fix(agent): inherit CodeBuddy model discovery for WorkBuddy`
5. `fix(daemon): preserve built-in launch prefixes during model discovery`
6. `fix(agent): normalize WorkBuddy thinking capabilities to CodeBuddy`
7. `test(agent): cover WorkBuddy end-to-end launch shape`
