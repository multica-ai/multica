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
 * The row a group renders as. It keeps the group's newest comment anchor when
 * it has none of its own, so opening it still scrolls to the comment.
 */
export function withCommentAnchor(row: InboxItem, group: readonly InboxItem[]): InboxItem {
  if (row.details?.comment_id) return row;
  const commentId = group.find((item) => item.details?.comment_id)?.details?.comment_id;
  if (!commentId) return row;
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
