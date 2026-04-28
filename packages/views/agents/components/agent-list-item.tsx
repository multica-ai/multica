"use client";

import { Cloud, Lock, Monitor } from "lucide-react";
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
import { AppLink } from "../../navigation";
import { availabilityConfig, taskStateConfig } from "../presence";
import { AgentPresenceIndicator } from "./agent-presence-indicator";
import { AgentRowActions } from "./agent-row-actions";
import { Sparkline } from "./sparkline";

// Shared grid template used by both the list header and every row, so the
// sticky header columns stay aligned with the rows below it.
//
// Why grid (not <Table table-fixed>): table-fixed forces every column to a
// declared width, which means the Status column was always reserving space
// for the worst case ("Working · 0 / 6 · +5 queued"), even when no agent in
// the workspace was in that state. Switching to grid + `max-content` lets the
// column shrink automatically when the longest cell is just "Available", and
// only widen when there's an agent that actually needs the room. The freed
// space flows into the Agent column (the primary content), via its `1.6fr`
// share — same ratio Skills uses, so the two list pages read as one family.
//
// Leading-avatar column (1.75rem) is a dedicated grid track so the header's
// "Agent" label sits on the same x as the row's name text. Without this the
// avatar (28px) + the internal flex gap pushes the name 40px right of the
// header label, which reads as misalignment. Same pattern is used in Runtimes
// (icon-box and Health dot extracted into their own tracks).
//
// Responsive strategy mirrors the previous table:
//   <md  → [avatar] Agent · Status (compact dot only) · Actions
//   md+  → adds Last run + Runtime + Runs
//   lg+  → adds Activity sparkline
// Cells use `hidden md:block` / `hidden lg:block` so they participate in the
// grid only at their breakpoint; the grid template at each tier matches the
// number of visible cells exactly.
//
// Two presence columns at md+ (Status + Last run) instead of one merged
// cell — splitting them lets the user scan each axis independently. The
// dot column is the same 3-color availability everywhere; the Last run
// column shows the task icon + label (running/completed/failed/etc).
export const AGENT_LIST_GRID =
  "grid items-center gap-4 " +
  "grid-cols-[1.75rem_minmax(0,1fr)_max-content_2.5rem] " +
  "md:grid-cols-[1.75rem_minmax(0,1.4fr)_5rem_minmax(0,max-content)_minmax(0,0.8fr)_4rem_2.5rem] " +
  "lg:grid-cols-[1.75rem_minmax(0,1.5fr)_5rem_minmax(0,max-content)_minmax(0,0.8fr)_5rem_4rem_2.5rem]";

interface AgentListItemProps {
  agent: Agent;
  runtime: AgentRuntime | null;
  presence: AgentPresenceDetail | null | undefined;
  // 30d activity series for this agent. Page derives once for the whole
  // workspace and passes a row-specific slice here, so the row component
  // doesn't subscribe to its own query (avoids N timers / N subscriptions).
  // The list only surfaces the trailing 7 days; we still take the 30d
  // shape so the cache stays aligned with the agent detail panel.
  activity: AgentActivity | null | undefined;
  // 30-day cumulative run count for the RUNS column. Same single-source pattern.
  runCount: number | null | undefined;
  // Inline owner avatar — non-null when the page wants to attribute the
  // agent to a teammate (typically All scope on someone else's agent).
  // The page does the "scope === all && owner !== me" decision so the row
  // stays pure presentation.
  ownerIdToShow: string | null;
  // True when the current user can archive / cancel-tasks on this agent.
  // Mirrors the back-end's canManageAgent check; the row uses it to gate
  // entries in the actions dropdown.
  canManage: boolean;
  // Page-level callback to open Create dialog with this agent as a
  // template (Duplicate action).
  onDuplicate: (agent: Agent) => void;
  href: string;
}

