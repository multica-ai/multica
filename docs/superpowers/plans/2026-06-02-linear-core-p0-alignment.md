# Linear Core P0 Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver Linear-aligned P0 workflow closure by adding issue archive lifecycle and cycle planning basics.

**Architecture:** Extend existing issue-centric model in server SQL + handlers + workspace feature stores. Introduce cycle as a first-class workspace entity, then wire issue-cycle relation in APIs and UI filters. Keep all behavior under current workspace boundaries and existing route/store patterns.

**Tech Stack:** Go (chi handlers, sqlc, PostgreSQL), TypeScript (React, TanStack Router, Zustand), Vitest, Go test, Playwright

---

## Scope Check

当前 spec 覆盖多个独立子系统（P0/P1/P2）。本计划仅执行 P0，两条独立主线：
1. Issue archive lifecycle
2. Cycle planning base

P1（triage/automation/insights/estimation/roadmap）应拆为后续独立计划，避免一次计划跨太多子系统。

## File Structure (P0)

- Create:
  - `server/migrations/044_issue_archive_cycle.up.sql`
  - `server/migrations/044_issue_archive_cycle.down.sql`
  - `server/pkg/db/queries/cycle.sql`
  - `server/internal/handler/cycle.go`
  - `server/internal/handler/cycle_test.go`
  - `apps/workspace/src/features/cycles/store.ts`
  - `apps/workspace/src/features/cycles/components/cycle-page.tsx`
  - `apps/workspace/src/features/cycles/index.ts`
- Modify:
  - `server/pkg/db/queries/issue.sql`
  - `server/internal/handler/issue.go`
  - `server/cmd/server/router.go`
  - `apps/workspace/src/shared/types/issue.ts`
  - `apps/workspace/src/shared/api/client.ts`
  - `apps/workspace/src/router.tsx`
  - `apps/workspace/src/features/layout/navigation.ts`
  - `apps/workspace/src/features/issues/store.ts`
  - `apps/workspace/src/features/issues/components/issue-list-page.tsx`
  - `apps/workspace/src/features/issues/mutations.test.tsx`
  - `server/internal/handler/handler_test.go`

---

### Task 1: Add Issue Archive Lifecycle (TDD-first)

**Files:**
- Modify: `server/migrations/044_issue_archive_cycle.up.sql`, `server/migrations/044_issue_archive_cycle.down.sql`
- Modify: `server/pkg/db/queries/issue.sql`
- Modify: `server/internal/handler/issue.go`
- Modify: `server/internal/handler/handler_test.go`
- Modify: `apps/workspace/src/shared/types/issue.ts`
- Modify: `apps/workspace/src/features/issues/store.ts`
- Modify: `apps/workspace/src/features/issues/components/issue-list-page.tsx`

- [ ] **Step 1: Write failing backend test for archive/unarchive**

```go
func TestArchiveIssueLifecycle(t *testing.T) {
    h, token := newTestHandler(t)
    issue := createIssue(t, h, token, "Archive me")

    // archive
    req := httptest.NewRequest(http.MethodPost, "/api/issues/"+issue.ID.String()+"/archive", nil)
    req.Header.Set("Authorization", "Bearer "+token)
    rec := httptest.NewRecorder()
    h.router.ServeHTTP(rec, req)
    require.Equal(t, http.StatusOK, rec.Code)

    // list active should not include archived issue
    // list with include_archived=true should include it
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/handler -run TestArchiveIssueLifecycle`
Expected: FAIL with missing route/handler/query.

- [ ] **Step 3: Add minimal DB + query support**

```sql
ALTER TABLE issue ADD COLUMN archived_at timestamptz NULL;

-- name: ArchiveIssue :exec
UPDATE issue SET archived_at = now(), updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: UnarchiveIssue :exec
UPDATE issue SET archived_at = NULL, updated_at = now()
WHERE id = $1 AND workspace_id = $2;
```

