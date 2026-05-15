/**
 * Mobile-local zod schemas + fallbacks for endpoints whose responses aren't
 * yet schematised in @multica/core/api/schemas. Lenient by design — see the
 * leniency rationale at the top of the core file (string enums tolerated,
 * loose() so unknown server fields pass through, defaults so a missing
 * array doesn't take the page down).
 *
 * If web/desktop later need these same schemas, promote them to core; until
 * then they live here so mobile satisfies its "Parse, don't cast" rule
 * (root CLAUDE.md "API Response Compatibility") for these endpoints.
 */
import { z } from "zod";
import type {
  Attachment,
  ChatMessage,
  ChatPendingTask,
  ChatSession,
  IssueLabelsResponse,
  Label,
  ListLabelsResponse,
  ListProjectResourcesResponse,
  ListProjectsResponse,
  Project,
  ProjectResource,
  SearchIssuesResponse,
  SearchProjectsResponse,
  SendChatMessageResponse,
} from "@multica/core/types";
import { IssueSchema } from "@multica/core/api/schemas";

/** Upload response. Only fields mobile actually consumes — `url` to put
 *  into the markdown link, `filename` for the `[📎 name](url)` form, `id`
 *  for future linking. `.loose()` so the server can add fields without
 *  breaking mobile. Web's AttachmentSchema (packages/core/api/schemas.ts:41)
 *  is even looser (only `id`); mobile validates more because the upload
 *  flow inserts `url` directly into editable text and an empty `url` would
 *  produce a broken link the user only notices after submit. */
export const AttachmentSchema: z.ZodType<Attachment> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  issue_id: z.string().nullable().default(null),
  comment_id: z.string().nullable().default(null),
  uploader_type: z.string().default(""),
  uploader_id: z.string().default(""),
  filename: z.string(),
  url: z.string(),
  download_url: z.string().default(""),
  content_type: z.string().default(""),
  size_bytes: z.number().default(0),
  created_at: z.string().default(""),
}).loose();

const LabelSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  color: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const ListLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_LABELS_RESPONSE: ListLabelsResponse = {
  labels: [],
  total: 0,
};

export const IssueLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
}).loose();

export const EMPTY_ISSUE_LABELS_RESPONSE: IssueLabelsResponse = {
  labels: [],
};

export const ProjectSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  title: z.string(),
  description: z.string().nullable(),
  icon: z.string().nullable(),
  status: z.string(),
  priority: z.string(),
  lead_type: z.string().nullable(),
  lead_id: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
  issue_count: z.number().default(0),
  done_count: z.number().default(0),
  resource_count: z.number().default(0),
}).loose();

