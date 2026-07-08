# Issue Token Usage: Cost + Per-Run Breakdown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the "Token usage" section on the issue detail sidebar understandable: show an estimated dollar cost, a per-run breakdown (so users can see which comment cycle burned tokens), and explain what cache read/write means.

**Architecture:** Everything reuses existing infrastructure. The backend already stores per-task per-model rows in `task_usage`; we add one sqlc query and extend the existing `GET /api/issues/{id}/usage` response with a `tasks` array (additive, backward compatible). The frontend already has a pricing table and `estimateCost()` in `packages/views/runtimes/utils.ts` (the dashboard imports it the same way); we compute cost client-side, matching the dashboard pattern. The token-usage JSX in `issue-detail.tsx` is extracted into its own component so it can be tested.

**Tech Stack:** Go + sqlc (backend), zod + TanStack Query (`packages/core`), React + vitest + testing-library (`packages/views`).

## Global Constraints

- Parse API JSON with `parseWithFallback` from `packages/core/api/schema.ts` + a zod schema; never cast network JSON to `T` (CLAUDE.md, API Compatibility). New/changed endpoints need a malformed-response test.
- After editing `pkg/db/queries/*.sql`, run `make sqlc` from the repo root to regenerate.
- `packages/views/` must not import `next/*` or `react-router-dom`; tests must not mock them.
- Before editing anything under `packages/views/locales/`, read `apps/docs/content/docs/developers/conventions.mdx` and `conventions.zh.mdx` (i18n glossary + Chinese product voice). Existing precedent: zh-Hans uses "Token 用量" for "Token usage".
- Commit prefixes: `feat(server)`, `feat(core)`, `feat(views)`.
- Go: `make test` must pass. TS: `pnpm typecheck` and `pnpm test` must pass.

---

### Task 1: Backend — per-task usage rows on the issue usage endpoint

**Files:**
- Modify: `server/pkg/db/queries/task_usage.sql` (add query after `GetIssueUsageSummary`, ~line 30)
- Modify: `server/internal/handler/daemon.go:2763-2785` (`GetIssueUsage`)
- Test: `server/internal/handler/daemon_test.go` (after `TestGetIssueUsage_CrossWorkspace_Returns404`, ~line 1524)

**Interfaces:**
- Consumes: existing `task_usage` and `agent_task_queue` tables; existing `h.loadIssueForUser`.
- Produces: JSON response gains `"tasks": [...]` where each element is `{task_id, created_at (RFC3339), comment_triggered (bool), provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens}`. Task 2 parses exactly this shape.

- [ ] **Step 1: Add the sqlc query**

In `server/pkg/db/queries/task_usage.sql`, directly after the `GetIssueUsageSummary` query (line 30), add:

```sql
-- name: ListIssueTaskUsage :many
-- Per-task per-model usage rows for one issue, newest task first. Powers the
-- per-run breakdown in the issue sidebar's Token usage section.
-- comment_triggered distinguishes comment-cycle runs from assignment runs.
SELECT
    tu.task_id,
    atq.created_at,
    (atq.trigger_comment_id IS NOT NULL)::bool AS comment_triggered,
    tu.provider,
    tu.model,
    tu.input_tokens,
    tu.output_tokens,
    tu.cache_read_tokens,
    tu.cache_write_tokens
FROM task_usage tu
JOIN agent_task_queue atq ON atq.id = tu.task_id
WHERE atq.issue_id = $1
ORDER BY atq.created_at DESC, tu.model;
```

- [ ] **Step 2: Regenerate sqlc**

```bash
make sqlc
```

Expected: `server/pkg/db/generated/task_usage.sql.go` gains `ListIssueTaskUsage` returning `[]ListIssueTaskUsageRow`. `git status` shows only generated files changed. If sqlc errors, fix the SQL — do not hand-edit generated files.

- [ ] **Step 3: Write the failing handler test**

