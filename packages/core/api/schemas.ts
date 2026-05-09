import { z } from "zod";
import type { ListIssuesResponse, TimelinePage } from "../types";

// ---------------------------------------------------------------------------
// Schemas for the highest-risk API endpoints — those whose responses drive
// the issue detail page (timeline, comments, subscribers) and the issues
// list. These are the surfaces that white-screened in #2143 / #2147 / #2192.
//
// These schemas are intentionally LENIENT:
//   - String enums are stored as `z.string()` rather than `z.enum([...])`.
//     A new server-side enum value should render as a generic fallback in
//     the UI, never crash a `safeParse`.
//   - Optional fields are unioned with `null` and given fallbacks where
//     existing UI code already coerces them.
//   - Arrays default to `[]` so a missing `reactions` / `attachments` /
//     `entries` field doesn't take the page down.
//   - Every object schema ends with `.loose()` so unknown server-side
//     fields pass through unchanged. zod 4's `.object()` defaults to STRIP,
//     which would silently delete fields the schema didn't explicitly list
//     — fine while the TS type doesn't claim them, but the moment a future
//     PR adds a TS field without updating the schema, the cast `as T` lies
//     and the field shows up as `undefined` at runtime. `.loose()` removes
//     that synchronisation hazard.
//
// These schemas are deliberately not typed as `z.ZodType<TimelineEntry>` /
// `z.ZodType<Issue>` etc. — the strict TS types narrow string fields to
// literal unions, which would defeat the leniency above. `parseWithFallback`
// returns the parsed value cast to the caller-supplied `T`, so the strict
// type still flows out at the call site; the schema only guards shape.
// ---------------------------------------------------------------------------

const ReactionSchema = z.object({
  id: z.string(),
  comment_id: z.string(),
  actor_type: z.string(),
  actor_id: z.string(),
  emoji: z.string(),
  created_at: z.string(),
});

const AttachmentSchema = z.object({
  id: z.string(),
}).loose();

// All object schemas use `.loose()` so unknown server-side fields pass
// through unchanged. zod 4's `.object()` defaults to STRIP, which would
// silently drop new fields and surface as a "field neither showed up in
// the UI" mystery the next time the TS type adopted them but the schema
// wasn't updated in lock-step. `.loose()` removes that synchronisation
// hazard — the schema validates the shape it knows about and leaves the
// rest alone.
const TimelineEntrySchema = z.object({
  type: z.string(),
  id: z.string(),
  actor_type: z.string(),
  actor_id: z.string(),
  created_at: z.string(),
  action: z.string().optional(),
  details: z.record(z.string(), z.unknown()).optional(),
  content: z.string().optional(),
  parent_id: z.string().nullable().optional(),
  updated_at: z.string().optional(),
  comment_type: z.string().optional(),
  reactions: z.array(ReactionSchema).optional(),
  attachments: z.array(AttachmentSchema).optional(),
  coalesced_count: z.number().optional(),
}).loose();

export const TimelinePageSchema = z.object({
  entries: z.array(TimelineEntrySchema).default([]),
  next_cursor: z.string().nullable().default(null),
  prev_cursor: z.string().nullable().default(null),
  has_more_before: z.boolean().default(false),
  has_more_after: z.boolean().default(false),
  target_index: z.number().optional(),
}).loose();

export const EMPTY_TIMELINE_PAGE: TimelinePage = {
  entries: [],
  next_cursor: null,
  prev_cursor: null,
  has_more_before: false,
  has_more_after: false,
};

