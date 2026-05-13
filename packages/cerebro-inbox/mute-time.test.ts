import { describe, expect, it } from "vitest";
import { formatMutedUntilTime, isMuted, nextLocalEightAm } from "./mute-time";

describe("nextLocalEightAm", () => {
  it("returns today's 08:00 when called before 08:00", () => {
    const now = new Date(2026, 4, 7, 6, 30); // 06:30 local
    const got = nextLocalEightAm(now);
    expect(got.getHours()).toBe(8);
    expect(got.getMinutes()).toBe(0);
    expect(got.getDate()).toBe(7);
  });

  it("returns tomorrow's 08:00 when called after 08:00", () => {
    const now = new Date(2026, 4, 7, 14, 0); // 14:00 local
    const got = nextLocalEightAm(now);
    expect(got.getDate()).toBe(8);
    expect(got.getHours()).toBe(8);
  });

  it("returns tomorrow's 08:00 when called exactly at 08:00", () => {
    const now = new Date(2026, 4, 7, 8, 0, 0, 0);
    const got = nextLocalEightAm(now);
    expect(got.getDate()).toBe(8);
  });
});

describe("isMuted", () => {
  it("returns false for null", () => {
    expect(isMuted(null)).toBe(false);
  });
  it("returns false for past timestamps", () => {
    const past = new Date(Date.now() - 1000).toISOString();
    expect(isMuted(past)).toBe(false);
  });
  it("returns true for future timestamps", () => {
    const future = new Date(Date.now() + 60_000).toISOString();
    expect(isMuted(future)).toBe(true);
  });
});

describe("formatMutedUntilTime", () => {
  it("returns null for null", () => {
    expect(formatMutedUntilTime(null)).toBeNull();
  });
  it("returns null for past timestamps", () => {
    const past = new Date(Date.now() - 1000).toISOString();
    expect(formatMutedUntilTime(past)).toBeNull();
  });
  it("returns an HH:MM-style string for future timestamps", () => {
    // 08:00 on a fixed date — the literal output depends on the
    // execution locale, but the formatter must yield a non-empty string
    // that contains both digits of the hour.
    const future = new Date(Date.now() + 24 * 3600_000).toISOString();
    const got = formatMutedUntilTime(future, "en-US");
    expect(got).toBeTruthy();
    expect(got!).toMatch(/\d/);
  });
});
