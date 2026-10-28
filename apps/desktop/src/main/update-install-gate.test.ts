// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { prepareDaemonForInstall, type DaemonInstallGateDeps } from "./update-install-gate";

const running = { alive: true, pid: 4242, activeTasks: 0, externallyManaged: false };
function makeDeps(): DaemonInstallGateDeps {
  return {
    probe: vi.fn().mockResolvedValue(running),
    stop: vi.fn(async () => {}),
    processExists: vi.fn(() => true),
    sleep: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
  };
}
beforeEach(() => vi.useFakeTimers());
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); });

describe("prepareDaemonForInstall", () => {
  it.each([null, { ...running, alive: false }, { ...running, externallyManaged: true }])(
    "leaves absent, stopped and external daemons alone (%j)", async (probe) => {
      const deps = makeDeps();
      vi.mocked(deps.probe).mockResolvedValue(probe);
      await expect(prepareDaemonForInstall(deps, { interruptActiveTasks: false })).resolves.toEqual({ kind: "ready" });
      expect(deps.stop).not.toHaveBeenCalled();
    },
  );

  it("requires permission before stopping active tasks", async () => {
    const deps = makeDeps();
    vi.mocked(deps.probe).mockResolvedValue({ ...running, activeTasks: 2 });
    await expect(prepareDaemonForInstall(deps, { interruptActiveTasks: false })).resolves.toEqual({ kind: "busy", activeTasks: 2 });
    expect(deps.stop).not.toHaveBeenCalled();
    vi.mocked(deps.probe).mockResolvedValueOnce({ ...running, activeTasks: 2 }).mockResolvedValue(null);
    vi.mocked(deps.processExists).mockReturnValue(false);
    await expect(prepareDaemonForInstall(deps, { interruptActiveTasks: true })).resolves.toEqual({ kind: "ready" });
    expect(deps.stop).toHaveBeenCalledTimes(1);
  });

  it("waits for both health and the original PID after a graceful stop", async () => {
    const deps = makeDeps();
    vi.mocked(deps.probe).mockResolvedValueOnce(running).mockResolvedValue(null);
    const completed = vi.fn();
    const prep = prepareDaemonForInstall(deps, { interruptActiveTasks: false }).then(completed);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(completed).not.toHaveBeenCalled();
    expect(deps.processExists).toHaveBeenCalledWith(4242);
    vi.mocked(deps.processExists).mockReturnValue(false);
    await vi.advanceTimersByTimeAsync(250);
    await prep;
    expect(completed).toHaveBeenCalledWith({ kind: "ready" });
  });

  it.each(["probe", "stop", "exit"])("bounds a stalled %s to 15 seconds", async (stage) => {
    const deps = makeDeps();
    if (stage === "probe") vi.mocked(deps.probe).mockImplementation(() => new Promise(() => {}));
    if (stage === "stop") vi.mocked(deps.stop).mockImplementation(() => new Promise(() => {}));
    const completed = vi.fn();
    const prep = prepareDaemonForInstall(deps, { interruptActiveTasks: false }).then(completed);
    await vi.advanceTimersByTimeAsync(14_999);
    expect(completed).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(1);
    await prep;
    expect(completed).toHaveBeenCalledWith({ kind: "timeout" });
    const stopSignal = vi.mocked(deps.stop).mock.calls[0]?.[0];
    if (stopSignal) expect(stopSignal.aborted).toBe(true);
  });

  it("still checks exit after the stop command fails", async () => {
    const deps = makeDeps();
    vi.mocked(deps.probe).mockResolvedValueOnce(running).mockResolvedValue(null);
    vi.mocked(deps.stop).mockRejectedValue(new Error("stop failed"));
    vi.mocked(deps.processExists).mockReturnValue(false);
    await expect(prepareDaemonForInstall(deps, { interruptActiveTasks: false })).resolves.toEqual({ kind: "ready" });
  });
});
