/**
 * Workspace-Usage-page aggregation. Mirrors (does not import)
 * packages/views/dashboard/utils.ts, plus the shared date/week helpers
 * from packages/views/runtimes/utils.ts that both surfaces reuse
 * (aggregateByWeek, todayIso, addDaysIso, weekStartIso, formatShortDate).
 * Behavioral parity requires this stay numerically identical to those
 * files; when either changes on web, sync this file.
 *
 * Deliberately NOT ported (out of scope — see the design spec's
 * Non-goals): sliceWindow, aggregateByDate, estimateCacheSavings,
 * isModelPriced/modelGroupingKey/aggregateCostByAgent/aggregateCostByModel
 * — all of these are used only by the per-runtime UsageSection (surface
 * A), never by DashboardPage. Do not add them here without re-verifying
 * they're actually needed.
 */
import type {
  DashboardUsageDaily,
  DashboardUsageByAgent,
  DashboardAgentRunTime,
  DashboardRunTimeDaily,
} from "@multica/core/types";
import { estimateCost, estimateCostBreakdown } from "./usage-pricing";

// ---------------------------------------------------------------------------
// Calendar helpers — all date math runs on YYYY-MM-DD strings in the
// viewing timezone. Pure string/UTC math so DST transitions never shift
// a result by an hour into a neighbouring day.
// ---------------------------------------------------------------------------

export function todayIso(tz: string): string {
  return new Intl.DateTimeFormat("en-CA", {
    timeZone: tz,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(new Date());
}

export function addDaysIso(iso: string, days: number): string {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  dt.setUTCDate(dt.getUTCDate() + days);
  return dt.toISOString().slice(0, 10);
}

// Monday-of-week as YYYY-MM-DD (ISO 8601 week-start).
function weekStartIso(iso: string): string {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  const day = dt.getUTCDay(); // 0 = Sun, 1 = Mon, ..., 6 = Sat
  const offset = (day + 6) % 7; // distance back to Monday
  dt.setUTCDate(dt.getUTCDate() - offset);
  return dt.toISOString().slice(0, 10);
}

// "May 12" — short month/day for a YYYY-MM-DD string.
function formatShortDate(iso: string): string {
  const [y, m, d] = iso.split("-").map(Number);
  const dt = new Date(Date.UTC(y ?? 1970, (m ?? 1) - 1, d ?? 1));
  return dt.toLocaleString("en", { month: "short", day: "numeric", timeZone: "UTC" });
}

function diffDaysIso(from: string, to: string): number {
  const [y1, m1, d1] = from.split("-").map(Number);
  const [y2, m2, d2] = to.split("-").map(Number);
  const a = Date.UTC(y1 ?? 1970, (m1 ?? 1) - 1, d1 ?? 1);
  const b = Date.UTC(y2 ?? 1970, (m2 ?? 1) - 1, d2 ?? 1);
  return Math.round((b - a) / 86_400_000);
}

function formatDateLabel(d: string): string {
  const date = new Date(d + "T00:00:00");
  return `${date.getMonth() + 1}/${date.getDate()}`;
}

// ---------------------------------------------------------------------------
// Daily aggregations
// ---------------------------------------------------------------------------

export interface DailyCostStack {
  date: string;
  label: string;
  input: number;
  output: number;
  cacheWrite: number;
  total: number;
}

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
      return { date, label: formatDateLabel(date), input, output, cacheWrite, total: round(input + output + cacheWrite) };
    });
}

