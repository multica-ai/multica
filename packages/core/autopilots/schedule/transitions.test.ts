// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { TimePattern } from "./model";
import {
  clampWindow,
  defaultAtTime,
  defaultEveryTime,
  displayWindow,
  isFullDay,
  timeAnchorOf,
  toggleDay,
  type EveryPattern,
} from "./transitions";

// The canonical matrix for the editor's schedule state transitions. These ran
// as full DOM mounts in schedule-editor.test.tsx, one userEvent chain per
// combination; the component suite now keeps the wiring and focus cases and
// defers the combination space to the table below.

const every = (over: Partial<EveryPattern> = {}): EveryPattern => ({
  kind: "every",
  interval: 1,
  unit: "hours",
  window: null,
  minute: 0,
  ...over,
});

describe("isFullDay", () => {
  it.each([
    ["00:00", "23:59", true],
    ["00:30", "23:30", true],
    ["00:00", "23:00", true],
    ["09:00", "21:00", false],
    ["00:00", "22:59", false],
    ["01:00", "23:59", false],
    ["22:00", "22:00", false],
  ])("%s–%s spans the day: %s", (from, to, expected) => {
    expect(isFullDay({ from, to })).toBe(expected);
  });

  // Only the hours decide. An hours-step window carries the firing minute at
  // both ends, and a minute-step window reads :00/:59 — neither may turn a
  // day-spanning window into something the model would hold as a real window.
  it("ignores the minutes at both bounds", () => {
    expect(isFullDay({ from: "00:59", to: "23:00" })).toBe(true);
  });
});

describe("displayWindow", () => {
  it("passes a real window through untouched", () => {
    const window = { from: "09:00", to: "21:00" };
    expect(displayWindow(every({ window }))).toEqual(window);
  });

  // All day has no window of its own, but the fields still render something:
  // the bounds it stands for, at the granularity of the unit.
  it("renders all day as the bounds it stands for, per unit", () => {
    expect(displayWindow(every({ unit: "hours", minute: 30 }))).toEqual({
      from: "00:30",
      to: "23:30",
    });
    expect(displayWindow(every({ unit: "minutes", minute: 30 }))).toEqual({
      from: "00:00",
      to: "23:59",
    });
  });
});

describe("timeAnchorOf", () => {
  it("takes a fixed time verbatim", () => {
    expect(timeAnchorOf({ kind: "at", time: "14:30" })).toBe("14:30");
  });

  // The hour comes from the window, the minute from `minute` — never from the
  // window's own text. A minute-step window is hour-granular, so its bounds
  // read :00; reading the minute off them would fire a 14:30 schedule at 14:00.
  it("takes the hour from the window and the minute from the model", () => {
    expect(
      timeAnchorOf(
        every({ unit: "minutes", minute: 30, window: { from: "14:00", to: "14:59" } }),
      ),
    ).toBe("14:30");
    expect(
      timeAnchorOf(
        every({ unit: "hours", minute: 30, window: { from: "14:30", to: "21:30" } }),
      ),
    ).toBe("14:30");
  });

  // All day has no hour to give: the 00:00 its fields show is the absence of a
  // bound, not a time the user picked. Carrying it over would rewrite a 14:30.
  it("has no anchor for an all-day interval", () => {
    expect(timeAnchorOf(every({ minute: 30 }))).toBeNull();
  });
});

describe("defaultAtTime", () => {
  it("carries the anchor the current pattern can give", () => {
    expect(defaultAtTime({ kind: "at", time: "14:30" }, "09:00")).toBe("14:30");
    expect(
      defaultAtTime(every({ minute: 30, window: { from: "14:30", to: "21:30" } }), "09:00"),
    ).toBe("14:30");
  });

  // Falling back to the remembered anchor keeps the hour, but the minute is the
  // schedule's own and outranks the anchor's.
  it("falls back to the remembered hour with the pattern's minute", () => {
    expect(defaultAtTime(every({ minute: 45 }), "08:15")).toBe("08:45");
    expect(defaultAtTime({ kind: "at", time: "00:00" }, "08:15")).toBe("00:00");
  });
});

