import { app, BrowserWindow, dialog, ipcMain, nativeImage, Notification } from "electron";
import { homedir } from "os";
import { join } from "path";
import { pathToFileURL } from "url";
import { electronApp, optimizer, is } from "@electron-toolkit/utils";
import fixPath from "fix-path";
import { setupAutoUpdater } from "./updater";
import { setupDaemonManager } from "./daemon-manager";
import { setupLocalDirectory } from "./local-directory";
import { openExternalSafely, downloadURLSafely } from "./external-url";
import { installContextMenu } from "./context-menu";
import { handleAppShortcut } from "./keyboard-shortcuts";
import { installNavigationGestures } from "./navigation-gestures";
import { installNavigationGuard } from "./navigation-guard";
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
import {
  encodeIssueWindowArgument,
  parseIssueWindowRequest,
  type IssueWindowContext,
} from "../shared/issue-window";
import {
  AUTH_SESSION_STATE_CHANNEL,
  parseAuthSessionUserId,
} from "../shared/auth-session";
import {
  MAIN_RENDERER_CHANNEL_STATE_CHANNEL,
  MainRendererMessageQueue,
  parseMainRendererChannelState,
  type MainRendererMessageChannel,
} from "../shared/main-renderer-messages";
import { AuthSessionCoordinator } from "./auth-session-coordinator";
import {
  NotificationGate,
  parseNativeNotificationPayload,
} from "./notification-gate";

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
const issueWindows = new Set<BrowserWindow>();
const authSessionCoordinator = new AuthSessionCoordinator<BrowserWindow>(
  (window) => {
    issueWindows.delete(window);
    if (!window.isDestroyed()) window.close();
  },
);
const notificationGate = new NotificationGate();
const mainRendererMessages = new MainRendererMessageQueue();
let desktopInitialized = false;
let authSessionGeneration = 0;
const rendererRouteContexts = new WeakMap<
  Electron.WebContents,
  RendererRouteContext
>();
let runtimeConfigResult: RuntimeConfigResult = {
  ok: false,
  error: { message: "Runtime config has not loaded yet" },
};

// --- Deep link helpers ---------------------------------------------------

function sendMainRendererMessage(
  channel: MainRendererMessageChannel,
  payload: unknown,
): void {
  const window = mainWindow;
  if (!window || window.isDestroyed()) return;
  window.webContents.send(channel, payload);
}

function focusMainWindow(window: BrowserWindow): void {
  if (window.isMinimized()) window.restore();
  window.show();
  window.focus();
}

function ensureMainWindow(): BrowserWindow | null {
  if (!desktopInitialized || !app.isReady()) return null;
  if (!mainWindow || mainWindow.isDestroyed()) return createWindow();
  return mainWindow;
}

function dispatchToMainRenderer(
  channel: MainRendererMessageChannel,
  payload: unknown,
): void {
  mainRendererMessages.enqueue(channel, payload, sendMainRendererMessage);
  const window = ensureMainWindow();
  if (window) focusMainWindow(window);
}