export interface DailyTokenData {
  date: string;
  label: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export function aggregateDailyTokens(usage: DashboardUsageDaily[]): DailyTokenData[] {
  const map = new Map<string, { input: number; output: number; cacheRead: number; cacheWrite: number }>();
  for (const u of usage) {
    const entry = map.get(u.date) ?? { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 };
    entry.input += u.input_tokens;
    entry.output += u.output_tokens;
    entry.cacheRead += u.cache_read_tokens;
    entry.cacheWrite += u.cache_write_tokens;
    map.set(u.date, entry);
  }
  return Array.from(map.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([date, t]) => ({ date, label: formatDateLabel(date), ...t }));
}

export interface DailyTimeData {
  date: string;
  label: string;
  totalSeconds: number;
}

export function aggregateDailyTime(rows: DashboardRunTimeDaily[]): DailyTimeData[] {
  return rows
    .toSorted((a, b) => a.date.localeCompare(b.date))
    .map((r) => ({ date: r.date, label: formatDateLabel(r.date), totalSeconds: r.total_seconds }));
}

export interface DailyTasksData {
  date: string;
  label: string;
  completed: number;
  failed: number;
}

export function aggregateDailyTasks(rows: DashboardRunTimeDaily[]): DailyTasksData[] {
  return rows
    .toSorted((a, b) => a.date.localeCompare(b.date))
    .map((r) => {
      const failed = r.failed_count;
      const completed = Math.max(0, r.task_count - failed);
      return { date: r.date, label: formatDateLabel(r.date), completed, failed };
    });
}

export interface DashboardTokenTotals {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
  cost: number;
  taskCount: number;
}

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

// ---------------------------------------------------------------------------
// Per-agent aggregations
// ---------------------------------------------------------------------------

export interface AgentCostRow {
  agentId: string;
  tokens: number;
  cost: number;
  taskCount: number;
}

export function aggregateAgentTokens(rows: DashboardUsageByAgent[]): AgentCostRow[] {
  const map = new Map<string, AgentCostRow>();
  for (const r of rows) {
    const entry = map.get(r.agent_id) ?? { agentId: r.agent_id, tokens: 0, cost: 0, taskCount: 0 };
    entry.tokens += r.input_tokens + r.output_tokens + r.cache_read_tokens + r.cache_write_tokens;
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

// taskCount comes from runTimeRows when available (a true per-agent
// distinct count); the token rollup double-counts a task that spans
// multiple models, so it's only used as a fallback for agents with no
// terminal run yet. Sorted by cost desc, then run time desc.
export function mergeAgentDashboardRows(
  tokenRows: AgentCostRow[],
  runTimeRows: DashboardAgentRunTime[],
): AgentDashboardRow[] {
  const runTimeByAgent = new Map(runTimeRows.map((r) => [r.agent_id, r] as const));
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
  for (const r of runTimeRows) {
    if (merged.has(r.agent_id)) continue;
    merged.set(r.agent_id, { agentId: r.agent_id, tokens: 0, cost: 0, seconds: r.total_seconds, taskCount: r.task_count });
  }
  return Array.from(merged.values()).toSorted((a, b) => {
    if (b.cost !== a.cost) return b.cost - a.cost;
    return b.seconds - a.seconds;
  });
}

// Synthetic agentId for the row that aggregates all hard-deleted agents.
export const DELETED_AGENTS_ROW_ID = "__deleted_agents__";

// Fold usage rows whose agent no longer exists into one aggregated
// "Deleted agents" row instead of dropping them (dropping would make
// sum(visible rows) != KPI total). knownAgentIds is null while the agent
// list is still loading — pass rows through untouched in that case.
export function bucketUnknownAgentRows(
  rows: AgentDashboardRow[],
  knownAgentIds: ReadonlySet<string> | null,
): AgentDashboardRow[] {
  if (!knownAgentIds) return rows;
  const known: AgentDashboardRow[] = [];
  const bucket: AgentDashboardRow = { agentId: DELETED_AGENTS_ROW_ID, tokens: 0, cost: 0, seconds: 0, taskCount: 0 };
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
// Weekly aggregations
// ---------------------------------------------------------------------------

interface WeekShell {
  weekStart: string;
  weekEnd: string;
  label: string;
  rangeLabel: string;
  partial: boolean;
  daysCovered: number;
}

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
    const clampedToday = today < weekStart ? weekStart : today < weekEnd ? today : weekEnd;
    const elapsed = Math.min(7, Math.max(1, diffDaysIso(weekStart, clampedToday) + 1));
    shells.push({
      weekStart,
      weekEnd,
      label: formatShortDate(weekStart),
      rangeLabel: `${formatShortDate(weekStart)} - ${formatShortDate(weekEnd)}`,
      partial,
      daysCovered: partial ? elapsed : 7,
    });
  }
  return shells;
}

export interface WeeklyTimeData extends WeekShell {
  totalSeconds: number;
}

export function aggregateWeeklyTime(rows: DashboardRunTimeDaily[], tz: string, weekCount: number): WeeklyTimeData[] {
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

export interface WeeklyTasksData extends WeekShell {
  completed: number;
  failed: number;
}

export function aggregateWeeklyTasks(rows: DashboardRunTimeDaily[], tz: string, weekCount: number): WeeklyTasksData[] {
  const shells = buildWeekShells(tz, weekCount);
  const buckets = new Map<string, { completed: number; failed: number }>();
  for (const shell of shells) buckets.set(shell.weekStart, { completed: 0, failed: 0 });
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

type WeeklyAggregable = {
  date: string;
  model: string;
  provider?: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
};

export interface WeeklyTokenData extends WeekShell {
  input: number;
  output: number;
  cacheRead: number;
  cacheWrite: number;
}

export interface WeeklyCostStackData extends WeekShell {
  input: number;
  output: number;
  cacheWrite: number;
  total: number;
}

export function aggregateByWeek(
  usage: readonly WeeklyAggregable[],
  tz: string,
  weekCount: number,
): { weeklyTokens: WeeklyTokenData[]; weeklyCostStack: WeeklyCostStackData[] } {
  const count = Math.max(1, Math.floor(weekCount));
  const today = todayIso(tz);
  const currentWeekStart = weekStartIso(today);
  const firstWeekStart = addDaysIso(currentWeekStart, -(count - 1) * 7);

  type TokenAgg = { weekStart: string; input: number; output: number; cacheRead: number; cacheWrite: number };
  const tokenMap = new Map<string, TokenAgg>();
  const stackMap = new Map<string, { input: number; output: number; cacheWrite: number }>();

  for (let i = 0; i < count; i++) {
    const wkStart = addDaysIso(firstWeekStart, i * 7);
    tokenMap.set(wkStart, { weekStart: wkStart, input: 0, output: 0, cacheRead: 0, cacheWrite: 0 });
    stackMap.set(wkStart, { input: 0, output: 0, cacheWrite: 0 });
  }

  for (const u of usage) {
    const wkStart = weekStartIso(u.date);
    if (wkStart < firstWeekStart || wkStart > currentWeekStart) continue;
    const tokens = tokenMap.get(wkStart);
    if (!tokens) continue;
    tokens.input += u.input_tokens;
    tokens.output += u.output_tokens;
    tokens.cacheRead += u.cache_read_tokens;
    tokens.cacheWrite += u.cache_write_tokens;

    const breakdown = estimateCostBreakdown(u);
    const stack = stackMap.get(wkStart);
    if (!stack) continue;
    stack.input += breakdown.input;
    stack.output += breakdown.output;
    stack.cacheWrite += breakdown.cacheWrite;
  }

  const decorate = (weekStart: string): WeekShell => {
    const weekEnd = addDaysIso(weekStart, 6);
    const partial = today < weekEnd;
    const elapsedDays = Math.min(
      7,
      Math.max(1, diffDaysIso(weekStart, today < weekStart ? weekStart : today < weekEnd ? today : weekEnd) + 1),
    );
    return {
      weekStart,
      weekEnd,
      label: formatShortDate(weekStart),
      rangeLabel: `${formatShortDate(weekStart)} - ${formatShortDate(weekEnd)}`,
      partial,
      daysCovered: partial ? elapsedDays : 7,
    };
  };

  const weeklyTokens: WeeklyTokenData[] = Array.from(tokenMap.values())
    .toSorted((a, b) => a.weekStart.localeCompare(b.weekStart))
    .map((t) => ({ ...decorate(t.weekStart), input: t.input, output: t.output, cacheRead: t.cacheRead, cacheWrite: t.cacheWrite }));

  const weeklyCostStack: WeeklyCostStackData[] = Array.from(stackMap.entries())
    .toSorted(([a], [b]) => a.localeCompare(b))
    .map(([weekStart, s]) => {
      const round = (n: number) => Math.round(n * 100) / 100;
      const input = round(s.input);
      const output = round(s.output);
      const cacheWrite = round(s.cacheWrite);
      return { ...decorate(weekStart), input, output, cacheWrite, total: round(input + output + cacheWrite) };
    });

  return { weeklyTokens, weeklyCostStack };
}

// ---------------------------------------------------------------------------
// Formatting
// ---------------------------------------------------------------------------

// Compact human duration: "1h 23m" / "12m 30s" / "45s" / "<1m".
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
