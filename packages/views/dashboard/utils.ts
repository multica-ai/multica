import type {
  DashboardUsageDaily,
  DashboardUsageByAgent,
  DashboardAgentRunTime,
  DashboardRunTimeDaily,
} from "@multica/core/types";
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

export interface DailyCostStack {
  date: string;
  label: string;
  input: number;
  output: number;
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

// Per-(date, model) rows → 1 row per date with cost broken into the three
// segments the stacked bar chart consumes. Stable sort by date asc so the
// chart x-axis is left-to-right oldest-to-newest.
export function aggregateDailyCost(usage: DashboardUsageDaily[]): DailyCostStack[] {
  const map = new Map<string, { input: number; output: number; cacheWrite: number }>();
  for (const u of usage) {
    const b = estimateCostBreakdown(u);
    const entry = map.get(u.date) ?? { input: 0, output: 0, cacheWrite: 0 };
    entry.input += b.input;
    entry.output += b.output;
    entry.cacheWrite += b.cacheWrite;
    map.set(u.date, entry);
  }
  const round = (n: number) => Math.round(n * 100) / 100;
  return Array.from(map.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, s]) => {
      const input = round(s.input);
      const output = round(s.output);
      const cacheWrite = round(s.cacheWrite);
      return {
        date,
        label: formatDateLabel(date),
        input,
        output,
        cacheWrite,
        total: round(input + output + cacheWrite),
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
    if (knownAgentIds.has(r.agentId)) {
      known.push(r);
      continue;
    }
    hasDeleted = true;
    bucket.tokens += r.tokens;
    bucket.cost += r.cost;
  }
  return hasDeleted ? [...known, bucket] : known;
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
  const buckets = new Map<string, { completed: number; failed: number }>();
  for (const shell of shells)
    buckets.set(shell.weekStart, { completed: 0, failed: 0 });
  for (const r of rows) {
    const wkStart = weekStartIso(r.date);
    const bucket = buckets.get(wkStart);
    if (!bucket) continue;
    const failed = r.failed_count;
    const completed = Math.max(0, r.task_count - failed);
    bucket.completed += completed;
    bucket.failed += failed;
  }
  return shells.map((s) => {
    const b = buckets.get(s.weekStart) ?? { completed: 0, failed: 0 };
    return { ...s, completed: b.completed, failed: b.failed };
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

// Per-date run-time rows → one row per date with `completed` and `failed`
// counts for the DailyTasksChart's stacked bar (failed_count is a subset
// of task_count, so completed = task_count - failed_count).
export function aggregateDailyTasks(rows: DashboardRunTimeDaily[]): DailyTasksData[] {
  return rows.toSorted((a, b) => a.date.localeCompare(b.date))
    .map((r) => {
      const failed = r.failed_count;
      const completed = Math.max(0, r.task_count - failed);
      return {
        date: r.date,
        label: formatDateLabel(r.date),
        completed,
        failed,
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
