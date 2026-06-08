# M4: Personal Execution Loop — Implementation Plan

## Overview

Add nightly review draft generation, next-day plan draft generation, and Pomodoro timer to complete the personal execution loop. All features are user-scoped (no shared object modification). LLM generation is optional (falls back to template if no API key).

---

## Phase 1: Database Migrations

### Task 1.1 — Migration 038: daily_review table

**File:** `server/migrations/038_daily_review.up.sql`

```sql
CREATE TABLE daily_review (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL,
    review_date DATE NOT NULL,
    draft_content TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'confirmed')),
    confirmed_at TIMESTAMPTZ,
    generated_by TEXT NOT NULL DEFAULT 'manual' CHECK (generated_by IN ('manual', 'scheduled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, user_id, review_date)
);

CREATE INDEX idx_daily_review_user ON daily_review (workspace_id, user_id, review_date DESC);
```

**File:** `server/migrations/038_daily_review.down.sql`

```sql
DROP TABLE IF EXISTS daily_review;
```

### Task 1.2 — Migration 039: daily_plan table

**File:** `server/migrations/039_daily_plan.up.sql`

```sql
CREATE TABLE daily_plan (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL,
    plan_date DATE NOT NULL,
    draft_content TEXT NOT NULL DEFAULT '',
    top_issue_ids UUID[] NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'confirmed')),
    confirmed_at TIMESTAMPTZ,
    generated_by TEXT NOT NULL DEFAULT 'manual' CHECK (generated_by IN ('manual', 'scheduled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, user_id, plan_date)
);

CREATE INDEX idx_daily_plan_user ON daily_plan (workspace_id, user_id, plan_date DESC);
```

**File:** `server/migrations/039_daily_plan.down.sql`

```sql
DROP TABLE IF EXISTS daily_plan;
```

---

## Phase 2: sqlc Queries

### Task 2.1 — daily_review SQL

**File:** `server/pkg/db/queries/daily_review.sql`

Queries:
- `UpsertDailyReview` — INSERT ... ON CONFLICT (workspace_id, user_id, review_date) DO UPDATE
- `GetDailyReviewByDate` — SELECT by workspace_id + user_id + review_date
- `GetDailyReviewByID` — SELECT by id + workspace_id
- `ListDailyReviews` — SELECT by workspace_id + user_id, ORDER BY review_date DESC, LIMIT
- `ConfirmDailyReview` — UPDATE status='confirmed', confirmed_at=now()

### Task 2.2 — daily_plan SQL

**File:** `server/pkg/db/queries/daily_plan.sql`

Queries:
- `UpsertDailyPlan` — INSERT ... ON CONFLICT DO UPDATE
- `GetDailyPlanByDate` — SELECT by workspace_id + user_id + plan_date
- `GetDailyPlanByID` — SELECT by id + workspace_id
- `ListDailyPlans` — SELECT by workspace_id + user_id, ORDER BY plan_date DESC, LIMIT
- `ConfirmDailyPlan` — UPDATE status='confirmed', confirmed_at=now()

### Task 2.3 — Regenerate sqlc

```bash
make sqlc
```

---

## Phase 3: LLM Client

### Task 3.1

**File:** `server/internal/llm/client.go`

```go
package llm

// LLMClient wraps the Anthropic Messages API.
// Falls back to template generation when ANTHROPIC_API_KEY is not set.
type LLMClient struct {
    apiKey  string
    baseURL string
    timeout time.Duration
}

func NewClient() *LLMClient
func (c *LLMClient) Generate(ctx context.Context, prompt string) (string, error)
func (c *LLMClient) IsConfigured() bool
```

Implementation: plain `net/http` POST to `https://api.anthropic.com/v1/messages` with headers `x-api-key`, `anthropic-version: 2023-06-01`, `content-type: application/json`. No external SDK.

---

## Phase 4: Backend Services

### Task 4.1

**File:** `server/internal/service/review.go`

