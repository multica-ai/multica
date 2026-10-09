// @vitest-environment node

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => {
  class MockTray {
    destroy = vi.fn();
    setContextMenu = vi.fn();
    setToolTip = vi.fn();
    listeners = new Map<string, () => void>();
    on(event: string, listener: () => void): this {
      this.listeners.set(event, listener);
      return this;
    }
  }
  return {
    MockTray,
    trays: [] as MockTray[],
    appListeners: new Map<string, () => void>(),
    menuTemplate: [] as Electron.MenuItemConstructorOptions[],
    resize: vi.fn(),
    preferredSystemLanguages: vi.fn(() => ["en-US"]),
  };
});

vi.mock("electron", () => ({
  app: {
    on: (event: string, listener: () => void) =>
      mocks.appListeners.set(event, listener),
    getPreferredSystemLanguages: mocks.preferredSystemLanguages,
  },
  Menu: {
    buildFromTemplate: (template: Electron.MenuItemConstructorOptions[]) => {
      mocks.menuTemplate = template;
      return { template };
    },
  },
  nativeImage: {
    createFromPath: () => ({
      resize: mocks.resize.mockReturnValue({}),
    }),
  },
  Tray: class {
    constructor() {
      const tray = new mocks.MockTray();
      mocks.trays.push(tray);
      return tray;
    }
  },
}));

import {
  buildTrayMenuTemplate,
  pickTrayMenuLabels,
  resetTrayForTests,
  setupTray,
} from "./tray-manager";

beforeEach(() => {
  mocks.trays.length = 0;
  mocks.appListeners.clear();
  mocks.menuTemplate = [];
  mocks.resize.mockClear();
  mocks.preferredSystemLanguages.mockClear();
  mocks.preferredSystemLanguages.mockReturnValue(["en-US"]);
});

afterEach(() => {
  resetTrayForTests();
});

describe("tray manager", () => {
  it("builds Open and explicit Quit actions", () => {
    const showWindow = vi.fn();
    const requestQuit = vi.fn();
    const template = buildTrayMenuTemplate({ showWindow, requestQuit });

    (template.find((item) => item.label === "Open Multica")?.click as () => void)();
    (template.find((item) => item.label === "Quit Multica")?.click as () => void)();

    expect(showWindow).toHaveBeenCalledOnce();
    expect(requestQuit).toHaveBeenCalledOnce();
  });

  it.each([
    ["zh-CN", { open: "打开 Multica", quit: "退出 Multica" }],
    ["zh-TW", { open: "打开 Multica", quit: "退出 Multica" }],
    ["ja-JP", { open: "Multica を開く", quit: "Multica を終了" }],
    ["ko-KR", { open: "Multica 열기", quit: "Multica 종료" }],
    ["fr-FR", { open: "Open Multica", quit: "Quit Multica" }],
  ])("localizes tray actions for %s", (locale, expected) => {
    expect(pickTrayMenuLabels(locale)).toEqual(expected);
  });

  it("restores from either tray click gesture and only mounts once", () => {
    const showWindow = vi.fn();
    setupTray({ iconPath: "/icon.png", showWindow, requestQuit: vi.fn() });
    setupTray({ iconPath: "/icon.png", showWindow, requestQuit: vi.fn() });

    expect(mocks.trays).toHaveLength(1);
    mocks.trays[0]?.listeners.get("click")?.();
    mocks.trays[0]?.listeners.get("double-click")?.();
    expect(showWindow).toHaveBeenCalledTimes(2);
  });

  it("destroys the tray when explicit application quit begins", () => {
    setupTray({
      iconPath: "/icon.png",
      showWindow: vi.fn(),
      requestQuit: vi.fn(),
    });
    const tray = mocks.trays[0];

    mocks.appListeners.get("before-quit")?.();

    expect(tray?.destroy).toHaveBeenCalledOnce();
  });
});
