import type { LayoutDict } from "./types";

export function createZhTwDict(): LayoutDict {
  return {
    nav: {
      inbox: "收件匣",
      chat: "聊天",
      myIssues: "我的 Issue",
      issues: "Issue",
      projects: "專案",
      autopilots: "自動化",
      agents: "Agent",
      runtimes: "Runtime",
      skills: "Skill",
      settings: "設定",
    },
    groups: {
      pinned: "已釘選",
      workspace: "工作區",
      configure: "設定",
    },
    sidebar: {
      workspacesLabel: "工作區",
      createWorkspace: "建立工作區",
      pendingInvitations: "待處理邀請",
      workspaceFallback: "工作區",
      join: "加入",
      decline: "拒絕",
      logOut: "登出",
      newIssue: "新 Issue",
      unpin: "取消釘選",
    },
    help: {
      triggerLabel: "說明",
      docs: "文件",
      changeLog: "更新紀錄",
      feedback: "意見回饋",
    },
    loader: {
      loadingPrefix: "正在載入 ",
      loadingSuffix: "…",
      loadingWorkspace: "正在載入工作區…",
    },
  };
}
