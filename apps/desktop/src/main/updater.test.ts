// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BrowserWindow, WebContents } from "electron";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { autoUpdater } from "electron-updater";
import type { UpdateInstallCheck } from "../shared/updater-types";

type Handler = (...args: unknown[]) => void;
type IpcHandler = (...args: unknown[]) => unknown;

const ctx = vi.hoisted(() => ({
  handlers: new Map<string, Handler[]>(),
  ipcHandlers: new Map<string, IpcHandler>(),
  ipcHandle: vi.fn(),
  checkForUpdates: vi.fn(async () => ({
    updateInfo: { version: "0.3.18" },
    isUpdateAvailable: false,
  })),
  downloadUpdate: vi.fn(),
  quitAndInstall: vi.fn(),
  getVersion: vi.fn(() => "0.3.17"),
  userDataPath: "",
  appHandlers: new Map<string, Handler>(),
  quit: vi.fn(),
  showMessageBox: vi.fn(async () => ({ response: 0 })),
}));

vi.mock("electron-updater", () => {
  const autoUpdater = {
    autoDownload: false,
    autoInstallOnAppQuit: false,
    channel: undefined as string | undefined,
    allowDowngrade: false,
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
  return { autoUpdater };
});

vi.mock("electron", () => ({
  app: {
    getVersion: ctx.getVersion,
    getPath: vi.fn(() => ctx.userDataPath),
    on: vi.fn((event: string, handler: Handler) => ctx.appHandlers.set(event, handler)),
    quit: ctx.quit,
  },
  dialog: { showMessageBox: ctx.showMessageBox },
  BrowserWindow: class BrowserWindow {},
  ipcMain: {
    handle: ctx.ipcHandle,
  },
}));

vi.mock("./update-install-guard", () => ({ checkWindowsUpdateInstall: vi.fn(async () => ({ allowed: false, reason: "runtime_running" })) }));

import {
  configureMacX64UpdateChannel,
  setupAutoUpdater,
} from "./updater";
import { updaterPreferencesPath } from "./updater-preferences";

const clear = { allowed: true } as const;
const blocked = { allowed: false, reason: "runtime_running" } as const;
function quitEvent() {
  return { defaultPrevented: false, preventDefault() { this.defaultPrevented = true; } };
}

describe("macOS x64 update channel", () => {
  it("does not touch established architecture paths", () => {
    for (const [platform, arch] of [
      ["darwin", "arm64"],
      ["win32", "x64"],
      ["win32", "arm64"],
      ["linux", "arm64"],
    ] as const) {
      const updater = { channel: null, allowDowngrade: true };

      configureMacX64UpdateChannel(updater, platform, arch);

      expect(updater).toEqual({ channel: null, allowDowngrade: true });
    }
  });

  it("does not enable downgrades when selecting an architecture feed", () => {
    const updater = { channel: null, allowDowngrade: true };

    configureMacX64UpdateChannel(updater, "darwin", "x64");

    expect(updater).toEqual({
      channel: "latest-x64",
      allowDowngrade: false,
    });
  });
});

function emitUpdater(event: string, ...args: unknown[]) {
  for (const handler of ctx.handlers.get(event) ?? []) {
    handler(...args);
  }
}

async function invokeIpc(channel: string, ...args: unknown[]) {
  const handler = ctx.ipcHandlers.get(channel);
  if (!handler) throw new Error(`Missing IPC handler: ${channel}`);
  return handler({}, ...args);
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
    ctx.userDataPath = mkdtempSync(join(tmpdir(), "multica-updater-test-"));
    ctx.handlers.clear();
    ctx.ipcHandlers.clear();
    ctx.ipcHandle.mockClear();
    ctx.ipcHandle.mockImplementation((channel: string, handler: IpcHandler) => {
      ctx.ipcHandlers.set(channel, handler);
    });
    ctx.checkForUpdates.mockClear();
    ctx.downloadUpdate.mockClear();
    ctx.quitAndInstall.mockClear();
    ctx.getVersion.mockClear();
    ctx.appHandlers.clear();
    ctx.quit.mockReset();
    ctx.quit.mockImplementation(() => {
      const before = quitEvent();
      ctx.appHandlers.get("before-quit")?.(before);
      if (before.defaultPrevented) return;
      const will = quitEvent();
      ctx.appHandlers.get("will-quit")?.(will);
      if (!will.defaultPrevented) ctx.appHandlers.get("quit")?.({}, 0);
    });
    autoUpdater.autoInstallOnAppQuit = true;
    ctx.showMessageBox.mockClear();
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    rmSync(ctx.userDataPath, { recursive: true, force: true });
  });

  it("enables automatic background updates by default", async () => {
    setupAutoUpdater(() => null);

    await expect(invokeIpc("updater:get-preferences")).resolves.toEqual({
      automaticUpdates: true,
    });

    await vi.advanceTimersByTimeAsync(5_000);
    expect(ctx.checkForUpdates).toHaveBeenCalledTimes(1);
  });

  it.each([true, false])("guards install-on-quit without stopping an agent (safe=%s)", async (safe) => {
    setupAutoUpdater(() => null, async () => safe ? clear : blocked, true);
    await invokeIpc("updater:get-preferences");
    emitUpdater("update-downloaded", { version: "0.4.37" });
    const preventDefault = vi.fn();
    ctx.appHandlers.get("before-quit")!({ preventDefault });
    await vi.advanceTimersByTimeAsync(0);
    expect(preventDefault).toHaveBeenCalledOnce();
    if (safe) expect(ctx.quitAndInstall).toHaveBeenCalledExactlyOnceWith(true, false);
    else {
      expect(ctx.quitAndInstall).not.toHaveBeenCalled();
      expect(ctx.quit).toHaveBeenCalledOnce();
    }
    expect(ctx.quit).toHaveBeenCalledOnce();
  });

  it("defers an explicit install when runtime status is unknown", async () => {
    setupAutoUpdater(() => null, async () => { throw new Error("probe failed"); }, true);
    await invokeIpc("updater:get-preferences");
    emitUpdater("update-downloaded", { version: "0.4.37" });
    await invokeIpc("updater:install");
    expect(ctx.quitAndInstall).not.toHaveBeenCalled();
    expect(ctx.quit).not.toHaveBeenCalled();
    expect(await invokeIpc("updater:get-install-state")).toMatchObject({ status: "deferred", reason: "probe_failed", diagnostic: "probe_failed" });
  });

  it("installs a downloaded update once after a successful explicit runtime check", async () => {
    const check = vi.fn(async () => clear);
    setupAutoUpdater(() => null, check, true);
    await invokeIpc("updater:get-preferences");
    await invokeIpc("updater:install");
    expect(check).not.toHaveBeenCalled();
    emitUpdater("update-downloaded", { version: "0.4.37" });
    await invokeIpc("updater:install");
    expect(ctx.quitAndInstall).toHaveBeenCalledExactlyOnceWith(false, true);
  });

  it("honors a quit received during an explicit check even when installation is deferred", async () => {
    let resolve!: (result: UpdateInstallCheck) => void;
    const check = vi.fn(() => new Promise<UpdateInstallCheck>((done) => { resolve = done; }));
    setupAutoUpdater(() => null, check, true);
    await invokeIpc("updater:get-preferences");
    emitUpdater("update-downloaded", { version: "0.4.37" });
    const installing = invokeIpc("updater:install");
    const preventDefault = vi.fn();
    ctx.appHandlers.get("before-quit")!({ preventDefault });
    resolve(blocked);
    await installing;
    expect(check).toHaveBeenCalledOnce();
    expect(ctx.quit).toHaveBeenCalledOnce();
    expect(ctx.quitAndInstall).not.toHaveBeenCalled();
    expect(ctx.showMessageBox).not.toHaveBeenCalled();
  });

  it("rechecks a later quit when another listener cancelled the resumed quit", async () => {
    ctx.quit.mockImplementation(() => {});
    const check = vi.fn(async () => blocked);
    setupAutoUpdater(() => null, check, true);
    await invokeIpc("updater:get-preferences");
    emitUpdater("update-downloaded", { version: "0.4.37" });
    ctx.appHandlers.get("before-quit")!({ preventDefault: vi.fn() });
    await vi.advanceTimersByTimeAsync(0);
    // app.quit returned without a final quit (e.g. a different close listener
    // vetoed it). A later user attempt must not inherit a permanent permit.
    ctx.appHandlers.get("before-quit")!({ preventDefault: vi.fn() });
    await vi.advanceTimersByTimeAsync(0);
    expect(check).toHaveBeenCalledTimes(2);
    expect(ctx.quitAndInstall).not.toHaveBeenCalled();
  });

  it("does not launch the installer before the final successful quit", async () => {
    ctx.quit.mockImplementation(() => {});
    setupAutoUpdater(() => null, async () => clear, true);
    await invokeIpc("updater:get-preferences");
    emitUpdater("update-downloaded", { version: "0.4.37" });
    await invokeIpc("updater:install");
    expect(ctx.quitAndInstall).not.toHaveBeenCalled();
    ctx.appHandlers.get("will-quit")!({ preventDefault: vi.fn() });
    ctx.appHandlers.get("quit")!({}, 0);
    expect(ctx.quitAndInstall).toHaveBeenCalledExactlyOnceWith(false, true);
  });

  it("rehydrates pending and deferred state instead of losing the download event", async () => {
    setupAutoUpdater(() => null, async () => blocked, true);
    await invokeIpc("updater:get-preferences");
    emitUpdater("update-downloaded", { version: "0.4.37" });
    expect(await invokeIpc("updater:get-install-state")).toEqual({ status: "ready", version: "0.4.37" });
    await invokeIpc("updater:install");
    expect(await invokeIpc("updater:get-install-state")).toMatchObject({ status: "deferred", version: "0.4.37" });
  });

  it("keeps checking and retains the latest download version during a probe", async () => {
    let resolve!: (result: UpdateInstallCheck) => void;
    setupAutoUpdater(() => null, () => new Promise<UpdateInstallCheck>((done) => { resolve = done; }), true);
    await invokeIpc("updater:get-preferences");
    emitUpdater("update-downloaded", { version: "0.4.37" });
    const installing = invokeIpc("updater:install");
    emitUpdater("update-downloaded", { version: "0.4.38" });
    expect(await invokeIpc("updater:get-install-state")).toEqual({ status: "checking", version: "0.4.38" });
    resolve(blocked);
    await installing;
    expect(await invokeIpc("updater:get-install-state")).toMatchObject({ status: "deferred", version: "0.4.38" });
  });

  it("coalesces duplicate install IPC and returns a visible checking/deferred state", async () => {
    let resolve!: (result: UpdateInstallCheck) => void;
    const check = vi.fn(() => new Promise<UpdateInstallCheck>((done) => { resolve = done; }));
    setupAutoUpdater(() => null, check, true);
    await invokeIpc("updater:get-preferences");
    emitUpdater("update-downloaded", { version: "0.4.37" });
    const first = invokeIpc("updater:install");
    const second = invokeIpc("updater:install");
    expect(await invokeIpc("updater:get-install-state")).toEqual({ status: "checking", version: "0.4.37" });
    resolve(blocked);
    expect(await first).toEqual(await second);
    expect(check).toHaveBeenCalledOnce();
    expect(ctx.quit).not.toHaveBeenCalled();
  });

  it("expires final-quit authorization when will-quit is vetoed", async () => {
    ctx.quit.mockImplementation(() => {});
    setupAutoUpdater(() => null, async () => clear, true);
    await invokeIpc("updater:get-preferences");
    emitUpdater("update-downloaded", { version: "0.4.37" });
    await invokeIpc("updater:install");
    const event = quitEvent();
    ctx.appHandlers.get("will-quit")!(event);
    event.preventDefault();
    ctx.appHandlers.get("quit")!({}, 0);
    expect(ctx.quitAndInstall).not.toHaveBeenCalled();
  });

  it("does not lose an asynchronous quit continuation from another listener", async () => {
    const check = vi.fn().mockResolvedValueOnce(clear).mockResolvedValueOnce(blocked);
    setupAutoUpdater(() => null, check, true);
    await invokeIpc("updater:get-preferences");
    emitUpdater("update-downloaded", { version: "0.4.37" });
    let attempts = 0;
    ctx.quit.mockImplementation(() => {
      if (attempts++ === 0) queueMicrotask(() => ctx.appHandlers.get("before-quit")!(quitEvent()));
      else {
        ctx.appHandlers.get("will-quit")!(quitEvent());
        ctx.appHandlers.get("quit")!({}, 0);
      }
    });
    await invokeIpc("updater:install");
    await vi.advanceTimersByTimeAsync(0);
    expect(check).toHaveBeenCalledTimes(2);
    expect(ctx.quit).toHaveBeenCalledTimes(2);
    expect(ctx.quitAndInstall).not.toHaveBeenCalled();
  });

  it.each([
    ["win32", "x64", true], ["win32", "arm64", true],
    ["darwin", "x64", false], ["darwin", "arm64", false], ["linux", "x64", false],
  ])("applies the default guard only on %s/%s", async (platform, arch, guarded) => {
    const platformDescriptor = Object.getOwnPropertyDescriptor(process, "platform")!;
    const archDescriptor = Object.getOwnPropertyDescriptor(process, "arch")!;
    try {
      Object.defineProperty(process, "platform", { value: platform });
      Object.defineProperty(process, "arch", { value: arch });
      setupAutoUpdater(() => null);
      await invokeIpc("updater:get-preferences");
      expect(ctx.appHandlers.has("before-quit")).toBe(guarded);
      expect(autoUpdater.autoInstallOnAppQuit).toBe(!guarded);
      if (guarded) {
        emitUpdater("update-downloaded", { version: "0.4.37" });
        await expect(invokeIpc("updater:install")).resolves.toMatchObject({
          status: "deferred", reason: "runtime_running",
        });
        expect(ctx.quitAndInstall).not.toHaveBeenCalled();
      }
    } finally {
      Object.defineProperty(process, "platform", platformDescriptor);
      Object.defineProperty(process, "arch", archDescriptor);
    }
  });

  it("skips startup and periodic checks when automatic updates are disabled", async () => {
    writeFileSync(
      updaterPreferencesPath(ctx.userDataPath),
      JSON.stringify({ automaticUpdates: false }),
    );
    setupAutoUpdater(() => null);

    // Let the async preference load settle before advancing timers; otherwise
    // the in-flight readFile can resolve after afterEach() removes the temp
    // dir, default back to enabled=true, and fire a background check into the
    // next test's freshly-cleared mock (flake on slow CI).
    await invokeIpc("updater:get-preferences");

    await vi.advanceTimersByTimeAsync(60 * 60 * 1000 + 5_000);

    expect(ctx.checkForUpdates).not.toHaveBeenCalled();
  });

  it("persists the automatic update preference and stops future background checks", async () => {
    setupAutoUpdater(() => null);

    await expect(
      invokeIpc("updater:set-automatic-updates", false),
    ).resolves.toEqual({ automaticUpdates: false });
    expect(
      JSON.parse(
        readFileSync(updaterPreferencesPath(ctx.userDataPath), "utf-8"),
      ),
    ).toEqual({ automaticUpdates: false });

    await vi.advanceTimersByTimeAsync(60 * 60 * 1000 + 5_000);
    expect(ctx.checkForUpdates).not.toHaveBeenCalled();
  });

  it("still allows an explicit manual check when automatic updates are disabled", async () => {
    writeFileSync(
      updaterPreferencesPath(ctx.userDataPath),
      JSON.stringify({ automaticUpdates: false }),
    );
    setupAutoUpdater(() => null);

    await expect(invokeIpc("updater:check")).resolves.toMatchObject({
      ok: true,
    });

    expect(ctx.checkForUpdates).toHaveBeenCalledTimes(1);
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
});
