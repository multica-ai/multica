import { BrowserWindow, ipcMain } from "electron";
import type { IssueBrowserResult } from "../shared/issue-browser";
import {
  cancelIssueBrowserPick,
  captureIssueBrowser,
  closeIssueBrowser,
  controlIssueBrowser,
  drawIssueBrowser,
  type DrawCommand,
  getIssueBrowserState,
  hideIssueBrowser,
  navigateIssueBrowser,
  parseBounds,
  pickIssueBrowserElement,
  releaseIssueBrowsersForWindow,
  setIssueBrowserBounds,
  showIssueBrowser,
} from "./issue-browser";

const invalid = { ok: false, reason: "invalid_request" } as const;
const noBrowser = { ok: false, reason: "no_browser" } as const;

const isId = (v: unknown): v is string =>
  typeof v === "string" && v.length > 0 && v.length <= 128;

async function guarded<T>(
  run: () => Promise<T | null> | T | null,
): Promise<IssueBrowserResult<T>> {
  try {
    const value = await run();
    return value === null ? noBrowser : { ok: true, value };
  } catch (err) {
    return {
      ok: false,
      reason: "error",
      error: err instanceof Error ? err.message : String(err),
    };
  }
}

function parseDrawCommand(raw: unknown): DrawCommand | null {
  if (!raw || typeof raw !== "object") return null;
  const c = raw as Record<string, unknown>;
  switch (c.type) {
    case "state":
    case "undo":
    case "clear":
      return { type: c.type };
    case "tool":
      return c.tool === null || typeof c.tool === "string"
        ? { type: "tool", tool: c.tool }
        : null;
    case "color":
      return typeof c.color === "string" ? { type: "color", color: c.color } : null;
    default:
      return null;
  }
}

export function registerIssueBrowserIpc(): void {
  ipcMain.handle("issue-browser:show", (event, issueId: unknown, bounds: unknown, url: unknown) => {
    const win = BrowserWindow.fromWebContents(event.sender);
    const b = parseBounds(bounds);
    if (!win || !isId(issueId) || !b) return invalid;
    return guarded(() =>
      showIssueBrowser(win, issueId, b, typeof url === "string" ? url : undefined),
    );
  });
  ipcMain.handle("issue-browser:set-bounds", (_e, issueId: unknown, bounds: unknown) => {
    const b = parseBounds(bounds);
    if (!isId(issueId) || !b) return invalid;
    return guarded(() => (setIssueBrowserBounds(issueId, b) ? true : null));
  });
  ipcMain.handle("issue-browser:hide", (_e, issueId: unknown) => {
    if (!isId(issueId)) return invalid;
    hideIssueBrowser(issueId);
    return { ok: true, value: true } as const;
  });
  ipcMain.handle("issue-browser:close", (_e, issueId: unknown) => {
    if (!isId(issueId)) return invalid;
    closeIssueBrowser(issueId);
    return { ok: true, value: true } as const;
  });
  ipcMain.handle("issue-browser:navigate", (_e, issueId: unknown, url: unknown) => {
    if (!isId(issueId) || typeof url !== "string") return invalid;
    return guarded(() => (navigateIssueBrowser(issueId, url) ? true : null));
  });
  ipcMain.handle("issue-browser:control", (_e, issueId: unknown, action: unknown) => {
    if (!isId(issueId) || !["back", "forward", "reload", "stop"].includes(action as string)) {
      return invalid;
    }
    return guarded(() =>
      controlIssueBrowser(issueId, action as "back" | "forward" | "reload" | "stop") ? true : null,
    );
  });
  ipcMain.handle("issue-browser:get-state", (_e, issueId: unknown) => {
    if (!isId(issueId)) return invalid;
    return guarded(() => getIssueBrowserState(issueId));
  });
  ipcMain.handle("issue-browser:pick-element", (_e, issueId: unknown) => {
    if (!isId(issueId)) return invalid;
    return guarded(() => pickIssueBrowserElement(issueId));
  });
  ipcMain.handle("issue-browser:cancel-pick", (_e, issueId: unknown) => {
    if (!isId(issueId)) return invalid;
    return guarded(async () => {
      await cancelIssueBrowserPick(issueId);
      return true;
    });
  });
  ipcMain.handle("issue-browser:draw", (_e, issueId: unknown, command: unknown) => {
    const cmd = parseDrawCommand(command);
    if (!isId(issueId) || !cmd) return invalid;
    return guarded(() => drawIssueBrowser(issueId, cmd));
  });
  ipcMain.handle("issue-browser:capture", (_e, issueId: unknown, rect: unknown) => {
    if (!isId(issueId)) return invalid;
    const r = rect === undefined || rect === null ? undefined : parseBounds(rect);
    if (r === null) return invalid;
    return guarded(() => captureIssueBrowser(issueId, r));
  });
}

/** Call from `browser-window-created` so a closing window drops its guest pages. */
export function watchWindowForIssueBrowsers(win: BrowserWindow): void {
  win.on("closed", () => releaseIssueBrowsersForWindow(win));
}