export const ListProjectsResponseSchema = z.object({
  projects: z.array(ProjectSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_PROJECTS_RESPONSE: ListProjectsResponse = {
  projects: [],
  total: 0,
};

// Fallback for `GET /api/projects/{id}` when the response shape drifts.
// `id` defaults to empty — caller can detect "not found / drift" by checking
// `data.id === ""` and rendering an error state instead of pretending the
// data is valid. Status / priority cast to the enum literals so TS callers
// downstream still flow correctly; runtime values came from the schema
// (`z.string()`), which would have already passed.
export const EMPTY_PROJECT: Project = {
  id: "",
  workspace_id: "",
  title: "",
  description: null,
  icon: null,
  status: "planned",
  priority: "none",
  lead_type: null,
  lead_id: null,
  created_at: "",
  updated_at: "",
  issue_count: 0,
  done_count: 0,
  resource_count: 0,
};

// Project resources are typed pointers to external resources (today: GitHub
// repos). resource_ref shape varies per resource_type; lenient on both
// `resource_type` (so a future type doesn't crash the list) and
// `resource_ref` (passes through unchanged for the renderer to dispatch on).
const ProjectResourceSchema = z.object({
  id: z.string(),
  project_id: z.string(),
  workspace_id: z.string(),
  resource_type: z.string(),
  resource_ref: z.unknown(),
  label: z.string().nullable(),
  position: z.number().default(0),
  created_at: z.string(),
  created_by: z.string().nullable(),
}).loose();

export const ListProjectResourcesResponseSchema = z.object({
  resources: z.array(ProjectResourceSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_PROJECT_RESOURCES_RESPONSE: ListProjectResourcesResponse = {
  resources: [],
  total: 0,
};

// =====================================================
// Chat (sessions / messages / pending task)
// =====================================================
// Lenient on every field that's purely informational (status enum, timestamps,
// agent/creator ids). `.loose()` so server-added fields pass through. The two
// fields mobile keys behaviour on — `id` and `chat_session_id` — are required.

export const ChatSessionSchema: z.ZodType<ChatSession> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  creator_id: z.string().default(""),
  title: z.string().default(""),
  // Enum drift defense (root CLAUDE.md "Enum drift downgrades, not crashes"):
  // unknown server values fall back to "active" so the row still renders.
  status: z.enum(["active", "archived"]).catch("active"),
  has_unread: z.boolean().default(false),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const ChatSessionListSchema = z.array(ChatSessionSchema).default([]);

export const EMPTY_CHAT_SESSION_LIST: ChatSession[] = [];

// `attachments` carried for parity rendering only — v1 doesn't author them on
// mobile. AttachmentSchema is reused as-is.
export const ChatMessageSchema: z.ZodType<ChatMessage> = z.object({
  id: z.string(),
  chat_session_id: z.string(),
  // If the server ever introduces a third role, fall back to "assistant" so
  // the message renders (as a left-aligned bubble) instead of crashing the
  // list. Matches Enum drift defense.
  role: z.enum(["user", "assistant"]).catch("assistant"),
  content: z.string().default(""),
  task_id: z.string().nullable().default(null),
  created_at: z.string().default(""),
  attachments: z.array(AttachmentSchema).optional(),
  failure_reason: z.string().nullable().optional(),
  elapsed_ms: z.number().nullable().optional(),
}).loose();

export const ChatMessageListSchema = z.array(ChatMessageSchema).default([]);

export const EMPTY_CHAT_MESSAGE_LIST: ChatMessage[] = [];

// All fields optional — server returns an empty object when no in-flight task.
export const ChatPendingTaskSchema: z.ZodType<ChatPendingTask> = z.object({
  task_id: z.string().optional(),
  status: z.string().optional(),
  created_at: z.string().optional(),
}).loose();

export const EMPTY_CHAT_PENDING_TASK: ChatPendingTask = {};

export const SendChatMessageResponseSchema: z.ZodType<SendChatMessageResponse> = z.object({
  message_id: z.string(),
  task_id: z.string(),
  created_at: z.string().default(""),
}).loose();

// =====================================================
// Search (issues + projects)
// =====================================================
// Mirrors SearchIssueResult / SearchProjectResult in packages/core/types/api.ts.
// Web does not currently route search responses through parseWithFallback, so
// the schemas live mobile-side. Promote to core when web adopts the same
// defense.
//
// match_source is the server's hint of which field matched. Enum-drift defense
// (root CLAUDE.md "Enum drift downgrades, not crashes"): unknown values fall
// back to "title" so the row still renders without a snippet line.

const SearchIssueResultSchema = IssueSchema.safeExtend({
  match_source: z.enum(["title", "description", "comment"]).catch("title"),
  matched_snippet: z.string().optional(),
});

export const SearchIssuesResponseSchema = z.object({
  issues: z.array(SearchIssueResultSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_SEARCH_ISSUES_RESPONSE: SearchIssuesResponse = {
  issues: [],
  total: 0,
};

const SearchProjectResultSchema = ProjectSchema.safeExtend({
  match_source: z.enum(["title", "description"]).catch("title"),
  matched_snippet: z.string().optional(),
});

export const SearchProjectsResponseSchema = z.object({
  projects: z.array(SearchProjectResultSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_SEARCH_PROJECTS_RESPONSE: SearchProjectsResponse = {
  projects: [],
  total: 0,
};

// Helpers re-exported for ergonomic single-import at the call site.
export type { Label, Project, ProjectResource };