In `server/internal/handler/daemon_test.go`, after `TestGetIssueUsage_CrossWorkspace_Returns404` (~line 1524), add. The fixture pattern (borrow `testPool` inserts) is copied from `TestDashboardEndpoints` in `dashboard_test.go:21-100`:

```go
// TestGetIssueUsage_PerTaskBreakdown verifies the usage endpoint returns
// per-task rows alongside the aggregate totals, with comment_triggered
// distinguishing comment-cycle runs from assignment runs.
func TestGetIssueUsage_PerTaskBreakdown(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent_runtime WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&runtimeID); err != nil {
		t.Fatalf("fetch runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID); err != nil {
		t.Fatalf("fetch agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
		VALUES ($1, 'usage breakdown test', $2, 'member',
			(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1))
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("insert issue: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, author_id, author_type, content)
		VALUES ($1, $2, 'member', 'trigger')
		RETURNING id
	`, issueID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM comment WHERE id = $1`, commentID) })

	mkTask := func(triggerCommentID any, createdOffset time.Duration, inputTokens int64) string {
		var taskID string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_task_queue (agent_id, issue_id, runtime_id, status, trigger_comment_id, created_at)
			VALUES ($1, $2, $3, 'completed', $4, now() + $5::interval)
			RETURNING id
		`, agentID, issueID, runtimeID, triggerCommentID,
			fmt.Sprintf("%d seconds", int(createdOffset.Seconds()))).Scan(&taskID); err != nil {
			t.Fatalf("insert task: %v", err)
		}
		t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })
		if _, err := testPool.Exec(ctx, `
			INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, updated_at)
			VALUES ($1, 'anthropic', 'claude-sonnet-4.6', $2, 100, 50, 25, now())
		`, taskID, inputTokens); err != nil {
			t.Fatalf("insert task_usage: %v", err)
		}
		return taskID
	}

	assignmentTaskID := mkTask(nil, 0, 1000)
	commentTaskID := mkTask(commentID, time.Minute, 2000) // newer → first in response

	w := httptest.NewRecorder()
	req := newRequest("GET", "/api/issues/"+issueID+"/usage", nil)
	req = withURLParam(req, "id", issueID)
	testHandler.GetIssueUsage(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetIssueUsage: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		TotalInputTokens int64 `json:"total_input_tokens"`
		TaskCount        int   `json:"task_count"`
		Tasks            []struct {
			TaskID           string `json:"task_id"`
			CreatedAt        string `json:"created_at"`
			CommentTriggered bool   `json:"comment_triggered"`
			Provider         string `json:"provider"`
			Model            string `json:"model"`
			InputTokens      int64  `json:"input_tokens"`
			OutputTokens     int64  `json:"output_tokens"`
			CacheReadTokens  int64  `json:"cache_read_tokens"`
			CacheWriteTokens int64  `json:"cache_write_tokens"`
		} `json:"tasks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.TotalInputTokens != 3000 {
		t.Fatalf("expected total_input_tokens 3000, got %d", resp.TotalInputTokens)
	}
	if resp.TaskCount != 2 {
		t.Fatalf("expected task_count 2, got %d", resp.TaskCount)
	}
	if len(resp.Tasks) != 2 {
		t.Fatalf("expected 2 task rows, got %d", len(resp.Tasks))
	}
	// Newest first.
	if resp.Tasks[0].TaskID != commentTaskID || !resp.Tasks[0].CommentTriggered {
		t.Fatalf("expected first row = comment task %s with comment_triggered=true, got %+v", commentTaskID, resp.Tasks[0])
	}
	if resp.Tasks[1].TaskID != assignmentTaskID || resp.Tasks[1].CommentTriggered {
		t.Fatalf("expected second row = assignment task %s with comment_triggered=false, got %+v", assignmentTaskID, resp.Tasks[1])
	}
	if resp.Tasks[0].InputTokens != 2000 || resp.Tasks[0].Model != "claude-sonnet-4.6" {
		t.Fatalf("unexpected first row fields: %+v", resp.Tasks[0])
	}
	if resp.Tasks[0].CreatedAt == "" {
		t.Fatal("expected created_at to be set")
	}
}
```

Add `"fmt"` and `"time"` to the test file's imports if not already present.

- [ ] **Step 4: Run the test to confirm it fails**

```bash
cd server && go test ./internal/handler/ -run TestGetIssueUsage_PerTaskBreakdown -v
```

Expected: FAIL — `expected 2 task rows, got 0` (the handler doesn't emit `tasks` yet). If it skips with "database not available", start the dev DB (`make dev` setup) and re-run.

- [ ] **Step 5: Extend the handler**

In `server/internal/handler/daemon.go`, replace the body of `GetIssueUsage` (lines 2763-2785) with:

```go
// GetIssueUsage returns aggregated token usage for all tasks belonging to an
// issue, plus per-task per-model rows for the sidebar's per-run breakdown.
func (h *Handler) GetIssueUsage(w http.ResponseWriter, r *http.Request) {
	issueID := chi.URLParam(r, "id")
	issue, ok := h.loadIssueForUser(w, r, issueID)
	if !ok {
		return
	}

	row, err := h.Queries.GetIssueUsageSummary(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get issue usage")
		return
	}

	taskRows, err := h.Queries.ListIssueTaskUsage(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get issue task usage")
		return
	}

	type taskUsageResponse struct {
		TaskID           string `json:"task_id"`
		CreatedAt        string `json:"created_at"`
		CommentTriggered bool   `json:"comment_triggered"`
		Provider         string `json:"provider"`
		Model            string `json:"model"`
		InputTokens      int64  `json:"input_tokens"`
		OutputTokens     int64  `json:"output_tokens"`
		CacheReadTokens  int64  `json:"cache_read_tokens"`
		CacheWriteTokens int64  `json:"cache_write_tokens"`
	}
	tasks := make([]taskUsageResponse, 0, len(taskRows))
	for _, tr := range taskRows {
		createdAt := ""
		if tr.CreatedAt.Valid {
			createdAt = tr.CreatedAt.Time.UTC().Format(time.RFC3339)
		}
		tasks = append(tasks, taskUsageResponse{
			TaskID:           uuidToString(tr.TaskID),
			CreatedAt:        createdAt,
			CommentTriggered: tr.CommentTriggered,
			Provider:         tr.Provider,
			Model:            tr.Model,
			InputTokens:      tr.InputTokens,
			OutputTokens:     tr.OutputTokens,
			CacheReadTokens:  tr.CacheReadTokens,
			CacheWriteTokens: tr.CacheWriteTokens,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total_input_tokens":       row.TotalInputTokens,
		"total_output_tokens":      row.TotalOutputTokens,
		"total_cache_read_tokens":  row.TotalCacheReadTokens,
		"total_cache_write_tokens": row.TotalCacheWriteTokens,
		"task_count":               row.TaskCount,
		"tasks":                    tasks,
	})
}
```

If field names on `ListIssueTaskUsageRow` differ from the above (check `server/pkg/db/generated/task_usage.sql.go` after Step 2), match the generated names — do not edit generated code. `time` is already imported in `daemon.go`.

- [ ] **Step 6: Run the new test to confirm it passes**

```bash
cd server && go test ./internal/handler/ -run TestGetIssueUsage -v
```

Expected: PASS for both `TestGetIssueUsage_PerTaskBreakdown` and `TestGetIssueUsage_CrossWorkspace_Returns404`.

- [ ] **Step 7: Run the full Go suite**

```bash
cd server && make test 2>&1 | tail -10
```

Expected: all tests pass, no vet errors.

- [ ] **Step 8: Commit**

```bash
git add server/pkg/db/queries/task_usage.sql server/pkg/db/generated/ \
        server/internal/handler/daemon.go server/internal/handler/daemon_test.go
git commit -m "feat(server): add per-task usage rows to issue usage endpoint

Additive tasks[] array on GET /api/issues/{id}/usage so the sidebar can
show a per-run token/cost breakdown and distinguish comment-triggered
runs from assignment runs."
```

---

### Task 2: Core — schema, type, and defensive parsing for issue usage

**Files:**
- Modify: `packages/core/types/agent.ts:641-647` (`IssueUsageSummary`)
- Modify: `packages/core/api/schemas.ts` (add `IssueTaskUsageSchema` + `IssueUsageSummarySchema` near the other usage schemas, ~line 430)
- Modify: `packages/core/api/client.ts:1477-1479` (`getIssueUsage`)
- Test: `packages/core/api/schemas.test.ts` (add malformed-response cases)

**Interfaces:**
- Consumes: Task 1's response shape (`tasks[]` with `task_id`, `created_at`, `comment_triggered`, `provider`, `model`, 4 token fields).
- Produces: `IssueTaskUsage` type and `IssueUsageSummary.tasks: IssueTaskUsage[]` — Task 3 renders these. `api.getIssueUsage()` now schema-parses; downstream callers keep the same call signature.

- [ ] **Step 1: Update the types**

In `packages/core/types/agent.ts`, replace the `IssueUsageSummary` interface (lines 641-647) with:

```ts
export interface IssueTaskUsage {
  task_id: string;
  created_at: string;
  comment_triggered: boolean;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
}

export interface IssueUsageSummary {
  total_input_tokens: number;
  total_output_tokens: number;
  total_cache_read_tokens: number;
  total_cache_write_tokens: number;
  task_count: number;
  tasks: IssueTaskUsage[];
}
```

Export `IssueTaskUsage` from `packages/core/types/index.ts` next to the existing `IssueUsageSummary` re-export (line 63).

- [ ] **Step 2: Write the failing malformed-response test**

In `packages/core/api/schemas.test.ts`, following the file's existing test style, add:

```ts
describe("IssueUsageSummarySchema", () => {
  it("fills defaults for a malformed response", () => {
    const parsed = IssueUsageSummarySchema.parse({});
    expect(parsed.total_input_tokens).toBe(0);
    expect(parsed.task_count).toBe(0);
    expect(parsed.tasks).toEqual([]);
  });

  it("defaults missing fields inside task rows", () => {
    const parsed = IssueUsageSummarySchema.parse({
      total_input_tokens: 10,
      tasks: [{ model: "claude-sonnet-4.6" }],
    });
    expect(parsed.tasks[0].model).toBe("claude-sonnet-4.6");
    expect(parsed.tasks[0].input_tokens).toBe(0);
    expect(parsed.tasks[0].comment_triggered).toBe(false);
  });

  it("drops a non-array tasks field to the default", () => {
    const parsed = IssueUsageSummarySchema.safeParse({ tasks: "nope" });
    expect(parsed.success).toBe(false);
  });
});
```

Import `IssueUsageSummarySchema` at the top alongside the other schema imports.

- [ ] **Step 3: Run to confirm it fails**

```bash
pnpm --filter @multica/core test -- schemas.test.ts
```

Expected: FAIL — `IssueUsageSummarySchema` is not exported.

- [ ] **Step 4: Add the schemas**

In `packages/core/api/schemas.ts`, near the other token-usage schemas (~line 430), add:

```ts
export const IssueTaskUsageSchema = z.object({
  task_id: z.string().default(""),
  created_at: z.string().default(""),
  comment_triggered: z.boolean().default(false),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
});

export const IssueUsageSummarySchema = z.object({
  total_input_tokens: z.number().default(0),
  total_output_tokens: z.number().default(0),
  total_cache_read_tokens: z.number().default(0),
  total_cache_write_tokens: z.number().default(0),
  task_count: z.number().default(0),
  tasks: z.array(IssueTaskUsageSchema).default([]),
});
```

- [ ] **Step 5: Route `getIssueUsage` through `parseWithFallback`**

In `packages/core/api/client.ts`, replace lines 1477-1479 with (matching the `getMe` pattern at line 448-452):

```ts
async getIssueUsage(issueId: string): Promise<IssueUsageSummary> {
  const raw = await this.fetch<unknown>(`/api/issues/${issueId}/usage`);
  return parseWithFallback(raw, IssueUsageSummarySchema, EMPTY_ISSUE_USAGE, {
    endpoint: "GET /api/issues/:id/usage",
  });
}
```

Add near the other `EMPTY_*` fallback constants in `client.ts`:

```ts
const EMPTY_ISSUE_USAGE: IssueUsageSummary = {
  total_input_tokens: 0,
  total_output_tokens: 0,
  total_cache_read_tokens: 0,
  total_cache_write_tokens: 0,
  task_count: 0,
  tasks: [],
};
```

Add `IssueUsageSummarySchema` to the existing schema import block (~line 165).

- [ ] **Step 6: Run tests + typecheck**

```bash
pnpm --filter @multica/core test -- schemas.test.ts
pnpm typecheck
```

Expected: tests PASS, typecheck clean. If typecheck flags other `IssueUsageSummary` consumers (they now see a required `tasks` field), those are Task 3's files — only fix compile errors, no behavior changes here.

- [ ] **Step 7: Commit**

```bash
git add packages/core/types/agent.ts packages/core/types/index.ts \
        packages/core/api/schemas.ts packages/core/api/client.ts \
        packages/core/api/schemas.test.ts
git commit -m "feat(core): parse issue usage with schema, add per-task rows

getIssueUsage previously cast raw JSON; route it through
parseWithFallback per the API compatibility rules and add the tasks[]
per-run rows shipped by the server."
```

---

### Task 3: Views — extracted usage section with cost, per-run breakdown, cache tooltip

**Files:**
- Create: `packages/views/issues/components/issue-token-usage-section.tsx`
- Test: `packages/views/issues/components/issue-token-usage-section.test.tsx`
- Modify: `packages/views/issues/components/issue-detail.tsx` (delete inline section at lines 1649-1683, `formatTokenCount` at 287-291, and the `tokenUsageOpen` state at 754; render the new component)
- Modify: `packages/views/locales/en/issues.json`, `zh-Hans/issues.json`, `ja/issues.json`, `ko/issues.json` (new keys under `detail`)

**Interfaces:**
- Consumes: `IssueUsageSummary` / `IssueTaskUsage` from `@multica/core/types` (Task 2); `estimateCost` from `../../runtimes/utils` (same cross-domain import the dashboard uses — see `packages/views/dashboard/utils.ts:27-28`); `PropRow` from `../../common/prop-row`; `useT` from `../../i18n`.
- Produces: `<IssueTokenUsageSection usage={usage} />` — presentational, renders nothing when `usage.task_count === 0`.

- [ ] **Step 0: Read the i18n conventions**

Read `apps/docs/content/docs/developers/conventions.mdx` and `conventions.zh.mdx`. If the glossary contradicts any translation below (especially the zh-Hans strings), the glossary wins.

- [ ] **Step 1: Add locale keys**

In `packages/views/locales/en/issues.json`, inside the `detail` object (near `prop_cache_value`, line 218), add:

```json
"prop_est_cost": "Est. cost",
"prop_cache_tooltip": "Cache read: earlier context replayed at ~10% of the input price (a big number here is good). Cache write: context stored for reuse, ~25% over the input price.",
"usage_runs_toggle": "{{count}} runs",
"usage_run_comment": "Comment",
"usage_run_assignment": "Assignment"
```

`zh-Hans/issues.json` (verify against the glossary from Step 0):

```json
"prop_est_cost": "预计费用",
"prop_cache_tooltip": "缓存读取:以约一折的价格复用之前的上下文(数值大是好事)。缓存写入:存储上下文以便复用,较输入价格约加价 25%。",
"usage_runs_toggle": "{{count}} 次运行",
"usage_run_comment": "评论",
"usage_run_assignment": "指派"
```

`ja/issues.json`:

```json
"prop_est_cost": "推定コスト",
"prop_cache_tooltip": "キャッシュ読み取り:以前のコンテキストを入力価格の約10%で再利用(大きい値は良い状態)。キャッシュ書き込み:再利用のための保存で、入力価格より約25%割高。",
"usage_runs_toggle": "{{count}} 回の実行",
"usage_run_comment": "コメント",
"usage_run_assignment": "アサイン"
```

`ko/issues.json`:

```json
"prop_est_cost": "예상 비용",
"prop_cache_tooltip": "캐시 읽기: 이전 컨텍스트를 입력 가격의 약 10%로 재사용(값이 크면 좋습니다). 캐시 쓰기: 재사용을 위한 저장으로 입력 가격보다 약 25% 비쌉니다.",
"usage_runs_toggle": "실행 {{count}}회",
"usage_run_comment": "댓글",
"usage_run_assignment": "할당"
```

- [ ] **Step 2: Write the failing component test**

Create `packages/views/issues/components/issue-token-usage-section.test.tsx`. Test setup style (jsdom pragma, `I18nProvider`, locale JSON resources) is copied from `packages/views/runtimes/components/usage-section.test.tsx:1-12`:

```tsx
// @vitest-environment jsdom

import type { ReactNode } from "react";
import { describe, it, expect } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import type { IssueUsageSummary } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { IssueTokenUsageSection } from "./issue-token-usage-section";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

function wrap(children: ReactNode) {
  return <I18nProvider resources={TEST_RESOURCES}>{children}</I18nProvider>;
}

const USAGE: IssueUsageSummary = {
  total_input_tokens: 3000,
  total_output_tokens: 200,
  total_cache_read_tokens: 50_000,
  total_cache_write_tokens: 10_000,
  task_count: 2,
  tasks: [
    {
      task_id: "t2",
      created_at: "2026-07-08T10:00:00Z",
      comment_triggered: true,
      provider: "anthropic",
      model: "claude-sonnet-4.6",
      input_tokens: 2000,
      output_tokens: 100,
      cache_read_tokens: 30_000,
      cache_write_tokens: 5_000,
    },
    {
      task_id: "t1",
      created_at: "2026-07-08T09:00:00Z",
      comment_triggered: false,
      provider: "anthropic",
      model: "claude-sonnet-4.6",
      input_tokens: 1000,
      output_tokens: 100,
      cache_read_tokens: 20_000,
      cache_write_tokens: 5_000,
    },
  ],
};

describe("IssueTokenUsageSection", () => {
  it("renders nothing when there are no runs", () => {
    const { container } = render(wrap(<IssueTokenUsageSection usage={{ ...USAGE, task_count: 0, tasks: [] }} />));
    expect(container).toBeEmptyDOMElement();
  });

  it("shows totals and an estimated cost", () => {
    render(wrap(<IssueTokenUsageSection usage={USAGE} />));
    expect(screen.getByText("3.0k")).toBeInTheDocument(); // input total
    expect(screen.getByText("Est. cost")).toBeInTheDocument();
    // claude-sonnet-4.6: (3000*3 + 200*15 + 50000*0.3 + 10000*3.75) / 1e6 ≈ $0.0645
    expect(screen.getByText(/\$0\.06/)).toBeInTheDocument();
  });

  it("expands a per-run breakdown labelled by trigger type", () => {
    render(wrap(<IssueTokenUsageSection usage={USAGE} />));
    fireEvent.click(screen.getByText("2 runs"));
    expect(screen.getByText("Comment")).toBeInTheDocument();
    expect(screen.getByText("Assignment")).toBeInTheDocument();
  });

  it("explains cache read/write in a tooltip", () => {
    render(wrap(<IssueTokenUsageSection usage={USAGE} />));
    expect(screen.getByText("Cache").closest("[title]")).toHaveAttribute(
      "title",
      expect.stringContaining("Cache read"),
    );
  });
});
```

If `I18nProvider`'s props differ from `usage-section.test.tsx` (it is the canonical example), copy that file's provider wiring exactly.

- [ ] **Step 3: Run to confirm it fails**

```bash
pnpm --filter @multica/views test -- issue-token-usage-section
```

Expected: FAIL — module `./issue-token-usage-section` not found.

- [ ] **Step 4: Create the component**

Create `packages/views/issues/components/issue-token-usage-section.tsx`:

```tsx
import { useState } from "react";
import { ChevronRight } from "lucide-react";
import type { IssueTaskUsage, IssueUsageSummary } from "@multica/core/types";
import { PropRow } from "../../common/prop-row";
import { useT } from "../../i18n";
import { estimateCost } from "../../runtimes/utils";

export function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

// One run can span multiple models (one task_usage row per model); fold the
// rows back into per-run entries for the breakdown list.
interface RunEntry {
  taskId: string;
  createdAt: string;
  commentTriggered: boolean;
  tokens: number;
  cost: number;
}

function foldRuns(tasks: IssueTaskUsage[]): RunEntry[] {
  const byTask = new Map<string, RunEntry>();
  for (const t of tasks) {
    const entry = byTask.get(t.task_id) ?? {
      taskId: t.task_id,
      createdAt: t.created_at,
      commentTriggered: t.comment_triggered,
      tokens: 0,
      cost: 0,
    };
    entry.tokens +=
      t.input_tokens + t.output_tokens + t.cache_read_tokens + t.cache_write_tokens;
    entry.cost += estimateCost(t);
    byTask.set(t.task_id, entry);
  }
  return [...byTask.values()];
}

function formatCost(cost: number): string {
  return cost >= 0.01 ? `$${cost.toFixed(2)}` : `$${cost.toFixed(4)}`;
}

export function IssueTokenUsageSection({ usage }: { usage: IssueUsageSummary }) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const [runsOpen, setRunsOpen] = useState(false);

  if (usage.task_count === 0) return null;

  const runs = foldRuns(usage.tasks);
  const totalCost = runs.reduce((sum, r) => sum + r.cost, 0);

  return (
    <div>
      <button
        type="button"
        className={`flex w-full items-center gap-1 rounded-md px-2 py-1 text-xs font-medium transition-colors mb-2 hover:bg-accent/70 ${open ? "" : "text-muted-foreground hover:text-foreground"}`}
        onClick={() => setOpen(!open)}
      >
        {t(($) => $.detail.section_token_usage)}
        <ChevronRight className={`!size-3 shrink-0 stroke-[2.5] text-muted-foreground transition-transform ${open ? "rotate-90" : ""}`} />
      </button>
      {open && (
        <div className="grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 pl-2">
          {totalCost > 0 && (
            <PropRow label={t(($) => $.detail.prop_est_cost)}>
              <span className="text-muted-foreground">{formatCost(totalCost)}</span>
            </PropRow>
          )}
          <PropRow label={t(($) => $.detail.prop_input)}>
            <span className="text-muted-foreground">{formatTokenCount(usage.total_input_tokens)}</span>
          </PropRow>
          <PropRow label={t(($) => $.detail.prop_output)}>
            <span className="text-muted-foreground">{formatTokenCount(usage.total_output_tokens)}</span>
          </PropRow>
          {(usage.total_cache_read_tokens > 0 || usage.total_cache_write_tokens > 0) && (
            <div className="contents" title={t(($) => $.detail.prop_cache_tooltip)}>
              <PropRow label={t(($) => $.detail.prop_cache)}>
                <span className="text-muted-foreground">
                  {t(($) => $.detail.prop_cache_value, {
                    read: formatTokenCount(usage.total_cache_read_tokens),
                    write: formatTokenCount(usage.total_cache_write_tokens),
                  })}
                </span>
              </PropRow>
            </div>
          )}
          {runs.length > 0 ? (
            <>
              <button
                type="button"
                className="col-span-2 flex items-center gap-1 text-left text-xs text-muted-foreground hover:text-foreground"
                onClick={() => setRunsOpen(!runsOpen)}
              >
                {t(($) => $.detail.usage_runs_toggle, { count: usage.task_count })}
                <ChevronRight className={`!size-3 shrink-0 transition-transform ${runsOpen ? "rotate-90" : ""}`} />
              </button>
              {runsOpen &&
                runs.map((run) => (
                  <PropRow
                    key={run.taskId}
                    label={
                      run.commentTriggered
                        ? t(($) => $.detail.usage_run_comment)
                        : t(($) => $.detail.usage_run_assignment)
                    }
                  >
                    <span className="truncate text-muted-foreground">
                      {formatTokenCount(run.tokens)}
                      {run.cost > 0 && ` · ${formatCost(run.cost)}`}
                    </span>
                  </PropRow>
                ))}
            </>
          ) : (
            <PropRow label={t(($) => $.detail.prop_runs)}>
              <span className="text-muted-foreground">{usage.task_count}</span>
            </PropRow>
          )}
        </div>
      )}
    </div>
  );
}
```

Notes for the implementer:
- `estimateCost` accepts `{ model, provider?, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens }` (`packages/views/runtimes/utils.ts:455-470`); `IssueTaskUsage` satisfies it structurally.
- Unpriced/unknown models make `estimateCost` return 0 — the cost row simply hides, tokens still show. This is why the old `prop_runs` row is kept as the no-rows fallback (old backend responses parse to `tasks: []` via the Task 2 schema defaults).
- If `PropRow`'s `label` prop does not accept a plain string here, match how `issue-detail.tsx:1442` passes labels.

- [ ] **Step 5: Wire it into `issue-detail.tsx`**

In `packages/views/issues/components/issue-detail.tsx`:
1. Delete the `formatTokenCount` function (lines 287-291).
2. Delete the `const [tokenUsageOpen, setTokenUsageOpen] = useState(true);` line (754).
3. Replace the whole `{/* Token usage */}` block (lines 1649-1683) with:

```tsx
{/* Token usage */}
{usage && <IssueTokenUsageSection usage={usage} />}
```

4. Add the import next to the other sibling component imports:

```tsx
import { IssueTokenUsageSection } from "./issue-token-usage-section";
```

5. If `ChevronRight` or `PropRow` imports become unused in `issue-detail.tsx`, they won't be (both are used elsewhere in the file) — leave them.

- [ ] **Step 6: Run the tests and typecheck**

```bash
pnpm --filter @multica/views test -- issue-token-usage-section
pnpm typecheck
pnpm --filter @multica/views test
```

Expected: new tests PASS, typecheck clean, no regressions in the views suite.

- [ ] **Step 7: Commit**

```bash
git add packages/views/issues/components/issue-token-usage-section.tsx \
        packages/views/issues/components/issue-token-usage-section.test.tsx \
        packages/views/issues/components/issue-detail.tsx \
        packages/views/locales/en/issues.json packages/views/locales/zh-Hans/issues.json \
        packages/views/locales/ja/issues.json packages/views/locales/ko/issues.json
git commit -m "feat(views): cost estimate, per-run breakdown, cache tooltip in issue token usage

Extracts the sidebar token-usage section into its own tested component.
Cost reuses the dashboard's estimateCost pricing; per-run rows label
comment vs assignment triggers so users can see which cycle burned
tokens."
```