```go
type ReviewService struct {
    q   *db.Queries
    db  *pgxpool.Pool
    llm *llm.LLMClient
}

func (s *ReviewService) GenerateReviewDraft(ctx, workspaceID, userID, date) (*db.DailyReview, error)
func (s *ReviewService) ConfirmReview(ctx, workspaceID, userID, reviewID) (*db.DailyReview, error)
func (s *ReviewService) GetTodayReview(ctx, workspaceID, userID) (*db.DailyReview, error)
func (s *ReviewService) ListReviews(ctx, workspaceID, userID, limit) ([]db.DailyReview, error)
func (s *ReviewService) buildReviewPrompt(entries []db.TimeEntry, issues []db.Issue, date) string
```

### Task 4.2

**File:** `server/internal/service/daily_plan.go`

```go
type DailyPlanService struct {
    q   *db.Queries
    db  *pgxpool.Pool
    llm *llm.LLMClient
}

func (s *DailyPlanService) GeneratePlanDraft(ctx, workspaceID, userID, date) (*db.DailyPlan, error)
func (s *DailyPlanService) ConfirmPlan(ctx, workspaceID, userID, planID) (*db.DailyPlan, error)
func (s *DailyPlanService) GetTomorrowPlan(ctx, workspaceID, userID) (*db.DailyPlan, error)
func (s *DailyPlanService) ListPlans(ctx, workspaceID, userID, limit) ([]db.DailyPlan, error)
func (s *DailyPlanService) buildPlanPrompt(issues []db.Issue, review *db.DailyReview, date) string
```

---

## Phase 5: Scheduler

### Task 5.1

**File:** `server/internal/scheduler/nightly.go`

```go
type Config struct {
    ReviewHour int // default: 22 (10 PM)
    PlanHour   int // default: 7  (7 AM)
}

type Scheduler struct {
    reviewSvc *service.ReviewService
    planSvc   *service.DailyPlanService
    q         *db.Queries
    cfg       Config
}

func New(reviewSvc, planSvc, q, cfg) *Scheduler
func (s *Scheduler) Start(ctx context.Context)            // launch goroutines
func (s *Scheduler) runReviewCycle(ctx)                   // generate for all active users
func (s *Scheduler) runPlanCycle(ctx)
func (s *Scheduler) nextFireTime(hour int) time.Duration  // time until next occurrence
```

Pattern: same goroutine+timer approach as `server/cmd/server/runtime_sweeper.go`.

---

## Phase 6: HTTP Handlers

### Task 6.1

**File:** `server/internal/handler/daily_review.go`

```go
type DailyReviewHandler struct {
    reviewSvc *service.ReviewService
}

func (h *DailyReviewHandler) GenerateReview(w, r)   // POST /api/daily-reviews/generate
func (h *DailyReviewHandler) GetTodayReview(w, r)   // GET  /api/daily-reviews/today
func (h *DailyReviewHandler) ListReviews(w, r)       // GET  /api/daily-reviews
func (h *DailyReviewHandler) ConfirmReview(w, r)     // POST /api/daily-reviews/{id}/confirm
```

### Task 6.2

**File:** `server/internal/handler/daily_plan.go`

```go
type DailyPlanHandler struct {
    planSvc *service.DailyPlanService
}

func (h *DailyPlanHandler) GeneratePlan(w, r)        // POST /api/daily-plans/generate
func (h *DailyPlanHandler) GetTomorrowPlan(w, r)     // GET  /api/daily-plans/tomorrow
func (h *DailyPlanHandler) ListPlans(w, r)           // GET  /api/daily-plans
func (h *DailyPlanHandler) ConfirmPlan(w, r)         // POST /api/daily-plans/{id}/confirm
```

### Task 6.3 — Register routes

**File:** `server/cmd/server/router.go`

