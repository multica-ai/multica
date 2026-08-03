# Workspace Storage Lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bound the daemon's per-task Codex cache growth and stop Spotlight from indexing daemon workspace roots without deleting resumable sessions, logs, source work, or user output.

**Architecture:** Keep the existing whole-root lifecycle GC and add `codex-home/.tmp` to the exact daemon-owned artifact catalog. Reclaim those exact artifacts in a `runTask` defer for Codex after the process exits and before `handleTask` can send a terminal callback; use the existing matcher so containment and symlink protections stay shared. Independently create a root-level `.metadata_never_index` marker during both fresh preparation and reuse.

**Tech Stack:** Go, `os`/`path/filepath`, the daemon `execenv` package, existing GC artifact matcher, Go table tests.

---

## File structure

- Create `server/internal/daemon/execenv/spotlight.go`: owns the root-level Spotlight marker name and idempotent creation helper.
- Create `server/internal/daemon/execenv/spotlight_test.go`: unit tests marker creation plus Prepare/Reuse self-healing.
- Modify `server/internal/daemon/execenv/execenv.go`: call the Spotlight helper on both environment entry paths, logging failures as non-fatal.
- Modify `server/internal/daemon/execenv/reclaimable.go`: add the exact `codex-home/.tmp` path to the daemon-managed artifact catalog.
- Modify `server/internal/daemon/gc.go`: add the provider-gated immediate cleanup wrapper used by `runTask`.
- Modify `server/internal/daemon/daemon.go`: install the cleanup defer immediately after environment preparation/reuse succeeds.
- Modify `server/internal/daemon/gc_test.go`: prove `.tmp` cleanup, preservation of durable Codex state, provider gating, and symlink safety.
- Modify `server/internal/daemon/diskusage_test.go`: prove disk usage reports both exact managed paths and deduplicates basename matches.
- Modify `CLI_AND_DAEMON.md`: document immediate Codex cache reclamation, legacy GC behavior, exact paths, and Spotlight exclusion.

### Task 1: Add the Spotlight exclusion marker

**Files:**

- Create: `server/internal/daemon/execenv/spotlight.go`
- Create: `server/internal/daemon/execenv/spotlight_test.go`
- Modify: `server/internal/daemon/execenv/execenv.go:251-280,465-485`

- [x] **Step 1: Write failing marker and lifecycle tests**

Create tests that exercise the public behavior rather than macOS indexing internals:

```go
func TestEnsureSpotlightExclusion(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces")
	if err := EnsureSpotlightExclusion(root); err != nil {
		t.Fatalf("EnsureSpotlightExclusion: %v", err)
	}
	marker := filepath.Join(root, SpotlightExclusionMarker)
	info, err := os.Stat(marker)
	if err != nil {
		t.Fatalf("stat marker: %v", err)
	}
	if info.IsDir() || info.Size() != 0 {
		t.Fatalf("marker info = dir:%v size:%d, want empty file", info.IsDir(), info.Size())
	}
	if err := EnsureSpotlightExclusion(root); err != nil {
		t.Fatalf("idempotent EnsureSpotlightExclusion: %v", err)
	}
}

func TestPrepare_WritesSpotlightExclusion(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID: "ws-spotlight-prepare",
		TaskID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		Task: TaskContextForEnv{IssueID: "issue-spotlight-prepare"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)
	if _, err := os.Stat(filepath.Join(root, SpotlightExclusionMarker)); err != nil {
		t.Fatalf("Prepare did not create Spotlight marker: %v", err)
	}
}

func TestReuse_SelfHealsSpotlightExclusion(t *testing.T) {
	root := t.TempDir()
	env, err := Prepare(PrepareParams{
		WorkspacesRoot: root,
		WorkspaceID: "ws-spotlight-reuse",
		TaskID: "bbbbbbbb-cccc-dddd-eeee-ffffffffffff",
		Task: TaskContextForEnv{IssueID: "issue-spotlight-reuse"},
	}, testLogger())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	defer env.Cleanup(true)
	marker := filepath.Join(root, SpotlightExclusionMarker)
	if err := os.Remove(marker); err != nil {
		t.Fatalf("remove marker: %v", err)
	}
	if got := Reuse(ReuseParams{WorkspacesRoot: root, WorkDir: env.WorkDir, Task: TaskContextForEnv{IssueID: "issue-spotlight-reuse"}}, testLogger()); got == nil {
		t.Fatal("Reuse returned nil")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("Reuse did not restore Spotlight marker: %v", err)
	}
}
```

