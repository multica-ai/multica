import type { SearchDict } from "./types";

export function createZhTwDict(): SearchDict {
  return {
    trigger: {
      label: "搜尋…",
    },
    dialog: {
      title: "搜尋",
      description: "搜尋頁面、Issue 與專案",
      inputPlaceholder: "輸入指令或搜尋 Multica…",
      noResults: "找不到結果。",
      emptyHint: "輸入文字以搜尋 Issue 與專案",
    },
    groups: {
      pages: "頁面",
      commands: "指令",
      switchWorkspace: "切換工作區",
      projects: "專案",
      issues: "Issue",
      recent: "最近",
    },
    navPages: {
      inbox: "收件匣",
      myIssues: "我的 Issue",
      issues: "Issue",
      projects: "專案",
      agents: "Agent",
      runtimes: "Runtime",
      skills: "Skill",
      settings: "設定",
    },
    commands: {
      newIssue: "新增 Issue",
      newProject: "新增專案",
      copyIssueLink: "複製 Issue 連結",
      copyIdentifier: (identifier) => `複製識別碼（${identifier}）`,
      switchToLight: "切換為淺色主題",
      switchToDark: "切換為深色主題",
      useSystem: "使用系統主題",
      currentTheme: "目前主題",
      linkCopied: "已複製連結",
      copiedIdentifier: (identifier) => `已複製 ${identifier}`,
    },
  };
}
