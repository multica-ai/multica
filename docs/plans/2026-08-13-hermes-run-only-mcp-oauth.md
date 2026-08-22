# Hermes Scheduled Figma MCP Isolation Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Prevent scheduled run-only Hermes autopilots from initializing the host-configured Figma OAuth MCP while preserving Figma for issue, chat, manual, and webhook tasks.

**Architecture:** Keep the host Hermes profile immutable. During task-local `HERMES_HOME` derivation, detect the exact scheduled run-only task kind (`AutopilotRunID != "" && AutopilotSource == "schedule"`) and set `enabled: false` only on the `mcp_servers.figma` entry. Every other task and MCP entry retains the source configuration unchanged.

**Tech Stack:** Go, `gopkg.in/yaml.v3`, Go unit tests.

---

### Task 1: Pin the scheduled configuration contract

**Objective:** Add a regression test proving only scheduled run-only tasks disable Figma.

**Files:**
- Modify: `server/internal/daemon/execenv/hermes_home_test.go`

**Step 1: Write failing test**

Create a source `config.yaml` with Figma plus an unrelated MCP. Prepare scheduled, manual run-only, and regular issue task environments. Assert only the scheduled derived config sets Figma `enabled: false`; assert every other MCP and task remains unchanged; assert source config stays byte-identical. Cover shared YAML anchors without mutating the unrelated alias target, malformed/non-mapping scheduled config fail-closed behavior, and scheduled↔manual reuse transitions.

**Step 2: Run test to verify failure**

Run: `cd server && go test ./internal/daemon/execenv -run TestPrepareHermesScheduledRunOnlyDisablesOnlyFigmaMCP -count=1 -v`

Expected: FAIL because current derived config preserves enabled Figma for scheduled tasks.

### Task 2: Implement scoped Figma isolation

**Objective:** Make the smallest reversible production change matching the regression contract.

**Files:**
- Modify: `server/internal/daemon/execenv/hermes_home.go`
- Modify: `server/internal/daemon/execenv/execenv.go`

**Step 1: Add preparation option**

Add an internal Hermes-home option for disabling Figma. Keep the existing helper as an explicitly documented no-policy compatibility wrapper for existing tests and callers.

**Step 2: Derive option from exact task kind**

Fresh and reused Hermes environments derive the option through one shared helper only when `TaskContextForEnv.AutopilotRunID` is non-empty and `AutopilotSource` equals `schedule`.

**Step 3: Rewrite only Figma**

In parsed derived YAML, find the case-insensitive `figma` entry under `mcp_servers` and set `enabled: false`. Materialize aliases before editing so a shared anchor used by another MCP remains unchanged. Preserve all other fields and entries. Fail closed for scheduled isolation when the config cannot be parsed or rewritten; retain legacy verbatim fallback for non-scheduled tasks. Do not read or log credential values.

**Step 4: Run regression test to verify pass**

Run: `cd server && go test ./internal/daemon/execenv -run TestPrepareHermesScheduledRunOnlyDisablesOnlyFigmaMCP -count=1 -v`

Expected: PASS.

### Task 3: Verify integration and review

**Objective:** Prove existing Hermes overlay behavior still passes and deliver a reviewable PR.

**Files:**
- Review: `server/internal/daemon/execenv/hermes_home.go`
- Review: `server/internal/daemon/execenv/execenv.go`
- Review: `server/internal/daemon/execenv/hermes_home_test.go`

**Step 1: Format and run scoped suite**

Run: `gofmt -w server/internal/daemon/execenv/hermes_home.go server/internal/daemon/execenv/execenv.go server/internal/daemon/execenv/hermes_home_test.go`

Run: `cd server && go test ./internal/daemon/execenv -count=1`

Expected: PASS.

**Step 2: Run broader daemon tests**

Run: `cd server && go test ./internal/daemon/... -count=1`

Expected: PASS.

**Step 3: Review and commit**

Inspect `git diff --check`, `git diff --stat`, and the full diff. Commit with `fix(daemon): isolate Figma MCP from scheduled Hermes tasks`, push the issue branch, and open a PR whose title contains `KAP-1605`.
