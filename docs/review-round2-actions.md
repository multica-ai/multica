# PR #6675 Dim ACP Runtime — Review Round 2 Action Plan

Source: multica-eve review on 2026-08-13 (CHANGES_REQUESTED), commit `966bb7ed5`.

## Resolved last round (confirmed by reviewer)

- [x] Runtime-brief delivery via AGENTS.md
- [x] Model discovery cache keyed by executable path
- [x] Deploy scripts + docs/design-kb removed from history
- [x] Formatting clean (gofmt + diff --check)
- [x] Direct-child early-error deadlock addressed
- [x] Normal success path sends bounded session/close

## Remaining blockers (7)

### 1. Cross-run continuity still unreliable

- **Problem**: `dim_integration_test.go:170-184` sleeps 8s before run B — does not
  cover an immediate continuation. dim.go:281-287 acknowledges pre-0.3.10 returns
  "held by another process" but discovery accepts every version and that error is
  not treated as recoverable.
- **Fix**:
  - [ ] Require and validate Dim >= 0.3.10 (version check at discovery/launch)
  - [ ] Make immediately adjacent continuation reliable (no sleep in test)
  - [ ] If a lock-release window still exists, handle with bounded retry, not
        silent fresh session
- **Test**: two-run regression where run B starts immediately after run A completes

### 2. Cleanup kills only direct child, not process tree

- **Problem**: cleanup at dim.go:220-245 kills the launched process but leaves
  descendants alive. The hanging-child test leaves a PPID=1 `sleep 60` orphan.
- **Fix**:
  - [ ] Start and terminate an owned process group/tree
  - [ ] Make the regression assert the descendant PID is gone before Result completes
- **Test**: hanging-child test verifies no orphan descendants

### 3. Late-notification race not covered or fixed

- **Problem**: `hermesClient.handleResponse` calls `extractPromptResult ->
  onPromptDone` for every valid session/prompt response regardless of stopReason
  (hermes.go:1214-1247). The select in dim.go:445-464 consumes an already-ready
  promptDone, not waiting for a quiet window. The `DIM_PROMPT_NO_STOP` fixture
  never sends a delayed final notification.
- **Fix**:
  - [ ] Restore activity-based notification quiescence (not just select on promptDone)
  - [ ] Add a Dim equivalent of `TestHermesBackendDrainsLateFinalNotificationAfterPromptResponse`
- **Test**: delayed final notification after prompt response is not lost

### 4. Permission setup not fail-closed across resumed failed sessions

- **Problem**: Errors setting permission/mode/model return before session/close
  (dim.go:392-417) while still returning a session ID. Platform can resume from
  failed tasks. Resumed sessions skip permission/mode setup (dim.go:372-400).
  A failed first config can be resumed without full-access.
- **Fix**:
  - [ ] Reapply or verify permission/mode on resume (don't blindly skip)
  - [ ] Close partially configured sessions where possible
  - [ ] Add `first setup fails -> next run resumes` regression
- **Test**: failed first config → resume re-establishes full-access

### 5. Migration prefix conflicts with main

- **Problem**: Branch adds `273_runtime_profile_add_dim`, but main already has
  `273_agent_task_queue_runtime_id_index`. Test fails.
- **Fix**:
  - [ ] Rename migration to next unique prefix
  - [ ] Update stale pre-255 down-migration comment
- **Verify**: `go test ./internal/migrations -run TestMigrationNumericPrefixesStayUnique`

### 6. Previously requested validation still incomplete

- **Fix**:
  - [ ] Add automated Dim runtime-brief mapping regression
  - [ ] Add two-executable model-cache call-site regression
  - [ ] Add unit assertion for session/close
  - [ ] Authorized real smoke must execute a harmless command AND write/check
        a sentinel file
  - [ ] Remove unrelated blank-line-only change in manage-views-dialog.tsx

### 7. Documentation counts and runtime lists inconsistent

- **Problem**: 22 built-in CLIs including Oh-My-Pi and Dim, but README.md:15,
  :55, :215 still disagree. CLI_AND_DAEMON.md:304 omits Dim from ACP
  runtime / session/load list.
- **Fix**:
  - [ ] Update README.md counts and lists (lines 15, 55, 215)
  - [ ] Add Dim to CLI_AND_DAEMON.md:304 ACP runtime / session/load list

## Order of execution

1. #5 migration prefix (blocking CI, fastest)
2. #2 process tree cleanup (structural, affects other fixes)
3. #3 notification quiescence (needs hermesClient understanding)
4. #4 permission fail-closed on resume
5. #1 version check + reliable immediate continuation
6. #6 validation tests + smoke command + remove unrelated change
7. #7 docs
8. Final: build + test + gofmt + diff --check + push
