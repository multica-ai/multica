import { app, dialog, globalShortcut, Tray, nativeImage, Menu, type BrowserWindow } from "electron";
import { join } from "node:path";
import { computerPrompts } from "@multica/computer-use/prompts";
import type { ComputerUseApprovals } from "../../shared/computer-use-approvals";
import { AppApprovals, isVerificationApp } from "./approvals";
import { ComputerBroker } from "./broker";
import { ComputerChannel, type ComputerClientState } from "./channel";
import { ComputerNative } from "./native";
import { COMPUTER_CAPABILITY_VERSION } from "./version";

interface Options {
  endpoint(): Promise<{ base: string; token: string }>;
  currentUser(): string | null | undefined;
  generation(): number;
  window(): BrowserWindow | null;
}

export function createComputerDesktop(options: Options) {
  // Packaged builds ship the staged SDK as an extra resource (see electron-builder.yml).
  const cuaDriver = app.isPackaged ? join(process.resourcesPath, "native/cua-driver") : join(app.getAppPath(), "resources/native/cua-driver");
  // The daemon runs this installation's sandboxed REPL runtime, whichever
  // daemon version is connected.
  const runtime = {
    executable: process.execPath,
    entry: app.isPackaged
      ? join(app.getAppPath().replace("app.asar", "app.asar.unpacked"), "resources/local-conversation-host/computer-host.cjs")
      : join(app.getAppPath(), "out/main/computer-host.cjs"),
  };
  const describe = (): ComputerClientState => ({
    user: { id: options.currentUser() ?? null },
    capabilities: { computer: { version: COMPUTER_CAPABILITY_VERSION, prompts: { ...computerPrompts }, runtime } },
  });
  const helper = new ComputerNative({ executable: process.execPath, args: [join(__dirname, "computer-native.js"), join(cuaDriver, "node_modules/@trycua/cua-driver/dist/index.js")] });
  const approvals = new AppApprovals(join(app.getPath("userData"), "computer-use-approvals.json"));
  let tray: Tray | undefined;
  let stopShortcut = false;
  let control: { conversationId: string; app?: string } | null = null;
  const zh = app.getLocale().toLowerCase().startsWith("zh");
  const text = (en: string, cn: string) => zh ? cn : en;
  const prompt = async (message: string, detail: string, allow: string, signal: AbortSignal, checkboxLabel?: string) => {
    const window = options.window();
    if (!window || window.isDestroyed()) throw new Error("desktop_unavailable");
    const result = await dialog.showMessageBox(window, { title: "Multica Computer Use", type: "question", message, detail, buttons: [text("Cancel", "取消"), allow], defaultId: 0, cancelId: 0, noLink: true, signal, ...(checkboxLabel ? { checkboxLabel } : {}) });
    signal.throwIfAborted();
    return { allowed: result.response === 1, checked: result.checkboxChecked };
  };
  const showActivity = (value: typeof control) => {
    control = value;
    if (!value) { tray?.destroy(); tray = undefined; return; }
    if (!tray) {
      const icon = nativeImage.createFromDataURL("data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAYAAAAf8/9hAAAAJ0lEQVR4nGNgGEzgP4kYqwGkWDZqwKA1gKJ0gM9QsgHZBlDkbPoCAOaOL9HV2NB4AAAAAElFTkSuQmCC");
      icon.setTemplateImage(true);
      tray = new Tray(icon);
      tray.setTitle("CU");
    }
    tray.setToolTip(text("Multica Computer Use", "Multica 电脑操作"));
    tray.setContextMenu(Menu.buildFromTemplate([
      { label: value.app ?? text("Computer Use active", "正在操作电脑"), enabled: false },
      { label: value.conversationId, enabled: false },
      { label: text("Stop Computer Use", "停止电脑操作"), click: () => broker.revoke() },    ]));
  };
  const broker = new ComputerBroker({
    ...options,
    native: (action, signal) => helper.call(action, signal),
    confirm: async (target, request, signal) => {
      const user = request.scope.userId;
      if (isVerificationApp(target.id) || approvals.has(user, target.id)) return true;
      const { allowed, checked } = await prompt(
        text(`Allow this conversation to use ${target.name}?`, `允许此会话操作 ${target.name}？`),
        text(`Multica can read this app's windows and operate its controls during this turn.\n\nApp: ${target.id}\nConversation: ${request.conversationId}\n\n${stopShortcut ? "Stop from the CU menu or press Control+Option+Escape." : "Stop from the CU menu."}`, `Multica 可在本轮执行中读取此应用的窗口并操作控件。\n\n应用：${target.id}\n会话：${request.conversationId}\n\n${stopShortcut ? "可通过 CU 菜单或 Control+Option+Escape 停止。" : "可通过 CU 菜单停止。"}`),
        text("Allow this turn", "允许本轮执行"), signal,
        text("Don't ask again for this app (change in Settings)", "不再询问此应用（可在设置中更改）"));
      if (allowed && checked) approvals.add(user, target);
      return allowed;
    },
    permissions: async (signal) => (await prompt(text("Set up Computer Use permissions?", "设置电脑操作权限？"), text("macOS requires Screen Recording and Accessibility for Multica. Grant these in System Settings yourself; the agent cannot approve them.", "macOS 需要为 Multica 开启屏幕录制和辅助功能权限。请自行在系统设置中授权，智能体不能代为确认。"), text("Request permissions", "请求权限"), signal)).allowed,
    activity: showActivity,
    stopNative: () => helper.stop(),
    stopped: (request) => channel.stopExecution(request),
  });
  const channel = new ComputerChannel({
    endpoint: options.endpoint, clientInfo: { name: "multica-desktop", version: app.getVersion() }, describe,
    handle: (payload, signal) => broker.handle(payload, signal), stop: () => broker.stop(), log: (message) => console.warn(message),
  });
  if (process.platform === "darwin") {
    stopShortcut = globalShortcut.register("Control+Alt+Escape", () => broker.revoke());
    void channel.run();
  }
  // Settings manage the signed-in account's "don't ask again" apps.
  const approvalsForCurrentUser = (change?: (user: string) => void): ComputerUseApprovals => {
    const user = options.currentUser();
    if (!user) return { available: process.platform === "darwin", apps: [] };
    change?.(user);
    return { available: process.platform === "darwin", apps: approvals.list(user) };
  };
  return {
    approvals: {
      list: () => approvalsForCurrentUser(),
      remove: (appId: string) => approvalsForCurrentUser((user) => approvals.remove(user, appId)),
      clear: () => approvalsForCurrentUser((user) => approvals.clear(user)),
    },
    revoke: () => broker.revoke(), refresh: () => channel.update(), close: () => { channel.close(); broker.stop(); globalShortcut.unregister("Control+Alt+Escape"); }, active: () => control };
}
