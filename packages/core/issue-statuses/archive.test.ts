// @vitest-environment node
import { describe, expect, it } from "vitest";
import { ApiError } from "../api/client";
import { issueStatusArchiveConflictCount, prepareIssueStatusList } from "./archive";
import { getIssueSurfaceViewStore } from "../issues/stores/surface-view-store";
import { useActiveIssueViewStore } from "../issue-views/active-view-store";
import { useIssuesScopeStore } from "../issues/stores/issues-scope-store";

describe("archive precondition", () => {
  it("parses only a valid, structured in-use conflict", () => {
    expect(issueStatusArchiveConflictCount(new ApiError("move first", 409, "Conflict", { code: "issue_status_in_use", issue_count: 12 }))).toBe(12);
    for (const body of [null, {}, { code: "other", issue_count: 12 }, { code: "issue_status_in_use", issue_count: "12" }, { code: "issue_status_in_use", issue_count: -1 }, { code: "issue_status_in_use", issue_count: 0 }]) {
      expect(issueStatusArchiveConflictCount(new ApiError("old server", 409, "Conflict", body))).toBeNull();
    }
    expect(issueStatusArchiveConflictCount(new Error("offline"))).toBeNull();
  });

  it("shows all matching work, including sub-issues and terminals, without changing a saved view", () => {
    const store = getIssueSurfaceViewStore("workspace:all");
    store.setState({ statusFilters: ["todo"], priorityFilters: ["urgent"], projectFilters: ["p"], dateFilter: { field: "created_at", preset: "today" } as never,
      hiddenStatuses: ["shipped"], listCollapsedStatuses: ["shipped"], showSubIssues: false, agentRunningFilter: true });
    const saved = getIssueSurfaceViewStore("view:v1");
    saved.setState({ statusFilters: ["backlog"] });
    useActiveIssueViewStore.getState().setActive("ws:workspace", "v1");
    useActiveIssueViewStore.getState().setActive("other:workspace", "v2");
    useIssuesScopeStore.getState().setScope("issues", "agents");
    prepareIssueStatusList("ws", "shipped");
    expect(store.getState()).toMatchObject({ statusFilters: ["shipped"], priorityFilters: [], projectFilters: [], dateFilter: null,
      hiddenStatuses: [], listCollapsedStatuses: [], showSubIssues: true, agentRunningFilter: false, viewMode: "list" });
    expect(useActiveIssueViewStore.getState().active["ws:workspace"]).toBeUndefined();
    expect(useActiveIssueViewStore.getState().active["other:workspace"]).toBe("v2");
    expect(useIssuesScopeStore.getState().scopes.issues).toBe("all");
    expect(saved.getState().statusFilters).toEqual(["backlog"]);
  });
});
