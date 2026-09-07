// @vitest-environment node

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BrowserWindow } from "electron";
import type { DaemonStatus } from "../shared/daemon-types";

type IpcHandler = (...args: unknown[]) => unknown;
type ExecCallback = (error: Error | null, stdout: string, stderr: string) => void;

const ctx = vi.hoisted(() => ({
  handlers: new Map<string, IpcHandler>(),
  execFile: vi.fn(),
  send: vi.fn(),
  health: null as Record<string, unknown> | null,
}));

vi.mock("electron", () => ({
  app: { getAppPath: () => "/fake/app", on: vi.fn() },
  BrowserWindow: {},
  shell: {},
  ipcMain: {
    handle: (name: string, handler: IpcHandler) => ctx.handlers.set(name, handler),
    on: vi.fn(),
  },
}));
vi.mock("child_process", () => ({ execFile: ctx.execFile }));
vi.mock("fs", () => ({ existsSync: () => false, watchFile: vi.fn(), unwatchFile: vi.fn() }));
vi.mock("fs/promises", () => ({
  readFile: vi.fn(async () => "{}"),
  writeFile: vi.fn(async () => {}),
  mkdir: vi.fn(async () => {}),
  rm: vi.fn(async () => {}),
  open: vi.fn(),
  stat: vi.fn(),
}));
vi.mock("./cli-bootstrap", () => ({
  managedCliPath: () => "/fake/multica",
  ensureManagedCli: async () => "/fake/multica",
}));

const installOutput = JSON.stringify({
  provider: "pi", version: "0.83.0", path: "/fake/pi", source: "managed", installed: true,
});

function invoke(name: string, ...args: unknown[]) {
  const handler = ctx.handlers.get(name);
  if (!handler) throw new Error(`Missing IPC handler: ${name}`);
  return Promise.resolve(handler({}, ...args));
}

function commands() {
  return ctx.execFile.mock.calls.map((call) => (call[1] as string[]).slice(0, 2).join(" "));
}

beforeEach(async () => {
  vi.resetModules();
  vi.useFakeTimers();
  ctx.handlers.clear();
  ctx.send.mockReset();
  ctx.health = null;
  ctx.execFile.mockReset().mockImplementation((_bin: string, args: string[], _options: unknown, callback: ExecCallback) => {
    callback(null, args[0] === "version" ? JSON.stringify({ version: "0.4.40" }) : installOutput, "");
  });
  vi.stubGlobal("fetch", vi.fn(async () => ({ ok: ctx.health !== null, json: async () => ctx.health })));
  const { setupDaemonManager } = await import("./daemon-manager");
  setupDaemonManager(() => ({ webContents: { send: ctx.send } }) as unknown as BrowserWindow);
  await vi.waitFor(async () => {
    expect(await invoke("daemon:get-status")).toMatchObject({ state: "stopped" });
  });
  await invoke("daemon:set-target-api-url", "https://api.example.test");
});

afterEach(() => {
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("Desktop managed runtime installation", () => {
  it("does not install on startup and starts the daemon after an explicit install", async () => {
    expect(commands()).not.toContain("daemon install-runtime");
    expect(await invoke("daemon:install-runtime", "pi")).toEqual({ success: true });
    const calls = commands();
    expect(calls.indexOf("daemon install-runtime")).toBeLessThan(calls.indexOf("daemon start"));
    expect(ctx.execFile.mock.calls.find((call) => call[1][1] === "start")?.[1]).toEqual([
      "daemon", "start", "--profile", "desktop-api.example.test",
    ]);
  });

  it("leaves an already-running daemon and its active tasks running", async () => {
    ctx.health = { status: "running", os: process.platform === "win32" ? "windows" : process.platform, active_task_count: 2 };
    expect(await invoke("daemon:install-runtime", "pi")).toEqual({ success: true });
    expect(commands()).not.toContain("daemon stop");
    expect(commands()).not.toContain("daemon start");
  });

  it("publishes a start failure so registration cannot spin forever", async () => {
    ctx.execFile.mockImplementation((_bin: string, args: string[], _options: unknown, callback: ExecCallback) => {
      callback(args[1] === "start" ? new Error("Sign in again") : null, installOutput, "");
    });
    expect(await invoke("daemon:install-runtime", "pi")).toEqual({ success: false, error: "Sign in again" });
    const status = await invoke("daemon:get-status") as DaemonStatus;
    expect(status.managedRuntimeSetup).toMatchObject({ phase: "failed", error: "Sign in again" });
  });

  it("does not install a native binary for a daemon running in another OS", async () => {
    ctx.health = { status: "running", os: process.platform === "linux" ? "windows" : "linux" };
    expect(await invoke("daemon:install-runtime", "pi")).toMatchObject({ success: false });
    expect(commands()).not.toContain("daemon install-runtime");
    expect(commands()).not.toContain("daemon start");
  });

  it("honors a stop requested while the runtime is downloading", async () => {
    let finishInstall: ExecCallback | undefined;
    ctx.execFile.mockImplementation((_bin: string, args: string[], _options: unknown, callback: ExecCallback) => {
      if (args[1] === "install-runtime") finishInstall = callback;
      else callback(null, "", "");
    });
    const installation = invoke("daemon:install-runtime", "pi");
    await vi.waitFor(() => expect(finishInstall).toBeDefined());
    const stop = invoke("daemon:stop");
    finishInstall!(null, installOutput, "");
    expect(await installation).toMatchObject({ success: false });
    await stop;
    expect(commands()).not.toContain("daemon start");
    expect(commands()).toContain("daemon stop");
  });
});
