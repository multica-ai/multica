import type { Comment, TimelineEntry } from "../types";
import { sortTimelineEntriesAsc } from "./timeline-sort";

export function commentToTimelineEntry(c: Comment): TimelineEntry {
  return {
    type: "comment",
    id: c.id,
    actor_type: c.author_type,
    actor_id: c.author_id,
    content: c.content,
    parent_id: c.parent_id,
    created_at: c.created_at,
    updated_at: c.updated_at,
    revision: c.revision,
    comment_type: c.type,
    reactions: c.reactions ?? [],
    attachments: c.attachments ?? [],
    resolved_at: c.resolved_at,
    resolved_by_type: c.resolved_by_type,
    resolved_by_id: c.resolved_by_id,
    source_task_id: c.source_task_id,
    supplements: c.supplements,
    supplement_task_id: c.supplement_task_id,
    supplement_status: c.supplement_status,
    supplement_failure_reason: c.supplement_failure_reason,
    supplement_delivered_at: c.supplement_delivered_at,
    deleted_at: c.deleted_at,
  };
}

/** Keep a requested historical thread available across bounded timeline refetches. */
export function mergeCommentThread(timeline: TimelineEntry[], comments: Comment[]): TimelineEntry[] {
  if (comments.length === 0) return timeline;
  const entries = new Map(comments.map((comment) => [comment.id, commentToTimelineEntry(comment)]));
  for (const entry of timeline) {
    const focused = entries.get(entry.id);
    entries.set(
      entry.id,
      focused?.revision !== undefined && entry.revision !== undefined && focused.revision > entry.revision
        ? { ...focused, actor_name: entry.actor_name, actor_avatar_url: entry.actor_avatar_url }
        : entry,
    );
  }
  return sortTimelineEntriesAsc([...entries.values()]);
}
