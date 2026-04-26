import { BrowserWindow, Menu, MenuItem, type WebContents } from "electron";

// Electron ships with no default right-click menu, so a user selecting text
// in the renderer has no way to copy it. Mirror Chrome's minimal clipboard
// menu using `roles`, which keeps i18n + accelerator handling native.
export function installContextMenu(webContents: WebContents): void {
  webContents.on("context-menu", (_event, params) => {
    const { editFlags, selectionText, isEditable } = params;
    const hasSelection = selectionText.trim().length > 0;

    const menu = new Menu();

    if (isEditable && editFlags.canCut) {
      menu.append(new MenuItem({ role: "cut" }));
    }
    if (hasSelection && editFlags.canCopy) {
      menu.append(new MenuItem({ role: "copy" }));
    }
    if (isEditable && editFlags.canPaste) {
      menu.append(new MenuItem({ role: "paste" }));
    }
    if (isEditable && editFlags.canSelectAll) {
      if (menu.items.length > 0) {
        menu.append(new MenuItem({ type: "separator" }));
      }
      menu.append(new MenuItem({ role: "selectAll" }));
    }

    if (menu.items.length === 0) return;
    const window = BrowserWindow.fromWebContents(webContents) ?? undefined;
    menu.popup({ window });
  });
}
