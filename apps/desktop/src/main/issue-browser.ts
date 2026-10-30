import { BrowserWindow, session, WebContentsView, type Session } from "electron";
import {
  ISSUE_BROWSER_STATE_CHANNEL,
  type IssueBrowserBounds,
  type IssueBrowserCapture,
  type IssueBrowserDrawState,
  type IssueBrowserPickedElement,
  type IssueBrowserState,
} from "../shared/issue-browser";
import { isSafeExternalHttpUrl } from "./external-url";
import { DEFAULT_URL, normalizeUrl } from "./issue-browser-url";
import {
  DRAW_CLEAR_SCRIPT,
  DRAW_UNDO_SCRIPT,
  PICK_CANCEL_SCRIPT,
  PICK_ELEMENT_SCRIPT,
  drawSetColorScript,
  drawSetToolScript,
  drawStateScript,
} from "./issue-browser-scripts";

// One persistent profile shared by every issue browser: logins, cookies and
// storage survive restarts and carry across issues, like a Chrome profile.
const BROWSER_PARTITION = "persist:multica-browser";
const HEX_COLOR = /^#[0-9a-fA-F]{6}$/;
const DRAW_TOOLS = new Set(["pen", "rect", "arrow", "eraser"]);

interface Entry {
  view: WebContentsView;
  window: BrowserWindow | null;
  /** Settles an in-flight element pick with null; navigation discards the page script. */
  abortPick: (() => void) | null;
}

const entries = new Map<string, Entry>();
let browserSession: Session | null = null;

function getSession(): Session {
  if (browserSession) return browserSession;
  const s = session.fromPartition(BROWSER_PARTITION);
  // Present as stock Chrome: many sites gate on the Electron/app tokens.
  s.setUserAgent(
    s.getUserAgent().replace(/\s(Electron|multica|Multica)\/\S+/g, ""),
  );
  // The guest browser never grants device or notification permissions.
  s.setPermissionRequestHandler((_wc, _permission, callback) => callback(false));
  browserSession = s;
  return s;
}

function snapshot(issueId: string, entry: Entry): IssueBrowserState {
  const wc = entry.view.webContents;
  return {
    issueId,
    url: wc.getURL(),
    title: wc.getTitle(),
    loading: wc.isLoading(),
    canGoBack: wc.navigationHistory.canGoBack(),
    canGoForward: wc.navigationHistory.canGoForward(),
  };
}

function emitState(issueId: string, entry: Entry): void {
  const win = entry.window;
  if (!win || win.isDestroyed()) return;
  win.webContents.send(ISSUE_BROWSER_STATE_CHANNEL, snapshot(issueId, entry));
}

function createEntry(issueId: string): Entry {
  const view = new WebContentsView({
    webPreferences: {
      session: getSession(),
      sandbox: true,
      contextIsolation: true,
      nodeIntegration: false,
    },
  });
  const entry: Entry = { view, window: null, abortPick: null };
  const wc = view.webContents;
  const update = () => emitState(issueId, entry);
  wc.on("did-start-loading", () => entry.abortPick?.());
  wc.on("did-start-loading", update);
  wc.on("did-stop-loading", update);
  wc.on("did-navigate", update);
  wc.on("did-navigate-in-page", update);
  wc.on("page-title-updated", update);
  // Popups stay inside the same view rather than spawning windows.
  wc.setWindowOpenHandler(({ url }) => {
    if (isSafeExternalHttpUrl(url)) void wc.loadURL(url);
    return { action: "deny" };
  });
  // A page reload drops injected overlays; nothing to restore beyond state.
  return entry;
}

function getOrCreate(issueId: string): Entry {
  let entry = entries.get(issueId);
  if (!entry) {
    entry = createEntry(issueId);
    entries.set(issueId, entry);
  }
  return entry;
}

function toRect(b: IssueBrowserBounds) {
  return {
    x: Math.round(b.x),
    y: Math.round(b.y),
    width: Math.max(0, Math.round(b.width)),
    height: Math.max(0, Math.round(b.height)),
  };
}

export function parseBounds(raw: unknown): IssueBrowserBounds | null {
  if (!raw || typeof raw !== "object") return null;
  const b = raw as Record<string, unknown>;
  for (const k of ["x", "y", "width", "height"]) {
    if (typeof b[k] !== "number" || !Number.isFinite(b[k] as number)) return null;
  }
  return b as unknown as IssueBrowserBounds;
}

/** Attach (or move) the issue's browser view into `win` and size it. */
export function showIssueBrowser(
  win: BrowserWindow,
  issueId: string,
  bounds: IssueBrowserBounds,
  initialUrl?: string,
): IssueBrowserState {
  const entry = getOrCreate(issueId);
  if (entry.window && entry.window !== win && !entry.window.isDestroyed()) {
    entry.window.contentView.removeChildView(entry.view);
  }
  entry.window = win;
  win.contentView.addChildView(entry.view);
  entry.view.setBounds(toRect(bounds));
  const wc = entry.view.webContents;
  if (!wc.getURL()) {
    const url = (initialUrl && normalizeUrl(initialUrl)) || DEFAULT_URL;
    void wc.loadURL(url);
  }
  return snapshot(issueId, entry);
}

