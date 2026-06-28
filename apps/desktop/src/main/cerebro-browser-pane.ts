// Cerebro feature: personal browser pane (FIR-2037).
//
// A Multica-owned Chromium surface that lives INSIDE the desktop app and runs
// as a tab, the same way an issue opens as a tab. The user logs into sites
// here; cookies/logins persist across restarts via a dedicated persistent
// session partition. Later phases let an agent drive this same pane over CDP
// (webContents.debugger) — but it is always Multica's own browser, never the
// user's system Chrome/Safari.
//
// Design follows the Craft Agents OSS blueprint (Apache-2.0,
// github.com/craft-ai-agents/craft-agents-oss — apps/electron/src/main/
// browser-pane-manager.ts): a native view positioned under the React tab via
// setBounds, shown/hidden on tab switch. This implementation is written fresh
// for Multica; Craft is credited as the design reference.
//
// Everything here is gated behind the `cerebro_browser` feature flag on the
// renderer side — the pane is only ever created when the renderer opens the
// Browser tab, so a flag-off build never instantiates it. The capability-gated
// agent transport (cerebro-browser-control-server.ts) only ever reaches the
// pane through the exported singleton accessor below.

import { WebContentsView, ipcMain, type BrowserWindow } from "electron";
import {
  BrowserCDP,
  type AccessibilitySnapshot,
} from "./cerebro-browser-cdp";
import { ensureCerebroBrowserControlServer } from "./cerebro-browser-control-server";

// Persistent session partition prefix: cookies, localStorage and logins written
// here survive app restarts (the `persist:` prefix is what makes Chromium back
// the partition with disk storage instead of memory). This is the whole
// "remember my login" mechanism — no manual cookie export needed.
//
// Fase 3: each personal-browser SESSION gets its OWN partition so the user (or
// an agent) can be logged into two different accounts on the same site at once,
// fully isolated from one another. The default session keeps the original
// partition string so existing saved logins are not orphaned by the upgrade.
const PARTITION_PREFIX = "persist:cerebro-browser";
const DEFAULT_SESSION_ID = "default";

function partitionFor(sessionId: string): string {
  return sessionId === DEFAULT_SESSION_ID
    ? PARTITION_PREFIX
    : `${PARTITION_PREFIX}-${sessionId}`;
}

// A session id is author-controlled (renderer UI or agent transport). Keep it to
// a safe slug so it can never escape the partition namespace or smuggle path
// separators into the Chromium partition string.
const SESSION_ID_RE = /^[a-z0-9][a-z0-9-]{0,63}$/;

function isValidSessionId(id: string): boolean {
  return SESSION_ID_RE.test(id);
}

// Default landing page when a fresh pane opens with no URL yet.
const DEFAULT_URL = "https://www.google.com";

// Each session owns a live WebContentsView (a Chromium renderer), so the count
// must be bounded — an agent driving `switch-session` with fresh ids could
// otherwise spawn renderers until the app runs out of memory. Far above any real
// use (a handful of logged-in accounts), low enough to fail loudly on abuse.
const MAX_SESSIONS = 20;

interface PaneBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

interface NavState {
  sessionId: string;
  url: string;
  title: string;
  canGoBack: boolean;
  canGoForward: boolean;
  isLoading: boolean;
}

export interface SessionInfo {
  id: string;
  label: string;
  active: boolean;
  url: string;
  title: string;
}

interface BrowserSession {
  id: string;
  label: string;
  view: WebContentsView;
  cdp: BrowserCDP;
}

/**
 * Owns every personal-browser session. Each session is its own
 * WebContentsView + isolated persistent partition; exactly one is visible at a
 * time (the active session), positioned under the React Browser tab. Sessions
 * are created lazily and kept alive across tab switches so logins and page
 * state survive.
 */
class CerebroBrowserPane {
  private sessions = new Map<string, BrowserSession>();
  private activeSessionId = DEFAULT_SESSION_ID;
  private bounds: PaneBounds | null = null;
  private getWindow: () => BrowserWindow | null;

  constructor(getWindow: () => BrowserWindow | null) {
    this.getWindow = getWindow;
  }

