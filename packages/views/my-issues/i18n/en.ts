import type { MyIssuesDict } from "./types";

export function createEnDict(): MyIssuesDict {
  return {
    page: {
      breadcrumbFallback: "Workspace",
      title: "My Issues",
      emptyTitle: "No issues assigned to you",
      emptyHint: "Issues you create or are assigned to will appear here.",
      moveFailed: "Failed to move issue",
    },
    scopes: {
      assigned: { label: "Assigned", description: "Issues assigned to me" },
      created: { label: "Created", description: "Issues I created" },
      agents: { label: "My Agents", description: "Issues assigned to my agents" },
    },
    toolbar: {
      filter: "Filter",
      displaySettings: "Display settings",
      boardView: "Board view",
      listView: "List view",
      viewLabel: "View",
      boardOption: "Board",
      listOption: "List",
      sortAscending: "Ascending",
      sortDescending: "Descending",
      sortFallback: "Manual",
      ordering: "Ordering",
      cardProperties: "Card properties",
      statusLabel: "Status",
      priorityLabel: "Priority",
      issueCount: (count) => `${count} ${count === 1 ? "issue" : "issues"}`,
      resetFilters: "Reset all filters",
    },
  };
}