function handleDeepLink(url: string): void {
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== `${PROTOCOL}:`) return;

    // multica://auth/callback?token=<jwt>
    if (parsed.hostname === "auth" && parsed.pathname === "/callback") {
      const token = parsed.searchParams.get("token");
      if (token) dispatchToMainRenderer("auth:token", token);
      return;
    }

    // multica://invite/<invitationId>
    // Dispatched from the web invite page when the user chooses "Open in
    // desktop app". The renderer opens the invite overlay — no tab, no
    // route persistence, so deep-linking the same invite twice stays safe.
    if (parsed.hostname === "invite") {
      const id = parsed.pathname.replace(/^\//, "");
      if (id) dispatchToMainRenderer("invite:open", decodeURIComponent(id));
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

function createRendererWebPreferences(
  systemLocale: string,
  additionalArguments: string[] = [],
): Electron.WebPreferences {
  return {
    preload: join(__dirname, "../preload/index.js"),
    sandbox: false,
    webSecurity: false,
    // Required for the Chromium PDF viewer (PDFium) to activate inside
    // iframes — used by the attachment preview modal for application/pdf
    // files. Default is false in Electron; without it <iframe src=*.pdf>
    // renders blank.
    //
    // Security trade-off, accepted intentionally:
    //   1. These windows already run with `webSecurity: false` +
    //      `sandbox: false`, so `plugins: true` does not meaningfully widen
    //      the renderer's attack surface beyond what is already accepted.
    //   2. The only PDFs that reach an iframe here are signed CloudFront URLs
    //      we ourselves issued (see useDownloadAttachment); user-supplied URLs
    //      are routed through `setWindowOpenHandler` → `openExternalSafely` and
    //      cannot land in this renderer.
    //   3. Chromium's PDFium plugin is itself sandboxed inside its own process
    //      and only handles the `application/pdf` MIME.
    //
    // If we ever tighten `webSecurity` / `sandbox`, revisit this by hosting
    // the PDF viewer in a dedicated BrowserView with `plugins: true` scoped
    // to that view, keeping the main renderer plugin-free.
    plugins: true,
    additionalArguments: [
      `--multica-locale=${systemLocale}`,
      ...additionalArguments,
    ],
  };
}

function loadRenderer(window: BrowserWindow): void {
  const rendererEntry = join(__dirname, "../renderer/index.html");
  const rendererURL =
    is.dev && process.env["ELECTRON_RENDERER_URL"]
      ? process.env["ELECTRON_RENDERER_URL"]
      : pathToFileURL(rendererEntry).toString();

  // Installed before the load so the very first navigation is already covered.
  // Both the main window and every issue window load through here, so guarding
  // this one site covers both — see navigation-guard.ts for what is and is not
  // in scope (it is origin hardening; in-app routing never reaches it).
  installNavigationGuard(window, rendererURL);

  if (is.dev && process.env["ELECTRON_RENDERER_URL"]) {
    void window.loadURL(process.env["ELECTRON_RENDERER_URL"]);
  } else {
    void window.loadFile(rendererEntry);
  }
}

function installLocaleRefresh(window: BrowserWindow): void {
  // Electron has no dedicated OS-language event. Check whenever any Multica
  // window regains focus, then broadcast so all open windows remain aligned.
  window.on("focus", () => {
    const current = getSystemLocale();
    if (current === lastKnownSystemLocale) return;
    lastKnownSystemLocale = current;
    for (const target of BrowserWindow.getAllWindows()) {
      if (!target.isDestroyed()) {
        target.webContents.send("locale:system-changed", current);
      }
    }
  });
}

function installWindowShortcutHandler(window: BrowserWindow): void {
  window.webContents.on("before-input-event", (event, input) => {
    const result = handleAppShortcut(input, window.webContents);
    if (result === "close-tab") {
      event.preventDefault();
      window.webContents.send("tab:close-active");
    } else if (result) {
      event.preventDefault();
    }
  });
}

function createWindow(): BrowserWindow {
  // Pass the OS-preferred language to the renderer via additionalArguments
  // instead of a sync IPC call. process.argv is available to the preload
  // script before the first network request, so the renderer's i18next
  // instance can initialize with the right locale on the very first paint.
  const systemLocale = getSystemLocale();
  lastKnownSystemLocale = systemLocale;

  mainRendererMessages.resetReady();
  mainWindow = new BrowserWindow({
    width: 1280,
    height: 800,
    minWidth: 900,
    minHeight: 600,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 16, y: 17 },
    show: false,
    autoHideMenuBar: true,
    // Windows/Linux pick up the window/taskbar icon from this option.
    // On macOS it's ignored (dock comes from app.dock.setIcon below).
    // Linux production needs this explicitly because AppImage direct-launch
    // does not install a .desktop entry, so the WM has no other path to
    // the bundled icon; without it Ubuntu falls back to the theme default.
    ...(is.dev || process.platform === "linux"
      ? { icon: BUNDLED_ICON_PATH }
      : {}),
    webPreferences: createRendererWebPreferences(systemLocale),
  });
  const window = mainWindow;

  window.on("closed", () => {
    if (mainWindow === window) {
      mainWindow = null;
      mainRendererMessages.resetReady();
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

  installLocaleRefresh(window);

  installDownloadSaveDialogHandler(window);

  window.webContents.setWindowOpenHandler((details) => {
    openExternalSafely(details.url);
    return { action: "deny" };
  });

  // Calling preventDefault in the shared shortcut handler prevents both the
  // renderer keydown and the application-menu accelerator from double-firing.
  installWindowShortcutHandler(window);

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
    getDiagnosticContext: () => {
      const routeContext = rendererRouteContexts.get(window.webContents);
      return {
        windowUrl: window.webContents.getURL(),
        ...(routeContext ? { desktopRoute: routeContext } : {}),
      };
    },
    // Only persist in production: a true hang/crash can't report itself, so we
    // write a breadcrumb and the next renderer boot flushes it to PostHog. Dev
    // is excluded to keep field telemetry clean.
    persistBreadcrumb: is.dev
      ? undefined
      : (payload) =>
          writeFreezeBreadcrumb(freezeBreadcrumbPath(), {
            ownerId: `main:${window.id}`,
            kind: payload.kind,
            context: payload.context,
            ts: Date.now(),
            version: getAppVersion(),
          }),
    clearBreadcrumb: is.dev
      ? undefined
      : () =>
          clearFreezeBreadcrumb(freezeBreadcrumbPath(), `main:${window.id}`),
  });

  installContextMenu(window.webContents);
  installNavigationGestures(window);

  loadRenderer(window);
  return window;
}

function createIssueWindow(context: IssueWindowContext): void {
  const systemLocale = getSystemLocale();
  lastKnownSystemLocale = systemLocale;

  const window = new BrowserWindow({
    width: 960,
    height: 760,
    minWidth: 720,
    minHeight: 520,
    title: context.title,
    titleBarStyle: "hiddenInset",
    trafficLightPosition: { x: 16, y: 17 },
    show: false,
    autoHideMenuBar: true,
    ...(is.dev || process.platform === "linux"
      ? { icon: BUNDLED_ICON_PATH }
      : {}),
    webPreferences: createRendererWebPreferences(systemLocale, [
      encodeIssueWindowArgument(context),
    ]),
  });

  issueWindows.add(window);
  authSessionCoordinator.registerIssueWindow(window);
  window.on("closed", () => {
    issueWindows.delete(window);
    authSessionCoordinator.unregisterIssueWindow(window);
  });

  window.on("ready-to-show", () => window.show());
  installLocaleRefresh(window);
  installDownloadSaveDialogHandler(window);

  window.webContents.setWindowOpenHandler((details) => {
    void openExternalSafely(details.url);
    return { action: "deny" };
  });
  installWindowShortcutHandler(window);

  const initialRouteContext = sanitizeRendererRouteContext({
    surface: "tab",
    path: context.path,
    workspaceSlug: context.workspaceSlug,
  });
  if (initialRouteContext) {
    rendererRouteContexts.set(window.webContents, initialRouteContext);
  }
  installRendererRecoveryHandlers(window as unknown as RendererRecoveryWindow, {
    isDev: is.dev,
    showReloadPrompt: createElectronReloadPrompt((options) =>
      dialog.showMessageBox(window, options),
    ),
    getDiagnosticContext: () => {
      const routeContext = rendererRouteContexts.get(window.webContents);
      return {
        windowUrl: window.webContents.getURL(),
        ...(routeContext ? { desktopRoute: routeContext } : {}),
      };
    },
    persistBreadcrumb: is.dev
      ? undefined
      : (payload) =>
          writeFreezeBreadcrumb(freezeBreadcrumbPath(), {
            ownerId: `issue:${window.id}`,
            kind: payload.kind,
            context: payload.context,
            ts: Date.now(),
            version: getAppVersion(),
          }),
    clearBreadcrumb: is.dev
      ? undefined
      : () =>
          clearFreezeBreadcrumb(freezeBreadcrumbPath(), `issue:${window.id}`),
  });

  installContextMenu(window.webContents);
  loadRenderer(window);
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
  // Register before `ready`: macOS can deliver a cold-start URL while runtime
  // config is still loading. handleDeepLink queues the payload until both the
  // main window and its matching React listener exist.
  app.on("open-url", (event, url) => {
    event.preventDefault();
    handleDeepLink(url);
  });

  // Windows/Linux: second instance passes deep link via argv
  app.on("second-instance", (_event, argv) => {
    const window = ensureMainWindow();
    if (window) focusMainWindow(window);

    // On Windows the deep link URL is the last argv entry
    const deepLinkUrl = argv.find((arg) => arg.startsWith(`${PROTOCOL}://`));
    if (deepLinkUrl) handleDeepLink(deepLinkUrl);
  });

  // Windows/Linux cold-start deep links are safe to parse now. Delivery is
  // queued because desktopInitialized remains false until runtime config and
  // IPC handlers are ready.
  const coldStartDeepLink = process.argv.find((arg) =>
    arg.startsWith(`${PROTOCOL}://`),
  );
  if (coldStartDeepLink) handleDeepLink(coldStartDeepLink);

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

    // Renderer requests its own window close (e.g. Cmd+W on the last main
    // tab, or Cmd+W anywhere in a dedicated issue window).
    ipcMain.on("window:close", (event) => {
      BrowserWindow.fromWebContents(event.sender)?.close();
    });

    ipcMain.handle("window:open-issue", (event, request: unknown) => {
      if (!BrowserWindow.fromWebContents(event.sender)) {
        return { ok: false, reason: "invalid_request" } as const;
      }
      const context = parseIssueWindowRequest(request);
      if (!context) {
        return { ok: false, reason: "invalid_request" } as const;
      }
      createIssueWindow(context);
      return { ok: true } as const;
    });

    ipcMain.handle("file:download-url", (event, url: string) => {
      const sourceWindow = BrowserWindow.fromWebContents(event.sender);
      if (!sourceWindow) {
        console.warn("[download] ignored file:download-url — source window torn down");
        return;
      }
      downloadURLSafely(sourceWindow, url);
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
      if (!BrowserWindow.fromWebContents(event.sender)) return;
      const sanitized = sanitizeRendererRouteContext(context);
      if (!sanitized) return;
      rendererRouteContexts.set(event.sender, sanitized);
    });

    // Preload announces each listener only after it has been installed by the
    // main renderer. Ignore issue-window senders so they can never drain a
    // payload intended for the tabbed application window.
    ipcMain.on(
      MAIN_RENDERER_CHANNEL_STATE_CHANNEL,
      (event, state: unknown) => {
        if (!mainWindow || event.sender !== mainWindow.webContents) return;
        const parsed = parseMainRendererChannelState(state);
        if (!parsed) return;
        mainRendererMessages.setReady(
          parsed.channel,
          parsed.ready,
          sendMainRendererMessage,
        );
      },
    );

    // Account identity is the only cross-renderer auth signal. Main remains
    // authoritative and closes issue windows instead of copying credentials.
    ipcMain.on(AUTH_SESSION_STATE_CHANNEL, (event, value: unknown) => {
      const sourceWindow = BrowserWindow.fromWebContents(event.sender);
      const userId = parseAuthSessionUserId(value);
      if (!sourceWindow || userId === undefined) return;

      if (sourceWindow === mainWindow) {
        const accountInvalidated = authSessionCoordinator.reportMain(userId);
        if (accountInvalidated) {
          authSessionGeneration += 1;
          mainRendererMessages.clear("inbox:open");
        }
        return;
      }
      if (issueWindows.has(sourceWindow)) {
        authSessionCoordinator.reportIssue(sourceWindow, userId);
      }
    });

    // IPC: toggle immersive mode — hides the macOS traffic lights so full-screen
    // modals (e.g. create-workspace) can place UI in the top-left corner
    // without fighting the native window controls' hit-test.
    ipcMain.handle("window:setImmersive", (event, immersive: boolean) => {
      if (process.platform !== "darwin") return;
      BrowserWindow.fromWebContents(event.sender)?.setWindowButtonVisibility(
        !immersive,
      );
    });

    // Main owns foreground detection and item-level dedupe. Every renderer
    // has its own WebSocket and `document.hasFocus()` only describes that one
    // window, so renderer-only gating can emit N duplicate system banners.
    ipcMain.on("notification:show", (event, value: unknown) => {
      const sourceWindow = BrowserWindow.fromWebContents(event.sender);
      if (!sourceWindow) return;
      if (sourceWindow === mainWindow) {
        if (!authSessionCoordinator.hasActiveMainSession()) return;
      } else if (
        !issueWindows.has(sourceWindow) ||
        !authSessionCoordinator.isCurrentIssueSession(sourceWindow)
      ) {
        return;
      }

      const payload = parseNativeNotificationPayload(value);
      if (!payload || !Notification.isSupported()) return;
      const anyWindowFocused = BrowserWindow.getAllWindows().some(
        (window) => !window.isDestroyed() && window.isFocused(),
      );
      if (!notificationGate.shouldShow(payload.itemId, anyWindowFocused)) {
        return;
      }

      const notification = new Notification({
        title: payload.title,
        body: payload.body,
      });
      const notificationSessionGeneration = authSessionGeneration;
      notification.on("click", () => {
        // A banner emitted for user A must not navigate after the main window
        // logs out or switches to user B.
        if (notificationSessionGeneration !== authSessionGeneration) return;
        // Recreate the main window when an issue-only window outlived it, then
        // wait for the inbox listener before delivering the navigation.
        dispatchToMainRenderer("inbox:open", {
          slug: payload.slug,
          itemId: payload.itemId,
          issueKey: payload.issueKey,
        });
      });
      notification.show();
    });

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

    desktopInitialized = true;
    createWindow();

    setupAutoUpdater(() => mainWindow);
    setupDaemonManager(() => mainWindow);
    setupLocalDirectory(() => mainWindow);

    app.on("activate", () => {
      const window = ensureMainWindow();
      if (window) focusMainWindow(window);
    });
  });
}

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") app.quit();
});
