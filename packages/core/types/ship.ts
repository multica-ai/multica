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
  // ---- Phase 7a — release decoration ----
  /** Set when this PR is part of an active release. Drives the
   *  "🚂 in <release>" badge on the card. Optional + nullable so
   *  older backends without the JOIN simply omit it. */
  active_release?: { id: string; title: string; stage: string } | null;
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
  /** Phase 6 — which deploy adapter handles this env's webhooks +
   *  poll + rollback. Defaults to "github_actions" for envs created
   *  before Phase 6 landed (server-side migration). Treated as a
   *  free-form string per CLAUDE.md API Response Compatibility — UI
   *  switches on it MUST have a default branch so a server-side enum
   *  addition doesn't crash older Electron builds. */
  adapter_kind?: string;
  /** Per-env GitHub Actions workflow filename the auto-detect poller
   *  watches. Null when the env inherits the workspace-level setting
   *  (`workspace.ship_hub_deploy_workflow_<kind>`). Multi-project
   *  workspaces use this to point each project at its own repo's
   *  workflow file. */
  deploy_workflow_filename?: string | null;
  /** When true, the release stage transition into this env's deploy
   *  stage (promoting for production, in_staging for staging) fires
   *  workflow_dispatch on `deploy_workflow_filename` against the
   *  project's GitHub repo. Turns Promote into a real one-click
   *  production deploy — no more "awaiting deploy" forever. Requires
   *  the workspace's GitHub PAT plus this env's deploy_workflow_filename
   *  to actually fire; otherwise the dispatch is logged as skipped.
   *  Defaults to false; opt-in via the Configure deploy env dialog. */
  auto_deploy?: boolean;
}

/** Phase 6 — entry returned by GET /api/deploy/adapters. */
export interface DeployAdapter {
  kind: string;
  supports_poll: boolean;
  supports_rollback: boolean;
  webhook_url: string;
}

/** Phase 6 — body for PUT /api/deploy_environments/:id/adapter. */
export interface ConfigureDeployAdapterRequest {
  adapter_kind: string;
  /** Adapter-specific JSON config (Vercel: {team_id, project_id, token};
   *  Cloudflare: {account_id, project_name, api_token}; etc.). */
  config: Record<string, unknown>;
  /** Optional inbound-webhook signing secret. Empty / omitted = leave
   *  the previously stored secret alone. */
  webhook_secret?: string;
}

/** Phase 6 — response from PUT /api/deploy_environments/:id/adapter. */
export interface ConfigureDeployAdapterResponse {
  environment_id: string;
  adapter_kind: string;
  webhook_url: string;
  webhook_secret_set: boolean;
}

/** Phase 6 — response from POST /api/deploy_environments/:id/poll_now. */
export interface PollDeployEnvironmentResponse {
  current_sha?: string;
  current_deployed_at?: string;
  changed?: boolean;
}

/** Phase 6 — body for POST /api/deploy_environments/:id/rollback. */
export interface RollbackDeployRequest {
  target_sha: string;
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
  deploy_workflow_filename?: string | null;
  auto_deploy?: boolean;
}

