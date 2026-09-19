import type { IssueStatusCategory } from "./issue";

/**
 * A workspace's issue status catalog (MUL-6243).
 *
 * Seven concrete built-in statuses are grouped into four lifecycle categories.
 * Category is presentation and workflow phase; the server keeps the legacy
 * status behavior projection separate so collapsing `in_review` and `blocked`
 * into `started` does not change existing automation behavior.
 */

// IssueStatusCategory is defined in ./issue, next to IssueStatus, because the
// two only make sense read together. Re-exported here so catalog consumers can
// import both from one place. (MUL-6243, MUL-7240)
export type { IssueStatusCategory } from "./issue";

/** Visual geometry only; never use these values to infer workflow behavior. */
export const ISSUE_STATUS_ICONS = ["dotted", "circle", "half", "three_quarters", "check", "slash", "cross"] as const;
export type IssueStatusIcon = (typeof ISSUE_STATUS_ICONS)[number];

export interface IssueStatusEntry {
  id: string;
  workspace_id: string;
  /**
   * Stable machine handle, immutable after creation. This is the value stored
   * in `issue.status`, accepted by `multica issue status`, and referenced in
   * agent instructions — so it does NOT track renames of `name`.
   */
  key: string;
  /** Human-facing label. Editable for custom statuses; locked for built-ins. */
  name: string;
  description: string;
  category: IssueStatusCategory;
  /** "#rrggbb". */
  color: string;
  /** Empty/absent means category default. Unknown future values render a fallback. */
  icon?: string | null;
  /**
   * True for the 7 built-ins. They cannot be renamed, recolored, archived, or
   * have their category changed.
   */
  is_system: boolean;
  /** Ordering within the category. */
  position: number;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface ListIssueStatusesResponse {
  statuses: IssueStatusEntry[];
  /** The four lifecycle categories, in display order. */
  categories: IssueStatusCategory[];
  total: number;
}

export interface CreateIssueStatusRequest {
  /** Optional; derived from `name` when omitted. Immutable once created. */
  key?: string;
  name: string;
  description?: string;
  category: IssueStatusCategory;
  color: string;
  icon?: IssueStatusIcon | "";
}

/**
 * `key` and `category` are absent by design — both are immutable. Changing a
 * category would regroup every issue already on that status; changing a key
 * would strand them.
 */
export interface UpdateIssueStatusRequest {
  name?: string;
  description?: string;
  color?: string;
  /** Omit to preserve; empty string resets to the default. */
  icon?: IssueStatusIcon | "";
  position?: number;
}

export type IssueWorkflowPhase = IssueStatusCategory;

export interface IssueWorkflowDefinition {
  id: string;
  workspace_id: string;
  scope_type: "workspace" | "project" | (string & {});
  scope_id: string;
  name: string;
  revision: number;
  initial_status_id: string | null;
  created_at: string;
  updated_at: string;
}

export type IssueWorkflowExecutorTarget =
  | { type: "none"; id?: never }
  | { type: "agent" | "squad"; id: string };

/** Action to run when an issue enters a status. */
export interface IssueWorkflowEntryPolicy {
  executor: IssueWorkflowExecutorTarget;
  /** Prompt supplied to the executor when the issue enters this node. */
  instructions: string;
}

export interface IssueWorkflowStatusNode {
  id: string;
  workflow_id: string;
  legacy_status_key: string | null;
  /** Stable key used by workflow YAML/JSON definitions. */
  spec_key: string;
  name: string;
  description: string;
  color: string;
  icon?: string;
  position: number;
  phase: IssueWorkflowPhase | (string & {});
  outcome: "completed" | "cancelled" | null | (string & {});
  entry_policy: IssueWorkflowEntryPolicy;
  entry_policy_revision: number;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface IssueWorkflowResponse {
  plan?: { migration?: WorkflowMigrationPlan };
  dry_run?: boolean;
  workflow: IssueWorkflowDefinition;
  statuses: IssueWorkflowStatusNode[];
  mode: "default" | "custom" | (string & {});
}

export interface UpdateIssueWorkflowStatusRequest {
  expected_revision: number;
  name?: string;
  description?: string;
  color?: string;
  phase?: IssueWorkflowPhase;
  entry_policy?: IssueWorkflowEntryPolicy;
}

export interface ReorderIssueWorkflowStatusesRequest {
  expected_revision: number;
  status_ids: string[];
}

export interface IssueTransitionRecord {
  id: string;
  from_status_id: string | null;
  to_status_id: string;
  actor_type: string;
  actor_id: string | null;
  cause: string;
  issue_revision_before: number;
  issue_revision_after: number;
  created_at: string;
}

export type AutomationExecutionStatus =
  | "dormant"
  | "pending"
  | "queued"
  | "running"
  | "completed"
  | "failed"
  | "cancelled"
  | "superseded";

export interface AutomationExecution {
  id: string;
  issue_id: string;
  trigger_transition_id: string;
  workflow_id: string;
  workflow_revision: number;
  status_id: string;
  policy_revision: number;
  policy_snapshot: IssueWorkflowEntryPolicy;
  executor_type: "agent" | "squad" | null | (string & {});
  executor_id: string | null;
  status: AutomationExecutionStatus | (string & {});
  created_at: string;
  updated_at: string;
}

export interface TransitionIssueStatusNodeRequest {
  /** Reject if the displayed entry effects have changed since preview. */
  expected_workflow_revision?: number;
  workflow_status_id: string;
  expected_revision?: number;
  expected_transition_id?: string;
}

export interface TransitionIssueStatusNodeResponse {
  issue: import("./issue").Issue;
  /** Null when the issue was already on the requested node. */
  transition: IssueTransitionRecord | null;
  /** Policy snapshot created for this concrete entry. */
  execution: AutomationExecution | null;
  /** Present only when the entry policy configured an agent or squad. */
  task_id: string | null;
}

export interface TakeOverAutomationExecutionResponse {
  issue: import("./issue").Issue;
  execution: AutomationExecution;
}

export interface IssueWorkflowSpec {
  api_version: 1;
  name: string;
  initial_status: string;
  statuses: Array<{
    key: string;
    name: string;
    description: string;
    color: string;
    icon?: string;
    phase: IssueWorkflowPhase;
    entry_policy: IssueWorkflowEntryPolicy;
  }>;
}

export interface ApplyProjectWorkflowRequest {
  mode: "custom" | "default";
  spec?: IssueWorkflowSpec;
  expected_revision: number;
  allow_archive?: boolean;
  dry_run?: boolean;
  status_mapping?: Record<string, string>;
  confirm_migration?: boolean;
  migration_fingerprint?: string;
}

export interface WorkflowMigrationPlan {
  rows: Array<{ source_status_id: string; source_name: string; source_phase: string; count: number; required: boolean; target_key: string; phase_changed: boolean }>;
  issue_count: number;
  view_count: number;
  blocked_issue_ids: string[];
  fingerprint: string;
}
