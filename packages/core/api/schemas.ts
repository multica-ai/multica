import { z } from "zod";
import type {
  AgentBuilderRuntimeSwitch,
  AgentBuilderSession,
  AgentBuilderSessionSummary,
  Attachment,
  AutopilotRun,
  BillingBalance,
  BillingBatchesPage,
  BillingCheckoutSessionStatus,
  BillingPriceTier,
  BillingTopupsPage,
  BillingTransactionsPage,
  CancelTaskResponse,
  ChatMessage,
  ChatDraftRestoresResponse,
  ChatPendingTask,
  PrioritizeQueuedChatTaskResponse,
  SendChatMessageResponse,
  StartMikaOnboardingResponse,
  Comment,
  CreateBillingCheckoutSessionResponse,
  CreateBillingPortalSessionResponse,
  WorkspaceSubscriptionEntitlements,
  WorkspaceSubscriptionSummary,
  WorkspaceSubscriptionPrice,
  WorkspaceSubscriptionPrices,
  CreateWorkspaceSubscriptionCheckoutResponse,
  WorkspaceSubscriptionSeatReconcileResult,
  CreateWorkspaceSubscriptionPortalResponse,
  CronPreviewResponse,
  DingTalkGroupRoute,
  DingTalkInstallation,
  ListDingTalkGroupRoutesResponse,
  ListDingTalkInstallationsResponse,
  RedeemDingTalkBindingTokenResponse,
  WecomInstallation,
  ListWecomInstallationsResponse,
  RedeemWecomBindingTokenResponse,
  GroupedIssuesResponse,
  GitHubConnectResponse,
  GitHubPullRequest,
  InboxItem,
  InboxWorkspaceUnread,
  Label,
  IssueProperty,
  ListPropertiesResponse,
  QuickAction,
  ListQuickActionsResponse,
  IssuePropertiesResponse,
  IssueTableGroupDescriptor,
  IssueTableFacetsResponse,
  IssueTableGroupsResponse,
  IssueTableRowsResponse,
  ListIssuesResponse,
  ListGitHubInstallationsResponse,
  ListGitHubRepositoriesResponse,
  ListLabelsResponse,
  ListWebhookDeliveriesResponse,
  NotificationPreferenceResponse,
  PluginCatalogResponse,
  PluginInstallation,
  PluginInstallationListResponse,
  ResourceLabelsResponse,
  RuntimeModelListRequest,
  SearchIssuesResponse,
  SearchProjectsResponse,
  Skill,
  Squad,
  TimelineEntry,
  User,
  WebhookDelivery,
} from "../types";
import type { CloudRuntimeNode } from "../runtimes/cloud-runtime";
import type { CreateFeedbackResponse } from "../feedback/types";

export const PluginBindingSchema = z.object({
  scope_type: z.string().default("workspace"),
  scope_id: z.string().default(""),
  enabled: z.boolean().default(false),
  revision: z.number().default(0),
}).loose();

export const PluginInstallationSchema = z.object({
  id: z.string(),
  plugin_key: z.string().default(""),
  display_name: z.string().default(""),
  desired_version: z.string().default(""),
  active_version: z.string().optional(),
  enabled: z.boolean().default(false),
  desired_generation: z.number().default(0),
  active_generation: z.number().default(0),
  lifecycle_status: z.string().default("error"),
  health_state: z.string().optional(),
  health_reason: z.string().optional(),
  description: z.string().optional(),
  publisher: z.string().default(""),
  publisher_type: z.string().default(""),
  trust_tier: z.string().default(""),
  source_kind: z.string().default("bundled"),
  source_ref: z.string().default(""),
  uploader_id: z.string().optional(),
  manifest_digest: z.string().default(""),
  archive_digest: z.string().default(""),
  artifact_digest: z.string().default(""),
  signature_verified: z.boolean().default(false),
  requested_capabilities: z.array(z.string()).default([]),
  available_versions: z.array(z.string()).default([]),
  contributions: z.array(z.string()).default([]),
  contribution_details: z.array(z.object({
    key: z.string(),
    type: z.string().default(""),
    name: z.string().default(""),
    description: z.string().default(""),
    entry_path: z.string().default(""),
    entry_digest: z.string().default(""),
  }).loose()).default([]),
  bindings: z.array(PluginBindingSchema).default([]),
}).loose();

export const EMPTY_PLUGIN_INSTALLATION: PluginInstallation = {
  id: "",
  plugin_key: "",
  display_name: "",
  desired_version: "",
  enabled: false,
  desired_generation: 0,
  active_generation: 0,
  lifecycle_status: "error",
  publisher: "",
  publisher_type: "",
  trust_tier: "",
  source_kind: "bundled",
  source_ref: "",
  manifest_digest: "",
  archive_digest: "",
  artifact_digest: "",
  signature_verified: false,
  requested_capabilities: [],
  available_versions: [],
  contributions: [],
  contribution_details: [],
  bindings: [],
};

export const PluginInstallationListResponseSchema = z.object({
  plugins: z.array(PluginInstallationSchema).default([]),
}).loose();

export const EMPTY_PLUGIN_INSTALLATION_LIST: PluginInstallationListResponse = {
  plugins: [],
};

export const PluginCatalogContributionSchema = z.object({
  key: z.string(),
  type: z.string().default(""),
  name: z.string().default(""),
  description: z.string().default(""),
  entry_path: z.string().default(""),
  entry_digest: z.string().default(""),
}).loose();

export const PluginCatalogReleaseSchema = z.object({
  plugin_key: z.string(),
  name: z.string().default(""),
  description: z.string().default(""),
  version: z.string(),
  publisher: z.string().default(""),
  publisher_type: z.string().default(""),
  trust_tier: z.string().default(""),
  source_kind: z.string().default("bundled"),
  source_ref: z.string().default(""),
  requested_capabilities: z.array(z.string()).default([]),
  host_api: z.string().default(""),
  required_daemon_features: z.array(z.string()).default([]),
  signature_key_id: z.string().default(""),
  signature_verified: z.boolean().default(false),
  manifest_digest: z.string().default(""),
  archive_digest: z.string().default(""),
  artifact_digest: z.string().default(""),
  compatible: z.boolean().default(false),
  compatibility_reason: z.string().optional(),
  contributions: z.array(PluginCatalogContributionSchema).default([]),
  installation: PluginInstallationSchema.optional(),
}).loose();

export const PluginCatalogResponseSchema = z.object({
  releases: z.array(PluginCatalogReleaseSchema).default([]),
  diagnostics: z.array(z.object({
    source_ref: z.string().default(""),
    code: z.string().default("unknown"),
    message: z.string().default(""),
  }).loose()).default([]),
  supported: z.boolean().optional().default(true),
}).loose();

export const EMPTY_PLUGIN_CATALOG: PluginCatalogResponse = {
  releases: [],
  diagnostics: [],
  supported: false,
};

export const GitHubInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  installation_id: z.number().optional(),
  account_login: z.string(),
  account_type: z.string(),
  account_avatar_url: z.string().nullable(),
  created_at: z.string(),
  connected_by: z.string().optional(),
}).loose();

export const ListGitHubInstallationsResponseSchema = z.object({
  installations: z.array(GitHubInstallationSchema).default([]),
  configured: z.boolean().optional().default(false),
  repository_browse_configured: z.boolean().optional().default(false),
  can_manage: z.boolean().optional().default(false),
}).loose();

export const EMPTY_LIST_GITHUB_INSTALLATIONS_RESPONSE: ListGitHubInstallationsResponse = {
  installations: [],
  configured: false,
  repository_browse_configured: false,
  can_manage: false,
};

export const GitHubConnectResponseSchema = z.object({
  url: z.string().optional(),
  configured: z.boolean().optional().default(false),
}).loose();

export const EMPTY_GITHUB_CONNECT_RESPONSE: GitHubConnectResponse = {
  configured: false,
};

export const GitHubRepositorySchema = z.object({
  id: z.number(),
  full_name: z.string(),
  html_url: z.string(),
  clone_url: z.string(),
  description: z.string().nullable(),
  private: z.boolean(),
  archived: z.boolean(),
  default_branch: z.string(),
}).loose();

export const ListGitHubRepositoriesResponseSchema = z.object({
  repositories: z.array(GitHubRepositorySchema).default([]),
  total_count: z.number().optional().default(0),
  next_page: z.number().nullable().optional().default(null),
}).loose();

export const EMPTY_LIST_GITHUB_REPOSITORIES_RESPONSE: ListGitHubRepositoriesResponse = {
  repositories: [],
  total_count: 0,
  next_page: null,
};

export const GitHubPullRequestSchema = z.object({
  id: z.string(),
  provider: z.string().optional().default("github"),
  workspace_id: z.string(),
  repo_owner: z.string(),
  repo_name: z.string(),
  number: z.number(),
  title: z.string(),
  state: z.string(),
  html_url: z.string(),
  branch: z.string().nullable(),
  author_login: z.string().nullable(),
  author_avatar_url: z.string().nullable(),
  merged_at: z.string().nullable(),
  closed_at: z.string().nullable(),
  pr_created_at: z.string(),
  pr_updated_at: z.string(),
  mergeable: z.string().nullable().optional(),
  merge_state_status: z.string().nullable().optional(),
  snapshot_available: z.boolean().optional(),
  checks_rollup: z.string().nullable().optional(),
  checks_conclusion: z.string().nullable().optional(),
  checks_total: z.number().optional().default(0),
  checks_passed: z.number().optional().default(0),
  checks_failed: z.number().optional().default(0),
  checks_running: z.number().optional().default(0),
  checks_pending: z.number().optional().default(0),
  failed_check_names: z.array(z.string()).optional().default([]),
  snapshot_stale: z.boolean().optional().default(false),
  snapshot_fetched_at: z.string().nullable().optional(),
  mergeable_state: z.string().nullable().optional(),
  additions: z.number().optional().default(0),
  deletions: z.number().optional().default(0),
  changed_files: z.number().optional().default(0),
}).loose();

export const IssuePullRequestsResponseSchema = z.object({
  pull_requests: z.array(GitHubPullRequestSchema).default([]),
}).loose();

