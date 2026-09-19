// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import type { Issue, IssueTableGroupDescriptor } from "@multica/core/types";
import type { IssueGroupBranches } from "../surface/use-issue-group-branches";
import { workflowLaneBranches } from "./workflow-lanes";
import { buildWorkflowStatusGroups } from "./workflow-status-groups";

const lane: IssueTableGroupDescriptor = {
  key: "workflow:design", value: { kind: "workflow", workflow_id: "design", name: "Design" }, count: 51,
  secondary_groups: [
    { key: "opaque-a", value: { kind: "workflow_status", workflow_id: "design", workflow_status_id: "review", status: "in_review", name: "Review", position: 2 }, count: 51 },
    { key: "opaque-b", value: { kind: "workflow_status", workflow_id: "design", workflow_status_id: "draft", status: "todo", name: "Draft", position: 1 }, count: 0 },
  ],
};
const loadMore = vi.fn();
const branches: IssueGroupBranches = {
  enabled: true, descriptors: [lane], total: 53, issues: [
    { id: "one", project_id: "old-project", workflow_id: "design", workflow_status_id: "review" },
    { id: "two", project_id: "other-repo", workflow_id: "design", workflow_status_id: "draft" },
    { id: "three", project_id: "old-project", workflow_id: "engineering", workflow_status_id: "eng-review" },
  ] as Issue[],
  pagination: { "opaque-a": { total: 51, loaded: 1, hasMore: true, isLoading: false, isFetching: false, isError: false, loadMore, retry: vi.fn() } },
  isLoading: false, isRefreshing: false, isError: false, hasMoreGroups: true, isLoadingMoreGroups: false, loadMoreGroups: vi.fn(), retryGroups: vi.fn(),
};

describe("workflow lanes", () => {
  it("uses pinned workflow identity across repositories, preserving exact server counts and opaque cursors", () => {
    const result = workflowLaneBranches(branches, lane, []);
    expect(result.issues.map((issue) => issue.id)).toEqual(["one", "two"]);
    expect(result.total).toBe(51);
    expect(result.descriptors.map((group) => group.key)).toEqual(["workflow_status:review", "workflow_status:draft"]);
    result.pagination["workflow_status:review"]!.loadMore();
    expect(loadMore).toHaveBeenCalledOnce();
    expect(result.hasMoreGroups).toBe(false);
    const columns = buildWorkflowStatusGroups([], result.descriptors, true);
    expect(columns.map((column) => [column.title, column.totalCount])).toEqual([["Draft", 0], ["Review", 51]]);
    expect(columns[0]?.createData).toEqual({ workflow_status_id: "draft" });
  });
  it("retains exact-node and legacy saved filters without changing their meaning", () => {
    expect(workflowLaneBranches(branches, lane, ["review"]).descriptors).toHaveLength(1);
    expect(workflowLaneBranches(branches, lane, ["in_review"]).descriptors).toHaveLength(1);
    const empty = { ...lane, count: 0, secondary_groups: lane.secondary_groups!.map((cell) => ({ ...cell, count: 0 })) };
    expect(workflowLaneBranches(branches, empty, ["eng-review"]).descriptors).toHaveLength(0);
  });
  it("retains server-matched custom columns for legacy saved filters without a legacy node alias", () => {
    const custom = { ...lane, secondary_groups: [{ ...lane.secondary_groups![0]!, value: { ...lane.secondary_groups![0]!.value, status: "" } }] } as IssueTableGroupDescriptor;
    expect(workflowLaneBranches(branches, custom, ["in_progress"]).descriptors).toHaveLength(1);
  });
  it("keeps archived occupied columns readable without a create action", () => {
    const archived = { ...lane.secondary_groups![0]!, value: { ...lane.secondary_groups![0]!.value, archived: true } } as IssueTableGroupDescriptor;
    const columns = buildWorkflowStatusGroups([], [archived], true);
    expect(columns[0]?.workflowStatusArchived).toBe(true);
    expect(columns[0]?.createData).toBeUndefined();
  });
});
