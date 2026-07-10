# Daemon Log Rotation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bound background Multica daemon logs and stop CPA raw-log capture before disk free space falls below 15 GiB.

**Architecture:** Add a small copy-truncate rotator at the CLI daemon lifecycle boundary. Pass the profile-specific log path to background children, run an initial check, and keep checking on a ticker. Separately align the existing CPA disk watermark with the host critical threshold.

**Tech Stack:** Go standard library, Go tests, Python unittest, launchd plist configuration.

---

### Task 1: Specify daemon log rotation behavior

**Files:**
- Modify: `server/cmd/multica/cmd_daemon_test.go`
- Test: `server/cmd/multica/cmd_daemon_test.go`

- [ ] **Step 1: Write failing tests** for a below-limit no-op and an over-limit copy-truncate rotation that keeps two numbered backups.
- [ ] **Step 2: Run the focused tests** with `go test ./cmd/multica -run 'TestRotateDaemonLog' -count=1` and confirm they fail because the helper does not exist.

### Task 2: Implement and wire daemon log rotation

**Files:**
- Modify: `server/cmd/multica/cmd_daemon.go`
- Modify: `server/cmd/multica/cmd_daemon_test.go`

- [ ] **Step 1: Implement the minimal helper** using a temporary archive, `io.Copy`, `Sync`, numbered backup shifting, and in-place truncation.
- [ ] **Step 2: Pass the profile log path** to every spawned background/restart child with `MULTICA_DAEMON_LOG_PATH`.
- [ ] **Step 3: Run an initial rotation check and ticker** only when that environment variable is present.
- [ ] **Step 4: Run focused tests** and confirm they pass.
- [ ] **Step 5: Run `gofmt` and `git diff --check`.**

### Task 3: Align CPA capture with the critical disk threshold

**Files:**
- Modify: `/Users/tangyuanjc/.openclaw/cpa-capture/test_puller.py`
- Modify: `/Users/tangyuanjc/.openclaw/cpa-capture/puller.py`

- [ ] **Step 1: Add a failing assertion** that the default critical watermark is 15 GiB.
- [ ] **Step 2: Run the focused unittest** and confirm it fails against the current 10 GiB value.
- [ ] **Step 3: Change only the default critical watermark** to 15 GiB.
- [ ] **Step 4: Re-run the focused and full CPA test suites.**

### Task 4: Reclaim disk safely and verify

**Files:**
- Runtime cache/log data only; no source file creation.

- [ ] **Step 1: Record pre-cleanup sizes and free space.**
- [ ] **Step 2: Remove reproducible caches** while preserving Chrome data, CloudKit user state, Pictures, GBrain model/runtime data, Claude VM bundles, active Hermes runtime files, and Multica workspace ledgers.
- [ ] **Step 3: Preserve recent daemon log tails and truncate active logs in place.**
- [ ] **Step 4: Re-run disk census and verify free space exceeds 30 GiB.**
- [ ] **Step 5: Kick the daemon-health launchd job and verify the disk check is loaded and reports the post-cleanup state.**

### Task 5: Handoff

**Files:**
- Commit the Multica repo changes on the task branch.
- Commit only the CPA files touched, preserving unrelated dirty files.

- [ ] **Step 1: Run final focused verification.**
- [ ] **Step 2: Open a PR whose title/body contains `WS-1673`.**
- [ ] **Step 3: Create the required Allen follow-up issue.**
- [ ] **Step 4: Post exactly one concise final comment on WS-1673 and move it to `in_review`.**
