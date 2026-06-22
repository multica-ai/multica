import { describe, expect, it } from "vitest";
import { mergeViewStatePersisted, useIssueViewStore } from "./view-store";
import type { IssueViewState } from "./view-store";

describe("mergeViewStatePersisted", () => {
  const currentDefault: IssueViewState = {
    viewMode: "list",
    grouping: "status",
    statusFilters: [],
    priorityFilters: [],
    assigneeFilters: [],
    includeNoAssignee: false,
    creatorFilters: [],
    projectFilters: [],
    includeNoProject: false,
    labelFilters: [],
    dateFilter: null,
    agentRunningFilter: false,
    showArchived: false,
    topLevelOnly: false,
    sortBy: "created_at",
    sortDirection: "desc",
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
    listCollapsedStatuses: [],
    ganttZoom: "month",
    ganttShowCompleted: false,
    swimlaneGrouping: "assignee",
    swimlaneOrders: {
      parent: [],
      project: [],
      assignee: [],
    },
    collapsedSwimlanes: {
      parent: [],
      project: [],
      assignee: [],
    },
    toggleTopLevelOnly: () => {},
    toggleShowArchived: () => {},
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
    setDateFilter: () => {},
    toggleAgentRunningFilter: () => {},
    hideStatus: () => {},
    showStatus: () => {},
    clearFilters: () => {},
    setSortBy: () => {},
    setSortDirection: () => {},
    toggleCardProperty: () => {},
    toggleListCollapsed: () => {},
    setSwimlaneGrouping: () => {},
    setSwimlaneOrder: () => {},
    toggleSwimlaneCollapsed: () => {},
  };

  it("should merge basic view state correctly", () => {
    const persisted = {
      viewMode: "board",
      topLevelOnly: true,
    };

    const result = mergeViewStatePersisted(persisted, currentDefault);

    expect(result.viewMode).toBe("board");
    expect(result.topLevelOnly).toBe(true);
    expect(result.swimlaneGrouping).toBe("assignee");
  });

  it("should reconcile conflicting state: reset topLevelOnly to false when swimlaneGrouping is parent", () => {
    const persisted = {
      swimlaneGrouping: "parent",
      topLevelOnly: true,
    };

    const result = mergeViewStatePersisted(persisted, currentDefault);

    expect(result.swimlaneGrouping).toBe("parent");
    expect(result.topLevelOnly).toBe(false);
  });

  it("should keep topLevelOnly as true when swimlaneGrouping is not parent", () => {
    const persisted = {
      swimlaneGrouping: "project",
      topLevelOnly: true,
    };

    const result = mergeViewStatePersisted(persisted, currentDefault);

    expect(result.swimlaneGrouping).toBe("project");
    expect(result.topLevelOnly).toBe(true);
  });
});

describe("useIssueViewStore actions", () => {
  it("toggleTopLevelOnly should remain false when swimlaneGrouping is parent", () => {
    useIssueViewStore.setState({
      swimlaneGrouping: "parent",
      topLevelOnly: false,
    });

    useIssueViewStore.getState().toggleTopLevelOnly();

    expect(useIssueViewStore.getState().topLevelOnly).toBe(false);
  });

  it("toggleTopLevelOnly should toggle state normally when swimlaneGrouping is not parent", () => {
    useIssueViewStore.setState({
      swimlaneGrouping: "assignee",
      topLevelOnly: false,
    });

    useIssueViewStore.getState().toggleTopLevelOnly();
    expect(useIssueViewStore.getState().topLevelOnly).toBe(true);

    useIssueViewStore.getState().toggleTopLevelOnly();
    expect(useIssueViewStore.getState().topLevelOnly).toBe(false);
  });
});

