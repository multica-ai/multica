import type {
  DashboardUsageDaily,
  DashboardUsageByAgent,
  DashboardAgentRunTime,
  DashboardRunTimeDaily,
  DashboardFailureDaily,
  DashboardFailureByAgent,
} from "@multica/core/types";
import {
  FAILURE_CLASSES,
  failureClassOf,
  type FailureClass,
} from "@multica/core/dashboard";
import {
  addDaysIso,
  estimateCost,
  estimateCostBreakdown,
  formatShortDate,
  todayIso,
  weekStartIso,
  type DailyTokenData,
} from "../runtimes/utils";
import type {
  DailyTimeData,
  DailyTasksData,
  WeeklyTimeData,
  WeeklyTasksData,
  DailyErrorsData,
  WeeklyErrorsData,
  FailureBucketTotals,
  FailureClassCounts,
} from "../runtimes/components/charts";

// ---------------------------------------------------------------------------
// Dashboard data aggregations
//
// The workspace dashboard returns the same per-(date, model) and
// per-(agent, model) shapes the runtime page does, so cost math reuses
// `estimateCost` / `estimateCostBreakdown` from the runtimes utils. What
// the runtimes view does with `aggregateByDate` (works on RuntimeUsage,
// which carries a `provider` field) we replicate here with a tighter
// type — fewer optional fields, less conditional logic on the consumer
// side.
// ---------------------------------------------------------------------------

// Mirrors `DailyCostStackData` in the runtimes utils, including its cache-read
// segment — the dashboard feeds the very same DailyCostChart, so a category
// missing here is a category missing from the chart's total (MUL-6334).
export interface DailyCostStack {
  date: string;
  label: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  total: number;
}

function formatDateLabel(d: string): string {
  // Anchor to local midnight so the formatted label matches the bucket the
  // server picked (which is already in workspace time). Pasting the raw
  // date as the body of `new Date()` would interpret it as UTC and shift
  // by the user's offset.
  const date = new Date(d + "T00:00:00");
  return `${date.getMonth() + 1}/${date.getDate()}`;
}

// Per-(date, model) rows → 1 row per date with cost broken into the four
// segments the stacked bar chart consumes. Stable sort by date asc so the
// chart x-axis is left-to-right oldest-to-newest.
export function aggregateDailyCost(usage: DashboardUsageDaily[]): DailyCostStack[] {
  const map = new Map<
    string,
    { input: number; output: number; cacheRead: number; cacheWrite: number }
  >();
  for (const u of usage) {
    const b = estimateCostBreakdown(u);
    const entry = map.get(u.date) ?? {
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
    };
    entry.input += b.input;
    entry.output += b.output;
    entry.cacheRead += b.cacheRead;
    entry.cacheWrite += b.cacheWrite;
    map.set(u.date, entry);
  }
  const round = (n: number) => Math.round(n * 100) / 100;
  return Array.from(map.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, s]) => {
      const input = round(s.input);
      const output = round(s.output);
      const cacheRead = round(s.cacheRead);
      const cacheWrite = round(s.cacheWrite);
      return {
        date,
        label: formatDateLabel(date),
        input,
        output,
        cacheRead,
        cacheWrite,
        total: round(input + output + cacheRead + cacheWrite),
      };
    });
}

// Per-(date, model) rows → 1 row per date with raw token counts split
// across the four chart segments. Independent of pricing — unmapped
// models still contribute here, even if they're excluded from cost.
// Mirrors `aggregateByDate(...).dailyTokens` from the runtimes utils so
// the Tokens chart on the Usage page consumes the same shape as the one
// on the runtime-detail page.
export function aggregateDailyTokens(usage: DashboardUsageDaily[]): DailyTokenData[] {
  const map = new Map<
    string,
    { input: number; output: number; cacheRead: number; cacheWrite: number }
  >();
  for (const u of usage) {
    const entry = map.get(u.date) ?? {
      input: 0,
      output: 0,
      cacheRead: 0,
      cacheWrite: 0,
    };
    entry.input += u.input_tokens;
    entry.output += u.output_tokens;
    entry.cacheRead += u.cache_read_tokens;
    entry.cacheWrite += u.cache_write_tokens;
    map.set(u.date, entry);
  }
  return Array.from(map.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, t]) => ({
      date,
      label: formatDateLabel(date),
      input: t.input,
      output: t.output,
      cacheRead: t.cacheRead,
      cacheWrite: t.cacheWrite,
    }));
}

