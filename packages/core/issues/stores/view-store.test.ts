import { afterEach, describe, expect, it } from "vitest";
import {
  buildActiveViewFilters,
  EMPTY_VIEW_FILTER_FIELDS,
  useIssueViewStore,
  viewFiltersToState,
} from "./view-store";
import type { SavedView } from "../../types";

function makeView(filters: SavedView["filters"], id = "v1"): SavedView {
  return {
    id,
    workspace_id: "ws",
    creator_id: null,
    name: "V",
    page: "issues",
    project_id: null,
    filters,
    display: {},
    position: 0,
    shared: true,
    is_default: false,
    created_at: "",
    updated_at: "",
  };
}

describe("viewFiltersToState", () => {
  it("maps flat API filters into store fields, splitting actor tokens", () => {
    const state = viewFiltersToState({
      statuses: ["todo", "in_progress"],
      priorities: ["high"],
      assignee_filters: ["member:u1", "agent:a1"],
      include_no_assignee: true,
      creator_filters: ["member:u2"],
      project_ids: ["p1"],
      label_ids: ["l1"],
    });
    expect(state.statusFilters).toEqual(["todo", "in_progress"]);
    expect(state.priorityFilters).toEqual(["high"]);
    expect(state.assigneeFilters).toEqual([
      { type: "member", id: "u1" },
      { type: "agent", id: "a1" },
    ]);
    expect(state.includeNoAssignee).toBe(true);
    expect(state.creatorFilters).toEqual([{ type: "member", id: "u2" }]);
    expect(state.projectFilters).toEqual(["p1"]);
    expect(state.labelFilters).toEqual(["l1"]);
  });

  it("drops bare tokens with no actor type (e.g. {my_agents}) from the UI state", () => {
    const state = viewFiltersToState({ assignee_filters: ["{my_agents}", "member:u1"] });
    expect(state.assigneeFilters).toEqual([{ type: "member", id: "u1" }]);
  });

  it("collapses an any_of view to empty filter fields", () => {
    const state = viewFiltersToState({
      any_of: [{ assignee_filters: ["member:{me}"] }, { creator_filters: ["member:{me}"] }],
    });
    expect(state).toEqual(EMPTY_VIEW_FILTER_FIELDS);
  });
});

describe("buildActiveViewFilters", () => {
  it("re-layers a flat view's assignee_types (no filter-bar field for it)", () => {
    const out = buildActiveViewFilters(
      EMPTY_VIEW_FILTER_FIELDS,
      makeView({ assignee_types: ["agent", "squad"] }),
    );
    expect(out.assignee_types).toEqual(["agent", "squad"]);
  });

  it("lets a concrete assignee selection override the class-level assignee_types", () => {
    // assignee_types and assignee_filters are mutually exclusive server-side
    // (sending both is a 400) — a pinned actor must win over the default.
    const out = buildActiveViewFilters(
      { ...EMPTY_VIEW_FILTER_FIELDS, assigneeFilters: [{ type: "member", id: "u1" }] },
      makeView({ assignee_types: ["member"] }),
    );
    expect(out.assignee_filters).toEqual(["member:u1"]);
    expect(out.assignee_types).toBeUndefined();
  });

  it("re-layers any_of and ANDs filter-bar edits onto the result", () => {
    const out = buildActiveViewFilters(
      { ...EMPTY_VIEW_FILTER_FIELDS, statusFilters: ["todo"] },
      makeView({ any_of: [{ assignee_filters: ["member:{me}"] }] }),
    );
    expect(out.any_of).toEqual([{ assignee_filters: ["member:{me}"] }]);
    expect(out.statuses).toEqual(["todo"]);
  });

  it("returns only the reconstructed flat fields when no view is active", () => {
    const out = buildActiveViewFilters(
      { ...EMPTY_VIEW_FILTER_FIELDS, priorityFilters: ["high"] },
      undefined,
    );
    expect(out).toEqual({ priorities: ["high"] });
  });
});

describe("useIssueViewStore.setActiveView", () => {
  afterEach(() => {
    useIssueViewStore.getState().setActiveView(null);
  });

  it("pins the view id and replaces filter fields in one shot", () => {
    useIssueViewStore.getState().setActiveView(
      makeView({ statuses: ["done"], priorities: ["urgent"] }),
    );
    const s = useIssueViewStore.getState();
    expect(s.currentViewId).toBe("v1");
    expect(s.statusFilters).toEqual(["done"]);
    expect(s.priorityFilters).toEqual(["urgent"]);
  });

  it("replaces (not merges) prior filter fields when switching views", () => {
    useIssueViewStore.getState().setActiveView(
      makeView({ statuses: ["done"], priorities: ["urgent"] }, "a"),
    );
    useIssueViewStore.getState().setActiveView(
      makeView({ statuses: ["todo"] }, "b"),
    );
    const s = useIssueViewStore.getState();
    expect(s.currentViewId).toBe("b");
    expect(s.statusFilters).toEqual(["todo"]);
    // Priority from the previous view must be gone — replace, not merge.
    expect(s.priorityFilters).toEqual([]);
  });

  it("clears the active view and filter fields when passed null", () => {
    useIssueViewStore.getState().setActiveView(makeView({ statuses: ["done"] }));
    useIssueViewStore.getState().setActiveView(null);
    const s = useIssueViewStore.getState();
    expect(s.currentViewId).toBeNull();
    expect(s.statusFilters).toEqual([]);
  });
});
