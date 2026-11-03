import { z } from "zod";

const id = z.string().uuid();
const actor = z.enum(["member", "agent", "unknown"]).catch("unknown");
export const BaselineChapterSchema = z.enum(["overview", "domain_rules", "architecture", "exclusions_and_risks"]);
export type BaselineChapter = z.infer<typeof BaselineChapterSchema>;
const ChangeSchema = z.object({
  anchor: z.string(), title: z.string(), before: z.string().default(""), after: z.string().default(""),
  before_chars: z.number().int().nonnegative().default(0), after_chars: z.number().int().nonnegative().default(0),
}).transform((v) => ({ anchor: v.anchor, title: v.title, before: v.before, after: v.after, beforeChars: v.before_chars, afterChars: v.after_chars }));
export const BaselineDiffSchema = z.object({
  chapter: BaselineChapterSchema,
  base_revision_number: z.number().int().nonnegative(),
  added: z.array(ChangeSchema), modified: z.array(ChangeSchema), removed: z.array(ChangeSchema),
  unchanged_count: z.number().int().nonnegative(),
  warnings: z.array(z.object({
    code: z.enum(["rule_removed", "rule_shrunk", "rule_deferred", "defer_missing_pool", "unknown"]).catch("unknown"),
    anchor: z.string(),
  })),
}).transform((v) => ({ chapter: v.chapter, baseRevisionNumber: v.base_revision_number, added: v.added, modified: v.modified, removed: v.removed, unchangedCount: v.unchanged_count, warnings: v.warnings }));

export const EngineeringStageSchema = z.object({
  stage: z.number().int().positive(), title: z.string(), goal: z.string().default(""),
});
export type EngineeringStage = z.infer<typeof EngineeringStageSchema>;

export const EngineeringConfigSchema = z.object({
  project_id: id, workspace_id: id, enabled: z.boolean().default(false),
  current_stage: z.number().int().positive().default(1), mode_summary: z.string().default(""),
  decider_member_ids: z.array(id).default([]), stages: z.array(EngineeringStageSchema).catch([]),
}).transform((v) => ({ projectId: v.project_id, workspaceId: v.workspace_id, enabled: v.enabled, currentStage: v.current_stage, modeSummary: v.mode_summary, deciderMemberIds: v.decider_member_ids, stages: v.stages }));

export const BaselineRevisionSchema = z.object({
  id, workspace_id: id, project_id: id, chapter: BaselineChapterSchema,
  revision_number: z.number().int().positive(),
  status: z.enum(["candidate", "adopted", "superseded", "withdrawn", "rejected", "unknown"]).catch("unknown"),
  content_markdown: z.string(), change_summary: z.string(),
  base_revision_id: id.nullable(), replaces_revision_id: id.nullable().default(null), source_issue_id: id.nullable().default(null),
  anchors_diff: BaselineDiffSchema,
  created_by_type: actor, created_by_id: id, created_by_task_id: id.nullable().default(null), created_at: z.string(),
  decided_by_type: actor.nullable().default(null), decided_by_id: id.nullable().default(null), decided_by_task_id: id.nullable().default(null),
  decided_at: z.string().nullable().default(null),
  decision_credential: z.enum(["session", "pat", "agent_task", "task", "unknown"]).catch("unknown").nullable().default(null),
}).transform((v) => ({
  id: v.id, workspaceId: v.workspace_id, projectId: v.project_id, chapter: v.chapter, revisionNumber: v.revision_number, status: v.status,
  contentMarkdown: v.content_markdown, changeSummary: v.change_summary, baseRevisionId: v.base_revision_id, replacesRevisionId: v.replaces_revision_id,
  sourceIssueId: v.source_issue_id, anchorsDiff: v.anchors_diff, createdByType: v.created_by_type, createdById: v.created_by_id, createdByTaskId: v.created_by_task_id,
  createdAt: v.created_at, decidedByType: v.decided_by_type, decidedById: v.decided_by_id, decidedByTaskId: v.decided_by_task_id,
  decidedAt: v.decided_at,
  // Normalize the pre-9046 API value at the boundary for older servers.
  decisionCredential: v.decision_credential === "task" ? "agent_task" : v.decision_credential,
}));
const ChapterStateSchema = z.object({ chapter: BaselineChapterSchema, adopted: BaselineRevisionSchema.nullable(), candidate: BaselineRevisionSchema.nullable() });
export const EngineeringOverviewSchema = z.object({ engineering: EngineeringConfigSchema, chapters: z.array(ChapterStateSchema) });
export const EngineeringUpdatedSchema = z.object({ engineering: EngineeringConfigSchema });
const OpenIssueSchema = z.object({
  id, identifier: z.string(), title: z.string(), status: z.string(), status_category: z.enum(["started", "unstarted", "unknown"]).catch("unknown"),
  assignee_type: actor.nullable(), assignee_id: id.nullable(),
}).transform((v) => ({ id: v.id, identifier: v.identifier, title: v.title, status: v.status, statusCategory: v.status_category, assigneeType: v.assignee_type, assigneeId: v.assignee_id }));
export const BaselineDecisionSchema = z.object({
  chapters: z.array(ChapterStateSchema),
  open_issues: z.object({ in_flight: z.array(OpenIssueSchema), queued: z.array(OpenIssueSchema), truncated: z.boolean() }),
}).transform((v) => ({ chapters: v.chapters, openIssues: { inFlight: v.open_issues.in_flight, queued: v.open_issues.queued, truncated: v.open_issues.truncated } }));
export const BaselineHistorySchema = z.object({ revisions: z.array(BaselineRevisionSchema), has_more: z.boolean(), next_before: z.number().int().positive().nullable() })
  .transform((v) => ({ revisions: v.revisions, hasMore: v.has_more, nextBefore: v.next_before }));

export type EngineeringConfig = z.infer<typeof EngineeringConfigSchema>;
export type EngineeringOverview = z.infer<typeof EngineeringOverviewSchema>;
export type BaselineRevision = z.infer<typeof BaselineRevisionSchema>;
export type BaselineDiff = z.infer<typeof BaselineDiffSchema>;
export type BaselineDecision = z.infer<typeof BaselineDecisionSchema>;
export type BaselineHistory = z.infer<typeof BaselineHistorySchema>;
