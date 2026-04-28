import type { WorkspaceDict } from "./types";

export function createZhTwDict(): WorkspaceDict {
  return {
    createForm: {
      nameLabel: "工作區名稱",
      namePlaceholder: "我的工作區",
      urlLabel: "工作區網址",
      slugPlaceholder: "my-workspace",
      submit: "建立工作區",
      submitting: "建立中...",
      slugFormatError: "只能使用小寫字母、數字和連字號",
      slugConflictError: "此工作區網址已被使用。",
      chooseDifferentSlug: "請選擇不同的工作區網址",
      createFailed: "建立工作區失敗",
    },
    noAccess: {
      title: "無法存取此工作區",
      description: "此工作區不存在，或你沒有存取權限。",
      goToWorkspaces: "前往我的工作區",
      signInDifferent: "以其他帳號登入",
    },
    newWorkspace: {
      back: "上一步",
      logOut: "登出",
      title: "歡迎使用 Multica",
      description:
        "一個工作區，讓你和你的 AI 隊友並肩協作 — 接 issue、留言討論、共享相同的脈絡。",
      inviteHint: "工作區建立完成後，你就可以邀請隊友加入。",
    },
  };
}