- [x] **Step 2: Run the tests and verify the expected compile failure**

Run:

```bash
cd server
go test ./internal/daemon/execenv -run 'Test(EnsureSpotlightExclusion|Prepare_WritesSpotlightExclusion|Reuse_SelfHealsSpotlightExclusion)$'
```

Expected: FAIL because `EnsureSpotlightExclusion` and `SpotlightExclusionMarker` do not exist.

- [x] **Step 3: Implement the minimal marker helper**

Create `spotlight.go`:

```go
package execenv

import (
	"fmt"
	"os"
	"path/filepath"
)

const SpotlightExclusionMarker = ".metadata_never_index"

func EnsureSpotlightExclusion(workspacesRoot string) error {
	if workspacesRoot == "" {
		return fmt.Errorf("execenv: workspaces root is required")
	}
	if err := os.MkdirAll(workspacesRoot, 0o755); err != nil {
		return fmt.Errorf("create workspaces root for Spotlight marker: %w", err)
	}
	if err := os.WriteFile(filepath.Join(workspacesRoot, SpotlightExclusionMarker), nil, 0o644); err != nil {
		return fmt.Errorf("write Spotlight exclusion marker: %w", err)
	}
	return nil
}
```

Call it after `EnsureWorkspacesRootMarker` in both `Prepare` and non-empty-root `Reuse`:

```go
if err := EnsureSpotlightExclusion(params.WorkspacesRoot); err != nil && logger != nil {
	logger.Warn("execenv: Spotlight exclusion marker not written", "error", err)
}
```

- [x] **Step 4: Run the focused tests and package tests**

Run:

```bash
cd server
go test ./internal/daemon/execenv -run 'Test(EnsureSpotlightExclusion|Prepare_WritesSpotlightExclusion|Reuse_SelfHealsSpotlightExclusion)$'
go test ./internal/daemon/execenv
```

Expected: PASS.

- [x] **Step 5: Commit the Spotlight unit**

```bash
git add server/internal/daemon/execenv/spotlight.go server/internal/daemon/execenv/spotlight_test.go server/internal/daemon/execenv/execenv.go
git commit -m "feat(daemon): exclude workspace roots from Spotlight"
```

### Task 2: Catalog and reclaim the Codex temporary cache

**Files:**

- Modify: `server/internal/daemon/execenv/reclaimable.go`
- Modify: `server/internal/daemon/gc_test.go:446-508,870-945`
- Modify: `server/internal/daemon/diskusage_test.go:207-242,407-454`

- [x] **Step 1: Extend GC and disk-usage tests to require `.tmp`**

In `TestGCWorkspace_ReclaimsLegacyCodexSandboxWithoutConfiguredPatterns`, seed `codex-home/.tmp/plugin/cache.bin`, expect both managed directories removed, and update stats to two directories and combined bytes. In `TestCleanTaskArtifacts_ManagedPathIsExactAndDeduplicated`, seed both exact Codex paths plus same-named directories under `workdir/repo`, then require only the exact paths to be removed unless the operator supplies the broad basename. Extend the symlink table with `codex-home/.tmp`.

Update `TestScanDiskUsage_ManagedCodexSandboxIsExactAndDeduplicated` to seed 200 bytes under `.tmp`, expect 500 managed bytes, and require:

```go
wantManaged := "codex-home/.sandbox-bin,codex-home/.tmp"
if got := strings.Join(report.ManagedArtifactSubpaths, ","); got != wantManaged {
	// fail
}
```

Apply the same managed-path expectation to `TestScanDiskUsageRoots_SumsAcrossRoots`.

