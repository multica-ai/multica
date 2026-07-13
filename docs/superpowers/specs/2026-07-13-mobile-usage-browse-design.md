# Mobile Usage Browse (More page) — Design

Date: 2026-07-13
Status: Approved

## Context

This is round 3 of a 4-round effort to bring suitable desktop-only sidebar
sections onto mobile's More page (round 1, Skills, and round 2, Runtimes,
already shipped — `docs/superpowers/specs/2026-07-08-mobile-skills-browse-design.md`,
`docs/superpowers/specs/2026-07-09-mobile-runtimes-browse-design.md`).
Round 4 (Agents) is a separate future spec.

"Usage" turns out to name two unrelated surfaces in this codebase:

- **(A) Per-runtime `UsageSection`** (`packages/views/runtimes/components/usage-section.tsx`) —
  a chart-heavy block mounted inside a single runtime's detail page. No
  route of its own; only reachable via Configure → Runtimes → pick a
  runtime → scroll past the Hero card.
- **(B) The workspace-level "Usage" nav page** — `DashboardPage`
  (`packages/views/dashboard/`, component name is legacy, the feature and
  its i18n namespace are both "usage"), routed at `/{slug}/usage` on both
  web and desktop, with its own top-level sidebar entry alongside
  Issues/Projects/Agents/Squads.

This spec covers **(B) only**. It is the thing actually labeled "Usage" in
product nav, has its own route, and is what a workspace member checking
"how much are we spending" would reach for. (A) has no nav entry on
desktop either and is out of scope for this round.

Both surfaces share the same client-side cost-estimation logic
(`packages/views/runtimes/utils.ts` — the server only returns raw token
counts, never dollar amounts) but mobile cannot import from
`packages/views` (outside its `@multica/core` type/pure-function
whitelist), so that logic must be mirrored, not imported — the same
"mirror the design, not the import" rule already applied to mobile's
realtime updaters and Runtimes' health/update-check helpers.

Mobile has no charting library installed today (confirmed: no
`*chart*` file anywhere in `apps/mobile`, no charting package in
`package.json`). This round adds one — see Changes §2.

## Goal

Add a read-only workspace Usage page to mobile's More list: project
filter, daily/weekly + period controls, 4 KPI cards (Cost/Tokens/Run
time/Tasks), a trend chart toggling between those same 4 metrics, and a
per-agent leaderboard sortable by the same 4 metrics — mirroring
`DashboardPage`'s single-page layout and exact numbers.

## Non-goals

- Per-runtime `UsageSection` (surface A) — no nav entry even on desktop;
  separate future work if ever wanted.
- The desktop-only custom-pricing override (`useCustomPricingStore`, a
  per-browser Zustand store with no server sync) — mobile has no settings
  surface for it. Unpriced/custom-priced models fall back to the static
  `MODEL_PRICING` table estimate on mobile even where a desktop user has
  configured an override. Low-stakes, cosmetic-only divergence (dollar
  estimates already carry a "these are estimates" framing on both
  platforms).
- Role-based gating — none exists on web/desktop (workspace membership
  only, confirmed in both `dashboard.go` handlers and the sidebar nav
  rendering); mobile matches, no gating added.
- Drill-down / tap-through from a leaderboard row to an agent detail
  screen — round 4 (Agents) hasn't shipped yet, matching the same
  constraint Runtimes' "agents on this runtime" list already accepted.
- Export or download of usage data (not present on web/desktop either).

## Changes

### 1. Data layer — `apps/mobile/data/queries/usage.ts`

Mirror the 4 endpoints from `packages/core/dashboard/queries.ts` (all
accept `?days=&project_id=&tz=`):

- `GET /api/dashboard/usage/daily` → `DashboardUsageDaily[]`
  (`{date, provider, model, input_tokens, output_tokens,
  cache_read_tokens, cache_write_tokens, task_count}`)
- `GET /api/dashboard/usage/by-agent` → `DashboardUsageByAgent[]` (same
  shape, keyed by `agent_id` instead of `date`)
- `GET /api/dashboard/agent-runtime` → `DashboardAgentRunTime[]`
  (`{agent_id, total_seconds, task_count, failed_count}`)
- `GET /api/dashboard/runtime/daily` → `DashboardRunTimeDaily[]`
  (`{date, total_seconds, task_count, failed_count}`)

All four types are already defined in `@multica/core/types` (confirmed —
`packages/core/types/agent.ts`) and importable directly as `import type`,
on mobile's whitelist. New `usageKeys` factory (3-segment shape,
workspace-scoped, matching every other mobile query factory):

```ts
export const usageKeys = {
  all: (wsId: string | null) => ["usage", wsId] as const,
  daily: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "daily", days, projectId, tz] as const,
  byAgent: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "by-agent", days, projectId, tz] as const,
  agentRuntime: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "agent-runtime", days, projectId, tz] as const,
  runtimeDaily: (wsId: string | null, days: number, projectId: string | null, tz: string) =>
    [...usageKeys.all(wsId), "runtime-daily", days, projectId, tz] as const,
};
```

