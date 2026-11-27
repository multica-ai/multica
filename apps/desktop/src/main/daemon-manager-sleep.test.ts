// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const context = vi.hoisted(() => ({
  handlers: new Map<string, (...args: unknown[]) => unknown>(),
  exec: vi.fn(), health: vi.fn(), exists: vi.fn(), writeFile: vi.fn(),
  start: vi.fn(), stop: vi.fn(),
  preventSleep: "off" as unknown,
}));
vi.mock("electron", () => ({
  app: {
    getAppPath: () => "/test/desktop", getPreferredSystemLanguages: () => ["en"],
    on: vi.fn(), quit: vi.fn(), emit: vi.fn(),
  },
  dialog: { showMessageBox: vi.fn() },
  BrowserWindow: { getAllWindows: () => [] },
  ipcMain: {
    handle: (name: string, handler: (...args: unknown[]) => unknown) => { context.handlers.set(name, handler); },
    on: vi.fn(),
  },
  powerSaveBlocker: { start: context.start, stop: context.stop },
}));
vi.mock("child_process", () => ({ execFile: context.exec }));
vi.mock("fs", async (original) => ({ ...await original<typeof import("fs")>(), existsSync: context.exists }));
vi.mock("fs/promises", async (original) => ({
  ...await original<typeof import("fs/promises")>(),
  readFile: vi.fn(async (path: string) => {
    if (path.endsWith("daemon.pid")) throw Object.assign(new Error("No PID file"), { code: "ENOENT" });
    return path.endsWith("desktop_prefs.json")
      ? JSON.stringify({ autoStart: false, autoStop: false, preventSleep: context.preventSleep })
      : JSON.stringify({ server_url: "https://example.test" });
  }),
  writeFile: context.writeFile, mkdir: vi.fn(),
}));
vi.mock("./cli-bootstrap", () => ({ ensureManagedCli: vi.fn(), managedCliPath: () => "/test/managed/multica" }));
vi.mock("./daemon-port", () => ({ probeProfileHealth: context.health, readDaemonPortRecord: vi.fn(async () => null) }));

const POLL_INTERVAL_MS = 5_000;

function daemonReports(activeTasks: number) {
  context.health.mockResolvedValue({ status: "running", active_task_count: activeTasks });
}
async function setup() {
  const { setupDaemonManager } = await import("./daemon-manager");
  setupDaemonManager(() => null);
  await context.handlers.get("daemon:set-target-api-url")!({}, "https://example.test");
  await context.handlers.get("daemon:is-cli-installed")!({});
  await poll();
}
async function poll() {
  await vi.advanceTimersByTimeAsync(POLL_INTERVAL_MS);
}

describe("desktop blocks idle sleep per the prevent-sleep preference", () => {
  beforeEach(() => {
    vi.resetModules(); vi.clearAllMocks(); vi.useFakeTimers(); context.handlers.clear();
    context.preventSleep = "off";
    context.exists.mockReturnValue(true);
    context.start.mockReturnValue(7);
    daemonReports(0);
    context.exec.mockImplementation((_bin: string, args: string[], _options: unknown, callback: (error: Error | null, output?: string) => void) => {
      callback(null, args[0] === "version" ? '{"version":"test"}' : undefined);
    });
    vi.spyOn(console, "log").mockImplementation(() => {});
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); vi.restoreAllMocks(); });

  it("never blocks sleep when the preference is off", async () => {
    daemonReports(2);
    await setup();
    expect(context.start).not.toHaveBeenCalled();
  });

  it("treats an unrecognized stored value as off", async () => {
    context.preventSleep = "always";
    daemonReports(2);
    await setup();
    expect(context.start).not.toHaveBeenCalled();
  });

  it("blocks sleep from launch in on mode, with no agent working", async () => {
    context.preventSleep = "on";
    await setup();
    expect(context.start).toHaveBeenCalledExactlyOnceWith("prevent-app-suspension");
    expect(context.stop).not.toHaveBeenCalled();
  });

  it("holds the block in agent mode only while the daemon has tasks in flight", async () => {
    context.preventSleep = "agent";
    await setup();
    expect(context.start).not.toHaveBeenCalled();

    daemonReports(1);
    await poll();
    expect(context.start).toHaveBeenCalledExactlyOnceWith("prevent-app-suspension");

    await poll();
    expect(context.start).toHaveBeenCalledTimes(1);
    expect(context.stop).not.toHaveBeenCalled();

    daemonReports(0);
    await poll();
    expect(context.stop).toHaveBeenCalledExactlyOnceWith(7);
  });

  it("releases the agent-mode block when the daemon goes away", async () => {
    context.preventSleep = "agent";
    daemonReports(1);
    await setup();
    expect(context.start).toHaveBeenCalledTimes(1);

    context.health.mockResolvedValue(null);
    await poll();
    expect(context.stop).toHaveBeenCalledExactlyOnceWith(7);
  });

  it("applies and persists a changed preference without a restart", async () => {
    await setup();
    const setPrefs = context.handlers.get("daemon:set-prefs")!;

    await expect(setPrefs({}, { preventSleep: "on" })).resolves.toMatchObject({ preventSleep: "on" });
    expect(context.start).toHaveBeenCalledTimes(1);
    expect(JSON.parse(context.writeFile.mock.calls.at(-1)![1] as string)).toMatchObject({ preventSleep: "on" });

    await expect(setPrefs({}, { preventSleep: "bogus" })).resolves.toMatchObject({ preventSleep: "off" });
    expect(context.stop).toHaveBeenCalledExactlyOnceWith(7);
  });
});
