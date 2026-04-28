export type MyIssuesDict = {
  page: {
    breadcrumbFallback: string;
    title: string;
    emptyTitle: string;
    emptyHint: string;
    moveFailed: string;
  };
  scopes: {
    assigned: { label: string; description: string };
    created: { label: string; description: string };
    agents: { label: string; description: string };
  };
  toolbar: {
    filter: string;
    displaySettings: string;
    boardView: string;
    listView: string;
    viewLabel: string;
    boardOption: string;
    listOption: string;
    sortAscending: string;
    sortDescending: string;
    sortFallback: string;
    ordering: string;
    cardProperties: string;
    statusLabel: string;
    priorityLabel: string;
    issueCount: (count: number) => string;
    resetFilters: string;
  };
};
