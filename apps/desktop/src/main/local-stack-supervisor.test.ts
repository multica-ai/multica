import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BrowserWindow, WebContents } from "electron";
import type { LocalStackState } from "../shared/local-stack";
import type { LocalStackConfig } from "./local-stack-config";

type IpcHandler = (...args: unknown[]) => unknown;
type SyncIpcListener = (event: { returnValue?: unknown }) => void;

const config: LocalStackConfig = {
  repoDir: "/repo",
  composeFile: "docker-compose.selfhost.yml",
  backendPort: 8081,
};

type ExecCallback = (
  err: Error | null,
  stdout: string,
  stderr: string,
) => void;

const ctx = vi.hoisted(() => ({
  ipcHandlers: new Map<string, (...args: unknown[]) => unknown>(),
  syncListeners: new Map<string, (event: { returnValue?: unknown }) => void>(),
  ipcHandle: vi.fn(),
  ipcOn: vi.fn(),
  loadConfig: vi.fn(),
  execFile: vi.fn(),
}));

vi.mock("electron", () => ({
  ipcMain: {
    handle: ctx.ipcHandle,
    on: ctx.ipcOn,
  },
}));

// The real loader reads ~/.multica/desktop-local-stack.json, which exists on
// the machine this feature targets. Stub it so the suite never depends on it.
vi.mock("./local-stack-config", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("./local-stack-config")>();
  return {
    ...actual,
    localStackConfigPath: () => "/nonexistent/desktop-local-stack.json",
    loadLocalStackConfig: ctx.loadConfig,
  };
});

// Nothing in the default suite may execute colima or docker. Both the named
// and default shapes are replaced so the interop cannot reach the real one.
vi.mock("child_process", async (importOriginal) => {
  const actual = await importOriginal<typeof import("child_process")>();
  const mocked = { ...actual, execFile: ctx.execFile };
  return { ...mocked, default: mocked };
});

import { setupLocalStack } from "./local-stack";

function invoke(channel: string, ...args: unknown[]): unknown {
  const handler = ctx.ipcHandlers.get(channel);
  if (!handler) throw new Error(`Missing IPC handler: ${channel}`);
  return handler({}, ...args);
}

function readSync(channel: string): unknown {
  const listener = ctx.syncListeners.get(channel);
  if (!listener) throw new Error(`Missing sync IPC listener: ${channel}`);
  const event: { returnValue?: unknown } = {};
  listener(event);
  return event.returnValue;
}

function makeWindow() {
  const send = vi.fn();
  return {
    win: {
      isDestroyed: () => false,
      webContents: { isDestroyed: () => false, send },
    } as unknown as BrowserWindow,
    send,
  };
}

/** Mirrors updater.test.ts: touching webContents on a dead window throws. */
function makeDestroyedWindow(): BrowserWindow {
  return {
    isDestroyed: () => true,
    get webContents(): WebContents {
      throw new TypeError("Object has been destroyed");
    },
  } as unknown as BrowserWindow;
}

function sentStates(send: ReturnType<typeof vi.fn>): LocalStackState[] {
  return send.mock.calls.map(([, state]) => state as LocalStackState);
}

/** Every command succeeds; colima reports itself as already running. */
function allCommandsSucceed() {
  ctx.execFile.mockImplementation(
    (bin: string, args: string[], _opts: unknown, cb: ExecCallback) => {
      const stdout = bin === "colima" && args[0] === "status" ? "running" : "";
      cb(null, stdout, "");
    },
  );
}

