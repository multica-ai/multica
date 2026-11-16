import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

let clearMeasures: ReturnType<typeof vi.fn>;

async function load() {
  vi.resetModules();
  return (await import("./user-timing-cap")).installUserTimingCap;
}

beforeEach(() => {
  vi.useFakeTimers();
  clearMeasures = vi.fn();
  vi.stubGlobal("window", {});
  vi.stubGlobal("performance", { clearMeasures });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

describe("installUserTimingCap", () => {
  it("empties the measure buffer once a minute", async () => {
    const installUserTimingCap = await load();
    installUserTimingCap();

    vi.advanceTimersByTime(59_999);
    expect(clearMeasures).not.toHaveBeenCalled();
    vi.advanceTimersByTime(1);
    expect(clearMeasures).toHaveBeenCalledTimes(1);
    vi.advanceTimersByTime(60_000);
    expect(clearMeasures).toHaveBeenCalledTimes(2);
  });

  it("installs a single timer when called repeatedly", async () => {
    const installUserTimingCap = await load();
    installUserTimingCap();
    installUserTimingCap();

    vi.advanceTimersByTime(60_000);
    expect(clearMeasures).toHaveBeenCalledTimes(1);
  });

  it("does nothing on the server", async () => {
    vi.stubGlobal("window", undefined);
    const installUserTimingCap = await load();
    installUserTimingCap();

    vi.advanceTimersByTime(120_000);
    expect(clearMeasures).not.toHaveBeenCalled();
  });
});