describe("defaultEveryTime", () => {
  it("leaves an interval pattern alone", () => {
    const time = every({ interval: 3, window: { from: "09:00", to: "21:00" } });
    expect(defaultEveryTime(time, null)).toBe(time);
  });

  // Step, unit and window are as much user input as the hour is; rebuilding
  // from defaults would turn "every 3h, 9–21" into "every hour, all day".
  it("restores the remembered step, unit and window", () => {
    const anchor = every({ interval: 3, window: { from: "09:00", to: "21:00" } });
    expect(defaultEveryTime({ kind: "at", time: "10:00" }, anchor)).toMatchObject({
      interval: 3,
      unit: "hours",
      window: { from: "10:00", to: "21:00" },
    });
  });

  it("falls back to hourly all day with no anchor", () => {
    expect(defaultEveryTime({ kind: "at", time: "10:15" }, null)).toEqual(
      every({ interval: 1, unit: "hours", window: null, minute: 15 }),
    );
  });

  // Rebasing "09:00–15:00" onto a 22:00 fixed time would drag the end up and
  // hand back the single hour 22:00–22:00, destroying the very window the
  // anchor exists to keep. Rebasing onto the end hour itself is the same
  // collapse by another name, so the guard is strictly-earlier.
  it.each([
    ["22:00", { from: "09:00", to: "15:00" }],
    ["15:00", { from: "09:00", to: "15:00" }],
  ])("keeps the window when %s would collapse it", (time, window) => {
    expect(
      defaultEveryTime({ kind: "at", time }, every({ interval: 3, window })),
    ).toMatchObject({ window });
  });

  // A window rebased across the whole day is not a window — the model spells
  // that one way, and nothing else may hand back a second form of it.
  it("collapses a rebased day-spanning window to all day", () => {
    expect(
      defaultEveryTime(
        { kind: "at", time: "00:00" },
        every({ interval: 3, window: { from: "09:00", to: "23:00" } }),
      ),
    ).toMatchObject({ window: null });
  });
});

describe("clampWindow", () => {
  // `minute` — not the window's own current text — is the single source of
  // truth for the firing minute, so an hours → minutes → hours round-trip
  // restores it instead of leaving the zeroed value behind.
  it.each([
    ["hours", 30, { from: "09:00", to: "21:00" }, { from: "09:30", to: "21:30" }],
    ["minutes", 30, { from: "09:30", to: "21:30" }, { from: "09:00", to: "21:59" }],
    ["hours", 0, { from: "09:30", to: "21:30" }, { from: "09:00", to: "21:00" }],
  ] as const)("%s at :%s canonicalises the bounds", (unit, minute, input, expected) => {
    expect(clampWindow(input, unit, minute)).toEqual(expected);
  });

  // The end is never earlier than the start: moving the start past the end
  // raises the end to meet it rather than inverting the range.
  it.each([
    [{ from: "18:00", to: "12:00" }, { from: "18:00", to: "18:00" }],
    [{ from: "09:00", to: "09:00" }, { from: "09:00", to: "09:00" }],
    [{ from: "00:00", to: "23:00" }, { from: "00:00", to: "23:00" }],
  ])("raises an overrun end to the start", (input, expected) => {
    expect(clampWindow(input, "hours", 0)).toEqual(expected);
  });

  it("survives a unit round-trip with the firing minute intact", () => {
    const start = { from: "09:30", to: "21:30" };
    const toMinutes = clampWindow(start, "minutes", 30);
    expect(clampWindow(toMinutes, "hours", 30)).toEqual(start);
  });
});

describe("toggleDay", () => {
  it("adds a day in ascending order", () => {
    expect(toggleDay([1, 5], 3)).toEqual([1, 3, 5]);
    expect(toggleDay([3], 0)).toEqual([0, 3]);
  });

  it("removes a day that is already selected", () => {
    expect(toggleDay([1, 3, 5], 3)).toEqual([1, 5]);
  });

  // A weekly schedule with no days never fires; the last one is not removable.
  it("keeps the last remaining day", () => {
    expect(toggleDay([3], 3)).toEqual([3]);
  });
});

// A fixed time survives a trip out to an interval and back. The component
// suite keeps one round-trip case for the wiring; the pattern-level guarantee
// it rests on is here.
describe("fixed time → interval → fixed time", () => {
  it.each([
    ["14:30", every({ interval: 3, window: { from: "09:00", to: "21:00" } })],
    ["14:30", null],
    ["00:15", null],
  ])("returns %s unchanged", (time, anchor) => {
    const at: TimePattern = { kind: "at", time };
    const interval = defaultEveryTime(at, anchor);
    expect(defaultAtTime(interval, time)).toBe(time);
  });
});