Add under protected routes:
```go
r.Post("/api/daily-reviews/generate", dailyReviewHandler.GenerateReview)
r.Get("/api/daily-reviews/today", dailyReviewHandler.GetTodayReview)
r.Get("/api/daily-reviews", dailyReviewHandler.ListReviews)
r.Post("/api/daily-reviews/{id}/confirm", dailyReviewHandler.ConfirmReview)

r.Post("/api/daily-plans/generate", dailyPlanHandler.GeneratePlan)
r.Get("/api/daily-plans/tomorrow", dailyPlanHandler.GetTomorrowPlan)
r.Get("/api/daily-plans", dailyPlanHandler.ListPlans)
r.Post("/api/daily-plans/{id}/confirm", dailyPlanHandler.ConfirmPlan)
```

### Task 6.4 — Wire up in main.go

**File:** `server/cmd/server/main.go`

- Initialize `llm.NewClient()`
- Initialize `ReviewService` and `DailyPlanService`
- Initialize handlers
- Start `scheduler.New(...).Start(ctx)`

---

## Phase 7: Frontend — API Client

### Task 7.1

**File:** `apps/workspace/src/shared/api/client.ts`

Add methods:
```typescript
// Daily Review
generateDailyReview(): Promise<DailyReview>
getTodayReview(): Promise<DailyReview | null>
listDailyReviews(limit?: number): Promise<DailyReview[]>
confirmDailyReview(id: string): Promise<DailyReview>

// Daily Plan
generateDailyPlan(planDate?: string): Promise<DailyPlan>
getTomorrowPlan(): Promise<DailyPlan | null>
listDailyPlans(limit?: number): Promise<DailyPlan[]>
confirmDailyPlan(id: string): Promise<DailyPlan>
```

---

## Phase 8: Frontend — Daily Review Feature

### Task 8.1 — Types

**File:** `apps/workspace/src/features/daily-review/types.ts`

```typescript
interface DailyReview {
  id: string;
  workspace_id: string;
  user_id: string;
  review_date: string;         // "YYYY-MM-DD"
  draft_content: string;       // Markdown
  status: "draft" | "confirmed";
  confirmed_at: string | null;
  generated_by: "manual" | "scheduled";
  created_at: string;
  updated_at: string;
}
```

### Task 8.2 — Hooks

**File:** `apps/workspace/src/features/daily-review/hooks/use-daily-review.ts`

```typescript
useTodayReviewQuery()
useDailyReviewsQuery(limit?)
useGenerateReviewMutation()  // POST generate, invalidates today query
useConfirmReviewMutation()   // POST confirm, updates status in cache
```

### Task 8.3 — Components

**File:** `apps/workspace/src/features/daily-review/components/DailyReviewPanel.tsx`

States:
- No review: "生成今日复盘" button
- Generating: spinner + "AI 正在生成复盘草稿..."
- Draft: Markdown content + "确认复盘" button + "重新生成" button
- Confirmed: Markdown content (read-only) + "已确认 ✓ {date}" badge

**File:** `apps/workspace/src/features/daily-review/components/ReviewMarkdownView.tsx`

Simple Markdown renderer (use existing `ReactMarkdown` if available, else `<pre>` fallback).

---

## Phase 9: Frontend — Daily Plan Feature

### Task 9.1 — Types

**File:** `apps/workspace/src/features/daily-plan/types.ts`

```typescript
interface DailyPlan {
  id: string;
  workspace_id: string;
  user_id: string;
  plan_date: string;           // "YYYY-MM-DD"
  draft_content: string;       // Markdown
  top_issue_ids: string[];     // UUID[]
  status: "draft" | "confirmed";
  confirmed_at: string | null;
  generated_by: "manual" | "scheduled";
  created_at: string;
  updated_at: string;
}
```

### Task 9.2 — Hooks

**File:** `apps/workspace/src/features/daily-plan/hooks/use-daily-plan.ts`

```typescript
useTomorrowPlanQuery()
useDailyPlansQuery(limit?)
useGeneratePlanMutation()
useConfirmPlanMutation()
```

### Task 9.3 — Components

**File:** `apps/workspace/src/features/daily-plan/components/DailyPlanPanel.tsx`

States:
- No plan: "生成明日计划" button
- Generating: spinner
- Draft: Markdown content + TopThreeIssues + "确认计划" button + "重新生成"
- Confirmed: read-only + "已确认 ✓" badge

