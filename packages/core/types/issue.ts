import type { Label } from "./label";
import type { IssuePropertyValues } from "./property";

/**
 * A status CATEGORY — the behavior equivalence class an issue's status belongs
 * to. There are exactly 7, and each is also the key of the built-in status that
 * defines it, which is why this stayed a closed union while `Issue.status`
 * became open. Board columns, filters and the presentation config are all keyed
 * off categories, so their shape is fixed no matter how many custom statuses a
 * workspace defines. (MUL-6243)
 */
export type IssueStatusCategory =
  | "backlog"
  | "todo"
  | "in_progress"
  | "in_review"
  | "done"
  | "blocked"
  | "cancelled";

/**
 * A status KEY as stored on the issue: one of the 7 built-ins, or a custom key
 * an admin defined for this workspace.
 *
 * OPEN by design. `(string & {})` keeps editor autocomplete for the 7 built-ins
 * while accepting any catalog key, which is what the server has always been
 * able to send. Anything that needs presentation (label, colour, board column)
 * must resolve the key to its CATEGORY first — `useIssueStatuses(wsId)` in a
 * component, `statusCategoryOfKey` in a pure path. (MUL-6243)
 */
export type IssueStatus = IssueStatusCategory | (string & {});

export type IssuePriority = "urgent" | "high" | "medium" | "low" | "none";

export type IssueAssigneeType = "member" | "agent" | "squad";

export interface IssueReaction {
  id: string;
  issue_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
  issue_revision?: number;
}

/**
 * Per-issue metadata is a flat KV map agents use to record pipeline state
 * (PR number, pipeline_status, waiting_on, ...). Values are primitives only —
 * string / number / bool — enforced by both the API and the DB. Always
 * present in responses (empty object when unset) so reads don't need a
 * nil guard on the parent field.
 */
export type IssueMetadataValue = string | number | boolean;
export type IssueMetadata = Record<string, IssueMetadataValue>;

export interface SourceContextAttachment {
  id: string;
  source_attachment_id?: string;
  owner_type: "issue" | "comment" | (string & {});
  owner_id: string;
  filename: string;
  content_type: string;
  size_bytes: number;
  created_at: string;
}

export interface SourceContextAuthor {
  type: "member" | "agent" | (string & {});
  id: string;
  name: string;
}

export interface SourceContextIssueSnapshot {
  id: string;
  identifier: string;
  number: number;
  title: string;
  description: string | null;
  created_at: string;
  updated_at: string;
  revision: number;
  attachments: SourceContextAttachment[];
}

export interface SourceContextCommentSnapshot {
  id: string;
  parent_id: string | null;
  type: string;
  content: string;
  author: SourceContextAuthor;
  created_at: string;
  updated_at: string;
  revision: number;
  attachments: SourceContextAttachment[];
}

export interface SourceContextSnapshot {
  /** Capture metadata is present on a persisted detail snapshot and omitted
   * from the pre-submit preview payload. */
  version?: number;
  captured_by_user_id?: string;
  captured_at?: string;
  source_issue: SourceContextIssueSnapshot;
  comment_thread: SourceContextCommentSnapshot[];
  anchor_comment_id: string;
}

export interface SourceContextLimitUsage {
  comment_count: number;
  text_bytes: number;
  attachment_count: number;
  attachment_bytes: number;
}

export interface SourceContextPreview extends SourceContextSnapshot {
  capture_token: string;
  limits: SourceContextLimitUsage;
}

export interface SourceContextAuthorState {
  type: string;
  id: string;
  captured_name: string;
  current_name?: string;
  state: string;
}

export interface SourceContextDescriptionAttachmentChange {
  kind: "added" | "removed" | "replaced" | (string & {});
  attachment_id: string;
  filename: string;
  previous_filename?: string;
}

export interface SourceContextChangeDetails {
  changed_comment_ids: string[];
  added_comments?: SourceContextCommentSnapshot[];
  removed_comment_ids?: string[];
  description_attachment_changes: SourceContextDescriptionAttachmentChange[];
}

export interface IssueSourceContext {
  id: string;
  version: number;
  usage: "read_only_historical_background" | (string & {});
  captured_at: string;
  display_state: "unchanged" | "changed" | "deleted" | "unavailable" | (string & {});
  source_issue_state: "unchanged" | "changed" | "deleted" | "unavailable" | (string & {});
  comment_thread_state: "unchanged" | "changed" | "unavailable" | (string & {});
  anchor_comment_state: "available" | "deleted" | "unavailable" | (string & {});
  can_open_current_source: boolean;
  change_reasons?: string[];
  change_details?: SourceContextChangeDetails;
  current_source?: { issue_id: string; identifier: string; anchor_comment_id: string };
  source_author_state?: SourceContextAuthorState[];
  snapshot: SourceContextSnapshot;
}

export interface Issue {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
  title: string;
  description: string | null;
  status: IssueStatus;
  /**
   * The category `status` belongs to, when the endpoint resolved it. Optional
   * because a BUILT-IN status is its own category and needs no resolution —
   * use `issueStatusCategory(issue)` rather than reading this directly.
   * (MUL-6243)
   */
  status_category?: IssueStatusCategory;
  /**
   * A CUSTOM status's display name, carried beside the key. Empty for the 7
   * built-ins, which are localized from the key — prefer `useStatusLabel`,
   * which handles both and stays correct when an admin renames a status.
   *
   * Optional only for compatibility with a server that predates it; a current
   * server always sends the field. (MUL-6749)
   */
  status_name?: string;
  priority: IssuePriority;
  assignee_type: IssueAssigneeType | null;
  assignee_id: string | null;
  creator_type: IssueAssigneeType;
  creator_id: string;
  parent_issue_id: string | null;
  project_id: string | null;
  position: number;
  // Ordered barrier group among sibling sub-issues (null = unstaged). The
  // parent assignee is notified/woken only when every sub-issue in a stage
  // finishes; see server/internal/handler/issue_child_done.go.
  stage: number | null;
  // Calendar days as date-only "YYYY-MM-DD" (no time, no timezone). Use the
  // helpers in @multica/core/issues/date to format/compare — never `new Date()`
  // + local formatting, which shifts the day by the viewer's offset.
  start_date: string | null;
  due_date: string | null;
  metadata: IssueMetadata;
  // Custom property values keyed by property definition id. Always present
  // in responses (empty object when unset), mirroring `metadata`.
  properties: IssuePropertyValues;
  reactions?: IssueReaction[];
  labels?: Label[];
  created_at: string;
  updated_at: string;
  /** Monotonic server revision; absent when connected to an older backend. */
  revision?: number;
  /**
   * Null until the server's historical activity backfill reaches this row.
   * This RFC3339 timestamp may include sub-second precision while legacy
   * created_at/updated_at values are second-precision; parse before comparing.
   */
  last_activity_at?: string | null;
  /** Present only on issue detail responses for issues created from a comment. */
  source_context?: IssueSourceContext;
}
