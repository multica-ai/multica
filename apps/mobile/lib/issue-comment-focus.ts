import type { TimelineRow } from "@/lib/timeline-thread";

/** Resolve a root or nested reply to the FlashList row that owns its thread. */
export function findCommentFocusRowIndex(
  rows: readonly TimelineRow[],
  commentId: string,
): number {
  return rows.findIndex(
    (row) =>
      row.entry.id === commentId ||
      row.replies.some((reply) => reply.id === commentId),
  );
}

interface FocusOffsetInput {
  currentOffset: number;
  targetTop: number;
  targetHeight: number;
  viewportTop: number;
  viewportHeight: number;
  padding?: number;
}

/**
 * Return the content offset needed to reveal the exact nested comment after
 * its owning FlashList row has been materialized. Screen coordinates keep the
 * calculation independent from the list header and recycled-cell layout.
 */
export function getCommentFocusOffset({
  currentOffset,
  targetTop,
  targetHeight,
  viewportTop,
  viewportHeight,
  padding = 16,
}: FocusOffsetInput): number | null {
  const visibleTop = viewportTop + padding;
  const visibleBottom = viewportTop + viewportHeight - padding;
  const targetBottom = targetTop + targetHeight;

  if (targetTop >= visibleTop && targetBottom <= visibleBottom) return null;

  const availableHeight = Math.max(0, viewportHeight - padding * 2);
  const desiredTop =
    targetHeight > availableHeight
      ? visibleTop
      : visibleTop + (availableHeight - targetHeight) / 2;

  return Math.max(0, currentOffset + targetTop - desiredTop);
}
