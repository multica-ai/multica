import type { CommentAuthorType, Reaction } from "./comment";
import type { Attachment } from "./attachment";

export interface AssigneeFrequencyEntry {
  assignee_type: string;
  assignee_id: string;
  frequency: number;
}

export interface TimelineEntry {
  type: "activity" | "comment";
  id: string;
  actor_type: string;
  actor_id: string;
  created_at: string;
  // Activity fields
  action?: string;
  details?: Record<string, unknown>;
  // Comment fields
  content?: string;
  parent_id?: string | null;
  updated_at?: string;
  comment_type?: string;
  reactions?: Reaction[];
  attachments?: Attachment[];
  resolved_at?: string | null;
  resolved_by_type?: CommentAuthorType | null;
  resolved_by_id?: string | null;
  /** Set by frontend coalescing when consecutive identical activities are merged. */
  coalesced_count?: number;
}

/**
 * Cursor-paginated timeline page. Entries are newest-first
 * (created_at DESC, id DESC). Cursors are opaque base64 strings — pass them
 * back unchanged via TimelinePageParam.
 */
export interface TimelinePage {
  entries: TimelineEntry[];
  next_cursor: string | null;
  prev_cursor: string | null;
  has_more_before: boolean;
  has_more_after: boolean;
  /** Set only in around-id mode; index of the anchor entry within `entries`. */
  target_index?: number;
}

export type TimelinePageParam =
  | { mode: "latest" }
  | { mode: "before"; cursor: string }
  | { mode: "after"; cursor: string }
  | { mode: "around"; id: string };