export function setIssueBrowserBounds(issueId: string, bounds: IssueBrowserBounds): boolean {
  const entry = entries.get(issueId);
  if (!entry) return false;
  entry.view.setBounds(toRect(bounds));
  return true;
}

/** Detach the view but keep its page alive so the browser is sticky per issue. */
export function hideIssueBrowser(issueId: string): void {
  const entry = entries.get(issueId);
  if (!entry?.window || entry.window.isDestroyed()) return;
  entry.window.contentView.removeChildView(entry.view);
  entry.window = null;
}

export function closeIssueBrowser(issueId: string): void {
  hideIssueBrowser(issueId);
  const entry = entries.get(issueId);
  if (!entry) return;
  entries.delete(issueId);
  if (!entry.view.webContents.isDestroyed()) entry.view.webContents.close();
}

export function navigateIssueBrowser(issueId: string, input: string): boolean {
  const entry = entries.get(issueId);
  const url = normalizeUrl(input);
  if (!entry || !url) return false;
  void entry.view.webContents.loadURL(url).catch(() => {
    // Failed loads surface through did-stop-loading; nothing else to do.
  });
  return true;
}

export function controlIssueBrowser(
  issueId: string,
  action: "back" | "forward" | "reload" | "stop",
): boolean {
  const entry = entries.get(issueId);
  if (!entry) return false;
  const wc = entry.view.webContents;
  if (action === "back") wc.navigationHistory.goBack();
  else if (action === "forward") wc.navigationHistory.goForward();
  else if (action === "reload") wc.reload();
  else wc.stop();
  return true;
}

export function getIssueBrowserState(issueId: string): IssueBrowserState | null {
  const entry = entries.get(issueId);
  return entry ? snapshot(issueId, entry) : null;
}

export async function pickIssueBrowserElement(
  issueId: string,
): Promise<IssueBrowserPickedElement | null> {
  const entry = entries.get(issueId);
  if (!entry) return null;
  entry.view.webContents.focus();
  entry.abortPick?.();
  let abort = () => {};
  const aborted = new Promise<null>((resolve) => {
    abort = () => resolve(null);
  });
  entry.abortPick = abort;
  let raw: unknown;
  try {
    raw = await Promise.race([
      entry.view.webContents.executeJavaScript(PICK_ELEMENT_SCRIPT),
      aborted,
    ]);
  } finally {
    // A newer pick may have replaced this handle; only clear our own.
    if (entry.abortPick === abort) entry.abortPick = null;
  }
  if (!raw || typeof raw !== "object") return null;
  const p = raw as Partial<IssueBrowserPickedElement>;
  return {
    selector: String(p.selector ?? ""),
    tag: String(p.tag ?? ""),
    text: String(p.text ?? ""),
    html: String(p.html ?? ""),
    pageUrl: String(p.pageUrl ?? ""),
    pageTitle: String(p.pageTitle ?? ""),
    rect: parseBounds(p.rect) ?? { x: 0, y: 0, width: 0, height: 0 },
  };
}

export async function cancelIssueBrowserPick(issueId: string): Promise<void> {
  const entry = entries.get(issueId);
  if (entry) await entry.view.webContents.executeJavaScript(PICK_CANCEL_SCRIPT);
}

export type DrawCommand =
  | { type: "state" }
  | { type: "tool"; tool: string | null }
  | { type: "color"; color: string }
  | { type: "undo" }
  | { type: "clear" };

export async function drawIssueBrowser(
  issueId: string,
  command: DrawCommand,
): Promise<IssueBrowserDrawState | null> {
  const entry = entries.get(issueId);
  if (!entry) return null;
  let script: string;
  switch (command.type) {
    case "state":
      script = drawStateScript();
      break;
    case "tool":
      if (command.tool !== null && !DRAW_TOOLS.has(command.tool)) return null;
      script = drawSetToolScript(command.tool);
      break;
    case "color":
      if (!HEX_COLOR.test(command.color)) return null;
      script = drawSetColorScript(command.color);
      break;
    case "undo":
      script = DRAW_UNDO_SCRIPT;
      break;
    case "clear":
      script = DRAW_CLEAR_SCRIPT;
      break;
    default:
      return null;
  }
  return (await entry.view.webContents.executeJavaScript(script)) as IssueBrowserDrawState;
}

/** Capture the visible page (including any drawing overlay) as a PNG. */
export async function captureIssueBrowser(
  issueId: string,
  rect?: IssueBrowserBounds,
): Promise<IssueBrowserCapture | null> {
  const entry = entries.get(issueId);
  if (!entry) return null;
  const wc = entry.view.webContents;
  const image = await wc.capturePage(rect ? toRect(rect) : undefined);
  const size = image.getSize();
  return {
    pngBase64: image.toPNG().toString("base64"),
    width: size.width,
    height: size.height,
    pageUrl: wc.getURL(),
    pageTitle: wc.getTitle(),
  };
}

/** Tear down views owned by a window that is closing. */
export function releaseIssueBrowsersForWindow(win: BrowserWindow): void {
  for (const [issueId, entry] of entries) {
    if (entry.window === win) closeIssueBrowser(issueId);
  }
}