export const CommentSchema = z.object({
  id: z.string(),
  issue_id: z.string(),
  author_type: z.string(),
  author_id: z.string(),
  content: z.string(),
  type: z.string(),
  parent_id: z.string().nullable(),
  reactions: z.array(ReactionSchema).default([]),
  attachments: z.array(AttachmentSchema).default([]),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const CommentsListSchema = z.array(CommentSchema);

const IssueSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  number: z.number(),
  identifier: z.string(),
  title: z.string(),
  description: z.string().nullable(),
  status: z.string(),
  priority: z.string(),
  assignee_type: z.string().nullable(),
  assignee_id: z.string().nullable(),
  creator_type: z.string(),
  creator_id: z.string(),
  parent_issue_id: z.string().nullable(),
  project_id: z.string().nullable(),
  position: z.number(),
  due_date: z.string().nullable(),
  reactions: z.array(z.unknown()).optional(),
  labels: z.array(z.unknown()).optional(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const ListIssuesResponseSchema = z.object({
  issues: z.array(IssueSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_ISSUES_RESPONSE: ListIssuesResponse = {
  issues: [],
  total: 0,
};

const SubscriberSchema = z.object({
  issue_id: z.string(),
  user_type: z.string(),
  user_id: z.string(),
  reason: z.string(),
  created_at: z.string(),
}).loose();

export const SubscribersListSchema = z.array(SubscriberSchema);

export const ChildIssuesResponseSchema = z.object({
  issues: z.array(IssueSchema).default([]),
}).loose();

// ---------------------------------------------------------------------------
// Ship Hub schemas. The Phase 1 contract is small and stable, but per CLAUDE.md
// we go through parseWithFallback anyway: the desktop app sitting on a user's
// laptop is older than any backend it talks to, and an unexpected null in
// `pull_requests` (or a new enum value in `state`) must downgrade gracefully
// instead of white-screening the Kanban.
// ---------------------------------------------------------------------------

const PullRequestLabelSchema = z.object({
  name: z.string().default(""),
  color: z.string().default(""),
}).loose();

const PullRequestSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  project_id: z.string().nullable().default(null),
  repo_url: z.string().default(""),
  // Backend serializes the GH PR number as `number` (Go int32). The frontend
  // type field is named `pr_number` historically, so we accept both keys
  // and surface a single canonical value via `transform`.
  number: z.number().default(0),
  title: z.string().default(""),
  state: z.string().default("open"),
  is_draft: z.boolean().default(false),
  author_login: z.string().default(""),
  author_avatar_url: z.string().nullable().default(null),
  base_ref: z.string().default(""),
  head_ref: z.string().default(""),
  head_sha: z.string().default(""),
  html_url: z.string().default(""),
  body: z.string().nullable().default(null),
  ci_status: z.string().nullable().default(""),
  review_decision: z.string().nullable().default(""),
  mergeable: z.string().nullable().default(""),
  additions: z.number().default(0),
  deletions: z.number().default(0),
  changed_files: z.number().default(0),
  labels: z.array(PullRequestLabelSchema).default([]),
  pr_created_at: z.string().default(""),
  pr_updated_at: z.string().default(""),
  pr_merged_at: z.string().nullable().default(null),
  pr_closed_at: z.string().nullable().default(null),
  fetched_at: z.string().default(""),
  // Phase 4 — linkage spine. Older backends omit these fields entirely;
  // we mark them optional + nullable so a missing key is fine.
  originating_issue_id: z.string().nullable().optional(),
  originating_agent_task_id: z.string().nullable().optional(),
  auto_close_issue_on_merge: z.boolean().optional(),
  conversation_channel_id: z.string().nullable().optional(),
  stack_parent_pr_id: z.string().nullable().optional(),
  source: z.string().optional(),
  // Phase 5 — risk profile. Older backends omit these; we accept
  // missing keys without complaint per the API compat contract.
  risk_level: z.string().optional(),
  risk_reasons: z.array(z.string()).optional(),
  risk_classified_at: z.string().nullable().optional(),
  // Phase 7a — release decoration. Older backends without the
  // ListActiveReleasesForPullRequests JOIN simply omit this field.
  active_release: z
    .object({
      id: z.string().default(""),
      title: z.string().default(""),
      stage: z.string().default(""),
    })
    .loose()
    .nullable()
    .optional(),
}).loose();

export const ListPullRequestsResponseSchema = z.object({
  pull_requests: z.array(PullRequestSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_PULL_REQUESTS_RESPONSE = {
  pull_requests: [],
  total: 0,
};

export const DeployEnvironmentSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  project_id: z.string(),
  kind: z.string().default("staging"),
  name: z.string().default(""),
  target_branch: z.string().default("main"),
  target_url: z.string().nullable().default(null),
  current_sha: z.string().nullable().default(null),
  current_deployed_at: z.string().nullable().default(null),
  auto_promote: z.boolean().default(false),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  // Phase 6 — adapter_kind defaults to "github_actions" for older
  // backends that don't supply it. Treated as a free-form string per
  // CLAUDE.md API Response Compatibility (don't pin to an enum that
  // forces a TS-side migration whenever a server adapter ships).
  adapter_kind: z.string().optional().default("github_actions"),
}).loose();

export const ListDeployEnvironmentsResponseSchema = z.object({
  environments: z.array(DeployEnvironmentSchema).default([]),
}).loose();

export const EMPTY_LIST_DEPLOY_ENVIRONMENTS_RESPONSE = {
  environments: [],
};

// Phase 6 — adapters listing.
export const DeployAdapterSchema = z.object({
  kind: z.string().default(""),
  supports_poll: z.boolean().default(false),
  supports_rollback: z.boolean().default(false),
  webhook_url: z.string().default(""),
}).loose();

export const ListDeployAdaptersResponseSchema = z.object({
  adapters: z.array(DeployAdapterSchema).default([]),
}).loose();

export const EMPTY_LIST_DEPLOY_ADAPTERS_RESPONSE = {
  adapters: [],
};

export const ConfigureDeployAdapterResponseSchema = z.object({
  environment_id: z.string().default(""),
  adapter_kind: z.string().default(""),
  webhook_url: z.string().default(""),
  webhook_secret_set: z.boolean().default(false),
}).loose();

export const EMPTY_CONFIGURE_DEPLOY_ADAPTER_RESPONSE = {
  environment_id: "",
  adapter_kind: "",
  webhook_url: "",
  webhook_secret_set: false,
};

export const PollDeployEnvironmentResponseSchema = z.object({
  current_sha: z.string().optional(),
  current_deployed_at: z.string().optional(),
  changed: z.boolean().optional(),
}).loose();

export const EMPTY_POLL_DEPLOY_ENVIRONMENT_RESPONSE = {};

export const DeploySchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  environment_id: z.string(),
  ref: z.string().default(""),
  sha: z.string().default(""),
  status: z.string().default("pending"),
  triggered_by: z.string().nullable().default(null),
  triggered_at: z.string().default(""),
  started_at: z.string().nullable().default(null),
  completed_at: z.string().nullable().default(null),
  log_url: z.string().nullable().default(null),
  error_message: z.string().nullable().default(null),
  created_at: z.string().default(""),
}).loose();

export const ListDeploysResponseSchema = z.object({
  deploys: z.array(DeploySchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_DEPLOYS_RESPONSE = {
  deploys: [],
  total: 0,
};

const ShipProjectSummarySchema = z.object({
  id: z.string(),
  title: z.string().default(""),
  icon: z.string().nullable().default(null),
  open_pr_count: z.number().default(0),
  env_count: z.number().default(0),
}).loose();

export const ListShipProjectsResponseSchema = z.object({
  projects: z.array(ShipProjectSummarySchema).default([]),
}).loose();

export const EMPTY_LIST_SHIP_PROJECTS_RESPONSE = {
  projects: [],
};

// Phase 3 — POST /api/pull_requests/{id}/{action}.
//
// Every chip endpoint returns the same shape: a status discriminator plus
// optional fields populated per-action (merge_sha for the merge chip,
// agent_task_id for the async chips, comment for any chip that posts a
// comment, error for the failed branch). We keep `status` as `z.string()`
// rather than a strict union so a future server-side status (e.g. "queued")
// renders as a generic in-flight state instead of crashing the chip.
const ShipActionCommentSchema = z.object({
  id: z.number().default(0),
  html_url: z.string().default(""),
  body: z.string().default(""),
  user: z
    .object({
      login: z.string().default(""),
      avatar_url: z.string().default(""),
    })
    .loose()
    .optional(),
}).loose();

// Phase 6.5 — submit_review payload. Optional on every chip's
// ActionResult so older Electron builds parse cleanly when the field
// arrives, and so chip handlers that don't populate review still
// validate. Lenient on every nested string for the same reason.
const ShipActionReviewSchema = z.object({
  id: z.number().default(0),
  html_url: z.string().default(""),
  state: z.string().default(""),
  body: z.string().default(""),
  user: z
    .object({
      login: z.string().default(""),
      avatar_url: z.string().default(""),
    })
    .loose()
    .optional(),
  submitted_at: z.string().optional(),
}).loose();

export const ActionResultSchema = z.object({
  status: z.string().default("failed"),
  action_id: z.string().default(""),
  agent_task_id: z.string().nullable().optional(),
  comment: ShipActionCommentSchema.nullable().optional(),
  merge_sha: z.string().optional(),
  error: z.string().optional(),
  review: ShipActionReviewSchema.nullable().optional(),
}).loose();

// Fallback used when an ActionResult fails schema validation. The chip code
// checks `status === "succeeded" | "in_progress"` and falls through to the
// failure toast otherwise — defaulting status to "failed" and shipping a
// generic error string keeps the UX coherent rather than swallowing the
// outcome silently.
export const EMPTY_ACTION_RESULT = {
  status: "failed",
  action_id: "",
  error: "Malformed response",
};

// Phase 3 audit-trail row. Mirrors `db.ShipCardAction` from the Go side. The
// row is workspace-scoped and carries a result_status that mirrors
// ActionResult.status. We keep payload/result_payload as `unknown` here
// because they're opaque JSON blobs — the audit footer only needs the
// action name + actor + timestamp to render its row.
const ShipCardActionSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  pull_request_id: z.string().default(""),
  actor_user_id: z.string().nullable().default(null),
  action: z.string().default(""),
  payload: z.unknown().nullable().optional(),
  result_status: z.string().default(""),
  result_payload: z.unknown().nullable().optional(),
  created_at: z.string().default(""),
  completed_at: z.string().nullable().default(null),
}).loose();

export const ListShipCardActionsResponseSchema = z.object({
  actions: z.array(ShipCardActionSchema).default([]),
}).loose();

export const EMPTY_LIST_SHIP_CARD_ACTIONS_RESPONSE = {
  actions: [],
};

// Phase 2 — POST /api/workspaces/{id}/ship_hub/regenerate_webhook_secret.
// The plaintext `webhook_secret` is returned exactly once, mirroring the
// PAT-create flow. We still parse with a lenient schema so a corrupted
// response shape (missing field, wrong type) downgrades to a usable empty
// state in the UI rather than throwing — the caller surfaces an error toast
// instead. The fallback intentionally has an empty webhook_secret so the
// UI can detect the failure ("show modal only if secret is non-empty").
export const WebhookSecretResponseSchema = z.object({
  webhook_secret: z.string().default(""),
  webhook_url: z.string().default(""),
  webhook_secret_set: z.boolean().default(false),
}).loose();

export const EMPTY_WEBHOOK_SECRET_RESPONSE = {
  webhook_secret: "",
  webhook_url: "",
  webhook_secret_set: false,
};

// Phase 4 — linked_issues / stacks. Both endpoints land schema-validated
// on the client so an older Electron build that calls a Phase-4 server
// gracefully degrades when a field flips shape mid-flight.

const LinkedIssueSchema = z.object({
  id: z.string().default(""),
  identifier: z.string().default(""),
  title: z.string().default(""),
  status: z.string().default(""),
  workspace_id: z.string().default(""),
}).loose();

const LinkedAgentTaskSchema = z.object({
  id: z.string().default(""),
  agent_id: z.string().default(""),
  agent_name: z.string().default(""),
  status: z.string().default(""),
  trigger_summary: z.string().nullable().optional(),
  issue_id: z.string().nullable().optional(),
}).loose();

export const LinkedIssuesResponseSchema = z.object({
  issue: LinkedIssueSchema.nullable().default(null),
  agent_task: LinkedAgentTaskSchema.nullable().default(null),
}).loose();

export const EMPTY_LINKED_ISSUES_RESPONSE = {
  issue: null,
  agent_task: null,
};

// PullRequestStackNode is recursive: a node carries a `pr` plus a list
// of children of the same shape. zod 4 expresses this as a `lazy` type.
type PullRequestStackNodeShape = {
  pr: unknown;
  children: PullRequestStackNodeShape[];
};
export const PullRequestStackNodeSchema: z.ZodType<PullRequestStackNodeShape> =
  z.lazy(() =>
    z.object({
      pr: PullRequestSchema,
      children: z.array(PullRequestStackNodeSchema).default([]),
    }).loose(),
  );

export const ListPullRequestStacksResponseSchema = z.object({
  stacks: z.array(PullRequestStackNodeSchema).default([]),
}).loose();

export const EMPTY_LIST_PULL_REQUEST_STACKS_RESPONSE = {
  stacks: [],
};

export const TalkToAgentResponseSchema = z.object({
  chat_session_id: z.string().default(""),
  agent_id: z.string().default(""),
}).loose();

export const EMPTY_TALK_TO_AGENT_RESPONSE = {
  chat_session_id: "",
  agent_id: "",
};

// Phase 5 — Ship Hub summary (sidebar widget).
export const ShipHubSummarySchema = z.object({
  in_staging: z.number().default(0),
  awaiting_review: z.number().default(0),
  failing: z.number().default(0),
  in_production_24h: z.number().default(0),
  promotion_pending: z.number().default(0),
  open_pr_total: z.number().default(0),
}).loose();

export const EMPTY_SHIP_HUB_SUMMARY = {
  in_staging: 0,
  awaiting_review: 0,
  failing: 0,
  in_production_24h: 0,
  promotion_pending: 0,
  open_pr_total: 0,
};

// Phase 5 — pre-flight checklist row.
export const DeployPreflightSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  environment_id: z.string().default(""),
  target_sha: z.string().default(""),
  migrations_ok: z.boolean().default(false),
  smoke_tests_ok: z.boolean().default(false),
  qa_verified_at: z.string().nullable().default(null),
  qa_verified_by: z.string().nullable().default(null),
  rollback_plan: z.string().nullable().default(null),
  approver_id: z.string().nullable().default(null),
  second_approver_id: z.string().nullable().default(null),
  approved_at: z.string().nullable().default(null),
  promoted_at: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  required_risk_level: z.string().default("medium"),
  gate_status: z.string().default("blocked"),
  gate_blocked_reasons: z.array(z.string()).default([]),
}).loose();

export const PromoteDeployPreflightResponseSchema = z.object({
  preflight: DeployPreflightSchema,
  // Existing DeploySchema is hoisted above so we reuse it. The
  // promote endpoint always returns one — but the loose() wrapper
  // means a stray missing field still parses.
  deploy: DeploySchema,
}).loose();

// Phase 5 — time-machine snapshot.
export const ShipSnapshotResponseSchema = z.object({
  at: z.string().default(""),
  pull_requests: z.array(PullRequestSchema).default([]),
  environments: z.array(DeployEnvironmentSchema).default([]),
  environment_shas_at_time: z.record(z.string(), z.string()).default({}),
}).loose();

export const EMPTY_SHIP_SNAPSHOT_RESPONSE = {
  at: "",
  pull_requests: [],
  environments: [],
  environment_shas_at_time: {},
};

// Phase 7a — Release schemas. Every endpoint that returns a release
// runs through one of these so a server-side schema drift (a new
// stage value, a new optional field) downgrades to a usable shape
// rather than throwing into the UI. Stage / risk_level stay as
// `z.string()` per the API-compat contract.
export const ReleaseSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  project_id: z.string().default(""),
  title: z.string().default(""),
  description: z.string().nullable().default(null),
  stage: z.string().default("assembling"),
  risk_level: z.string().default("medium"),
  channel_id: z.string().nullable().default(null),
  issue_id: z.string().nullable().default(null),
  approver_id: z.string().nullable().default(null),
  second_approver_id: z.string().nullable().default(null),
  staging_deploy_id: z.string().nullable().default(null),
  production_deploy_id: z.string().nullable().default(null),
  created_by: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  merged_at: z.string().nullable().default(null),
  staged_at: z.string().nullable().default(null),
  promoted_at: z.string().nullable().default(null),
  done_at: z.string().nullable().default(null),
  rollback_reason: z.string().nullable().default(null),
  pr_count: z.number().default(0),
}).loose();

