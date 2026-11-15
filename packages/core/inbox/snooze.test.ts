// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  inboxRowTime,
  inboxSnoozeOptions,
  inboxSnoozeReturnedAt,
  inboxSnoozePickedTime,
  isInboxItemSnoozed,
} from "./snooze";

// Local-time constructors on purpose: the choices are in the viewer's zone.
const at = (y: number, m: number, d: number, h: number, min = 0) => new Date(y, m - 1, d, h, min);

describe("inboxSnoozeOptions", () => {
  it("offers one and three hours, then one day, three days and a week at the same time", () => {
    // A Monday.
    expect(inboxSnoozeOptions(at(2026, 11, 16, 14, 20))).toEqual([
      { preset: "hours_1", until: at(2026, 11, 16, 15, 20) },
      { preset: "hours_3", until: at(2026, 11, 16, 17, 20) },
      { preset: "days_1", until: at(2026, 11, 17, 14, 20) },
      { preset: "days_3", until: at(2026, 11, 19, 14, 20) },
      { preset: "weeks_1", until: at(2026, 11, 23, 14, 20) },
    ]);
  });

  it("brings days back in the morning when snoozed at night", () => {
    expect(inboxSnoozeOptions(at(2026, 11, 16, 23, 40))).toEqual([
      { preset: "hours_1", until: at(2026, 11, 17, 0, 40) },
      { preset: "hours_3", until: at(2026, 11, 17, 2, 40) },
      { preset: "days_1", until: at(2026, 11, 17, 9) },
      { preset: "days_3", until: at(2026, 11, 19, 9) },
      { preset: "weeks_1", until: at(2026, 11, 23, 9) },
    ]);
    // After midnight a day is still counted from today.
    expect(inboxSnoozeOptions(at(2026, 11, 17, 1, 30))[2]?.until).toEqual(at(2026, 11, 18, 9));
  });

  it("keeps the clock time across the end of daylight saving", () => {
    // Calendar days, not 24-hour steps: 9:30 stays 9:30 whatever the zone does.
    expect(inboxSnoozeOptions(at(2026, 10, 31, 9, 30))[4]?.until).toEqual(at(2026, 11, 7, 9, 30));
  });
});

describe("inboxSnoozePickedTime", () => {
  const now = at(2026, 11, 16, 14, 20);

  it("joins the picked day and time", () => {
    expect(inboxSnoozePickedTime(at(2026, 11, 20, 0), "09:00", now)).toEqual(at(2026, 11, 20, 9));
    expect(inboxSnoozePickedTime(at(2026, 11, 16, 0), "16:45", now)).toEqual(at(2026, 11, 16, 16, 45));
  });

  it("refuses a time that is not in the future", () => {
    expect(inboxSnoozePickedTime(at(2026, 11, 16, 0), "14:20", now)).toBeNull();
    expect(inboxSnoozePickedTime(at(2026, 11, 16, 0), "09:00", now)).toBeNull();
  });

  it("refuses a time past the server's limit, and a time it cannot read", () => {
    expect(inboxSnoozePickedTime(at(2027, 12, 1, 0), "09:00", now)).toBeNull();
    expect(inboxSnoozePickedTime(at(2026, 11, 20, 0), "", now)).toBeNull();
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
