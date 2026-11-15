import type { InboxItem } from "../types";
import { inboxRowTime } from "./snooze";

function byNewest(a: InboxItem, b: InboxItem): number {
  return new Date(b.created_at).getTime() - new Date(a.created_at).getTime();
}

/** Notifications grouped by issue (`issue_id ?? id`), each group newest first. */
export function groupInboxRows(items: readonly InboxItem[]): InboxItem[][] {
  const groups = new Map<string, InboxItem[]>();
  for (const item of items) {
    const key = item.issue_id ?? item.id;
    const group = groups.get(key);
    if (group) group.push(item);
    else groups.set(key, [item]);
  }
  const out = [...groups.values()];
  for (const group of out) group.sort(byNewest);
  return out;
}

export interface InboxAnchorOptions {
  /** An app request row opens at its request rather than a timeline entry. Defaults to true. */
  keepRequestTarget?: boolean;
}

/**
 * The timeline entry a notification is about: its comment, or the failed run
 * it reports, which the timeline addresses by task id like a comment.
 */
function inboxTimelineTarget(item: InboxItem): string | undefined {
  if (item.details?.comment_id) return item.details.comment_id;
  if (item.type === "task_failed" && item.details?.task_id) return item.details.task_id;
  return undefined;
}

/**
 * The row a group renders as, with `details.comment_id` naming the timeline
 * entry it opens at. Unread, it opens at the oldest unread comment or run, so
 * the issue reads forward from there; a reaction leads only when nothing
 * else unread has an entry, since the comment it marks is usually old.
 * Unread with no entry at all (a status change, an assignment), it opens
 * where the reader left off rather than at a comment already read. Read, it
 * opens at the newest notification's entry. An app notification lands on its
 * own request instead, so it never borrows one — unless `keepRequestTarget`
 * is off, for a client that renders no requests.
 */
export function withCommentAnchor(
  row: InboxItem,
  group: readonly InboxItem[],
  { keepRequestTarget = true }: InboxAnchorOptions = {},
): InboxItem {
  if (keepRequestTarget && row.type.startsWith("app_") && row.details?.request_id) return row;
  const unread = group.filter((item) => item.read !== true);
  const anchor =
    unread.length > 0
      ? (unread.findLast((item) => item.type !== "reaction_added" && !!inboxTimelineTarget(item)) ??
        unread.findLast((item) => !!inboxTimelineTarget(item)))
      : group.find((item) => !!inboxTimelineTarget(item));
  const target = anchor ? inboxTimelineTarget(anchor) : undefined;
  if (target === row.details?.comment_id) return row;
  const { comment_id: _dropped, ...details } = row.details ?? {};
  return { ...row, details: target ? { ...details, comment_id: target } : details };
}

/**
 * One row per issue: its newest notification. Newest group first, where an
 * issue back from a snooze counts from when it came back.
 */
export function groupInboxItemsByIssue(
  items: readonly InboxItem[],
  options?: InboxAnchorOptions,
): InboxItem[] {
  const now = Date.now();
  return groupInboxRows(items)
    .flatMap((group) => (group[0] ? [withCommentAnchor(group[0], group, options)] : []))
    .sort((a, b) => inboxRowTime(b, now) - inboxRowTime(a, now));
}

/** Newest first, where an issue back from a snooze counts from when it came back. */
export function sortInboxNewestFirst(items: InboxItem[]): InboxItem[] {
  const now = Date.now();
  return items.sort((a, b) => inboxRowTime(b, now) - inboxRowTime(a, now));
}