export interface DashboardTokenTotals {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cost: number;
  taskCount: number;
}

// Whole-window totals for the KPI tiles. taskCount sums DISTINCT task counts
// per row — these are already collapsed server-side per (date, model), so
// the value can over-count if the same task has tokens in two days; that's
// acceptable for a KPI ("rough volume") and the per-agent run-time card
// gives the precise figure.
export function computeDailyTotals(usage: DashboardUsageDaily[]): DashboardTokenTotals {
  return usage.reduce<DashboardTokenTotals>(
    (acc, u) => ({
      input: acc.input + u.input_tokens,
      output: acc.output + u.output_tokens,
      cacheRead: acc.cacheRead + u.cache_read_tokens,
      cacheWrite: acc.cacheWrite + u.cache_write_tokens,
      cost: acc.cost + estimateCost(u),
      taskCount: acc.taskCount + u.task_count,
    }),
    { input: 0, output: 0, cacheRead: 0, cacheWrite: 0, cost: 0, taskCount: 0 },
  );
}

export interface AgentCostRow {
  agentId: string;
  tokens: number;
  cost: number;
  taskCount: number;
}

// Fold per-(agent, model) rows into one row per agent. Cost is the sum
// across this agent's models, which is the figure the user cares about.
// Sort by cost desc so the heaviest spender lands first.
export function aggregateAgentTokens(rows: DashboardUsageByAgent[]): AgentCostRow[] {
  const map = new Map<string, AgentCostRow>();
  for (const r of rows) {
    const entry = map.get(r.agent_id) ?? {
      agentId: r.agent_id,
      tokens: 0,
      cost: 0,
      taskCount: 0,
    };
    entry.tokens +=
      r.input_tokens + r.output_tokens + r.cache_read_tokens + r.cache_write_tokens;
    entry.cost += estimateCost(r);
    entry.taskCount += r.task_count;
    map.set(r.agent_id, entry);
  }
  return Array.from(map.values()).toSorted((a, b) => b.cost - a.cost);
}

export interface AgentDashboardRow {
  agentId: string;
  tokens: number;
  cost: number;
  seconds: number;
  taskCount: number;
}

// Merge per-agent token totals with per-agent run-time totals into one
// row per agent.
//
// taskCount comes from `runTimeRows` when available — that rollup is a
// true per-agent distinct count (`COUNT(*)` on (agent, terminal-task) in
// SQL). The token rollup's per-(agent, model) counts double-count a task
// when it spans multiple models, so we only fall back to it for agents
// with no terminal run yet (in-flight tasks reported tokens but haven't
// completed). Sorted by cost desc, then run time desc.
export function mergeAgentDashboardRows(
  tokenRows: AgentCostRow[],
  runTimeRows: DashboardAgentRunTime[],
): AgentDashboardRow[] {
  const runTimeByAgent = new Map(
    runTimeRows.map((r) => [r.agent_id, r] as const),
  );
  const merged = new Map<string, AgentDashboardRow>();
  for (const r of tokenRows) {
    const rt = runTimeByAgent.get(r.agentId);
    merged.set(r.agentId, {
      agentId: r.agentId,
      tokens: r.tokens,
      cost: r.cost,
      seconds: rt?.total_seconds ?? 0,
      taskCount: rt ? rt.task_count : r.taskCount,
    });
  }
  // Agents with run-time rows but zero tokens still belong on the list
  // (a task that errored before producing usage). Their token columns
  // stay at 0.
  for (const r of runTimeRows) {
    if (merged.has(r.agent_id)) continue;
    merged.set(r.agent_id, {
      agentId: r.agent_id,
      tokens: 0,
      cost: 0,
      seconds: r.total_seconds,
      taskCount: r.task_count,
    });
  }
  return Array.from(merged.values()).toSorted((a, b) => {
    if (b.cost !== a.cost) return b.cost - a.cost;
    return b.seconds - a.seconds;
  });
}

// Synthetic agentId for the row that aggregates all hard-deleted agents.
// Sentinel (not a real UUID) so the component can detect it and render a
// placeholder instead of looking the id up in the agent list.
export const DELETED_AGENTS_ROW_ID = "__deleted_agents__";