const ReleasePullRequestSchema = PullRequestSchema.extend({
  position: z.number().default(0),
  merged_sha: z.string().nullable().default(null),
  merged_at_release: z.string().nullable().default(null),
  merge_error: z.string().nullable().default(null),
  added_at: z.string().default(""),
  is_active: z.boolean().default(true),
}).loose();

const ReleaseEventSchema = z.object({
  id: z.string(),
  release_id: z.string().default(""),
  event_type: z.string().default(""),
  actor_user_id: z.string().nullable().default(null),
  payload: z.unknown().nullable().optional(),
  created_at: z.string().default(""),
}).loose();

export const ListReleasesResponseSchema = z.object({
  releases: z.array(ReleaseSchema).default([]),
}).loose();

export const EMPTY_LIST_RELEASES_RESPONSE = {
  releases: [],
};

const ReleaseChannelRefSchema = z.object({
  id: z.string().default(""),
  name: z.string().default(""),
  display_name: z.string().optional(),
}).loose();

const ReleaseIssueRefSchema = z.object({
  id: z.string().default(""),
  identifier: z.string().optional(),
  title: z.string().optional(),
  status: z.string().optional(),
}).loose();

export const ReleaseDetailResponseSchema = z.object({
  release: ReleaseSchema,
  pull_requests: z.array(ReleasePullRequestSchema).default([]),
  events: z.array(ReleaseEventSchema).default([]),
  channel: ReleaseChannelRefSchema.nullable().optional(),
  issue: ReleaseIssueRefSchema.nullable().optional(),
}).loose();

// EMPTY_RELEASE_DETAIL is rendered when a malformed detail response
// arrives — picks an empty release row with stage="assembling" so
// the UI's switch statements have a defined branch.
export const EMPTY_RELEASE_DETAIL = {
  release: {
    id: "",
    workspace_id: "",
    project_id: "",
    title: "",
    description: null,
    stage: "assembling",
    risk_level: "medium",
    channel_id: null,
    issue_id: null,
    approver_id: null,
    second_approver_id: null,
    staging_deploy_id: null,
    production_deploy_id: null,
    created_by: null,
    created_at: "",
    updated_at: "",
    merged_at: null,
    staged_at: null,
    promoted_at: null,
    done_at: null,
    rollback_reason: null,
    pr_count: 0,
  },
  pull_requests: [],
  events: [],
};

export const CreateReleaseResponseSchema = z.object({
  release: ReleaseSchema,
  channel: ReleaseChannelRefSchema.nullable().optional(),
  issue: ReleaseIssueRefSchema.nullable().optional(),
  warnings: z.array(z.string()).default([]),
}).loose();

export const EMPTY_CREATE_RELEASE_RESPONSE = {
  release: EMPTY_RELEASE_DETAIL.release,
  warnings: [],
};
