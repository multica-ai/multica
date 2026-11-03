// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import { BaselineDecisionSchema, BaselineDiffSchema, BaselineHistorySchema, BaselineRevisionSchema, EngineeringOverviewSchema, EngineeringUpdatedSchema } from "./labrastro-engineering-schemas";

const id = "00000000-0000-4000-8000-000000000001";
const diff = { chapter: "domain_rules", base_revision_number: 0, added: [], modified: [], removed: [], unchanged_count: 0, warnings: [] };
const revision = { id, workspace_id: id, project_id: id, chapter: "domain_rules", revision_number: 1, status: "candidate", content_markdown: "### [Rule-一] Text", change_summary: "Add", base_revision_id: null, anchors_diff: diff, created_by_type: "agent", created_by_id: id, created_at: "2026-10-30T00:00:00Z" };

describe("engineering API response contracts", () => {
  it("defaults the capability to disabled without inventing a baseline", () => {
    const parsed = EngineeringOverviewSchema.parse({ engineering: { project_id: id, workspace_id: id }, chapters: [] });
    expect(parsed.engineering.enabled).toBe(false);
    expect(parsed.engineering).not.toHaveProperty("agentDecisionsEnabled");
    expect(parsed.engineering.currentStage).toBe(1);
    expect(parsed.engineering.stages).toEqual([]);
    expect(parsed.chapters).toEqual([]);
  });
  it("keeps audit identity and unknown enums explicit", () => {
    const parsed = BaselineRevisionSchema.parse({ ...revision, status: "future_status", decision_credential: "future_credential", decided_by_type: "agent", decided_by_id: id, decided_by_task_id: id });
    expect(parsed.status).toBe("unknown");
    expect(parsed.decisionCredential).toBe("unknown");
    expect(parsed.decidedById).toBe(id);
    expect(parsed.decidedByTaskId).toBe(id);
    expect(parsed.contentMarkdown).toContain("Rule-一");
    expect(BaselineDiffSchema.parse({ ...diff, warnings: [{ code: "future_warning", anchor: "Rule-一" }] }).warnings[0]?.code).toBe("unknown");
  });
  it.each(["session", "pat", "agent_task", "task"])("preserves %s decision attribution", (credential) => {
    const parsed = BaselineRevisionSchema.parse({ ...revision, decision_credential: credential, decided_by_type: "agent", decided_by_id: id, decided_by_task_id: id });
    expect(parsed.decisionCredential).toBe(credential === "task" ? "agent_task" : credential);
    expect(parsed.decidedById).toBe(id);
    expect(parsed.decidedByTaskId).toBe(id);
  });
  it("parses decisions and historical pagination", () => {
    const adopted = { ...revision, status: "adopted" };
    const parsed = BaselineDecisionSchema.parse({ chapters: [{ chapter: "domain_rules", adopted, candidate: null }], open_issues: { in_flight: [], queued: [{ id, identifier: "OL-1", title: "Next", status: "backlog_2", status_category: "unstarted", assignee_type: null, assignee_id: null }], truncated: true } });
    expect(parsed.openIssues.queued[0]?.statusCategory).toBe("unstarted");
    expect(parsed.openIssues.truncated).toBe(true);
    expect(BaselineHistorySchema.parse({ revisions: [revision], has_more: true, next_before: 1 }).nextBefore).toBe(1);
    expect(EngineeringUpdatedSchema.parse({ engineering: { project_id: id, workspace_id: id, enabled: true } }).engineering.enabled).toBe(true);
  });
  it("rejects malformed payloads instead of approving or hiding changes", () => {
    expect(parseWithFallback({ ...revision, anchors_diff: "not a diff" }, BaselineRevisionSchema, null, { endpoint: "engineering test" })).toBeNull();
    expect(parseWithFallback({ engineering: { project_id: id, workspace_id: id, enabled: "true" }, chapters: [] }, EngineeringOverviewSchema, null, { endpoint: "engineering test" })).toBeNull();
    expect(parseWithFallback({ ...diff, removed: null }, BaselineDiffSchema, null, { endpoint: "engineering test" })).toBeNull();
    expect(parseWithFallback({ chapters: [], open_issues: null }, BaselineDecisionSchema, null, { endpoint: "engineering test" })).toBeNull();
    expect(parseWithFallback({ revisions: [revision], has_more: false, next_before: "bad" }, BaselineHistorySchema, null, { endpoint: "engineering test" })).toBeNull();
  });
});

it.each([undefined, null, "W10=", {}, [null], [{ stage: 0, title: "Bad" }], [{ stage: 1.5, title: "Bad" }], [{ stage: 1, title: 42 }], [{ stage: 1, title: "Bad", goal: null }]])("isolates malformed stages: %j", (stages) => {
  const parsed = EngineeringOverviewSchema.parse({ engineering: { project_id: id, workspace_id: id, stages, current_stage: 9 }, chapters: [] });
  expect(parsed.engineering.stages).toEqual([]);
  expect(parsed.engineering.currentStage).toBe(9);
});
it("parses dynamic stages and defaults an omitted goal", () => {
  const parsed = EngineeringUpdatedSchema.parse({ engineering: { project_id: id, workspace_id: id, stages: [{ stage: 9, title: "Release" }] } });
  expect(parsed.engineering.stages).toEqual([{ stage: 9, title: "Release", goal: "" }]);
});
