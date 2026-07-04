import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { timeAgo } from "./time-ago";

describe("timeAgo", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-04T12:00:00Z"));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("returns an em-dash for missing/invalid input", () => {
    expect(timeAgo(null)).toBe("—");
    expect(timeAgo(undefined)).toBe("—");
    expect(timeAgo("not-a-date")).toBe("—");
  });

  it("labels sub-minute deltas as 'just now'", () => {
    expect(timeAgo("2026-07-04T11:59:30Z")).toBe("just now");
  });

  it("scales the unit with the delta", () => {
    expect(timeAgo("2026-07-04T11:57:00Z")).toBe("3m ago");
    expect(timeAgo("2026-07-04T06:00:00Z")).toBe("6h ago");
    expect(timeAgo("2026-05-03T12:00:00Z")).toBe("62d ago");
    expect(timeAgo("2026-01-04T12:00:00Z")).toBe("6mo ago");
    expect(timeAgo("2024-07-04T12:00:00Z")).toBe("2y ago");
  });

  it("never returns a negative label for future timestamps", () => {
    expect(timeAgo("2026-07-04T12:00:30Z")).toBe("just now");
  });
});
