import type { InviteDict } from "./types";

export function createZhTwDict(): InviteDict {
  return {
    shell: {
      back: "上一步",
      logOut: "登出",
    },
    notFound: {
      title: "找不到邀請",
      description: "此邀請可能已過期、被撤銷，或不屬於你的帳號。",
      goToDashboard: "前往主控台",
    },
    accepted: {
      titlePrefix: "你已加入 ",
      titleSuffix: "！",
      redirecting: "正在導向工作區...",
    },
    declined: {
      title: "已拒絕邀請",
      description: "你不會被加入此工作區。",
      goToDashboard: "前往主控台",
    },
    invitation: {
      joinTitlePrefix: "加入 ",
      workspaceFallback: "工作區",
      invitedAsAdminPrefix: "",
      invitedAsAdminSuffix: " 邀請你以管理員身分加入。",
      invitedAsMemberPrefix: "",
      invitedAsMemberSuffix: " 邀請你以成員身分加入。",
      alreadyHandledAccepted: "此邀請已被接受。",
      alreadyHandledDeclined: "此邀請已被拒絕。",
      expired: "此邀請已過期。",
      accept: "接受並加入",
      accepting: "加入中...",
      decline: "拒絕",
      declining: "拒絕中...",
      acceptFailed: "接受邀請失敗",
      declineFailed: "拒絕邀請失敗",
    },
  };
}
