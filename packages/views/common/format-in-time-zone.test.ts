import { describe, expect, it } from "vitest";
import { formatInTimeZone } from "./format-in-time-zone";

// An autopilot's next run is printed on the schedule's clock, not the reader's:
// a trigger that says "18:00 (America/Los_Angeles)" must not contradict itself by
// showing its next run as 09:00 to a reader in UTC+8. A run that already happened
// is an instant in the reader's day, and passes no zone.
describe("formatInTimeZone", () => {
  // 2026-07-14T01:00:00Z is 18:00 of the 13th in Los Angeles (PDT, UTC-7).
  const iso = "2026-07-14T01:00:00Z";

  it("renders the instant in the given timezone", () => {
    const out = formatInTimeZone(iso, "America/Los_Angeles", "en-US");
    expect(out).toContain("13");
    expect(out).toMatch(/6:00\s?PM|18:00/);
  });

  it("renders the same instant differently in another timezone", () => {
    const out = formatInTimeZone(iso, "Asia/Shanghai", "en-US");
    expect(out).toContain("14");
    expect(out).toMatch(/9:00\s?AM|09:00/);
  });

  it("falls back to local time for a zone this runtime does not know", () => {
    // Wrong by an offset, but still a time of day — which a raw ISO string is not.
    expect(formatInTimeZone(iso, "Not/AZone", "en-US")).not.toBe("");
    expect(formatInTimeZone(iso, "Not/AZone", "en-US")).not.toBe(iso);
  });

  it("keeps the reader's locale when it falls back over a bad zone", () => {
    // The zone is what failed, not the language: dropping to the runtime's default
    // locale here would hand a zh-Hans reader an English date.
    expect(formatInTimeZone(iso, "Not/AZone", "zh-CN")).toMatch(/月/);
  });

  it("hands back an unreadable timestamp instead of throwing", () => {
    // Intl throws on an invalid Date. A drifted backend timestamp must degrade to
    // text, not take the whole detail page down with it.
    expect(formatInTimeZone("not-a-date", "UTC", "en-US")).toBe("not-a-date");
  });
});
