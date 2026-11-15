// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  inboxRowTime,
  inboxSnoozeOptions,
  inboxSnoozeReturnedAt,
  isInboxItemSnoozed,
  parseInboxSnoozeInput,
} from "./snooze";

// Local-time constructors on purpose: the choices are in the viewer's zone.
const at = (y: number, m: number, d: number, h: number, min = 0) => new Date(y, m - 1, d, h, min);

describe("inboxSnoozeOptions", () => {
  it("offers an hour, this evening, tomorrow morning and Monday morning", () => {
    // A Wednesday.
    const options = inboxSnoozeOptions(at(2026, 6, 17, 14, 20));
    expect(options).toEqual([
      { preset: "in_one_hour", until: at(2026, 6, 17, 15, 20) },
      { preset: "later_today", until: at(2026, 6, 17, 18) },
      { preset: "tomorrow", until: at(2026, 6, 18, 9) },
      { preset: "next_week", until: at(2026, 6, 22, 9) },
    ]);
  });

  it("drops this evening once it is less than an hour away", () => {
    const presets = inboxSnoozeOptions(at(2026, 6, 17, 17, 30)).map((option) => option.preset);
    expect(presets).toEqual(["in_one_hour", "tomorrow", "next_week"]);
  });

  it("drops Monday on a Sunday, when it is tomorrow", () => {
    const options = inboxSnoozeOptions(at(2026, 6, 21, 10));
    expect(options.map((option) => option.preset)).toEqual(["in_one_hour", "later_today", "tomorrow"]);
    expect(options[2]?.until).toEqual(at(2026, 6, 22, 9));
  });

  it("means the following Monday on a Monday", () => {
    const options = inboxSnoozeOptions(at(2026, 6, 22, 10));
    expect(options.find((option) => option.preset === "next_week")?.until).toEqual(at(2026, 6, 29, 9));
  });

  it("means this morning by tomorrow before 5:00", () => {
    // 1:30 on a Wednesday.
    const options = inboxSnoozeOptions(at(2026, 6, 17, 1, 30));
    expect(options.find((option) => option.preset === "tomorrow")?.until).toEqual(at(2026, 6, 17, 9));
  });

  it("drops Monday after midnight on a Monday, when tomorrow is that morning", () => {
    const presets = inboxSnoozeOptions(at(2026, 6, 22, 2)).map((option) => option.preset);
    expect(presets).toEqual(["in_one_hour", "later_today", "tomorrow", "next_week"]);
    expect(inboxSnoozeOptions(at(2026, 6, 22, 2)).find((o) => o.preset === "next_week")?.until).toEqual(at(2026, 6, 29, 9));
    // Sunday 23:00 → Monday is tomorrow.
    expect(inboxSnoozeOptions(at(2026, 6, 21, 23)).map((option) => option.preset)).toEqual(["in_one_hour", "tomorrow"]);
  });
});

describe("parseInboxSnoozeInput", () => {
  const now = at(2026, 6, 17, 14);

  it("reads a local date and time", () => {
    expect(parseInboxSnoozeInput("2026-06-18T09:30", now)).toEqual(at(2026, 6, 18, 9, 30));
  });

  it("refuses a time that is not in the future", () => {
    expect(parseInboxSnoozeInput("2026-06-17T13:00", now)).toBeNull();
    expect(parseInboxSnoozeInput("2026-06-17T14:00", now)).toBeNull();
  });

  it("refuses a time past the server's limit, and anything unreadable", () => {
    expect(parseInboxSnoozeInput("2028-01-01T09:00", now)).toBeNull();
    expect(parseInboxSnoozeInput("", now)).toBeNull();
    expect(parseInboxSnoozeInput("soon", now)).toBeNull();
  });
});

describe("snooze state of a row", () => {
  const now = at(2026, 6, 17, 9, 2).getTime();
  const row = (created: Date, snoozedUntil: Date | null) => ({
    created_at: created.toISOString(),
    snoozed_until: snoozedUntil ? snoozedUntil.toISOString() : null,
  });

  it("is snoozed only until its time", () => {
    expect(isInboxItemSnoozed(row(at(2026, 6, 16, 10), at(2026, 6, 17, 10)), now)).toBe(true);
    expect(isInboxItemSnoozed(row(at(2026, 6, 16, 10), at(2026, 6, 17, 9)), now)).toBe(false);
    expect(isInboxItemSnoozed(row(at(2026, 6, 16, 10), null), now)).toBe(false);
  });

  it("sorts a row back from a snooze by when it came back", () => {
    const back = row(at(2026, 6, 16, 10), at(2026, 6, 17, 9));
    expect(inboxSnoozeReturnedAt(back, now)).toBe(at(2026, 6, 17, 9).getTime());
    expect(inboxRowTime(back, now)).toBe(at(2026, 6, 17, 9).getTime());
  });

  it("reads a row written after the snooze ran out as itself", () => {
    const later = row(at(2026, 6, 17, 9, 1), at(2026, 6, 17, 9));
    expect(inboxSnoozeReturnedAt(later, now)).toBeNull();
    expect(inboxRowTime(later, now)).toBe(at(2026, 6, 17, 9, 1).getTime());
  });
});