// Synthetic agentId the SERVER sends for the bucket aggregating every agent it
// refuses to name (MUL-5409): agents the viewer may not see, plus the hidden
// system carriers behind agent-builder sessions, which no client can resolve to
// a name for anyone. Mirrors `restrictedAgentsRowID` in
// server/internal/handler/dashboard.go — the two strings must stay in sync.
//
// Distinct from DELETED_AGENTS_ROW_ID on purpose: those agents are gone, these
// are alive and still running. Labelling them "Deleted agents" told the user
// something false, which is why the bucket renders as a neutral "Other agents".
export const RESTRICTED_AGENTS_ROW_ID = "__restricted_agents__";

// Fold usage rows whose agent no longer exists in the workspace into a single
// aggregated "Deleted agents" row instead of dropping them. The agent list is
// fetched with `include_archived: true`, so archived agents keep their names
// and stay on the leaderboard as themselves; only hard-deleted agents fall out
// of `knownAgentIds` and collapse into the bucket.
//
// MUL-3771 (PR #4637) originally *dropped* these rows so they'd stop rendering
// as a bare UUID — but the top-line Cost/Tokens KPIs still count their spend
// (those totals aggregate `task_usage_hourly` without joining `agent`), so the
// per-agent breakdown no longer reconciled with the totals (MUL-3776, #4640).
// Aggregating instead of dropping keeps `sum(visible rows) == KPI total` while
// still never exposing a UUID. The bucket carries tokens + cost only; seconds
// and taskCount stay 0 because the run-time rollups inner-join `agent`, so
// deleted agents already contribute nothing to the Time/Tasks KPIs — the
// component renders those two columns as "—" for this row.
//
// `knownAgentIds` is `null` while the agent list is still loading; callers
// pass `null` in that case so the rows pass through untouched instead of the
// whole leaderboard collapsing into one bucket on a slow fetch.
//
// The server's restricted bucket is NOT in `knownAgentIds` either (it is not an
// agent), so it is passed through explicitly rather than swept into the deleted
// bucket. It also keeps its seconds / taskCount: unlike a hard-deleted agent it
// really did run, and the run-time rollup folds those numbers into it.
export function bucketUnknownAgentRows(
  rows: AgentDashboardRow[],
  knownAgentIds: ReadonlySet<string> | null,
): AgentDashboardRow[] {
  if (!knownAgentIds) return rows;
  const known: AgentDashboardRow[] = [];
  const bucket: AgentDashboardRow = {
    agentId: DELETED_AGENTS_ROW_ID,
    tokens: 0,
    cost: 0,
    seconds: 0,
    taskCount: 0,
  };
  let hasDeleted = false;
  for (const r of rows) {
    if (knownAgentIds.has(r.agentId) || r.agentId === RESTRICTED_AGENTS_ROW_ID) {
      known.push(r);
      continue;
    }
    hasDeleted = true;
    bucket.tokens += r.tokens;
    bucket.cost += r.cost;
  }
  return hasDeleted ? [...known, bucket] : known;
}

// Rows the leaderboard renders as a synthetic bucket rather than an agent.
// `deletedAgentCount` and the caption's agent count both have to exclude these,
// or the card claims more agents (or more deletions) than it is showing.
export function isSyntheticAgentRow(agentId: string): boolean {
  return agentId === DELETED_AGENTS_ROW_ID || agentId === RESTRICTED_AGENTS_ROW_ID;
}

// ---------------------------------------------------------------------------
// Weekly fold for run-time + tasks. Mirrors `aggregateByWeek` in
// `runtimes/utils.ts` which already covers cost / tokens — same calendar
// week semantics (Mon–Sun anchored at today-in-tz), same pre-zeroed buckets,
// same partial-week metadata. Workspace dashboard uses the user-chosen
// timezone here; the runtime page uses the runtime's IANA tz. Behaviour is
// identical apart from where the tz comes from.
// ---------------------------------------------------------------------------

interface WeekShell {
  weekStart: string;
  weekEnd: string;
  label: string;
  rangeLabel: string;
  partial: boolean;
  daysCovered: number;
}