describe("setupLocalStack", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    ctx.ipcHandlers.clear();
    ctx.syncListeners.clear();
    ctx.ipcHandle.mockReset();
    ctx.ipcOn.mockReset();
    ctx.loadConfig.mockReset();
    ctx.execFile.mockReset();
    ctx.ipcHandle.mockImplementation((channel: string, handler: IpcHandler) => {
      ctx.ipcHandlers.set(channel, handler);
    });
    ctx.ipcOn.mockImplementation((channel: string, listener: SyncIpcListener) => {
      ctx.syncListeners.set(channel, listener);
    });
    // Backend never answers, so the bring-up always reaches the backend poll.
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => {
        throw new Error("connection refused");
      }),
    );
    ctx.loadConfig.mockResolvedValue(config);
    allCommandsSucceed();
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  // CRITICAL: a malformed ~/.multica/desktop.json leaves main with no apiUrl.
  // The IPC surface must still exist, and must resolve open, or the renderer
  // blocks forever on a button-less overlay instead of showing the config error
  // screen that names the file.
  describe("without a runtime config to gate on", () => {
    it("still registers the synchronous initial-state channel", () => {
      setupLocalStack(() => null, null);

      expect(ctx.syncListeners.has("local-stack:get-initial-state")).toBe(true);
      expect(ctx.ipcHandlers.has("local-stack:get-state")).toBe(true);
      expect(ctx.ipcHandlers.has("local-stack:retry")).toBe(true);
      expect(ctx.ipcHandlers.has("local-stack:skip")).toBe(true);
    });

    it("resolves ready synchronously, before any renderer can read it", () => {
      setupLocalStack(() => null, null);

      expect(readSync("local-stack:get-initial-state")).toEqual({
        phase: "ready",
      });
      expect(ctx.loadConfig).not.toHaveBeenCalled();
    });
  });

  it("never touches docker for a SaaS apiUrl", async () => {
    const { win, send } = makeWindow();
    setupLocalStack(() => win, "https://api.multica.ai");

    await vi.advanceTimersByTimeAsync(0);

    expect(sentStates(send)).toEqual([{ phase: "ready" }]);
    expect(ctx.execFile).not.toHaveBeenCalled();
    expect(ctx.loadConfig).not.toHaveBeenCalled();
  });

  // IMPORTANT 2. Skip is pressed most often during the backend step, which then
  // polls for up to 90 more seconds. A late `failed` would re-block the window
  // and unmount CoreProvider (React Query cache, WebSocket, editor state)
  // mid-session with no user action.
  it("keeps a late bring-up failure from re-blocking after skip", async () => {
    const { win, send } = makeWindow();
    setupLocalStack(() => win, "http://localhost:8081");

    // Let the run reach the backend poll.
    await vi.advanceTimersByTimeAsync(2_000);
    expect(sentStates(send).at(-1)).toEqual({
      phase: "running",
      step: "backend",
    });

    await invoke("local-stack:skip");
    expect(sentStates(send).at(-1)).toEqual({ phase: "ready" });
    expect(readSync("local-stack:get-initial-state")).toEqual({
      phase: "ready",
    });

    // Run the abandoned bring-up past its 90s ceiling.
    await vi.advanceTimersByTimeAsync(120_000);

    expect(sentStates(send).filter((s) => s.phase === "failed")).toEqual([]);
    expect(sentStates(send).at(-1)).toEqual({ phase: "ready" });
    expect(readSync("local-stack:get-initial-state")).toEqual({
      phase: "ready",
    });
  });

  // IMPORTANT 6. Both buttons are on screen between the click and the first
  // `running` broadcast, so a double-click is enough. Two concurrent
  // `compose up` runs on the same project produce container-name conflicts, and
  // the two runs then race to set the state.
  it("coalesces concurrent retries onto a single bring-up", async () => {
    const { win } = makeWindow();
    setupLocalStack(() => win, "http://localhost:8081");
    await vi.advanceTimersByTimeAsync(200_000);

    const composeCallsBefore = composeUpCount();
    const first = invoke("local-stack:retry");
    const second = invoke("local-stack:retry");
    expect(first).toBe(second);

    await vi.advanceTimersByTimeAsync(200_000);

    expect(composeUpCount() - composeCallsBefore).toBe(1);
  });

  it("starts a fresh bring-up once the previous one has settled", async () => {
    const { win } = makeWindow();
    setupLocalStack(() => win, "http://localhost:8081");
    await vi.advanceTimersByTimeAsync(200_000);

    const composeCallsBefore = composeUpCount();
    const rerun = invoke("local-stack:retry") as Promise<LocalStackState>;
    await vi.advanceTimersByTimeAsync(200_000);
    await rerun;

    expect(composeUpCount() - composeCallsBefore).toBe(1);
  });

  // IMPORTANT 4. A throw out of the broadcast would reject the bring-up, land
  // as an unhandled rejection off `void start()`, and freeze the overlay on the
  // last running step with no Retry and no Skip until relaunch.
  describe("broadcasting into a dying window", () => {
    it("keeps advancing state when the window is already destroyed", async () => {
      setupLocalStack(() => makeDestroyedWindow(), "http://localhost:8081");

      await vi.advanceTimersByTimeAsync(120_000);

      expect(readSync("local-stack:get-initial-state")).toEqual({
        phase: "failed",
        step: "backend",
        message: expect.stringMatching(/did not respond/i),
      });
    });

    it("swallows a destroyed-object throw from send", async () => {
      const send = vi.fn(() => {
        throw new TypeError("Object has been destroyed");
      });
      const win = {
        isDestroyed: () => false,
        webContents: { isDestroyed: () => false, send },
      } as unknown as BrowserWindow;

      setupLocalStack(() => win, "http://localhost:8081");
      await vi.advanceTimersByTimeAsync(120_000);

      expect(readSync("local-stack:get-initial-state")).toMatchObject({
        phase: "failed",
        step: "backend",
      });
    });

    it("turns an unexpected send failure into a failed state, not a rejection", async () => {
      const send = vi.fn(() => {
        throw new Error("kaboom");
      });
      const win = {
        isDestroyed: () => false,
        webContents: { isDestroyed: () => false, send },
      } as unknown as BrowserWindow;

      setupLocalStack(() => win, "http://localhost:8081");
      await vi.advanceTimersByTimeAsync(120_000);

      // The very first broadcast throws, and it is attributed to the step that
      // broadcast was carrying rather than being lost as a rejection.
      expect(readSync("local-stack:get-initial-state")).toEqual({
        phase: "failed",
        step: "probe",
        message: "kaboom",
      });
    });
  });

  it("reports a broken supervisor config on the config step", async () => {
    ctx.loadConfig.mockRejectedValue(
      new Error("local stack config: repoDir is required"),
    );
    const { win, send } = makeWindow();

    setupLocalStack(() => win, "http://localhost:8081");
    await vi.advanceTimersByTimeAsync(0);

    expect(sentStates(send).at(-1)).toEqual({
      phase: "failed",
      step: "config",
      message: "local stack config: repoDir is required",
    });
  });
});

function composeUpCount(): number {
  return ctx.execFile.mock.calls.filter(
    ([bin, args]) =>
      bin === "docker" && (args as string[]).includes("up"),
  ).length;
}
