"use client";

import { useMemo, useState } from "react";
import { EyeOff, Trash2 } from "lucide-react";
import { ActorAvatar } from "../../common/actor-avatar";
import { formatTokens } from "../../runtimes/utils";
import { useT } from "../../i18n";
import {
  DELETED_AGENTS_ROW_ID,
  formatDuration,
  isSyntheticAgentRow,
  RESTRICTED_AGENTS_ROW_ID,
  type AgentDashboardRow,
} from "../utils";
import { Segmented } from "./dashboard-shared";

// Which metric ranks the leaderboard. Drives row order, progress bar
// width, and which column header is emphasised — keeping the three in
// lockstep so the user always sees what the ranking actually measures.
type LeaderboardSort = "tokens" | "cost" | "time" | "tasks";

const SORT_METRIC: Record<LeaderboardSort, (r: AgentDashboardRow) => number> = {
  tokens: (r) => r.tokens,
  cost: (r) => r.cost,
  time: (r) => r.seconds,
  tasks: (r) => r.taskCount,
};

// How many agents the leaderboard ranks before collapsing the tail behind a
// toggle, mirroring the offender list's cap. A workspace with dozens of agents
// rendered every one of them, which pushed everything below it a full screen or
// more down the page (MUL-5388). Ten answers "who is spending the most" — the
// tail is reachable via the toggle.
const LEADERBOARD_LIMIT = 10;

const LEADERBOARD_GRID =
  "grid grid-cols-[minmax(0,1.6fr)_minmax(0,1fr)_5rem_5rem_5rem_4rem] items-center gap-3";

