import { z } from "zod";
const actor = z.enum(["member", "agent"]).catch("member");
const summary = z.object({
  id: z.string().min(1),
  workspace_id: z.string(),
  folder_id: z.string().nullable(),
  title: z.string(),
  revision: z.number().int().positive(),
  version_count: z.number().int().positive(),
  created_by_type: actor,
  created_by_id: z.string(),
  updated_by_type: actor,
  updated_by_id: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
});
function mapSummary(d: z.infer<typeof summary>) {
  return {
    id: d.id,
    workspaceId: d.workspace_id,
    folderId: d.folder_id,
    title: d.title,
    revision: d.revision,
    versionCount: d.version_count,
    createdByType: d.created_by_type,
    createdById: d.created_by_id,
    updatedByType: d.updated_by_type,
    updatedById: d.updated_by_id,
    createdAt: d.created_at,
    updatedAt: d.updated_at,
  };
}
export const DocSummarySchema = summary.transform(mapSummary);
// Never default missing content or revision: a malformed fetch must not erase a doc on autosave.
export const DocSchema = summary
  .extend({ content: z.string() })
  .transform((d) => ({ ...mapSummary(d), content: d.content }));
export const DocFolderSchema = z
  .object({
    id: z.string().min(1),
    workspace_id: z.string(),
    parent_id: z.string().nullable(),
    title: z.string(),
    created_by_type: actor,
    created_by_id: z.string(),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .transform((d) => ({
    id: d.id,
    workspaceId: d.workspace_id,
    parentId: d.parent_id,
    title: d.title,
    createdByType: d.created_by_type,
    createdById: d.created_by_id,
    createdAt: d.created_at,
    updatedAt: d.updated_at,
  }));
export const DocTreeSchema = z.object({
  folders: z.array(DocFolderSchema),
  docs: z.array(DocSummarySchema),
});
// One unparseable hit is dropped instead of emptying the whole result list.
const searchHit = summary
  .extend({
    match_source: z.enum(["title", "content"]).catch("content"),
    matched_snippet: z.string().nullable().optional(),
  })
  .transform((d) => ({
    ...mapSummary(d),
    matchSource: d.match_source,
    matchedSnippet: d.matched_snippet ?? null,
  }));
export const DocSearchResponseSchema = z.object({
  docs: z
    .array(z.unknown())
    .transform((rows) =>
      rows.flatMap((row) => {
        const hit = searchHit.safeParse(row);
        return hit.success ? [hit.data] : [];
      }),
    ),
});
export const DocCommentSchema = z
  .object({
    id: z.string().min(1),
    doc_id: z.string(),
    parent_id: z.string().nullable().default(null),
    author_type: actor,
    author_id: z.string(),
    content: z.string(),
    anchor_quote: z.string().default(""),
    anchor_prefix: z.string().default(""),
    anchor_suffix: z.string().default(""),
    resolved_at: z.string().nullable().default(null),
    resolved_by_type: actor.nullable().default(null),
    resolved_by_id: z.string().nullable().default(null),
    created_at: z.string(),
    updated_at: z.string(),
  })
  .transform((c) => ({
    id: c.id,
    docId: c.doc_id,
    parentId: c.parent_id,
    authorType: c.author_type,
    authorId: c.author_id,
    content: c.content,
    anchorQuote: c.anchor_quote,
    anchorPrefix: c.anchor_prefix,
    anchorSuffix: c.anchor_suffix,
    resolvedAt: c.resolved_at,
    resolvedByType: c.resolved_by_type,
    resolvedById: c.resolved_by_id,
    createdAt: c.created_at,
    updatedAt: c.updated_at,
  }));
// One unreadable comment is dropped instead of emptying the discussion list.
export const DocCommentListSchema = z.object({
  comments: z.array(z.unknown()).transform((rows) =>
    rows.flatMap((row) => {
      const comment = DocCommentSchema.safeParse(row);
      return comment.success ? [comment.data] : [];
    }),
  ),
});
const cancelledBy = z.object({ type: z.string(), id: z.string().optional(), name: z.string().optional() });
export const DocTaskRunSchema = z
  .object({
    id: z.string().min(1),
    agent_id: z.string(),
    doc_thread_id: z.string(),
    status: z.string(),
    failure_reason: z.string().default(""),
    error: z.string().nullable().catch(null).default(null),
    cancelled_by: cancelledBy.optional().catch(undefined),
    created_at: z.string().default(""),
    completed_at: z.string().nullable().catch(null).default(null),
    can_cancel: z.boolean().catch(false).default(false),
  })
  .transform((r) => ({
    id: r.id,
    agentId: r.agent_id,
    docThreadId: r.doc_thread_id,
    status: r.status,
    failureReason: r.failure_reason,
    error: r.error,
    cancelledBy: r.cancelled_by,
    createdAt: r.created_at,
    completedAt: r.completed_at,
    canCancel: r.can_cancel,
  }));
// One unreadable run is dropped instead of hiding every discussion's status.
export const DocTaskRunListSchema = z.object({
  tasks: z.array(z.unknown()).transform((rows) =>
    rows.flatMap((row) => {
      const run = DocTaskRunSchema.safeParse(row);
      return run.success ? [run.data] : [];
    }),
  ),
});
export const DocVersionSchema = z
  .object({
    doc_id: z.string(),
    number: z.number().int().positive(),
    title: z.string(),
    author_type: actor,
    author_id: z.string(),
    message: z.string().default(""),
    action: z.enum(["create", "edit", "restore"]).catch("edit"),
    action_ref: z.number().nullable().default(null),
    task_id: z.string().nullable().default(null),
    issue_id: z.string().nullable().default(null),
    doc_thread_id: z.string().nullable().catch(null).default(null),
    doc_thread_quote: z.string().nullable().catch(null).default(null),
    contributors: z
      .array(z.object({ type: actor, id: z.string() }))
      .catch([])
      .default([]),
    created_at: z.string(),
    updated_at: z.string(),
    content: z.string().optional(),
  })
  .transform((d) => ({
    docId: d.doc_id,
    number: d.number,
    title: d.title,
    authorType: d.author_type,
    authorId: d.author_id,
    message: d.message,
    action: d.action,
    actionRef: d.action_ref,
    taskId: d.task_id,
    issueId: d.issue_id,
    docThreadId: d.doc_thread_id,
    docThreadQuote: d.doc_thread_quote,
    contributors: d.contributors,
    createdAt: d.created_at,
    updatedAt: d.updated_at,
    content: d.content,
  }));
export const DocVersionListSchema = z.object({
  versions: z.array(DocVersionSchema),
});
export const DocCollabTicketSchema = z
  .object({
    enabled: z.boolean(),
    token: z.string().optional(),
    document: z.string().optional(),
    expires_at: z.string().optional(),
    state: z.string().optional(),
  })
  .transform((t) =>
    t.enabled && t.token && t.document
      ? {
          enabled: true as const,
          token: t.token,
          document: t.document,
          expiresAt: t.expires_at ?? "",
          ...(t.state ? { state: t.state } : {}),
        }
      : { enabled: false as const },
  );