Each read method on `apps/mobile/data/api.ts` uses `fetchValidated`
against the matching zod schema already exported from
`packages/core/api/schemas.ts` (pure exports, on the sharing whitelist).

### 2. Display logic — two new `apps/mobile/lib/` files (highest-risk work this round)

Confirmed by directly reading both source files during plan-writing:
`DashboardPage` draws its pricing primitives from `packages/views/runtimes/utils.ts`
but its actual aggregation functions from a **second, sibling file**,
`packages/views/dashboard/utils.ts` — not from `runtimes/utils.ts`'s own
`aggregateByDate`. The mobile port mirrors this same two-file split:

**`apps/mobile/lib/usage-pricing.ts`** mirrors the pricing primitives from
`packages/views/runtimes/utils.ts`: `MODEL_PRICING` table (~40 rows,
USD/1M tokens), `estimateCost` / `resolvePricing` / `canonicalCandidates`
— the 4 model-id normalization tolerances (strip `provider/` prefix,
Anthropic dot↔dash, strip dated snapshot suffix, strip trailing `[1m]`
tag), applied in the same order — plus `formatTokens` and a small
`fmtMoney` formatter (mirrored from `dashboard-page.tsx` itself, where it
is a private, unexported helper).

**`apps/mobile/lib/usage-display.ts`** mirrors the dashboard-specific
aggregation from `packages/views/dashboard/utils.ts`, plus the
shared week/date helpers from `runtimes/utils.ts` that both surfaces
reuse:

- `aggregateDailyCost`, `aggregateDailyTokens`, `computeDailyTotals` — one
  row per date; cost math calls into `usage-pricing.ts`'s
  `estimateCost`/`estimateCostBreakdown`.
- `aggregateAgentTokens`, `mergeAgentDashboardRows` — per-agent token
  rollup merged with the per-agent run-time rollup; `task_count` prefers
  the run-time rollup (the token rollup double-counts a task that spans
  2 models).
- `bucketUnknownAgentRows` / `DELETED_AGENTS_ROW_ID` — folds any per-agent
  row whose agent has been hard-deleted into one synthetic "Deleted
  agents" row so the leaderboard sum reconciles with the KPI total. This
  is the exact class of hazard the mobile CLAUDE.md's inbox-dedup
  incident warns about.
- `aggregateByWeek` (shared, from `runtimes/utils.ts`), plus
  `aggregateWeeklyTime`/`aggregateWeeklyTasks` (dashboard-specific) — all
  three pre-zero trailing ISO (Monday-start) weeks so sparse weeks render
  as empty bars, not gaps (the MUL-2382 bug, previously fixed on web;
  mobile must not reintroduce it), current week marked `partial: true`,
  pure `YYYY-MM-DD` string math (no host-tz/DST drift).
- `aggregateDailyTime`/`aggregateDailyTasks` (dashboard-specific).
- `todayIso(tz)` / `addDaysIso` (shared, from `runtimes/utils.ts`) — the
  selected period's cutoff date is
  `addDaysIso(todayIso(viewTZ), -(days - 1))`, then daily/runtime rows
  are filtered to `date >= cutoff`, where `viewTZ` is the **viewing**
  timezone (see below), not device-local.
- `formatDuration` (dashboard's own `(seconds, lessThanMinuteLabel)`
  version — there are 3 unrelated functions named `formatDuration`
  elsewhere in `packages/views`; this is specifically the one
  `dashboard/utils.ts` exports, not `runtimes/utils.ts`'s or either of
  the other two).

