import type { TimelineEntry } from "@multica/core/types";

/**
 * True for a comment deleted while it still had replies (#8296). The server
 * keeps its row as a tombstone — empty content, no attachments, reactions or
 * resolution — so every reply keeps its direct parent. Clients render nothing
 * in its place: the replies it held render flat, where it was. The one
 * exception is a thread ROOT, which keeps a placeholder as the thread's head.
 */
export function isDeletedComment(entry: { deleted_at?: string | null }): boolean {
  return typeof entry.deleted_at === "string" && entry.deleted_at !== "";
}

/**
 * The comment a deep link should land on. A deleted REPLY renders nothing, so
 * an inbox notification or a share link that names one has no anchor to scroll
 * to and no row to flash: land on the comment just above where it was — the
 * nearest visible reply before it, or the thread root when it had none. The
 * original id stays the notification's identity; only the landing moves.
 *
 * Any other id is its own target, including a deleted ROOT — that one still
 * renders. `replies` must be the thread's replies in render order.
 */
export function commentLandingTarget(
  commentId: string,
  rootId: string,
  replies: readonly TimelineEntry[],
): string {
  const index = replies.findIndex((reply) => reply.id === commentId);
  if (index < 0 || !isDeletedComment(replies[index]!)) return commentId;
  for (let i = index - 1; i >= 0; i--) {
    const previous = replies[i]!;
    if (!isDeletedComment(previous)) return previous.id;
  }
  return rootId;
}

function hasReplies(entries: readonly TimelineEntry[], commentId: string): boolean {
  return entries.some((e) => e.type === "comment" && e.parent_id === commentId);
}

/**
 * Removes a comment and every cached descendant: what a server from before
 * #8296 does on delete, and the only safe reading of a removal event, since a
 * newer server never removes a comment that still has replies.
 */
export function removeCommentSubtree(entries: TimelineEntry[], commentId: string): TimelineEntry[] {
  const removed = new Set([commentId]);
  let changed = true;
  while (changed) {
    changed = false;
    for (const e of entries) {
      if (e.parent_id && removed.has(e.parent_id) && !removed.has(e.id)) {
        removed.add(e.id);
        changed = true;
      }
    }
  }
  return entries.filter((e) => !removed.has(e.id));
}

/**
 * Applies a confirmed comment delete to a flat timeline cache the way the
 * server does: a comment with replies becomes a tombstone; one without is
 * removed, together with every tombstone ancestor it leaves without replies.
 * The cache may not hold every reply, so callers still reconcile with the
 * server (realtime events plus a refetch).
 */
export function applyCommentDeletion(
  entries: TimelineEntry[],
  commentId: string,
  deletedAt: string,
): TimelineEntry[] {
  if (hasReplies(entries, commentId)) {
    return entries.map((e) =>
      e.id === commentId
        ? {
            ...e,
            content: "",
            attachments: [],
            reactions: [],
            resolved_at: null,
            resolved_by_type: null,
            resolved_by_id: null,
            deleted_at: deletedAt,
          }
        : e,
    );
  }

  const byId = new Map(entries.map((e) => [e.id, e]));
  let remaining = entries.filter((e) => e.id !== commentId);
  const visited = new Set([commentId]);
  let parentId = byId.get(commentId)?.parent_id;
  while (parentId && !visited.has(parentId)) {
    visited.add(parentId);
    const parent = byId.get(parentId);
    if (!parent || !isDeletedComment(parent) || hasReplies(remaining, parentId)) break;
    remaining = remaining.filter((e) => e.id !== parentId);
    parentId = parent.parent_id;
  }
  return remaining;
}
