import type { Label } from "./label";
import type { IssuePropertyValues } from "./property";

export type IssueStatus =
  | "backlog"
  | "todo"
  | "in_progress"
  | "in_review"
  | "done"
  | "blocked"
  | "cancelled";

// The 5 immutable status Categories — the only machine-readable status semantics
// in the custom-status model (MUL-4809). A custom or built-in status belongs to
// exactly one Category; automation branches on the Category, never the name.
export type StatusCategory =
  | "backlog"
  | "todo"
  | "in_progress"
  | "done"
  | "cancelled";

// StatusDetail is the resolved catalog view of an issue's status, attached to
// issue responses by list/detail endpoints (MUL-4809). Optional/nullable: absent
// or null when the issue has no status_id yet — callers fall back to the legacy
// `status` token. name/icon/color are human-facing; category is the machine key.
export interface StatusDetail {
  id: string;
  name: string;
  category: StatusCategory;
  icon: string;
  color: string;
}

export type IssuePriority = "urgent" | "high" | "medium" | "low" | "none";

export type IssueAssigneeType = "member" | "agent" | "squad";

export interface IssueReaction {
  id: string;
  issue_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
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

export interface Issue {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
  title: string;
  description: string | null;
  status: IssueStatus;
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
  // Authoritative custom-status catalog id + resolved detail (MUL-4809),
  // bulk-attached by list/detail endpoints like `labels`. Absent/null when the
  // issue has no status_id yet or on paths that don't hydrate them; the legacy
  // `status` field stays the fallback.
  status_id?: string | null;
  status_detail?: StatusDetail | null;
  created_at: string;
  updated_at: string;
}
