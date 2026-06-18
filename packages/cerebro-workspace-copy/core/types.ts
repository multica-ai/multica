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
  | "autopilot"
  | "skill";

// Name-clash policy for uniquely-named entities (agent, skill). Mirrors the
// backend conflict policy in conflict.go. "skip" reuses the existing target row,
// "overwrite" updates it from the source, "keep_both" creates a suffixed copy.
export type ConflictPolicy = "skip" | "overwrite" | "keep_both";

// Entity types whose copy can hit a name clash and therefore honour a policy.
export const CONFLICT_CAPABLE: ReadonlySet<WorkspaceCopyEntityType> = new Set([
  "agent",
  "skill",
]);

// The fixed per-item merge order shown in the console after the foundation:
// agents, projects, issues/channels/dms, chats, autopilots. Each copy resolves
// against what the earlier steps already carried (skill bindings, project
// leads, parent/project links, …).
export const COPY_ORDER: readonly WorkspaceCopyEntityType[] = [
  "agent",
  "project",
  "issue",
  "channel",
  "dm",
  "chat",
  "autopilot",
];

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
  // Conflict resolves a name clash for entity types in CONFLICT_CAPABLE.
  conflict?: ConflictPolicy;
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
  wakeups_copied: z.number().optional(),
  reminders_copied: z.number().optional(),
  already_copied: z.boolean().optional(),
  cascade_copied: z.number().optional(),
});
export type CopyResult = z.infer<typeof CopyResultSchema>;

// Mirrors workspacecopy.FoundationResult (copy_foundation.go) — the one-time
// bulk pass that must run first.
export const FoundationResultSchema = z.object({
  labels_copied: z.number().optional(),
  skills_copied: z.number().optional(),
  skill_versions_copied: z.number().optional(),
  skill_files_copied: z.number().optional(),
  connections_copied: z.number().optional(),
  credentials_copied: z.number().optional(),
  roles_copied: z.number().optional(),
  groups_copied: z.number().optional(),
  role_assignments_copied: z.number().optional(),
  grants_copied: z.number().optional(),
  github_installations_copied: z.number().optional(),
  settings_rows_copied: z.number().optional(),
});
export type FoundationResult = z.infer<typeof FoundationResultSchema>;

// Mirrors workspacecopy.relinkResponse (RelinkResult + RewriteResult, handler.go).
// The relink post-pass also rewrites internal references (mention-link UUIDs +
// identifier tokens) to the copies in issues, comments, agent instructions,
// skill bodies, autopilot descriptions and chat messages.
export const RelinkResultSchema = z.object({
  parents_relinked: z.number().optional(),
  projects_relinked: z.number().optional(),
  issues_rewritten: z.number().optional(),
  comments_rewritten: z.number().optional(),
  agents_rewritten: z.number().optional(),
  skills_rewritten: z.number().optional(),
  autopilots_rewritten: z.number().optional(),
  chat_messages_rewritten: z.number().optional(),
  // TECH-3766: file blobs physically copied into the target so the source can be deleted.
  artifact_files_materialized: z.number().optional(),
  attachments_materialized: z.number().optional(),
  files_skipped: z.number().optional(),
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
