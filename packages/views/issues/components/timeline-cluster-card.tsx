"use client";

import { Fragment } from "react";
import { ChevronDown } from "lucide-react";
import { Card } from "@multica/ui/components/ui/card";
import { ActorAvatar } from "../../common/actor-avatar";
import { useT } from "../../i18n";
import { cn } from "@multica/ui/lib/utils";
import type { TimelineEntry } from "@multica/core/types";

export const CLUSTER_MIN_SIZE = 3;

/**
 * Normalize the first line of a comment's content into a stable key so that
 * same-template comments (e.g. automated patrol summaries that differ only in
 * date/number) collapse into one cluster. Strips parenthesised content, dates,
 * times, bracketed ids, leading enumeration markers, and trailing punctuation;
 * collapses whitespace; lower-cases for case-insensitive comparison.
 */
export function normalizeTimelineFirstLine(content: string | undefined): string {
  if (!content) return "";
  const firstLine = content.split(/\r?\n/).find((l) => l.trim().length > 0) ?? "";
  return firstLine
    .replace(/[（(][^）)]*[）)]/g, "") // parenthesised content (full/half-width)
    .replace(/\[[^\]]*\]/g, "") // bracketed ids like [SHAR-201]
    .replace(/\d{4}[-/]\d{1,2}[-/]\d{1,2}/g, "") // dates 2026-10-20
    .replace(/\d{1,2}:\d{2}(:\d{2})?/g, "") // times 12:10
    .replace(/#[A-Za-z]{0,8}-?\d+/g, "") // issue numbers #SHAR-201 / #201
    .replace(/^\s*(?:[-*•·]|\d+[.、)）])\s*/, "") // leading enum markers
    .replace(/[。，,.!！?？:：;；、…\s]+$/g, "") // trailing punctuation/ws
    .trim()
    .replace(/\s+/g, " ")
    .toLowerCase();
}

/** Display title for a cluster — the first line with parenthesised content,
 *  dates, times, bracketed ids and leading enum markers stripped, case
 *  preserved (unlike {@link normalizeTimelineFirstLine}). */
export function clusterDisplayTitle(content: string | undefined): string {
  if (!content) return "";
  const firstLine = content.split(/\r?\n/).find((l) => l.trim().length > 0) ?? "";
  return firstLine
    .replace(/[（(][^）)]*[）)]/g, "")
    .replace(/\[[^\]]*\]/g, "")
    .replace(/\d{4}[-/]\d{1,2}[-/]\d{1,2}/g, "")
    .replace(/\d{1,2}:\d{2}(:\d{2})?/g, "")
    .replace(/#[A-Za-z]{0,8}-?\d+/g, "")
    .replace(/^\s*(?:[-*•·]|\d+[.、)）])\s*/, "")
    .replace(/[。，,.!！?？:：;；、…\s]+$/g, "")
    .trim()
    .replace(/\s+/g, " ");
}

/** Short date for the cluster header span, e.g. "10-20". */
export function clusterShortDate(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Short date + time for a cluster row, e.g. "10-20 12:10". */
export function clusterShortDateTime(iso: string | undefined): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

interface TimelineClusterCardProps {
  /** Cluster members in chronological order (oldest first, newest last). */
  entries: TimelineEntry[];
  /** Latest entry — drives header avatar + "latest" summary. */
  latest: TimelineEntry;
  /** Display title (first line of the template). */
  title: string;
  /** Whether the cluster card is expanded showing individual rows. */
  expanded: boolean;
  /** IDs of individual comments rendered as full cards inside the cluster. */
  expandedRowIds: ReadonlySet<string>;
  onToggle: () => void;
  onToggleRow: (entryId: string) => void;
  /** Renders a full CommentCard for a given entry, with id="comment-{id}". */
  renderEntry: (entry: TimelineEntry) => React.ReactNode;
  getActorName: (type: string, id: string) => string;
}

/**
 * Summary cluster card for same-origin same-template comments (≥3 entries).
 * Collapsed: a compact two-line card — title + latest summary.
 * Expanded: a compact row list; clicking a row expands it into a full
 * CommentCard in place (the same renderer as a standalone comment).
 */
export function TimelineClusterCard({
  entries,
  latest,
  title,
  expanded,
  expandedRowIds,
  onToggle,
  onToggleRow,
  renderEntry,
  getActorName,
}: TimelineClusterCardProps) {
  const { t } = useT("issues");

  const count = entries.length;
  const latestTime = clusterShortDateTime(latest.created_at);
  const latestSummary = (latest.content ?? "")
    .split(/\r?\n/)
    .find((l) => l.trim().length > 0) ?? "";
  const actorName =
    latest.actor_name || getActorName(latest.actor_type, latest.actor_id);
  const dateSpan =
    entries.length > 0
      ? `${clusterShortDate(entries[0]!.created_at)} – ${clusterShortDate(latest.created_at)}`
      : "";

  return (
    <div className="pb-3">
      <Card className="!py-0 !gap-0 overflow-hidden">
        {/* Header row */}
        <button
          type="button"
          aria-expanded={expanded}
          onClick={onToggle}
          className="flex w-full items-center gap-2.5 px-4 py-3 text-left transition-colors hover:bg-muted/50 cursor-pointer"
        >
          <ActorAvatar
            actorType={latest.actor_type}
            actorId={latest.actor_id}
            size="xs"
          />
          <span className="min-w-0 flex-1 truncate text-body font-medium text-foreground">
            {title || actorName}
          </span>
          <span className="inline-flex shrink-0 items-center rounded-full bg-muted px-1.5 text-micro tabular-nums text-muted-foreground" style={{ height: 18, lineHeight: "18px" }}>
            {t(($) => $.detail.timeline.group_count, { count })}
          </span>
          <span className="ml-auto shrink-0 text-micro tabular-nums text-muted-foreground">
            {dateSpan}
            <ChevronDown
              className={cn(
                "inline-block size-3 ml-1 transition-transform align-middle",
                expanded && "rotate-180",
              )}
            />
          </span>
        </button>
        {/* Collapsed: latest summary line */}
        {!expanded && (
          <div className="truncate px-4 pb-3 pl-[34px] text-caption text-muted-foreground">
            {t(($) => $.detail.timeline.group_latest, { time: latestTime, summary: latestSummary })}
          </div>
        )}
        {/* Expanded: compact row list, each click → full CommentCard */}
        {expanded && (
          <div>
            {entries.map((entry) =>
              expandedRowIds.has(entry.id) ? (
                <Fragment key={entry.id}>{renderEntry(entry)}</Fragment>
              ) : (
                <button
                  key={entry.id}
                  type="button"
                  onClick={() => onToggleRow(entry.id)}
                  className="flex h-7 w-full items-center gap-2 border-t border-border/60 px-4 text-caption transition-colors hover:bg-surface-hover"
                >
                  <span className="w-[68px] shrink-0 text-micro tabular-nums text-muted-foreground">
                    {clusterShortDateTime(entry.created_at)}
                  </span>
                  <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">
                    {(entry.content ?? "").split(/\r?\n/).find((l) => l.trim().length > 0) ?? ""}
                  </span>
                </button>
              ),
            )}
          </div>
        )}
      </Card>
    </div>
  );
}
