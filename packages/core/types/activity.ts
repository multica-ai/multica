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
  source_task_id?: string | null;
  /** Set by frontend coalescing when consecutive identical activities are merged. */
  coalesced_count?: number;
}

/**
 * One entry in an issue description's version history.
 *
 * A version is one editing SESSION, not one write: the description editor
 * autosaves on a debounce, and the server coalesces consecutive writes by the
 * same actor into a single row. `content` is only present on single-version
 * reads — the list endpoint omits snapshots so the panel stays cheap.
 */
export interface DescriptionVersion {
  id: string;
  issue_id: string;
  parent_version_id: string | null;
  actor_type: string | null;
  actor_id: string | null;
  source_task_id?: string | null;
  content?: string | null;
  added_lines: number;
  removed_lines: number;
  created_at: string;
  updated_at: string;
  /** The seeded V0: the description as it stood before the first tracked edit. */
  is_original: boolean;
}
