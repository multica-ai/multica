import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BrowserWindow, WebContents } from "electron";

type Handler = (...args: unknown[]) => void;

const ctx = vi.hoisted(() => ({
  handlers: new Map<string, Handler[]>(),
  ipcHandle: vi.fn(),
  checkForUpdates: vi.fn(async () => ({
    updateInfo: { version: "0.3.18" },
    isUpdateAvailable: false,
  })),
  downloadUpdate: vi.fn(),
  quitAndInstall: vi.fn(),
  getVersion: vi.fn(() => "0.3.17"),
  autoUpdater: null as {
    allowPrerelease: boolean;
  } | null,
  showMessageBox: vi.fn(async () => ({ response: 1 })),
  openExternalSafely: vi.fn(),
  releasesPageUrl: "https://github.com/multica-ai/multica/releases/latest",
}));

vi.mock("electron-updater", () => {
  const autoUpdater = {
    autoDownload: false,
    autoInstallOnAppQuit: false,
    allowPrerelease: true as boolean,
    channel: undefined as string | undefined,
    on: vi.fn((event: string, handler: Handler) => {
      const handlers = ctx.handlers.get(event) ?? [];
      handlers.push(handler);
      ctx.handlers.set(event, handlers);
      return autoUpdater;
    }),
    checkForUpdates: ctx.checkForUpdates,
    downloadUpdate: ctx.downloadUpdate,
    quitAndInstall: ctx.quitAndInstall,
  };
  ctx.autoUpdater = autoUpdater;
  return { autoUpdater };
});

vi.mock("electron", () => ({
  app: {
    getVersion: ctx.getVersion,
  },
  BrowserWindow: class BrowserWindow {},
  dialog: {
    showMessageBox: ctx.showMessageBox,
  },
  ipcMain: {
    handle: ctx.ipcHandle,
  },
}));

vi.mock("./external-url", () => ({
  openExternalSafely: ctx.openExternalSafely,
}));

vi.mock("./github-release-base", () => ({
  githubReleasesLatestPageUrl: () => ctx.releasesPageUrl,
}));

import { setupAutoUpdater } from "./updater";

function emitUpdater(event: string, ...args: unknown[]) {
  for (const handler of ctx.handlers.get(event) ?? []) {
    handler(...args);
  }
}

function makeWindow() {
  const send = vi.fn();
  return {
    win: {
      isDestroyed: () => false,
      webContents: {
        isDestroyed: () => false,
        send,
      },
    } as unknown as BrowserWindow,
    send,
  };
}

function makeDestroyedWindow() {
  return {
    isDestroyed: () => true,
    get webContents(): WebContents {
      throw new TypeError("Object has been destroyed");
    },
  } as unknown as BrowserWindow;
}

function makeWindowWithDestroyedWebContents() {
  const send = vi.fn(() => {
    throw new TypeError("Object has been destroyed");
  });
  return {
    win: {
      isDestroyed: () => false,
      webContents: {
        isDestroyed: () => true,
        send,
      },
    } as unknown as BrowserWindow,
    send,
  };
}

function makeWindowWithThrowingSend(error: Error) {
  const send = vi.fn(() => {
    throw error;
  });
  return {
    win: {
      isDestroyed: () => false,
      webContents: {
        isDestroyed: () => false,
        send,
      },
    } as unknown as BrowserWindow,
    send,
  };
}

describe("setupAutoUpdater", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    ctx.handlers.clear();
    ctx.ipcHandle.mockClear();
    ctx.checkForUpdates.mockClear();
    ctx.downloadUpdate.mockClear();
    ctx.quitAndInstall.mockClear();
    ctx.getVersion.mockClear();
    ctx.showMessageBox.mockClear();
    ctx.openExternalSafely.mockClear();
    vi.stubGlobal("process", { ...process, platform: "darwin" });
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it("forwards update progress to a live renderer", () => {
    const { win, send } = makeWindow();
    setupAutoUpdater(() => win);

    emitUpdater("download-progress", { percent: 42 });

    expect(send).toHaveBeenCalledWith("updater:download-progress", {
      percent: 42,
    });
  });

  it("skips update progress when the BrowserWindow has already been destroyed", () => {
    setupAutoUpdater(() => makeDestroyedWindow());

    expect(() => emitUpdater("download-progress", { percent: 42 })).not.toThrow();
  });

  it("skips update progress when the BrowserWindow webContents has already been destroyed", () => {
    const { win, send } = makeWindowWithDestroyedWebContents();
    setupAutoUpdater(() => win);

    expect(() => emitUpdater("download-progress", { percent: 42 })).not.toThrow();
    expect(send).not.toHaveBeenCalled();
  });

  it("skips update progress when webContents.send loses a destroy race", () => {
    const { win, send } = makeWindowWithThrowingSend(
      new TypeError("Object has been destroyed"),
    );
    setupAutoUpdater(() => win);

    expect(() => emitUpdater("download-progress", { percent: 42 })).not.toThrow();
    expect(send).toHaveBeenCalledWith("updater:download-progress", {
      percent: 42,
    });
  });

  it("rethrows non-destroy errors from webContents.send", () => {
    const { win } = makeWindowWithThrowingSend(new Error("boom"));
    setupAutoUpdater(() => win);

    expect(() => emitUpdater("download-progress", { percent: 42 })).toThrow(
      "boom",
    );
  });

  it("disables allowPrerelease so git-describe local builds can check stable releases", () => {
    setupAutoUpdater(() => makeWindow().win);
    expect(ctx.autoUpdater?.allowPrerelease).toBe(false);
  });

  it("notifies the renderer and shows a dialog on macOS code-signature install failures", async () => {
    const { win, send } = makeWindow();
    setupAutoUpdater(() => win);

    emitUpdater(
      "error",
      new Error(
        "Code signature at URL file:///tmp/Multica.app/ did not pass validation: code did not meet specified code requirements",
      ),
    );

    expect(send).toHaveBeenCalledWith("updater:update-error", {
      code: "signature_mismatch",
      message:
        "Code signature at URL file:///tmp/Multica.app/ did not pass validation: code did not meet specified code requirements",
    });
    expect(ctx.showMessageBox).toHaveBeenCalled();
  });
});
