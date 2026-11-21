import { BrowserWindow, ipcMain, Menu, MenuItem } from "electron";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import {
  DEV_TOOLBAR_VISIBLE_CHANGED_CHANNEL,
  DEV_TOOLBAR_VISIBLE_GET_CHANNEL,
} from "../shared/dev-toolbar";

export const DEV_TOOLBAR_MENU_LABEL = "Show Dev Toolbar";

export function devToolbarPreferencesPath(userDataPath: string): string {
  return join(userDataPath, "dev-toolbar.json");
}

export function loadDevToolbarVisible(filePath: string): boolean {
  try {
    const parsed = JSON.parse(readFileSync(filePath, "utf-8")) as { visible?: unknown } | null;
    return typeof parsed?.visible === "boolean" ? parsed.visible : true;
  } catch {
    return true;
  }
}

export function saveDevToolbarVisible(filePath: string, visible: boolean): void {
  try {
    mkdirSync(dirname(filePath), { recursive: true });
    writeFileSync(filePath, JSON.stringify({ visible }, null, 2), "utf-8");
  } catch (err) {
    console.warn("[dev-toolbar] could not save the toolbar preference", err);
  }
}

/**
 * Owns whether the opt-in dev toolbar is shown. When the toolbar is built in
 * (`enabled`), View gets a "Show Dev Toolbar" checkbox whose state survives
 * restarts. Register before any window loads: preload reads the value
 * synchronously, and the handler answers `false` when the toolbar is off.
 */
export function installDevToolbarToggle(options: {
  enabled: boolean;
  preferencesPath: string;
}): void {
  let visible = options.enabled && loadDevToolbarVisible(options.preferencesPath);
  ipcMain.on(DEV_TOOLBAR_VISIBLE_GET_CHANNEL, (event) => {
    event.returnValue = visible;
  });
  if (!options.enabled) return;

  // Electron reports roles in lower case at runtime ("viewmenu").
  const menu = Menu.getApplicationMenu();
  const viewMenu = menu?.items.find((item) => item.role?.toLowerCase() === "viewmenu")?.submenu;
  if (!menu || !viewMenu) return;

  viewMenu.append(new MenuItem({ type: "separator" }));
  viewMenu.append(
    new MenuItem({
      label: DEV_TOOLBAR_MENU_LABEL,
      type: "checkbox",
      checked: visible,
      click: (item) => {
        visible = item.checked;
        saveDevToolbarVisible(options.preferencesPath, visible);
        for (const window of BrowserWindow.getAllWindows()) {
          window.webContents.send(DEV_TOOLBAR_VISIBLE_CHANGED_CHANNEL, visible);
        }
      },
    }),
  );
  Menu.setApplicationMenu(menu);
}
