export type ProjectsDict = {
  page: {
    title: string;
    newProject: string;
    emptyTitle: string;
    emptyAction: string;
    columnName: string;
    columnPriority: string;
    columnStatus: string;
    columnProgress: string;
    columnLead: string;
    columnCreated: string;
    today: string;
    dayAgo: string;
    daysAgo: (days: number) => string;
    monthsAgo: (months: number) => string;
  };
  detail: {
    notFound: string;
    breadcrumbFallback: string;
    titlePlaceholder: string;
    changeIcon: string;
    properties: string;
    progress: string;
    description: string;
    descriptionPlaceholder: string;
    status: string;
    priority: string;
    lead: string;
    noLead: string;
    pinToSidebar: string;
    unpinFromSidebar: string;
    toggleSidebar: string;
    copyLink: string;
    linkCopied: string;
    deleteProject: string;
    deleteTitle: string;
    deleteDescription: string;
    deleteCancel: string;
    deleteConfirm: string;
    deleteSuccess: string;
    issuesEmptyTitle: string;
    issuesEmptyHint: string;
    moveIssueFailed: string;
  };
  picker: {
    noProject: string;
    noProjectsYet: string;
    removeFromProject: string;
  };
  chip: {
    fallback: string;
  };
  leadPopover: {
    placeholder: string;
    noLead: string;
    membersHeading: string;
    agentsHeading: string;
    noResults: string;
  };
};
