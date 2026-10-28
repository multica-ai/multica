// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const context = vi.hoisted(() => ({
  handlers: new Map<string, (...args: unknown[]) => unknown>(),
  beforeQuit: null as null | ((event: { preventDefault: () => void }) => void),
  quit: vi.fn(), emit: vi.fn(), dialog: vi.fn(), exec: vi.fn(), health: vi.fn(), readPort: vi.fn(), exists: vi.fn(), install: vi.fn(),
  pid: null as number | null,
  autoStart: false, autoStop: true, locale: "en", holdStart: false,
  windows: [] as Array<{
    isDestroyed: () => boolean; isVisible: () => boolean;
    hide: () => void; show: () => void;
  }>,
  finishStart: null as null | (() => void),
  finishStop: null as null | ((error?: Error) => void),
}));
vi.mock("electron", () => ({
  app: {
    getAppPath: () => "/test/desktop", getPreferredSystemLanguages: () => [context.locale],
    on: (_event: string, handler: (event: { preventDefault: () => void }) => void) => { context.beforeQuit = handler; },
    quit: context.quit, emit: context.emit,
  },
  dialog: { showMessageBox: context.dialog },
  BrowserWindow: { getAllWindows: () => context.windows },
  ipcMain: {
    handle: (name: string, handler: (...args: unknown[]) => unknown) => { context.handlers.set(name, handler); },
    on: vi.fn(),
  },
}));
vi.mock("child_process", () => ({ execFile: context.exec }));
vi.mock("fs", async (original) => ({ ...await original<typeof import("fs")>(), existsSync: context.exists }));
vi.mock("fs/promises", async (original) => ({
  ...await original<typeof import("fs/promises")>(),
  readFile: vi.fn(async (path: string) => {
    if (path.endsWith("daemon.pid")) {
      if (context.pid !== null) return String(context.pid);
      throw Object.assign(new Error("No PID file"), { code: "ENOENT" });
    }
    return path.endsWith("desktop_prefs.json")
      ? JSON.stringify({ autoStart: context.autoStart, autoStop: context.autoStop })
      : JSON.stringify({ server_url: "https://example.test" });
  }),
  writeFile: vi.fn(), mkdir: vi.fn(),
}));
vi.mock("./cli-bootstrap", () => ({ ensureManagedCli: context.install, managedCliPath: () => "/test/managed/multica" }));
vi.mock("./daemon-port", () => ({ probeProfileHealth: context.health, readDaemonPortRecord: context.readPort }));

async function setup() {
  const { setupDaemonManager } = await import("./daemon-manager");
  setupDaemonManager(() => null);
  await context.handlers.get("daemon:set-target-api-url")!({}, "https://example.test");
  await context.handlers.get("daemon:is-cli-installed")!({});
  context.exec.mockClear();
}
async function setupInstallingCLI() {
  context.exists.mockReturnValue(false);
  context.health.mockResolvedValue(null);
  context.install.mockReturnValue(new Promise(() => {}));
  const { setupDaemonManager } = await import("./daemon-manager");
  setupDaemonManager(() => null);
  await context.handlers.get("daemon:set-target-api-url")!({}, "https://example.test");
}
function requestQuit() {
  const event = { preventDefault: vi.fn() };
  context.beforeQuit!(event);
  expect(event.preventDefault).toHaveBeenCalledOnce();
}

function addWindow(initiallyVisible = true) {
  let visible = initiallyVisible;
  const window = {
    isDestroyed: vi.fn(() => false), isVisible: () => visible,
    hide: vi.fn(() => { visible = false; }),
    show: vi.fn(() => { visible = true; }),
  };
  context.windows.push(window);
  return window;
}

