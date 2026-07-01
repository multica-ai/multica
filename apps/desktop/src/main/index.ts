import { app, BrowserWindow, dialog, ipcMain, nativeImage, Notification } from "electron";
import { homedir } from "os";
import { join } from "path";
import { electronApp, optimizer, is } from "@electron-toolkit/utils";
import fixPath from "fix-path";
import { setupAutoUpdater } from "./updater";
import { setupDaemonManager } from "./daemon-manager";
import { setupLocalDirectory } from "./local-directory";
import { openExternalSafely, downloadURLSafely } from "./external-url";
import { installContextMenu } from "./context-menu";
import { handleAppShortcut } from "./keyboard-shortcuts";
import { installNavigationGestures } from "./navigation-gestures";
import { getAppVersion } from "./app-version";
import { loadRuntimeConfig } from "./runtime-config-loader";
import type { RuntimeConfigResult } from "../shared/runtime-config";
import {
  RENDERER_ROUTE_CONTEXT_CHANNEL,
  sanitizeRendererRouteContext,
  type RendererRouteContext,
} from "../shared/renderer-route-context";
import {
  createElectronReloadPrompt,
  installRendererRecoveryHandlers,
  type RendererRecoveryWindow,
} from "./renderer-recovery";
import {
  writeFreezeBreadcrumb,
  readAndClearFreezeBreadcrumb,
  clearFreezeBreadcrumb,
} from "./freeze-breadcrumb";

// Guards against registering the will-download handler more than once on the
// same session. window.webContents.session is shared, and createWindow() can
// be called again on macOS (app "activate" after all windows are closed).
const downloadDialogSessions = new WeakSet<Electron.Session>();

function installDownloadSaveDialogHandler(window: BrowserWindow): void {
  const { session } = window.webContents;
  if (downloadDialogSessions.has(session)) return;
  downloadDialogSessions.add(session);
  session.on("will-download", (_event, item) => {
    item.setSaveDialogOptions({
      defaultPath: join(app.getPath("downloads"), item.getFilename()),
    });
  });
}

// Bundled icon used for dock/taskbar branding. macOS/Windows production
// builds let the OS pick up the icon from the .app bundle / .exe resources,
// but Linux production needs an explicit BrowserWindow `icon` — AppImage
// direct-launch doesn't register the .desktop entry, so GNOME has no path
// from the running window to the hicolor icon and falls back to the
// theme default. Consumed in createWindow() (all platforms in dev, Linux
// in prod) and the macOS dev dock branch.
//
// `asarUnpack: resources/**` in electron-builder.yml extracts the icon to
// `app.asar.unpacked/`, but `__dirname` resolves into `app.asar/`. The
// Linux native window-icon code path expects a real filesystem path
// (unlike Electron's nativeImage loader which transparently reads from
// asar), so swap the segment — same pattern as bundledCliPath() in
// daemon-manager.ts. In dev `__dirname` has no `app.asar`, so the replace
// is a no-op.
const BUNDLED_ICON_PATH = join(__dirname, "../../resources/icon.png").replace(
  "app.asar",
  "app.asar.unpacked",
);

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

// Where the main process parks a freeze/crash breadcrumb until the next
// renderer boot flushes it to telemetry. Lives in userData so it survives a
// force-quit. Resolved lazily — app.getPath is only valid after `ready`.
function freezeBreadcrumbPath(): string {
  return join(app.getPath("userData"), "last-client-failure.json");
}

let mainWindow: BrowserWindow | null = null;
let latestRendererRouteContext: RendererRouteContext | null = null;
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

// Tracks the OS-preferred language as last seen by the running process.
// Updated on each window-focus check so we can emit a `locale:system-changed`
// event to the renderer when the user changes their OS language without
// quitting the app — without restart, app.getPreferredSystemLanguages()
// would still report the boot value forever.
let lastKnownSystemLocale = "en";

function getSystemLocale(): string {
  return app.getPreferredSystemLanguages()[0] ?? "en";
}