  private emitNavState(sessionId: string): void {
    const win = this.getWindow();
    if (!win || win.isDestroyed()) return;
    // Only the active session drives the visible toolbar.
    if (sessionId !== this.activeSessionId) return;
    const sess = this.sessions.get(sessionId);
    if (!sess) return;
    const wc = sess.view.webContents;
    const state: NavState = {
      sessionId,
      url: wc.getURL(),
      title: wc.getTitle(),
      canGoBack: wc.navigationHistory.canGoBack(),
      canGoForward: wc.navigationHistory.canGoForward(),
      isLoading: wc.isLoading(),
    };
    win.webContents.send("cerebro-browser:nav-state", state);
  }

  /** Create a session's view on first use and attach it to the main window. */
  private ensureSession(sessionId: string, label?: string): BrowserSession {
    const existing = this.sessions.get(sessionId);
    if (existing) return existing;
    if (!isValidSessionId(sessionId)) {
      throw new Error(`[cerebro-browser] invalid session id: ${sessionId}`);
    }
    if (this.sessions.size >= MAX_SESSIONS) {
      throw new Error(
        `[cerebro-browser] session limit reached (${MAX_SESSIONS}); close a session first`,
      );
    }
    const win = this.getWindow();
    if (!win) throw new Error("[cerebro-browser] no main window to attach to");

    const view = new WebContentsView({
      webPreferences: {
        partition: partitionFor(sessionId),
        // This is an untrusted web surface: keep it isolated from the app's
        // renderer and Node. The agent never reaches it except through the
        // explicit CDP layer.
        contextIsolation: true,
        nodeIntegration: false,
        sandbox: true,
      },
    });

    const wc = view.webContents;
    // External target=_blank / window.open navigations load in-place rather
    // than spawning unmanaged child windows we'd never position or close.
    wc.setWindowOpenHandler(({ url }) => {
      void wc.loadURL(url);
      return { action: "deny" };
    });

    const forward = (): void => this.emitNavState(sessionId);
    wc.on("did-navigate", forward);
    wc.on("did-navigate-in-page", forward);
    wc.on("did-start-loading", forward);
    wc.on("did-stop-loading", forward);
    wc.on("page-title-updated", forward);

    win.contentView.addChildView(view);
    view.setVisible(false);
    const sess: BrowserSession = {
      id: sessionId,
      label: label?.trim() || sessionId,
      view,
      cdp: new BrowserCDP(wc),
    };
    this.sessions.set(sessionId, sess);
    return sess;
  }

  private active(): BrowserSession | null {
    return this.sessions.get(this.activeSessionId) ?? null;
  }

  /** Show a session, creating it on first open and hiding any other. */
  open(sessionId: string = DEFAULT_SESSION_ID, label?: string): void {
    // Bring up the capability-gated agent transport the first time the pane is
    // ever opened. Inert until then (and never reached on a flag-off build).
    void ensureCerebroBrowserControlServer().catch((err) =>
      console.warn("[cerebro-browser] control server failed to start", err),
    );
    const sess = this.ensureSession(sessionId, label);
    this.activeSessionId = sessionId;
    for (const other of this.sessions.values()) {
      other.view.setVisible(other.id === sessionId);
    }
    if (this.bounds) sess.view.setBounds(this.roundBounds(this.bounds));
    if (!sess.view.webContents.getURL()) {
      void sess.view.webContents.loadURL(DEFAULT_URL);
    }
    this.emitNavState(sessionId);
  }

  private roundBounds(b: PaneBounds): PaneBounds {
    return {
      x: Math.round(b.x),
      y: Math.round(b.y),
      width: Math.round(b.width),
      height: Math.round(b.height),
    };
  }

  /** Position the active pane to sit exactly over the renderer's host rect. */
  setBounds(bounds: PaneBounds): void {
    this.bounds = bounds;
    this.active()?.view.setBounds(this.roundBounds(bounds));
  }

  /** Hide all panes when the user switches away from the Browser tab. Views
   *  (and their logged-in sessions) are kept alive for instant re-show. */
  hide(): void {
    for (const sess of this.sessions.values()) sess.view.setVisible(false);
  }

  navigate(rawUrl: string): void {
    const sess = this.ensureSession(this.activeSessionId);
    const url = normalizeUrl(rawUrl);
    if (url) void sess.view.webContents.loadURL(url);
  }

  goBack(): void {
    const wc = this.active()?.view.webContents;
    if (wc?.navigationHistory.canGoBack()) wc.navigationHistory.goBack();
  }

