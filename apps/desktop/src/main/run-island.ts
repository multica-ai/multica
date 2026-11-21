import {
  BrowserWindow,
  ipcMain,
  screen,
  type Display,
  type IpcMainEvent,
  type IpcMainInvokeEvent,
} from "electron";
import { is } from "@electron-toolkit/utils";
import { execFile } from "node:child_process";
import { readdirSync } from "node:fs";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import {
  defaultIslandProfile,
  islandPreferences,
  islandRunGroups,
  type RunIslandSnapshot,
  type RunIslandView,
  type IslandProfile,
} from "../shared/run-island";
import { parseWorkspaceWindowPath } from "../shared/workspace-window";
import type { MainWindowRegistry } from "./main-window-registry";
import islandPreload from "../preload/run-island.cjs?asset";
import { installNavigationGuard } from "./navigation-guard";

export function islandLayout(
  display: Pick<Display, "internal" | "bounds" | "workArea">,
  expanded: boolean,
  count: number,
  profile: IslandProfile = defaultIslandProfile,
  groupCount = 0,
) {
  const { bounds, workArea } = display;
  const inset = workArea.y - bounds.y;
  // Electron exposes the menu-bar inset in points, but no camera cutout rect.
  // Keep the middle of the header clear on built-in displays with a tall bar.
  const attached = display.internal && inset >= 30;
  const headerHeight = attached ? inset + 2 : 30;
  const compactWidth = attached
    ? 272
    : profile.center !== "none"
      ? 224
      : profile.trailing === "agents"
        ? 108
        : profile.trailing === "hidden"
          ? 44
          : 84;
  const width = Math.min(workArea.width, expanded ? 368 : compactWidth);
  const body = expanded
    ? 41 + Math.min(240, Math.max(1, count) * 48 + groupCount * 24) + 8
    : 0;
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

  const played = new Set<string>();
  const soundsDir = "/System/Library/Sounds";
  let sounds: string[] = [];
  try {
    sounds = readdirSync(soundsDir, { withFileTypes: true })
      .filter((file) => file.isFile() && file.name.endsWith(".aiff"))
      .map((file) => file.name.slice(0, -5))
      .sort();
  } catch {
    /* The island remains usable without system audio files. */
  }

  function mainSender(event: IpcMainEvent | IpcMainInvokeEvent) {
    const source = BrowserWindow.fromWebContents(event.sender);
    return source &&
      windows.has(source) &&
      event.senderFrame === event.sender.mainFrame
      ? source
      : null;
  }

  let playback: ReturnType<typeof execFile> | undefined;
  function play(name: unknown, preview = false) {
    if (typeof name !== "string" || !sounds.includes(name)) return;
    if (playback && !preview) return;
    playback?.kill();
    const child = execFile(
      "/usr/bin/afplay",
      [join(soundsDir, name + ".aiff")],
      { timeout: 5000 },
      () => {
        if (playback === child) playback = undefined;
      },
    );
    playback = child;
  }

  ipcMain.handle("run-island:devices", (event) =>
    mainSender(event)
      ? {
          sounds,
          displays: screen
            .getAllDisplays()
            .map((d) => ({ value: String(d.id), label: d.label })),
        }
      : { sounds: [], displays: [] },
  );
  ipcMain.on("run-island:preview-sound", (event, name: unknown) => {
    if (mainSender(event)) play(name, true);
  });
  ipcMain.on("run-island:finish", (event, slug: unknown, id: unknown) => {
    const source = mainSender(event);
    if (
      !source ||
      source !== windows.mostRecent() ||
      windows.workspaceOf(source) !== slug ||
      typeof id !== "string"
    )
      return;
    const snapshot = snapshots.get(source);
    if (
      !snapshot ||
      snapshot.slug !== slug ||
      snapshot.preferences.muted ||
      played.has(id) ||
      !snapshot.runs.some((r) => r.id === id && r.audible)
    )
      return;
    played.add(id);
    if (played.size > 512) played.delete(played.values().next().value!);
    play(snapshot.preferences.sound);
  });

  function createPanel() {
    const win = new BrowserWindow({
      frame: false,
      acceptFirstMouse: true,
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
    win.webContents.on("render-process-gone", () => {
      if (!win.isDestroyed()) win.destroy();
    });
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
    const target = snapshot.preferences.display;
    const display =
      screen.getAllDisplays().find((d) => String(d.id) === target) ??
      screen.getDisplayMatching(next.getBounds());
    const attachedToNotch =
      display.internal && display.workArea.y - display.bounds.y >= 30;
    const profile =
      snapshot.preferences[attachedToNotch ? "notch" : "floating"];
    const { bounds, attached, headerHeight } = islandLayout(
      display,
      expanded,
      snapshot.runs.length,
      profile,
      expanded && profile.group !== "none"
        ? islandRunGroups(snapshot.runs, profile).length
        : 0,
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
      const source = mainSender(event);
      if (!source) return;
      if (value && typeof value.slug === "string" && Array.isArray(value.runs))
        snapshots.set(source, {
          ...value,
          preferences: islandPreferences(value.preferences),
        });
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
  screen.on("display-added", refresh);
  return {
    refresh,
    clear(window: BrowserWindow) {
      snapshots.delete(window);
      refresh();
    },
  };
}
