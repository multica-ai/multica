"use client";

import { Cloud, Lock, Monitor } from "lucide-react";
import type { ColumnDef } from "@tanstack/react-table";
import type { Agent, AgentRuntime } from "@multica/core/types";
import {
  type AgentActivity,
  type AgentPresenceDetail,
  summarizeActivityWindow,
} from "@multica/core/agents";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { availabilityConfig, workloadConfig } from "../presence";
import { AgentRowActions } from "./agent-row-actions";
import { Sparkline } from "./sparkline";

// Per-row data shape. We assemble agent + runtime + presence + activity +
// run count into one struct at the page level so the column cells just
// read off `row.original` without each pulling its own queries.
export interface AgentRow {
  agent: Agent;
  runtime: AgentRuntime | null;
  presence: AgentPresenceDetail | null | undefined;
  activity: AgentActivity | null | undefined;
  runCount: number;
  // Inline owner avatar — non-null when the page wants to attribute the
  // agent to a teammate (typically All scope on someone else's agent).
  ownerIdToShow: string | null;
  // True when the current user can archive / cancel-tasks on this agent.
  canManage: boolean;
}

// Sized columns render at exactly `size` in fixed table-layout mode —
// column.size doubles as the cell's effective max-width: truncatable
// cells with `truncate` inside hit ellipsis at the column edge.
//
// The Agent and Runtime columns have `meta.grow: true` so DataTable skips
// their inline widths until the user resizes them. Fixed table-layout splits
// the leftover space between them, which keeps Agent from monopolising wide
// viewports while still giving both columns a real floor.
//
// The grow columns also keep their `size` values even though those widths
// are skipped for initial rendering. TanStack folds them into
// `table.getTotalSize()`, which DataTable applies as the table's `min-width`.
// That's how the grow columns get real floors: when the viewport drops below
// the summed column sizes, the table refuses to shrink further and the
// container scrolls instead.
const COL_WIDTHS = {
  agent: 240,
  status: 120,
  workload: 140,
  runtime: 200,
  activity: 100,
  runs: 64,
  // 60 = 16 left padding + 28 kebab + 16 right padding. Keeps the
  // kebab's right edge 16px from the card so it lines up with the
  // toolbar's px-4 right inset.
  actions: 60,
} as const;

export function createAgentColumns({
  onDuplicate,
}: {
  onDuplicate: (agent: Agent) => void;
}): ColumnDef<AgentRow>[] {
  return [
    {
      id: "agent",
      header: "Agent",
      size: COL_WIDTHS.agent,
      meta: { grow: true },
      cell: ({ row }) => <AgentNameCell row={row.original} />,
    },
    {
      id: "status",
      header: "Status",
      size: COL_WIDTHS.status,
      cell: ({ row }) => {
        if (row.original.agent.archived_at) {
          return <span className="text-xs text-muted-foreground">—</span>;
        }
        return <AvailabilityCell presence={row.original.presence} />;
      },
    },
    {
      id: "workload",
      header: "Workload",
      size: COL_WIDTHS.workload,
      cell: ({ row }) => {
        if (row.original.agent.archived_at) {
          return <span className="text-xs text-muted-foreground">—</span>;
        }
        return <WorkloadCell presence={row.original.presence} />;
      },
    },
    {
      id: "runtime",
      header: "Runtime",
      size: COL_WIDTHS.runtime,
      meta: { grow: true },
      cell: ({ row }) => <RuntimeCell row={row.original} />,
    },
    {
      id: "activity",
      header: "Activity (7d)",
      size: COL_WIDTHS.activity,
      cell: ({ row }) => <ActivityCell row={row.original} />,
    },
    {
      id: "runs",
      header: () => <div className="text-right">Runs</div>,
      size: COL_WIDTHS.runs,
      cell: ({ row }) => (
        <div className="text-right font-mono text-xs tabular-nums text-muted-foreground">
          {row.original.runCount == null
            ? "—"
            : row.original.runCount.toLocaleString()}
        </div>
      ),
    },
    {
      id: "actions",
      header: () => null,
      size: COL_WIDTHS.actions,
      enableResizing: false,
      cell: ({ row }) => (
        <div
          className="flex justify-end"
          // The kebab dropdown owns its own click target. Stop the row
          // click handler from firing as a side-effect.
          onClick={(e) => e.stopPropagation()}
        >
          <AgentRowActions
            agent={row.original.agent}
            presence={row.original.presence}
            canManage={row.original.canManage}
            onDuplicate={onDuplicate}
          />
        </div>
      ),
    },
  ];
}

// ---------------------------------------------------------------------------
// Cell renderers
// ---------------------------------------------------------------------------

function AgentNameCell({ row }: { row: AgentRow }) {
  const { agent, ownerIdToShow } = row;
  const isArchived = !!agent.archived_at;
  const isPrivate = agent.visibility === "private";

  return (
    <div className="flex min-w-0 items-center gap-3">
      <ActorAvatar
        actorType="agent"
        actorId={agent.id}
        size={28}
        className={`shrink-0 rounded-md ${isArchived ? "opacity-50 grayscale" : ""}`}
        showStatusDot
      />
      <div className="min-w-0 flex-1">
        <div className="flex min-w-0 items-center gap-2">
          <span
            className={`min-w-0 truncate font-medium ${
              isArchived ? "text-muted-foreground" : ""
            }`}
          >
            {agent.name}
          </span>
          {isPrivate && !isArchived && (
            <Tooltip>
              <TooltipTrigger
                render={
                  <Lock className="h-3 w-3 shrink-0 text-muted-foreground/60" />
                }
              />
              <TooltipContent>
                Private — only the owner can assign work
              </TooltipContent>
            </Tooltip>
          )}
          {ownerIdToShow && (
            <ActorAvatar
              actorType="member"
              actorId={ownerIdToShow}
              size={14}
            />
          )}
          {isArchived && (
            <span className="shrink-0 rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
              Archived
            </span>
          )}
        </div>
        <div
          className={`mt-0.5 truncate text-xs ${
            agent.description
              ? "text-muted-foreground"
              : "italic text-muted-foreground/50"
          }`}
        >
          {agent.description || "No description"}
        </div>
      </div>
    </div>
  );
}

