"use client";

import { memo, useCallback, useMemo, useState } from "react";
import { Bell, Clock3 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import {
  HoverCard,
  HoverCardTrigger,
  HoverCardContent,
} from "@multica/ui/components/ui/hover-card";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import type { AgentTask, IssueWakeupSummaryRow } from "@multica/core/types";
import { workspaceWakeupSummariesOptions } from "@multica/core/issues/wakeups";
import { cn } from "@multica/ui/lib/utils";
import type { AvatarSize } from "@multica/ui/lib/avatar-size";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { AgentActivityHoverContent } from "../../agents/components/agent-activity-hover-content";
import { selectIssueTasks, type IssueTaskGroups } from "../surface/activity";
import { useT } from "../../i18n";
import { useWakeupText } from "./wakeup-presentation";

const EMPTY_GROUPS: IssueTaskGroups = { running: [], queued: [] };

// Dwell threshold before the activity card opens (MUL-5189).
//
// This badge is a passive cue riding on the right edge of dense scrolling
// lists (inbox rows, issue rows, board cards), and it appears on every issue
// an agent currently touches. Base UI's 600ms default is tuned for a hover
// target the user aims at; here the pointer crosses the badge constantly on
// its way to the row, the archive button, or the next row, so 600ms fires on
// travel rather than on intent and a 288px card lands over the rows below.
//
// 900ms sits past casual travel but still inside a deliberate "what is it
// doing?" pause. The header chip (issue-agent-header-chip) keeps its 150ms
// on purpose: it is one large chip the user aims at, not a per-row cue.
//
// The card body is read-only — no links, no buttons — so there is no hover
// bridge to protect and the close delay only needs to absorb pointer wobble
// across the 4px gap.
const OPEN_DELAY_MS = 900;
const CLOSE_DELAY_MS = 150;

interface IssueAgentActivityIndicatorProps {
  issueId: string;
  // Avatar tier. Kept very small — this is a corner-of-card cue, not a
  // primary control. Default xs (16 px) reads as a dot at typical board
  // densities while still showing the agent's face on hover-zoom.
  size?: AvatarSize;
  // Whether hovering opens the activity card. Opt OUT where the card's only
  // incremental information is not worth a popup (Inbox — see below).
  hoverCard?: boolean;
}

/**
 * Small "is there an agent working on this issue right now" badge shown
 * in the top-right of board cards and right after the identifier in list
 * rows. Derives state from the workspace-wide agent task snapshot:
 *
 *   - has ≥1 running task  → tiny avatar stack + shimmering "Working"
 *   - 0 running, ≥1 queued → half-opacity stack + muted "Queued"
 *   - future wakeups only   → next time/event + remaining count
 *   - no tasks or wakeups   → return null (no chrome, no placeholder)
 *
 * The shimmer reuses chat's `animate-chat-text-shimmer` utility (defined
 * in packages/ui/styles/base.css). Earlier iterations layered a brand
 * ring + opacity pulse around the avatars; both read as nervous on a
 * dense board. Moving the "alive" signal onto the label keeps the
 * avatars themselves still and lets the cue ride a piece of text the
 * user can already read.
 *
 * Hover opens AgentActivityHoverContent which lists every active task
 * with status dot + duration. No link rows — the card itself is the
 * navigation target for issue detail.
 *
 * Surfaces that only need the cue can pass `hoverCard={false}` and get the
 * badge alone. Inbox does (MUL-5189): the badge already shows who is running
 * and whether they are working or queued, so on a triage surface the card's
 * only incremental fact is elapsed time — which never changes the one
 * decision an inbox row exists to support ("do I open this?"). Issue lists
 * and board cards keep it: monitoring work in flight is what those views are
 * for, and elapsed time is load-bearing there.
 *
 * Subscribes to the one shared workspace snapshot query but narrows it to
 * this issue's tasks with a `select`. React Query's structural sharing keeps
 * that selected value referentially stable when this issue's tasks are
 * unchanged, so a snapshot invalidation (WS task:* events, driven by
 * use-realtime-sync) only re-renders the rows whose own tasks actually moved
 * — not the whole list. This is the de-amplification that keeps large issue
 * lists cheap when agents are busy (MUL-4474). 30s staleTime is the offline
 * fallback only.
 */
export const IssueAgentActivityIndicator = memo(
  function IssueAgentActivityIndicator({
    issueId,
    size = "xs",
    hoverCard = true,
  }: IssueAgentActivityIndicatorProps) {
    const { t } = useT("issues");
    const wsId = useWorkspaceId();
    const text = useWakeupText();
    const [open, setOpen] = useState(false);
    const selectWakeups = useCallback(
      (rows: IssueWakeupSummaryRow[]) =>
        rows.filter((row) => row.issue_id === issueId),
      [issueId],
    );
    const { data: wakeups = [] } = useQuery({
      ...workspaceWakeupSummariesOptions(wsId),
      select: selectWakeups,
    });
    const select = useCallback(
      (snapshot: AgentTask[]) => selectIssueTasks(snapshot, issueId),
      [issueId],
    );
    const { data: groups = EMPTY_GROUPS } = useQuery({
      ...agentTaskSnapshotOptions(wsId),
      select,
    });

    const { agentIds, opacity } = useMemo(() => {
      // Stack heads: prefer running. If 0 running, fall back to queued.
      // Each case is visually distinct (running gets shimmer, queued gets
      // muted text) so the indicator always offers a face to hover.
      const primary =
        groups.running.length > 0 ? groups.running : groups.queued;
      const uniqueAgents = [...new Set(primary.map((t) => t.agent_id))];
      return {
        agentIds: uniqueAgents,
        opacity: (groups.running.length > 0 ? "full" : "half") as
          | "full"
          | "half",
      };
    }, [groups]);

    const wakeupCount = wakeups[0]?.active_count ?? 0;
    const hasTasks = agentIds.length > 0;
    const hoverTasks = [...groups.running, ...groups.queued];
    const wakeupTriggered = hoverTasks.some((task) => !!task.wakeup_id);
    if (!hasTasks && !wakeupCount) return null;
    const isRunning = opacity === "full";
    const waitingLabel =
      wakeups[0]?.kind === "event"
        ? t(($) => $.wakeups.waiting_event)
        : wakeups[0]
          ? text.trigger(wakeups[0])
          : "";
    const label = hasTasks
      ? isRunning
        ? t(($) => $.agent_activity.status_running)
        : t(($) => $.agent_activity.status_queued)
      : waitingLabel;

    const badge = hasTasks ? (
      <>
        <AgentAvatarStack
          agentIds={agentIds}
          size={size}
          opacity={opacity}
          max={3}
        />
        {/* No leading-none: the shimmer paints glyphs via background-clip:
          text, and the background only covers the line box — a squeezed
          line box leaves descenders transparent. */}
        <span
          className={cn(
            "text-micro",
            isRunning ? "animate-chat-text-shimmer" : "text-muted-foreground",
          )}
        >
          {isRunning
            ? t(($) => $.agent_activity.status_running)
            : t(($) => $.agent_activity.status_queued)}
        </span>
        {wakeupTriggered && (
          <Bell
            className="size-3 text-muted-foreground"
            aria-label={t(($) => $.wakeups.triggered_by_wakeup)}
          />
        )}
        {wakeupCount > 0 && (
          <span
            className="inline-flex items-center gap-1 text-micro tabular-nums text-muted-foreground"
            title={t(($) => $.wakeups.upcoming, { count: wakeupCount })}
          >
            {!wakeupTriggered && <Bell className="size-3" aria-hidden="true" />}
            +{wakeupCount}
          </span>
        )}
      </>
    ) : (
      <>
        {wakeups[0]?.kind === "event" ? (
          <Bell className="size-3 text-muted-foreground" aria-hidden="true" />
        ) : (
          <Clock3 className="size-3 text-muted-foreground" aria-hidden="true" />
        )}
        <span className="max-w-36 truncate text-micro text-muted-foreground">
          {waitingLabel}
        </span>
        {wakeupCount > 1 && (
          <span className="text-micro tabular-nums text-muted-foreground">
            +{wakeupCount - 1}
          </span>
        )}
      </>
    );

    if (!hoverCard) {
      return (
        <span className="inline-flex shrink-0 items-center gap-1">{badge}</span>
      );
    }

    return (
      <HoverCard open={open} onOpenChange={setOpen}>
        <HoverCardTrigger
          delay={OPEN_DELAY_MS}
          closeDelay={CLOSE_DELAY_MS}
          render={
            <span
              tabIndex={0}
              aria-label={`${label}${wakeupTriggered ? ` · ${t(($) => $.wakeups.triggered_by_wakeup)}` : ""}${wakeupCount ? ` · ${t(($) => $.wakeups.upcoming, { count: wakeupCount })}` : ""}`}
              onFocus={() => setOpen(true)}
              onBlur={() => setOpen(false)}
              className="inline-flex shrink-0 items-center gap-1 rounded-sm focus-visible:outline-2 focus-visible:outline-ring"
            />
          }
        >
          {badge}
        </HoverCardTrigger>
        <HoverCardContent align="end" className="w-72">
          {hasTasks && <AgentActivityHoverContent tasks={hoverTasks} />}
          {wakeupCount > 0 && (
            <div
              className={cn(
                "space-y-2 text-caption",
                hasTasks && "mt-2 border-t border-border pt-2",
              )}
            >
              <p className="text-muted-foreground">
                {t(($) => $.wakeups.upcoming, { count: wakeupCount })}
              </p>
              {wakeups.map((wakeup) => (
                <div key={wakeup.id}>
                  <p className="break-words font-medium">
                    {text.trigger(wakeup)}
                  </p>
                  <p className="text-muted-foreground">
                    {t(($) => $.wakeups.wake_agent, {
                      agent: wakeup.agent_name,
                    })}{" "}
                    · {text.schedule(wakeup)}
                  </p>
                </div>
              ))}
              {wakeupCount > wakeups.length && (
                <p className="text-muted-foreground">
                  {t(($) => $.wakeups.more, {
                    count: wakeupCount - wakeups.length,
                  })}
                </p>
              )}
            </div>
          )}
        </HoverCardContent>
      </HoverCard>
    );
  },
);
