// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { RuntimeRateLimitWindow } from "../types";
import {
  describeRateLimitWindow,
  runtimeReportsRateLimits,
  tightestRateLimitWindow,
} from "./rate-limits";

const HOUR = 60 * 60_000;
// The 5-hour window below runs 09:00-14:00 UTC.
const RESET = Date.parse("2026-11-16T14:00:00Z");

function session(used: number | undefined, extra: Partial<RuntimeRateLimitWindow> = {}): RuntimeRateLimitWindow {
  return {
    kind: "session",
    used_percent: used,
    window_minutes: 300,
    resets_at: new Date(RESET).toISOString(),
    status: "",
    ...extra,
  };
}

describe("describeRateLimitWindow", () => {
  it("projects when a window that runs ahead of pace will run out", () => {
    // 72% used at 11:46, 2h46m (55%) into the window.
    const observed = new Date(RESET - 2 * HOUR - 14 * 60_000).toISOString();
    const view = describeRateLimitWindow(session(72), observed, Date.parse(observed));
    expect(view.tone).toBe("warning");
    expect(view.elapsedPercent).toBeCloseTo(55.3, 0);
    expect(view.aheadOfPace).toBe(17);
    // 28% left at 72%/166min burns in ~65 minutes, before the 14:00 reset.
    expect(view.runsOutInMinutes).toBe(65);
    expect(view.resetsInMinutes).toBe(134);
  });

  it("counts the projection from now, not from the report", () => {
    const observed = RESET - 2 * HOUR - 14 * 60_000;
    const view = describeRateLimitWindow(session(72), new Date(observed).toISOString(), observed + 20 * 60_000);
    expect(view.runsOutInMinutes).toBe(45);
  });

  it("gives no runout when usage stays behind the elapsed time", () => {
    const observed = new Date(RESET - 2 * HOUR).toISOString();
    const view = describeRateLimitWindow(session(40), observed, Date.parse(observed));
    expect(view.tone).toBe("normal");
    expect(view.aheadOfPace).toBeUndefined();
    expect(view.runsOutInMinutes).toBeUndefined();
  });

  it("ignores usage within the pace tolerance", () => {
    // 84% used 4h in (80% elapsed) is ahead by less than the tolerance.
    const observed = new Date(RESET - HOUR).toISOString();
    const close = describeRateLimitWindow(session(84), observed, Date.parse(observed));
    expect(close.aheadOfPace).toBeUndefined();
    const over = describeRateLimitWindow(session(86), observed, Date.parse(observed));
    expect(over.aheadOfPace).toBe(6);
    // 14% left at 86%/240min lasts ~39 minutes.
    expect(over.runsOutInMinutes).toBe(39);
  });

  it("marks a limited window critical without a pace verdict", () => {
    const observed = new Date(RESET - HOUR).toISOString();
    for (const window of [session(100), session(40, { status: "rejected" })]) {
      const view = describeRateLimitWindow(window, observed, Date.parse(observed));
      expect(view.limited).toBe(true);
      expect(view.tone).toBe("critical");
      expect(view.aheadOfPace).toBeUndefined();
    }
  });

  it("drops the usage of a window that reset after the report", () => {
    const view = describeRateLimitWindow(session(95), new Date(RESET - HOUR).toISOString(), RESET + 60_000);
    expect(view.hasReset).toBe(true);
    expect(view.used).toBeUndefined();
    expect(view.tone).toBe("normal");
    expect(view.limited).toBe(false);
    expect(view.resetsInMinutes).toBeUndefined();
  });

  it("uses the provider's warning status when no share is reported", () => {
    const observed = new Date(RESET - HOUR).toISOString();
    const view = describeRateLimitWindow(session(undefined, { status: "warning" }), observed, Date.parse(observed));
    expect(view.tone).toBe("warning");
    expect(view.used).toBeUndefined();
  });

  it("skips the pace verdict without a duration or reset", () => {
    const observed = new Date(RESET - HOUR).toISOString();
    expect(describeRateLimitWindow(session(80, { window_minutes: 0 }), observed, Date.parse(observed)).elapsedPercent).toBeUndefined();
    expect(describeRateLimitWindow(session(80, { resets_at: undefined }), observed, Date.parse(observed)).elapsedPercent).toBeUndefined();
  });
});

describe("tightestRateLimitWindow", () => {
  const observed = new Date(RESET - HOUR).toISOString();
  const now = Date.parse(observed);

  it("picks the highest share, and a limited window over any share", () => {
    const weekly = describeRateLimitWindow({ ...session(93), kind: "weekly", window_minutes: 10080 }, observed, now);
    const five = describeRateLimitWindow(session(18), observed, now);
    expect(tightestRateLimitWindow([five, weekly])).toBe(weekly);
    const limited = describeRateLimitWindow(session(undefined, { status: "rejected" }), observed, now);
    expect(tightestRateLimitWindow([weekly, limited])).toBe(limited);
    expect(tightestRateLimitWindow([])).toBeUndefined();
  });
});

describe("runtimeReportsRateLimits", () => {
  it("covers the CLIs that report rate limits", () => {
    expect(runtimeReportsRateLimits("claude")).toBe(true);
    expect(runtimeReportsRateLimits("codex")).toBe(true);
    expect(runtimeReportsRateLimits("cursor")).toBe(false);
  });
});