function AvailabilityCell({
  presence,
}: {
  presence: AgentPresenceDetail | null | undefined;
}) {
  if (!presence) {
    return (
      <span className="inline-flex h-3 w-16 animate-pulse rounded bg-muted/60" />
    );
  }
  const av = availabilityConfig[presence.availability];
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${av.dotClass}`} />
      <span className={`text-xs ${av.textClass}`}>{av.label}</span>
    </span>
  );
}

function WorkloadCell({
  presence,
}: {
  presence: AgentPresenceDetail | null | undefined;
}) {
  if (!presence) {
    return (
      <span className="inline-flex h-3 w-20 animate-pulse rounded bg-muted/60" />
    );
  }
  // All three workload states render with the same shape (icon + label +
  // optional counts). Idle agents show "Idle" rather than a bare em-dash
  // — that hyphen used to mean both "no presence data" and "agent is
  // idle", which conflated two distinct things. Em-dash is now reserved
  // for archived rows / undefined presence (handled at the column level).
  const wl = workloadConfig[presence.workload];
  const isWorking = presence.workload === "working";
  const isQueued = presence.workload === "queued";
  // Queued's amber from workloadConfig is the severe tone for "stuck on
  // offline runtime". On an online runtime queued is just a brief race
  // between enqueue and daemon claim, where amber misreads as a warning.
  // Compose with availability so the colour matches the actual signal.
  const queuedTone =
    presence.availability === "online" ? "text-muted-foreground" : wl.textClass;
  const labelTone = isQueued ? queuedTone : wl.textClass;
  // Working: show running/capacity, optionally with +Nq when overflow.
  // Queued (= nothing running, things waiting — typically a stuck-on-
  // offline-runtime signal): show the queued count directly so the user
  // sees "Queued · 2" instead of misleading "Running 0/3 +2q".
  // Idle: no counts — the label alone carries the meaning.
  const counts = isWorking
    ? presence.queuedCount > 0
      ? `${presence.runningCount}/${presence.capacity} +${presence.queuedCount}q`
      : `${presence.runningCount}/${presence.capacity}`
    : isQueued
      ? `${presence.queuedCount}`
      : null;
  return (
    <span className="inline-flex items-center gap-1 text-xs">
      {/* Icon only renders for working/queued — those carry visual meaning
          (spinner = in motion, clock = waiting). Idle adding an icon read
          as a warning marker, which is the wrong signal. */}
      {presence.workload !== "idle" && (
        <wl.icon
          className={`h-3 w-3 shrink-0 ${labelTone} ${isWorking ? "animate-spin" : ""}`}
        />
      )}
      <span className={`shrink-0 ${labelTone}`}>{wl.label}</span>
      {counts && (
        <span className="truncate text-muted-foreground">{counts}</span>
      )}
    </span>
  );
}

function RuntimeCell({ row }: { row: AgentRow }) {
  const { agent, runtime } = row;
  const isCloud = agent.runtime_mode === "cloud";
  const RuntimeIcon = isCloud ? Cloud : Monitor;
  const runtimeLabel = runtime?.name ?? (isCloud ? "Cloud" : "Local");

  return (
    <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
      <RuntimeIcon className="h-3 w-3 shrink-0" />
      <Tooltip>
        <TooltipTrigger
          render={
            <span className="block min-w-0 truncate">{runtimeLabel}</span>
          }
        />
        <TooltipContent>{runtimeLabel}</TooltipContent>
      </Tooltip>
    </div>
  );
}

function ActivityCell({ row }: { row: AgentRow }) {
  const { agent, activity } = row;
  if (agent.archived_at) {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  if (!activity) {
    return (
      <span
        className="inline-block animate-pulse rounded bg-muted/60"
        style={{ width: 64, height: 20 }}
      />
    );
  }
  const summary = summarizeActivityWindow(activity, 7);
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <div className="inline-flex cursor-default items-center">
            <Sparkline buckets={summary.buckets} width={64} height={20} />
          </div>
        }
      />
      <TooltipContent>
        <ActivityTooltipBody activity={activity} />
      </TooltipContent>
    </Tooltip>
  );
}

function ActivityTooltipBody({ activity }: { activity: AgentActivity }) {
  const summary = summarizeActivityWindow(activity, 7);
  const { totalRuns, totalFailed } = summary;
  const { daysSinceCreated } = activity;

  const isPartial = daysSinceCreated < 7;
  const headerText = isPartial
    ? `Created ${daysSinceCreated === 0 ? "today" : `${daysSinceCreated} day${daysSinceCreated === 1 ? "" : "s"} ago`}`
    : "Last 7 days";

  let bodyText: string;
  if (totalRuns === 0) {
    bodyText = "No activity";
  } else {
    const failedFragment =
      totalFailed > 0
        ? ` · ${totalFailed} failed (${Math.round((totalFailed / totalRuns) * 100)}%)`
        : "";
    bodyText = `${totalRuns} run${totalRuns === 1 ? "" : "s"}${failedFragment}`;
  }

  return (
    <div className="flex flex-col gap-0.5">
      <span className="text-[10px] font-medium uppercase tracking-wider text-muted-foreground">
        {headerText}
      </span>
      <span className="text-xs">{bodyText}</span>
    </div>
  );
}
