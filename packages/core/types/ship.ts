// Ship Hub types — mirror the Go shapes in server/internal/handler/ship.go.
//
// Phase 1 surface only. Per CLAUDE.md "API Response Compatibility":
//  - String enums are LOOSELY typed at runtime (the zod schemas are
//    `z.string()`); the strict literal unions below describe today's
//    contract but a future server enum widening renders as a generic
//    fallback in the UI rather than crashing.
//  - Optional fields that the server may omit on older builds are
//    explicitly nullable so the consumer can default-render.

export type PullRequestState = "open" | "closed" | "merged";

export type DeployEnvironmentKind = "staging" | "production";

export type DeployStatus =
  | "pending"
  | "in_progress"
  | "succeeded"
  | "failed"
  | "rolled_back";

export interface PullRequestLabel {
  name: string;
  color: string;
}

/** Cached PR row backing the Ship Hub Kanban. */
export interface PullRequest {
  id: string;
  workspace_id: string;
  /** May be null when the PR was synced before being attached to a project. */
  project_id: string | null;
  repo_url: string;
  /** GitHub PR number — display as `#1234`. */
  number: number;
  title: string;
  state: PullRequestState;
  is_draft: boolean;
  author_login: string;
  author_avatar_url: string | null;
  base_ref: string;
  head_ref: string;
  head_sha: string;
  html_url: string;
  body: string | null;
  /** "pending" | "success" | "failure" | "" when no CI run reported. */
  ci_status: string;
  /** "" in Phase 1 — backend doesn't enrich review state yet. */
  review_decision: string;
  /** "MERGEABLE" | "CONFLICTING" | "UNKNOWN" — string-typed for drift. */
  mergeable: string;
  additions: number;
  deletions: number;
  changed_files: number;
  labels: PullRequestLabel[];
  pr_created_at: string;
  pr_updated_at: string;
  pr_merged_at: string | null;
  pr_closed_at: string | null;
  /** When this row was last refreshed from GitHub by the sync service. */
  fetched_at: string;
  // ---- Phase 4 — linkage spine ----
  /** Multica issue this PR was opened against, if any. */
  originating_issue_id?: string | null;
  /** agent_task_queue row that produced this PR's commits, if any. */
  originating_agent_task_id?: string | null;
  /** When true, merging the PR also moves the originating issue to done. */
  auto_close_issue_on_merge?: boolean;
  /** PR-scoped Multica channel for the discussion, if one was opened. */
  conversation_channel_id?: string | null;
  /** Open PR this PR was rebased onto (stack visualization). */
  stack_parent_pr_id?: string | null;
  /** Source classifier — see ClassifySource in
   *  server/internal/service/ship/linkage.go. Treat as a loose string;
   *  the UI maps `multica_agent | multica_human | external_tool |
   *  external_contributor` to icons and falls back generically. */
  source?: string;
  // ---- Phase 5 — risk profile ----
  /** "low" | "medium" | "high" | "critical". Loose-typed for drift —
   *  see RiskLevel for the canonical literal union. */
  risk_level?: string;
  /** Free-form list of trigger strings — render verbatim in the
   *  "Why this risk?" popover. Empty array when the classifier had
   *  nothing to say (medium / pre-classification). */
  risk_reasons?: string[];
  risk_classified_at?: string | null;
}

/** Per-project deploy target (one staging + one production by convention). */
export interface DeployEnvironment {
  id: string;
  workspace_id: string;
  project_id: string;
  kind: DeployEnvironmentKind;
  name: string;
  target_branch: string;
  target_url: string | null;
  current_sha: string | null;
  current_deployed_at: string | null;
  auto_promote: boolean;
  created_at: string;
  updated_at: string;
}

/** Single deploy attempt logged against an environment. */
export interface Deploy {
  id: string;
  workspace_id: string;
  environment_id: string;
  ref: string;
  sha: string;
  status: DeployStatus;
  /** UUID of the member who logged the deploy. Null for system-triggered rows. */
  triggered_by: string | null;
  triggered_at: string;
  started_at: string | null;
  completed_at: string | null;
  log_url: string | null;
  error_message: string | null;
  created_at: string;
}

/** Project entry as returned by GET /api/ship/projects — only carries the
 * fields the Ship landing page needs. The full Project type lives in
 * ./project.ts. */
export interface ShipProjectSummary {
  id: string;
  title: string;
  /** Mirrors `Project.icon`. Null when the project has no custom icon. */
  icon: string | null;
  open_pr_count: number;
  env_count: number;
}