export function Leaderboard({
  rows,
  agents,
  deletedAgentCount,
  lessThanMinuteLabel,
}: {
  rows: AgentDashboardRow[];
  agents: { id: string; name: string }[];
  deletedAgentCount: number;
  lessThanMinuteLabel: string;
}) {
  const { t } = useT("usage");
  const [sortBy, setSortBy] = useState<LeaderboardSort>("tokens");
  const [showAll, setShowAll] = useState(false);

  const sortOptions = useMemo(
    () => [
      { value: "tokens" as const, label: t(($) => $.leaderboard.header_tokens) },
      { value: "cost" as const, label: t(($) => $.leaderboard.header_cost) },
      { value: "time" as const, label: t(($) => $.leaderboard.header_time) },
      { value: "tasks" as const, label: t(($) => $.leaderboard.header_tasks) },
    ],
    [t],
  );

  // Re-rank when the metric changes; keep the merged input untouched so
  // upstream `mergeAgentDashboardRows`'s tiebreaker (run time desc) still
  // applies inside an equal-bucket.
  const sortedRows = useMemo(() => {
    const metric = SORT_METRIC[sortBy];
    return rows.toSorted((a, b) => metric(b) - metric(a));
  }, [rows, sortBy]);

  // Measured across every row, not just the visible ones, so a bar's width
  // means the same thing collapsed and expanded — the leader always fills the
  // track and nothing re-scales when the tail comes into view.
  const maxValue = useMemo(() => {
    const metric = SORT_METRIC[sortBy];
    return sortedRows.reduce((m, r) => Math.max(m, metric(r)), 0);
  }, [sortedRows, sortBy]);

  const visibleRows = showAll
    ? sortedRows
    : sortedRows.slice(0, LEADERBOARD_LIMIT);

  // "N agents" counts the rows that actually name an agent. Up to two of the
  // rows are synthetic buckets (deleted, restricted), and subtracting a fixed 1
  // reported one agent too many whenever both were present.
  const namedAgentCount = useMemo(
    () => rows.filter((r) => !isSyntheticAgentRow(r.agentId)).length,
    [rows],
  );

  // Active column gets foreground text; others stay muted. Helps the user
  // see "this is what the bar is measuring" at a glance.
  const colClass = (key: LeaderboardSort) =>
    `text-right ${sortBy === key ? "text-foreground" : "text-muted-foreground"}`;

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
        <h4 className="text-body font-semibold">{t(($) => $.leaderboard.title)}</h4>
        <div className="flex flex-wrap items-center justify-end gap-3">
          <Segmented
            label={t(($) => $.leaderboard.sort_label)}
            value={sortBy}
            onChange={setSortBy}
            options={sortOptions}
          />
          <span className="text-caption text-muted-foreground">
            {deletedAgentCount > 0
              ? t(($) => $.leaderboard.caption_with_deleted, {
                  count: namedAgentCount,
                  deleted: deletedAgentCount,
                })
              : t(($) => $.leaderboard.caption, { count: namedAgentCount })}
          </span>
          {/* The caption right beside this already states how many agents the
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
        <>
          <div
            className={`${LEADERBOARD_GRID} border-b px-4 py-2 text-caption font-medium text-muted-foreground`}
          >
            <span>{t(($) => $.leaderboard.header_agent)}</span>
            <span />
            <span className={colClass("tokens")}>{t(($) => $.leaderboard.header_tokens)}</span>
            <span className={colClass("cost")}>{t(($) => $.leaderboard.header_cost)}</span>
            <span className={colClass("time")}>{t(($) => $.leaderboard.header_time)}</span>
            <span className={colClass("tasks")}>{t(($) => $.leaderboard.header_tasks)}</span>
          </div>
          {/* A real list, like the offender list on the Errors tab: the rows are
              a truncated ranking, so screen readers need the count and the
              item boundaries rather than a bag of divs. */}
          <ul aria-label={t(($) => $.leaderboard.title)} className="divide-y">
            {visibleRows.map((row) => {
              // Two synthetic rows, neither a real agent: both render a neutral
              // placeholder (no avatar fetch / hover card / UUID) instead of
              // looking the id up in the agent list.
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
              const isDeletedBucket = row.agentId === DELETED_AGENTS_ROW_ID;
              const isRestrictedBucket = row.agentId === RESTRICTED_AGENTS_ROW_ID;
              const isBucket = isDeletedBucket || isRestrictedBucket;
              const agent = agents.find((a) => a.id === row.agentId);
              const value = SORT_METRIC[sortBy](row);
              const pct = maxValue > 0 ? (value / maxValue) * 100 : 0;
              return (
                <li key={row.agentId} className={`${LEADERBOARD_GRID} px-4 py-2`}>
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
                        <span className="truncate text-body font-medium italic text-muted-foreground">
                          {isDeletedBucket
                            ? t(($) => $.leaderboard.deleted_agents)
                            : t(($) => $.leaderboard.other_agents)}
                        </span>
                      </>
                    ) : (
                      <>
                        <ActorAvatar
                          actorType="agent"
                          actorId={row.agentId}
                          size="md"
                          enableHoverCard
                        />
                        <span className="cursor-pointer truncate text-body font-medium">
                          {agent?.name ?? row.agentId}
                        </span>
                      </>
                    )}
                  </div>
                  <div className="relative h-2 overflow-hidden rounded-full bg-muted">
                    <div
                      className="h-full rounded-full bg-chart-1 transition-[width] duration-300 ease-out"
                      style={{ width: `${pct}%` }}
                    />
                  </div>
                  <div
                    className={`text-right text-caption tabular-nums ${sortBy === "tokens" ? "font-medium text-foreground" : "text-muted-foreground"}`}
                  >
                    {formatTokens(row.tokens)}
                  </div>
                  <div
                    className={`text-right tabular-nums ${sortBy === "cost" ? "text-body font-medium" : "text-caption text-muted-foreground"}`}
                  >
                    ${row.cost.toFixed(2)}
                  </div>
                  <div
                    className={`text-right text-caption tabular-nums ${sortBy === "time" ? "font-medium text-foreground" : "text-muted-foreground"}`}
                  >
                    {isDeletedBucket
                      ? "—"
                      : formatDuration(row.seconds, lessThanMinuteLabel)}
                  </div>
                  <div
                    className={`text-right text-caption tabular-nums ${sortBy === "tasks" ? "font-medium text-foreground" : "text-muted-foreground"}`}
                  >
                    {isDeletedBucket ? "—" : row.taskCount}
                  </div>
                </li>
              );
            })}
          </ul>
        </>
      )}
    </div>
  );
}
