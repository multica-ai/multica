import type { AuthDict } from "./types";

export function createZhTwDict(): AuthDict {
  return {
    loginPage: {
      title: "登入 Multica",
      description: "輸入電子郵件以取得登入驗證碼",
      emailLabel: "電子郵件",
      emailPlaceholder: "you@example.com",
      continue: "繼續",
      sendingCode: "正在發送驗證碼...",
      orDivider: "或",
      continueWithGoogle: "使用 Google 繼續",
      emailRequired: "請輸入電子郵件",
      sendCodeFailed: "發送驗證碼失敗，請確認伺服器正在執行。",
    },
    codePage: {
      title: "請查看你的電子郵件",
      description: "我們已將驗證碼寄送至",
      invalidCode: "驗證碼無效或已過期",
      resendCode: "重新發送驗證碼",
      resendIn: (seconds) => `${seconds} 秒後可重新發送`,
      resendFailed: "重新發送驗證碼失敗",
      back: "上一步",
    },
    cliConfirm: {
      title: "授權 CLI",
      descriptionPrefix: "允許 CLI 以 ",
      descriptionSuffix: " 的身分存取 Multica？",
      authorize: "授權",
      authorizing: "授權中...",
      useDifferentAccount: "使用其他帳號",
      authorizeFailed: "授權 CLI 失敗，請重新登入。",
    },
  };
}
