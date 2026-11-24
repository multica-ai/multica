// @vitest-environment node
import { describe, expect, it } from "vitest";
import { KEEP, checkTiming, reminderPresets, timingChange } from "./chat-wakeup-timing";

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const NOW = new Date(2026, 10, 24, 10, 10).getTime();
const iso = (at: number) => new Date(at).toISOString();

describe("checkTiming", () => {
  // A check every 30 minutes until tomorrow 10:00, as an agent creates it.
  const check = { interval_seconds: 1800, expires_at: iso(NOW + 23 * HOUR + 50 * MINUTE) };

  it("keeps a schedule within the check limit", () => {
    expect(checkTiming(check, 900, KEEP, NOW)).toEqual({
      intervalSeconds: 900,
      deadline: NOW + 23 * HOUR + 50 * MINUTE,
      limited: false,
    });
  });

  it("ends a schedule over 100 checks at the last deadline the limit allows", () => {
    // Every 10 minutes for 23h50m would be 143 checks.
    expect(checkTiming(check, 600, KEEP, NOW)).toEqual({
      intervalSeconds: 600,
      deadline: NOW + 100 * 10 * MINUTE,
      limited: true,
    });
    // A longer deadline at the current interval is limited the same way.
    expect(checkTiming(check, null, { kind: "at", at: NOW + 3 * 24 * HOUR }, NOW).limited).toBe(true);
  });
});

describe("timingChange", () => {
  const check = {
    kind: "check" as const,
    interval_seconds: 1800,
    expires_at: iso(NOW + 23 * HOUR + 50 * MINUTE),
  };

  it("sends only what changed", () => {
    expect(timingChange(check, { interval: null, deadline: KEEP, reminder: KEEP }, NOW)).toBeNull();
    expect(timingChange(check, { interval: 1800, deadline: KEEP, reminder: KEEP }, NOW)).toBeNull();
    expect(timingChange(check, { interval: 900, deadline: KEEP, reminder: KEEP }, NOW)).toEqual({ interval_seconds: 900 });
    expect(
      timingChange(check, { interval: null, deadline: { kind: "at", at: NOW + 4 * HOUR }, reminder: KEEP }, NOW),
    ).toEqual({ expires_at: iso(NOW + 4 * HOUR) });
  });

  it("sends the earlier deadline when the check limit moves it", () => {
    expect(timingChange(check, { interval: 600, deadline: KEEP, reminder: KEEP }, NOW)).toEqual({
      interval_seconds: 600,
      expires_at: iso(NOW + 1000 * MINUTE),
    });
  });

  it("takes a time for a reminder and a deadline for a condition", () => {
    const reminder = { kind: "at" as const, interval_seconds: null, expires_at: iso(NOW + 2 * HOUR) };
    const at = NOW + 20 * HOUR;
    expect(timingChange(reminder, { interval: null, deadline: KEEP, reminder: { kind: "at", at } }, NOW)).toEqual({
      at: iso(at),
    });
    const condition = { kind: "condition" as const, interval_seconds: null, expires_at: iso(NOW + 20 * HOUR) };
    expect(timingChange(condition, { interval: null, deadline: { kind: "at", at }, reminder: KEEP }, NOW)).toEqual({
      expires_at: iso(at),
    });
    expect(timingChange(condition, { interval: null, deadline: KEEP, reminder: KEEP }, NOW)).toBeNull();
  });
});

describe("reminderPresets", () => {
  it("offers this evening only while it is more than three hours away", () => {
    const morning = reminderPresets(NOW).map((p) => p.at);
    expect(morning).toEqual([
      NOW + HOUR,
      NOW + 3 * HOUR,
      new Date(2026, 10, 24, 18).getTime(),
      new Date(2026, 10, 25, 9).getTime(),
    ]);
    const afternoon = new Date(2026, 10, 24, 16).getTime();
    expect(reminderPresets(afternoon).map((p) => p.at)).toEqual([
      afternoon + HOUR,
      afternoon + 3 * HOUR,
      new Date(2026, 10, 25, 9).getTime(),
    ]);
  });
});
