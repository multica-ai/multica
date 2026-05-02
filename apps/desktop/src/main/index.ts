import { app, BrowserWindow, ipcMain, nativeImage, Notification } from "electron";
import { homedir } from "os";
import { join } from "path";
import { electronApp, optimizer, is } from "@electron-toolkit/utils";
import fixPath from "fix-path";
import { setupAutoUpdater } from "./updater";
import { setupDaemonManager } from "./daemon-manager";
import { openExternalSafely } from "./external-url";
import { installContextMenu } from "./context-menu";
import { getAppVersion } from "./app-version";
import { loadRuntimeConfig } from "./runtime-config-loader";
import type { RuntimeConfigResult } from "../shared/runtime-config";

// Bundled icon used for dev-mode dock/taskbar branding. In production the
// app bundle icon (from electron-builder) wins; this path is only consumed
// by the `is.dev` branch below.
const DEV_ICON_PATH = join(__dirname, "../../resources/icon.png");

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
let runtimeConfigResult: RuntimeConfigResult = {
  ok: false,
  error: { message: "Runtime config has not loaded yet" },
};

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
      return;
    }

    // multica://invite/<invitationId>
    // Dispatched from the web invite page when the user chooses "Open in
    // desktop app". The renderer opens the invite overlay — no tab, no
    // route persistence, so deep-linking the same invite twice stays safe.
    if (parsed.hostname === "invite") {
      const id = parsed.pathname.replace(/^\//, "");
      if (id && mainWindow) {
        mainWindow.webContents.send("invite:open", decodeURIComponent(id));
      }
      return;
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
    // Windows/Linux pick up the window/taskbar icon from this option in
    // dev — on macOS it's ignored (dock comes from app.dock.setIcon below).
    ...(is.dev ? { icon: DEV_ICON_PATH } : {}),
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
    openExternalSafely(details.url);
    return { action: "deny" };
  });

  // Prevent Cmd+R / Ctrl+R / Shift+Cmd+R / Shift+Ctrl+R / F5 from
  // reloading the page. In a desktop app an accidental reload destroys
  // in-memory state (tabs, drafts, WS connections) with no URL bar to
  // navigate back. DevTools refresh (via the DevTools UI) still works.
  mainWindow.webContents.on("before-input-event", (_event, input) => {
    if (input.type !== "keyDown") return;
    const cmdOrCtrl =
      process.platform === "darwin" ? input.meta : input.control;
    if (
      (cmdOrCtrl && input.key.toLowerCase() === "r") ||
      input.key === "F5"
    ) {
      _event.preventDefault();
    }
  });

  installContextMenu(mainWindow.webContents);

  if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
    mainWindow.loadURL(process.env["ELECTRON_RENDERER_URL"]);
  } else {
    mainWindow.loadFile(join(__dirname, "../renderer/index.html"));
  }
}

// --- Dev / production isolation -------------------------------------------
// Give dev mode a separate app name and userData path so it gets its own
// single-instance lock file and doesn't conflict with the packaged production
// app. Must run BEFORE requestSingleInstanceLock() because the lock location
// is derived from the userData path. (Same approach VS Code uses for
// Stable / Insiders coexistence.)

// DESKTOP_APP_SUFFIX lets parallel worktrees run dev Electron side-by-side
// without fighting for the shared single-instance lock. The suffix is
// appended to the app name + userData path, so each worktree gets its own
// lock file. Default (no env var) keeps behavior unchanged — the common
// single-worktree case still lands at "Multica Canary".
const DEV_APP_NAME = process.env.DESKTOP_APP_SUFFIX
  ? `Multica Canary ${process.env.DESKTOP_APP_SUFFIX}`
  : "Multica Canary";