- [x] **Step 2: Run focused tests and verify failures**

Run:

```bash
cd server
go test ./internal/daemon -run 'Test(GCWorkspace_ReclaimsLegacyCodexSandboxWithoutConfiguredPatterns|CleanTaskArtifacts_ManagedPath|ScanDiskUsage_ManagedCodexSandboxIsExactAndDeduplicated|ScanDiskUsageRoots_SumsAcrossRoots)$'
```

Expected: FAIL because `.tmp` is not in `ManagedReclaimableArtifactSubpaths`.

- [x] **Step 3: Add `.tmp` to the exact managed catalog**

Change `reclaimable.go` to:

```go
const (
	codexHomeDirName       = "codex-home"
	codexSandboxBinDirName = ".sandbox-bin"
	codexTempDirName       = ".tmp"
)

func ManagedReclaimableArtifactSubpaths() []string {
	return []string{
		filepath.Join(codexHomeDirName, codexSandboxBinDirName),
		filepath.Join(codexHomeDirName, codexTempDirName),
	}
}
```

- [x] **Step 4: Run focused and package tests**

Run:

```bash
cd server
go test ./internal/daemon -run 'Test(GCWorkspace_ReclaimsLegacyCodexSandboxWithoutConfiguredPatterns|CleanTaskArtifacts_ManagedPath|ScanDiskUsage_ManagedCodexSandboxIsExactAndDeduplicated|ScanDiskUsageRoots_SumsAcrossRoots)$'
go test ./internal/daemon
```

Expected: PASS, including preservation of auth, config, sessions, logs, output, and symlink targets.

- [x] **Step 5: Commit the artifact catalog unit**

```bash
git add server/internal/daemon/execenv/reclaimable.go server/internal/daemon/gc_test.go server/internal/daemon/diskusage_test.go
git commit -m "feat(daemon): manage Codex temporary caches"
```

### Task 3: Reclaim managed Codex artifacts at process exit

**Files:**

- Modify: `server/internal/daemon/gc.go:636-646`
- Modify: `server/internal/daemon/daemon.go:4990-5040`
- Modify: `server/internal/daemon/gc_test.go`
- Modify: `server/internal/daemon/workdir_race_test.go`

- [x] **Step 1: Write failing provider-gating and runTask-return tests**

Add a helper test in `gc_test.go` that creates both managed paths and calls the desired wrapper:

```go
func TestReclaimTaskExitArtifacts_OnlyCodex(t *testing.T) {
	tests := []struct {
		provider string
		wantGone bool
	}{{"codex", true}, {"claude", false}}
	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			taskDir := t.TempDir()
			writeFile(t, filepath.Join(taskDir, "codex-home/.tmp/cache.bin"), 10)
			d := newGCTestDaemon(t, http.NewServeMux())
			d.reclaimTaskExitArtifacts(tc.provider, taskDir, slog.Default())
			_, err := os.Stat(filepath.Join(taskDir, "codex-home/.tmp"))
			if tc.wantGone != os.IsNotExist(err) {
				t.Fatalf("stat err=%v, wantGone=%v", err, tc.wantGone)
			}
		})
	}
}
```

Add a POSIX `runTask` regression fixture in `workdir_race_test.go` using a fake Codex app-server script. The script creates `$CODEX_HOME/.tmp/plugin/cache.bin` before emitting either a terminal success response or exiting non-zero. Invoke `runTask` and assert the cache is absent when it returns for both subtests. Also create `codex-home/sessions/keep.jsonl` and `logs/keep.log` from the script and require they remain, proving that task-exit cleanup is narrow. A Claude control subtest should leave a same-shaped directory untouched.

- [x] **Step 2: Run focused tests and verify the expected failures**

Run:

```bash
cd server
go test ./internal/daemon -run 'Test(ReclaimTaskExitArtifacts_OnlyCodex|RunTask_ReclaimsCodexArtifactsBeforeReturn)$'
```

Expected: FAIL because `reclaimTaskExitArtifacts` is undefined and `runTask` does not install cleanup.