export const EMPTY_ISSUE_PULL_REQUESTS_RESPONSE: { pull_requests: GitHubPullRequest[] } = {
  pull_requests: [],
};

// Label responses are consumed by settings tables and resource pickers. Keep
// the resource type lenient so newer server scopes do not break older clients,
// while defaulting fields that predate scoped label catalogs.
export const LabelSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  resource_type: z.string().optional().default("issue"),
  name: z.string(),
  description: z.string().optional().default(""),
  color: z.string(),
  usage_count: z.number().optional().default(0),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const EMPTY_LABEL: Label = {
  id: "",
  workspace_id: "",
  resource_type: "issue",
  name: "",
  description: "",
  color: "#6b7280",
  usage_count: 0,
  created_at: "",
  updated_at: "",
};

export const ListLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_LABELS_RESPONSE: ListLabelsResponse = {
  labels: [],
  total: 0,
};

export const ResourceLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
}).loose();

export const EMPTY_RESOURCE_LABELS_RESPONSE: ResourceLabelsResponse = {
  labels: [],
};

// Saved issue views (MUL-4796). `query`/`display` are opaque definition
// blobs interpreted client-side per `definition_version` — keep them as
// loose records so newer servers can add fields freely. `scope_type` /
// `visibility` stay lenient strings; downstream code uses explicit `===`
// comparisons and default branches per the API-compat rules.
export const IssueViewSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  owner_id: z.string().default(""),
  name: z.string().default(""),
  scope_type: z.string().default("workspace"),
  scope_id: z.string().nullish(),
  scope_variant: z.string().nullish(),
  visibility: z.string().default("private"),
  definition_version: z.number().default(1),
  query: z.record(z.string(), z.unknown()).default({}),
  display: z.record(z.string(), z.unknown()).default({}),
  revision: z.number().default(1),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export type IssueView = z.infer<typeof IssueViewSchema>;

export const IssueViewListSchema = z.array(IssueViewSchema);

export const IssueViewPreferenceSchema = z.object({
  scope_type: z.string().default("workspace"),
  scope_id: z.string().nullish(),
  prefs: z.object({
    hidden: z.array(z.string()).default([]),
    order: z.array(z.string()).default([]),
  }).loose().default({ hidden: [], order: [] }),
  updated_at: z.string().default(""),
}).loose();

export type IssueViewPreference = z.infer<typeof IssueViewPreferenceSchema>;

export const EMPTY_ISSUE_VIEW_PREFERENCE: IssueViewPreference = {
  scope_type: "workspace",
  scope_id: null,
  prefs: { hidden: [], order: [] },
  updated_at: "",
};

export interface CreateIssueViewRequest {
  name: string;
  scope_type: "workspace" | "my" | "project";
  scope_id?: string | null;
  scope_variant?: "assigned" | "created" | "involved" | "any" | "members" | "agents" | null;
  visibility: "private" | "workspace";
  definition_version: number;
  query: Record<string, unknown>;
  display: Record<string, unknown>;
}

// Custom property definitions. `type` stays a lenient string so newer server
// types don't break installed clients; UI narrows with isKnownPropertyType.
export const IssuePropertySchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  type: z.string(),
  description: z.string().optional().default(""),
  icon: z.string().optional().default(""),
  config: z.object({
    options: z.array(z.object({
      id: z.string(),
      name: z.string(),
      color: z.string().optional().default("#6b7280"),
    }).loose()).optional(),
  }).loose().default({}),
  position: z.number().optional().default(0),
  archived: z.boolean().optional().default(false),
  archived_at: z.string().nullable().optional(),
  usage_count: z.number().optional().default(0),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const EMPTY_ISSUE_PROPERTY: IssueProperty = {
  id: "",
  workspace_id: "",
  name: "",
  type: "text",
  description: "",
  icon: "",
  config: {},
  position: 0,
  archived: false,
  usage_count: 0,
  created_at: "",
  updated_at: "",
};

// Quick actions (MUL-5465). `visibility` and `status` stay z.string() rather
// than z.enum: they are server-driven, and a newer server adding a value must
// degrade to the UI's default branch, not blank the whole list.
export const QuickActionSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().optional().default(""),
  assignee_type: z.string(),
  assignee_id: z.string(),
  prompt: z.string().optional().default(""),
  visibility: z.string().optional().default("public"),
  status: z.string().optional().default("active"),
  last_used_at: z.string().nullable().optional().default(null),
  use_count: z.number().optional().default(0),
  created_by_id: z.string().optional().default(""),
  created_at: z.string(),
  updated_at: z.string(),
  target_name: z.string().optional(),
  // Both default to the pessimistic reading on an older server: "not known to
  // be public" and "not known to be missing" keep the settings row honest
  // rather than asserting a state the server never sent.
  target_public: z.boolean().optional().default(false),
  target_missing: z.boolean().optional().default(false),
}).loose();

export const EMPTY_QUICK_ACTION: QuickAction = {
  id: "",
  workspace_id: "",
  name: "",
  description: "",
  assignee_type: "agent",
  assignee_id: "",
  prompt: "",
  visibility: "public",
  status: "active",
  last_used_at: null,
  use_count: 0,
  created_by_id: "",
  created_at: "",
  updated_at: "",
  target_public: false,
  target_missing: true,
};

export const ListQuickActionsResponseSchema = z.object({
  quick_actions: z.array(QuickActionSchema).default([]),
}).loose();

export const EMPTY_LIST_QUICK_ACTIONS_RESPONSE: ListQuickActionsResponse = {
  quick_actions: [],
};

export const QuickActionRenderSchema = z.object({
  content: z.string().default(""),
}).loose();

export const ListPropertiesResponseSchema = z.object({
  properties: z.array(IssuePropertySchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_PROPERTIES_RESPONSE: ListPropertiesResponse = {
  properties: [],
  total: 0,
};

// Value bag: keyed by definition UUID; values are primitives or string
// arrays (multi_select). The preprocess step drops entries with unknown
// shapes BEFORE validation — a newer server shipping an object-shaped value
// (future actor/relation types) must degrade to "that one property missing",
// never fail the whole IssueSchema and blank the list via parseWithFallback.
export const IssuePropertyValuesSchema = z.preprocess(
  (raw) => {
    if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return {};
    const out: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(raw)) {
      const ok =
        typeof value === "string" ||
        typeof value === "number" ||
        typeof value === "boolean" ||
        (Array.isArray(value) && value.every((item) => typeof item === "string"));
      if (ok) out[key] = value;
    }
    return out;
  },
  z.record(z.string(), z.union([z.string(), z.number(), z.boolean(), z.array(z.string())])).default({}),
);

export const IssuePropertiesResponseSchema = z.object({
  properties: IssuePropertyValuesSchema,
}).loose();

export const EMPTY_ISSUE_PROPERTIES_RESPONSE: IssuePropertiesResponse = {
  properties: {},
};

export interface AppConfigResponse {
  cdn_domain: string;
  // True when the CDN domain serves private content via time-bounded signed
  // URLs (CloudFront signing) — raw storage URLs on that domain are NOT
  // publicly fetchable and must not be used as native media sources
  // (MUL-3254). Older servers omit the field; treat that as false.
  cdn_signed?: boolean;
  allow_signup: boolean;
  google_client_id?: string;
  posthog_key?: string;
  posthog_host?: string;
  analytics_environment?: string;
  daemon_server_url?: string;
  daemon_app_url?: string;
  workspace_creation_disabled?: boolean;
  /** Whether this deployment offers the self-hosted Git provider integration
   * (self-host only; off on the managed cloud). Absent/false hides the whole
   * Settings → Integrations "Git providers" section. */
  vcs_integration_available?: boolean;
  feature_flags?: Record<string, boolean>;
  server_version?: string;
}

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

// Nested attachments embedded in timeline/comment responses stay lenient on
// purpose: a single malformed attachment must not knock the whole timeline
// into the fallback `[]`.
const AttachmentSchema = z.object({
  id: z.string(),
}).loose();

const ChatQuickActionSchema = z.object({
  label: z.string(),
  prompt: z.string(),
  primary: z.boolean().optional(),
}).loose();

export const ChatMessageSchema = z.object({
  id: z.string(),
  chat_session_id: z.string(),
  role: z.enum(["user", "assistant"]).catch("assistant"),
  content: z.string().default(""),
  task_id: z.string().nullable().default(null),
  created_at: z.string().default(""),
  attachments: z.array(AttachmentSchema).optional(),
  failure_reason: z.string().nullable().optional(),
  elapsed_ms: z.number().nullable().optional(),
  message_kind: z
    .enum(["message", "no_response", "onboarding_kickoff", "onboarding_opening"])
    .catch("message")
    .optional(),
  // Optional additive data degrades independently: a malformed suggestion
  // must not hide the assistant reply that contains it.
  quick_actions: z.array(ChatQuickActionSchema).catch([]).optional().default([]),
}).loose();

export const ChatMessageListSchema = z.array(ChatMessageSchema).default([]);
export const EMPTY_CHAT_MESSAGE_LIST: ChatMessage[] = [];

export const ChatMessagesPageSchema = z.object({
  messages: z.array(ChatMessageSchema).default([]),
  limit: z.number().default(50),
  has_more: z.boolean().default(false),
  next_cursor: z.object({
    created_at: z.string(),
    id: z.string(),
  }).loose().nullable().optional(),
}).loose();

// Standalone attachment lookup (`GET /api/attachments/{id}`) is the source of
// truth for click-time download URLs. The two fields the download flow opens
// in a new tab — `download_url` and `url` — must be strings, otherwise we'd
// happily `window.open(undefined)`. `filename` gates the toast/title and is
// also enforced so a missing value falls back to the empty record below.
//
// `markdown_url` is parsed lenient: a server old enough to predate
// MUL-3192 omits the field, in which case the schema defaults it to "".
// Callers that need to persist a URL into markdown should go through the
// `useFileUpload` helper (which falls back to the legacy
// `attachmentDownloadPath` shape when `markdown_url` is empty), so the
// empty-string default does not silently break any persistence path.
export const AttachmentResponseSchema = z.object({
  id: z.string(),
  url: z.string(),
  download_url: z.string(),
  // Forced-attachment ("download button") URL — credential-free and, unlike
  // `download_url`, always Content-Disposition: attachment across every storage
  // mode. Optional: a server older than this field omits it, and callers fall
  // back to `download_url` / the stable endpoint. Never persisted (short-lived).
  attachment_download_url: z.string().optional(),
  markdown_url: z.string().optional().default(""),
  filename: z.string(),
  chat_session_id: z.string().nullable().optional(),
  chat_message_id: z.string().nullable().optional(),
}).loose();

export const EMPTY_ATTACHMENT: Attachment = {
  id: "",
  workspace_id: "",
  issue_id: null,
  comment_id: null,
  chat_session_id: null,
  chat_message_id: null,
  uploader_type: "",
  uploader_id: "",
  filename: "",
  url: "",
  download_url: "",
  markdown_url: "",
  content_type: "",
  size_bytes: 0,
  created_at: "",
};

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
  source_task_id: z.string().nullable().optional(),
  coalesced_count: z.number().optional(),
}).loose();

