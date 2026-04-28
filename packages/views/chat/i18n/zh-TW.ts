import type { ChatDict } from "./types";

export function createZhTwDict(): ChatDict {
  return {
    page: {
      activeTitleFallback: "新對話",
      newChat: "新對話",
      newChatAria: "新對話",
      history: "歷史紀錄",
      historyAria: "歷史紀錄",
      noPreviousChats: "尚無對話紀錄",
      unknownAgent: "未知 Agent",
    },
    agentPicker: {
      noAgents: "沒有可用的 Agent",
      myAgents: "我的 Agent",
      others: "其他",
    },
    emptyState: {
      welcome: "歡迎使用 Multica",
      greeting: (agentName) => `嗨,我是 ${agentName}`,
      helpPrompt: "有什麼我可以幫忙的嗎?",
      starterListTasks: "依優先順序列出我未完成的任務",
      starterSummarize: "總結我今天做了什麼",
      starterPlanNext: "規劃接下來要做什麼",
    },
    input: {
      archivedPlaceholder: "此對話已封存",
      placeholderForAgent: (agentName) => `告訴 ${agentName} 要做什麼…`,
      placeholderDefault: "告訴我要做什麼…",
    },
    messages: {
      toolCount: (count) => `${count} 個工具`,
      resultPrefix: (tool) => `${tool} 結果:`,
      resultPrefixEmpty: "結果:",
      truncatedSuffix: "\n…(已截斷)",
    },
    contextAnchor: {
      nothingToShare: "此頁面沒有可分享給 Multica 的內容",
      knowsViewingIssue: (label) =>
        `Multica 已知道你正在檢視 ${label} · 點擊以關閉`,
      knowsViewingProject: (label) =>
        `Multica 已知道你正在檢視專案「${label}」· 點擊以關閉`,
      letMulticaKnow: "讓 Multica 知道你正在看什麼",
      knowsViewingIssueWithSubtitle: (label, subtitle) =>
        `Multica 已知道你正在檢視 ${label} — ${subtitle}`,
      knowsViewingIssueShort: (label) =>
        `Multica 已知道你正在檢視 ${label}`,
      knowsViewingProjectShort: (label) =>
        `Multica 已知道你正在檢視專案「${label}」`,
      stopSharingAria: "停止分享目前頁面",
      shareAria: "與 Multica 分享目前頁面",
    },
  };
}
