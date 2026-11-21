import { BrowserWindow, ipcMain, screen, type Display } from "electron";
import { is } from "@electron-toolkit/utils";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import type { RunIslandSnapshot, RunIslandView } from "../shared/run-island";
import { parseWorkspaceWindowPath } from "../shared/workspace-window";
import type { MainWindowRegistry } from "./main-window-registry";
import islandPreload from "../preload/run-island.cjs?asset";
import { installNavigationGuard } from "./navigation-guard";

export function islandLayout(
  display: Pick<Display, "internal" | "bounds" | "workArea">,
  expanded: boolean,
  count: number,
) {
  const { bounds, workArea } = display;
  const inset = workArea.y - bounds.y;
  // Electron exposes the menu-bar inset in points, but no camera cutout rect.
  // Keep the middle of the header clear on built-in displays with a tall bar.
  const attached = display.internal && inset >= 30;
  const headerHeight = attached ? inset + 2 : 30;
  const width = Math.min(workArea.width, expanded ? 368 : attached ? 272 : 84);
  const body = expanded ? 41 + Math.max(1, Math.min(count, 5)) * 48 + 8 : 0;
  return {
    attached,
    headerHeight,
    bounds: {
      x: Math.round(bounds.x + (bounds.width - width) / 2),
      y: attached ? bounds.y : workArea.y + 4,
      width,
      height: headerHeight + body,
    },
  };
}

export function installRunIsland(
  windows: MainWindowRegistry<BrowserWindow>,
  openPath: (owner: BrowserWindow, path: string) => void,
) {
  const snapshots = new Map<BrowserWindow, RunIslandSnapshot>();
  let panel: BrowserWindow | null = null;
  let owner: BrowserWindow | null = null;
  let expanded = false;
  let ready = false;

  function createPanel() {
    const win = new BrowserWindow({
      frame: false,
      transparent: true,
      show: false,
      hasShadow: false,
      type: "panel",
      skipTaskbar: true,
      resizable: false,
      movable: false,
      fullscreenable: false,
      minimizable: false,
      maximizable: false,
      enableLargerThanScreen: true,
      roundedCorners: false,
      webPreferences: {
        preload: islandPreload,
        sandbox: true,
        contextIsolation: true,
        nodeIntegration: false,
        partition: "run-island",
      },
    });
    win.setAlwaysOnTop(true, "status");
    win.setVisibleOnAllWorkspaces(true, {
      visibleOnFullScreen: true,
      skipTransformProcessType: true,
    });
    win.webContents.setWindowOpenHandler(() => ({ action: "deny" }));
    const url =
      is.dev && process.env.ELECTRON_RENDERER_URL
        ? new URL("run-island.html", process.env.ELECTRON_RENDERER_URL).href
        : pathToFileURL(join(__dirname, "../renderer/run-island.html")).href;
    installNavigationGuard(win, url);
    win.on("closed", () => {
      if (panel === win) {
        panel = null;
        ready = false;
        expanded = false;
      }
    });
    void win.loadURL(url).catch((error: unknown) => {
      if (!win.isDestroyed()) {
        console.warn("[run-island] unable to load panel", error);
        win.destroy();
      }
    });
    return win;
  }

  function refresh() {
    const next = windows.mostRecent();
    const snapshot = next && snapshots.get(next);
    if (next !== owner) expanded = false;
    owner = next;
    if (!next || !snapshot || windows.workspaceOf(next) !== snapshot.slug) {
      expanded = false;
      panel?.hide();
      if (!snapshots.size) panel?.destroy();
      return;
    }
    panel ??= createPanel();
    const display = screen.getDisplayMatching(next.getBounds());
    const { bounds, attached, headerHeight } = islandLayout(
      display,
      expanded,
      snapshot.runs.length,
    );
    panel.setBounds(bounds);
    if (!ready) return;
    panel.webContents.send("run-island:view", {
      snapshot,
      expanded,
      attached,
      headerHeight,
    } satisfies RunIslandView);
    if (!panel.isVisible()) panel.showInactive();
  }

  ipcMain.on(
    "run-island:snapshot",
    (event, value: RunIslandSnapshot | null) => {
      const source = BrowserWindow.fromWebContents(event.sender);
      if (
        !source ||
        !windows.has(source) ||
        event.senderFrame !== event.sender.mainFrame
      )
        return;
      if (value && typeof value.slug === "string" && Array.isArray(value.runs))
        snapshots.set(source, value);
      else snapshots.delete(source);
      refresh();
    },
  );
  ipcMain.on("run-island:ready", (event) => {
    if (event.sender !== panel?.webContents) return;
    ready = true;
    refresh();
  });
  ipcMain.on("run-island:expand", (event, value: unknown) => {
    if (
      event.sender !== panel?.webContents ||
      typeof value !== "boolean" ||
      expanded === value
    )
      return;
    expanded = value;
    refresh();
  });
  ipcMain.on("run-island:open", (event, id: unknown) => {
    if (event.sender !== panel?.webContents || !owner) return;
    const snapshot = snapshots.get(owner);
    if (!snapshot || snapshot.slug !== windows.workspaceOf(owner)) return;
    const run = snapshot.runs.find((row) => row.id === id);
    const path = parseWorkspaceWindowPath(snapshot.slug, run?.path);
    if (!path) return;
    expanded = false;
    refresh();
    openPath(owner, path);
  });
  screen.on("display-metrics-changed", refresh);
  screen.on("display-removed", refresh);
  return {
    refresh,
    clear(window: BrowserWindow) {
      snapshots.delete(window);
      refresh();
    },
  };
}
