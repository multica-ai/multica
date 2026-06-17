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
}

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
});
export type CopyResult = z.infer<typeof CopyResultSchema>;

// Mirrors workspacecopy.RelinkResult (copy_more.go).
export const RelinkResultSchema = z.object({
  parents_relinked: z.number().optional(),
  projects_relinked: z.number().optional(),
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
