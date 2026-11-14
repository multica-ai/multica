// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "fs";
import { tmpdir } from "os";
import { join } from "path";

type FakeMenuItem = {
  label?: string;
  type?: string;
  role?: string;
  checked?: boolean;
  click?: (item: FakeMenuItem) => void;
  submenu?: { items: FakeMenuItem[]; append: (item: FakeMenuItem) => void };
};

const ctx = vi.hoisted(() => ({
  ipcHandlers: new Map<string, (event: { returnValue?: unknown }) => void>(),
  appMenu: null as { items: FakeMenuItem[] } | null,
  setApplicationMenu: vi.fn(),
  sent: [] as Array<{ channel: string; payload: unknown }>,
}));

vi.mock("electron", () => ({
  ipcMain: {
    on: (channel: string, handler: (event: { returnValue?: unknown }) => void) =>
      ctx.ipcHandlers.set(channel, handler),
  },
  Menu: {
    getApplicationMenu: () => ctx.appMenu,
    setApplicationMenu: ctx.setApplicationMenu,
  },
  MenuItem: class {
    constructor(options: FakeMenuItem) {
      Object.assign(this, options);
    }
  },
  BrowserWindow: {
    getAllWindows: () => [
      {
        webContents: {
          send: (channel: string, payload: unknown) => ctx.sent.push({ channel, payload }),
        },
      },
    ],
  },
}));

import {
  DEV_TOOLBAR_MENU_LABEL,
  devToolbarPreferencesPath,
  installDevToolbarToggle,
} from "./dev-toolbar-toggle";
import {
  DEV_TOOLBAR_VISIBLE_CHANGED_CHANNEL,
  DEV_TOOLBAR_VISIBLE_GET_CHANNEL,
} from "../shared/dev-toolbar";

function submenu(items: FakeMenuItem[] = []) {
  return { items, append: (item: FakeMenuItem) => items.push(item) };
}

function readVisible(): unknown {
  const event: { returnValue?: unknown } = {};
  ctx.ipcHandlers.get(DEV_TOOLBAR_VISIBLE_GET_CHANNEL)?.(event);
  return event.returnValue;
}

let dir: string;
let viewMenu: ReturnType<typeof submenu>;

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), "dev-toolbar-"));
  viewMenu = submenu([{ label: "Reload" }]);
  ctx.appMenu = {
    items: [
      { role: "filemenu", submenu: submenu() },
      { role: "viewmenu", submenu: viewMenu },
    ],
  };
  ctx.ipcHandlers.clear();
  ctx.setApplicationMenu.mockClear();
  ctx.sent.length = 0;
});

afterEach(() => {
  rmSync(dir, { recursive: true, force: true });
});

describe("installDevToolbarToggle", () => {
  it("adds a checked View item by default and saves each toggle", () => {
    const preferencesPath = devToolbarPreferencesPath(dir);
    installDevToolbarToggle({ enabled: true, preferencesPath });

    const item = viewMenu.items.find((i) => i.label === DEV_TOOLBAR_MENU_LABEL)!;
    expect(item).toMatchObject({ type: "checkbox", checked: true });
    expect(ctx.setApplicationMenu).toHaveBeenCalledWith(ctx.appMenu);
    expect(readVisible()).toBe(true);

    // Electron flips `checked` before calling click.
    item.checked = false;
    item.click!(item);

    expect(readVisible()).toBe(false);
    expect(ctx.sent).toEqual([{ channel: DEV_TOOLBAR_VISIBLE_CHANGED_CHANNEL, payload: false }]);
    expect(JSON.parse(readFileSync(preferencesPath, "utf-8"))).toEqual({ visible: false });
  });

  it("restores a hidden toolbar from the saved preference", () => {
    const preferencesPath = devToolbarPreferencesPath(dir);
    writeFileSync(preferencesPath, JSON.stringify({ visible: false }));
    installDevToolbarToggle({ enabled: true, preferencesPath });

    expect(viewMenu.items.find((i) => i.label === DEV_TOOLBAR_MENU_LABEL)?.checked).toBe(false);
    expect(readVisible()).toBe(false);
  });

  it("treats an unreadable preference as shown", () => {
    const preferencesPath = devToolbarPreferencesPath(dir);
    writeFileSync(preferencesPath, "{not json");
    installDevToolbarToggle({ enabled: true, preferencesPath });

    expect(readVisible()).toBe(true);
  });

  it("leaves the menu alone and answers false when the toolbar is not built in", () => {
    const preferencesPath = devToolbarPreferencesPath(dir);
    writeFileSync(preferencesPath, JSON.stringify({ visible: true }));
    installDevToolbarToggle({ enabled: false, preferencesPath });

    expect(viewMenu.items.map((i) => i.label)).toEqual(["Reload"]);
    expect(ctx.setApplicationMenu).not.toHaveBeenCalled();
    expect(readVisible()).toBe(false);
  });
});
