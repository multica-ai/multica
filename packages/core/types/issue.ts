import type { IssueLink } from "./link";

export type IssueStatus =
  | "backlog"
  | "todo"
  | "planning"
  | "ready_for_dev"
  | "in_progress"
  | "code_review"
  | "fixing"
  | "testing"
  | "coderabbit"
  | "resolving"
  | "staged"
  | "done"
  | "blocked"
  | "cancelled";

export type IssuePriority = "urgent" | "high" | "medium" | "low" | "none";

export type IssueAssigneeType = "member" | "agent";

/** Canonical label color palette. Keep in sync with:
 *  - server: migration 101 CHECK constraint + AllowedLabelColors map in label.go
 *  - client: LABEL_COLORS below
 */
export type LabelColor =
  | "slate"
  | "gray"
  | "red"
  | "orange"
  | "amber"
  | "green"
  | "teal"
  | "blue"
  | "indigo"
  | "purple"
  | "pink";

export const LABEL_COLORS: readonly LabelColor[] = [
  "slate",
  "gray",
  "red",
  "orange",
  "amber",
  "green",
  "teal",
  "blue",
  "indigo",
  "purple",
  "pink",
] as const;

export const MAX_LABEL_NAME_LEN = 32;
export const MAX_LABELS_PER_WORKSPACE = 100;
export const MAX_LABELS_PER_ISSUE = 8;

/** A workspace-scoped label that can be attached to issues. */
export interface IssueLabel {
  id: string;
  workspace_id: string;
  name: string;
  color: LabelColor;
  creator_type: IssueAssigneeType;
  creator_id: string;
  created_at: string;
  updated_at: string;
}

export interface IssueReaction {
  id: string;
  issue_id: string;
  actor_type: string;
  actor_id: string;
  emoji: string;
  created_at: string;
}

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
  estimate_minutes?: number | null;
  /** Detail responses include the parent roll-up; list responses omit it. */
  computed_estimate_minutes?: number | null;
  position: number;
  due_date: string | null;
  reactions?: IssueReaction[];
  /** Attached labels for this issue. Server always serialises this as an array
   *  (empty when no labels), never null. Ordered by label name ascending. */
  labels: IssueLabel[];
  /** Attached issue-to-issue links (both outgoing and incoming directions are
   *  embedded). Server always serialises this as an array (empty when no links).
   *  Optional in the type so legacy test fixtures (and pre-L-PR#2 cached issues)
   *  remain valid; consumers should default to []. */
  links?: IssueLink[];
  phase_state?: Record<string, unknown> | null;
  pr_url?: string | null;
  pr_number?: number | null;
  pr_repo?: string | null;
  created_at: string;
  updated_at: string;
}
