"use client";

import { Fragment, useMemo, useState, type CSSProperties } from "react";
import { Bot, ChevronDown, ChevronRight, EyeOff, Trash2 } from "@multica/ui/icons";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { ActorAvatar } from "../../common/actor-avatar";
import { formatTokens } from "../../runtimes/utils";
import { useT } from "../../i18n";
import {
  DELETED_AGENTS_ROW_ID,
  DELETED_SQUADS_ROW_ID,
  formatDuration,
  isSyntheticAgentRow,
  isSyntheticSquadRow,
  RESTRICTED_AGENTS_ROW_ID,
  STANDALONE_SQUAD_ROW_ID,
  type AgentDashboardRow,
  type SquadDashboardRow,
} from "../utils";
import { Segmented } from "./dashboard-shared";
import "./leaderboard.css";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";

// Which metric ranks the leaderboard. Drives row order, progress bar
// width, and which column header is emphasised — keeping the three in
// lockstep so the user always sees what the ranking actually measures.
type LeaderboardSort = "tokens" | "cost" | "time" | "tasks";
export type LeaderboardGroupBy = "agent" | "squad";

const SORT_METRIC: Record<
  LeaderboardSort,
  (r: { tokens: number; cost: number; seconds: number; taskCount: number }) => number
> = {
  tokens: (r) => r.tokens,
  cost: (r) => r.cost,
  time: (r) => r.seconds,
  tasks: (r) => r.taskCount,
};

// How many entities the leaderboard ranks before collapsing the tail behind a
// toggle, mirroring the offender list's cap.
const LEADERBOARD_LIMIT = 10;

const LEADERBOARD_GRID_STYLE = {
  minWidth: "fit-content",
  gridTemplateColumns:
    "minmax(10rem, 1.6fr) minmax(6rem, 1fr) 5rem 5rem 5rem 4rem",
} satisfies CSSProperties;

const LEADERBOARD_GRID = "grid items-center gap-3";

type LeaderboardSquad = {
  id: string;
  name: string;
  avatar_url: string | null;
  leader_id: string;
};

