"use client";

import { useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { EyeOff, Trash2 } from "lucide-react";
import { ActorAvatar } from "../../common/actor-avatar";
import { formatTokens } from "../../runtimes/utils";
import { useT } from "../../i18n";
import {
  DELETED_AGENTS_ROW_ID,
  aggregateModelRows,
  aggregateRuntimeRows,
  formatDuration,
  isSyntheticAgentRow,
  RESTRICTED_AGENTS_ROW_ID,
  type AgentDashboardRow,
  type ModelDashboardRow,
  type RuntimeDashboardRow,
} from "../utils";
import { Segmented } from "./dashboard-shared";
import "./leaderboard.css";

// What the leaderboard ranks: whole agents, individual models, or runtimes.
// Agent is the default because it is the scope the page has always shown.
type LeaderboardScope = "agent" | "model" | "runtime";

// Which metric ranks the leaderboard. Drives row order, progress bar
// width, and which column header is emphasised — keeping the three in
// lockstep so the user always sees what the ranking actually measures.
// All four toggles stay visible in every scope; a metric the current scope
// does not carry reads "—" in the rows and sorts at zero (natural zeroing,
// not hidden controls).
type LeaderboardSort = "tokens" | "cost" | "time" | "tasks";

type LeaderboardEntry =
  | AgentDashboardRow
  | ModelDashboardRow
  | RuntimeDashboardRow;

// One extractor per (scope, metric) so row order, the progress bar and the
// emphasised column can never disagree about what the ranking measures.
const SCOPE_SORT_METRIC: Record<
  LeaderboardScope,
  Record<LeaderboardSort, (r: LeaderboardEntry) => number>
> = {
  agent: {
    tokens: (r) => (r as AgentDashboardRow).tokens,
    cost: (r) => (r as AgentDashboardRow).cost,
    time: (r) => (r as AgentDashboardRow).seconds,
    tasks: (r) => (r as AgentDashboardRow).taskCount,
  },
  model: {
    tokens: (r) => (r as ModelDashboardRow).tokens,
    cost: (r) => (r as ModelDashboardRow).cost,
    time: () => 0,
    tasks: (r) => (r as ModelDashboardRow).taskCount,
  },
  runtime: {
    tokens: () => 0,
    cost: () => 0,
    time: (r) => (r as RuntimeDashboardRow).seconds,
    tasks: (r) => (r as RuntimeDashboardRow).taskCount,
  },
};

// How many rows the leaderboard ranks before collapsing the tail behind a
// toggle, mirroring the offender list's cap. A workspace with dozens of agents
// rendered every one of them, which pushed everything below it a full screen or
// more down the page (MUL-5388). Ten answers "who is spending the most" — the
// tail is reachable via the toggle.
const LEADERBOARD_LIMIT = 10;

// The flexible columns own the readability constraints they protect instead
// of deriving them from a detached total width. Agent keeps a 24px avatar, an
// 8px gap, and 128px of readable name; Progress keeps its 8px bar at a 12:1
// comparison length. `fit-content` lets those floors plus the fixed tracks,
// gaps, and row padding form the intrinsic scroll width while the fr tracks
// still expand on wider cards.
const LEADERBOARD_GRID_STYLE = {
  minWidth: "fit-content",
  gridTemplateColumns:
    "minmax(10rem, 1.6fr) minmax(6rem, 1fr) 5rem 5rem 5rem 4rem",
} satisfies CSSProperties;

const LEADERBOARD_GRID = "grid items-center gap-3";

export function Leaderboard({
  agentRows,
  agents,
  deletedAgentCount,
  byModelUsage,
  runtimeRunTime,
  runtimes,
  lessThanMinuteLabel,
}: {
  agentRows: AgentDashboardRow[];
  agents: { id: string; name: string }[];
  deletedAgentCount: number;
  byModelUsage: import("@multica/core/types").DashboardUsageByModel[];
  runtimeRunTime: import("@multica/core/types").DashboardRuntimeRunTime[];
  runtimes: { id: string; name: string }[];
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");
  const [scope, setScope] = useState<LeaderboardScope>("agent");
  const [sortBy, setSortBy] = useState<LeaderboardSort>("tokens");
  const [showAll, setShowAll] = useState(false);

  const modelRows = useMemo(() => aggregateModelRows(byModelUsage), [byModelUsage]);
  const runtimeRows = useMemo(
    () => aggregateRuntimeRows(runtimeRunTime),
    [runtimeRunTime],
  );

  const scopeOptions = useMemo(
    () => [
      { value: "agent" as const, label: t(($) => $.leaderboard.scope_agent) },
      { value: "model" as const, label: t(($) => $.leaderboard.scope_model) },
      { value: "runtime" as const, label: t(($) => $.leaderboard.scope_runtime) },
    ],
    [t],
  );

  const sortOptions = useMemo(
    () => [
      { value: "tokens" as const, label: t(($) => $.leaderboard.header_tokens) },
      { value: "cost" as const, label: t(($) => $.leaderboard.header_cost) },
      { value: "time" as const, label: t(($) => $.leaderboard.header_time) },
      { value: "tasks" as const, label: t(($) => $.leaderboard.header_tasks) },
    ],
    [t],
  );

  // Re-rank when the scope or the metric changes; keep each scope's input
  // untouched so upstream `mergeAgentDashboardRows`'s tiebreaker (run time desc)
  // still applies inside an equal-bucket.
  const sortedRows = useMemo(() => {
    const metric = SCOPE_SORT_METRIC[scope][sortBy];
    const base = scope === "agent" ? agentRows : scope === "model" ? modelRows : runtimeRows;
    return [...base].sort((a, b) => metric(b) - metric(a));
  }, [scope, sortBy, agentRows, modelRows, runtimeRows]);

  // Measured across every row, not just the visible ones, so a bar's width
  // means the same thing collapsed and expanded — the leader always fills the
  // track and nothing re-scales when the tail comes into view.
  const maxValue = useMemo(() => {
    const metric = SCOPE_SORT_METRIC[scope][sortBy];
    return sortedRows.reduce((m, r) => Math.max(m, metric(r)), 0);
  }, [sortedRows, scope, sortBy]);

  const visibleRows = showAll
    ? sortedRows
    : sortedRows.slice(0, LEADERBOARD_LIMIT);

  // "N agents" counts the rows that actually name an agent. Up to two of the
  // rows are synthetic buckets (deleted, restricted), and subtracting a fixed 1
  // reported one agent too many whenever both were present.
  const namedAgentCount = useMemo(
    () => agentRows.filter((r) => !isSyntheticAgentRow(r.agentId)).length,
    [agentRows],
  );

  const caption =
    scope === "agent"
      ? deletedAgentCount > 0
        ? t(($) => $.leaderboard.caption_with_deleted, {
            count: namedAgentCount,
            deleted: deletedAgentCount,
          })
        : t(($) => $.leaderboard.caption, { count: namedAgentCount })
      : scope === "model"
        ? t(($) => $.leaderboard.caption_models, { count: modelRows.length })
        : t(($) => $.leaderboard.caption_runtimes, { count: runtimeRows.length });

  const firstColHeader =
    scope === "agent"
      ? t(($) => $.leaderboard.header_agent)
      : scope === "model"
        ? t(($) => $.leaderboard.header_model)
        : t(($) => $.leaderboard.header_runtime);

  // Active column gets foreground text; others stay muted. Helps the user
  // see "this is what the bar is measuring" at a glance.
  const colClass = (key: LeaderboardSort) =>
    `text-right ${sortBy === key ? "text-foreground" : "text-muted-foreground"}`;

  const getCoverageText = (
    unreportedTaskCount: number,
    totalsPending: boolean,
  ) => {
    if (unreportedTaskCount > 0) {
      return totalsPending
        ? t(($) => $.leaderboard.usage_unreported_pending, {
            count: unreportedTaskCount,
          })
        : t(($) => $.leaderboard.usage_unreported, {
            count: unreportedTaskCount,
          });
    }
    return totalsPending ? t(($) => $.leaderboard.usage_totals_pending) : null;
  };

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
        <h4 className="text-body font-semibold">{t(($) => $.leaderboard.title)}</h4>
        <div className="flex flex-wrap items-center justify-end gap-3">
          <Segmented
            label={t(($) => $.leaderboard.scope_label)}
            value={scope}
            onChange={setScope}
            options={scopeOptions}
          />
          <Segmented
            label={t(($) => $.leaderboard.sort_label)}
            value={sortBy}
            onChange={setSortBy}
            options={sortOptions}
          />
          <span className="text-caption text-muted-foreground">{caption}</span>
          {/* The caption right beside this already states how many rows the
              window covers, so the toggle carries a count only when
              collapsing — spelling the total out twice reads as two different
              numbers once the deleted-agents bucket splits the caption. */}
          {sortedRows.length > LEADERBOARD_LIMIT ? (
            <button
              type="button"
              onClick={() => setShowAll((v) => !v)}
              className="text-caption text-muted-foreground underline-offset-2 hover:text-foreground hover:underline"
            >
              {showAll
                ? t(($) => $.leaderboard.show_less, { count: LEADERBOARD_LIMIT })
                : t(($) => $.leaderboard.show_all)}
            </button>
          ) : null}
        </div>
      </div>
      {sortedRows.length === 0 ? (
        <p className="px-4 py-8 text-center text-caption text-muted-foreground">
          {t(($) => $.leaderboard.no_data)}
        </p>
      ) : (
        <div
          role="region"
          aria-label={t(($) => $.leaderboard.title)}
          tabIndex={0}
          className="leaderboard-scroll-region overflow-x-auto overscroll-x-contain [-webkit-overflow-scrolling:touch]"
        >
          <div>
            <div
              className={`${LEADERBOARD_GRID} border-b px-4 py-2 text-caption font-medium text-muted-foreground`}
              style={LEADERBOARD_GRID_STYLE}
            >
              <span>{firstColHeader}</span>
              <span />
              <span className={colClass("tokens")}>
                {t(($) => $.leaderboard.header_tokens)}
              </span>
              <span className={colClass("cost")}>
                {t(($) => $.leaderboard.header_cost)}
              </span>
              <span className={colClass("time")}>
                {t(($) => $.leaderboard.header_time)}
              </span>
              <span className={colClass("tasks")}>
                {t(($) => $.leaderboard.header_tasks)}
              </span>
            </div>
            {/* A real list, like the offender list on the Errors tab: the rows are
                a truncated ranking, so screen readers need the count and the
                item boundaries rather than a bag of divs. */}
            <ul aria-label={t(($) => $.leaderboard.title)} className="divide-y">
              {visibleRows.map((row) => {
                const metric = SCOPE_SORT_METRIC[scope][sortBy];
                const value = metric(row);
                const pct = maxValue > 0 ? (value / maxValue) * 100 : 0;
                if (scope === "model") {
                  return (
                    <li
                      key={(row as ModelDashboardRow).model}
                      className={`${LEADERBOARD_GRID} px-4 py-2`}
                      style={LEADERBOARD_GRID_STYLE}
                    >
                      <span className="truncate text-body font-medium">
                        {(row as ModelDashboardRow).model}
                      </span>
                      <ProgressBar pct={pct} />
                      <MetricCell active={sortBy === "tokens"}>
                        {formatTokens((row as ModelDashboardRow).tokens)}
                      </MetricCell>
                      <MetricCell active={sortBy === "cost"} size="sm">
                        ${(row as ModelDashboardRow).cost.toFixed(2)}
                      </MetricCell>
                      <MetricCell active={false}>—</MetricCell>
                      <MetricCell active={sortBy === "tasks"}>
                        {(row as ModelDashboardRow).taskCount}
                      </MetricCell>
                    </li>
                  );
                }
                if (scope === "runtime") {
                  const r = row as RuntimeDashboardRow;
                  return (
                    <li
                      key={r.runtimeId}
                      className={`${LEADERBOARD_GRID} px-4 py-2`}
                      style={LEADERBOARD_GRID_STYLE}
                    >
                      <span className="truncate text-body font-medium">
                        {runtimes.find((rt) => rt.id === r.runtimeId)?.name ??
                          r.runtimeId}
                      </span>
                      <ProgressBar pct={pct} />
                      <MetricCell active={false}>—</MetricCell>
                      <MetricCell active={false}>—</MetricCell>
                      <MetricCell active={sortBy === "time"}>
                        {formatDuration(r.seconds, lessThanMinuteLabel)}
                      </MetricCell>
                      <MetricCell active={sortBy === "tasks"}>{r.taskCount}</MetricCell>
                    </li>
                  );
                }
                // Agent scope. Two synthetic rows, neither a real agent: both
                // render a neutral placeholder (no avatar fetch / hover card /
                // UUID) instead of looking the id up in the agent list.
                //
                // Only the deleted bucket dashes out Time/Tasks — it genuinely
                // never carries them (see bucketUnknownAgentRows). The server's
                // bucket does: those agents are alive and ran, the server just
                // merged them (MUL-5409), so zeroing their columns would
                // under-report the workspace's run time.
                //
                // Its copy is the neutral "Other agents" rather than anything
                // about permissions, because it covers two populations: agents
                // this viewer may not see, and the hidden system carriers behind
                // agent-builder sessions, which nobody can name — including the
                // admin who owns them.
                const r = row as AgentDashboardRow;
                const isDeletedBucket = r.agentId === DELETED_AGENTS_ROW_ID;
                const isRestrictedBucket = r.agentId === RESTRICTED_AGENTS_ROW_ID;
                const isBucket = isDeletedBucket || isRestrictedBucket;
                const agent = agents.find((a) => a.id === r.agentId);
                const usageUnavailable = !r.hasUsageTotals;
                const usageIncomplete = r.unreportedTaskCount > 0;
                const usageTotalsPending = r.hasReportedUsage && !r.hasUsageTotals;
                const tokenText = usageUnavailable
                  ? "—"
                  : `${usageIncomplete ? "≥" : ""}${formatTokens(r.tokens)}`;
                const costText = usageUnavailable
                  ? "—"
                  : `${usageIncomplete ? "≥" : ""}$${r.cost.toFixed(2)}`;
                const coverageText = getCoverageText(
                  r.unreportedTaskCount,
                  usageTotalsPending,
                );
                return (
                  <li
                    key={r.agentId}
                    className={`${LEADERBOARD_GRID} px-4 py-2`}
                    style={LEADERBOARD_GRID_STYLE}
                  >
                    <div className="flex min-w-0 items-center gap-2">
                      {isBucket ? (
                        <>
                          <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
                            {isDeletedBucket ? (
                              <Trash2 className="h-3 w-3" />
                            ) : (
                              <EyeOff className="h-3 w-3" />
                            )}
                          </span>
                          <span className="min-w-0">
                            <span className="block truncate text-body font-medium italic text-muted-foreground">
                              {isDeletedBucket
                                ? t(($) => $.leaderboard.deleted_agents)
                                : t(($) => $.leaderboard.other_agents)}
                            </span>
                            {coverageText ? (
                              <span
                                className="block truncate text-caption text-muted-foreground"
                                title={coverageText}
                              >
                                {coverageText}
                              </span>
                            ) : null}
                          </span>
                        </>
                      ) : (
                        <>
                          <ActorAvatar
                            actorType="agent"
                            actorId={r.agentId}
                            size="md"
                            enableHoverCard
                          />
                          <span className="min-w-0">
                            <span className="block cursor-pointer truncate text-body font-medium">
                              {agent?.name ?? r.agentId}
                            </span>
                            {coverageText ? (
                              <span
                                className="block truncate text-caption text-muted-foreground"
                                title={coverageText}
                              >
                                {coverageText}
                              </span>
                            ) : null}
                          </span>
                        </>
                      )}
                    </div>
                    <ProgressBar pct={pct} />
                    <MetricCell active={sortBy === "tokens"}>{tokenText}</MetricCell>
                    <MetricCell active={sortBy === "cost"} size="sm">
                      {costText}
                    </MetricCell>
                    <MetricCell active={sortBy === "time"}>
                      {isDeletedBucket
                        ? "—"
                        : formatDuration(r.seconds, lessThanMinuteLabel)}
                    </MetricCell>
                    <MetricCell active={sortBy === "tasks"}>
                      {isDeletedBucket ? "—" : r.taskCount}
                    </MetricCell>
                  </li>
                );
              })}
            </ul>
          </div>
        </div>
      )}
    </div>
  );
}

function ProgressBar({ pct }: { pct: number }) {
  return (
    <div className="relative h-2 overflow-hidden rounded-full bg-muted">
      <div
        className="h-full rounded-full bg-chart-1 transition-[width] duration-300 ease-out"
        style={{ width: `${pct}%` }}
      />
    </div>
  );
}

function MetricCell({
  active,
  size = "xs",
  children,
}: {
  active: boolean;
  size?: "xs" | "sm";
  children: ReactNode;
}) {
  return (
    <div
      className={`text-right tabular-nums ${size === "sm" ? "text-body" : "text-caption"} ${active ? "font-medium text-foreground" : "text-muted-foreground"}`}
    >
      {children}
    </div>
  );
}
