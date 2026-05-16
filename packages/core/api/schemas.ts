import { z } from "zod";
import type { CRMEmailEngineStatus, CRMIMAPImportResponse, CRMIMAPPreviewResponse, ListIssuesResponse, TimelinePage } from "../types";

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

export const CRMIMAPPreviewMessageSchema = z.object({
  uid: z.string(),
  external_message_id: z.string().default(""),
  subject: z.string().default(""),
  from_email: z.string().default(""),
  from_name: z.string().default(""),
  to_emails: z.array(z.string()).default([]),
  cc_emails: z.array(z.string()).default([]),
  received_at: z.string().nullable().optional(),
  snippet: z.string().default(""),
  raw_size: z.number().default(0),
}).loose();

export const CRMIMAPPreviewResponseSchema = z.object({
  messages: z.array(CRMIMAPPreviewMessageSchema).default([]),
  total: z.number().default(0),
  limit: z.number().default(0),
  sync_enabled: z.boolean().default(false),
  note: z.string().default(""),
}).loose();

export const EMPTY_CRM_IMAP_PREVIEW_RESPONSE: CRMIMAPPreviewResponse = {
  messages: [],
  total: 0,
  limit: 0,
  sync_enabled: false,
  note: "",
};

export const CRMIMAPImportResponseSchema = z.object({
  ok: z.boolean().default(false),
  run_id: z.string().optional(),
  fetched: z.number().default(0),
  imported: z.number().default(0),
  skipped: z.number().default(0),
}).loose();

export const EMPTY_CRM_IMAP_IMPORT_RESPONSE: CRMIMAPImportResponse = {
  ok: false,
  fetched: 0,
  imported: 0,
  skipped: 0,
};

const CRMEmailEngineFolderSchema = z.object({
  path: z.string().default(""),
  name: z.string().default(""),
  special_use: z.string().nullable().optional(),
  total: z.number().default(0),
  unread: z.number().default(0),
}).loose();

export const CRMEmailEngineStatusSchema = z.object({
  enabled: z.boolean().default(false),
  configured: z.boolean().default(false),
  base_url: z.string().nullable().optional(),
  account: z.string().nullable().optional(),
  state: z.string().nullable().optional(),
  syncing: z.boolean().default(false),
  last_error: z.string().nullable().optional(),
  folders: z.array(CRMEmailEngineFolderSchema).default([]),
  fallback_provider: z.string().default("imap_smtp"),
}).loose();

export const EMPTY_CRM_EMAILENGINE_STATUS: CRMEmailEngineStatus = {
  enabled: false,
  configured: false,
  syncing: false,
  folders: [],
  fallback_provider: "imap_smtp",
};
