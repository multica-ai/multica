import { z } from "zod";

// The entity kinds the backend copy engine accepts (handler.go dispatch). The
// special "relink" kind is a target-only post-pass run after issue copies and
// is not an entity the per-item action exposes directly.
export type WorkspaceCopyEntityType =
  | "issue"
  | "channel"
  | "dm"
  | "agent"
  | "project"
  | "chat"
  | "autopilot";

// Issue-shaped copies (issue/channel/dm) carry parent + project links that the
// backend only fully heals once both ends exist in the target, via a "relink"
// post-pass. The mutation fires that pass automatically after these copies.
export const ISSUE_SHAPED: ReadonlySet<WorkspaceCopyEntityType> = new Set([
  "issue",
  "channel",
  "dm",
]);

export interface CopyToWorkspaceInput {
  targetWorkspaceId: string;
  entityType: WorkspaceCopyEntityType;
  sourceId: string;
  // Cascade copies everything underneath the picked root: for an issue, all
  // open descendant sub-issues; for a project, all its open issues. Only
  // meaningful for entity types in CASCADE_CAPABLE.
  cascade?: boolean;
}

// Entity types that support a cascade ("take everything underneath") copy.
export const CASCADE_CAPABLE: ReadonlySet<WorkspaceCopyEntityType> = new Set([
  "issue",
  "project",
]);

// Mirrors workspacecopy.CopyResult (server/internal/cerebro/workspacecopy/store.go).
// Every count is optional because the backend omits zero values (omitempty).
export const CopyResultSchema = z.object({
  entity_type: z.string(),
  source_id: z.string(),
  target_id: z.string(),
  source_number: z.number().optional(),
  target_number: z.number().optional(),
  comments_copied: z.number().optional(),
  reactions_copied: z.number().optional(),
  labels_copied: z.number().optional(),
  attachments_copied: z.number().optional(),
  already_copied: z.boolean().optional(),
  cascade_copied: z.number().optional(),
});
export type CopyResult = z.infer<typeof CopyResultSchema>;

// Mirrors workspacecopy.relinkResponse (RelinkResult + RewriteResult, handler.go).
// The relink post-pass also rewrites internal references (mention-link UUIDs +
// identifier tokens) to the copies, so the response carries both link counts and
// rewrite counts.
export const RelinkResultSchema = z.object({
  parents_relinked: z.number().optional(),
  projects_relinked: z.number().optional(),
  issues_rewritten: z.number().optional(),
  comments_rewritten: z.number().optional(),
});
export type RelinkResult = z.infer<typeof RelinkResultSchema>;

export const EMPTY_COPY_RESULT = (
  entityType: string,
  sourceId: string,
): CopyResult => ({
  entity_type: entityType,
  source_id: sourceId,
  target_id: "",
});