describe("desktop quit uses the CLI lifecycle", () => {
  beforeEach(() => {
    vi.resetModules(); vi.clearAllMocks(); vi.useFakeTimers(); context.handlers.clear();
    context.autoStart = false; context.autoStop = true; context.locale = "en"; context.holdStart = false;
    context.windows = [];
    context.pid = null; context.readPort.mockResolvedValue(null);
    context.exists.mockReturnValue(true); context.install.mockReset();
    context.finishStart = null; context.finishStop = null;
    context.health.mockResolvedValue({ status: "running", active_task_count: 1 });
    context.dialog.mockReset().mockResolvedValue({ response: 1 });
    context.exec.mockImplementation((_bin: string, args: string[], _options: unknown, callback: (error: Error | null, output?: string) => void) => {
      if (args[0] === "version") callback(null, '{"version":"test"}');
      else if (args[1] === "start" && context.holdStart) context.finishStart = () => callback(null);
      else if (args[1] === "stop") context.finishStop = (error) => callback(error ?? null);
      else callback(null);
    });
    vi.spyOn(console, "log").mockImplementation(() => {});
    vi.spyOn(console, "warn").mockImplementation(() => {});
  });
  afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); vi.restoreAllMocks(); });

  it("leaves an already prevented quit to the Windows updater", async () => {
    await setup();
    const event = { defaultPrevented: true, preventDefault: vi.fn() };
    context.beforeQuit!(event);
    await vi.advanceTimersByTimeAsync(0);
    expect(event.preventDefault).not.toHaveBeenCalled();
    expect(context.dialog).not.toHaveBeenCalled();
    expect(context.exec).not.toHaveBeenCalled();
    expect(context.quit).not.toHaveBeenCalled();
  });

  it("allows the updater-owned final quit without a second daemon stop", async () => {
    await setup();
    const manager = await import("./daemon-manager");
    await expect(manager.prepareDaemonForUpdateInstall({ interruptActiveTasks: false }))
      .resolves.toEqual({ kind: "busy", activeTasks: 1 });
    const event = { defaultPrevented: false, preventDefault: vi.fn() };
    context.beforeQuit!(event);
    await vi.advanceTimersByTimeAsync(0);
    expect(event.preventDefault).not.toHaveBeenCalled();
    expect(context.dialog).not.toHaveBeenCalled();
    expect(context.exec).not.toHaveBeenCalled();
    expect(context.quit).not.toHaveBeenCalled();
    manager.resumeDaemonAfterAbortedUpdateInstall();
  });

  it("shows English confirmation and waits for the profile-scoped CLI result", async () => {
    await setup(); requestQuit();
    await vi.waitFor(() => expect(context.finishStop).toBeTypeOf("function"));
    expect(context.dialog).toHaveBeenCalledWith(expect.objectContaining({
      message: "1 run is active on this device.",
      buttons: ["Cancel", "Stop now and quit"], defaultId: 0,
    }));
    expect(context.exec).toHaveBeenCalledWith(expect.any(String), ["daemon", "stop", "--force", "--profile", "desktop-example.test"], expect.objectContaining({ timeout: 55_000 }), expect.any(Function));
    expect(context.quit).not.toHaveBeenCalled();
    context.finishStop!();
    await vi.waitFor(() => expect(context.quit).toHaveBeenCalledOnce());
  });

  it("cancels the Chinese task warning without issuing a stop", async () => {
    context.locale = "zh-CN"; context.dialog.mockResolvedValue({ response: 0 });
    await setup(); requestQuit();
    await vi.waitFor(() => expect(context.emit).toHaveBeenCalledWith("activate"));
    expect(context.dialog).toHaveBeenCalledWith(expect.objectContaining({ buttons: ["取消", "立即停止并退出"] }));
    expect(context.exec).not.toHaveBeenCalled(); expect(context.quit).not.toHaveBeenCalled();
  });

  it("offers ignore-and-quit only after the CLI reports failure", async () => {
    await setup(); requestQuit();
    await vi.waitFor(() => expect(context.finishStop).toBeTypeOf("function"));
    context.finishStop!(new Error("daemon was forcibly terminated; task cleanup could not be confirmed"));
    await vi.waitFor(() => expect(context.quit).toHaveBeenCalledOnce());
    expect(context.dialog).toHaveBeenLastCalledWith(expect.objectContaining({
      message: "Could not confirm whether the daemon and its runs have stopped.",
      detail: expect.stringContaining("automatic daemon restart disabled"),
      buttons: ["Cancel quit", "Ignore and quit"], defaultId: 0,
    }));
  });

  it("hides visible windows during shutdown and does not restore them on success", async () => {
    const main = addWindow();
    const issue = addWindow();
    const hidden = addWindow(false);
    await setup(); requestQuit();
    await vi.waitFor(() => expect(context.finishStop).toBeTypeOf("function"));
    expect(main.hide).toHaveBeenCalledOnce();
    expect(issue.hide).toHaveBeenCalledOnce();
    expect(hidden.hide).not.toHaveBeenCalled();
    requestQuit();
    expect(main.hide).toHaveBeenCalledOnce();
    expect(context.quit).not.toHaveBeenCalled();
    context.finishStop!();
    await vi.waitFor(() => expect(context.quit).toHaveBeenCalledOnce());
    expect(main.show).not.toHaveBeenCalled();
    expect(issue.show).not.toHaveBeenCalled();
  });

  it("keeps windows visible when the interruption warning is cancelled", async () => {
    const window = addWindow();
    context.dialog.mockResolvedValue({ response: 0 });
    await setup(); requestQuit();
    await vi.waitFor(() => expect(context.emit).toHaveBeenCalledWith("activate"));
    expect(window.hide).not.toHaveBeenCalled();
    expect(window.isVisible()).toBe(true);
  });

  it("restores visible windows before reporting failed cleanup and keeps the daemon stopped after cancellation", async () => {
    const main = addWindow();
    const hidden = addWindow(false);
    context.dialog.mockResolvedValueOnce({ response: 1 });
    context.dialog.mockImplementationOnce(async () => {
      expect(main.show).toHaveBeenCalledOnce();
      expect(main.isVisible()).toBe(true);
      expect(hidden.show).not.toHaveBeenCalled();
      return { response: 0 };
    });
    await setup(); requestQuit();
    await vi.waitFor(() => expect(context.finishStop).toBeTypeOf("function"));
    context.health.mockResolvedValue(null);
    context.finishStop!(new Error("daemon was forcibly terminated; task cleanup could not be confirmed"));
    await vi.waitFor(() => expect(context.emit).toHaveBeenCalledWith("activate"));
    expect(context.dialog).toHaveBeenLastCalledWith(expect.objectContaining({
      message: "The daemon has stopped, but its runs could not be confirmed to have exited.",
      detail: expect.stringContaining("the daemon remains stopped"),
    }));
    await vi.advanceTimersByTimeAsync(60_000);
    expect(context.exec.mock.calls.filter(([, args]) => args[1] === "start")).toHaveLength(0);
    expect(context.quit).not.toHaveBeenCalled();
  });

  it("does not prompt for an idle daemon", async () => {
    context.health.mockResolvedValue({ status: "running", active_task_count: 0 });
    const window = addWindow();
    await setup(); requestQuit();
    await vi.waitFor(() => expect(context.finishStop).toBeTypeOf("function"));
    expect(context.dialog).not.toHaveBeenCalled();
    expect(window.hide).toHaveBeenCalledOnce();
    context.finishStop!();
    await vi.waitFor(() => expect(context.quit).toHaveBeenCalledOnce());
  });

  it.each([undefined, -1, 1.5, Infinity, "2"])("uses unknown-state copy for an unconfirmed run count %s", async (count) => {
    context.health.mockResolvedValue({ status: "running", active_task_count: count });
    context.dialog.mockResolvedValue({ response: 0 });
    await setup(); requestQuit();
    await vi.waitFor(() => expect(context.dialog).toHaveBeenCalledWith(expect.objectContaining({
      message: "Could not confirm whether any runs are active on this device.",
    })));
    expect(context.exec).not.toHaveBeenCalled();
  });

  it("queues the final stop behind an earlier CLI start", async () => {
    await setup(); context.holdStart = true; context.health.mockResolvedValue(null);
    const started = context.handlers.get("daemon:start")!({});
    await vi.waitFor(() => expect(context.finishStart).toBeTypeOf("function"));
    requestQuit(); await vi.advanceTimersByTimeAsync(0);
    expect(context.finishStop).toBeNull(); expect(context.quit).not.toHaveBeenCalled();
    context.finishStart!(); await started;
    await vi.waitFor(() => expect(context.finishStop).toBeTypeOf("function"));
    context.finishStop!();
    await vi.waitFor(() => expect(context.quit).toHaveBeenCalledOnce());
  });

  it("does not wait for CLI installation when no daemon has started", async () => {
    await setupInstallingCLI();
    requestQuit();
    await vi.waitFor(() => expect(context.quit).toHaveBeenCalledOnce());
    expect(context.dialog).not.toHaveBeenCalled();
    expect(context.exec).not.toHaveBeenCalled();
  });

  it.each([false, true])("requires explicit ignore=%s when the CLI is unavailable and a port-record process is alive", async (ignore) => {
    context.readPort.mockResolvedValue({ port: 19515, pid: 20001 });
    vi.spyOn(process, "kill").mockImplementation((pid, signal) => {
      if (pid === 20001 && signal === 0) return true;
      throw Object.assign(new Error("Process is absent"), { code: "ESRCH" });
    });
    context.dialog.mockResolvedValueOnce({ response: 1 }).mockResolvedValueOnce({ response: ignore ? 1 : 0 });
    await setupInstallingCLI();
    requestQuit();
    await vi.waitFor(() => expect(context.dialog).toHaveBeenCalledTimes(2));
    expect(context.dialog).toHaveBeenLastCalledWith(expect.objectContaining({
      buttons: ["Cancel quit", "Ignore and quit"], defaultId: 0,
    }));
    if (ignore) await vi.waitFor(() => expect(context.quit).toHaveBeenCalledOnce());
    else {
      await vi.waitFor(() => expect(context.emit).toHaveBeenCalledWith("activate"));
      expect(context.quit).not.toHaveBeenCalled();
    }
    expect(context.exec).not.toHaveBeenCalled();
  });

  it("rechecks process evidence after confirmation and exits if cleanup has completed", async () => {
    let alive = true;
    context.readPort.mockResolvedValue({ port: 19515, pid: 20001 });
    vi.spyOn(process, "kill").mockImplementation((pid, signal) => {
      if (alive && pid === 20001 && signal === 0) return true;
      throw Object.assign(new Error("Process is absent"), { code: "ESRCH" });
    });
    context.dialog.mockImplementation(async () => { alive = false; return { response: 1 }; });
    await setupInstallingCLI();
    requestQuit();
    await vi.waitFor(() => expect(context.quit).toHaveBeenCalledOnce());
    expect(context.dialog).toHaveBeenCalledOnce();
    expect(context.exec).not.toHaveBeenCalled();
  });

  it.each(["PID file", "port record"])("confirms task interruption when health is lost but the %s PID is alive", async (source) => {
    await setup();
    if (source === "PID file") context.pid = 20001;
    else context.readPort.mockResolvedValue({ port: 19515, pid: 20001 });
    vi.spyOn(process, "kill").mockImplementation((pid, signal) => {
      if (pid === 20001 && signal === 0) return true;
      throw Object.assign(new Error("Process is absent"), { code: "ESRCH" });
    });
    context.health.mockResolvedValue(null);
    await vi.advanceTimersByTimeAsync(5_000);
    expect(await context.handlers.get("daemon:get-status")!({})).toMatchObject({ state: "stopped" });
    context.dialog.mockResolvedValue({ response: 0 });
    requestQuit();
    await vi.waitFor(() => expect(context.dialog).toHaveBeenCalledWith(expect.objectContaining({
      type: "warning", buttons: ["Cancel", "Stop now and quit"], defaultId: 0,
    })));
    expect(context.exec).not.toHaveBeenCalled();
    expect(context.quit).not.toHaveBeenCalled();
  });
  it("keeps auto-start suppressed when cancelling a failed quit recreates the renderer", async () => {
    context.autoStart = true;
    context.dialog.mockResolvedValueOnce({ response: 1 }).mockResolvedValueOnce({ response: 0 });
    await setup(); requestQuit();
    await vi.waitFor(() => expect(context.finishStop).toBeTypeOf("function"));
    context.health.mockResolvedValue(null);
    context.finishStop!(new Error("task cleanup could not be confirmed"));
    await vi.waitFor(() => expect(context.emit).toHaveBeenCalledWith("activate"));
    // A recreated renderer calls auto-start again through syncDaemonOnLogin.
    await context.handlers.get("daemon:auto-start")!({});
    await vi.advanceTimersByTimeAsync(60_000);
    expect(context.exec.mock.calls.filter(([, args]) => args[1] === "start")).toHaveLength(0);
    expect(await context.handlers.get("daemon:get-prefs")!({})).toMatchObject({ autoStart: true });
    expect(context.quit).not.toHaveBeenCalled();

    await context.handlers.get("daemon:start")!({});
    expect(context.exec.mock.calls.filter(([, args]) => args[1] === "start")).toHaveLength(1);
    context.exec.mockClear();
    await context.handlers.get("daemon:auto-start")!({});
    expect(context.exec.mock.calls.filter(([, args]) => args[1] === "start")).toHaveLength(1);
  });

  it("still allows auto-start after cancelling the initial interruption warning", async () => {
    context.autoStart = true;
    context.dialog.mockResolvedValue({ response: 0 });
    await setup(); requestQuit();
    await vi.waitFor(() => expect(context.emit).toHaveBeenCalledWith("activate"));
    context.health.mockResolvedValue(null);
    await context.handlers.get("daemon:auto-start")!({});
    expect(context.exec.mock.calls.filter(([, args]) => args[1] === "start")).toHaveLength(1);
  });

  it("does not perform a deferred version restart after cancelling a failed quit", async () => {
    context.autoStart = true;
    context.health.mockResolvedValue({ status: "running", active_task_count: 1, cli_version: "old" });
    context.dialog.mockResolvedValueOnce({ response: 1 }).mockResolvedValueOnce({ response: 0 });
    await setup();
    await context.handlers.get("daemon:auto-start")!({});
    requestQuit();
    await vi.waitFor(() => expect(context.finishStop).toBeTypeOf("function"));
    context.finishStop!(new Error("OS refused termination"));
    await vi.waitFor(() => expect(context.emit).toHaveBeenCalledWith("activate"));
    context.exec.mockClear();
    context.health.mockResolvedValue({ status: "running", active_task_count: 0, cli_version: "old" });
    await vi.advanceTimersByTimeAsync(5_000);
    expect(context.exec.mock.calls.filter(([, args]) => args[1] === "stop" || args[1] === "start")).toHaveLength(0);
  });
});