// --- Request bodies ---------------------------------------------------------

export interface CreateDeployEnvironmentRequest {
  kind: DeployEnvironmentKind;
  name: string;
  target_branch?: string | null;
  target_url?: string | null;
  auto_promote?: boolean;
}

export interface UpdateDeployEnvironmentRequest {
  name?: string | null;
  target_branch?: string | null;
  target_url?: string | null;
  auto_promote?: boolean;
}

export interface LogDeployRequest {
  ref?: string;
  sha: string;
  status: DeployStatus;
  log_url?: string | null;
  error_message?: string | null;
}

// --- Response envelopes -----------------------------------------------------

export interface ListShipProjectsResponse {
  projects: ShipProjectSummary[];
}

export interface ListPullRequestsResponse {
  pull_requests: PullRequest[];
  total: number;
}

/** Result of POST /api/projects/:id/pull_requests/sync. */
export interface SyncPullRequestsResult {
  /** Repo URL the sync ran against (one repo per project today). */
  repo: string;
  /** Number of PR rows upserted in this run. */
  upserted: number;
  /** Per-PR or per-repo errors; empty on full success. */
  errors: string[];
}

export interface ListDeployEnvironmentsResponse {
  environments: DeployEnvironment[];
}

export interface ListDeploysResponse {
  deploys: Deploy[];
  total: number;
}

// --- Phase 3: card actions ("chips") -----------------------------------------

/** Canonical action names (must match the strings the backend dispatches on —
 * see server/internal/service/ship/actions.go). Treat the union as advisory:
 * the chip dispatcher routes unknown strings to a no-op via the fallback
 * branch, so a server-side rename never crashes the UI. */
export type ShipCardActionName =
  | "merge"
  | "rebase_on_main"
  | "comment"
  | "dismiss_review"
  | "diagnose_ci_failure"
  | "summarize_review_feedback"
  | "nudge_author"
  | "run_smoke_tests"
  | "close_as_stale";

export type ShipCardActionStatus = "succeeded" | "failed" | "in_progress";

/** GitHub comment shape echoed back by the comment + nudge + close_as_stale
 * chips. The frontend uses it for the optimistic "comment posted" toast and
 * (eventually) inline preview. Fields are loose-typed because the GitHub
 * client may not populate every nested user field on every error path. */
export interface ShipActionComment {
  id: number;
  html_url: string;
  body: string;
  user?: {
    login: string;
    avatar_url: string;
  };
}

/** Result of every POST /api/pull_requests/{id}/{action}. Fields are
 *  populated per-action; consult `status` to decide which branch to render. */
export interface ActionResult {
  status: ShipCardActionStatus | string;
  action_id: string;
  agent_task_id?: string | null;
  comment?: ShipActionComment | null;
  merge_sha?: string;
  error?: string;
}

/** Audit-trail row backing the "recent actions" footer on PR cards. Mirrors
 *  the `ship_card_action` table. */
export interface ShipCardAction {
  id: string;
  workspace_id: string;
  pull_request_id: string;
  actor_user_id: string | null;
  action: string;
  payload?: unknown;
  result_status: string;
  result_payload?: unknown;
  created_at: string;
  completed_at: string | null;
}

export interface ListShipCardActionsResponse {
  actions: ShipCardAction[];
}

// --- Phase 3 request bodies (one per chip endpoint) -------------------------

export interface MergePullRequestRequest {
  /** Optional — server defaults to "merge" when omitted. */
  method?: "merge" | "squash" | "rebase";
}

export interface CommentPullRequestRequest {
  body: string;
}

export interface DismissPullRequestReviewRequest {
  review_id: number;
  message: string;
}

export interface NudgePullRequestAuthorRequest {
  /** Optional — server uses a default polite-nudge string when omitted. */
  message?: string;
}

export interface RunSmokeTestsRequest {
  environment_id: string;
}

export interface ClosePullRequestAsStaleRequest {
  reason?: string;
}

// --- Phase 4: linkage / talk-to-agent / stacks ----------------------

export interface UpdatePullRequestRequest {
  originating_issue_id?: string | null;
  originating_agent_task_id?: string | null;
  auto_close_issue_on_merge?: boolean;
}

