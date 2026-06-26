import { describe, expect, it, beforeEach, afterEach, vi } from "vitest";
import { timeAgo } from "./time-ago";

// Fixed "now" at noon UTC; the comparison instants are also noon UTC so the
// calendar-day difference is the same in any device timezone (both shift by the
// same offset), keeping these assertions stable regardless of where the test
// runner lives. Fake only Date so nothing else is affected.
const NOW = "2026-03-15T12:00:00Z";

describe("timeAgo (mobile)", () => {
  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date(NOW));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("buckets sub-day elapsed time", () => {
    expect(timeAgo("2026-03-15T11:59:30Z")).toBe("Just now"); // 30s
    expect(timeAgo("2026-03-15T11:55:00Z")).toBe("5m ago"); // 5m
    expect(timeAgo("2026-03-15T09:00:00Z")).toBe("3h ago"); // 3h
  });

  it("counts day granularity by calendar days", () => {
    expect(timeAgo("2026-03-14T12:00:00Z")).toBe("1d ago");
    expect(timeAgo("2026-03-13T12:00:00Z")).toBe("2d ago");
  });

  it("continues into calendar months and years past the day cap", () => {
    expect(timeAgo("2026-02-13T12:00:00Z")).toBe("30d ago"); // exactly 30d
    expect(timeAgo("2026-02-12T12:00:00Z")).toBe("1mo ago"); // 31d → months
    expect(timeAgo("2025-03-15T12:00:00Z")).toBe("1y ago"); // exactly 1 year
  });

  it("mirrors the gradient into the future direction", () => {
    expect(timeAgo("2026-03-15T15:00:00Z")).toBe("in 3h"); // +3h
    expect(timeAgo("2026-03-17T12:00:00Z")).toBe("in 2d"); // +2 calendar days
    expect(timeAgo("2026-06-15T12:00:00Z")).toBe("in 3mo"); // +3 calendar months
  });

  it("renders a placeholder for an unparseable date", () => {
    expect(timeAgo("not-a-date")).toBe("—");
  });
});