export function Leaderboard({
  rows,
  agents,
  deletedAgentCount,
  lessThanMinuteLabel,
  squadRows = [],
  squads = [],
  groupBy = "agent",
  onGroupByChange,
  selectedSquad = null,
}: {
  rows: AgentDashboardRow[];
  agents: { id: string; name: string }[];
  deletedAgentCount: number;
  lessThanMinuteLabel: string;
  squadRows?: SquadDashboardRow[];
  squads?: LeaderboardSquad[];
  groupBy?: LeaderboardGroupBy;
  onGroupByChange?: (next: LeaderboardGroupBy) => void;
  /** When the page is filtered to a single Squad, the leaderboard renders that
   *  Squad's member breakdown directly (no redundant Squad grouping toggle). */
  selectedSquad?: LeaderboardSquad | null;
}) {
  const { t } = useT("usage");
  const { t: tSquads } = useT("squads");
  const [sortBy, setSortBy] = useState<LeaderboardSort>("tokens");
  const [showAll, setShowAll] = useState(false);
  const [expandedSquads, setExpandedSquads] = useState<Record<string, boolean>>({});

  const effectiveGroupBy: LeaderboardGroupBy =
    selectedSquad || !onGroupByChange ? "agent" : groupBy;
  const showGroupSwitch = !selectedSquad && !!onGroupByChange;

  const sortOptions = useMemo(
    () => [
      { value: "tokens" as const, label: t(($) => $.leaderboard.header_tokens) },
      { value: "cost" as const, label: t(($) => $.leaderboard.header_cost) },
      { value: "time" as const, label: t(($) => $.leaderboard.header_time) },
      { value: "tasks" as const, label: t(($) => $.leaderboard.header_tasks) },
    ],
    [t],
  );

  const groupOptions = useMemo(
    () => [
      { value: "agent" as const, label: t(($) => $.leaderboard.group_agent) },
      { value: "squad" as const, label: t(($) => $.leaderboard.group_squad) },
    ],
    [t],
  );

  const sortedRows = useMemo(() => {
    const metric = SORT_METRIC[sortBy];
    return rows.toSorted((a, b) => metric(b) - metric(a));
  }, [rows, sortBy]);

  // Keep real squads ranked at the top by the active metric and pin the
  // standalone (non-squad) reconciliation bucket at the bottom so the reader
  // compares squads cleanly while all rows still sum to 100% of top KPIs.
  const sortedSquadRows = useMemo(() => {
    const metric = SORT_METRIC[sortBy];
    return squadRows.toSorted((a, b) => {
      const aStandalone = a.squadId === STANDALONE_SQUAD_ROW_ID ? 1 : 0;
      const bStandalone = b.squadId === STANDALONE_SQUAD_ROW_ID ? 1 : 0;
      if (aStandalone !== bStandalone) return aStandalone - bStandalone;
      return metric(b) - metric(a);
    });
  }, [squadRows, sortBy]);

  const activeRows = effectiveGroupBy === "squad" ? sortedSquadRows : sortedRows;

  const maxValue = useMemo(() => {
    const metric = SORT_METRIC[sortBy];
    return activeRows.reduce((m, r) => Math.max(m, metric(r)), 0);
  }, [activeRows, sortBy]);

  const visibleAgentRows = showAll
    ? sortedRows
    : sortedRows.slice(0, LEADERBOARD_LIMIT);

  const visibleSquadRows = showAll
    ? sortedSquadRows
    : sortedSquadRows.slice(0, LEADERBOARD_LIMIT);

  const namedAgentCount = useMemo(
    () => rows.filter((r) => !isSyntheticAgentRow(r.agentId)).length,
    [rows],
  );

  const namedSquadCount = useMemo(
    () => squadRows.filter((r) => !isSyntheticSquadRow(r.squadId)).length,
    [squadRows],
  );

  const hasStandaloneSquadRow = useMemo(
    () => squadRows.some((r) => r.squadId === STANDALONE_SQUAD_ROW_ID),
    [squadRows],
  );

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

  const toggleSquad = (squadId: string) => {
    setExpandedSquads((prev) => ({ ...prev, [squadId]: !prev[squadId] }));
  };

  const renderAgentRow = (
    row: AgentDashboardRow,
    opts?: {
      indented?: boolean;
      parentMaxValue?: number;
      leaderId?: string;
      keyPrefix?: string;
    },
  ) => {
    const isDeletedBucket = row.agentId === DELETED_AGENTS_ROW_ID;
    const isRestrictedBucket = row.agentId === RESTRICTED_AGENTS_ROW_ID;
    const isBucket = isDeletedBucket || isRestrictedBucket;
    const agent = agents.find((a) => a.id === row.agentId);
    const value = SORT_METRIC[sortBy](row);
    const denom = opts?.parentMaxValue ?? maxValue;
    const pct = denom > 0 ? Math.min(100, (value / denom) * 100) : 0;
    const usageUnavailable = !row.hasUsageTotals;
    const usageIncomplete = row.unreportedTaskCount > 0;
    const usageTotalsPending = row.hasReportedUsage && !row.hasUsageTotals;
    const tokenText = usageUnavailable
      ? "—"
      : `${usageIncomplete ? "≥" : ""}${formatTokens(row.tokens)}`;
    const costText = usageUnavailable
      ? "—"
      : `${usageIncomplete ? "≥" : ""}$${row.cost.toFixed(2)}`;
    const coverageText = getCoverageText(
      row.unreportedTaskCount,
      usageTotalsPending,
    );
    const isLeader = !!opts?.leaderId && row.agentId === opts.leaderId;

    return (
      <li
        key={opts?.keyPrefix ? `${opts.keyPrefix}:${row.agentId}` : row.agentId}
        className={`${LEADERBOARD_GRID} px-4 ${opts?.indented ? "bg-muted/25 py-1.5" : "py-2"}`}
        style={LEADERBOARD_GRID_STYLE}
      >
        <div
          className={`flex min-w-0 items-center gap-2 ${opts?.indented ? "pl-7" : ""}`}
        >
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
                  <TruncatedTooltip content={coverageText}>
                    <span className="block truncate text-caption text-muted-foreground">
                      {coverageText}
                    </span>
                  </TruncatedTooltip>
                ) : null}
              </span>
            </>
          ) : (
            <>
              <ActorAvatar
                actorType="agent"
                actorId={row.agentId}
                size={opts?.indented ? "sm" : "md"}
                enableHoverCard
              />
              <span className="min-w-0">
                <span className="flex min-w-0 items-center gap-1.5">
                  <span
                    className={`block cursor-pointer truncate font-medium ${opts?.indented ? "text-label" : "text-body"}`}
                  >
                    {agent?.name ?? row.agentId}
                  </span>
                  {isLeader ? (
                    <span className="shrink-0 rounded-md bg-amber-100 px-1 py-0.5 text-micro font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-400">
                      {tSquads(($) => $.members_tab.leader_chip)}
                    </span>
                  ) : null}
                </span>
                {coverageText ? (
                  <TruncatedTooltip content={coverageText}>
                    <span className="block truncate text-caption text-muted-foreground">
                      {coverageText}
                    </span>
                  </TruncatedTooltip>
                ) : null}
              </span>
            </>
          )}
        </div>
        <div className="relative h-2 overflow-hidden rounded-full bg-muted">
          <div
            className={`h-full rounded-full transition-[width] duration-300 ease-out ${
              opts?.indented ? "bg-chart-3" : "bg-chart-1"
            }`}
            style={{ width: `${pct}%` }}
          />
        </div>
        <div
          className={`text-right text-caption tabular-nums ${sortBy === "tokens" ? "font-medium text-foreground" : "text-muted-foreground"}`}
        >
          {tokenText}
        </div>
        <div
          className={`text-right tabular-nums ${sortBy === "cost" ? "text-body font-medium" : "text-caption text-muted-foreground"}`}
        >
          {costText}
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
  };

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pt-4 pb-3">
        <div className="flex flex-wrap items-center gap-2.5">
          {selectedSquad ? (
            <div className="flex items-center gap-2">
              <ActorAvatarBase
                name={selectedSquad.name}
                initials={selectedSquad.name.slice(0, 2).toUpperCase()}
                avatarUrl={resolvePublicFileUrl(selectedSquad.avatar_url)}
                isSquad
                size="sm"
              />
              <h4 className="text-body font-semibold">
                {t(($) => $.leaderboard.squad_members_title, {
                  name: selectedSquad.name,
                })}
              </h4>
            </div>
          ) : (
            <h4 className="text-body font-semibold">{t(($) => $.leaderboard.title)}</h4>
          )}
          {showGroupSwitch ? (
            <Segmented
              label={t(($) => $.leaderboard.group_label)}
              value={effectiveGroupBy}
              onChange={(next) => {
                setShowAll(false);
                onGroupByChange(next);
              }}
              options={groupOptions}
            />
          ) : null}
        </div>
        <div className="flex flex-wrap items-center justify-end gap-3">
          <Segmented
            label={t(($) => $.leaderboard.sort_label)}
            value={sortBy}
            onChange={setSortBy}
            options={sortOptions}
          />
          <span className="text-caption text-muted-foreground">
            {effectiveGroupBy === "squad"
              ? hasStandaloneSquadRow
                ? t(($) => $.leaderboard.squad_caption_with_standalone, {
                    count: namedSquadCount,
                  })
                : t(($) => $.leaderboard.squad_caption, {
                    count: namedSquadCount,
                  })
              : deletedAgentCount > 0
                ? t(($) => $.leaderboard.caption_with_deleted, {
                    count: namedAgentCount,
                    deleted: deletedAgentCount,
                  })
                : t(($) => $.leaderboard.caption, { count: namedAgentCount })}
          </span>
          {activeRows.length > LEADERBOARD_LIMIT ? (
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
      {activeRows.length === 0 ? (
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
              <span>
                {effectiveGroupBy === "squad"
                  ? t(($) => $.leaderboard.header_squad)
                  : t(($) => $.leaderboard.header_agent)}
              </span>
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
            <ul aria-label={t(($) => $.leaderboard.title)} className="divide-y">
              {effectiveGroupBy === "agent"
                ? visibleAgentRows.map((row) =>
                    renderAgentRow(row, {
                      leaderId: selectedSquad?.leader_id,
                    }),
                  )
                : visibleSquadRows.map((row) => {
                    const isStandalone = row.squadId === STANDALONE_SQUAD_ROW_ID;
                    const isDeleted = row.squadId === DELETED_SQUADS_ROW_ID;
                    const isSynthetic = isStandalone || isDeleted;
                    const squad = squads.find((s) => s.id === row.squadId);
                    const value = SORT_METRIC[sortBy](row);
                    const pct = maxValue > 0 ? (value / maxValue) * 100 : 0;
                    const usageUnavailable = !row.hasUsageTotals;
                    const usageIncomplete = row.unreportedTaskCount > 0;
                    const usageTotalsPending =
                      row.hasReportedUsage && !row.hasUsageTotals;
                    const tokenText = usageUnavailable
                      ? "—"
                      : `${usageIncomplete ? "≥" : ""}${formatTokens(row.tokens)}`;
                    const costText = usageUnavailable
                      ? "—"
                      : `${usageIncomplete ? "≥" : ""}$${row.cost.toFixed(2)}`;
                    const coverageText = getCoverageText(
                      row.unreportedTaskCount,
                      usageTotalsPending,
                    );
                    const canExpand = row.members.length > 0;
                    const expanded = canExpand && !!expandedSquads[row.squadId];
                    const sortedMembers = expanded
                      ? row.members.toSorted(
                          (a, b) => SORT_METRIC[sortBy](b) - SORT_METRIC[sortBy](a),
                        )
                      : [];
                    const namedMembersCount = row.members.filter(
                      (m) => !isSyntheticAgentRow(m.agentId),
                    ).length;

                    return (
                      <Fragment key={row.squadId}>
                        <li
                          className={`${LEADERBOARD_GRID} px-4 py-2 ${expanded ? "bg-muted/40" : ""}`}
                          style={LEADERBOARD_GRID_STYLE}
                        >
                          <div className="flex min-w-0 items-center gap-2">
                            {canExpand ? (
                              <button
                                type="button"
                                onClick={() => toggleSquad(row.squadId)}
                                aria-expanded={expanded}
                                aria-label={t(($) => $.leaderboard.toggle_squad_members, {
                                  name: isStandalone
                                    ? t(($) => $.leaderboard.standalone_agents)
                                    : isDeleted
                                      ? t(($) => $.leaderboard.deleted_squads)
                                      : squad?.name ?? row.squadId,
                                })}
                                className="flex size-5 shrink-0 items-center justify-center rounded-sm text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                              >
                                {expanded ? (
                                  <ChevronDown className="size-3.5" />
                                ) : (
                                  <ChevronRight className="size-3.5" />
                                )}
                              </button>
                            ) : (
                              <span className="size-5 shrink-0" aria-hidden="true" />
                            )}
                            {isSynthetic ? (
                              <span className="flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
                                {isStandalone ? (
                                  <Bot className="h-3 w-3" />
                                ) : (
                                  <Trash2 className="h-3 w-3" />
                                )}
                              </span>
                            ) : (
                              <ActorAvatar
                                actorType="squad"
                                actorId={row.squadId}
                                size="md"
                                enableHoverCard
                              />
                            )}
                            <span className="min-w-0">
                              <button
                                type="button"
                                onClick={() => canExpand && toggleSquad(row.squadId)}
                                className={`block max-w-full truncate text-left text-body font-medium ${
                                  isSynthetic
                                    ? "italic text-muted-foreground"
                                    : "text-foreground"
                                } ${canExpand ? "cursor-pointer hover:underline" : "cursor-default"}`}
                              >
                                {isStandalone
                                  ? t(($) => $.leaderboard.standalone_agents)
                                  : isDeleted
                                    ? t(($) => $.leaderboard.deleted_squads)
                                    : squad?.name ?? row.squadId}
                              </button>
                              <span className="block truncate text-caption text-muted-foreground">
                                {coverageText ??
                                  (isStandalone
                                    ? t(($) => $.leaderboard.standalone_agents_hint, {
                                        count: namedMembersCount,
                                      })
                                    : t(($) => $.leaderboard.squad_members_count, {
                                        count: namedMembersCount,
                                      }))}
                              </span>
                            </span>
                          </div>
                          <div className="relative h-2 overflow-hidden rounded-full bg-muted">
                            <div
                              className={`h-full rounded-full transition-[width] duration-300 ease-out ${
                                isStandalone ? "bg-chart-4" : "bg-chart-1"
                              }`}
                              style={{ width: `${pct}%` }}
                            />
                          </div>
                          <div
                            className={`text-right text-caption tabular-nums ${sortBy === "tokens" ? "font-medium text-foreground" : "text-muted-foreground"}`}
                          >
                            {tokenText}
                          </div>
                          <div
                            className={`text-right tabular-nums ${sortBy === "cost" ? "text-body font-medium" : "text-caption text-muted-foreground"}`}
                          >
                            {costText}
                          </div>
                          <div
                            className={`text-right text-caption tabular-nums ${sortBy === "time" ? "font-medium text-foreground" : "text-muted-foreground"}`}
                          >
                            {formatDuration(row.seconds, lessThanMinuteLabel)}
                          </div>
                          <div
                            className={`text-right text-caption tabular-nums ${sortBy === "tasks" ? "font-medium text-foreground" : "text-muted-foreground"}`}
                          >
                            {row.taskCount}
                          </div>
                        </li>
                        {expanded
                          ? sortedMembers.map((member) =>
                              renderAgentRow(member, {
                                indented: true,
                                parentMaxValue: Math.max(value, 1),
                                leaderId: squad?.leader_id,
                                keyPrefix: row.squadId,
                              }),
                            )
                          : null}
                      </Fragment>
                    );
                  })}
            </ul>
          </div>
        </div>
      )}
    </div>
  );
}
