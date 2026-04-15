import { app, shell, BrowserWindow, ipcMain } from "electron";
import { homedir } from "os";
import { join } from "path";
import { electronApp, optimizer, is } from "@electron-toolkit/utils";
import fixPath from "fix-path";
import { setupAutoUpdater } from "./updater";
import { setupDaemonManager } from "./daemon-manager";

// macOS/Linux GUI launches inherit a minimal PATH from launchd that omits
// the user's shell config (~/.zshrc, Homebrew, nvm, ~/.local/bin, etc.).
// Run the user's login shell once to recover the real PATH so the bundled
// multica CLI can find agent binaries like claude/codex/opencode. Must run
// before any child_process.spawn / execFile call in the main process —
// ES module imports are hoisted, so this block executes before createWindow
// or any daemon-manager spawn.
if (process.platform !== "win32") {
  fixPath();
  // Fallback: prepend common install locations in case fix-path came up
  // short (broken shell rc, non-interactive $SHELL, missing entries). Safe
  // to duplicate — PATH lookups short-circuit on first match.
  const fallbackPaths = [
    "/opt/homebrew/bin",
    "/usr/local/bin",
    join(homedir(), ".local/bin"),
  ];
  process.env.PATH = `${fallbackPaths.join(":")}:${process.env.PATH ?? ""}`;
}

const PROTOCOL = "multica";

let mainWindow: BrowserWindow | null = null;

// --- Deep link helpers ---------------------------------------------------

function handleDeepLink(url: string): void {
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== `${PROTOCOL}:`) return;

    // multica://auth/callback?token=<jwt>
    if (parsed.hostname === "auth" && parsed.pathname === "/callback") {
      const token = parsed.searchParams.get("token");
      if (token && mainWindow) {
        mainWindow.webContents.send("auth:token", token);
      }
    }
  } catch {
    // Ignore malformed URLs
  }
}

// --- Window creation -----------------------------------------------------

function createWindow(): void {
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 900,
    minHeight: 600,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 16, y: 13 },
    show: false,
    autoHideMenuBar: true,
    webPreferences: {
      preload: join(__dirname, "../preload/index.js"),
      sandbox: false,
      webSecurity: false,
    },
  });

  // Strip Origin header from WebSocket upgrade requests so the server's
  // origin whitelist doesn't reject connections from localhost dev origins.
  mainWindow.webContents.session.webRequest.onBeforeSendHeaders(
    { urls: ["wss://*/*", "ws://*/*"] },
    (details, callback) => {
      delete details.requestHeaders["Origin"];
      callback({ requestHeaders: details.requestHeaders });
    },
  );

  mainWindow.on("ready-to-show", () => {
    mainWindow?.show();
  });

  mainWindow.webContents.setWindowOpenHandler((details) => {
    shell.openExternal(details.url);
    return { action: "deny" };
  });

  if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
    mainWindow.loadURL(process.env["ELECTRON_RENDERER_URL"]);
  } else {
    mainWindow.loadFile(join(__dirname, "../renderer/index.html"));
  }
}

// --- Protocol registration -----------------------------------------------

if (process.defaultApp) {
  // In dev, register with the path to the electron binary + app path
  app.setAsDefaultProtocolClient(PROTOCOL, process.execPath, [
    app.getAppPath(),
  ]);
} else {
  app.setAsDefaultProtocolClient(PROTOCOL);
}

// --- Single instance lock ------------------------------------------------

const gotTheLock = app.requestSingleInstanceLock();

if (!gotTheLock) {
  app.quit();
} else {
  // Windows/Linux: second instance passes deep link via argv
  app.on("second-instance", (_event, argv) => {
    if (mainWindow) {
      if (mainWindow.isMinimized()) mainWindow.restore();
      mainWindow.focus();
    }

    // On Windows the deep link URL is the last argv entry
    const deepLinkUrl = argv.find((arg) => arg.startsWith(`${PROTOCOL}://`));
    if (deepLinkUrl) handleDeepLink(deepLinkUrl);
  });

  app.whenReady().then(() => {
    electronApp.setAppUserModelId("ai.multica.desktop");

    app.on("browser-window-created", (_, window) => {
      optimizer.watchWindowShortcuts(window);
    });

    // IPC: open URL in default browser (used by renderer for Google login)
    ipcMain.handle("shell:openExternal", (_event, url: string) => {
      return shell.openExternal(url);
    });

    // IPC: toggle immersive mode — hides the macOS traffic lights so full-screen
    // modals (create-workspace, onboarding) can place UI in the top-left corner
    // without fighting the native window controls' hit-test.
    ipcMain.handle("window:setImmersive", (_event, immersive: boolean) => {
      if (process.platform !== "darwin") return;
      mainWindow?.setWindowButtonVisibility(!immersive);
    });

    createWindow();

    setupAutoUpdater(() => mainWindow);
    setupDaemonManager(() => mainWindow);

    // macOS: deep link arrives via open-url event
    app.on("open-url", (_event, url) => {
      if (mainWindow) {
        if (mainWindow.isMinimized()) mainWindow.restore();
        mainWindow.focus();
      }
      handleDeepLink(url);
    });

    app.on("activate", () => {
      if (BrowserWindow.getAllWindows().length === 0) createWindow();
    });
  });

  // Check argv for deep link on cold start (Windows/Linux)
  const deepLinkArg = process.argv.find((arg) =>
    arg.startsWith(`${PROTOCOL}://`),
  );
  if (deepLinkArg) {
    app.whenReady().then(() => handleDeepLink(deepLinkArg));
  }
}

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});
