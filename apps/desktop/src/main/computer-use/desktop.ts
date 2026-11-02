import { app, dialog, globalShortcut, Tray, nativeImage, Menu, type BrowserWindow } from "electron";
import { join } from "node:path";
import { ComputerBroker } from "./broker";
import { ComputerChannel } from "./channel";
import { ComputerNative } from "./native";

interface Options {
  endpoint(): Promise<{ base: string; token: string }>;
  currentUser(): string | null | undefined;
  generation(): number;
  window(): BrowserWindow | null;
}

export function createComputerDesktop(options: Options) {
  const root = app.getAppPath().replace("app.asar", "app.asar.unpacked");
  const helper = new ComputerNative(join(root, "resources/native/Multica Computer Use.app/Contents/MacOS/multica-computer-use"));
  let tray: Tray | undefined;
  let stopShortcut = false;
  let control: { conversationId: string; app?: string } | null = null;
  const zh = app.getLocale().toLowerCase().startsWith("zh");
  const text = (en: string, cn: string) => zh ? cn : en;
  const prompt = async (message: string, detail: string, allow: string, signal: AbortSignal) => {
    const window = options.window();
    if (!window || window.isDestroyed()) throw new Error("desktop_unavailable");
    const result = await dialog.showMessageBox(window, { title: "Multica Computer Use", type: "question", message, detail, buttons: [text("Cancel", "取消"), allow], defaultId: 0, cancelId: 0, noLink: true, signal });
    signal.throwIfAborted();
    return result.response === 1;
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
      { label: text("Stop Computer Use", "停止电脑操作"), click: () => broker.revoke() },
    ]));
  };
  const broker = new ComputerBroker({
    ...options,
    native: (action, signal) => helper.call(action, signal),
    confirm: (target, request, signal) => prompt(
      text(`Allow this conversation to use ${target.name}?`, `允许此会话操作 ${target.name}？`),
      text(`Multica can read this app's windows and operate its controls during this turn.\n\nApp: ${target.id}\nConversation: ${request.conversationId}\n\n${stopShortcut ? "Stop from the CU menu or press Control+Option+Escape." : "Stop from the CU menu."}`, `Multica 可在本轮执行中读取此应用的窗口并操作控件。\n\n应用：${target.id}\n会话：${request.conversationId}\n\n${stopShortcut ? "可通过 CU 菜单或 Control+Option+Escape 停止。" : "可通过 CU 菜单停止。"}`),
      text("Allow this turn", "允许本轮执行"), signal),
    permissions: (signal) => prompt(text("Set up Computer Use permissions?", "设置电脑操作权限？"), text("macOS requires Screen Recording and Accessibility for Multica Computer Use. Grant these in System Settings yourself; the agent cannot approve them.", "macOS 需要为 Multica Computer Use 开启屏幕录制和辅助功能权限。请自行在系统设置中授权，智能体不能代为确认。"), text("Request permissions", "请求权限"), signal),
    activity: showActivity,
    stopNative: () => helper.stop(),
    stopped: (request) => channel.stopExecution(request),
  });
  const channel = new ComputerChannel({ endpoint: options.endpoint, handle: (payload, signal) => broker.handle(payload, signal), stop: () => broker.stop() });
  if (process.platform === "darwin") {
    stopShortcut = globalShortcut.register("Control+Alt+Escape", () => broker.revoke());
    void channel.run();
  }
  return { revoke: () => broker.revoke(), close: () => { channel.close(); broker.stop(); globalShortcut.unregister("Control+Alt+Escape"); }, active: () => control };
}
