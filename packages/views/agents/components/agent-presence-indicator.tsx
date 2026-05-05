"use client";

import { Skeleton } from "@multica/ui/components/ui/skeleton";
import type { AgentPresenceDetail } from "@multica/core/agents";
import { useT } from "@multica/i18n/react";
import { availabilityConfig, workloadConfig } from "../presence";

interface PresenceIndicatorProps {
  // null/undefined = still loading. Caller passes the detail computed at
  // the page level (or via the useAgentPresenceDetail hook for single-agent
  // views). Keeping this as a prop avoids per-row hook subscriptions in
  // long lists.
  detail: AgentPresenceDetail | null | undefined;
  // Compact = dot only, no label / no workload chip. Used in dense rows.
  compact?: boolean;
}

/**
 * Renders an agent's two-dimension presence: an availability dot + an
 * optional workload chip. The dot's colour reads only from the
 * availability dimension (3 colours), so a runtime-healthy agent whose
 * last task failed shows a green dot — workload no longer carries
 * historical state at all.
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
  const t = useT("agents");
  if (!detail) {
    return compact ? (
      <Skeleton className="h-1.5 w-1.5 rounded-full" />
    ) : (
      <Skeleton className="h-3 w-24 rounded" />
    );
  }

  const av = availabilityConfig[detail.availability];
  const wl = workloadConfig[detail.workload];
  const isWorking = detail.workload === "working";
  const isQueued = detail.workload === "queued";
  const showQueueBadge = isWorking && detail.queuedCount > 0;
  // Queued's amber comes from workloadConfig as the *severe* tone — meant
  // for "stuck on offline runtime", which is the dominant cause. But on a
  // healthy runtime, queued is just a brief race between enqueue and the
  // daemon's claim, and amber there reads as a warning that isn't there.
  // Compose with availability: online ⇒ muted (transient), otherwise ⇒
  // keep amber (genuine stuck signal).
  const queuedTone =
    detail.availability === "online" ? "text-muted-foreground" : wl.textClass;

  const avLabel = t(`availability_${detail.availability}`);
  const wlLabel = t(`workload_${detail.workload}`);

  if (compact) {
    return (
      <span
        className="inline-flex items-center"
        title={`${avLabel}${detail.workload !== "idle" ? ` · ${wlLabel}` : ""}`}
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
        <span className={`text-xs ${av.textClass}`}>{avLabel}</span>
      </span>

      {/* Workload — separator + label, with counts when working/queued.
          All three workload states render here for symmetry: idle gets
          its own "Idle" label so the difference between "no presence
          data" (no chip at all) and "agent is idle" (explicit Idle chip)
          is visible. */}
      <span className="inline-flex items-center gap-1">
        <span className="text-xs text-muted-foreground">·</span>
        <span
          className={`text-xs ${
            isQueued ? queuedTone : wl.textClass
          }`}
        >
          {wlLabel}
        </span>
        {isWorking && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground">
            {detail.runningCount} / {detail.capacity}
          </span>
        )}
        {showQueueBadge && (
          <span className="rounded-md bg-muted px-1 py-0 text-xs font-medium text-muted-foreground">
            +{detail.queuedCount} {t("presence_queued")}
          </span>
        )}
        {/* Queued (no running) — show the queued count directly, since
            there's no running/capacity ratio to anchor on. Honestly
            surfaces "stuck" on offline runtimes. */}
        {isQueued && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground">
            {detail.queuedCount}
          </span>
        )}
      </span>
    </span>
  );
}
