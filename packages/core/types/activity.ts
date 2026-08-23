import type { CommentAuthorType, Reaction } from "./comment";
import type { Attachment } from "./attachment";

export interface AssigneeFrequencyEntry {
  assignee_type: string;
  assignee_id: string;
  frequency: number;
}

/**
 * Total wall-clock time an issue has spent on one status, summed across every
 * visit to it. Backs the "time in status" hover on the issue detail sidebar.
 */
export interface StatusDurationEntry {
  /** Raw status key. May name a status archived out of the workspace catalog. */
  status: string;
  seconds: number;
  /** The status the issue sits on now — its `seconds` is still growing. */
  current: boolean;
}

export interface StatusDurations {
  /** Ordered by first entry into each status, oldest first. */
  entries: StatusDurationEntry[];
  /** Server clock when the open segment was closed off, RFC3339. */
  computed_at: string;
  /** True when no transitions were ever logged, so this is a single bucket. */
  partial: boolean;
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
  revision?: number;
  comment_type?: string;
  /** Set only on comments a quick action produced (MUL-5465). Unforgeable. */
  quick_action_id?: string | null;
  reactions?: Reaction[];
  attachments?: Attachment[];
  resolved_at?: string | null;
  resolved_by_type?: CommentAuthorType | null;
  resolved_by_id?: string | null;
  source_task_id?: string | null;
  /** Set by frontend coalescing when consecutive identical activities are merged. */
  coalesced_count?: number;
}
