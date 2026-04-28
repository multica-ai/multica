import type { ChatDict } from "./types";

export function createEnDict(): ChatDict {
  return {
    page: {
      activeTitleFallback: "New chat",
      newChat: "New chat",
      newChatAria: "New chat",
      history: "History",
      historyAria: "History",
      noPreviousChats: "No previous chats",
      unknownAgent: "Unknown agent",
    },
    agentPicker: {
      noAgents: "No agents",
      myAgents: "My agents",
      others: "Others",
    },
    emptyState: {
      welcome: "Welcome to Multica",
      greeting: (agentName) => `Hi, I'm ${agentName}`,
      helpPrompt: "How can I help?",
      starterListTasks: "List my open tasks by priority",
      starterSummarize: "Summarize what I did today",
      starterPlanNext: "Plan what to work on next",
    },
    input: {
      archivedPlaceholder: "This session is archived",
      placeholderForAgent: (agentName) => `Tell ${agentName} what to do…`,
      placeholderDefault: "Tell me what to do…",
    },
    messages: {
      toolCount: (count) => `${count} ${count === 1 ? "tool" : "tools"}`,
      resultPrefix: (tool) => `${tool} result: `,
      resultPrefixEmpty: "result: ",
      truncatedSuffix: "\n... (truncated)",
    },
    contextAnchor: {
      nothingToShare: "Nothing to share with Multica on this page",
      knowsViewingIssue: (label) =>
        `Multica knows you're viewing ${label} · Click to turn off`,
      knowsViewingProject: (label) =>
        `Multica knows you're viewing project "${label}" · Click to turn off`,
      letMulticaKnow: "Let Multica know what you're viewing",
      knowsViewingIssueWithSubtitle: (label, subtitle) =>
        `Multica knows you're viewing ${label} — ${subtitle}`,
      knowsViewingIssueShort: (label) =>
        `Multica knows you're viewing ${label}`,
      knowsViewingProjectShort: (label) =>
        `Multica knows you're viewing project "${label}"`,
      stopSharingAria: "Stop sharing current page",
      shareAria: "Share current page with Multica",
    },
  };
}