**Confirmed NOT needed** (present in `runtimes/utils.ts` but used only by
the per-runtime `UsageSection`, surface A, never by `DashboardPage`) —
do not port these: `sliceWindow`, `aggregateByDate`, `estimateCacheSavings`,
`isModelPriced`/`modelGroupingKey`/`aggregateCostByAgent`/`aggregateCostByModel`/
`collectUnmappedModels` (the "cost by agent/model" tabs and the unmapped-
pricing banner are surface-A-only UI that `DashboardPage` doesn't have).

New `apps/mobile/lib/use-viewing-timezone.ts`, mirroring
`packages/views/common/use-viewing-timezone.ts`'s fallback chain: stored
`user.timezone` (from the shared `@multica/core` auth store type, already
on the whitelist) → device IANA timezone via `expo-localization`'s
`Localization.getCalendars()[0]?.timeZone` (the non-deprecated Expo API,
already used elsewhere in `apps/mobile/lib/i18n/`) → `"UTC"`.

### 3. Charting dependency

Add `react-native-gifted-charts` (+ `expo-linear-gradient`, an optional
peer dependency the library's own install instructions bundle) via
`pnpm exec expo install`. Verified before adding: peer deps on
react/react-native are unconstrained (`*`), no animation-library
dependency of its own (uses RN's built-in `Animated`, so no version
coupling with the existing `react-native-reanimated`), builds on
`react-native-svg` (already installed, matching version range), actively
maintained (latest `1.4.77`, published 2026-05-19).

### 4. Nav entry + screen — `more/usage.tsx` (new)

Add a "Usage" `NavRow` to the More page's "Workspace" `SectionGroup`
(`apps/mobile/app/(app)/[workspace]/(tabs)/more.tsx`), after Agents.
Pushes `more/usage`, a pushed Stack route registered in
`[workspace]/_layout.tsx` with a native header — same pattern as
`more/runtimes.tsx`.

Screen layout, top to bottom:

- **Project filter** — `useActionSheet()` (already wired for
  cross-platform one-of-N pickers per the mobile tech-stack baseline),
  options are "All projects" + the workspace's project list
  (`projectListOptions(wsId)`, already fetched elsewhere in the app).
- **Dimension (Daily/Weekly) + Period segmented controls** —
  `@react-native-segmented-control/segmented-control` (already
  installed). Legal periods per dimension, confirmed against
  `packages/views/dashboard/components/dashboard-page.tsx`'s
  `TIME_RANGES`/`DEFAULT_DAYS_BY_DIM`: daily allows 1/7/30/90d
  (default 30 on switch), weekly allows 30/90/180d (default 90 on
  switch) — switching dimension resets period only when the current
  value isn't legal in the new dimension. "1d" means the natural
  calendar day from 00:00 in the **viewing** timezone, not a rolling
  24-hour window — mirror this exactly, it's an easy place to
  accidentally diverge.
- **KPI row** — 2×2 grid of 4 new `components/usage/usage-stat-card.tsx`
  cards (Cost/Tokens/Run time/Tasks), matching `DashboardPage`'s exact
  value/hint content verbatim (confirmed by reading
  `packages/views/dashboard/components/dashboard-page.tsx`): Cost has no
  hint (plain `fmtMoney` total, no period-over-period delta — that delta
  only exists on surface A's per-runtime Cost card, not here); Tokens'
  hint is the input/output split; Run time's hint is the task count;
  Tasks' value AND Run time's hint both read `task_count` from the
  runtime rollup (not the token rollup, matching `mergeAgentDashboardRows`'s
  task-count preference), and Tasks' hint is the failed count. New domain
  component, 4 call sites within one screen, clears the "3 callers, no
  RNR/iOS-native alternative" threshold for a new primitive.
- **Trend chart** — `BarChart` from `react-native-gifted-charts`, a
  4-way metric toggle (Tokens/Cost/Time/Tasks) reusing the same
  segmented-control pattern. Exact per-metric stacking rules (which
  metrics stack multiple series vs render a single value per bucket) are
  not fully pinned down in this spec — the implementation plan must read
  `packages/views/dashboard/components/*.tsx`'s trend-chart
  sub-components directly and mirror their exact behavior per metric,
  not guess from this description.
- **Leaderboard** — `FlatList`, each row `ActorAvatar` + name + a
  horizontal bar (width scaled to the row max, same visual idea as
  desktop's bar-list) + the currently-selected metric's value. A
  horizontal row of "Sort: Tokens/Cost/Time/Tasks" chips above the list
  controls both the sort key and which value column is shown (mobile has
  no literal table columns to click, so sort-and-relabel via chip is the
  native-feeling equivalent). Includes the synthetic "Deleted agents" row
  from `bucketUnknownAgentRows` when applicable.

### 5. i18n

New `apps/mobile/locales/{en,zh-Hans}/usage.json` namespace, following the
established per-feature-namespace pattern. zh-Hans nav label "用量",
matching desktop's own precedent (`packages/views/locales/zh-Hans/usage.json`'s
`"title": "用量"`).

## Testing

Unlike Skills/Runtimes/Chat-nav (typecheck + lint + manual pass only, no
new test files), this round adds `apps/mobile/lib/usage-display.test.ts`.
Justification: `usage-display.ts` contains genuine non-trivial pure
functions (cost estimation, weekly aggregation with pre-zeroed buckets,
deleted-agent bucketing) where a silent arithmetic bug would show wrong
dollar amounts with no visible symptom — exactly the class of risk the
brainstorming skill's "design for isolation and testability" principle
and the mobile CLAUDE.md's behavioral-parity hazard framing both call
out. Port the same test vectors as
`packages/views/runtimes/utils.test.ts` where the logic is identical.

## Verification

1. `pnpm --filter @multica/mobile typecheck`
2. `pnpm --filter @multica/mobile lint`
3. `pnpm --filter @multica/mobile test` (`usage-display.test.ts` +
   locale parity)
4. Manual: tap Usage row on More page → project filter / dimension /
   period controls behave and legal-period resets work → KPI cards show
   plausible numbers that match the same workspace's desktop Usage page
   for the same period/project selection → trend chart renders and
   correctly switches across all 4 metrics → leaderboard sorts correctly
   per chip and its per-agent totals sum to the visible KPI total →
   "Deleted agents" row appears only when the workspace actually has
   activity from a hard-deleted agent → confirm both languages render
   correctly, including the zh-Hans "用量" nav label.