  goForward(): void {
    const wc = this.active()?.view.webContents;
    if (wc?.navigationHistory.canGoForward()) wc.navigationHistory.goForward();
  }

  reload(): void {
    this.active()?.view.webContents.reload();
  }

  // --- Session management (Fase 3) ---------------------------------------

  /** List every live session plus which one is active. */
  listSessions(): SessionInfo[] {
    return Array.from(this.sessions.values()).map((s) => ({
      id: s.id,
      label: s.label,
      active: s.id === this.activeSessionId,
      url: s.view.webContents.getURL(),
      title: s.view.webContents.getTitle(),
    }));
  }

  /** Switch the visible pane to an existing or new session. */
  switchSession(sessionId: string, label?: string): void {
    this.open(sessionId, label);
  }

  /**
   * Log out of a session: wipe every cookie, storage area and HTTP cache for
   * its isolated partition, then reload to the default page. The view stays
   * alive so the user lands on a clean, logged-out browser. This only ever
   * touches the personal-browser partition — never the user's system browser
   * and never another session's partition.
   */
  async logout(sessionId: string = DEFAULT_SESSION_ID): Promise<void> {
    const sess = this.ensureSession(sessionId);
    const ses = sess.view.webContents.session;
    await ses.clearStorageData(); // cookies, localStorage, IndexedDB, service workers, …
    await ses.clearCache();
    void sess.view.webContents.loadURL(DEFAULT_URL);
    this.emitNavState(sessionId);
  }

  /** Clear only cookies for a session (lighter than a full logout). */
  async clearCookies(sessionId: string = DEFAULT_SESSION_ID): Promise<void> {
    const sess = this.ensureSession(sessionId);
    await sess.view.webContents.session.clearStorageData({
      storages: ["cookies"],
    });
    this.emitNavState(sessionId);
  }

  /**
   * Destroy a session entirely: detach and close its view. The persistent
   * partition on disk is left intact unless logout() was called first, so a
   * re-opened session restores its logins — matching how the user expects a
   * "closed tab" to behave.
   */
  closeSession(sessionId: string): void {
    if (sessionId === DEFAULT_SESSION_ID) return; // default is always available
    const sess = this.sessions.get(sessionId);
    if (!sess) return;
    const win = this.getWindow();
    if (win && !win.isDestroyed()) win.contentView.removeChildView(sess.view);
    sess.view.webContents.close();
    this.sessions.delete(sessionId);
    if (this.activeSessionId === sessionId) this.open(DEFAULT_SESSION_ID);
  }

  /**
   * Current page URL of a session (for per-action host authorization). Returns
   * "" when the session has no page loaded yet. Creates the session if needed so
   * the host-check path is consistent with the action that follows.
   */
  currentUrl(sessionId: string = DEFAULT_SESSION_ID): string {
    const sess = this.ensureSession(sessionId);
    return sess.view.webContents.getURL();
  }

  // --- Agent control surface (Fase 2) ------------------------------------
  // These drive the same logged-in pane via CDP. They are reached only through
  // the capability-gated agent transport, never the user's system browser.

  /** Read the page as a ref-based accessibility tree the agent can act on. */
  async agentSnapshot(
    sessionId: string = DEFAULT_SESSION_ID,
  ): Promise<AccessibilitySnapshot> {
    const sess = this.ensureSession(sessionId);
    return sess.cdp.getAccessibilitySnapshot();
  }

  /** Click an element by its accessibility ref (e.g. "@e12"). */
  async agentClick(
    ref: string,
    sessionId: string = DEFAULT_SESSION_ID,
  ): Promise<void> {
    const sess = this.ensureSession(sessionId);
    await sess.cdp.clickElement(ref);
  }

  /** Type a value into an input/textarea by its accessibility ref. */
  async agentFill(
    ref: string,
    value: string,
    sessionId: string = DEFAULT_SESSION_ID,
  ): Promise<void> {
    const sess = this.ensureSession(sessionId);
    await sess.cdp.fillElement(ref, value);
  }

  /** Navigate a (possibly background) session for the agent. */
  async agentNavigate(
    rawUrl: string,
    sessionId: string = DEFAULT_SESSION_ID,
  ): Promise<void> {
    const sess = this.ensureSession(sessionId);
    const url = normalizeUrl(rawUrl);
    if (url) await sess.view.webContents.loadURL(url);
  }