// Build N trailing calendar week shells anchored at today-in-tz. Each shell
// carries the labels and partial-week metadata the chart components consume;
// downstream aggregators fold their own per-week values onto the matching
// shell.
function buildWeekShells(tz: string, weekCount: number): WeekShell[] {
  const count = Math.max(1, Math.floor(weekCount));
  const today = todayIso(tz);
  const currentWeekStart = weekStartIso(today);
  const firstWeekStart = addDaysIso(currentWeekStart, -(count - 1) * 7);
  const shells: WeekShell[] = [];
  for (let i = 0; i < count; i++) {
    const weekStart = addDaysIso(firstWeekStart, i * 7);
    const weekEnd = addDaysIso(weekStart, 6);
    const partial = today < weekEnd;
    // Inclusive count of how many days of this week have actually elapsed.
    // Closed weeks sit at 7; the current week reports 1..6.
    const clampedToday =
      today < weekStart ? weekStart : today < weekEnd ? today : weekEnd;
    const elapsed = Math.min(7, Math.max(1, diffDaysIso(weekStart, clampedToday) + 1));
    shells.push({
      weekStart,
      weekEnd,
      label: formatShortDate(weekStart),
      rangeLabel: `${formatShortDate(weekStart)} – ${formatShortDate(weekEnd)}`,
      partial,
      daysCovered: partial ? elapsed : 7,
    });
  }
  return shells;
}

function diffDaysIso(from: string, to: string): number {
  const [y1, m1, d1] = from.split("-").map(Number);
  const [y2, m2, d2] = to.split("-").map(Number);
  const a = Date.UTC(y1 ?? 1970, (m1 ?? 1) - 1, d1 ?? 1);
  const b = Date.UTC(y2 ?? 1970, (m2 ?? 1) - 1, d2 ?? 1);
  return Math.round((b - a) / 86_400_000);
}

export function aggregateWeeklyTime(
  rows: DashboardRunTimeDaily[],
  tz: string,
  weekCount: number,
): WeeklyTimeData[] {
  const shells = buildWeekShells(tz, weekCount);
  const totals = new Map<string, number>();
  for (const shell of shells) totals.set(shell.weekStart, 0);
  for (const r of rows) {
    const wkStart = weekStartIso(r.date);
    if (!totals.has(wkStart)) continue;
    totals.set(wkStart, (totals.get(wkStart) ?? 0) + r.total_seconds);
  }
  return shells.map((s) => ({ ...s, totalSeconds: totals.get(s.weekStart) ?? 0 }));
}

export function aggregateWeeklyTasks(
  rows: DashboardRunTimeDaily[],
  tz: string,
  weekCount: number,
): WeeklyTasksData[] {
  const shells = buildWeekShells(tz, weekCount);
  const buckets = new Map<
    string,
    { completed: number; failed: number; cancelled: number }
  >();
  for (const shell of shells)
    buckets.set(shell.weekStart, { completed: 0, failed: 0, cancelled: 0 });
  for (const r of rows) {
    const wkStart = weekStartIso(r.date);
    const bucket = buckets.get(wkStart);
    if (!bucket) continue;
    const failed = r.failed_count;
    const cancelled = r.cancelled_count;
    const completed = Math.max(0, r.task_count - failed - cancelled);
    bucket.completed += completed;
    bucket.failed += failed;
    bucket.cancelled += cancelled;
  }
  return shells.map((s) => {
    const b =
      buckets.get(s.weekStart) ?? { completed: 0, failed: 0, cancelled: 0 };
    return {
      ...s,
      completed: b.completed,
      failed: b.failed,
      cancelled: b.cancelled,
    };
  });
}

// Per-date run-time rows → one row per date with `totalSeconds` for the
// DailyTimeChart. Sorted ascending so the x-axis reads oldest-to-newest,
// matching the cost / tokens aggregators.
export function aggregateDailyTime(rows: DashboardRunTimeDaily[]): DailyTimeData[] {
  return rows.toSorted((a, b) => a.date.localeCompare(b.date))
    .map((r) => ({
      date: r.date,
      label: formatDateLabel(r.date),
      totalSeconds: r.total_seconds,
    }));
}

