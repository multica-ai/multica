export type LayoutDict = {
  nav: {
    inbox: string;
    chat: string;
    myIssues: string;
    issues: string;
    projects: string;
    autopilots: string;
    agents: string;
    runtimes: string;
    skills: string;
    settings: string;
  };
  groups: {
    pinned: string;
    workspace: string;
    configure: string;
  };
  sidebar: {
    workspacesLabel: string;
    createWorkspace: string;
    pendingInvitations: string;
    workspaceFallback: string;
    join: string;
    decline: string;
    logOut: string;
    newIssue: string;
    unpin: string;
  };
  help: {
    triggerLabel: string;
    docs: string;
    changeLog: string;
    feedback: string;
  };
  loader: {
    loadingPrefix: string;
    loadingSuffix: string;
    loadingWorkspace: string;
  };
};
