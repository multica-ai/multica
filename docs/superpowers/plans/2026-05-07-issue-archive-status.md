# Issue Archive Status Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an `archive` issue status that users can select, hide from the board, and treat as a closed terminal status that cancels active issue tasks.

**Architecture:** Shared TypeScript status config drives frontend pickers and board columns. The database status check constraint must accept every persisted issue status. SQL queries and dynamic search define backend closed/open semantics. Issue update handlers are responsible for terminal task cancellation on status transitions.

**Tech Stack:** TypeScript, React view packages, Vitest, Go, PostgreSQL migrations, sqlc-generated query code, Cobra CLI tests.

---

## File Structure

- Modify `packages/core/types/issue.ts`: add `archive` to `IssueStatus`.
- Modify `packages/core/issues/config/status.ts`: add `archive` to selectable status lists and config; keep it out of `BOARD_STATUSES`.
- Create `packages/core/issues/config/status.test.ts`: assert `archive` is selectable and absent from board columns.
- Modify `packages/views/locales/en/issues.json`: add `Archive`.
- Modify `packages/views/locales/zh-Hans/issues.json`: add `已归档`.
- Modify `server/cmd/multica/cmd_issue.go`: add `archive` to CLI status allowlist.
- Modify `server/cmd/multica/cmd_issue_test.go`: expect `archive` in the CLI allowlist.
- Create `server/migrations/069_issue_archive_status.up.sql`: update the issue status check constraint to accept `archive`.
- Create `server/migrations/069_issue_archive_status.down.sql`: map `archive` to `done`, then restore the previous issue status check constraint.
- Modify `server/pkg/db/queries/issue.sql`: add `archive` to open issue exclusions and child progress closed counts.
- Modify `server/pkg/db/queries/inbox.sql`: include `archive` in finished issue notification lookup.
- Modify `server/pkg/db/queries/project.sql`: count `archive` as done for project linked issue metrics.
- Modify generated sqlc files under `server/pkg/db/generated/*.sql.go` to match query SQL.
- Modify `server/internal/handler/issue.go`: add `archive` to dynamic search closed filtering, status rank, backlog trigger exclusions, and task cancellation.
- Modify `server/internal/handler/search_test.go`: expect `archive` in issue search closed filters.
- Modify `server/internal/handler/handler_test.go`: add tests that archive status cancels active tasks in single and batch update paths.

### Task 1: Shared Status Model

**Files:**
- Create: `packages/core/issues/config/status.test.ts`
- Modify: `packages/core/types/issue.ts`
- Modify: `packages/core/issues/config/status.ts`

- [ ] **Step 1: Write failing test**

Create `packages/core/issues/config/status.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { ALL_STATUSES, BOARD_STATUSES, STATUS_CONFIG, STATUS_ORDER } from "./status";

describe("issue status config", () => {
  it("includes archive as a selectable issue status", () => {
    expect(STATUS_ORDER).toContain("archive");
    expect(ALL_STATUSES).toContain("archive");
    expect(STATUS_CONFIG.archive.label).toBe("Archive");
  });

  it("excludes archive from board columns", () => {
    expect(BOARD_STATUSES).not.toContain("archive");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter @multica/core test -- issues/config/status.test.ts`

Expected: FAIL because `archive` is not in the current status config.

- [ ] **Step 3: Implement status config**

Add `| "archive"` to `IssueStatus`. Add `"archive"` to `STATUS_ORDER` and `ALL_STATUSES`, keep it out of `BOARD_STATUSES`, and add muted `STATUS_CONFIG.archive`.

- [ ] **Step 4: Run test to verify it passes**

Run: `pnpm --filter @multica/core test -- issues/config/status.test.ts`

Expected: PASS.

### Task 2: Labels and CLI Allowlist

**Files:**
- Modify: `packages/views/locales/en/issues.json`
- Modify: `packages/views/locales/zh-Hans/issues.json`
- Modify: `server/cmd/multica/cmd_issue_test.go`
- Modify: `server/cmd/multica/cmd_issue.go`

- [ ] **Step 1: Update failing CLI test**

Add `"archive": true` to `TestValidIssueStatuses` expected map in `server/cmd/multica/cmd_issue_test.go`.

- [ ] **Step 2: Run CLI test to verify it fails**

From `server`, run: `go test ./cmd/multica -run TestValidIssueStatuses -count=1`

Expected: FAIL because `validIssueStatuses` has 7 entries instead of 8.

- [ ] **Step 3: Implement labels and CLI allowlist**

Add `"archive": "Archive"` to `packages/views/locales/en/issues.json`.
Add `"archive": "已归档"` to `packages/views/locales/zh-Hans/issues.json`.
Add `"archive"` to `validIssueStatuses` in `server/cmd/multica/cmd_issue.go`.

- [ ] **Step 4: Verify labels and CLI pass**

Run:

```bash
node -e "JSON.parse(require('fs').readFileSync('packages/views/locales/en/issues.json','utf8')); JSON.parse(require('fs').readFileSync('packages/views/locales/zh-Hans/issues.json','utf8'))"
go test ./cmd/multica -run TestValidIssueStatuses -count=1
```

Run the Go command from `server`.

Expected: both pass.

### Task 3: Database Status Constraint