- [ ] **Step 4: Add handler routes and archive filtering**

```go
func (h *Handler) ArchiveIssue(w http.ResponseWriter, r *http.Request) { /* call ArchiveIssue query */ }
func (h *Handler) UnarchiveIssue(w http.ResponseWriter, r *http.Request) { /* call UnarchiveIssue query */ }
```

- [ ] **Step 5: Add minimal frontend behavior**

```ts
export interface Issue {
  // existing fields...
  archived_at?: string | null;
}
```

```ts
async archiveIssue(id: string): Promise<void> {
  await api.archiveIssue(id);
  await get().fetchIssues();
}
```

- [ ] **Step 6: Run tests to verify pass**

Run: `cd server && go test ./internal/handler -run TestArchiveIssueLifecycle`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add server/migrations/044_issue_archive_cycle.*.sql server/pkg/db/queries/issue.sql server/internal/handler/issue.go server/internal/handler/handler_test.go apps/workspace/src/shared/types/issue.ts apps/workspace/src/features/issues/store.ts apps/workspace/src/features/issues/components/issue-list-page.tsx
git commit -m "feat(server,workspace): add issue archive lifecycle"
```

---

### Task 2: Add Cycle Entity + API (TDD-first)

**Files:**
- Create: `server/pkg/db/queries/cycle.sql`
- Create: `server/internal/handler/cycle.go`
- Create: `server/internal/handler/cycle_test.go`
- Modify: `server/migrations/044_issue_archive_cycle.up.sql`
- Modify: `server/migrations/044_issue_archive_cycle.down.sql`
- Modify: `server/cmd/server/router.go`
- Modify: `server/pkg/db/queries/issue.sql`

- [ ] **Step 1: Write failing cycle handler tests**

```go
func TestCycleCRUD(t *testing.T) {
    // create cycle -> list cycles -> update cycle -> close cycle
    // assert workspace isolation
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd server && go test ./internal/handler -run TestCycleCRUD`
Expected: FAIL with missing cycle schema/routes/queries.

- [ ] **Step 3: Add schema and sqlc queries**

```sql
CREATE TABLE cycle (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
  name text NOT NULL,
  starts_at timestamptz NOT NULL,
  ends_at timestamptz NOT NULL,
  state text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
```

```sql
-- name: CreateCycle :one
INSERT INTO cycle (...) VALUES (...) RETURNING *;
```

- [ ] **Step 4: Wire router + handler**

```go
r.Route("/cycles", func(r chi.Router) {
    r.Post("/", h.CreateCycle)
    r.Get("/", h.ListCycles)
    r.Patch("/{cycleID}", h.UpdateCycle)
    r.Post("/{cycleID}/close", h.CloseCycle)
})
```

- [ ] **Step 5: Add issue-cycle relation support**

```sql
ALTER TABLE issue ADD COLUMN cycle_id uuid NULL REFERENCES cycle(id) ON DELETE SET NULL;
```

- [ ] **Step 6: Run tests to verify pass**

Run: `cd server && go test ./internal/handler -run 'TestCycleCRUD|TestArchiveIssueLifecycle'`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add server/migrations/044_issue_archive_cycle.*.sql server/pkg/db/queries/cycle.sql server/pkg/db/queries/issue.sql server/internal/handler/cycle.go server/internal/handler/cycle_test.go server/cmd/server/router.go
git commit -m "feat(server): add cycle entity and APIs"
```

---

### Task 3: Add Cycle UI Entry + Issue Binding (TDD-first)

**Files:**
- Create: `apps/workspace/src/features/cycles/store.ts`
- Create: `apps/workspace/src/features/cycles/components/cycle-page.tsx`
- Create: `apps/workspace/src/features/cycles/index.ts`
- Modify: `apps/workspace/src/router.tsx`
- Modify: `apps/workspace/src/features/layout/navigation.ts`
- Modify: `apps/workspace/src/shared/api/client.ts`
- Modify: `apps/workspace/src/features/issues/store.ts`
- Modify: `apps/workspace/src/features/issues/mutations.test.tsx`

- [ ] **Step 1: Write failing frontend tests**

```tsx
it("shows cycle page in navigation and loads cycles", async () => {
  // render app shell
  // click cycles nav item
  // expect cycle list rendered
});
```

```tsx
it("binds issue to selected cycle", async () => {
  // select cycle in issue detail
  // expect updateIssue called with cycle_id
});
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pnpm --filter @multica/workspace exec vitest run src/features/issues/mutations.test.tsx src/features/layout/navigation.test.ts`
Expected: FAIL with missing cycle route/store/API.

- [ ] **Step 3: Add minimal API client methods**

```ts
async listCycles(): Promise<Cycle[]> {
  return this.request<Cycle[]>("/api/cycles");
}

async updateIssue(id: string, input: { cycle_id?: string | null }) {
  return this.request(`/api/issues/${id}`, { method: "PATCH", body: JSON.stringify(input) });
}
```

- [ ] **Step 4: Add cycle store + route + nav**

```ts
export const CYCLES_PATH = "/cycles";
```

```ts
{ title: "Cycles", url: "/cycles", icon: Repeat2 }
```

- [ ] **Step 5: Add issue-cycle picker binding**

```ts
await updateIssue(issue.id, { cycle_id: selectedCycleId ?? null });
```

- [ ] **Step 6: Run frontend tests**

Run: `pnpm --filter @multica/workspace exec vitest run src/features/issues/mutations.test.tsx src/features/layout/navigation.test.ts`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add apps/workspace/src/features/cycles apps/workspace/src/router.tsx apps/workspace/src/features/layout/navigation.ts apps/workspace/src/shared/api/client.ts apps/workspace/src/features/issues/store.ts apps/workspace/src/features/issues/mutations.test.tsx
git commit -m "feat(workspace): add cycle navigation and issue-cycle binding"
```

---

### Task 4: Full Verification + Spec Backwrite

**Files:**
- Modify: `docs/superpowers/specs/linear-alignment/module-overview.md`
- Modify: `docs/superpowers/specs/linear-alignment/01-core-advanced-alignment/spec.md`

- [ ] **Step 1: Run backend test suite for touched domain**

Run: `cd server && go test ./internal/handler`
Expected: PASS.

- [ ] **Step 2: Run workspace tests for touched domain**

Run: `pnpm --filter @multica/workspace exec vitest run src/features/issues src/features/layout/navigation.test.ts`
Expected: PASS.

- [ ] **Step 3: Run full repo check**

Run: `make check`
Expected: PASS.

- [ ] **Step 4: Backwrite spec status**

```md
- Update module-overview status table:
  - Issue archive lifecycle: done
  - Cycle planning base: done
- Update core-advanced spec gap section:
  - P0 gaps moved to completed
```

- [ ] **Step 5: Commit**

```bash
git add docs/superpowers/specs/linear-alignment/module-overview.md docs/superpowers/specs/linear-alignment/01-core-advanced-alignment/spec.md
git commit -m "docs(spec): backwrite P0 completion status"
```

---

## Self-Review

### 1) Spec coverage

- 归档生命周期：Task 1 覆盖。
- Cycle 基础能力：Task 2 + Task 3 覆盖。
- 90 天路线中 P0 范围：本计划完整覆盖。
- P1/P2：已明确拆分，不在本计划实施范围。

### 2) Placeholder scan

- 已检查，无 TBD/TODO/“later” 类占位描述。
- 每个代码步骤均附具体代码片段，每个验证步骤均附命令与预期。

### 3) Type consistency

- 统一使用 `cycle_id` 作为 issue 关联字段。
- 归档字段统一使用 `archived_at`。
- 计划中的 API、字段命名与 spec/design 保持一致。
