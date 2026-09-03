import type { IssueStatusCategory } from "./issue";

/**
 * A workspace's issue status catalog (MUL-6243).
 *
 * The 7 categories map one-to-one onto the 7 built-in statuses: a category's
 * value IS its canonical status key. A custom status names a category and
 * inherits that canonical status's platform behavior in full — a custom status
 * in the `todo` category starts an agent exactly like Todo does, one in the
 * `in_review` category finalizes an autopilot run exactly like In Review does.
 *
 * That is why there is no separate "behavior" field here: the category is the
 * behavior.
 */

// IssueStatusCategory is defined in ./issue, next to IssueStatus, because the
// two only make sense read together: a category is one of the 7 built-in keys,
// and a status key is a category or a workspace's custom key. Re-exported here
// so catalog consumers can import both from one place. (MUL-6243)
export type { IssueStatusCategory } from "./issue";

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
  /**
   * True for the 7 built-ins. They cannot be renamed, recolored, archived, or
   * have their category changed: each one is its category's canonical
   * definition, and the default workspace must look identical for every user
   * who never opens the status settings.
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
  /** The 7 categories, in display order. */
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
}

/**
 * `key` and `category` are absent by design — both are immutable. Changing a
 * category would silently rewrite the machine semantics of every issue already
 * on that status; changing a key would strand them.
 */
export interface UpdateIssueStatusRequest {
  name?: string;
  description?: string;
  color?: string;
  position?: number;
}

export type IssueLifecyclePhase =
  | "backlog"
  | "unstarted"
  | "started"
  | "completed"
  | "cancelled";

export interface IssueLifecycleDefinition {
  id: string;
  workspace_id: string;
  scope_type: "workspace" | "project" | (string & {});
  scope_id: string;
  name: string;
  revision: number;
  created_at: string;
  updated_at: string;
}

export type IssueLifecycleAssigneeTarget =
  | { type: "keep"; id?: never }
  | { type: "human" | "agent" | "squad"; id: string };

export type IssueLifecycleExecutorTarget =
  | { type: "none"; id?: never }
  | { type: "agent" | "squad"; id: string };

export interface IssueLifecycleEntryPolicy {
  assignee: IssueLifecycleAssigneeTarget;
  executor: IssueLifecycleExecutorTarget;
  /** Prompt supplied to the executor when the issue enters this node. */
  instructions: string;
  advance: "executor_may_transition" | "human_confirms";
}

export interface IssueLifecycleStatusNode {
  id: string;
  lifecycle_id: string;
  legacy_status_key: string | null;
  name: string;
  description: string;
  color: string;
  position: number;
  phase: IssueLifecyclePhase | (string & {});
  outcome: "completed" | "cancelled" | null | (string & {});
  entry_policy: IssueLifecycleEntryPolicy;
  entry_policy_revision: number;
  archived_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface IssueLifecycleResponse {
  lifecycle: IssueLifecycleDefinition;
  statuses: IssueLifecycleStatusNode[];
  mode: "default" | "custom" | (string & {});
}

export interface UpdateIssueLifecycleStatusRequest {
  expected_revision: number;
  name?: string;
  description?: string;
  color?: string;
  phase?: IssueLifecyclePhase;
  entry_policy?: IssueLifecycleEntryPolicy;
}

export interface ReorderIssueLifecycleStatusesRequest {
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

export interface TransitionIssueStatusNodeRequest {
  lifecycle_status_id: string;
  expected_revision?: number;
  expected_transition_id?: string;
}

export interface TransitionIssueStatusNodeResponse {
  issue: import("./issue").Issue;
  /** Null when the issue was already on the requested node. */
  transition: IssueTransitionRecord | null;
}
