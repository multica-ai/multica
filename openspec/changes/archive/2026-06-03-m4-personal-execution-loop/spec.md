# M4: Personal Execution Loop — Spec

## Goal

Transform time tracking from a passive log ("what did I do?") into an active execution loop that supports nightly review, next-day planning, and focus sessions. The user needs fewer daily decisions, and each day has clear structure.

---

## Current State

| Capability | Exists |
|---|---|
| Live timer (start/stop) | ✅ |
| Manual time entry | ✅ |
| Calendar view (DnD) | ✅ |
| Issue-linked time tracking | ✅ |
| Worklog (type: manual/pomodoro schema) | ✅ (schema only, no Pomodoro UI) |
| ntfy push notification | ✅ |
| AI draft generation | ❌ |
| Nightly review | ❌ |
| Next-day plan | ❌ |
| Scheduled tasks | ❌ |
| Pomodoro UI | ❌ |

**Scheduler infrastructure:** Goroutine-based tickers exist in `server/internal/daemon/` and `server/cmd/server/runtime_sweeper.go`. No cron system.

**AI infrastructure:** `server/pkg/agent/` provides CLI-based Claude/Codex agent SDK (for code tasks). No direct LLM API client for text generation.

---

## Exit Criteria

1. Nightly review draft can be AI-generated (scheduled or manual trigger)
2. Next-day plan draft can be AI-generated (scheduled or manual trigger), includes Top 3 / Three Frogs / suggested order
3. At least two explicit confirmation points: confirm review draft, confirm plan draft
4. Auto-generation only creates personal drafts; never silently modifies shared objects
5. Pomodoro/focus support wired into time_entry or worklog system

---

## V1 Scope (INCLUDED)

### Feature 1: Nightly Review Draft

**Trigger:** Manual (user clicks "Generate Review") + scheduled (goroutine ticker at configurable hour, default 22:00).

**Data source:** Today's `time_entry` rows for the user + open/completed issues assigned to the user (issue title + status only).

**Storage:** New `daily_review` table (personal, per-user, per-date).

**State machine:** `draft` → `confirmed`. Confirmed means user has read and approved.

**Confirmed action:** Record `confirmed_at`; no side effects on shared objects.

**AI prompt strategy:**
- Summarize time blocks by project/issue
- Note completed vs. incomplete planned items
- Identify patterns (time sinks, gaps)
- Output: Markdown with sections: 今日完成、时间分布、遗留问题、简短反思

**LLM call:** Direct HTTP call to Anthropic Messages API using `ANTHROPIC_API_KEY` env var. Fallback: structured template if no key configured.

### Feature 2: Next-Day Plan Draft

**Trigger:** Manual (user clicks "Generate Plan") + scheduled (goroutine ticker at configurable hour, default 07:00).

**Data source:** 
- User's open issues filtered by: assignee=me, not done, ordered by priority + due date
- Yesterday's review confirmed content (if any)
- Recent time patterns (avg duration by issue category)

**Storage:** New `daily_plan` table (personal, per-user, per-date).

**State machine:** `draft` → `confirmed`.

**Confirmed action:** Record `confirmed_at`; no side effects on shared objects.

**Plan structure (AI output):**
- 🐸 Top 3 Issues (Three Frogs): most important/hardest items to tackle first
- 📋 Suggested daily schedule with time blocks
- ⏰ Estimated total focused work time

**LLM call:** Same Anthropic HTTP client.

### Feature 3: Pomodoro Timer

**UI:** New Pomodoro mode toggle in GlobalTimerWidget (25-minute countdown). Supports: Start, Pause, Skip, Cancel.

**On session complete:**
1. Auto-create `worklog` with `type='pomodoro'`, `duration_minutes=25`, linked to current issue (if any)
2. Auto-stop the running `time_entry` (if one is active for the same issue)
3. Optional: send ntfy notification "🍅 番茄完成！休息 5 分钟"

**Short break (5 min) and long break (15 min):** UI-only countdown, no backend action.

**Integration point:** Pomodoro count is visible in next-day plan generation context ("yesterday: 6 pomodoros on Issue X").

---

## V1 Scope (EXCLUDED)

| Feature | Reason |
|---|---|
| Scheduled plan pushed to issue status | Modifies shared objects — excluded by exit criteria |
| Cross-user team daily summary | Not a personal tool |
| Slack/email notification of plan | Beyond ntfy scope for V1 |
| Plan → issue creation | Scope creep |
| Custom prompt configuration | Too early |
| Self-hosted LLM | Infrastructure concern |
| Automatic Pomodoro break tracking | Low signal, adds complexity |

---

## Data Model

### `daily_review` table

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
```

### `daily_plan` table

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
```

---

## Backend Architecture

### New Package: `server/internal/llm/`

Simple HTTP client for Anthropic Messages API:
- `client.go`: `LLMClient` struct with `Generate(ctx, prompt) (string, error)`
- Uses `ANTHROPIC_API_KEY` env var
- Falls back to template output if key not set
- 30-second timeout, single-turn prompt only

### New Service: `server/internal/service/review.go`

`ReviewService`:
- `GenerateReviewDraft(ctx, workspaceID, userID, date) (*DailyReview, error)` — fetch today's time entries + assigned issues, build prompt, call LLM, upsert draft
- `ConfirmReview(ctx, workspaceID, userID, reviewID) error` — set status=confirmed
- `GetTodayReview(ctx, workspaceID, userID) (*DailyReview, error)`
- `ListReviews(ctx, workspaceID, userID, limit) ([]*DailyReview, error)`

### New Service: `server/internal/service/daily_plan.go`

