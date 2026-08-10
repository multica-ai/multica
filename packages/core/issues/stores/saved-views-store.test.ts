import { beforeEach, describe, expect, it } from "vitest";
import type { IssueViewState } from "./view-store";
import {
  useSavedViewsStore,
  selectSavedViews,
  snapshotIssueViewState,
  applySavedViewState,
} from "./saved-views-store";

function makeState(overrides: Partial<IssueViewState> = {}): IssueViewState {
  return {
    viewMode: "board",
    grouping: "status",
    statusFilters: [],
    priorityFilters: [],
    assigneeFilters: [],
    includeNoAssignee: false,
    creatorFilters: [],
    projectFilters: [],
    includeNoProject: false,
    labelFilters: [],
    propertyFilters: {},
    dateFilter: { field: "created_at", from: "2026-08-01", to: "2026-08-09" },
    agentRunningFilter: true,
    sortBy: "position",
    sortDirection: "asc",
    cardProperties: {
      priority: true,
      description: true,
      assignee: true,
      startDate: true,
      dueDate: true,
      project: true,
      childProgress: true,
      labels: true,
    },
    cardPropertyIds: [],
    showSubIssues: true,
    listCollapsedStatuses: [],
    ganttZoom: "week",
    ganttShowCompleted: false,
    swimlaneGrouping: "assignee",
    swimlaneOrders: { parent: [], project: [], assignee: [] },
    collapsedSwimlanes: { parent: [], project: [], assignee: [] },
    tableColumns: [],
    tableGrouping: "none",
    tableCollapsedGroups: [],
    tableCollapsedParents: [],
    tableHierarchy: true,
    tableCalculation: "none",
    // Store methods — not read by snapshotting.
    setViewMode: () => {},
    setGanttZoom: () => {},
    toggleGanttShowCompleted: () => {},
    setGrouping: () => {},
    toggleStatusFilter: () => {},
    togglePriorityFilter: () => {},
    toggleAssigneeFilter: () => {},
    toggleNoAssignee: () => {},
    toggleCreatorFilter: () => {},
    toggleProjectFilter: () => {},
    toggleNoProject: () => {},
    toggleLabelFilter: () => {},
    togglePropertyFilter: () => {},
    setDateFilter: () => {},
    toggleAgentRunningFilter: () => {},
    hideStatus: () => {},
    showStatus: () => {},
    clearFilters: () => {},
    setSortBy: () => {},
    setSortDirection: () => {},
    toggleCardProperty: () => {},
    toggleCardPropertyId: () => {},
    toggleShowSubIssues: () => {},
    toggleListCollapsed: () => {},
    setSwimlaneGrouping: () => {},
    setSwimlaneOrder: () => {},
    toggleSwimlaneCollapsed: () => {},
    toggleTableColumn: () => {},
    reorderTableColumn: () => {},
    setTableColumnWidth: () => {},
    setTableGrouping: () => {},
    toggleTableGroupCollapsed: () => {},
    toggleTableParentCollapsed: () => {},
    toggleTableHierarchy: () => {},
    setTableCalculation: () => {},
    ...overrides,
  };
}

beforeEach(() => {
  useSavedViewsStore.setState({ byWorkspace: {} });
});

describe("useSavedViewsStore.saveView", () => {
  it("snapshots the view state and stores it under the workspace", () => {
    const state = makeState({
      viewMode: "table",
      statusFilters: ["in_progress"],
      sortBy: "due_date",
    });
    const id = useSavedViewsStore.getState().saveView("ws-a", state, "My queue");

    const views = useSavedViewsStore.getState().byWorkspace["ws-a"] ?? [];
    expect(views).toHaveLength(1);
    expect(views[0]).toMatchObject({
      id,
      name: "My queue",
      state: {
        viewMode: "table",
        statusFilters: ["in_progress"],
        sortBy: "due_date",
      },
    });
  });

  it("keeps saved views namespaced by workspace", () => {
    const { saveView } = useSavedViewsStore.getState();
    saveView("ws-a", makeState(), "A");
    saveView("ws-b", makeState(), "B");

    const state = useSavedViewsStore.getState().byWorkspace;
    expect(state["ws-a"]?.map((v) => v.name)).toEqual(["A"]);
    expect(state["ws-b"]?.map((v) => v.name)).toEqual(["B"]);
  });

  it("trims the name and appends later saves to the same workspace", () => {
    const { saveView } = useSavedViewsStore.getState();
    saveView("ws-a", makeState(), "  First  ");
    saveView("ws-a", makeState(), "Second");

    const names = useSavedViewsStore.getState().byWorkspace["ws-a"]?.map((v) => v.name);
    expect(names).toEqual(["First", "Second"]);
  });
});

