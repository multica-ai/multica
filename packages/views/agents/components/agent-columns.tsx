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
import { availabilityConfig, taskStateConfig } from "../presence";
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

// Column widths in px. Sum determines the table's natural width; when the
// sum exceeds the viewport, the DataTable container scrolls horizontally.
// Values picked to fit content comfortably without truncating in typical
// cases — e.g. Agent at 360 holds avatar + 18-char name + a one-line
// description excerpt.
const COL_WIDTHS = {
  agent: 360,
  status: 120,
  lastRun: 160,
  runtime: 200,
  activity: 100,
  runs: 80,
  actions: 48,
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
      id: "lastRun",
      header: "Last run",
      size: COL_WIDTHS.lastRun,
      cell: ({ row }) => {
        if (row.original.agent.archived_at) {
          return <span className="text-xs text-muted-foreground">—</span>;
        }
        return <LastRunCell presence={row.original.presence} />;
      },
    },
    {
      id: "runtime",
      header: "Runtime",
      size: COL_WIDTHS.runtime,
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
    <div className="flex items-center gap-3">
      <ActorAvatar
        actorType="agent"
        actorId={agent.id}
        size={28}
        className={`shrink-0 rounded-md ${isArchived ? "opacity-50 grayscale" : ""}`}
        showStatusDot
      />
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span
            className={`truncate font-medium ${
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
          className={`mt-0.5 line-clamp-1 text-xs ${
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

function LastRunCell({
  presence,
}: {
  presence: AgentPresenceDetail | null | undefined;
}) {
  if (!presence) {
    return (
      <span className="inline-flex h-3 w-20 animate-pulse rounded bg-muted/60" />
    );
  }
  if (presence.lastTask === "idle") {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  const ts = taskStateConfig[presence.lastTask];
  const isRunning = presence.lastTask === "running";
  const counts =
    isRunning && presence.queuedCount > 0
      ? `${presence.runningCount}/${presence.capacity} +${presence.queuedCount}q`
      : isRunning
        ? `${presence.runningCount}/${presence.capacity}`
        : null;
  return (
    <span className="inline-flex items-center gap-1 text-xs">
      <ts.icon className={`h-3 w-3 shrink-0 ${ts.textClass}`} />
      <span className={`shrink-0 ${ts.textClass}`}>{ts.label}</span>
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
          render={<span className="truncate">{runtimeLabel}</span>}
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
