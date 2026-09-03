"use client";

import { useEffect, useMemo, useState } from "react";
import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  HoverCard,
  HoverCardContent,
  HoverCardTrigger,
} from "@multica/ui/components/ui/hover-card";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { issueStatusDurationsOptions } from "@multica/core/issues/queries";
import type { StatusDurationEntry } from "@multica/core/types";
import { StatusIcon } from "./status-icon";
import { useT } from "../../i18n";
import { useStatusLabel } from "../utils/status-label";
import { statusDurationParts } from "../utils/status-duration";

/**
 * Latency between the server closing off the open segment and this client
 * rendering it is seconds at worst. A larger gap means the two clocks
 * disagree, not that time passed, so the drift correction is discarded rather
 * than trusted — better to under-report the live segment by a few seconds than
 * to add hours of clock skew to it.
 */
const MAX_TRUSTED_DRIFT_SECONDS = 300;

/**
 * Ticks once a second so the current status's duration counts up while the
 * card is open. The interval exists only while the card is mounted — Base UI
 * tears the portalled popup down on close — so a closed card costs nothing.
 *
 * Returns elapsed milliseconds since mount rather than a wall-clock instant,
 * so the figure advances at the right RATE even on a client whose clock is
 * wrong; absolute skew is handled separately and defensively below.
 */
function useElapsedSinceOpen(): number {
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    const startedAt = Date.now();
    const id = setInterval(() => setElapsed(Date.now() - startedAt), 1000);
    return () => clearInterval(id);
  }, []);
  return elapsed;
}

/** One status row: glyph, label, duration. */
function StatusDurationRow({
  entry,
  extraSeconds,
  wsId,
}: {
  entry: StatusDurationEntry;
  /** Live seconds to add — non-zero only for the current status. */
  extraSeconds: number;
  wsId: string;
}) {
  const { t } = useT("issues");
  const { categoryOf, colorOf } = useIssueStatuses(wsId);
  const labelOf = useStatusLabel(wsId);

  const { value, unit } = statusDurationParts(entry.seconds + extraSeconds);

  return (
    <div className="flex items-center gap-2">
      <StatusIcon
        status={entry.status}
        category={categoryOf(entry.status)}
        color={colorOf(entry.status)}
        className="h-3.5 w-3.5 shrink-0"
      />
      {/* The current status is the one the sidebar row already shows;
          everything above it is history. Muting the past rows makes that
          reading order obvious without spending a second column on it. */}
      <span
        className={`min-w-0 flex-1 truncate ${
          entry.current ? "text-foreground" : "text-muted-foreground"
        }`}
      >
        {labelOf(entry.status)}
      </span>
      <span className="shrink-0 tabular-nums text-muted-foreground">
        {t(($) => $.status_duration[unit], { count: value })}
      </span>
    </div>
  );
}

/**
 * "Time in status" hover card for the issue-detail sidebar status row.
 *
 * Shows how long the issue has spent on each status it has passed through,
 * summed across repeat visits and ordered by first entry, so the list reads as
 * the issue's history from the top down.
 *
 * Like IssueHoverCard, the query lives in the BODY, not here: Base UI mounts
 * the popup subtree only while the card is open, so this component adds no
 * request until a user actually points at the row. Hoisting the query up would
 * fire one per mounted issue detail.
 */
export function StatusDurationHover({
  issueId,
  wsId,
  children,
  disabled = false,
  delay,
}: {
  issueId: string;
  wsId: string;
  children: ReactNode;
  /**
   * Suppresses the card while the status picker popover is open. Two layers
   * anchored to the same element read as a glitch, and a user who just clicked
   * to CHANGE the status is no longer asking how long it has been there.
   */
  disabled?: boolean;
  /**
   * Open-delay override in milliseconds, for tests. Production passes nothing
   * and gets Base UI's default, matching every other hover card in the app.
   */
  delay?: number;
}) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(false);

  // Driving `open` rather than unmounting the HoverCard on `disabled` keeps
  // the trigger's DOM node stable, so the picker anchored inside it does not
  // lose its position mid-interaction.
  const isOpen = open && !disabled;

  // Clear the latched hover when the picker takes over, not just mask it.
  // Masking alone makes the card flash back on when the picker closes: the
  // pointer is wherever the chosen option was, so `open` is still true from
  // the hover that preceded the click. Reopening should require a fresh hover.
  useEffect(() => {
    if (disabled) setOpen(false);
  }, [disabled]);

  return (
    <HoverCard open={isOpen} onOpenChange={setOpen}>
      {/* A real box, not `display: contents`. The positioner anchors to the
          trigger's bounding rect, and a `contents` element has none — the card
          renders pinned to the viewport corner instead of beside the row.
          `flex min-w-0` keeps it transparent to the surrounding layout: it is a
          flex item in PropRow's value cell and passes the truncation budget
          straight through to the picker inside. */}
      <HoverCardTrigger
        delay={delay}
        render={<span className="flex min-w-0 items-center" />}
      >
        {children}
      </HoverCardTrigger>
      <HoverCardContent side="left" align="start" className="w-60">
        <div className="mb-2 text-caption font-medium">
          {t(($) => $.status_duration.title)}
        </div>
        <StatusDurationBody issueId={issueId} wsId={wsId} />
      </HoverCardContent>
    </HoverCard>
  );
}

/**
 * Card body — mounted only while the card is open, which is what makes the
 * fetch lazy. Exported so tests can render it directly instead of driving real
 * pointer hover through a portal.
 */
export function StatusDurationBody({
  issueId,
  wsId,
}: {
  issueId: string;
  wsId: string;
}) {
  const { t } = useT("issues");
  const elapsedMs = useElapsedSinceOpen();
  const { data, isPending, isError } = useQuery(
    issueStatusDurationsOptions(issueId),
  );

  const computedAt = data?.computed_at;

  // Seconds to add to the current status on FIRST paint, so the figure already
  // accounts for request latency instead of restarting from the server's
  // snapshot. Recomputed only when a new response arrives.
  const driftSeconds = useMemo(() => {
    if (!computedAt) return 0;
    const computed = new Date(computedAt).getTime();
    if (!Number.isFinite(computed)) return 0;
    const drift = Math.floor((Date.now() - computed) / 1000);
    if (drift < 0 || drift > MAX_TRUSTED_DRIFT_SECONDS) return 0;
    return drift;
  }, [computedAt]);

  if (isPending) {
    return (
      <div
        data-testid="status-duration-skeleton"
        className="flex flex-col gap-1.5"
      >
        <Skeleton className="h-4 w-full" />
        <Skeleton className="h-4 w-4/5" />
      </div>
    );
  }

  // One terminal state for every way the aggregate can fail to arrive: an
  // error after retries, or a response with nothing in it.
  if (isError || !data || data.entries.length === 0) {
    return (
      <p className="text-caption text-muted-foreground">
        {t(($) => $.status_duration.unavailable)}
      </p>
    );
  }

  // The ticker advances only the CURRENT status — past segments are closed and
  // their totals are final.
  const liveExtra = driftSeconds + Math.floor(elapsedMs / 1000);

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-col gap-1.5 text-caption">
        {data.entries.map((entry) => (
          <StatusDurationRow
            key={entry.status}
            entry={entry}
            extraSeconds={entry.current ? liveExtra : 0}
            wsId={wsId}
          />
        ))}
      </div>
      {/* Says so when the numbers are a reconstruction rather than recorded
          history, instead of presenting the two as the same thing. */}
      {data.partial && (
        <p className="text-micro text-muted-foreground">
          {t(($) => $.status_duration.partial_note)}
        </p>
      )}
    </div>
  );
}