/** GET /api/pull_requests/{id}/linked_issues. */
export interface LinkedIssuesResponse {
  /** Linked Multica issue, when originating_issue_id is set. */
  issue: {
    id: string;
    identifier: string;
    title: string;
    status: string;
    workspace_id: string;
  } | null;
  /** Originating agent task, with the prompt summary so the chat-with-agent
   * chip can display it. */
  agent_task: {
    id: string;
    agent_id: string;
    agent_name: string;
    status: string;
    trigger_summary?: string | null;
    issue_id?: string | null;
  } | null;
}

/** POST /api/pull_requests/{id}/talk_to_agent body. */
export interface TalkToAgentRequest {
  /** Optional first message; the chat panel surfaces a textarea when omitted. */
  message?: string;
}

/** POST /api/pull_requests/{id}/talk_to_agent response. */
export interface TalkToAgentResponse {
  chat_session_id: string;
  agent_id: string;
}

/** GET /api/projects/{id}/pull_request_stacks response.
 *
 *  Each stack is a single root PR plus its (recursive) children. The
 *  card list renders root + immediate children inline; deeper nesting
 *  falls back to a "view stack" link.
 */
export interface PullRequestStackNode {
  pr: PullRequest;
  children: PullRequestStackNode[];
}

export interface ListPullRequestStacksResponse {
  stacks: PullRequestStackNode[];
}

// --- Phase 5: risk profile, pre-flight, time-machine, summary ----------

/** Risk tier as classified by server/internal/service/ship/risk.go.
 *  Per CLAUDE.md "API Response Compatibility" the wire is loose — we
 *  treat the literal union as advisory; the UI maps unknown values to
 *  the same neutral fallback as `medium`. */
export type RiskLevel = "low" | "medium" | "high" | "critical";

/** GET /api/ship_hub/summary response. Each field is a count surfaced
 *  in the multi-segment ambient sidebar widget. */
export interface ShipHubSummary {
  in_staging: number;
  awaiting_review: number;
  failing: number;
  in_production_24h: number;
  promotion_pending: number;
  open_pr_total: number;
}

/** Pre-flight checklist row for a (env, sha) pair. Mirrors the
 *  deploy_preflight Postgres table + the gate evaluation the server
 *  runs on every read. */
export interface DeployPreflight {
  id: string;
  workspace_id: string;
  environment_id: string;
  target_sha: string;
  migrations_ok: boolean;
  smoke_tests_ok: boolean;
  qa_verified_at: string | null;
  qa_verified_by: string | null;
  rollback_plan: string | null;
  approver_id: string | null;
  second_approver_id: string | null;
  approved_at: string | null;
  promoted_at: string | null;
  created_at: string;
  updated_at: string;
  /** Risk tier the server derived from the linked PR. */
  required_risk_level: RiskLevel | string;
  /** "ready" when every required check passes for the risk tier;
   *  "blocked" otherwise. Surfaced verbatim by the Promote button. */
  gate_status: "ready" | "blocked" | string;
  /** Human-readable reasons the gate is blocked. Empty when status
   *  is "ready". */
  gate_blocked_reasons: string[];
}

export interface CreatePreflightRequest {
  target_sha: string;
}

export interface UpdatePreflightRequest {
  migrations_ok?: boolean;
  smoke_tests_ok?: boolean;
  /** When set, server stamps qa_verified_at and qa_verified_by from
   *  the requesting user. */
  qa_verified?: boolean;
  rollback_plan?: string;
  /** First approver — server stamps approver_id with the requesting
   *  user. Set to false to clear. */
  approve?: boolean;
  /** Second approver — required for critical-risk preflights. */
  second_approve?: boolean;
}

export interface PromoteDeployPreflightResponse {
  preflight: DeployPreflight;
  deploy: Deploy;
}

/** GET /api/projects/{id}/ship_snapshot?at=<RFC3339>. */
export interface ShipSnapshotResponse {
  at: string;
  pull_requests: PullRequest[];
  environments: DeployEnvironment[];
  /** Map of environment_id -> SHA running on that env at the moment
   *  in time. */
  environment_shas_at_time: Record<string, string>;
}

/** Response of POST /api/workspaces/{id}/ship_hub/regenerate_webhook_secret.
 * Mirrors the personal-access-token create flow: `webhook_secret` is the
 * PLAINTEXT value, returned exactly once. The UI must capture it from this
 * response — subsequent reads of the workspace only echo
 * `ship_hub_webhook_secret_set: true`. */
export interface WebhookSecretResponse {
  webhook_secret: string;
  webhook_url: string;
  webhook_secret_set: boolean;
}