export function AgentListItem({
  agent,
  runtime,
  presence,
  activity,
  runCount,
  ownerIdToShow,
  canManage,
  onDuplicate,
  href,
}: AgentListItemProps) {
  const isArchived = !!agent.archived_at;
  const isPrivate = agent.visibility === "private";
  const isCloud = agent.runtime_mode === "cloud";
  const RuntimeIcon = isCloud ? Cloud : Monitor;
  const runtimeLabel = runtime?.name ?? (isCloud ? "Cloud" : "Local");

  return (
    <AppLink
      href={href}
      className={`${AGENT_LIST_GRID} group border-b px-4 py-3 text-sm transition-colors last:border-b-0 hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none`}
    >
      {/* Avatar — dedicated leading column so the header label "Agent"
          aligns with the name text below it. */}
      <ActorAvatar
        actorType="agent"
        actorId={agent.id}
        size={28}
        className={`rounded-md ${isArchived ? "opacity-50 grayscale" : ""}`}
        showStatusDot
      />

      {/* Agent — primary text column, eats remaining space. */}
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span
            className={`truncate font-medium ${
              isArchived ? "text-muted-foreground" : ""
            }`}
          >
            {agent.name}
          </span>
          {/* Lock = private visibility — back-end rejects assignment by
              non-owners, so flag it visually. We deliberately do NOT
              filter private agents out of the list (mirrors server's
              ListAgents behaviour); the icon warns the viewer that
              picking this in a picker will fail. */}
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
          {/* Owner attribution — only set in "All" scope when the agent
              isn't yours. Tiny avatar (14px) keeps it lightweight; the
              hover card on the agent's main avatar already covers the
              full owner detail. */}
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

      {/* Status — availability dimension only. Compact dot under md;
          dot + label at md+. Always 3 colours. */}
      <div className="flex items-center">
        {isArchived ? (
          <span className="text-xs text-muted-foreground">—</span>
        ) : (
          <>
            <span className="md:hidden">
              <AgentPresenceIndicator detail={presence} compact />
            </span>
            <span className="hidden md:inline-flex">
              <AvailabilityCell presence={presence} />
            </span>
          </>
        )}
      </div>

      {/* Last run — md+. Task icon + label + (running counts | reason | time).
          Dedicated column so the user can scan "what just happened" without
          merging it into the availability cell. */}
      <div className="hidden items-center md:flex">
        {isArchived ? (
          <span className="text-xs text-muted-foreground">—</span>
        ) : (
          <LastRunCell presence={presence} />
        )}
      </div>

      {/* Runtime — md+. Sans-font label fits ~25% more chars per pixel than
          the previous mono treatment, so most hostnames no longer truncate. */}
      <div className="hidden min-w-0 items-center gap-1.5 text-xs text-muted-foreground md:flex">
        <RuntimeIcon className="h-3 w-3 shrink-0" />
        <Tooltip>
          <TooltipTrigger
            render={<span className="min-w-0 truncate">{runtimeLabel}</span>}
          />
          <TooltipContent>{runtimeLabel}</TooltipContent>
        </Tooltip>
      </div>

      {/* Activity (7d) — lg+. The 7d sparkline is sliced off the 30d
          workspace cache here (single source of truth shared with the
          detail panel); no extra request. */}
      <div className="hidden lg:block">
        {isArchived ? (
          <span className="text-xs text-muted-foreground/50">—</span>
        ) : !activity ? (
          <span
            className="inline-block animate-pulse rounded bg-muted/60"
            style={{ width: 64, height: 20 }}
          />
        ) : (
          <Tooltip>
            <TooltipTrigger
              render={
                <div className="inline-flex cursor-default items-center">
                  <Sparkline
                    buckets={summarizeActivityWindow(activity, 7).buckets}
                    width={64}
                    height={20}
                  />
                </div>
              }
            />
            <TooltipContent>
              <ActivityTooltip activity={activity} />
            </TooltipContent>
          </Tooltip>
        )}
      </div>

      {/* Runs (30d) — md+. tabular-nums + right-align keeps the digit column
          visually clean across orders of magnitude. */}
      <div className="hidden text-right font-mono text-xs tabular-nums text-muted-foreground md:block">
        {runCount == null ? "—" : runCount.toLocaleString()}
      </div>

      {/* Actions — kebab dropdown, always visible. Empty cell is preserved
          (the column has fixed width) when the user has no operable
          actions, so column alignment stays stable across rows. */}
      <div className="flex justify-end">
        <AgentRowActions
          agent={agent}
          presence={presence}
          canManage={canManage}
          onDuplicate={onDuplicate}
        />
      </div>
    </AppLink>
  );
}

// Availability cell — dot + label, colour from availabilityConfig. Three
// states only; the colour reflects "can the agent take work right now".
function AvailabilityCell({
  presence,
}: {
  presence: AgentPresenceDetail | null | undefined;
}) {
  if (!presence) {
    return <span className="inline-flex h-3 w-16 animate-pulse rounded bg-muted/60" />;
  }
  const av = availabilityConfig[presence.availability];
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${av.dotClass}`} />
      <span className={`text-xs ${av.textClass}`}>{av.label}</span>
    </span>
  );
}

// Last-run cell — task icon + label, with running counts only when active.
// No timestamp: it eats horizontal space and the relative age of a
// completed task isn't actionable in a scan. Hidden when idle so brand-
// new agents show "—" instead of "Idle" everywhere.
function LastRunCell({
  presence,
}: {
  presence: AgentPresenceDetail | null | undefined;
}) {
  if (!presence) {
    return <span className="inline-flex h-3 w-20 animate-pulse rounded bg-muted/60" />;
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
    <span className="inline-flex min-w-0 items-center gap-1 text-xs">
      <ts.icon className={`h-3 w-3 shrink-0 ${ts.textClass}`} />
      <span className={`shrink-0 ${ts.textClass}`}>{ts.label}</span>
      {counts && (
        <span className="truncate text-muted-foreground">{counts}</span>
      )}
    </span>
  );
}

/**
 * Tooltip body for the activity sparkline. Header = window label, body =
 * the actual counts (rolled up from the same 7-day slice the bars
 * render). Two short lines so the eye lands on the numbers immediately
 * on hover.
 */
function ActivityTooltip({ activity }: { activity: AgentActivity }) {
  const summary = summarizeActivityWindow(activity, 7);
  const { totalRuns, totalFailed } = summary;
  const { daysSinceCreated } = activity;

  // Header: when the agent is younger than the rendered window, label by
  // age so an empty sparkline reads as "new agent" not "broken agent".
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
