// @vitest-environment node
import { describe, expect, it } from "vitest";
import { inboxSnoozeOptions, parseInboxSnoozeInput } from "./snooze";

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