**File:** `apps/workspace/src/features/daily-plan/components/TopThreeIssues.tsx`

Renders `top_issue_ids` fetched from issue store/query. Shows issue title + priority icon + status.

---

## Phase 10: Frontend — Pomodoro Timer

### Task 10.1

**File:** `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx`

```typescript
interface PomodoroTimerProps {
  issueId?: string;  // optional issue to link worklog to
  onComplete: () => void;
}
```

State machine:
- idle → running (25 min countdown) → break (5 min) → idle
- Pause/resume supported
- On complete: call `useStartTimerMutation()` + `useCreateWorklogMutation()` or direct API
- Shows: 🍅 count for current day, minutes remaining, issue name if linked

### Task 10.2 — GlobalTimerWidget update

**File:** `apps/workspace/src/features/time-tracking/components/GlobalTimerWidget.tsx`

Add toggle between "时间记录模式" and "🍅 番茄模式". When Pomodoro mode active, render PomodoroTimer instead of standard controls.

---

## Phase 11: Frontend — MyTimePage Integration

### Task 11.1

**File:** `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`

Add at top of page, below date header:

```tsx
<div className="grid grid-cols-2 gap-4 mb-6">
  <DailyReviewPanel />
  <DailyPlanPanel />
</div>
```

Import from respective feature directories.

---

## File Change Summary

### New Files

| File | Purpose |
|---|---|
| `server/migrations/038_daily_review.up.sql` | daily_review table |
| `server/migrations/038_daily_review.down.sql` | rollback |
| `server/migrations/039_daily_plan.up.sql` | daily_plan table |
| `server/migrations/039_daily_plan.down.sql` | rollback |
| `server/pkg/db/queries/daily_review.sql` | sqlc queries |
| `server/pkg/db/queries/daily_plan.sql` | sqlc queries |
| `server/pkg/db/generated/daily_review.sql.go` | generated (via sqlc) |
| `server/pkg/db/generated/daily_plan.sql.go` | generated (via sqlc) |
| `server/internal/llm/client.go` | Anthropic HTTP client |
| `server/internal/service/review.go` | review generation logic |
| `server/internal/service/daily_plan.go` | plan generation logic |
| `server/internal/scheduler/nightly.go` | goroutine scheduler |
| `server/internal/handler/daily_review.go` | HTTP handlers |
| `server/internal/handler/daily_plan.go` | HTTP handlers |
| `apps/workspace/src/features/daily-review/` | complete feature directory |
| `apps/workspace/src/features/daily-plan/` | complete feature directory |
| `apps/workspace/src/features/time-tracking/components/PomodoroTimer.tsx` | Pomodoro UI |

### Modified Files

| File | Change |
|---|---|
| `server/cmd/server/router.go` | Register 8 new routes |
| `server/cmd/server/main.go` | Wire up services + scheduler |
| `server/pkg/db/generated/` | Regenerated (sqlc) |
| `apps/workspace/src/shared/api/client.ts` | 8 new API methods |
| `apps/workspace/src/features/time-tracking/components/GlobalTimerWidget.tsx` | Add Pomodoro mode toggle |
| `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` | Add review + plan panels |

---

## Environment Variables

| Var | Purpose | Default |
|---|---|---|
| `ANTHROPIC_API_KEY` | LLM generation API key | (optional, fallback to template) |
| `REVIEW_HOUR` | Hour (0-23) to run nightly review | `22` |
| `PLAN_HOUR` | Hour (0-23) to run morning plan | `7` |

---

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| No ANTHROPIC_API_KEY | Template-based fallback; clear error in UI |
| Scheduler misfire on restart | Check for today's missing review on startup |
| LLM call timeout | 30s HTTP timeout; loading state in UI; retry button |
| sqlc regeneration breaks existing code | Run `make sqlc` and verify compilation before merging |
| Pomodoro creates duplicate worklog | Check for existing worklog same day+issue before insert |

---

STATUS: WAITING FOR USER APPROVAL
