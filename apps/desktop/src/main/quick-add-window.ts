import { BrowserWindow, globalShortcut, ipcMain, screen } from "electron";
import { join } from "path";
import { is } from "@electron-toolkit/utils";

const QUICK_ADD_SHORTCUT = "CmdOrCtrl+Shift+C";

let quickAddWindow: BrowserWindow | null = null;

function getRendererUrl(slug: string | null): string {
  const base = is.dev && process.env["ELECTRON_RENDERER_URL"]
    ? process.env["ELECTRON_RENDERER_URL"]
    : `file://${join(__dirname, "../renderer/index.html")}`;

  const params = new URLSearchParams({ quickAdd: "1" });
  if (slug) params.set("workspace", slug);

  const separator = base.includes("?") ? "&" : "?";
  return `${base}${separator}${params.toString()}`;
}

function createQuickAddWindow(slug: string | null, mainWin?: BrowserWindow): void {
  if (quickAddWindow && !quickAddWindow.isDestroyed()) {
    quickAddWindow.destroy();
    quickAddWindow = null;
  }

  const targetDisplay = mainWin
    ? screen.getDisplayMatching(mainWin.getBounds())
    : screen.getPrimaryDisplay();
  const { width: screenWidth, height: screenHeight } = targetDisplay.workAreaSize;
  const winWidth = 640;
  const winHeight = 420;

  quickAddWindow = new BrowserWindow({
    width: winWidth,
    height: winHeight,
    x: Math.round((screenWidth - winWidth) / 2),
    y: Math.round((screenHeight - winHeight) / 2),
    frame: false,
    transparent: true,
    resizable: false,
    alwaysOnTop: true,
    show: false,
    skipTaskbar: true,
    webPreferences: {
      preload: join(__dirname, "../preload/index.js"),
      sandbox: false,
      webSecurity: false,
    },
  });

  quickAddWindow.on("closed", () => {
    quickAddWindow = null;
  });

  quickAddWindow.on("ready-to-show", () => {
    quickAddWindow?.show();
  });

  quickAddWindow.on("blur", () => {
    setTimeout(async () => {
      if (!quickAddWindow || quickAddWindow.isDestroyed() || quickAddWindow.isFocused()) return;
      try {
        const busy = await quickAddWindow.webContents.executeJavaScript(
          'window.__quickAddFileDialog === true',
        );
        if (!busy && quickAddWindow && !quickAddWindow.isDestroyed() && !quickAddWindow.isFocused()) {
          quickAddWindow.destroy();
          quickAddWindow = null;
        }
      } catch {
        // webContents may throw if window was destroyed between checks
      }
    }, 150);
  });

  ipcMain.once("quick-add:close", () => {
    if (quickAddWindow && !quickAddWindow.isDestroyed()) {
      quickAddWindow.destroy();
      quickAddWindow = null;
    }
  });

  quickAddWindow.loadURL(getRendererUrl(slug));
}

function requestWorkspaceSlug(mainWin: BrowserWindow): Promise<string | null> {
  return new Promise((resolve) => {
    const handler = (_event: Electron.IpcMainEvent, slug: string | null) => {
      clearTimeout(timeout);
      ipcMain.removeListener("workspace:receiveCurrentSlug", handler);
      resolve(slug);
    };
    ipcMain.on("workspace:receiveCurrentSlug", handler);
    mainWin.webContents.send("workspace:sendCurrentSlug");

    const timeout = setTimeout(() => {
      ipcMain.removeListener("workspace:receiveCurrentSlug", handler);
      resolve(null);
    }, 2000);
  });
}

export function setupQuickAdd(getMainWindow: () => BrowserWindow | null): void {
  ipcMain.on("quick-add:set-size", (_event, width: number, height: number) => {
    if (quickAddWindow && !quickAddWindow.isDestroyed()) {
      const display = screen.getDisplayMatching(quickAddWindow.getBounds());
      const { width: sw, height: sh } = display.workAreaSize;
      // Animate: true — content uses w-full h-full, so only the window
      // animates. The webview naturally reflows; no CSS size transitions
      // means no sync issues between window bounds and content.
      quickAddWindow.setBounds(
        {
          x: Math.round((sw - width) / 2),
          y: Math.round((sh - height) / 2),
          width,
          height,
        },
        true,
      );
    }
  });

  const registered = globalShortcut.register(QUICK_ADD_SHORTCUT, () => {
    const mainWin = getMainWindow();
    if (!mainWin) return;

    requestWorkspaceSlug(mainWin).then((slug) => {
      createQuickAddWindow(slug, mainWin);
    });
  });

  if (!registered) {
    console.warn(`Failed to register global shortcut: ${QUICK_ADD_SHORTCUT}`);
  }
}

export function cleanupQuickAdd(): void {
  globalShortcut.unregister(QUICK_ADD_SHORTCUT);
  ipcMain.removeAllListeners("quick-add:set-size");
  if (quickAddWindow && !quickAddWindow.isDestroyed()) {
    quickAddWindow.destroy();
    quickAddWindow = null;
  }
}