// Per-date run-time rows → one row per date with `completed`, `failed` and
// `cancelled` counts for the DailyTasksChart's stacked bar. failed_count and
// cancelled_count are disjoint subsets of task_count, so the succeeded count
// is the remainder. Subtracting cancelled matters: without it a run the user
// stopped would render in the green "completed" segment.
export function aggregateDailyTasks(rows: DashboardRunTimeDaily[]): DailyTasksData[] {
  return rows.toSorted((a, b) => a.date.localeCompare(b.date))
    .map((r) => {
      const failed = r.failed_count;
      const cancelled = r.cancelled_count;
      const completed = Math.max(0, r.task_count - failed - cancelled);
      return {
        date: r.date,
        label: formatDateLabel(r.date),
        completed,
        failed,
        cancelled,
      };
    });
}

// Compact human duration: "1h 23m" / "12m 30s" / "45s" / "<1m". Used for
// the dashboard run-time KPI and the per-agent run-time column. Keeps two
// segments max — three segments adds visual noise without precision the
// dashboard actually needs.
export function formatDuration(seconds: number, lessThanMinuteLabel: string): string {
  if (seconds < 0 || !Number.isFinite(seconds)) return lessThanMinuteLabel;
  if (seconds < 60) {
    if (seconds < 1) return lessThanMinuteLabel;
    return `${Math.round(seconds)}s`;
  }
  const totalMinutes = Math.floor(seconds / 60);
  const hours = Math.floor(totalMinutes / 60);
  const mins = totalMinutes % 60;
  if (hours === 0) {
    const secs = Math.floor(seconds) % 60;
    return secs > 0 ? `${mins}m ${secs}s` : `${mins}m`;
  }
  if (hours >= 24) {
    const days = Math.floor(hours / 24);
    const h = hours % 24;
    return h > 0 ? `${days}d ${h}h` : `${days}d`;
  }
  return mins > 0 ? `${hours}h ${mins}m` : `${hours}h`;
}

// ---------------------------------------------------------------------------
// Failure aggregations
//
// The two failure rollups ship every terminal task, with `failure_reason: ""`
// marking the succeeded bucket. Keeping successes in the same payload is what
// lets these helpers produce an error *rate* whose numerator and denominator
// come from identical filters — the run-time rollups can't serve as the
// denominator because they require `started_at IS NOT NULL` and a task that
// expired in the queue never started.
//
// Everything here folds raw reasons into the seven display classes from
// `@multica/core/dashboard`; the raw reason survives only in
// `aggregateFailureReasons`, which powers the detail rows under the class
// summary.
// ---------------------------------------------------------------------------

function emptyClassCounts(): FailureClassCounts {
  return Object.fromEntries(
    FAILURE_CLASSES.map((c) => [c, 0]),
  ) as FailureClassCounts;
}

// Fold one rollup row into a mutable accumulator. `failure_reason: ""` is the
// succeeded bucket: it moves `total` only, never `failed` or a class.
function foldFailureRow(
  acc: FailureClassCounts & FailureBucketTotals,
  reason: string,
  count: number,
): void {
  acc.total += count;
  if (reason === "") return;
  acc.failed += count;
  acc[failureClassOf(reason)] += count;
}

// Per-(date, reason) rows → one row per date with per-class failure counts
// and the day's failed / total totals. Sorted date asc to match the other
// daily aggregators.
export function aggregateDailyErrors(
  rows: DashboardFailureDaily[],
): DailyErrorsData[] {
  const map = new Map<string, FailureClassCounts & FailureBucketTotals>();
  for (const r of rows) {
    let entry = map.get(r.date);
    if (!entry) {
      entry = { ...emptyClassCounts(), failed: 0, total: 0 };
      map.set(r.date, entry);
    }
    foldFailureRow(entry, r.failure_reason, r.task_count);
  }
  return Array.from(map.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, counts]) => ({
      ...counts,
      date,
      label: formatDateLabel(date),
    }));
}

// Weekly counterpart. Buckets are pre-zeroed from the same week shells the
// time / tasks weekly aggregators use, so a week with no terminal tasks
// renders as an empty bar instead of collapsing the x-axis.
export function aggregateWeeklyErrors(
  rows: DashboardFailureDaily[],
  tz: string,
  weekCount: number,
): WeeklyErrorsData[] {
  const shells = buildWeekShells(tz, weekCount);
  const buckets = new Map<string, FailureClassCounts & FailureBucketTotals>();
  for (const shell of shells) {
    buckets.set(shell.weekStart, { ...emptyClassCounts(), failed: 0, total: 0 });
  }
  for (const r of rows) {
    const bucket = buckets.get(weekStartIso(r.date));
    if (!bucket) continue;
    foldFailureRow(bucket, r.failure_reason, r.task_count);
  }
  return shells.map((s) => ({
    ...(buckets.get(s.weekStart) ?? { ...emptyClassCounts(), failed: 0, total: 0 }),
    ...s,
  }));
}

