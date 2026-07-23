import { z } from "zod";
import type {
  Agent,
  AgentTemplate,
  AgentTemplateSummary,
  AgentBuilderRuntimeSwitch,
  AgentBuilderSession,
  Attachment,
  AutopilotRun,
  BillingBalance,
  BillingBatchesPage,
  BillingCheckoutSessionStatus,
  BillingPriceTier,
  BillingTopupsPage,
  BillingTransactionsPage,
  CancelTaskResponse,
  ChatDraftRestoresResponse,
  CreateAgentFromTemplateResponse,
  CreateBillingCheckoutSessionResponse,
  CreateBillingPortalSessionResponse,
  CronPreviewResponse,
  GroupedIssuesResponse,
  InboxItem,
  InboxWorkspaceUnread,
  Label,
  IssueProperty,
  ListPropertiesResponse,
  IssuePropertiesResponse,
  IssueTableGroupDescriptor,
  IssueTableFacetsResponse,
  IssueTableGroupsResponse,
  IssueTableRowsResponse,
  ListIssuesResponse,
  ListLabelsResponse,
  ListWebhookDeliveriesResponse,
  NotificationPreferenceResponse,
  ResourceLabelsResponse,
  SearchIssuesResponse,
  SearchProjectsResponse,
  Squad,
  TimelineEntry,
  User,
  WebhookDelivery,
} from "../types";
import type { CloudRuntimeNode } from "../runtimes/cloud-runtime";
import type { CreateFeedbackResponse } from "../feedback/types";

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
}).loose();

export const CommentsListSchema = z.array(CommentSchema);

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
  kind: z.enum(["status", "priority", "assignee", "creator", "project", "label", "property"]),
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

const DashboardUsageDailySchema = z.object({
  date: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
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
  task_count: z.number().default(0),
}).loose();

export const DashboardUsageByAgentListSchema = z.array(DashboardUsageByAgentSchema);

const DashboardAgentRunTimeSchema = z.object({
  agent_id: z.string().default(""),
  total_seconds: z.number().default(0),
  task_count: z.number().default(0),
  failed_count: z.number().default(0),
}).loose();

export const DashboardAgentRunTimeListSchema = z.array(DashboardAgentRunTimeSchema);

const DashboardRunTimeDailySchema = z.object({
  date: z.string().default(""),
  total_seconds: z.number().default(0),
  task_count: z.number().default(0),
  failed_count: z.number().default(0),
}).loose();

export const DashboardRunTimeDailyListSchema = z.array(DashboardRunTimeDailySchema);

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

// ---------------------------------------------------------------------------
// Agent template catalog — `/api/agent-templates*` and the
// create-from-template response. The desktop app's create-agent picker
// reaches these endpoints, and a future server change to the template shape
// would white-screen older installed builds (#2192 pattern) without these
// parsers. Lenient by the same rules as IssueSchema above: arrays default to
// `[]`, optional fields stay optional, `.loose()` lets unknown fields pass
// through unchanged.
// ---------------------------------------------------------------------------

const AgentTemplateSkillRefSchema = z.object({
  source_url: z.string(),
  cached_name: z.string().default(""),
  cached_description: z.string().default(""),
}).loose();

const AgentTemplateSummarySchemaBase = z.object({
  slug: z.string(),
  name: z.string(),
  description: z.string().default(""),
  category: z.string().optional(),
  icon: z.string().optional(),
  accent: z.string().optional(),
  // skills MUST default to [] — picker code reads `template.skills.length`
  // and `.map(...)`, both of which crash on `undefined`. The most common
  // future drift (field renamed / wrapped) lands here.
  skills: z.array(AgentTemplateSkillRefSchema).default([]),
}).loose();

export const AgentTemplateSummarySchema = AgentTemplateSummarySchemaBase;

// List endpoint historically returns a bare array. Server could legitimately
// migrate to `{templates: [...]}` later — we accept either shape so an old
// desktop survives the upgrade.
export const AgentTemplateSummaryListSchema = z.union([
  z.array(AgentTemplateSummarySchemaBase),
  z.object({ templates: z.array(AgentTemplateSummarySchemaBase).default([]) })
    .loose()
    .transform((v) => v.templates),
]);

export const EMPTY_AGENT_TEMPLATE_SUMMARY_LIST: AgentTemplateSummary[] = [];

export const AgentTemplateSchema = AgentTemplateSummarySchemaBase.extend({
  // Detail-only field. Default "" so a malformed detail still renders the
  // header + skill list; the user just sees an empty Instructions block.
  instructions: z.string().default(""),
}).loose();

// Used as the parse fallback for `GET /api/agent-templates/:slug`. Slug comes
// from the URL, so we round-trip the requested one back into the fallback
// at the call site (see `getAgentTemplate` in client.ts).
export const EMPTY_AGENT_TEMPLATE_DETAIL: AgentTemplate = {
  slug: "",
  name: "",
  description: "",
  skills: [],
  instructions: "",
};

// ---------------------------------------------------------------------------
// Agent invocation permissions (MUL-3963)
//
// Full agent request/response payloads are NOT zod-validated today — the API
// client returns them typed directly (see client.ts `listAgents` /
// `getAgent` / `createAgent`), so there is no `AgentSchema` /
// `CreateAgentRequestSchema` / `UpdateAgentRequestSchema` to extend here.
// These lenient, exported fragments encode the new permission fields so any
// future agent schema — and the from-template minimal agent below — can reuse
// them. Per this file's convention the enum stays lenient (a future
// server-side value degrades to the strict default rather than failing the
// parse), and the target array defaults to `[]`.
// ---------------------------------------------------------------------------

export const AgentPermissionModeSchema = z
  .enum(["private", "public_to"])
  .catch("private");

export const AgentInvocationTargetSchema = z
  .object({
    target_type: z.string(),
    target_id: z.string().nullable().optional().transform((v) => v ?? null),
  })
  .loose();

export const AgentInvocationTargetsSchema = z
  .array(AgentInvocationTargetSchema)
  .default([]);

// `agent` is a full Agent record — schematising every field would duplicate
// a 50-field interface and bit-rot fast. We keep it loose and require only
// `id`, the one field the create-from-template flow consumes (used to
// navigate to the new agent's detail page). Downstream code already
// optional-chains the rest. The permission fields are parsed leniently when
// present so the from-template response carries a well-formed access shape.
const MinimalAgentSchema = z.object({
  id: z.string(),
  permission_mode: AgentPermissionModeSchema.optional(),
  invocation_targets: AgentInvocationTargetsSchema.optional(),
}).loose();

export const CreateAgentFromTemplateResponseSchema = z.object({
  agent: MinimalAgentSchema,
  imported_skill_ids: z.array(z.string()).default([]),
  reused_skill_ids: z.array(z.string()).default([]),
}).loose();

// Fallback when the success response fails to parse. The agent server-side
// has likely been created already, so we can't pretend nothing happened —
// the caller (`create-agent-dialog.tsx`) is responsible for noticing
// `agent.id === ""` and skipping navigation while keeping the list
// invalidation, so the user finds their new agent in the list.
export const EMPTY_CREATE_AGENT_FROM_TEMPLATE_RESPONSE: CreateAgentFromTemplateResponse = {
  agent: { id: "" } as Agent,
  imported_skill_ids: [],
  reused_skill_ids: [],
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
