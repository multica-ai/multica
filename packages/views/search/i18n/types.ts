export type SearchDict = {
  trigger: {
    label: string;
  };
  dialog: {
    title: string;
    description: string;
    inputPlaceholder: string;
    noResults: string;
    emptyHint: string;
  };
  groups: {
    pages: string;
    commands: string;
    switchWorkspace: string;
    projects: string;
    issues: string;
    recent: string;
  };
  navPages: {
    inbox: string;
    myIssues: string;
    issues: string;
    projects: string;
    agents: string;
    runtimes: string;
    skills: string;
    settings: string;
  };
  commands: {
    newIssue: string;
    newProject: string;
    copyIssueLink: string;
    copyIdentifier: (identifier: string) => string;
    switchToLight: string;
    switchToDark: string;
    useSystem: string;
    currentTheme: string;
    linkCopied: string;
    copiedIdentifier: (identifier: string) => string;
  };
};