// Whole-window failure totals for the Errors KPI hint and the breakdown
// header. `rate` is a fraction in [0, 1]; 0 when the window has no terminal
// tasks at all.
export interface FailureTotals {
  failed: number;
  total: number;
  rate: number;
}

export function computeFailureTotals(
  rows: { failure_reason: string; task_count: number }[],
): FailureTotals {
  let failed = 0;
  let total = 0;
  for (const r of rows) {
    total += r.task_count;
    if (r.failure_reason !== "") failed += r.task_count;
  }
  return { failed, total, rate: total > 0 ? failed / total : 0 };
}

export interface FailureClassRow {
  failureClass: FailureClass;
  count: number;
}

// Per-class window totals, heaviest first, zero-count classes dropped. Ties
// break on FAILURE_CLASSES order so the list doesn't reshuffle between
// renders when two classes sit at the same count.
export function aggregateFailureClasses(
  rows: { failure_reason: string; task_count: number }[],
): FailureClassRow[] {
  const counts = emptyClassCounts();
  for (const r of rows) {
    if (r.failure_reason === "") continue;
    counts[failureClassOf(r.failure_reason)] += r.task_count;
  }
  return FAILURE_CLASSES.map((failureClass) => ({
    failureClass,
    count: counts[failureClass],
  }))
    .filter((r) => r.count > 0)
    .toSorted((a, b) => b.count - a.count);
}

export interface FailureReasonRow {
  reason: string;
  failureClass: FailureClass;
  count: number;
}

// Per-raw-reason window totals, heaviest first. This is the row set that
// answers "which specific error", under the coarser class summary.
export function aggregateFailureReasons(
  rows: { failure_reason: string; task_count: number }[],
): FailureReasonRow[] {
  const counts = new Map<string, number>();
  for (const r of rows) {
    if (r.failure_reason === "") continue;
    counts.set(r.failure_reason, (counts.get(r.failure_reason) ?? 0) + r.task_count);
  }
  return Array.from(counts.entries())
    .map(([reason, count]) => ({
      reason,
      failureClass: failureClassOf(reason),
      count,
    }))
    .toSorted((a, b) => b.count - a.count || a.reason.localeCompare(b.reason));
}

// Synthetic agentId for the row aggregating every agent the viewer can't
// resolve to a name. Distinct from DELETED_AGENTS_ROW_ID because this bucket
// covers two populations at once: hard-deleted agents, and the server's
// already-anonymized restricted bucket. Naming it after deletion would be a
// lie for the second group, so the Errors card labels it neutrally
// ("Other agents"), which is honest for both.
export const UNRESOLVED_AGENTS_ROW_ID = "__unresolved_agents__";

export interface AgentFailureRow {
  agentId: string;
  failed: number;
  total: number;
  rate: number;
  // Full per-class split of this agent's failures, which the offender row
  // draws as a stacked bar. Carries the whole composition rather than just
  // the heaviest class: "fails one way" and "fails five ways" are different
  // problems, and a single dominant-class label collapsed them into the same
  // row. Every class is present (0 when unused) so the bar can be built
  // without existence checks.
  classes: FailureClassCounts;
}

