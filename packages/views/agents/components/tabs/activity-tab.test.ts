// @vitest-environment node
import { describe, expect, it } from "vitest";
import { formatDurationMs } from "./activity-tab";

describe("formatDurationMs", () => {
  it("renders sub-minute durations in seconds", () => {
    expect(formatDurationMs(800)).toBe("1s"); // floor avoidance
    expect(formatDurationMs(12_000)).toBe("12s");
    expect(formatDurationMs(59_500)).toBe("60s");
  });

  it("renders sub-hour durations as 'm SS' with padded seconds", () => {
    expect(formatDurationMs(60_000)).toBe("1m 00s");
    expect(formatDurationMs(125_000)).toBe("2m 05s");
    expect(formatDurationMs(605_000)).toBe("10m 05s");
  });

  it("renders multi-hour durations as 'h m'", () => {
    expect(formatDurationMs(3 * 60 * 60_000 + 30 * 60_000)).toBe("3h 30m");
  });

  it("handles zero / negative defensively", () => {
    expect(formatDurationMs(0)).toBe("—");
    expect(formatDurationMs(-100)).toBe("—");
  });
});