describe("useSavedViewsStore.renameView", () => {
  it("renames the matching view and trims the input", () => {
    const { saveView, renameView } = useSavedViewsStore.getState();
    const id = saveView("ws-a", makeState(), "Old");

    renameView("ws-a", id, "  New name  ");

    const views = useSavedViewsStore.getState().byWorkspace["ws-a"] ?? [];
    expect(views[0]?.name).toBe("New name");
  });

  it("is a no-op for an unknown id", () => {
    const { saveView, renameView } = useSavedViewsStore.getState();
    saveView("ws-a", makeState(), "Only");
    const before = useSavedViewsStore.getState().byWorkspace;

    renameView("ws-a", "missing", "Renamed");

    expect(useSavedViewsStore.getState().byWorkspace).toBe(before);
  });

  it("is a no-op when the workspace has no views", () => {
    const { renameView } = useSavedViewsStore.getState();
    const before = useSavedViewsStore.getState().byWorkspace;

    renameView("ws-missing", "id", "Renamed");

    expect(useSavedViewsStore.getState().byWorkspace).toBe(before);
  });
});

describe("useSavedViewsStore.deleteView", () => {
  it("removes the matching view", () => {
    const { saveView, deleteView } = useSavedViewsStore.getState();
    saveView("ws-a", makeState(), "Keep");
    const id = saveView("ws-a", makeState(), "Drop");

    deleteView("ws-a", id);

    const names = useSavedViewsStore.getState().byWorkspace["ws-a"]?.map((v) => v.name);
    expect(names).toEqual(["Keep"]);
  });

  it("drops the bucket entirely when the last view is removed", () => {
    const { saveView, deleteView } = useSavedViewsStore.getState();
    const id = saveView("ws-a", makeState(), "Only");
    saveView("ws-b", makeState(), "Other");

    deleteView("ws-a", id);

    const state = useSavedViewsStore.getState().byWorkspace;
    expect(state["ws-a"]).toBeUndefined();
    expect(state["ws-b"]?.map((v) => v.name)).toEqual(["Other"]);
  });

  it("is a no-op for an unknown id or workspace", () => {
    const { saveView, deleteView } = useSavedViewsStore.getState();
    saveView("ws-a", makeState(), "Only");
    const before = useSavedViewsStore.getState().byWorkspace;

    deleteView("ws-a", "missing");
    expect(useSavedViewsStore.getState().byWorkspace).toBe(before);

    deleteView("ws-missing", "id");
    expect(useSavedViewsStore.getState().byWorkspace).toBe(before);
  });
});

describe("snapshotIssueViewState", () => {
  it("captures the persisted fields and drops ephemeral ones", () => {
    const snapshot = snapshotIssueViewState(
      makeState({
        viewMode: "swimlane",
        statusFilters: ["todo", "in_progress"],
        swimlaneGrouping: "parent",
        dateFilter: { field: "created_at", from: "2026-08-01", to: "2026-08-09" },
        agentRunningFilter: true,
      }),
    );

    expect(snapshot.viewMode).toBe("swimlane");
    expect(snapshot.statusFilters).toEqual(["todo", "in_progress"]);
    expect(snapshot.swimlaneGrouping).toBe("parent");
    // Ephemeral by design — never part of a saved view.
    expect(snapshot).not.toHaveProperty("dateFilter");
    expect(snapshot).not.toHaveProperty("agentRunningFilter");
    // Functions never make it into a snapshot.
    expect(snapshot).not.toHaveProperty("setViewMode");
  });
});

describe("applySavedViewState", () => {
  it("returns a partial that can restore a view store", () => {
    const snapshot = snapshotIssueViewState(
      makeState({ viewMode: "table", sortDirection: "desc" }),
    );

    const partial = applySavedViewState(snapshot);
    expect(partial.viewMode).toBe("table");
    expect(partial.sortDirection).toBe("desc");
    // Must be a plain object of data — safe for Zustand setState.
    expect(typeof partial).toBe("object");
    expect(partial.setViewMode).toBeUndefined();
  });
});

describe("selectSavedViews", () => {
  it("returns the views for the given workspace", () => {
    useSavedViewsStore.getState().saveView("ws-a", makeState(), "A");
    const views = selectSavedViews("ws-a")(useSavedViewsStore.getState());
    expect(views.map((v) => v.name)).toEqual(["A"]);
  });

  it("returns a stable empty array when wsId is null or unknown", () => {
    const a = selectSavedViews(null)(useSavedViewsStore.getState());
    const b = selectSavedViews(null)(useSavedViewsStore.getState());
    const c = selectSavedViews("missing")(useSavedViewsStore.getState());
    expect(a).toBe(b);
    expect(a).toBe(c);
    expect(a).toEqual([]);
  });
});