// Per-agent failure totals, worst first. Default order is absolute failure
// count: an agent with 1/1 failed is a 100% rate but is rarely the thing an
// operator should look at before the agent that failed 40 times. The rate
// rides along on the row, and `sortAgentFailures` can re-rank on it.
//
// Agents with zero failures are dropped — this list is a triage aid, not a
// census; the leaderboard above it already shows every agent.
export function aggregateAgentFailures(
  rows: DashboardFailureByAgent[],
): AgentFailureRow[] {
  const map = new Map<
    string,
    { failed: number; total: number; classes: FailureClassCounts }
  >();
  for (const r of rows) {
    let entry = map.get(r.agent_id);
    if (!entry) {
      entry = { failed: 0, total: 0, classes: emptyClassCounts() };
      map.set(r.agent_id, entry);
    }
    entry.total += r.task_count;
    if (r.failure_reason === "") continue;
    entry.failed += r.task_count;
    entry.classes[failureClassOf(r.failure_reason)] += r.task_count;
  }
  return sortAgentFailures(
    Array.from(map.entries())
      .filter(([, v]) => v.failed > 0)
      .map(([agentId, v]) => ({
        agentId,
        failed: v.failed,
        total: v.total,
        rate: v.total > 0 ? v.failed / v.total : 0,
        classes: v.classes,
      })),
    "failed",
  );
}

// Which metric ranks the offender list, and therefore how long its bars are.
// The two used to disagree: the list ranked on absolute failures while the
// most prominent number on the row was the rate, so the bar looked like it
// measured a percentage it had nothing to do with. Mirrors LeaderboardSort's
// contract — sort metric, bar length and the emphasised column move together.
export type OffenderSort = "failed" | "rate";

export const OFFENDER_METRIC: Record<
  OffenderSort,
  (r: AgentFailureRow) => number
> = {
  failed: (r) => r.failed,
  rate: (r) => r.rate,
};

// Minimum terminal runs before an agent's failure rate is allowed to compete
// on the Rate ranking. One run that failed is a 100% rate, and without a floor
// that row wins outright and buries every agent worth looking at.
//
// Small-sample rows are demoted, NOT hidden: this list has to keep reconciling
// with the workspace failure count above it, and an agent that failed its only
// two runs is still a real thing an operator may want to see.
export const MIN_RATE_SAMPLE = 10;

export function hasRateSample(row: AgentFailureRow): boolean {
  return row.total >= MIN_RATE_SAMPLE;
}

// Re-rank the offender rows for the selected metric. Ties break on the other
// metric so an equal-valued bucket keeps a stable, meaningful order instead of
// reshuffling on every render.
export function sortAgentFailures(
  rows: AgentFailureRow[],
  sortBy: OffenderSort,
): AgentFailureRow[] {
  if (sortBy === "failed") {
    return rows.toSorted((a, b) => b.failed - a.failed || b.rate - a.rate);
  }
  const sample = (r: AgentFailureRow) => (hasRateSample(r) ? 0 : 1);
  return rows.toSorted(
    (a, b) => sample(a) - sample(b) || b.rate - a.rate || b.failed - a.failed,
  );
}

// Fold rows whose agent the viewer cannot resolve into one aggregated bucket
// so the Errors list never renders a bare agent UUID.
//
// The privacy boundary itself now lives on the server: GetDashboardFailuresByAgent
// folds agents the caller may not view onto RESTRICTED_AGENTS_ROW_ID before
// serializing (MUL-5409), because client-side filtering is decoration — one
// curl bypasses it. This function stays as the display-side backstop: it still
// owns hard-deleted agents, whose ids the server has no reason to hide but
// which resolve to no name, and it re-anonymizes the server's bucket into the
// same neutral row so the card has one "not an agent you can open" case.
//
// `knownAgentIds` is null while the agent list is still loading. Unlike
// `bucketUnknownAgentRows`, which passes rows through in that window, this
// one anonymizes them: a transient flash of UUIDs is exactly the leak the
// function exists to prevent, and one merged row for a few hundred
// milliseconds is the cheaper failure.
//
// This rewrites the RAW per-(agent, reason) rows rather than merging the
// aggregated ones, so the bucket is just another agent_id by the time
// `aggregateAgentFailures` runs: its totals, rate, class split and rank all
// come out of the same code path as every other row. Merging aggregated rows
// would mean re-deriving `total` and `rate` by hand at the merge site — a
// second, easily-skewed copy of arithmetic that already exists once.
export function anonymizeUnresolvedAgentRows(
  rows: DashboardFailureByAgent[],
  knownAgentIds: ReadonlySet<string> | null,
): DashboardFailureByAgent[] {
  if (!rows.some((r) => !knownAgentIds?.has(r.agent_id))) return rows;
  return rows.map((r) =>
    knownAgentIds?.has(r.agent_id)
      ? r
      : { ...r, agent_id: UNRESOLVED_AGENTS_ROW_ID },
  );
}