**Files:**
- Create: `server/migrations/069_issue_archive_status.up.sql`
- Create: `server/migrations/069_issue_archive_status.down.sql`

- [ ] **Step 1: Add issue status constraint migration**

Create `server/migrations/069_issue_archive_status.up.sql`:

```sql
ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;

ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled', 'archive'));
```

- [ ] **Step 2: Add rollback migration**

Create `server/migrations/069_issue_archive_status.down.sql`:

```sql
UPDATE issue SET status = 'done' WHERE status = 'archive';

ALTER TABLE issue DROP CONSTRAINT IF EXISTS issue_status_check;

ALTER TABLE issue ADD CONSTRAINT issue_status_check
    CHECK (status IN ('backlog', 'todo', 'in_progress', 'in_review', 'done', 'blocked', 'cancelled'));
```

- [ ] **Step 3: Verify migration files are tracked in the diff**

Run: `git diff --stat -- server/migrations/069_issue_archive_status.up.sql server/migrations/069_issue_archive_status.down.sql`

Expected: both migration files are listed.

### Task 4: Backend Closed Status Semantics

**Files:**
- Modify: `server/pkg/db/queries/issue.sql`
- Modify: `server/pkg/db/queries/inbox.sql`
- Modify: `server/pkg/db/queries/project.sql`
- Modify: `server/pkg/db/generated/issue.sql.go`
- Modify: `server/pkg/db/generated/inbox.sql.go`
- Modify: `server/pkg/db/generated/project.sql.go`
- Modify: `server/internal/handler/issue.go`
- Modify: `server/internal/handler/search_test.go`

- [ ] **Step 1: Update failing search tests**

In `server/internal/handler/search_test.go`, update issue search expectations so `includeClosed=false` requires `NOT IN ('done', 'cancelled', 'archive')`, and `includeClosed=true` asserts that string is absent.

- [ ] **Step 2: Run search tests to verify they fail**

From `server`, run: `go test ./internal/handler -run TestSearch -count=1`

Expected: FAIL because search still filters only `done` and `cancelled`.

- [ ] **Step 3: Implement closed status SQL and search changes**

Update issue closed status sets from `('done', 'cancelled')` to `('done', 'cancelled', 'archive')` in source SQL, generated SQL, and dynamic search. Add `archive` after `cancelled` in search status rank.

- [ ] **Step 4: Run search tests to verify they pass**

From `server`, run: `go test ./internal/handler -run TestSearch -count=1`

Expected: PASS.

### Task 5: Archive Cancels Active Tasks

**Files:**
- Modify: `server/internal/handler/handler_test.go`
- Modify: `server/internal/handler/issue.go`

- [ ] **Step 1: Add failing handler tests**

Add tests covering single issue update to `archive` and batch issue update to `archive`, each with an active task row that should become `cancelled`.

- [ ] **Step 2: Run handler tests to verify they fail**

From `server`, run: `go test ./internal/handler -run 'Archive.*Cancel|Batch.*Archive' -count=1`

Expected: FAIL because only `cancelled` status currently cancels tasks.

- [ ] **Step 3: Implement archive task cancellation**

In `server/internal/handler/issue.go`, replace status checks equivalent to `issue.Status == "cancelled"` for task cancellation with a helper or direct condition that also includes `archive`. Prevent backlog-to-active enqueue when the new status is `archive`.

- [ ] **Step 4: Run handler tests to verify they pass**

From `server`, run: `go test ./internal/handler -run 'Archive.*Cancel|Batch.*Archive' -count=1`

Expected: PASS.

### Task 6: Final Verification and Commit

**Files:**
- All files changed in Tasks 1-5.

- [ ] **Step 1: Run focused tests**

Run:

```bash
pnpm --filter @multica/core test -- issues/config/status.test.ts
pnpm --filter @multica/core typecheck
pnpm --filter @multica/views typecheck
```

From `server`, run:

```bash
go test ./cmd/multica -run TestValidIssueStatuses -count=1
go test ./internal/handler -run 'TestSearch|Archive.*Cancel|Batch.*Archive' -count=1
```

Expected: all pass.

- [ ] **Step 2: Review diff**

Run: `git diff --check`

Expected: no whitespace errors.

Run: `git diff --stat`

Expected: only planned files changed.

- [ ] **Step 3: Commit**

Run:

```bash
git add docs/superpowers/specs/2026-05-07-issue-archive-status-design.md docs/superpowers/plans/2026-05-07-issue-archive-status.md packages/core/types/issue.ts packages/core/issues/config/status.ts packages/core/issues/config/status.test.ts packages/views/locales/en/issues.json packages/views/locales/zh-Hans/issues.json server/cmd/multica/cmd_issue.go server/cmd/multica/cmd_issue_test.go server/migrations/069_issue_archive_status.up.sql server/migrations/069_issue_archive_status.down.sql server/pkg/db/queries/issue.sql server/pkg/db/queries/inbox.sql server/pkg/db/queries/project.sql server/pkg/db/generated/issue.sql.go server/pkg/db/generated/inbox.sql.go server/pkg/db/generated/project.sql.go server/internal/handler/issue.go server/internal/handler/search_test.go server/internal/handler/handler_test.go
git commit -m "feat: add archive issue status"
```

Expected: commit succeeds.