function createWindow(): void {
  // Pass the OS-preferred language to the renderer via additionalArguments
  // instead of a sync IPC call. process.argv is available to the preload
  // script before the first network request, so the renderer's i18next
  // instance can initialize with the right locale on the very first paint.
  const systemLocale = getSystemLocale();
  lastKnownSystemLocale = systemLocale;

  mainWindow = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 900,
    minHeight: 600,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 16, y: 17 },
    show: false,
    // Dark background prevents the white flash between reload and first paint.
    // Matches --background in dark mode: oklch(0.18 0.005 285.823) ≈ #111114.
    backgroundColor: "#111114",
    autoHideMenuBar: true,
    // Windows/Linux pick up the window/taskbar icon from this option.
    // On macOS it's ignored (dock comes from app.dock.setIcon below).
    // Linux production needs this explicitly because AppImage direct-launch
    // does not install a .desktop entry, so the WM has no other path to
    // the bundled icon; without it Ubuntu falls back to the theme default.
    ...(is.dev || process.platform === "linux"
      ? { icon: BUNDLED_ICON_PATH }
      : {}),
    webPreferences: {
      preload: join(__dirname, "../preload/index.js"),
      sandbox: false,
      webSecurity: false,
      // Required for the Chromium PDF viewer (PDFium) to activate inside
      // iframes — used by the attachment preview modal for application/pdf
      // files. Default is false in Electron; without it <iframe src=*.pdf>
      // renders blank.
      //
      // Security trade-off, accepted intentionally:
      //   1. This window already runs with `webSecurity: false` + `sandbox: false`,
      //      so `plugins: true` does NOT meaningfully widen the renderer's
      //      attack surface beyond what is already accepted.
      //   2. The only PDFs that reach an iframe here are signed CloudFront URLs
      //      we ourselves issued (see useDownloadAttachment); user-supplied URLs
      //      are routed through `setWindowOpenHandler` → `openExternalSafely` and
      //      cannot land in this renderer.
      //   3. Chromium's PDFium plugin is itself sandboxed inside its own process
      //      and only handles the `application/pdf` MIME — it does not expose
      //      Flash, Java, or other historical plugin surfaces.
      //
      // If we ever tighten `webSecurity` / `sandbox`, revisit this by hosting
      // the PDF viewer in a dedicated BrowserView with `plugins: true` scoped
      // to that view, keeping the main renderer plugin-free.
      plugins: true,
      additionalArguments: [`--multica-locale=${systemLocale}`],
    },
  });
  const window = mainWindow;
  latestRendererRouteContext = null;

  window.on("closed", () => {
    if (mainWindow === window) {
      mainWindow = null;
      latestRendererRouteContext = null;
    }
  });

  // Strip Origin header from WebSocket upgrade requests so the server's
  // origin whitelist doesn't reject connections from localhost dev origins.
  window.webContents.session.webRequest.onBeforeSendHeaders(
    { urls: ["wss://*/*", "ws://*/*"] },
    (details, callback) => {
      delete details.requestHeaders["Origin"];
      callback({ requestHeaders: details.requestHeaders });
    },
  );

  window.on("ready-to-show", () => {
    window.show();
  });

  // Detect OS language changes while the app is running. Electron has no
  // dedicated event for this on any platform, so we poll on focus regain —
  // catches the common case where users switch System Settings → Language
  // and bring the app back. The renderer decides whether to act (it ignores
  // the signal when the user has an explicit Settings choice).
  window.on("focus", () => {
    const current = getSystemLocale();
    if (current === lastKnownSystemLocale) return;
    lastKnownSystemLocale = current;
    window.webContents.send("locale:system-changed", current);
  });

  installDownloadSaveDialogHandler(window);

  window.webContents.setWindowOpenHandler((details) => {
    openExternalSafely(details.url);
    return { action: "deny" };
  });

  // Window-level keyboard shortcuts. Calling preventDefault here prevents
  // both the renderer keydown AND the application menu accelerator, so
  // anything we own here (reload-block, zoom, tab-close) is the sole handler
  // for that combination — no double-fire with the macOS default View menu.
  window.webContents.on("before-input-event", (event, input) => {
    const result = handleAppShortcut(input, window.webContents);
    if (result === "close-tab") {
      event.preventDefault();
      window.webContents.send("tab:close-active");
    } else if (result === "open-settings") {
      event.preventDefault();
      window.webContents.send("navigate:settings");
    } else if (result === "reload") {
      event.preventDefault();
      window.webContents.reload();
    } else if (result === "force-reload") {
      event.preventDefault();
      window.webContents.reloadIgnoringCache();
    } else if (result) {
      event.preventDefault();
    }
  });

  // Dev-mode renderer diagnostics. When the renderer crashes hard enough
  // that DevTools can't be opened (white screen with no clickable surface),
  // the only way to recover the actual JS error is to forward it from the
  // main process to the terminal running `make dev`. Without these, the
  // user sees only the daemon-manager polling noise (`Render frame was
  // disposed before WebFrameMain could be accessed`) which is a downstream
  // symptom, not the cause.
  //
  // Gated by `is.dev` to keep production stderr clean — packaged builds
  // don't have a terminal anyway, and we ship to crash-reporting separately.
  if (is.dev) {
    const log = (tag: string, ...args: unknown[]) =>
      process.stderr.write(`[renderer ${tag}] ${args.map(String).join(" ")}\n`);

    // Forward every renderer-side console.* call. The detail object also
    // carries source URL + line — included so a thrown stack trace from
    // window.onerror is traceable back to a file.
    window.webContents.on("console-message", (details) => {
      const { level, message, sourceId, lineNumber } = details;
      log(level, `${message} (${sourceId}:${lineNumber})`);
    });

    // Fires when loadURL / loadFile can't reach its target (dev server
    // not up yet, network blip, file missing). errorCode is a Chromium
    // net error number; -3 = ABORTED is normal during HMR and skipped.
    window.webContents.on(
      "did-fail-load",
      (_event, errorCode, errorDescription, validatedURL, isMainFrame) => {
        if (errorCode === -3) return;
        log(
          "did-fail-load",
          `code=${errorCode} desc=${errorDescription} url=${validatedURL} mainFrame=${isMainFrame}`,
        );
      },
    );

  }

  installRendererRecoveryHandlers(window as unknown as RendererRecoveryWindow, {
    isDev: is.dev,
    showReloadPrompt: createElectronReloadPrompt((options) =>
      dialog.showMessageBox(window, options),
    ),
    getDiagnosticContext: () => ({
      windowUrl: window.webContents.getURL(),
      ...(latestRendererRouteContext
        ? { desktopRoute: latestRendererRouteContext }
        : {}),
    }),
    // Only persist in production: a true hang/crash can't report itself, so we
    // write a breadcrumb and the next renderer boot flushes it to PostHog. Dev
    // is excluded to keep field telemetry clean.
    persistBreadcrumb: is.dev
      ? undefined
      : (payload) =>
          writeFreezeBreadcrumb(freezeBreadcrumbPath(), {
            kind: payload.kind,
            context: payload.context,
            ts: Date.now(),
            version: getAppVersion(),
          }),
    clearBreadcrumb: is.dev
      ? undefined
      : () => clearFreezeBreadcrumb(freezeBreadcrumbPath()),
  });

  installContextMenu(window.webContents);
  installNavigationGestures(window);

  if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
    window.loadURL(process.env["ELECTRON_RENDERER_URL"]);
  } else {
    window.loadFile(join(__dirname, "../renderer/index.html"));
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
} else {
  // Pin the production app name in code. Electron's Linux WM_CLASS is set
  // from app.getName() when the first BrowserWindow is realized; the
  // packaged ASAR's package.json `productName` already steers app.getName()
  // to "Multica", but anchoring it here makes WM_CLASS ↔ StartupWMClass
  // (declared in electron-builder.yml) survive a regression in
  // productName / the build pipeline. Must run before requestSingleInstanceLock().
  app.setName("Multica");
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
    const viteEnv = import.meta.env as ImportMetaEnv & {
      readonly VITE_API_URL?: string;
      readonly VITE_WS_URL?: string;
      readonly VITE_APP_URL?: string;
    };

    runtimeConfigResult = await loadRuntimeConfig({
      isDev: is.dev,
      // electron-vite exposes VITE_* on import.meta.env for the main process;
      // keep dev URL overrides on the same source the renderer used before
      // runtime config moved endpoint resolution into main/preload.
      env: {
        apiUrl: viteEnv.VITE_API_URL,
        wsUrl: viteEnv.VITE_WS_URL,
        appUrl: viteEnv.VITE_APP_URL,
      },
    });

    electronApp.setAppUserModelId(
      is.dev ? "ai.multica.desktop.dev" : "ai.multica.desktop",
    );

    // macOS: replace the default Electron dock icon with the bundled logo
    // so the Canary dev build is visually distinct from a stock Electron
    // run. `app.dock` is macOS-only — guard the call.
    if (is.dev && process.platform === "darwin" && app.dock) {
      const icon = nativeImage.createFromPath(BUNDLED_ICON_PATH);
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

    // Renderer requests window close (e.g. Cmd+W on last tab).
    ipcMain.on("window:close", () => {
      mainWindow?.close();
    });

    ipcMain.handle("file:download-url", (_event, url: string) => {
      if (!mainWindow) {
        console.warn("[download] ignored file:download-url — mainWindow torn down");
        return;
      }
      downloadURLSafely(mainWindow, url);
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

    // Sync IPC: read + clear any freeze/crash breadcrumb left by a previous
    // session. The renderer flushes it to telemetry on boot (it couldn't be
    // reported when it happened — the renderer was hung or gone). Read-and-
    // clear so a failure reports exactly once.
    ipcMain.on("freeze:get-last", (event) => {
      event.returnValue = readAndClearFreezeBreadcrumb(freezeBreadcrumbPath());
    });

    // Sync IPC: preload exposes the validated runtime config before renderer
    // boot. If desktop.json exists but is invalid, renderer receives the
    // blocking error and must not silently fall back to the cloud defaults.
    ipcMain.on("runtime-config:get", (event) => {
      event.returnValue = runtimeConfigResult;
    });

    ipcMain.on(RENDERER_ROUTE_CONTEXT_CHANNEL, (event, context: unknown) => {
      if (!mainWindow || event.sender !== mainWindow.webContents) return;
      const sanitized = sanitizeRendererRouteContext(context);
      if (!sanitized) return;
      latestRendererRouteContext = sanitized;
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
    setupLocalDirectory(() => mainWindow);

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
