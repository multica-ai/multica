import { app, Menu, type MenuItemConstructorOptions } from "electron";

export function prefersTraditionalChinese(languages: readonly string[]): boolean {
  const preferred = languages[0]?.toLowerCase() ?? "";
  return /^zh-(?:hant|tw|hk|mo)(?:-|$)/.test(preferred);
}

/**
 * Electron's generated macOS menu stays in English even when the renderer
 * resolves to zh-Hant. Install a native Traditional Chinese template for
 * Traditional Chinese system preferences; roles retain the standard macOS
 * shortcuts and behavior.
 */
export function installApplicationMenu(): void {
  if (process.platform !== "darwin") return;
  if (!prefersTraditionalChinese(app.getPreferredSystemLanguages())) return;

  const appName = app.name;
  const template: MenuItemConstructorOptions[] = [
    {
      label: appName,
      submenu: [
        { role: "about", label: `關於 ${appName}` },
        { type: "separator" },
        { role: "services", label: "服務" },
        { type: "separator" },
        { role: "hide", label: `隱藏 ${appName}` },
        { role: "hideOthers", label: "隱藏其他項目" },
        { role: "unhide", label: "全部顯示" },
        { type: "separator" },
        { role: "quit", label: `結束 ${appName}` },
      ],
    },
    {
      label: "檔案",
      submenu: [{ role: "close", label: "關閉視窗" }],
    },
    {
      label: "編輯",
      submenu: [
        { role: "undo", label: "還原" },
        { role: "redo", label: "重做" },
        { type: "separator" },
        { role: "cut", label: "剪下" },
        { role: "copy", label: "複製" },
        { role: "paste", label: "貼上" },
        { role: "pasteAndMatchStyle", label: "貼上並符合樣式" },
        { role: "delete", label: "刪除" },
        { role: "selectAll", label: "全選" },
      ],
    },
    {
      label: "顯示方式",
      submenu: [
        { role: "reload", label: "重新載入" },
        { role: "forceReload", label: "強制重新載入" },
        { role: "toggleDevTools", label: "切換開發人員工具" },
        { type: "separator" },
        { role: "resetZoom", label: "實際大小" },
        { role: "zoomIn", label: "放大" },
        { role: "zoomOut", label: "縮小" },
        { type: "separator" },
        { role: "togglefullscreen", label: "切換全螢幕" },
      ],
    },
    {
      label: "視窗",
      submenu: [
        { role: "minimize", label: "縮到最小" },
        { role: "zoom", label: "縮放" },
        { type: "separator" },
        { role: "front", label: "將全部移至最前方" },
      ],
    },
    {
      role: "help",
      label: "輔助說明",
      submenu: [{ label: "Multica 輔助說明", enabled: false }],
    },
  ];

  Menu.setApplicationMenu(Menu.buildFromTemplate(template));
}
