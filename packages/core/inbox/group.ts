import type { InboxItem } from "../types";

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

/**
 * The row a group renders as, opening where the reader left off: the oldest
 * unread comment, so the thread reads forward from there, or the newest
 * comment once the issue is read. An app notification lands on its own
 * request instead, so it never borrows one.
 */
export function withCommentAnchor(row: InboxItem, group: readonly InboxItem[]): InboxItem {
  if (row.type.startsWith("app_") && row.details?.request_id) return row;
  const hasComment = (item: InboxItem) => !!item.details?.comment_id;
  const anchor =
    group.findLast((item) => item.read !== true && hasComment(item)) ?? group.find(hasComment);
  const commentId = anchor?.details?.comment_id;
  if (!commentId || commentId === row.details?.comment_id) return row;
  return { ...row, details: { ...(row.details ?? {}), comment_id: commentId } };
}

/** One row per issue: its newest notification. Newest group first. */
export function groupInboxItemsByIssue(items: readonly InboxItem[]): InboxItem[] {
  return groupInboxRows(items)
    .flatMap((group) => (group[0] ? [withCommentAnchor(group[0], group)] : []))
    .sort(byNewest);
}

export function sortInboxNewestFirst(items: InboxItem[]): InboxItem[] {
  return items.sort(byNewest);
}
