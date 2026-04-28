"use client";

import { Skeleton } from "@multica/ui/components/ui/skeleton";
import type { AgentPresenceDetail } from "@multica/core/agents";
import { availabilityConfig, taskStateConfig } from "../presence";

interface PresenceIndicatorProps {
  // null/undefined = still loading. Caller passes the detail computed at
  // the page level (or via the useAgentPresenceDetail hook for single-agent
  // views). Keeping this as a prop avoids per-row hook subscriptions in
  // long lists.
  detail: AgentPresenceDetail | null | undefined;
  // Compact = dot only, no label / no last-task chip. Used in dense rows.
  compact?: boolean;
}

/**
 * Renders an agent's two-dimension presence: an availability dot + an
 * optional last-task chip. The dot's colour reads only from the
 * availability dimension (3 colours), so a runtime-healthy agent whose
 * last task failed shows a green dot + a red "Failed" chip — the dot
 * stops being sticky-red.
 *
 * Compact mode collapses to dot-only — used in dense surfaces where the
 * full chip would crowd the row.
 *
 * Pure presentation — takes the already-derived detail object as a prop.
 * The page-level component is responsible for sourcing it (via
 * `useAgentPresenceDetail` for a single agent, or `useWorkspacePresenceMap`
 * for lists).
 */
export function AgentPresenceIndicator({
  detail,
  compact,
}: PresenceIndicatorProps) {
  if (!detail) {
    return compact ? (
      <Skeleton className="h-1.5 w-1.5 rounded-full" />
    ) : (
      <Skeleton className="h-3 w-24 rounded" />
    );
  }

  const av = availabilityConfig[detail.availability];
  const ts = taskStateConfig[detail.lastTask];
  const isRunning = detail.lastTask === "running";
  const showQueueBadge = isRunning && detail.queuedCount > 0;

  if (compact) {
    return (
      <span
        className="inline-flex items-center"
        title={`${av.label}${detail.lastTask !== "idle" ? ` · ${ts.label}` : ""}`}
      >
        <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${av.dotClass}`} />
      </span>
    );
  }

  return (
    <span className="inline-flex flex-wrap items-center gap-x-1.5 gap-y-0.5">
      {/* Availability — dot + label. Single dimension, single colour. */}
      <span className="inline-flex items-center gap-1.5">
        <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${av.dotClass}`} />
        <span className={`text-xs ${av.textClass}`}>{av.label}</span>
      </span>

      {/* Last task — separator + label, with running counts when active.
          Hidden for `idle` to keep brand-new agents clean. */}
      {detail.lastTask !== "idle" && (
        <span className="inline-flex items-center gap-1">
          <span className="text-xs text-muted-foreground">·</span>
          <span className={`text-xs ${ts.textClass}`}>{ts.label}</span>
          {isRunning && (
            <span className="font-mono text-xs tabular-nums text-muted-foreground">
              {detail.runningCount} / {detail.capacity}
            </span>
          )}
          {showQueueBadge && (
            <span className="rounded-md bg-muted px-1 py-0 text-xs font-medium text-muted-foreground">
              +{detail.queuedCount} queued
            </span>
          )}
        </span>
      )}
    </span>
  );
}
