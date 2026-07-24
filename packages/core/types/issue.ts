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

// The semantic color tokens a status may carry. Mirrors the server allowlist
// (validIssueStatusColors) so the settings picker can only offer values the API
// accepts, and so every surface can map a color to theme classes.
export const STATUS_COLORS = [
  "muted-foreground",
  "warning",
  "success",
  "info",
  "destructive",
] as const;
export type StatusColor = (typeof STATUS_COLORS)[number];

// The icon shapes a status may carry — the built-in status glyphs. Mirrors the
// server allowlist (validIssueStatusIcons). A custom status reuses whichever
// shape best fits its Category; icon is human-facing only.
export const STATUS_ICONS = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "blocked",
  "done",
  "cancelled",
] as const;
export type StatusIconKey = (typeof STATUS_ICONS)[number];

// IssueStatusDefinition is a catalog entry: the workspace-configurable
// definition behind a status (MUL-4809 §5). `category` and `system_key` are
// immutable after create; the 7 built-ins (is_system) can be renamed/recolored
// but never archived.
export interface IssueStatusDefinition {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  icon: string;
  color: string;
  category: StatusCategory;
  system_key: string | null;
  is_system: boolean;
  is_default: boolean;
  position: number;
  archived: boolean;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
}

// IssueStatusCatalog is the workspace's status catalog plus the alias table.
// `category_defaults` maps each Category to its current default status id;
// `aliases` maps every alias token (5 Category + 2 legacy) to the status id it
// resolves to today, so a rename never leaves a caller guessing (§3.2).
export interface IssueStatusCatalog {
  statuses: IssueStatusDefinition[];
  category_defaults: Record<string, string>;
  aliases: Record<string, string>;
  total: number;
}

export interface CreateIssueStatusRequest {
  name: string;
  /** Immutable after create — pick a new status instead of moving Category. */
  category: StatusCategory;
  description?: string;
  icon: string;
  color: string;
  is_default?: boolean;
}

/**
 * Only the mutable fields. `category` / `system_key` are deliberately absent:
 * the API rejects them with 400 immutable_field rather than ignoring them.
 */
export interface UpdateIssueStatusRequest {
  name?: string;
  description?: string;
  icon?: string;
  color?: string;
  position?: number;
  is_default?: boolean;
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
