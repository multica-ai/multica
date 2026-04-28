import type { IssuesDict } from "./types";

export function createEnDict(): IssuesDict {
  return {
    page: {
      breadcrumbFallback: "Workspace",
      title: "Issues",
      emptyTitle: "No issues yet",
      emptyHint: "Create an issue to get started.",
      moveFailed: "Failed to move issue",
    },
    scope: {
      all: { label: "All", description: "All issues in this workspace" },
      members: {
        label: "Members",
        description: "Issues assigned to team members",
      },
      agents: { label: "Agents", description: "Issues assigned to AI agents" },
    },
    toolbar: {
      filterPlaceholder: "Filter...",
      filterTooltip: "Filter",
      displayTooltip: "Display settings",
      sortAscending: "Ascending",
      sortDescending: "Descending",
      boardView: "Board view",
      listView: "List view",
      viewLabel: "View",
      membersLabel: "Members",
      agentsLabel: "Agents",
      sortFallback: "Manual",
    },
    detail: {
      description: "Description",
      activity: "Activity",
      properties: "Properties",
      status: "Status",
      priority: "Priority",
      assignee: "Assignee",
      labels: "Labels",
      project: "Project",
      parent: "Parent",
      subIssues: "Sub-issues",
      created: "Created",
      updated: "Updated",
    },
  };
}