// /timeline returns a flat array of TimelineEntry, oldest first. The
// previously cursor-paginated wrapper was removed (#1929) — at observed data
// sizes (p99 ~30 entries per issue) paged delivery only created bugs.
export const TimelineEntriesSchema = z.array(TimelineEntrySchema);

export const EMPTY_TIMELINE_ENTRIES: TimelineEntry[] = [];

const OptionalStringSchema = z.preprocess(
  (value) => (typeof value === "string" ? value : undefined),
  z.string().optional(),
);

const BooleanWithDefaultSchema = (fallback: boolean) =>
  z.preprocess(
    (value) => (typeof value === "boolean" ? value : undefined),
    z.boolean().default(fallback),
  );

const FeatureFlagsSchema = z.preprocess(
  (value) =>
    value && typeof value === "object" && !Array.isArray(value)
      ? value
      : undefined,
  z.record(z.string(), BooleanWithDefaultSchema(false)).default({}),
);

export const AppConfigSchema = z.object({
  cdn_domain: z.string().default(""),
  cdn_signed: BooleanWithDefaultSchema(false),
  allow_signup: BooleanWithDefaultSchema(true),
  google_client_id: OptionalStringSchema,
  posthog_key: OptionalStringSchema,
  posthog_host: OptionalStringSchema,
  analytics_environment: OptionalStringSchema,
  daemon_server_url: OptionalStringSchema,
  daemon_app_url: OptionalStringSchema,
  workspace_creation_disabled: BooleanWithDefaultSchema(false).optional(),
  vcs_integration_available: BooleanWithDefaultSchema(false).optional(),
  feature_flags: FeatureFlagsSchema,
  server_version: OptionalStringSchema,
}).loose();

export const EMPTY_APP_CONFIG: AppConfigResponse = {
  cdn_domain: "",
  cdn_signed: false,
  allow_signup: true,
  google_client_id: "",
  daemon_server_url: "",
  daemon_app_url: "",
  workspace_creation_disabled: false,
  vcs_integration_available: false,
  feature_flags: {},
};

// Preference keys may grow over time, so keep both the key and value spaces
// forward-compatible while still rejecting non-string persisted data.
export const NotificationPreferenceResponseSchema = z.object({
  workspace_id: z.string(),
  preferences: z.record(z.string(), z.string()).default({}),
}).loose();

export const EMPTY_NOTIFICATION_PREFERENCE_RESPONSE: NotificationPreferenceResponse = {
  workspace_id: "",
  preferences: {},
};

export const CreateFeedbackResponseSchema = z.object({
  id: z.string(),
  created_at: z.string(),
}).loose();

export const EMPTY_CREATE_FEEDBACK_RESPONSE: CreateFeedbackResponse = {
  id: "",
  created_at: "",
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
  source_task_id: z.string().nullable().optional(),
  // Set only on comments a quick action produced (MUL-5465). Server-only.
  quick_action_id: z.string().nullable().optional(),
}).loose();

export const CommentsListSchema = z.array(CommentSchema);

// Degraded placeholder for a comment response that failed schema validation.
// The empty id is the caller's signal that nothing usable came back — the run
// UI treats it as "could not read the result" rather than a successful run.
export const EMPTY_COMMENT: Comment = {
  id: "",
  issue_id: "",
  author_type: "member",
  author_id: "",
  content: "",
  type: "comment",
  parent_id: null,
  reactions: [],
  attachments: [],
  created_at: "",
  updated_at: "",
  resolved_at: null,
  resolved_by_type: null,
  resolved_by_id: null,
};

const CommentTriggerPreviewAgentSchema = z.object({
  id: z.string(),
  name: z.string().default(""),
  avatar_url: z.string().optional(),
  source: z.string().default(""),
  reason: z.string().default(""),
}).loose();

// Per-target outcome of an explicit @agent / @squad mention (MUL-4525 §2).
// target_id is required to correlate with the client's rendered mention; a
// malformed entry (missing id) is dropped rather than failing the whole payload.
export const CommentTriggerOutcomeSchema = z.object({
  target_type: z.string().default(""),
  target_id: z.string(),
  status: z.string().default(""),
  reason_code: z.string().default(""),
}).loose();

export const CommentTriggerPreviewSchema = z.object({
  agents: z.array(CommentTriggerPreviewAgentSchema).default([]),
  // Drop malformed blocked entries INDIVIDUALLY (MUL-4525): a single bad item
  // must not discard the whole set of valid blocked mentions. A non-array
  // degrades to []; each valid entry is kept, each malformed one dropped.
  blocked: z
    .array(z.unknown())
    .catch([])
    .default([])
    .transform((items) =>
      items.flatMap((item) => {
        const parsed = CommentTriggerOutcomeSchema.safeParse(item);
        return parsed.success ? [parsed.data] : [];
      }),
    ),
}).loose();

const IssueTriggerPreviewItemSchema = z.object({
  issue_id: z.string(),
  agent_id: z.string().default(""),
  source: z.string().default(""),
  handoff_supported: z.boolean().default(false),
}).loose();

export const IssueTriggerPreviewSchema = z.object({
  triggers: z.array(IssueTriggerPreviewItemSchema).default([]),
  total_count: z.number().default(0),
}).loose();

// Metadata is primitive-only by API/DB contract. Stay lenient on shape:
// unknown keys land as `unknown` to a caller, but the field itself defaults
// to {} so consumers never need to nil-guard `issue.metadata`.
const IssueMetadataSchema = z.record(z.string(), z.union([z.string(), z.number(), z.boolean()])).default({});