- [x] **Step 3: Implement provider-gated best-effort cleanup**

Add next to `cleanManagedTaskArtifacts`:

```go
func (d *Daemon) reclaimTaskExitArtifacts(provider, taskDir string, logger *slog.Logger) {
	if provider != "codex" || taskDir == "" {
		return
	}
	removed, bytes, _ := d.cleanManagedTaskArtifacts(taskDir)
	if removed > 0 && logger != nil {
		logger.Info("reclaimed managed task-exit artifacts", "directories", removed, "bytes", bytes)
	}
}
```

Immediately after `env` is known to be non-nil in `runTask`, before any subsequent early return, install:

```go
defer d.reclaimTaskExitArtifacts(provider, env.RootDir, taskLog)
```

Because Go evaluates the defer arguments immediately and executes it before `runTask` returns, every post-prepare success, failure, timeout, and cancellation path cleans the cache before `handleTask` can call `reportTaskResult`. Preparation failures do not run cleanup because no usable environment/process exists.

- [x] **Step 4: Run focused tests and package tests**

Run:

```bash
cd server
go test ./internal/daemon -run 'Test(ReclaimTaskExitArtifacts_OnlyCodex|RunTask_ReclaimsCodexArtifactsBeforeReturn)$'
go test ./internal/daemon
```

Expected: PASS.

- [x] **Step 5: Commit the task-exit lifecycle unit**

```bash
git add server/internal/daemon/gc.go server/internal/daemon/daemon.go server/internal/daemon/gc_test.go server/internal/daemon/workdir_race_test.go
git commit -m "feat(daemon): reclaim Codex caches at task exit"
```

### Task 4: Document and verify the complete lifecycle

**Files:**

- Modify: `CLI_AND_DAEMON.md:183-200`
- Modify: `docs/superpowers/plans/2026-08-02-workspace-storage-lifecycle.md`

- [x] **Step 1: Update operator documentation**

Document these exact contracts:

```text
- Codex task processes retain isolated `.tmp` caches while running and the daemon
  removes `codex-home/.tmp` plus `codex-home/.sandbox-bin` before reporting the
  task terminal.
- Artifact GC also reclaims legacy copies of both exact paths after the artifact
  TTL; it does not broaden matching to repositories unless configured.
- The daemon creates `.metadata_never_index` at the workspaces root and repairs
  it on Prepare/Reuse to prevent Spotlight indexing on macOS.
- Sessions, auth/config, workdir, output, logs, and user-created data are retained
  under the existing issue/orphan lifecycle.
```

- [x] **Step 2: Format and run focused verification**

Run:

```bash
gofmt -w server/internal/daemon/execenv/spotlight.go server/internal/daemon/execenv/spotlight_test.go server/internal/daemon/execenv/execenv.go server/internal/daemon/execenv/reclaimable.go server/internal/daemon/gc.go server/internal/daemon/daemon.go server/internal/daemon/gc_test.go server/internal/daemon/diskusage_test.go server/internal/daemon/workdir_race_test.go
cd server
go test ./internal/daemon/execenv ./internal/daemon
```

Expected: PASS.

- [x] **Step 3: Run repository checks and inspect the diff**

Run:

```bash
git diff --check
git status --short
git diff --stat
```

Expected: no whitespace errors; only the design, plan, code, tests, and daemon documentation for this issue are changed.

- [x] **Step 4: Commit documentation and plan state**

```bash
git add CLI_AND_DAEMON.md docs/superpowers/plans/2026-08-02-workspace-storage-lifecycle.md
git commit -m "docs: explain bounded workspace storage lifecycle"
```

- [x] **Step 5: Push and open the linked pull request**

```bash
git push -u origin agent/cto-codex/3962c1ca-1785685966
gh pr create --title "WS-2867: bound daemon workspace cache growth" --body-file ./pr-body.md
```

The PR body must include `Closes WS-2867`, a concise summary, the two Go package test command, and the `c37d6ffd` outlier note explaining that its live `data/runs` application retention remains intentionally untouched.
