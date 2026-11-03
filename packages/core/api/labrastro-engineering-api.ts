import type { z } from "zod";
import type { ApiClient } from "./client";
import { clientFetch, installLabrastroApiMethods } from "./labrastro-api-helpers";
import { parseWithFallback } from "./schema";
import { BaselineDecisionSchema, BaselineDiffSchema, BaselineHistorySchema, BaselineRevisionSchema, EngineeringOverviewSchema, EngineeringUpdatedSchema, type BaselineChapter, type EngineeringStageSchema } from "./labrastro-engineering-schemas";

const headers = (workspaceId: string) => ({ "X-Workspace-ID": workspaceId, "X-Workspace-Slug": "" });
const projectPath = (projectId: string) => `/api/projects/${encodeURIComponent(projectId)}`;
const labrastroEngineeringApi = {
  async getProjectEngineering(this: ApiClient, workspaceId: string, projectId: string) {
    const endpoint = `${projectPath(projectId)}/engineering`;
    const raw = await clientFetch<unknown>(this, endpoint, { headers: headers(workspaceId) });
    return parseWithFallback<z.infer<typeof EngineeringOverviewSchema> | null>(raw, EngineeringOverviewSchema, null, { endpoint });
  },
  async updateProjectEngineering(this: ApiClient, workspaceId: string, projectId: string, data: {
    enabled?: boolean; currentStage?: number; modeSummary?: string; deciderMemberIds?: string[]; stages?: z.input<typeof EngineeringStageSchema>[];
  }) {
    const endpoint = `${projectPath(projectId)}/engineering`;
    const raw = await clientFetch<unknown>(this, endpoint, { method: "PATCH", headers: headers(workspaceId), body: JSON.stringify({
      enabled: data.enabled, current_stage: data.currentStage, mode_summary: data.modeSummary, decider_member_ids: data.deciderMemberIds, stages: data.stages,
    }) });
    return parseWithFallback<z.infer<typeof EngineeringUpdatedSchema> | null>(raw, EngineeringUpdatedSchema, null, { endpoint });
  },
  async proposeProjectBaseline(this: ApiClient, workspaceId: string, projectId: string, data: {
    chapter: BaselineChapter; contentMarkdown: string; changeSummary: string; baseRevisionId: string | null; replacesCandidateId?: string; sourceIssueId?: string;
  }) {
    const endpoint = `${projectPath(projectId)}/baseline/candidates`;
    const raw = await clientFetch<unknown>(this, endpoint, { method: "POST", headers: headers(workspaceId), body: JSON.stringify({
      chapter: data.chapter, content_markdown: data.contentMarkdown, change_summary: data.changeSummary, base_revision_id: data.baseRevisionId, replaces_candidate_id: data.replacesCandidateId, source_issue_id: data.sourceIssueId,
    }) });
    return parseWithFallback<z.infer<typeof BaselineRevisionSchema> | null>(raw, BaselineRevisionSchema, null, { endpoint });
  },
  async decideProjectBaseline(this: ApiClient, workspaceId: string, projectId: string, action: "adopt" | "reject", revisions: { revisionId: string; expectedAdoptedRevisionId: string | null }[]) {
    const endpoint = `${projectPath(projectId)}/baseline/decisions`;
    const raw = await clientFetch<unknown>(this, endpoint, { method: "POST", headers: headers(workspaceId), body: JSON.stringify({
      action, revisions: revisions.map((v) => ({ revision_id: v.revisionId, expected_adopted_revision_id: v.expectedAdoptedRevisionId })),
    }) });
    return parseWithFallback<z.infer<typeof BaselineDecisionSchema> | null>(raw, BaselineDecisionSchema, null, { endpoint });
  },
  async getProjectBaselineHistory(this: ApiClient, workspaceId: string, projectId: string, chapter: BaselineChapter, options: { limit?: number; before?: number } = {}) {
    const query = new URLSearchParams({ chapter });
    if (options.limit !== undefined) query.set("limit", String(options.limit));
    if (options.before !== undefined) query.set("before", String(options.before));
    const endpoint = `${projectPath(projectId)}/baseline/revisions?${query}`;
    const raw = await clientFetch<unknown>(this, endpoint, { headers: headers(workspaceId) });
    return parseWithFallback<z.infer<typeof BaselineHistorySchema> | null>(raw, BaselineHistorySchema, null, { endpoint });
  },
  async diffProjectBaselineHistory(this: ApiClient, workspaceId: string, projectId: string, chapter: BaselineChapter, from: number, to: number) {
    const query = new URLSearchParams({ chapter, diff_from: String(from), diff_to: String(to) });
    const endpoint = `${projectPath(projectId)}/baseline/revisions?${query}`;
    const raw = await clientFetch<unknown>(this, endpoint, { headers: headers(workspaceId) });
    return parseWithFallback<z.infer<typeof BaselineDiffSchema> | null>(raw, BaselineDiffSchema, null, { endpoint });
  },
};
export type LabrastroEngineeringApi = typeof labrastroEngineeringApi;
export function installLabrastroEngineeringApi(target: { prototype: ApiClient }): void {
  installLabrastroApiMethods(target, labrastroEngineeringApi);
}
