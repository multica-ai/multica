/**
 * Comment-directory derivation for the issue comments modal (RUYI-28).
 *
 * Pure functions over the same `TimelineRow[]` the timeline already
 * renders — the modal reads the existing TanStack timeline cache and
 * derives its list locally; no server state is copied or refetched
 * (approved architecture: read the existing TanStack timeline cache).
 *
 * `buildCommentDirectory` keeps the timeline's ASC (oldest-first) order —
 * the spec asks for a root-ASC list, i.e. the same order the user scrolls
 * through, not newest-first.
 */
import type { TimelineEntry } from "@multica/core/types";
import { commentSummary } from "./comment-summary";
import type { TimelineRow } from "./timeline-thread";

export interface CommentDirectoryItem {
  rootId: string;
  /** Root comment's actor ref for avatar + author-name resolution. */
  authorType: string;
  authorId: string;
  createdAt: string;
  /** Reply count inside the thread (direct + nested, flattened). */
  replyCount: number;
  /** 120-cp / 2-line summary of the root comment body. */
  summary: string;
  /** Raw root content — the modal's own summary cut can re-run cheaply. */
  content: string;
  resolved: boolean;
}

export function buildCommentDirectory(
  rows: TimelineRow[],
): CommentDirectoryItem[] {
  // Defensive ASC sort by created_at. Server returns ASC and
  // buildTimelineRows preserves input order, but WS patches / optimistic
  // inserts append at the end — an out-of-order row must not shuffle the
  // directory. Stable: equal timestamps keep the input (timeline) order.
  //
  // Row-level filter only: every TimelineRow is a TOP-LEVEL row by
  // construction (buildTimelineRows folds nested replies into
  // `row.replies`), INCLUDING orphan replies promoted to top-level when
  // their parent is missing from the batch (web #1857 rescue). The old
  // `!parent_id` check dropped those promoted rows — a comment the
  // timeline renders but the directory hid (Counts-agree violation).
  const ordered = [...rows]
    .filter((r) => r.entry.type === "comment")
    .sort((a, b) =>
      a.entry.created_at < b.entry.created_at
        ? -1
        : a.entry.created_at > b.entry.created_at
          ? 1
          : 0,
    );
  const out: CommentDirectoryItem[] = [];
  for (const row of ordered) {
    const e: TimelineEntry = row.entry;
    out.push({
      rootId: e.id,
      authorType: e.actor_type,
      authorId: e.actor_id,
      createdAt: e.created_at,
      replyCount: row.replies.length,
      summary: commentSummary(e.content),
      content: e.content ?? "",
      resolved: !!e.resolved_at,
    });
  }
  return out;
}

/**
 * Local, case-insensitive substring filter over author display name and
 * summary text. Runs on-device against the derived list — no network.
 *
 * `authorName` is optional; when the caller hasn't resolved a display
 * name yet (actor lists still loading) only the summary participates.
 */
export function filterCommentDirectory(
  items: CommentDirectoryItem[],
  query: string,
  authorNames?: Record<string, string>,
): CommentDirectoryItem[] {
  const q = query.trim().toLowerCase();
  if (!q) return items;
  return items.filter((it) => {
    if (it.summary.toLowerCase().includes(q)) return true;
    if (it.content.toLowerCase().includes(q)) return true;
    const name = authorNames?.[it.rootId];
    if (name && name.toLowerCase().includes(q)) return true;
    return false;
  });
}