`DailyPlanService`:
- `GeneratePlanDraft(ctx, workspaceID, userID, date) (*DailyPlan, error)` — fetch open issues by priority/due, recent time patterns, yesterday's review, build prompt, call LLM, upsert draft
- `ConfirmPlan(ctx, workspaceID, userID, planID) error` — set status=confirmed
- `GetTomorrowPlan(ctx, workspaceID, userID) (*DailyPlan, error)`
- `ListPlans(ctx, workspaceID, userID, limit) ([]*DailyPlan, error)`

### New Package: `server/internal/scheduler/`

`nightly.go`:
- `Scheduler` struct with configurable review hour (default 22) and plan hour (default 7)
- Uses goroutine + `time.NewTimer` pattern (same as runtime_sweeper)
- On trigger: iterate all workspaces → all active users → call ReviewService/DailyPlanService
- Graceful shutdown via context cancellation

### New Handler: `server/internal/handler/daily_review.go`

Routes:
- `POST /api/daily-reviews/generate` — manual trigger
- `GET /api/daily-reviews/today` — get today's review
- `GET /api/daily-reviews` — list reviews (limit=30)
- `POST /api/daily-reviews/{id}/confirm` — confirm draft

### New Handler: `server/internal/handler/daily_plan.go`

Routes:
- `POST /api/daily-plans/generate` — manual trigger
- `GET /api/daily-plans/tomorrow` — get tomorrow's plan
- `GET /api/daily-plans` — list plans (limit=30)
- `POST /api/daily-plans/{id}/confirm` — confirm draft

---

## Frontend Architecture

### New Feature: `apps/workspace/src/features/daily-review/`

```
features/daily-review/
├── index.ts
├── types.ts                         # DailyReview type
├── hooks/
│   └── use-daily-review.ts          # React Query hooks
└── components/
    ├── DailyReviewPanel.tsx          # Main panel (generate + view + confirm)
    └── ReviewMarkdownView.tsx        # Markdown render of draft_content
```

### New Feature: `apps/workspace/src/features/daily-plan/`

```
features/daily-plan/
├── index.ts
├── types.ts                         # DailyPlan type
├── hooks/
│   └── use-daily-plan.ts            # React Query hooks
└── components/
    ├── DailyPlanPanel.tsx            # Main panel (generate + view + confirm)
    ├── TopThreeIssues.tsx            # Renders top_issue_ids with issue details
    └── PlanMarkdownView.tsx          # Markdown render of draft_content
```

### Updated: `apps/workspace/src/features/time-tracking/`

New component:
- `components/PomodoroTimer.tsx` — 25-min countdown, start/pause/skip/cancel, auto-creates worklog on complete

Updated component:
- `components/GlobalTimerWidget.tsx` — add Pomodoro mode toggle (🍅 button)

### Updated: `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`

Add two panels at the top of the page:
- `DailyReviewPanel` (today's review status + generate/confirm)
- `DailyPlanPanel` (tomorrow's plan status + generate/confirm)

### Updated: `apps/workspace/src/shared/api/client.ts`

Add new API methods for daily review and daily plan endpoints.

---

## API Design

### Review Endpoints

```
POST /api/daily-reviews/generate
Body: {} (uses today's date and authenticated user)
Response: DailyReview

GET /api/daily-reviews/today
Response: DailyReview | null

GET /api/daily-reviews?limit=30
Response: DailyReview[]

POST /api/daily-reviews/{id}/confirm
Response: DailyReview
```

### Plan Endpoints

```
POST /api/daily-plans/generate
Body: { plan_date?: string } (default: tomorrow)
Response: DailyPlan

GET /api/daily-plans/tomorrow
Response: DailyPlan | null

GET /api/daily-plans?limit=30
Response: DailyPlan[]

POST /api/daily-plans/{id}/confirm
Response: DailyPlan
```

---

## LLM Prompt Templates

### Review Prompt (condensed)

```
You are a personal productivity assistant. Generate a nightly review in Chinese markdown.

Today: {date}
Time entries: {time_entry_summary}
Assigned issues completed today: {completed_issues}
Assigned issues still open: {open_issues}

Sections to include:
## 今日完成
## 时间分布 (top 3 time blocks)
## 遗留问题
## 简短反思 (1-2 sentences)

Keep it under 400 words. Be concrete, not generic.
```

### Plan Prompt (condensed)

```
You are a personal productivity assistant. Generate a next-day plan in Chinese markdown.

Tomorrow: {date}
Open issues assigned to me (by priority): {priority_issues}
Issues with due dates: {due_issues}
Yesterday's unfinished items: {unfinished_from_review}
Recent focus pattern: avg {avg_pomodoros} pomodoros/day

Output:
## 🐸 三只青蛙 (Top 3 most important/hardest — do these first)
## 📋 建议顺序 (numbered list with time estimates)
## ⏰ 预计专注时间

Keep it under 300 words. Be specific about issue titles.
```

---

## Confirmation UX

Both panels follow this state machine:

```
[No draft] → [Generate] → [Draft: showing AI content] → [Confirm] → [Confirmed ✓]
                                ↑
                           [Regenerate] (replaces draft, resets status)
```

- Confirmed state is read-only (no edit, only view)
- User can regenerate at any time (replaces existing draft)
- Confirmation timestamp shown in confirmed state

---

## Risks

| Risk | Mitigation |
|---|---|
| No ANTHROPIC_API_KEY configured | Template fallback; clear error message in UI |
| Server restart loses scheduled trigger | Scheduler recalculates next fire time on startup; at-startup check for missed triggers |
| LLM response not valid Markdown | Display raw text; no parsing assumed |
| LLM timeout (slow generation) | 30s timeout; show loading state; retry on failure |
| Too many users trigger at same time | Sequential processing with rate limit in scheduler |
| Pomodoro creates duplicate worklog | Deduplication via issue+user+date check before insert |