export interface UpdateDeployEnvironmentRequest {
  name?: string | null;
  target_branch?: string | null;
  target_url?: string | null;
  auto_promote?: boolean;
  deploy_workflow_filename?: string | null;
  auto_deploy?: boolean;
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

export interface ListDeployAdaptersResponse {
  adapters: DeployAdapter[];
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
  | "close_as_stale"
  | "close_pr"
  | "submit_review";

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

/** Phase 6.5 — review payload returned by the submit_review chip. Mirrors
 *  the GitHub Reviews API response. SubmittedAt is a string because the
 *  client passes through the raw ISO timestamp; we don't bother converting
 *  to Date here since the chip toast renders the action right after a
 *  fresh submission. */
export interface ShipActionReview {
  id: number;
  html_url: string;
  /** GitHub returns "APPROVED" / "CHANGES_REQUESTED" / "COMMENTED" /
   *  "DISMISSED" on REST and lowercased equivalents on webhooks. We
   *  carry the literal string and let the UI degrade for unknown values
   *  per CLAUDE.md "Enum drift downgrades, not crashes". */
  state: string;
  body: string;
  user?: {
    login: string;
    avatar_url: string;
  };
  submitted_at?: string;
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
  /** Phase 6.5 — populated by submit_review. Optional so older Electron
   *  builds that don't know the field still parse the response cleanly. */
  review?: ShipActionReview | null;
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

/** Phase 6.5 — submit_review request body. The three events match
 *  GitHub's Reviews API exactly; the union is a UI hint only — the
 *  server still accepts an arbitrary string and rejects with a clean
 *  400 if it isn't one of these. body is required for COMMENT and
 *  REQUEST_CHANGES (the dialog disables the button until then). */
export interface SubmitPullRequestReviewRequest {
  event: "APPROVE" | "REQUEST_CHANGES" | "COMMENT";
  body?: string;
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

// --- PR detail drawer ------------------------------------------------------
//
// GET /api/pull_requests/{id}/details — bundled response so the drawer
// renders without an N+1 fetch on open. Every optional section is
// nullable so a freshly-opened PR with no enrichment renders gracefully.

export interface DrawerLinkedIssue {
  id: string;
  workspace_id: string;
  number: number;
  identifier: string;
  title: string;
  status: string;
  priority: string;
}

export interface DrawerAgentTaskRef {
  id: string;
  agent_id: string;
  agent_name: string;
  title: string;
  status: string;
}

export interface DrawerChannelRef {
  id: string;
  name: string;
  display_name: string;
}

export interface DrawerPullRequestRef {
  id: string;
  number: number;
  title: string;
  state: string;
  html_url: string;
}

export interface DrawerReview {
  id: string;
  reviewer_login: string;
  reviewer_avatar_url: string | null;
  /** "APPROVED" / "CHANGES_REQUESTED" / "COMMENTED" / "DISMISSED" /
   *  "PENDING" — loose-typed per the API drift contract. */
  state: string;
  body: string | null;
  submitted_at: string;
}

export interface DrawerCheck {
  id: string;
  name: string;
  /** "success" / "failure" / "neutral" / "cancelled" / "skipped" /
   *  "timed_out" / "action_required" / null when still running. */
  conclusion: string | null;
  /** "queued" / "in_progress" / "completed". */
  status: string;
  details_url: string | null;
  started_at: string | null;
  completed_at: string | null;
}

export interface DrawerCardAction {
  id: string;
  /** Mirrors ShipCardActionName but loose-typed for drift. */
  action: string;
  /** "succeeded" / "failed" / "in_progress". */
  result_status: string;
  actor_user_id: string | null;
  created_at: string;
  completed_at: string | null;
}

export interface PullRequestDetailsResponse {
  pull_request: PullRequest;
  linked_issue?: DrawerLinkedIssue | null;
  originating_agent_task?: DrawerAgentTaskRef | null;
  active_release?: { id: string; title: string; stage: string } | null;
  conversation_channel?: DrawerChannelRef | null;
  reviews: DrawerReview[];
  checks: DrawerCheck[];
  recent_actions: DrawerCardAction[];
  stack_parent?: DrawerPullRequestRef | null;
  stack_children: DrawerPullRequestRef[];
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

// --- Phase 7a: Releases -----------------------------------------------------

/** All possible release stages — keep loose-typed at the wire (the
 *  schema accepts any string) so a future server-side stage drop-in
 *  doesn't crash older Electron builds. The strict literal union below
 *  describes today's contract. */
export type ReleaseStage =
  | "assembling"
  | "merging"
  | "in_staging"
  | "verifying"
  | "promoting"
  | "in_production"
  | "done"
  | "rolled_back"
  | "cancelled";

/** Release row returned by GET /api/releases/{id} (top-level `release`
 *  field), plus the rail / project listings. PRs, events, channel,
 *  and issue ride alongside as separate fields on the detail
 *  response. */
export interface Release {
  id: string;
  workspace_id: string;
  project_id: string;
  title: string;
  description: string | null;
  stage: ReleaseStage | string;
  risk_level: RiskLevel | string;
  channel_id: string | null;
  issue_id: string | null;
  approver_id: string | null;
  second_approver_id: string | null;
  staging_deploy_id: string | null;
  production_deploy_id: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  merged_at: string | null;
  staged_at: string | null;
  promoted_at: string | null;
  done_at: string | null;
  rollback_reason: string | null;
  pr_count: number;
  /** Phase 7b — soft-state flag set when the merge train hits a
   *  failure mid-flight. The UI reads this AND `stage === "merging"`
   *  to render the paused banner + Resume / Skip / Abort buttons. */
  merge_paused: boolean;
  /** Phase 7b — workspace-default merge method ("merge" / "squash" /
   *  "rebase"). Stamped at start_merge time. */
  merge_method: string;
  // ---- Phase 7c — staging-stage signals ----
  /** GitHub Actions workflow run id for the most recent smoke test
   *  trigger. Empty / null when no smoke has been run. */
  smoke_run_id?: string | null;
  /** Deep link to the smoke run on GitHub. */
  smoke_run_url?: string | null;
  /** "" | "queued" | "in_progress" | "completed_success" |
   *  "completed_failure" | "skipped" | "manual_pass". Loose-typed
   *  per the API drift contract. */
  smoke_status?: string | null;
  smoke_completed_at?: string | null;
  /** When non-null, the release has been QA-verified by the user
   *  whose id is qa_verified_by. */
  qa_verified_at?: string | null;
  qa_verified_by?: string | null;
  /** SHA of the merge commit produced by the LAST PR in the train.
   *  Used by the deploy webhook handler to link a deploy back to the
   *  release. Surfaced in the linked-staging-deploy panel. */
  merged_main_sha?: string | null;
  // ---- Phase 7d — production-stage signals ----
  /** User who clicked Promote. */
  promoted_by?: string | null;
  /** SHA that landed in production. Usually equals merged_main_sha;
   *  diverges when a hotfix or revert sha lands in prod that's
   *  different from what was originally merged. */
  production_main_sha?: string | null;
  /** User who initiated the rollback. */
  rolled_back_by?: string | null;
  /** When the rollback completed (v1: same instant as
   *  rolled_back_by — the user-driven flow records the decision and
   *  the channel post lists the merged PRs to revert manually). */
  rolled_back_completed_at?: string | null;
}

/** Phase 7b — per-PR merge state values. Drives the pill rendering on
 *  the release detail page. Treated as `string` everywhere downstream
 *  per the API-compat contract; this type is exported so the few
 *  switch statements in the UI can narrow safely. */
export type ReleasePRMergeState =
  | "queued"
  | "merging"
  | "merged"
  | "failed"
  | "skipped";

/** Per-release PR row — extends the regular PullRequest shape with
 *  membership-table columns (position in the merge train, per-PR
 *  merge sha / error from Phase 7b). */
export interface ReleasePullRequest extends PullRequest {
  position: number;
  merged_sha: string | null;
  merged_at_release: string | null;
  merge_error: string | null;
  added_at: string;
  is_active: boolean;
  /** Phase 7b — per-PR merge state. queued / merging / merged /
   *  failed / skipped. Treated as `string` so a future server-side
   *  enum value renders a generic fallback rather than crashing. */
  merge_state: string;
}

/** Audit-log row in the release timeline. event_type is loose-typed
 *  per the API compat contract. */
export interface ReleaseEvent {
  id: string;
  release_id: string;
  event_type: string;
  actor_user_id: string | null;
  payload: unknown;
  created_at: string;
}

/** GET /api/releases/{id} response. */
export interface ReleaseDetailResponse {
  release: Release;
  pull_requests: ReleasePullRequest[];
  events: ReleaseEvent[];
  /** Auto-created discussion channel (when present). The shape
   *  matches ChannelResponse in core/types/channel.ts; treated as
   *  unknown here to avoid a circular import — consumers narrow at
   *  the call site. */
  channel?: { id: string; name: string; display_name?: string } | null;
  /** Auto-created tracking issue. Same opaque-typed treatment. */
  issue?: { id: string; identifier?: string; title?: string; status?: string } | null;
  /** Phase 7d follow-up — ship_release_signoff rows for the "two"
   *  approval rule. Empty for releases not using the rule. Older
   *  servers don't include this field (treat undefined === []). */
  signoffs?: import("./workspace").ReleaseSignoff[];
}

/** POST /api/projects/{id}/releases response. Same shape as the
 *  detail page, plus `warnings` for the soft-gate concerns the
 *  service layer surfaced. */
export interface CreateReleaseResponse {
  release: Release;
  channel?: { id: string; name: string } | null;
  issue?: { id: string; title: string } | null;
  warnings: string[];
}

/** GET /api/workspaces/{id}/releases/active and GET /api/projects/{id}/releases. */
export interface ListReleasesResponse {
  releases: Release[];
}

/** Body for POST /api/projects/{id}/releases. */
export interface CreateReleaseRequest {
  title: string;
  description?: string;
  pull_request_ids: string[];
  approver_id?: string | null;
  second_approver_id?: string | null;
}

/** Body for PATCH /api/releases/{id}. Pointer-style nullables — null
 *  on `approver_id` clears the field, undefined leaves it untouched. */
export interface UpdateReleaseRequest {
  title?: string;
  description?: string;
  approver_id?: string | null;
  second_approver_id?: string | null;
}

/** Body for POST /api/releases/{id}/pull_requests. */
export interface AddPullRequestToReleaseRequest {
  pull_request_id: string;
}

/** Body for POST /api/releases/{id}/cancel. */
export interface CancelReleaseRequest {
  reason?: string;
}

/** Body for POST /api/releases/{id}/start_merge. */
export interface StartMergeRequest {
  merge_method?: "merge" | "squash" | "rebase";
}

/** Body for POST /api/releases/{id}/resume_merge. */
export interface ResumeMergeRequest {
  /** PR ids to mark `skipped` before resuming the train. Use this
   *  when a PR is hopelessly conflicting and the user wants to
   *  abandon it rather than retrying. */
  skip_pr_ids?: string[];
}

/** Body for POST /api/releases/{id}/abort_merge. */
export interface AbortMergeRequest {
  reason?: string;
}

// --- Phase 7c — staging-stage requests --------------------------------------

/** Body for POST /api/releases/{id}/run_smoke_tests. Server reads
 *  the workspace's configured smoke workflow + the release's
 *  merged_main_sha; today the body is empty. Typed as a
 *  `Record<string, never>` rather than an empty `{}` so a future
 *  optional field landing here doesn't silently widen the call sites
 *  that pass an extra prop. */
export type RunReleaseSmokeTestsRequest = Record<string, never>;

/** Body for POST /api/releases/{id}/mark_smoke_pass. */
export interface MarkSmokePassRequest {
  /** Optional note recorded in the audit log + channel post. */
  note?: string;
}

/** Body for POST /api/releases/{id}/mark_verified. */
export interface MarkReleaseVerifiedRequest {
  /** Optional note recorded in the audit log + channel post. */
  note?: string;
}

/** Body for POST /api/releases/{id}/unverify. Reason is REQUIRED on
 *  the wire — the server returns 400 if empty. */
export interface UnverifyReleaseRequest {
  reason: string;
}

// --- Phase 7d — production-stage requests + health rollup -------------------

/** Body for POST /api/releases/{id}/promote. rollback_plan is captured
 *  at click-time and stored in the audit log; it's surfaced in the
 *  Promote dialog and required for high/critical risk releases (the
 *  client gates the submit button on it). */
export interface PromoteReleaseRequest {
  rollback_plan?: string;
}

/** Body for POST /api/releases/{id}/rollback. Reason REQUIRED. */
export interface RollbackReleaseRequest {
  reason: string;
}

/** Phase 7d — release health rollup. Each Δ is the delta vs baseline;
 *  null means "no signal" (the metric isn't being collected for this
 *  workspace, OR the release just promoted and the monitor hasn't
 *  written a row yet). The UI renders "—" for null. */
export interface ReleaseHealth {
  release_id: string;
  /** "ok" | "warning" | "alert" — drives the pill color and whether
   *  the rollback affordance is highlighted. Loose-typed per the API
   *  drift contract. */
  overall_status: string;
  snapshot_at: string;
  error_rate_delta: number | null;
  p99_latency_delta_ms: number | null;
  inbox_issues_since_promote: number;
  agent_failure_rate_delta: number | null;
}

/** Phase 7b — lightweight merge_state poll response. */
export interface MergeStateResponse {
  release_id: string;
  stage: string;
  merge_paused: boolean;
  merge_method: string;
  merged_count: number;
  total: number;
  pull_requests: Array<{
    pull_request_id: string;
    position: number;
    merge_state: string;
    merged_sha: string | null;
    merge_error: string | null;
  }>;
}

/** Phase 7a — minimal release reference attached to a PullRequest
 *  when it's part of an active release. Drives the per-card
 *  "🚂 in <release>" badge. */
export interface ActiveReleaseRef {
  id: string;
  title: string;
  stage: ReleaseStage | string;
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
