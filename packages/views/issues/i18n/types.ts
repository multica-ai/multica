export type IssuesDict = {
  page: {
    breadcrumbFallback: string;
    title: string;
    emptyTitle: string;
    emptyHint: string;
    moveFailed: string;
  };
  scope: {
    all: { label: string; description: string };
    members: { label: string; description: string };
    agents: { label: string; description: string };
  };
  toolbar: {
    filterPlaceholder: string;
    filterTooltip: string;
    displayTooltip: string;
    sortAscending: string;
    sortDescending: string;
    boardView: string;
    listView: string;
    viewLabel: string;
    membersLabel: string;
    agentsLabel: string;
    sortFallback: string;
  };
  detail: {
    description: string;
    activity: string;
    properties: string;
    status: string;
    priority: string;
    assignee: string;
    labels: string;
    project: string;
    parent: string;
    subIssues: string;
    created: string;
    updated: string;
  };
};
