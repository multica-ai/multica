# Daemon Restart Environment Preservation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve the running daemon's executable, flags, and environment across a plain restart, and make Loop-1 triage description files pass Multica's workdir safety check.

**Architecture:** Route plain background restarts through a new localhost daemon endpoint that reuses the existing self-handoff path. Keep caller-owned stop/start behavior for explicit flag overrides. Store assertion-runner temp files below its configured repository workdir.

**Tech Stack:** Go, Cobra, `net/http`, Bun, TypeScript, Vitest-compatible Bun tests.

---

### Task 1: Daemon self-restart endpoint

**Files:**
- Modify: `server/internal/daemon/health.go`
- Test: `server/internal/daemon/health_test.go`

- [ ] **Step 1: Write failing handler tests**

Add tests that POST `/restart`, assert the response is `restarting`, and verify
the daemon restart binary and cancellation signal are set. Add a GET test that
expects `405 Method Not Allowed`.

- [ ] **Step 2: Run the focused tests and confirm red**

Run: `go test ./internal/daemon -run 'TestRestartHandler'`

Expected: compilation or assertion failure because `restartHandler` does not
exist.

- [ ] **Step 3: Implement the handler**

Add `restartHandler`, respond before asynchronously invoking the existing
`triggerRestart`, and register `/restart` in `serveHealth`.

- [ ] **Step 4: Run the focused tests and confirm green**

Run: `go test ./internal/daemon -run 'TestRestartHandler'`

Expected: PASS.

### Task 2: Plain CLI restart uses daemon-owned handoff

**Files:**
- Modify: `server/cmd/multica/cmd_daemon.go`
- Test: `server/cmd/multica/cmd_daemon_test.go`

- [ ] **Step 1: Write failing restart-selection tests**

Add a helper test proving a plain background restart has no caller overrides,
while `--foreground`, `--no-auto-update`, timing, identity, capacity, and server
flags count as overrides. `--profile` remains a target selector and still uses
self-handoff.

- [ ] **Step 2: Run the focused tests and confirm red**

Run: `go test ./cmd/multica -run 'TestDaemonRestartUsesSelfHandoff'`

Expected: compilation failure because the selection helper does not exist.

- [ ] **Step 3: Implement request and readiness wait**

For the no-override case, POST `/restart`, then wait for the old PID to disappear
and a different PID to report `status=running`. Preserve the existing stop/start
path when overrides are present.

- [ ] **Step 4: Run the focused tests and confirm green**

Run: `go test ./cmd/multica -run 'TestDaemonRestartUsesSelfHandoff'`

Expected: PASS.

### Task 3: Loop-1 triage file stays in the task workdir

**Files:**
- Modify: `/Users/tangyuanjc/blackboard-v3/scripts/consumer-assertions.ts`
- Test: `/Users/tangyuanjc/blackboard-v3/scripts/consumer-assertions.test.ts`

- [ ] **Step 1: Write a failing triage-path test**

Capture the `multica issue create` arguments, assert `--description-file` is
inside `blackboardRepo`, and assert the path no longer exists after collection.

- [ ] **Step 2: Run the focused test and confirm red**

Run: `/Users/tangyuanjc/.bun/bin/bun test scripts/consumer-assertions.test.ts`

Expected: FAIL because the current path is under the system temp directory.

- [ ] **Step 3: Implement in-workdir temporary storage**

Create the temporary directory with `mkdtempSync(join(config.blackboardRepo,
'.consumer-assertion-triage-'))` and remove the entire directory in `finally`.

- [ ] **Step 4: Run the focused test and confirm green**

Run: `/Users/tangyuanjc/.bun/bin/bun test scripts/consumer-assertions.test.ts`

Expected: PASS.

### Task 4: Verification and delivery

**Files:**
- Verify all modified files above.

- [ ] **Step 1: Run focused Go tests**

Run: `go test ./internal/daemon ./cmd/multica`

Expected: PASS.

- [ ] **Step 2: Run the runner test suite**

Run: `/Users/tangyuanjc/.bun/bin/bun test scripts/consumer-assertions.test.ts`

Expected: PASS.

- [ ] **Step 3: Review diffs and commit only scoped files**

Commit the Multica change with a `fix(daemon)` conventional commit and the
blackboard change separately, without staging unrelated dirty-worktree files.

- [ ] **Step 4: Push/open a PR and perform safe operational handoff**

Use `WS-2062` in the PR title or body. Do not restart the daemon while unrelated
tasks are active; once only the current task remains, hand off to the configured
daemon and verify the live process environment plus new trace growth.