if (is.dev) {
  app.setName(DEV_APP_NAME);
  app.setPath("userData", join(app.getPath("appData"), DEV_APP_NAME));
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

  app.whenReady().then(async () => {
    runtimeConfigResult = await loadRuntimeConfig({
      isDev: is.dev,
      env: {
        apiUrl: process.env.VITE_API_URL,
        wsUrl: process.env.VITE_WS_URL,
        appUrl: process.env.VITE_APP_URL,
      },
    });

    electronApp.setAppUserModelId(
      is.dev ? "ai.multica.desktop.dev" : "ai.multica.desktop",
    );

    // macOS: replace the default Electron dock icon with the bundled logo
    // so the Canary dev build is visually distinct from a stock Electron
    // run. `app.dock` is macOS-only — guard the call.
    if (is.dev && process.platform === "darwin" && app.dock) {
      const icon = nativeImage.createFromPath(DEV_ICON_PATH);
      if (!icon.isEmpty()) app.dock.setIcon(icon);
    }

    app.on("browser-window-created", (_, window) => {
      optimizer.watchWindowShortcuts(window);
    });

    // IPC: open URL in default browser (used by renderer for Google login).
    // All scheme-allowlist enforcement lives in openExternalSafely — this
    // is the single audit point for renderer-controlled URLs reaching the
    // OS shell under the app's intentional webSecurity: false + sandbox:
    // false configuration.
    ipcMain.handle("shell:openExternal", (_event, url: string) => {
      return openExternalSafely(url);
    });

    // Sync IPC: app version + normalized OS for preload. Sync (not invoke) so
    // preload can attach the values to `desktopAPI.appInfo` before any renderer
    // code reads them, ensuring the very first HTTP request from the renderer
    // already carries X-Client-Version and X-Client-OS.
    ipcMain.on("app:get-info", (event) => {
      const p = process.platform;
      const os = p === "darwin" ? "macos" : p === "win32" ? "windows" : p === "linux" ? "linux" : "unknown";
      event.returnValue = { version: getAppVersion(), os };
    });

    // Sync IPC: preload exposes the validated runtime config before renderer
    // boot. If desktop.json exists but is invalid, renderer receives the
    // blocking error and must not silently fall back to the cloud defaults.
    ipcMain.on("runtime-config:get", (event) => {
      event.returnValue = runtimeConfigResult;
    });

    // IPC: toggle immersive mode — hides the macOS traffic lights so full-screen
    // modals (e.g. create-workspace) can place UI in the top-left corner
    // without fighting the native window controls' hit-test.
    ipcMain.handle("window:setImmersive", (_event, immersive: boolean) => {
      if (process.platform !== "darwin") return;
      mainWindow?.setWindowButtonVisibility(!immersive);
    });

    // IPC: show a native OS notification for a new inbox item. The renderer
    // only fires this when the app is unfocused (it gates on
    // `document.hasFocus()`), so we don't fight macOS foreground suppression
    // here. Clicking the banner focuses the main window and routes to the
    // inbox item via a renderer-side listener.
    ipcMain.on(
      "notification:show",
      (
        _event,
        {
          slug,
          itemId,
          issueKey,
          title,
          body,
        }: {
          slug: string;
          itemId: string;
          issueKey: string;
          title: string;
          body: string;
        },
      ) => {
        if (!Notification.isSupported()) return;
        const notification = new Notification({ title, body });
        notification.on("click", () => {
          if (!mainWindow) return;
          if (mainWindow.isMinimized()) mainWindow.restore();
          mainWindow.show();
          mainWindow.focus();
          // Ship the full context back — the renderer pins the route to the
          // source workspace (slug), marks the row read (itemId), and uses
          // issueKey as the ?issue=<…> selector.
          mainWindow.webContents.send("inbox:open", {
            slug,
            itemId,
            issueKey,
          });
        });
        notification.show();
      },
    );

    // IPC: update the dock / taskbar unread badge. Values above 99 render as
    // "99+". macOS is the primary target (user-visible dock badge); Linux
    // Unity launchers also respect `setBadgeCount`. Windows' taskbar overlay
    // needs a pre-rendered PNG and is deferred — the OS notification + the
    // in-app inbox sidebar cover the core UX there for now.
    ipcMain.on("badge:set", (_event, rawCount: number) => {
      const count = Math.max(0, Math.floor(rawCount));
      if (process.platform === "darwin") {
        const label = count === 0 ? "" : count > 99 ? "99+" : String(count);
        app.dock?.setBadge(label);
      } else {
        app.setBadgeCount(count);
      }
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
