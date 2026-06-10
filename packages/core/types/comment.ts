// CEREBRO-PATCH(wakeup-comment-type): TECH-3298 — agent wakeups render as a
// small collapsible action note (type "wakeup") instead of a full comment.
export type CommentType = "comment" | "status_change" | "progress_update" | "system" | "wakeup";

// `system` is used by platform-generated rows (e.g. the parent-issue
// child-done notification, MUL-2538). System rows carry a zero UUID for
// author_id; render paths should branch on author_type rather than the UUID.
export type CommentAuthorType = "member" | "agent" | "system";

export interface Reaction {
  id: string;
  comment_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
}

export interface Comment {
  id: string;
  issue_id: string;
  author_type: CommentAuthorType;
  author_id: string;
  content: string;
  type: CommentType;
  parent_id: string | null;
  reactions: Reaction[];
  attachments: import("./attachment").Attachment[];
  created_at: string;
  updated_at: string;
  resolved_at: string | null;
  resolved_by_type: CommentAuthorType | null;
  resolved_by_id: string | null;
}

// CEREBRO-PATCH(comments-move-to-subissue-ui): JEH-1309 frontend response for moving a thread to a sub-issue.
export interface MoveCommentToSubIssueResponse {
  issue_id: string;
  identifier: string;
  number: number;
}

// CEREBRO-PATCH(comments-move-to-thread-ui): JEH-2488 response for moving picked comments to a new thread.
export interface MoveCommentsToThreadResponse {
  root_comment_id: string;
  issue_id: string;
  moved_count: number;
}

// CEREBRO-PATCH(issue-comment-cost-types): FIR-39 per-comment cost badge. Spend
// for one agent comment, pinned to the last comment its task produced so a
// run that posts progress + result does not double-count. Looked up by
// comment_id on the comment card / channel message footer; numbers sum to
// IssueUsageSummary (already shown in the issue sidebar).
export interface IssueCommentCost {
  task_id: string;
  comment_id: string;
  /** Single model used, or "" when the task spanned multiple models. */
  model: string;
  cost_cents: number;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
}

// CEREBRO-PATCH(issue-comment-cost-types): FIR-39 — GET /comment-costs response.
export interface IssueCommentCosts {
  costs: IssueCommentCost[];
}
