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
    createdAt: d.created_at,
    updatedAt: d.updated_at,
    content: d.content,
  }));
export const DocVersionListSchema = z.object({
  versions: z.array(DocVersionSchema),
});