  /** Ask the renderer to open & focus the Browser tab so a human can watch/log in. */
  requestOpenTab(): void {
    const win = this.getWindow();
    if (!win || win.isDestroyed()) return;
    win.webContents.send("cerebro-browser:open-tab");
    if (win.isMinimized()) win.restore();
    win.show();
    win.focus();
  }
}

/**
 * Turn a user-typed address bar string into a loadable URL. A bare host
 * (`example.com`) gets `https://`; anything with spaces or no dot becomes a
 * Google search, matching what users expect from a browser address bar.
 */
function normalizeUrl(input: string): string | null {
  const trimmed = input.trim();
  if (!trimmed) return null;
  if (/^https?:\/\//i.test(trimmed)) return trimmed;
  const looksLikeDomain = /^[^\s]+\.[^\s]+$/.test(trimmed);
  if (looksLikeDomain) return `https://${trimmed}`;
  return `https://www.google.com/search?q=${encodeURIComponent(trimmed)}`;
}

// Singleton pane instance. Held so the capability-gated agent transport
// (cerebro-browser-control-server.ts) can drive the SAME pane the user sees,
// without re-registering IPC. Null until setupCerebroBrowserPane runs.
let paneSingleton: CerebroBrowserPane | null = null;

/** Accessor for the agent transport. Returns null before setup. */
export function getCerebroBrowserPane(): CerebroBrowserPane | null {
  return paneSingleton;
}

export type { CerebroBrowserPane };

/**
 * Register the IPC surface for the personal browser pane. Call once during app
 * startup. The renderer reaches these through `window.cerebroBrowser`
 * (see preload). All channels are namespaced `cerebro-browser:*`.
 */
export function setupCerebroBrowserPane(
  getWindow: () => BrowserWindow | null,
): CerebroBrowserPane {
  const pane = new CerebroBrowserPane(getWindow);
  paneSingleton = pane;

  // Bring up the capability-gated agent transport at app startup (flag-on),
  // before the human ever opens the Browser tab — otherwise the sidecar the
  // CLI reads would only exist after a manual tab open, and an agent could not
  // open the tab for the human in the first place.
  ipcMain.handle("cerebro-browser:ensure-control-server", () =>
    ensureCerebroBrowserControlServer(),
  );

  ipcMain.handle("cerebro-browser:open", (_e, sessionId?: string) =>
    pane.open(sessionId),
  );
  ipcMain.handle("cerebro-browser:hide", () => pane.hide());
  ipcMain.handle("cerebro-browser:set-bounds", (_e, bounds: PaneBounds) =>
    pane.setBounds(bounds),
  );
  ipcMain.handle("cerebro-browser:navigate", (_e, url: string) =>
    pane.navigate(url),
  );
  ipcMain.handle("cerebro-browser:back", () => pane.goBack());
  ipcMain.handle("cerebro-browser:forward", () => pane.goForward());
  ipcMain.handle("cerebro-browser:reload", () => pane.reload());

  // Session management (Fase 3).
  ipcMain.handle("cerebro-browser:list-sessions", () => pane.listSessions());
  ipcMain.handle(
    "cerebro-browser:switch-session",
    (_e, sessionId: string, label?: string) =>
      pane.switchSession(sessionId, label),
  );
  ipcMain.handle("cerebro-browser:logout", (_e, sessionId?: string) =>
    pane.logout(sessionId),
  );
  ipcMain.handle("cerebro-browser:clear-cookies", (_e, sessionId?: string) =>
    pane.clearCookies(sessionId),
  );
  ipcMain.handle("cerebro-browser:close-session", (_e, sessionId: string) =>
    pane.closeSession(sessionId),
  );

  // Agent control surface (Fase 2). Exposed over IPC here for parity; the
  // capability-gated transport that lets a runtime agent reach these is wired
  // in cerebro-browser-control-server.ts via getCerebroBrowserPane().
  ipcMain.handle(
    "cerebro-browser:agent-snapshot",
    (_e, sessionId?: string) => pane.agentSnapshot(sessionId),
  );
  ipcMain.handle(
    "cerebro-browser:agent-click",
    (_e, ref: string, sessionId?: string) => pane.agentClick(ref, sessionId),
  );
  ipcMain.handle(
    "cerebro-browser:agent-fill",
    (_e, ref: string, value: string, sessionId?: string) =>
      pane.agentFill(ref, value, sessionId),
  );

  return pane;
}
