export type ChatDict = {
  page: {
    activeTitleFallback: string;
    newChat: string;
    newChatAria: string;
    history: string;
    historyAria: string;
    noPreviousChats: string;
    unknownAgent: string;
  };
  agentPicker: {
    noAgents: string;
    myAgents: string;
    others: string;
  };
  emptyState: {
    welcome: string;
    greeting: (agentName: string) => string;
    helpPrompt: string;
    starterListTasks: string;
    starterSummarize: string;
    starterPlanNext: string;
  };
  input: {
    archivedPlaceholder: string;
    placeholderForAgent: (agentName: string) => string;
    placeholderDefault: string;
  };
  messages: {
    toolCount: (count: number) => string;
    resultPrefix: (tool: string) => string;
    resultPrefixEmpty: string;
    truncatedSuffix: string;
  };
  contextAnchor: {
    nothingToShare: string;
    knowsViewingIssue: (label: string) => string;
    knowsViewingProject: (label: string) => string;
    letMulticaKnow: string;
    knowsViewingIssueWithSubtitle: (label: string, subtitle: string) => string;
    knowsViewingIssueShort: (label: string) => string;
    knowsViewingProjectShort: (label: string) => string;
    stopSharingAria: string;
    shareAria: string;
  };
};
