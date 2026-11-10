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

/**
 * Notifications that only tell the user something. The server archives them
 * when their issue is opened (inbox.NoticeTypes); every other type waits for
 * the user to archive it or for the issue to move on.
 */
const INBOX_NOTICE_TYPES: ReadonlySet<string> = new Set([
  "reaction_added",
  "unassigned",
  "issue_subscribed",
  "quick_create_done",
  "app_action_result",
]);

export function isInboxNotice(type: InboxItem["type"]): boolean {
  return INBOX_NOTICE_TYPES.has(type);
}

export interface InboxGroupSummary {
  /** Notifications on the issue besides the one its row shows. */
  more: number;
  /** The newest thing someone wrote on it, on one line. */
  preview: string | null;
}

/** What each issue's row says beyond its newest notification, keyed like the rows. */
export function summarizeInboxGroups(items: readonly InboxItem[]): Map<string, InboxGroupSummary> {
  const out = new Map<string, InboxGroupSummary>();
  for (const group of groupInboxRows(items)) {
    const first = group[0];
    if (!first) continue;
    const written = group.find(
      (item) => (item.type === "new_comment" || item.type === "mentioned") && item.body?.trim(),
    );
    out.set(first.issue_id ?? first.id, {
      more: group.length - 1,
      preview: written?.body ? written.body.replace(/\s+/g, " ").trim() : null,
    });
  }
  return out;
}

export interface InboxGroupUnread {
  /** The oldest unread comment or run on the issue, where reading starts. */
  target: string | null;
  /** Unread notifications that point at the timeline, that one included. */
  count: number;
}

/**
 * Where the unread part of an issue's timeline starts. A reaction points at a
 * comment the user wrote earlier, so it neither counts nor anchors.
 */
export function inboxGroupUnread(items: readonly InboxItem[], key: string): InboxGroupUnread {
  const unread = items
    .filter(
      (item) =>
        (item.issue_id ?? item.id) === key &&
        !item.read &&
        !item.archived &&
        item.type !== "reaction_added" &&
        !!(item.details?.comment_id ?? item.details?.task_id),
    )
    .sort(byNewest);
  const oldest = unread[unread.length - 1];
  return {
    target: oldest?.details?.comment_id ?? oldest?.details?.task_id ?? null,
    count: unread.length,
  };
}
