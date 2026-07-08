import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../analytics", () => ({ captureEvent: vi.fn() }));

// A controllable PerformanceObserver stand-in: records the callback so a test
// can fire synthetic long-task entries, and counts constructions so we can
// assert idempotent install.
let lastCallback: ((list: { getEntries: () => Array<{ duration: number }> }) => void) | null;
let constructed: number;
let observeCalls: number;

class FakePerformanceObserver {
  constructor(cb: (list: { getEntries: () => Array<{ duration: number }> }) => void) {
    constructed += 1;
    lastCallback = cb;
  }
  observe() {
    observeCalls += 1;
  }
}

function fireLongTask(duration: number) {
  lastCallback?.({ getEntries: () => [{ duration }] });
}

async function load() {
  vi.resetModules();
  const mod = await import("./freeze-watchdog");
  const { captureEvent } = await import("../analytics");
  return {
    installFreezeWatchdog: mod.installFreezeWatchdog,
    captureEvent: captureEvent as unknown as ReturnType<typeof vi.fn>,
  };
}

beforeEach(() => {
  lastCallback = null;
  constructed = 0;
  observeCalls = 0;
  vi.stubGlobal("window", {});
  vi.stubGlobal("location", { pathname: "/acme/issues" });
  vi.stubGlobal("PerformanceObserver", FakePerformanceObserver);
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
  vi.useRealTimers();
});

describe("installFreezeWatchdog", () => {
  it("reports a long task at or above the 2s threshold with duration + path", async () => {
    const { installFreezeWatchdog, captureEvent } = await load();
    installFreezeWatchdog();

    fireLongTask(2300);

    expect(captureEvent).toHaveBeenCalledTimes(1);
    expect(captureEvent).toHaveBeenCalledWith("client_unresponsive", {
      source: "longtask",
      duration_ms: 2300,
      path: "/acme/issues",
    });
  });

  it("ignores blocks below the threshold (normal render cost)", async () => {
    const { installFreezeWatchdog, captureEvent } = await load();
    installFreezeWatchdog();

    fireLongTask(600);
    fireLongTask(1999);

    expect(captureEvent).not.toHaveBeenCalled();
  });

  it("is idempotent — a second install does not add a second observer", async () => {
    const { installFreezeWatchdog } = await load();
    installFreezeWatchdog();
    installFreezeWatchdog();

    expect(constructed).toBe(1);
    expect(observeCalls).toBe(1);
  });

  it("is a no-op on the server (no window)", async () => {
    vi.stubGlobal("window", undefined);
    const { installFreezeWatchdog, captureEvent } = await load();

    expect(() => installFreezeWatchdog()).not.toThrow();
    expect(constructed).toBe(0);
    expect(captureEvent).not.toHaveBeenCalled();
  });

  it("is a no-op when PerformanceObserver is unavailable", async () => {
    vi.stubGlobal("PerformanceObserver", undefined);
    const { installFreezeWatchdog } = await load();

    expect(() => installFreezeWatchdog()).not.toThrow();
  });

  it("emits at most one client_unresponsive per 60s cooldown window", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    const { installFreezeWatchdog, captureEvent } = await load();
    installFreezeWatchdog();

    // A sustained freeze arrives as several long-task entries back to back.
    fireLongTask(2500);
    fireLongTask(2500);
    fireLongTask(3000);

    expect(captureEvent).toHaveBeenCalledTimes(1);
  });

  it("emits again only after the cooldown window elapses", async () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-01-01T00:00:00Z"));
    const { installFreezeWatchdog, captureEvent } = await load();
    installFreezeWatchdog();

    fireLongTask(2500);
    expect(captureEvent).toHaveBeenCalledTimes(1);

    // Still inside the window → suppressed.
    vi.advanceTimersByTime(59_999);
    fireLongTask(2500);
    expect(captureEvent).toHaveBeenCalledTimes(1);

    // Window elapsed → emits again.
    vi.advanceTimersByTime(1);
    fireLongTask(2500);
    expect(captureEvent).toHaveBeenCalledTimes(2);
  });
});