export const IssueSchema = z.object({
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
  // Older backends predate `stage`; default to null so a missing field parses
  // cleanly into the non-optional Issue.stage (number | null).
  stage: z.number().nullable().default(null),
  start_date: z.string().nullable(),
  due_date: z.string().nullable(),
  metadata: IssueMetadataSchema,
  // Older backends predate custom properties; default {} so consumers never
  // nil-guard issue.properties.
  properties: IssuePropertyValuesSchema,
  reactions: z.array(z.unknown()).optional(),
  labels: z.array(z.unknown()).optional(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const ListIssuesResponseSchema = z.object({
  issues: z.array(IssueSchema).default([]),
  total: z.number().default(0),
}).loose();

// Response schema for POST /api/issues. Two tightenings over IssueSchema:
//
//   - `id` must be non-empty. A created issue always carries a real id, so an
//     empty/absent id means the create effectively failed. createIssue turns a
//     schema failure into a rejection (not a fabricated success), so tightening
//     id here routes an id-less body to that same failure path.
//   - `labels` is the backend-compatibility signal the create modal reads to
//     decide whether the backend attached labels in the create transaction
//     (present) or predates that (absent → fall back to per-label attach).
//     Validate it strictly as Label[] and degrade a malformed value to
//     `undefined` — the same as an absent field — so a wrong shape (null,
//     object, a garbage array) can never masquerade as "handled" and suppress
//     the fallback. Unlike the loose IssueSchema.labels (z.array(z.unknown())),
//     the elements are fully validated. See packages/views/modals/create-issue.tsx.
export const CreateIssueResponseSchema = IssueSchema.extend({
  id: z.string().min(1),
  labels: z.array(LabelSchema).optional().catch(undefined),
}).loose();

export const EMPTY_LIST_ISSUES_RESPONSE: ListIssuesResponse = {
  issues: [],
  total: 0,
};

const SearchIssueResultSchema = IssueSchema.extend({
  match_source: z.string(),
  matched_snippet: z.string().optional(),
  matched_description_snippet: z.string().optional(),
  matched_comment_snippet: z.string().optional(),
}).loose();

export const SearchIssuesResponseSchema = z.object({
  issues: z.array(SearchIssueResultSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_SEARCH_ISSUES_RESPONSE: SearchIssuesResponse = {
  issues: [],
  total: 0,
};

const ProjectSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  title: z.string(),
  description: z.string().nullable(),
  icon: z.string().nullable(),
  status: z.string(),
  priority: z.string(),
  lead_type: z.string().nullable(),
  lead_id: z.string().nullable(),
  // .default(null) so a project from an older backend (frontend deploys before
  // backend) that omits these keys parses to null instead of failing the whole
  // object — which would degrade a search/list batch to the empty fallback.
  start_date: z.string().nullable().default(null),
  due_date: z.string().nullable().default(null),
  created_at: z.string(),
  updated_at: z.string(),
  issue_count: z.number().default(0),
  done_count: z.number().default(0),
  resource_count: z.number().default(0),
}).loose();

const SearchProjectResultSchema = ProjectSchema.extend({
  match_source: z.string(),
  matched_snippet: z.string().optional(),
}).loose();

export const SearchProjectsResponseSchema = z.object({
  projects: z.array(SearchProjectResultSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_SEARCH_PROJECTS_RESPONSE: SearchProjectsResponse = {
  projects: [],
  total: 0,
};

const IssueAssigneeGroupSchema = z.object({
  id: z.string(),
  assignee_type: z.string().nullable(),
  assignee_id: z.string().nullable(),
  issues: z.array(IssueSchema).default([]),
  total: z.number().default(0),
}).loose();

export const GroupedIssuesResponseSchema = z.object({
  groups: z.array(IssueAssigneeGroupSchema).default([]),
}).loose();

export const EMPTY_GROUPED_ISSUES_RESPONSE: GroupedIssuesResponse = {
  groups: [],
};

const IssueTableActorRefSchema = z.object({
  // Server-driven enums stay open so installed desktop clients survive a
  // backend that introduces another actor kind.
  type: z.string(),
  id: z.string(),
}).loose();

const IssueTableParentRefSchema = z.object({
  id: z.string(),
  number: z.number(),
  identifier: z.string(),
  title: z.string(),
  status: z.string(),
}).loose();

const IssueTableGroupValueSchema = z.discriminatedUnion("kind", [
  z.object({
    kind: z.literal("status"),
    status: z.string(),
  }).loose(),
  z.object({
    kind: z.literal("assignee"),
    actor: IssueTableActorRefSchema.nullable(),
  }).loose(),
  z.object({
    kind: z.literal("project"),
    project_id: z.string().nullable().optional().default(null),
  }).loose(),
  z.object({
    kind: z.literal("parent"),
    parent_id: z.string().nullable().optional().default(null),
    parent: IssueTableParentRefSchema.nullable().optional().default(null),
    value_state: z.enum(["value", "unavailable", "unset"]),
  }).loose(),
  z.object({
    kind: z.literal("property"),
    property_id: z.string(),
    value: z.union([z.string(), z.boolean(), z.null()]).optional(),
    value_state: z.enum(["value", "unavailable", "unset"]),
  }).loose(),
]);

const IssueTableGroupDescriptorSchema: z.ZodType<IssueTableGroupDescriptor> = z.lazy(() => z.object({
  key: z.string(),
  value: IssueTableGroupValueSchema,
  count: z.number(),
  secondary_groups: z.array(IssueTableGroupDescriptorSchema).optional(),
}).loose());

export const IssueTableGroupsResponseSchema = z.object({
  query_fingerprint: z.string(),
  total: z.number(),
  groups: z.array(IssueTableGroupDescriptorSchema).default([]),
  next_cursor: z.string().nullable().default(null),
}).loose();

export const EMPTY_ISSUE_TABLE_GROUPS_RESPONSE: IssueTableGroupsResponse = {
  query_fingerprint: "",
  total: 0,
  groups: [],
  next_cursor: null,
};

const IssueTableRowSchema = z.object({
  issue: IssueSchema,
  direct_child_count: z.number().default(0),
}).loose();

export const IssueTableRowsResponseSchema = z.object({
  query_fingerprint: z.string(),
  group_key: z.string().nullable().default(null),
  parent_id: z.string().nullable().default(null),
  total: z.number(),
  rows: z.array(IssueTableRowSchema).default([]),
  branch_total: z.number(),
  next_cursor: z.string().nullable().default(null),
}).loose();

export const EMPTY_ISSUE_TABLE_ROWS_RESPONSE: IssueTableRowsResponse = {
  query_fingerprint: "",
  group_key: null,
  parent_id: null,
  total: 0,
  rows: [],
  branch_total: 0,
  next_cursor: null,
};

const IssueTableFacetValueSchema = z.object({
  key: z.string(),
  count: z.number(),
}).loose();

const IssueTableFacetSchema = z.object({
  kind: z.enum(["status", "priority", "assignee", "creator", "project", "label", "property", "working_agents"]),
  property_id: z.string().optional(),
  values: z.array(IssueTableFacetValueSchema).default([]),
}).loose();

export const IssueTableFacetsResponseSchema = z.object({
  query_fingerprint: z.string(),
  total: z.number(),
  facets: z.array(IssueTableFacetSchema).default([]),
}).loose();

export const EMPTY_ISSUE_TABLE_FACETS_RESPONSE: IssueTableFacetsResponse = {
  query_fingerprint: "",
  total: 0,
  facets: [],
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

export const CloudRuntimeNodeSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  instance_id: z.string(),
  region: z.string(),
  instance_type: z.string(),
  image_id: z.string(),
  subnet_id: z.string(),
  name: z.string(),
  status: z.string(),
  tags: z.record(z.string(), z.string()).default({}),
  metadata: z.record(z.string(), z.unknown()).default({}),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const CloudRuntimeNodeListSchema = z.array(CloudRuntimeNodeSchema);

export const EMPTY_CLOUD_RUNTIME_NODE_LIST: CloudRuntimeNode[] = [];

export const EMPTY_CLOUD_RUNTIME_NODE: CloudRuntimeNode = {
  id: "",
  owner_id: "",
  instance_id: "",
  region: "",
  instance_type: "",
  image_id: "",
  subnet_id: "",
  name: "",
  status: "",
  tags: {},
  metadata: {},
  created_at: "",
  updated_at: "",
};

// ---------------------------------------------------------------------------
// Workspace dashboard schemas
//
// The dashboard hits three independent rollup endpoints. Each returns a flat
// array, and every field is consumed by chart / KPI math — a missing number
// silently degrades to NaN downstream, so we coerce missing numbers to 0.
// String fields default to "" (no enum narrowing) to survive future model /
// agent ID drift, and so a single null from tz-aware SQL bucketing fails
// only that row instead of dropping the whole array to the `[]` fallback.
// ---------------------------------------------------------------------------

// Cost split carried by every usage row. `cost_usd_ticks` is what the provider
// itself charged for the rows behind this aggregate (1e-10 USD); the
// `uncosted_*` counts are the tokens from rows the provider did NOT price, and
// so are the only ones the client should run through its rate table.
//
// The `uncosted_*` fields are deliberately `.optional()` rather than
// `.default(0)`: a backend that predates them sends nothing, and defaulting
// those rows to "0 tokens left to estimate" would silently zero their cost.
// `undefined` means "this backend doesn't split", and the consumer falls back
// to the full token counts — i.e. exactly the old behaviour. A real 0 from a
// current backend means "everything here is already priced", which is a
// different thing and must stay distinguishable.
const CostSplitShape = {
  cost_usd_ticks: z.number().optional(),
  uncosted_input_tokens: z.number().optional(),
  uncosted_output_tokens: z.number().optional(),
  uncosted_cache_read_tokens: z.number().optional(),
  uncosted_cache_write_tokens: z.number().optional(),
};

const DashboardUsageDailySchema = z.object({
  date: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const DashboardUsageDailyListSchema = z.array(DashboardUsageDailySchema);

const DashboardUsageByAgentSchema = z.object({
  agent_id: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const DashboardUsageByAgentListSchema = z.array(DashboardUsageByAgentSchema);

// `cancelled_count` defaults to 0 so an installed client pointed at a
// backend that predates it still renders: those rows simply carry no
// cancelled segment, which is exactly what that backend measured.
const DashboardAgentRunTimeSchema = z.object({
  agent_id: z.string().default(""),
  total_seconds: z.number().default(0),
  task_count: z.number().default(0),
  failed_count: z.number().default(0),
  cancelled_count: z.number().default(0),
}).loose();

export const DashboardAgentRunTimeListSchema = z.array(DashboardAgentRunTimeSchema);

const DashboardRunTimeDailySchema = z.object({
  date: z.string().default(""),
  total_seconds: z.number().default(0),
  task_count: z.number().default(0),
  failed_count: z.number().default(0),
  cancelled_count: z.number().default(0),
}).loose();

export const DashboardRunTimeDailyListSchema = z.array(DashboardRunTimeDailySchema);

// Failure rollups. `failure_reason` is an open string on purpose — it carries
// the backend's canonical taxonomy, which grows as new classifier rules land
// (server/pkg/taskfailure). Pinning it to a z.enum would make an installed
// desktop client drop rows for a reason its build predates; the client folds
// unrecognised reasons into an "other" display class instead. The empty
// string is the succeeded bucket, so `.default("")` is a meaningful default
// only for a row that already lost its reason — such a row lands in the
// denominator rather than inventing a failure that never happened.
const DashboardFailureDailySchema = z.object({
  date: z.string().default(""),
  failure_reason: z.string().default(""),
  task_count: z.number().default(0),
}).loose();

export const DashboardFailureDailyListSchema = z.array(DashboardFailureDailySchema);

const DashboardFailureByAgentSchema = z.object({
  agent_id: z.string().default(""),
  failure_reason: z.string().default(""),
  task_count: z.number().default(0),
}).loose();

export const DashboardFailureByAgentListSchema = z.array(
  DashboardFailureByAgentSchema,
);

// ---------------------------------------------------------------------------
// Runtime usage schemas — the runtime-detail page's four usage endpoints
// (`/api/runtimes/:id/usage*`). Same leniency rules as the dashboard
// schemas above: numbers default to 0, strings to "", `.loose()` passes
// unknown fields.
// ---------------------------------------------------------------------------

const RuntimeUsageSchema = z.object({
  runtime_id: z.string().default(""),
  date: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
}).loose();

export const RuntimeUsageListSchema = z.array(RuntimeUsageSchema);

const RuntimeHourlyActivitySchema = z.object({
  hour: z.number().default(0),
  count: z.number().default(0),
}).loose();

export const RuntimeHourlyActivityListSchema = z.array(RuntimeHourlyActivitySchema);

const RuntimeUsageByAgentSchema = z.object({
  agent_id: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const RuntimeUsageByAgentListSchema = z.array(RuntimeUsageByAgentSchema);

const RuntimeUsageByHourSchema = z.object({
  hour: z.number().default(0),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const RuntimeUsageByHourListSchema = z.array(RuntimeUsageByHourSchema);

// ---------------------------------------------------------------------------
// Agent task responses. The base object stays loose so daemon/runtime fields
// can drift while task-list consumers still validate the fields they render.
// ---------------------------------------------------------------------------

// Human attribution (MUL-4302 §9): who an agent run is accountable to, and how
// that human was resolved. Every field is defensive so a departed member, an
// autopilot run (no originator), or an older backend degrades to a partial
// object instead of a parse failure.
const AttributionUserSchema = z.object({
  id: z.string().default(""),
  name: z.string().optional(),
  email: z.string().optional(),
  avatar_url: z.string().optional(),
}).loose();

const TaskEvidenceSchema = z.object({
  kind: z.string().default(""),
  ref_id: z.string().default(""),
}).loose();

const TaskAttributionSchema = z.object({
  source: z.string().default("unattributed"),
  precise: z.boolean().default(false),
  initiator: AttributionUserSchema.optional(),
  originator: AttributionUserSchema.optional(),
  evidence: TaskEvidenceSchema.optional(),
  rule_version_id: z.string().optional(),
  delegated_from_task_id: z.string().optional(),
  retry_of_task_id: z.string().optional(),
  rerun_of_task_id: z.string().optional(),
}).loose();

const OptionalStringArraySchema = z.preprocess(
  (value) =>
    Array.isArray(value) && value.every((item) => typeof item === "string")
      ? value
      : undefined,
  z.array(z.string()).optional(),
);

// One (provider, model) slice of a run's token usage. Token counts default to
// 0 rather than failing the row: a slice missing one counter is still worth
// pricing on the counters it does have, and the "we have no usage at all" case
// is carried by the field's absence, not by a zeroed entry.
const TaskUsageSchema = z.object({
  provider: z.string().optional(),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  cost_usd_ticks: z.number().optional(),
}).loose();

export const AgentTaskSchema = z.object({
  id: z.string(),
  agent_id: z.string().default(""),
  runtime_id: z.string().default(""),
  issue_id: z.string().default(""),
  status: z.string().default("cancelled"),
  priority: z.number().default(0),
  dispatched_at: z.string().nullable().default(null),
  started_at: z.string().nullable().default(null),
  completed_at: z.string().nullable().default(null),
  result: z.unknown().default(null),
  error: z.string().nullable().default(null),
  failure_reason: z.string().optional(),
  created_at: z.string().default(""),
  chat_session_id: z.string().optional(),
  autopilot_run_id: z.string().optional(),
  parent_task_id: z.string().optional(),
  attempt: z.number().optional(),
  trigger_comment_id: z.string().optional(),
  // Coverage is additive display metadata. A mixed-version or partially
  // upgraded server must not make one malformed optional field erase the
  // entire execution log, so degrade that field to "absent" independently.
  coalesced_comment_ids: OptionalStringArraySchema,
  delivered_comment_ids: OptionalStringArraySchema,
  trigger_summary: z.string().optional(),
  handoff_note: z.string().optional(),
  kind: z.string().optional(),
  work_dir: z.string().optional(),
  relative_work_dir: z.string().optional(),
  attribution: TaskAttributionSchema.optional(),
  // Per-run token usage. Same independent-degradation rule as the coverage
  // arrays above: usage is additive display metadata, so one malformed entry
  // must cost the row its usage figure, not erase the whole execution log.
  // `.catch(undefined)` collapses a bad array to "no usage recorded", which
  // the UI already renders as an em dash.
  usage: z.array(TaskUsageSchema).optional().catch(undefined),
}).loose();

export const AgentTaskListSchema = z.array(AgentTaskSchema);

// Task cancellation (`POST /api/tasks/:id/cancel`) is consumed directly by
// chat recovery. Its optional message payload must be well-formed before the
// UI deletes a message from cache or restores text into the input.
const CancelledChatMessageSchema = z.object({
  chat_session_id: z.string(),
  message_id: z.string(),
  content: z.string(),
  restore_to_input: z.boolean().default(false),
  // Attachments detached from the deleted message so a restored draft can
  // re-bind them on re-send. Absent on servers that predate the field.
  attachments: z.array(AttachmentSchema).optional(),
}).loose();

export const CancelTaskResponseSchema = AgentTaskSchema.extend({
  cancelled_chat_message: CancelledChatMessageSchema.nullish()
    .transform((value) => value ?? undefined),
}).loose();

// Deferred-cancellation draft restores
// (`GET /api/chat/sessions/{id}/draft-restores`, #5219) feed the composer
// directly: `content` becomes the draft text, `attachments` re-bind on
// re-send, and `id` is the consume key. A malformed response falls back to
// an empty list — the durable row stays pending server-side, so nothing is
// lost by skipping a fetch.
const ChatDraftRestoreSchema = z.object({
  id: z.string(),
  chat_session_id: z.string(),
  task_id: z.string().optional(),
  content: z.string().default(""),
  attachments: z.array(AttachmentSchema).optional(),
  created_at: z.string().optional(),
}).loose();

export const ChatDraftRestoresResponseSchema = z.object({
  restores: z.array(ChatDraftRestoreSchema).default([]),
}).loose();

const ChatQueuedTaskSchema = z.object({
  task_id: z.string(),
  status: z.string().default("queued"),
  created_at: z.string().default(""),
  message_id: z.string().optional(),
  content: z.string().optional(),
}).loose();

const ChatQueuedTasksSchema = z.array(z.unknown()).transform((tasks) =>
  tasks.flatMap((task) => {
    const parsed = ChatQueuedTaskSchema.safeParse(task);
    return parsed.success ? [parsed.data] : [];
  }),
);

// Root fields retain the legacy single-task response shape. Keep additive
// fields optional so callers can distinguish an older server from an empty
// queue. A malformed queue row is ignored without discarding a valid head.
export const ChatPendingTaskSchema: z.ZodType<ChatPendingTask> = z.object({
  task_id: z.string().optional(),
  status: z.string().optional(),
  created_at: z.string().optional(),
  supports_queue: z.boolean().optional(),
  queued_tasks: ChatQueuedTasksSchema.optional(),
}).loose();

export const EMPTY_CHAT_PENDING_TASK: ChatPendingTask = {};

export const SendChatMessageResponseSchema: z.ZodType<SendChatMessageResponse> = z.object({
  message_id: z.string().min(1),
  task_id: z.string().min(1),
  supports_queue: z.boolean().optional(),
  queued: z.boolean().optional().catch(undefined),
  created_at: z.string().min(1),
  attachment_ids: z.array(z.string()).nullish().transform((ids) => ids ?? undefined),
}).loose();

// `started` is the only field the flow branches on, and a malformed response
// must not be read as "the opening landed" — parseWithFallback's fallback says
// it did not, which leaves the flow's own retry as the recovery path.
export const StartMikaOnboardingResponseSchema: z.ZodType<StartMikaOnboardingResponse> = z.object({
  started: z.boolean(),
  message_id: z.string().nullish().transform((id) => id ?? undefined),
  created_at: z.string().nullish().transform((at) => at ?? undefined),
}).loose();

export const PrioritizeQueuedChatTaskResponseSchema:
  z.ZodType<PrioritizeQueuedChatTaskResponse> = z.object({
    task_id: z.string(),
    active_task_id: z.string().optional(),
  }).loose();

export const EMPTY_PRIORITIZE_QUEUED_CHAT_TASK_RESPONSE:
  PrioritizeQueuedChatTaskResponse = { task_id: "" };

export const EMPTY_CHAT_DRAFT_RESTORES: ChatDraftRestoresResponse = {
  restores: [],
};

export const EMPTY_CANCEL_TASK_RESPONSE: CancelTaskResponse = {
  id: "",
  agent_id: "",
  runtime_id: "",
  issue_id: "",
  status: "cancelled",
  priority: 0,
  dispatched_at: null,
  started_at: null,
  completed_at: null,
  result: null,
  error: null,
  created_at: "",
};

export const AgentBuilderSessionSchema = z.object({
  session_id: z.string(),
  builder_agent_id: z.string(),
  runtime_id: z.string(),
}).loose();

export const EMPTY_AGENT_BUILDER_SESSION: AgentBuilderSession = {
  session_id: "",
  builder_agent_id: "",
  runtime_id: "",
};

/**
 * The stored configuration of a creation conversation. Every field falls back
 * to empty on its own: a draft written by a newer build (or truncated in
 * transit) must still restore the fields it does understand rather than
 * discarding the user's work wholesale.
 */
export const StoredAgentDraftSchema = z.object({
  name: z.string().catch(""),
  description: z.string().catch(""),
  instructions: z.string().catch(""),
  avatar_url: z.string().nullable().catch(null),
  model: z.string().catch(""),
  thinking_level: z.string().catch(""),
  service_tier: z.string().catch(""),
  skill_ids: z.array(z.string()).catch([]),
  permission_scope: z
    .enum(["private", "workspace", "members"])
    .catch("private"),
  member_ids: z.array(z.string()).catch([]),
  team_ids: z.array(z.string()).catch([]),
  applied_message_id: z.string().nullable().catch(null),
}).loose();

/**
 * One unfinished creation draft. Every field except the id has a safe empty
 * default: an older server that omits `runtime_id` must degrade to "let the
 * user pick" rather than dropping the whole row and losing the conversation.
 */
export const AgentBuilderSessionSummarySchema = z.object({
  session_id: z.string(),
  title: z.string().catch(""),
  runtime_id: z.string().catch(""),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
  last_message_content: z.string().catch(""),
  last_message_role: z.string().catch(""),
  last_message_at: z.string().catch(""),
  // Absent for a conversation the user has never hand-edited; the client then
  // replays the last <agent_draft> block instead of restoring a stored copy.
  draft: StoredAgentDraftSchema.nullish().catch(null),
}).loose();

export const AgentBuilderSessionListSchema = z.object({
  sessions: z.array(AgentBuilderSessionSummarySchema).catch([]),
}).loose();

export const EMPTY_AGENT_BUILDER_SESSION_LIST: {
  sessions: AgentBuilderSessionSummary[];
} = { sessions: [] };

export const AgentBuilderRuntimeSwitchSchema = z.object({
  runtime_id: z.string(),
}).loose();

// This endpoint returns 2xx only after the carrier has been bound to the
// runtime the caller asked for; anything else is a thrown error and no commit.
// So the safe fallback for an unparseable SUCCESS body is the requested id, not
// an empty one: the rebind did happen, and reporting "unknown" would leave the
// picker showing a runtime that is no longer executing — the exact split this
// endpoint exists to close.
export const agentBuilderRuntimeSwitchFallback = (
  requestedRuntimeID: string,
): AgentBuilderRuntimeSwitch => ({ runtime_id: requestedRuntimeID });

// Squad list responses carry lightweight membership previews used by hover
// cards. The preview fields are additive API fields, so older backends default
// cleanly to no preview instead of breaking newer frontends.
const SquadMemberPreviewSchema = z.object({
  member_type: z.string(),
  member_id: z.string(),
  role: z.string().default(""),
}).loose();

export const SquadSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().default(""),
  instructions: z.string().default(""),
  system_key: z.string().optional(),
  system_instructions: z.string().optional(),
  avatar_url: z.string().nullable().optional().transform((v) => v ?? null),
  leader_id: z.string(),
  creator_id: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
  archived_at: z.string().nullable().optional().transform((v) => v ?? null),
  archived_by: z.string().nullable().optional().transform((v) => v ?? null),
  member_count: z.number().default(0),
  member_preview: z.array(SquadMemberPreviewSchema).default([]),
}).loose();

export const SquadListSchema = z.array(SquadSchema);
export const EMPTY_SQUAD_LIST: Squad[] = [];
export const EMPTY_SQUAD: Squad = {
  id: "",
  workspace_id: "",
  name: "",
  description: "",
  instructions: "",
  avatar_url: null,
  leader_id: "",
  creator_id: "",
  created_at: "",
  updated_at: "",
  archived_at: null,
  archived_by: null,
  member_count: 0,
  member_preview: [],
};

// Squad member status — backs the Squad detail page's Members tab. status
// is `string | null` (not the narrow `SquadMemberStatusValue` union) so a
// new server-side status doesn't fail the parse; the UI defaults to a
// neutral pill for unknown values.
const SquadActiveIssueBriefSchema = z.object({
  issue_id: z.string(),
  identifier: z.string(),
  title: z.string(),
  issue_status: z.string(),
}).loose();

const SquadMemberStatusSchema = z.object({
  member_type: z.string(),
  member_id: z.string(),
  status: z.string().nullable().optional().transform((v) => v ?? null),
  active_issues: z.array(SquadActiveIssueBriefSchema).default([]),
  last_active_at: z.string().nullable().optional().transform((v) => v ?? null),
}).loose();

export const SquadMemberStatusListResponseSchema = z.object({
  members: z.array(SquadMemberStatusSchema).default([]),
}).loose();

export const EMPTY_SQUAD_MEMBER_STATUS_LIST = { members: [] };

// ---------------------------------------------------------------------------
// Structured error body — POST /api/workspaces/:wsId/issues 409 conflict.
//
// When the server detects an active issue with the same title in the same
// workspace, it returns `{ code: "active_duplicate_issue", error, issue }`
// instead of letting the create through. The UI uses the embedded issue ref
// to offer "view existing" rather than dropping the user into a generic
// "create failed" toast.
//
// Strict guarantees:
//   - `code` is a literal so a future server rename (e.g. `duplicate_issue`)
//     fails the parse and falls back to a normal error toast — drift never
//     ships as a broken duplicate UI.
//   - `issue` is required; without an id/identifier/title the "view existing"
//     button has nothing to point at, so we'd rather fall back than guess.
//   - `issue.status` is intentionally OMITTED: the duplicate toast doesn't
//     render a StatusIcon (which has no fallback for unknown enum values),
//     so a future server-side rename of `status` must not knock this branch
//     out. `.loose()` lets the field pass through unchanged for any other
//     consumer.
// ---------------------------------------------------------------------------

export const DuplicateIssueErrorBodySchema = z.object({
  code: z.literal("active_duplicate_issue"),
  error: z.string().optional(),
  issue: z.object({
    id: z.string(),
    identifier: z.string(),
    title: z.string(),
  }).loose(),
}).loose();

export interface DuplicateIssueErrorBody {
  code: "active_duplicate_issue";
  error?: string;
  issue: {
    id: string;
    identifier: string;
    title: string;
  };
}

// ---------------------------------------------------------------------------
// Webhook delivery schemas — backing the Autopilot Deliveries section. Enums
// (`status`, `signature_status`, `provider`) are kept as `z.string()` so a
// future server-side value (e.g. a Stripe provider, a new dedupe state)
// degrades to a generic UI fallback rather than collapsing the list into
// the empty array. `.loose()` lets unknown fields pass through, matching
// the rule used by every other endpoint here.
// ---------------------------------------------------------------------------

const WebhookDeliverySchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  autopilot_id: z.string(),
  trigger_id: z.string(),
  provider: z.string(),
  event: z.string(),
  dedupe_key: z.string().nullable(),
  dedupe_source: z.string().nullable(),
  signature_status: z.string(),
  status: z.string(),
  attempt_count: z.number().default(0),
  // Older servers predate the durable dispatch queue. Defaults preserve
  // compatibility while the UI rolls out alongside the new worker.
  dispatch_attempts: z.number().default(0),
  available_at: z.string().default(""),
  content_type: z.string().nullable(),
  response_status: z.number().nullable(),
  autopilot_run_id: z.string().nullable(),
  replayed_from_delivery_id: z.string().nullable(),
  error: z.string().nullable(),
  received_at: z.string(),
  last_attempt_at: z.string(),
  created_at: z.string(),
  // Detail-only fields. The list endpoint omits them; the detail endpoint
  // populates raw_body / selected_headers / response_body.
  selected_headers: z.record(z.string(), z.unknown()).nullable().optional(),
  raw_body: z.string().nullable().optional(),
  response_body: z.string().nullable().optional(),
}).loose();

export const ListWebhookDeliveriesResponseSchema = z.object({
  deliveries: z.array(WebhookDeliverySchema).default([]),
  total: z.number().default(0),
}).loose();

export const WebhookDeliveryResponseSchema = WebhookDeliverySchema;

export const EMPTY_LIST_WEBHOOK_DELIVERIES_RESPONSE: ListWebhookDeliveriesResponse = {
  deliveries: [],
  total: 0,
};

// ---------------------------------------------------------------------------
// Autopilot list schema. Enums (`status`, `execution_mode`, `trigger_kinds`,
// `last_run_status`) stay `z.string()` so future server-side values degrade
// to a generic UI fallback. The three derived fields (trigger_kinds /
// next_run_at / last_run_status) are list-endpoint-only and absent on older
// servers — optional by contract, the list renders "—" without them.
// ---------------------------------------------------------------------------

const AutopilotListItemSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  title: z.string(),
  description: z.string().nullable().optional(),
  project_id: z.string().nullable().optional(),
  // Older servers (pre-MUL-2429) omit assignee_type; "agent" is the
  // documented default.
  assignee_type: z.string().default("agent"),
  assignee_id: z.string(),
  status: z.string(),
  execution_mode: z.string(),
  issue_title_template: z.string().nullable().optional(),
  created_by_type: z.string(),
  created_by_id: z.string(),
  last_run_at: z.string().nullable().optional(),
  created_at: z.string(),
  updated_at: z.string(),
  trigger_kinds: z.array(z.string()).optional(),
  next_run_at: z.string().nullable().optional(),
  last_run_status: z.string().nullable().optional(),
  // Per-caller write capability; absent on older servers (treated as unknown).
  can_write: z.boolean().optional(),
  // Narrower per-caller access-management capability (detail endpoint only).
  can_manage_access: z.boolean().optional(),
}).loose();

export const ListAutopilotsResponseSchema = z.object({
  autopilots: z.array(AutopilotListItemSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_AUTOPILOTS_RESPONSE = {
  autopilots: [],
  total: 0,
};

// Autopilot run (POST /trigger, GET /runs). Consumed by the "run now" flow,
// which branches on `status` to avoid a false-success toast (MUL-4525), so the
// response must be schema-parsed. `reason_code` is an additive, stable
// classification of a non-success run the UI localizes; older servers omit it.
// Defaults are conservative: an unreadable run degrades to a non-success status
// so the UI never shows success it cannot confirm. .loose() tolerates new fields.
export const AutopilotRunSchema = z.object({
  id: z.string().default(""),
  autopilot_id: z.string().default(""),
  trigger_id: z.string().nullable().default(null),
  source: z.string().default("manual"),
  status: z.string().default("failed"),
  issue_id: z.string().nullable().default(null),
  task_id: z.string().nullable().default(null),
  triggered_at: z.string().default(""),
  completed_at: z.string().nullable().default(null),
  failure_reason: z.string().nullable().default(null),
  reason_code: z.string().optional(),
  trigger_payload: z.unknown().default(null),
  result: z.unknown().default(null),
  created_at: z.string().default(""),
}).loose();

export const FALLBACK_AUTOPILOT_RUN: AutopilotRun = {
  id: "",
  autopilot_id: "",
  trigger_id: null,
  source: "manual",
  status: "failed",
  issue_id: null,
  task_id: null,
  triggered_at: "",
  completed_at: null,
  failure_reason: null,
  trigger_payload: null,
  result: null,
  created_at: "",
};

// Cron preview: the server is the authority on the next occurrences. No
// `.default([])` here — a missing or reshaped field must fail validation so it
// degrades to the `next_runs: null` fallback ("preview unreadable") instead of
// masquerading as a valid empty list ("this expression never fires").
export const CronPreviewResponseSchema = z.object({
  next_runs: z.array(z.string()),
}).loose();

export const UNREADABLE_CRON_PREVIEW_RESPONSE: CronPreviewResponse = {
  next_runs: null,
};

export const EMPTY_WEBHOOK_DELIVERY: WebhookDelivery = {
  id: "",
  workspace_id: "",
  autopilot_id: "",
  trigger_id: "",
  provider: "",
  event: "",
  dedupe_key: null,
  dedupe_source: null,
  signature_status: "not_required",
  status: "queued",
  attempt_count: 0,
  dispatch_attempts: 0,
  available_at: "",
  content_type: null,
  response_status: null,
  autopilot_run_id: null,
  replayed_from_delivery_id: null,
  error: null,
  received_at: "",
  last_attempt_at: "",
  created_at: "",
};

// ---------------------------------------------------------------------------
// User (`/api/me` GET + PATCH). The auth store and Settings → Account both
// trust this shape — a drift here would knock both surfaces out. Kept
// lenient by the same rules as IssueSchema: enums stay `z.string()`,
// nullable fields are unioned with `null`, unknown server fields pass
// through via `.loose()`. `profile_description` is the field added in
// MUL-2406; the server emits `""` when unset (NOT NULL DEFAULT ''), so
// the schema defaults to `""` too — keeps the type tight without
// breaking older backends that don't return the column yet.
// ---------------------------------------------------------------------------

export const UserSchema = z.object({
  id: z.string(),
  name: z.string().default(""),
  email: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  onboarded_at: z.string().nullable().default(null),
  onboarding_questionnaire: z.record(z.string(), z.unknown()).default({}),
  starter_content_state: z.string().nullable().default(null),
  language: z.string().nullable().default(null),
  profile_description: z.string().default(""),
  timezone: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_USER: User = {
  id: "",
  name: "",
  email: "",
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: {},
  starter_content_state: null,
  language: null,
  profile_description: "",
  timezone: null,
  created_at: "",
  updated_at: "",
};

// ---------------------------------------------------------------------------
// Cross-workspace unread inbox summary (`/api/inbox/unread-summary` GET).
// One entry per workspace the user belongs to that has unread items; the
// sidebar derives the workspace-switcher dot from it. Lenient per the usual
// rules so a future field addition can't blank the dot — on malformed JSON
// parseWithFallback returns the empty list, which simply hides the dot.
// ---------------------------------------------------------------------------

export const InboxUnreadSummarySchema = z.array(
  z
    .object({
      workspace_id: z.string(),
      count: z.number(),
    })
    .loose(),
);

export const EMPTY_INBOX_UNREAD_SUMMARY: InboxWorkspaceUnread[] = [];

// ---------------------------------------------------------------------------
// Archived inbox items (`/api/inbox/archived` GET).
// Lenient per the usual rules: `severity` / `type` / `recipient_type` stay
// `z.string()` so a notification kind this client doesn't know yet still
// parses and renders (the UI's type-label lookup already tolerates unknown
// kinds). Nullable optional fields are declared optional as well, since older
// rows can omit them entirely. On malformed JSON parseWithFallback returns the
// empty list — the archived view then reads as empty rather than white-
// screening the inbox.
// ---------------------------------------------------------------------------

export const InboxItemListSchema = z.array(
  z
    .object({
      id: z.string(),
      workspace_id: z.string(),
      recipient_type: z.string(),
      recipient_id: z.string(),
      type: z.string(),
      severity: z.string(),
      issue_id: z.string().nullish(),
      title: z.string(),
      body: z.string().nullish(),
      read: z.boolean(),
      archived: z.boolean(),
      created_at: z.string(),
    })
    .loose(),
);

export const EMPTY_INBOX_ITEMS: InboxItem[] = [];

// ---------------------------------------------------------------------------
// Billing schemas (cloud-billing proxy surface)
//
// All billing JSON we receive comes from multica-cloud verbatim — we proxy
// the bytes without re-shaping. These schemas use `loose()` so a future
// non-breaking field addition on the cloud side doesn't crash us; required
// fields are still strictly enforced. EMPTY_* constants supply the
// fallback parseWithFallback uses when the upstream response is malformed
// or unparseable.

export const BillingBalanceSchema = z.object({
  owner_id: z.string(),
  balance_micro: z.number(),
  balance_credit: z.number(),
  updated_at: z.string(),
}).loose();

export const EMPTY_BILLING_BALANCE: BillingBalance = {
  owner_id: "",
  balance_micro: 0,
  balance_credit: 0,
  updated_at: "",
};

// `tx_type` and `source` are kept as plain strings here; the cloud doc
// enumerates the canonical values but the frontend display tolerates
// unknown ones gracefully. Strict enums would crash the page on a future
// addition (e.g. a new `topup` source kind).
export const BillingTransactionSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  idempotency_key: z.string().default(""),
  tx_type: z.string(),
  source: z.string(),
  amount_micro: z.number(),
  balance_after: z.number(),
  reference_id: z.string().default(""),
  description: z.string().default(""),
  metadata: z.record(z.string(), z.unknown()).default({}),
  created_at: z.string(),
}).loose();

export const BillingTransactionsPageSchema = z.object({
  items: z.array(BillingTransactionSchema).default([]),
  total: z.number().default(0),
  page: z.number().default(1),
  page_size: z.number().default(20),
}).loose();

export const EMPTY_BILLING_TRANSACTIONS_PAGE: BillingTransactionsPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingBatchSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  source_tx_id: z.string().default(""),
  source_type: z.string(),
  total_micro: z.number(),
  remaining_micro: z.number(),
  // Cloud either omits the key (never expires) or sends a string
  // timestamp. Null is also tolerated since some serializers emit
  // explicit nulls for absent timestamps.
  expires_at: z.string().nullable().optional(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const BillingBatchesPageSchema = z.object({
  items: z.array(BillingBatchSchema).default([]),
  total: z.number().default(0),
  page: z.number().default(1),
  page_size: z.number().default(20),
}).loose();

export const EMPTY_BILLING_BATCHES_PAGE: BillingBatchesPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingTopupSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  amount_cents: z.number(),
  currency: z.string().default("usd"),
  credits: z.number(),
  bonus_credits: z.number().default(0),
  status: z.string(),
  tier_id: z.string().default(""),
  stripe_checkout_id: z.string().default(""),
  // Only set after status reaches `credited` — leave optional rather
  // than coerce to "" so a UI can branch on existence.
  purchase_batch_id: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const BillingTopupsPageSchema = z.object({
  items: z.array(BillingTopupSchema).default([]),
  total: z.number().default(0),
  page: z.number().default(1),
  page_size: z.number().default(20),
}).loose();

export const EMPTY_BILLING_TOPUPS_PAGE: BillingTopupsPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingPriceTierSchema = z.object({
  id: z.string(),
  // Cloud doc says display_name falls back to id; tolerate empty too.
  display_name: z.string().default(""),
  amount_cents: z.number(),
  credits: z.number(),
  bonus_credits: z.number().optional(),
  bonus_expires_in: z.string().optional(),
}).loose();

export const BillingPriceTierListSchema = z.array(BillingPriceTierSchema);

export const EMPTY_BILLING_PRICE_TIER_LIST: BillingPriceTier[] = [];

export const CreateBillingCheckoutSessionResponseSchema = z.object({
  order_id: z.string(),
  session_id: z.string(),
  url: z.string(),
}).loose();

export const EMPTY_CREATE_BILLING_CHECKOUT_SESSION_RESPONSE: CreateBillingCheckoutSessionResponse = {
  order_id: "",
  session_id: "",
  url: "",
};

export const BillingCheckoutSessionStatusSchema = z.object({
  order_id: z.string(),
  status: z.string(),
  amount_cents: z.number(),
  credits: z.number(),
  bonus_credits: z.number().default(0),
  currency: z.string().default("usd"),
  tier_id: z.string().default(""),
}).loose();

export const EMPTY_BILLING_CHECKOUT_SESSION_STATUS: BillingCheckoutSessionStatus = {
  order_id: "",
  status: "pending",
  amount_cents: 0,
  credits: 0,
  bonus_credits: 0,
  currency: "usd",
  tier_id: "",
};

export const CreateBillingPortalSessionResponseSchema = z.object({
  url: z.string(),
}).loose();

export const EMPTY_CREATE_BILLING_PORTAL_SESSION_RESPONSE: CreateBillingPortalSessionResponse = {
  url: "",
};

// ---------------------------------------------------------------------------
// Workspace subscriptions (`/api/cloud-subscriptions/*`)
//
// These schemas are the compatibility boundary with multica-cloud. Three rules
// hold for all of them:
//
//  1. There is no fallback value. Callers get `null` on any parse failure and
//     must render "unavailable" — never a synthetic Free plan, because that
//     turns an upstream outage or an older cloud into a silent downgrade of a
//     paying workspace.
//  2. `.loose()` keeps unknown keys, so a cloud that adds fields does not break
//     an older client.
//  3. `plan` and `status` stay open strings. A new plan or Stripe status must
//     surface as unknown rather than be coerced into a known one.

const WorkspaceSubscriptionIntervalSchema = z.enum(["month", "year"]);

// Stripe hosts Checkout and Portal, so those URLs leave the app. `z.string()
// .url()` is not enough on its own — `new URL("javascript:...")` parses — and
// the caller hands this value to location.assign, so the scheme is pinned here.
const StripeHostedURLSchema = z.string().url().refine(
  (value) => value.startsWith("https://"),
  { message: "Stripe hosted URL must use HTTPS" },
);


export const WorkspaceSubscriptionEntitlementsSchema = z
  .object({
    workspace_id: z.string(),
    plan: z.string(),
    status: z.string(),
    // Cloud documents seats as >= 1, but accepting 0 costs nothing and keeps a
    // workspace that momentarily reports no human members readable instead of
    // failing the whole snapshot.
    seats: z.number().int().nonnegative(),
    issue_window: z.number().int().nonnegative().nullable(),
    autopilot_runs: z.number().int().nonnegative().nullable(),
    current_period_end: z.string().nullable().optional(),
    snapshot_expires_at: z.string().nullable().optional(),
    version: z.number().int().nonnegative(),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionEntitlements => ({
      workspaceId: value.workspace_id,
      plan: value.plan,
      status: value.status,
      seats: value.seats,
      issueWindow: value.issue_window,
      autopilotRuns: value.autopilot_runs,
      currentPeriodEnd: value.current_period_end ?? null,
      snapshotExpiresAt: value.snapshot_expires_at ?? null,
      version: value.version,
    }),
  );

export const WorkspaceSubscriptionSummarySchema = z
  .object({
    entitlement: WorkspaceSubscriptionEntitlementsSchema,
    billing_interval: WorkspaceSubscriptionIntervalSchema.nullable().optional(),
    actual_seats: z.number().int().nonnegative(),
    billed_seats: z.number().int().nonnegative().nullable().optional(),
    pending_seat_quantity: z.number().int().nonnegative().nullable().optional(),
    cancel_at_period_end: z.boolean().optional(),
    grace_until: z.string().nullable().optional(),
    has_stripe_customer: z.boolean().optional(),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionSummary => ({
      entitlement: value.entitlement,
      billingInterval: value.billing_interval ?? null,
      actualSeats: value.actual_seats,
      billedSeats: value.billed_seats ?? null,
      pendingSeatQuantity: value.pending_seat_quantity ?? null,
      cancelAtPeriodEnd: value.cancel_at_period_end ?? false,
      graceUntil: value.grace_until ?? null,
      hasStripeCustomer: value.has_stripe_customer ?? false,
    }),
  );

const WorkspaceSubscriptionPriceSchema = (
  expected: "month" | "year",
) =>
  z
    .object({
      currency: z.string().min(1),
      // Reject 0 and negatives: a free or malformed Price must read as
      // "price unavailable", not as a real amount shown next to a purchase
      // button.
      unit_amount: z.number().int().positive(),
      // Pinned to the slot it arrived in. Cloud validates this too, but a
      // schema that accepted a yearly Price under `month` would let the UI
      // quote a yearly amount as a monthly one — the schema is an independent
      // boundary, so it checks the correspondence itself.
      interval: z.literal(expected),
      interval_count: z.literal(1),
    })
    .loose()
    .transform(
      (value): WorkspaceSubscriptionPrice => ({
        currency: value.currency,
        unitAmount: value.unit_amount,
        interval: value.interval,
        intervalCount: value.interval_count,
      }),
    );

export const WorkspaceSubscriptionPricesSchema = z
  .object({
    month: WorkspaceSubscriptionPriceSchema("month"),
    year: WorkspaceSubscriptionPriceSchema("year"),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionPrices => ({
      month: value.month,
      year: value.year,
    }),
  );

export const CreateWorkspaceSubscriptionCheckoutResponseSchema = z
  .object({
    request_id: z.string(),
    session_id: z.string(),
    url: StripeHostedURLSchema,
  })
  .loose()
  .transform(
    (value): CreateWorkspaceSubscriptionCheckoutResponse => ({
      requestId: value.request_id,
      sessionId: value.session_id,
      url: value.url,
    }),
  );

export const WorkspaceSubscriptionSeatReconcileResultSchema = z
  .object({
    workspace_id: z.string(),
    billed_seats: z.number().int().nonnegative(),
    actual_seats: z.number().int().nonnegative(),
    action: z.string(),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionSeatReconcileResult => ({
      workspaceId: value.workspace_id,
      billedSeats: value.billed_seats,
      actualSeats: value.actual_seats,
      action: value.action,
    }),
  );

export const CreateWorkspaceSubscriptionPortalResponseSchema = z
  .object({
    url: StripeHostedURLSchema,
  })
  .loose()
  .transform(
    (value): CreateWorkspaceSubscriptionPortalResponse => ({
      url: value.url,
    }),
  );

// ---------------------------------------------------------------------------
// Runtime model discovery (`POST /api/runtimes/:id/models`,
// `GET /api/runtimes/:id/models/:requestId`). Both endpoints return the same
// request record, and the UI drives a state machine off `status`, so the two
// fields that decide behaviour are pinned: `status` gates the polling loop and
// `supported` gates whether the picker is usable at all. Everything else stays
// lenient per the rules at the top of this file.
//
// `status` deliberately stays `z.string()` (a newer server may add a state);
// `resolveRuntimeModels` treats anything it does not recognise as an explicit
// failure rather than a completed-but-empty catalog. `supported` defaults to
// true so a server old enough to omit it keeps the picker enabled instead of
// rendering "managed by runtime" off an `undefined`.
//
// `cached` / `cached_at` are additive markers for a snapshot served from the
// server-side catalog cache (MUL-5444); an older backend omits them.
// ---------------------------------------------------------------------------

const RuntimeModelThinkingLevelSchema = z.object({
  value: z.string(),
  label: z.string().default(""),
  description: z.string().optional(),
}).loose();

const RuntimeModelThinkingSchema = z.object({
  supported_levels: z.array(RuntimeModelThinkingLevelSchema).default([]),
  default_level: z.string().optional(),
}).loose();

const RuntimeModelServiceTierSchema = z.object({
  id: z.string(),
  name: z.string().default(""),
  description: z.string().optional(),
}).loose();

// A model entry with no `id` is unselectable — `onChange(m.id)` would persist
// an empty model — so `id` is required and a malformed entry drops the whole
// response to the fallback rather than rendering a dead row.
const RuntimeModelSchema = z.object({
  id: z.string(),
  label: z.string().default(""),
  provider: z.string().optional(),
  default: z.boolean().optional(),
  thinking: RuntimeModelThinkingSchema.nullable().optional()
    .transform((v) => v ?? undefined),
  service_tiers: z.array(RuntimeModelServiceTierSchema).optional(),
}).loose();

export const RuntimeModelListRequestSchema = z.object({
  id: z.string().default(""),
  runtime_id: z.string().default(""),
  status: z.string(),
  models: z.array(RuntimeModelSchema).optional(),
  supported: z.boolean().default(true),
  error: z.string().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  cached: z.boolean().optional(),
  cached_at: z.string().optional(),
}).loose();

// Fallback for an unparseable model-discovery response. `failed` is the only
// honest choice: `completed` would fabricate an empty catalog (and silently
// clear a saved model when `supported` is read as false), while `pending`
// would spin the picker until the client-side poll timeout. `failed` surfaces
// "discovery failed" immediately and leaves the creatable manual-entry field
// working, which is the same degradation as a real discovery failure.
export const MALFORMED_RUNTIME_MODEL_LIST_REQUEST: RuntimeModelListRequest = {
  id: "",
  runtime_id: "",
  status: "failed",
  supported: true,
  error: "invalid model discovery response",
  created_at: "",
  updated_at: "",
};

export const DingTalkInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  installer_user_id: z.string().default(""),
  status: z.string().default("revoked"),
  installed_at: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_DINGTALK_INSTALLATION: DingTalkInstallation = {
  id: "",
  workspace_id: "",
  agent_id: "",
  installer_user_id: "",
  status: "revoked",
  installed_at: "",
  created_at: "",
  updated_at: "",
};

export const ListDingTalkInstallationsResponseSchema = z.object({
  installations: z.array(DingTalkInstallationSchema).default([]),
  configured: z.boolean().default(false),
  install_supported: z.boolean().optional(),
  group_routing_supported: z.boolean().optional(),
}).loose();

export const EMPTY_LIST_DINGTALK_INSTALLATIONS_RESPONSE: ListDingTalkInstallationsResponse = {
  installations: [],
  configured: false,
};

export const DingTalkGroupRouteSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  installation_id: z.string().default(""),
  conversation_id: z.string().default(""),
  conversation_title: z.string().default(""),
  agent_id: z.string().default(""),
  discovered_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_DINGTALK_GROUP_ROUTE: DingTalkGroupRoute = {
  id: "",
  workspace_id: "",
  installation_id: "",
  conversation_id: "",
  conversation_title: "",
  agent_id: "",
  discovered_at: "",
  updated_at: "",
};

export const ListDingTalkGroupRoutesResponseSchema = z.object({
  routes: z.array(DingTalkGroupRouteSchema).default([]),
}).loose();

export const EMPTY_LIST_DINGTALK_GROUP_ROUTES_RESPONSE: ListDingTalkGroupRoutesResponse = {
  routes: [],
};

export const RedeemDingTalkBindingTokenResponseSchema = z.object({
  workspace_id: z.string().default(""),
  installation_id: z.string().default(""),
  dingtalk_user_id: z.string().default(""),
}).loose();

export const EMPTY_REDEEM_DINGTALK_BINDING_TOKEN_RESPONSE: RedeemDingTalkBindingTokenResponse = {
  workspace_id: "",
  installation_id: "",
  dingtalk_user_id: "",
};

// WeCom smart-bot ("智能机器人" / aibot) installation responses. `.loose()` so a
// newer backend field never fails the parse on an older desktop build (see
// CLAUDE.md → API Compatibility). Defaults are chosen so a malformed response
// degrades safely: `configured` defaults false (renders the "ask your operator"
// state rather than a Connect dialog whose submit is guaranteed to fail), and a
// missing `status` defaults to "revoked" rather than "active" so a broken read
// never shows a bot as connected when it may not be.
export const WecomInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  bot_id: z.string().default(""),
  installer_user_id: z.string().default(""),
  status: z.string().default("revoked"),
}).loose();

export const EMPTY_WECOM_INSTALLATION: WecomInstallation = {
  id: "",
  workspace_id: "",
  agent_id: "",
  bot_id: "",
  installer_user_id: "",
  status: "revoked",
};

export const ListWecomInstallationsResponseSchema = z.object({
  installations: z.array(WecomInstallationSchema).default([]),
  configured: z.boolean().default(false),
  install_supported: z.boolean().optional(),
}).loose();

export const EMPTY_LIST_WECOM_INSTALLATIONS_RESPONSE: ListWecomInstallationsResponse = {
  installations: [],
  configured: false,
};

export const RedeemWecomBindingTokenResponseSchema = z.object({
  workspace_id: z.string().default(""),
  installation_id: z.string().default(""),
  wecom_user_id: z.string().default(""),
}).loose();

export const EMPTY_REDEEM_WECOM_BINDING_TOKEN_RESPONSE: RedeemWecomBindingTokenResponse = {
  workspace_id: "",
  installation_id: "",
  wecom_user_id: "",
};

// Skills. Introduced for `POST /api/skills/:id/refresh` (update a skill from
// its imported source). `config` stays a loose record: the server owns the
// `origin` provenance shape and may extend it freely.
export const SkillFileSchema = z.object({
  id: z.string(),
  skill_id: z.string(),
  path: z.string(),
  content: z.string().optional().default(""),
  created_at: z.string().optional().default(""),
  updated_at: z.string().optional().default(""),
}).loose();

export const SkillSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().optional().default(""),
  content: z.string().optional().default(""),
  config: z.record(z.string(), z.unknown()).optional().default({}),
  created_by: z.string().nullable().optional().default(null),
  created_at: z.string().optional().default(""),
  updated_at: z.string().optional().default(""),
  files: z.array(SkillFileSchema).optional().default([]),
}).loose();

export const EMPTY_SKILL: Skill = {
  id: "",
  workspace_id: "",
  name: "",
  description: "",
  content: "",
  config: {},
  created_by: null,
  created_at: "",
  updated_at: "",
  files: [],
};
