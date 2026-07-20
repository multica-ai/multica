import { describe, it, expect } from "vitest";
import { cronFields, parseCron, toCron } from "./cron-mapping";
import type { ScheduleConfig } from "./model";
import { getDefaultScheduleConfig } from "./model";

// The backend accepts robfig/cron v3 5-field expressions (minute, hour, dom,
// month, dow — no seconds, no @descriptors, no L/W/#). The structured model
// echoes the subset its controls can express; everything else must come back as
// advanced-only with the original string preserved verbatim in `raw`.
//
// These cases pin the canonical FORM the editor writes back. The exhaustive
// check that the form still MEANS the same thing lives in cron-grammar.test.ts,
// which enumerates the grammar against a reference parser rather than against
// examples anyone had to think of.

const TZ = "Asia/Shanghai";

function structured(expr: string): ScheduleConfig {
  const parsed = parseCron(expr, TZ);
  expect(parsed.raw, `expected ${JSON.stringify(expr)} to be structurable`).toBeNull();
  return parsed;
}

describe("parseCron — structurable expressions", () => {
  it("parses a fixed daily time", () => {
    const p = structured("0 9 * * *");
    expect(p.time).toEqual({ kind: "at", time: "09:00" });
    expect(p.days).toEqual({ kind: "every" });
    expect(p.timezone).toBe(TZ);
  });

  it("parses zero-padded fields", () => {
    expect(structured("05 09 * * *").time).toEqual({ kind: "at", time: "09:05" });
  });

  it("parses the weekday preset shape", () => {
    const p = structured("30 18 * * 1-5");
    expect(p.time).toEqual({ kind: "at", time: "18:30" });
    expect(p.days).toEqual({ kind: "weekly", daysOfWeek: [1, 2, 3, 4, 5] });
  });

  it("tolerates surrounding and repeated whitespace", () => {
    expect(structured("  0  9  *  *  *  ").time).toEqual({ kind: "at", time: "09:00" });
  });

  // The backend drops empty list elements, so a stray comma leaves an ordinary
  // single value behind in EVERY field — not just day-of-week. Greying the editor
  // out on these would refuse a schedule the server runs perfectly happily.
  it.each([
    ["minute", "0, 9 * * *"],
    ["leading comma in minute", ",0 9 * * *"],
    ["hour", "0 9, * * *"],
    ["leading comma in hour", "0 ,9 * * *"],
  ])("reads through a stray comma in the %s field", (_label, expr) => {
    expect(structured(expr).time).toEqual({ kind: "at", time: "09:00" });
  });

  it("reads through a stray comma in the day-of-month field", () => {
    expect(structured("0 9 15, * *").days).toEqual({ kind: "monthly", dayOfMonth: 15 });
  });

  // "?" is a wildcard to the backend, in every field. Reading it as anything
  // else — or as nothing at all — greys the controls out over a schedule that is
  // simply "every day".
  it.each([
    ["minute", "? 10 * * *"],
    ["hour", "0 ? * * *"],
    ["day-of-month", "0 9 ? * 1-5"],
    ["day-of-week", "0 9 15 * ?"],
  ])("takes a question mark in the %s field as a wildcard", (_label, expr) => {
    expect(structured(expr).raw).toBeNull();
  });
  it("reads a question mark as the wildcard it is", () => {
    const p = structured("? 10 * * *");
    expect(p.time).toEqual({
      kind: "every",
      unit: "minutes",
      interval: 1,
      minute: 0,
      window: { from: "10:00", to: "10:59" },
    });
    expect(cronFields(p)).toBe("* 10 * * *");
  });

  // robfig decides a range is the field's full span from its LOW end alone and
  // never reads what followed the "-" (parser.go:261). So these are wildcards to
  // the parser that will run the schedule — every one of them is accepted, and
  // fires exactly as "*" does. The editor reads them the same way and saves them
  // back canonically; greying its controls out over them would be refusing an
  // expression that means "every hour".
  it.each([
    ["an upper end on a star", "0 *-19 * * *", "0 * * * *"],
    ["an upper end on a question mark", "0 ?-19 * * *", "0 * * * *"],
    ["a stepped wildcard range", "0 *-19/2 * * *", "0 */2 * * *"],
    ["the same in the minute field", "*-30/10 9 * * *", "*/10 9 * * *"],
    ["nonsense past the dash, which is never read", "0 *-abc * * *", "0 * * * *"],
    ["more dashes than a range may have", "0 *-10-20 * * *", "0 * * * *"],
    ["day-of-week", "0 9 * * ?-5", "0 9 * * *"],
    ["day-of-month", "0 9 *-15 * *", "0 9 * * *"],
  ])("reads a wildcard's unread upper end the way the server does: %s", (_label, expr, canonical) => {
    const p = structured(expr);
    expect(cronFields(p)).toBe(canonical);
  });

  // Only the low end. A "?" anywhere else is the syntax error it has always been,
  // and normalizing it would structure an expression the server rejects.
  it("leaves a question mark that is not a range's low end alone", () => {
    expect(parseCron("0 9-? * * *", TZ).raw).toBe("0 9-? * * *");
  });

  // robfig ORs a list's parts together, and a step-1 wildcard part spans the
  // whole field with its star bit set — so a list carrying one IS "*", in every
  // field, the AND-or-OR choice between dom and dow included. The parsers
  // enumerate lists only where the model can hold them (day-of-week), so without
  // the collapse a "0,*" minute greys the editor out over "every minute".
  it.each([
    ["minute", "0,* 9 * * *", "* 9 * * *"],
    ["minute, beside a stepped wildcard", "*,*/2 9 * * *", "* 9 * * *"],
    ["minute, spelled with a question mark", "?,15 9 * * *", "* 9 * * *"],
    ["minute, with an unread upper end", "0,*-30 9 * * *", "* 9 * * *"],
    ["hour", "0 9,* * * *", "0 * * * *"],
    ["day-of-month", "0 9 1,* * *", "0 9 * * *"],
    ["month", "0 9 * 1,* *", "0 9 * * *"],
    ["month, with the wildcard beside a name", "0 9 * JAN,* *", "0 9 * * *"],
    ["day-of-week", "0 9 * * 1,*", "0 9 * * *"],
  ])("collapses a list carrying a wildcard in the %s field", (_label, expr, canonical) => {
    expect(cronFields(structured(expr))).toBe(canonical);
  });

  // But only when every part parses: "*,abc" and "*,60" are syntax errors to
  // the server, and a collapse that read just the wildcard part would structure
  // an expression the server rejects.
  it.each([["*,abc 9 * * *"], ["*,60 9 * * *"], ["0 9 *,32 * *"], ["0 9 * FOO,* *"]])(
    "leaves a wildcard list with an unparseable part alone: %s",
    (expr) => {
      expect(parseCron(expr, TZ).raw).toBe(expr);
    },
  );

  // A range that selects one value IS that value, however it was written: a step
  // wider than its range never gets past the range's first value, and "5-5" is 5.
  // The server runs all of these; refusing them means telling the user the
  // controls cannot show a schedule they can show perfectly well.
  it.each([
    ["step wider than the minute field", "*/65 * * * *", "0 * * * *"],
    ["step wider than the hour field", "0 */24 * * *", "0 0 * * *"],
    ["step wider than its hour range", "0 10-20/30 * * *", "0 10 * * *"],
    ["step wider than the dom field", "0 9 */40 * *", "0 9 1 * *"],
    ["range of one value", "5-5 9 * * *", "5 9 * * *"],
    ["three-digit step", "*/100 * * * *", "0 * * * *"],
  ])("collapses a degenerate %s", (_label, expr, canonical) => {
    const p = structured(expr);
    expect(p.raw).toBeNull();
    expect(cronFields(p)).toBe(canonical);
  });

  // A step wider than the window it steps over never reaches the window's second
  // hour, so this fires at 09:00 alone. The hour is a range slot, though, so the
  // window the user set survives the round-trip instead of being flattened to a
  // fixed 09:00 — and the interval, 23, is inside what the control can hold.
  it("keeps an hour window whose step outruns it", () => {
    const p = structured("0 9-19/23 * * *");
    expect(p.time).toEqual({
      kind: "every",
      unit: "hours",
      interval: 23,
      minute: 0,
      window: { from: "09:00", to: "19:00" },
    });
    expect(cronFields(p)).toBe("0 9-19/23 * * *");
  });

  // The expression from the bug: minute */65 is 0, so this is the 14th of the
  // month, on the hour, between 10:00 and 20:59 — every control can hold it.
  it("structures a degenerate step across all three dimensions", () => {
    const p = structured("*/65 10-20 14 * *");
    expect(p.days).toEqual({ kind: "monthly", dayOfMonth: 14 });
    expect(p.time).toEqual({
      kind: "every",
      unit: "hours",
      interval: 1,
      minute: 0,
      window: { from: "10:00", to: "20:00" },
    });
    expect(cronFields(p)).toBe("0 10-20 14 * *");
  });

  it.each([
    ["0 * * * *", 1, 0],
    ["15 * * * *", 1, 15],
    ["0 */2 * * *", 2, 0],
    ["15 */3 * * *", 3, 15],
  ])("parses hourly-interval %s", (expr, interval, minute) => {
    expect(structured(expr).time).toEqual({
      kind: "every",
      unit: "hours",
      interval,
      minute,
      window: null,
    });
  });

  // An hour range that covers every hour of the day is all day. The model spells
  // all day exactly one way — window: null — so the two spellings of it cannot
  // come back as two different schedules.
  it.each([
    ["0 0-23 * * *", "hours", 1, 0],
    ["0 0-23/3 * * *", "hours", 3, 0],
    ["15 0-23/2 * * *", "hours", 2, 15],
    ["*/30 0-23 * * *", "minutes", 30, 0],
    ["*/30 0-23/1 * * *", "minutes", 30, 0],
  ])("reads an hour range that spans the day as all day: %s", (expr, unit, interval, minute) => {
    expect(structured(expr).time).toEqual({
      kind: "every",
      unit,
      interval,
      minute,
      window: null,
    });
  });

  it.each([
    ["0 9-21 * * *", 1, "09:00", "21:00", 0],
    ["30 9-21 * * *", 1, "09:30", "21:30", 30],
    ["0 9-21/2 * * *", 2, "09:00", "21:00", 0],
  ])("parses hour window %s", (expr, interval, from, to, minute) => {
    expect(structured(expr).time).toEqual({
      kind: "every",
      unit: "hours",
      interval,
      minute,
      window: { from, to },
    });
  });

  // A bare value carrying a step runs to the field's max. robfig reads "9/2" as
  // 9-23/2, which is exactly the window the editor writes back out — so it has to
  // reach the controls, not grey them out.
  it.each([
    ["0 9/2 * * *", 2, "09:00", "23:00", 0, "0 9-23/2 * * *"],
    ["30 9/1 * * *", 1, "09:30", "23:30", 30, "30 9-23 * * *"],
    // "0/2" is "0-23/2" — every hour of the day, which is all day, not a window
    // that happens to span it.
    ["0 0/2 * * *", 2, null, null, 0, "0 */2 * * *"],
    // "?" is the wildcard wherever a range may start, a step above it included.
    ["0 ?/2 * * *", 2, null, null, 0, "0 */2 * * *"],
  ])("reads a bare hour with a step as running to the field max: %s", (expr, interval, from, to, minute, back) => {
    const p = structured(expr);
    expect(p.time).toEqual({
      kind: "every",
      unit: "hours",
      interval,
      minute,
      window: from === null ? null : { from, to },
    });
    expect(cronFields(p)).toBe(back);
  });

  // The editor writes "9-9/3" itself, whenever a dragged window start meets its
  // end at interval 3. Collapsing it to a fixed time would fire at the same
  // instant and lose the interval, the unit and the window on reload.
  it.each([
    ["0 9-9 * * *", 1, "09:00", "09:00"],
    ["0 9-9/3 * * *", 3, "09:00", "09:00"],
    ["0 9-11/5 * * *", 5, "09:00", "11:00"],
    ["0 9-21/23 * * *", 23, "09:00", "21:00"],
  ])("keeps a degenerate hour window as a window: %s", (expr, interval, from, to) => {
    expect(structured(expr).time).toEqual({
      kind: "every",
      unit: "hours",
      interval,
      minute: 0,
      window: { from, to },
    });
  });

  // The minute-step branch reads the same degenerate hour range as the single
  // hour it selects — a one-hour window, not a second step dimension. Keeping the
  // stepped range for the hours branch (above) must not cost this one its window.
  it.each([
    ["*/5 9-9/3 * * *", 5, "09:00", "09:59"],
    ["*/10 9-21/23 * * *", 10, "09:00", "09:59"],
    ["*/10 9-9 * * *", 10, "09:00", "09:59"],
  ])("reads a degenerate stepped hour beside a minute step as a single-hour window: %s", (expr, interval, from, to) => {
    expect(structured(expr).time).toEqual({
      kind: "every",
      unit: "minutes",
      interval,
      minute: 0,
      window: { from, to },
    });
  });

  // A step past what the model's interval can carry has no window form left, so
  // it still collapses to the single value it selects.
  it.each([
    ["0 9-9/24 * * *", "09:00"],
    ["0 9-21/30 * * *", "09:00"],
    ["0 */24 * * *", "00:00"],
  ])("collapses an hour step past the model's bound to the value it selects: %s", (expr, time) => {
    expect(structured(expr).time).toEqual({ kind: "at", time });
  });

  // robfig puts no ceiling on a step. A step wider than the field it steps over
  // selects that field's first value and nothing else — which is a day set, and
  // the chips hold day sets. Day-of-week was the last parser still drawing its own
  // step bound (1-59) and greying the editor out over these.
  it.each([
    ["0 9 * * */100", [0]],
    ["0 9 * * 1-5/60", [1]],
    ["0 9 * * MON-FRI/60", [1]],
    ["0 9 * * */7", [0]],
  ])("reads a day-of-week step wider than the week: %s", (expr, daysOfWeek) => {
    expect(structured(expr).days).toEqual({ kind: "weekly", daysOfWeek });
  });

  // Every dimension at once: a stepped interval, a window, and a day set.
  it("parses a compound expression across all three dimensions", () => {
    const p = structured("0 9-21/2 * * 2-4");
    expect(p.time).toEqual({
      kind: "every",
      unit: "hours",
      interval: 2,
      minute: 0,
      window: { from: "09:00", to: "21:00" },
    });
    expect(p.days).toEqual({ kind: "weekly", daysOfWeek: [2, 3, 4] });
  });

  it.each([
    ["* * * * *", 1, null],
    ["*/10 * * * *", 10, null],
    ["0/5 * * * *", 5, null],
    ["*/10 9-18 * * *", 10, { from: "09:00", to: "18:59" }],
    ["* 9 * * *", 1, { from: "09:00", to: "09:59" }],
    ["*/15 9 * * *", 15, { from: "09:00", to: "09:59" }],
  ])("parses minute-interval %s", (expr, interval, window) => {
    expect(structured(expr).time).toEqual({
      kind: "every",
      unit: "minutes",
      interval,
      minute: 0,
      window,
    });
  });

  it.each([
    ["30 10 15 * *", { kind: "at", time: "10:30" }, 15],
    ["0 9 1 * *", { kind: "at", time: "09:00" }, 1],
    ["0 9 31 * *", { kind: "at", time: "09:00" }, 31],
    [
      "*/10 * 15 * *",
      { kind: "every", unit: "minutes", interval: 10, minute: 0, window: null },
      15,
    ],
  ])("parses monthly %s", (expr, time, dayOfMonth) => {
    const p = structured(expr);
    expect(p.time).toEqual(time);
    expect(p.days).toEqual({ kind: "monthly", dayOfMonth });
  });

  it.each([
    ["0 9 * * 0", [0]],
    ["0 9 * * 6", [6]],
    ["0 9 * * 1,3,5", [1, 3, 5]],
    ["0 9 * * 5,1,5", [1, 5]],
    ["0 9 * * 0-6", [0, 1, 2, 3, 4, 5, 6]],
    ["0 9 * * MON", [1]],
    // The two ends of the name map: an off-by-one there moves the schedule by a
    // day, and every other named case sits in its interior.
    ["0 9 * * SUN", [0]],
    ["0 9 * * SAT", [6]],
    ["0 9 * * mon-fri", [1, 2, 3, 4, 5]],
    ["0 9 * * MON,WED", [1, 3]],
    ["0 9 * * */2", [0, 2, 4, 6]],
    ["0 9 * * 1-5/2", [1, 3, 5]],
    ["0 9 * * 1/2", [1, 3, 5]],
    ["0 9 * * MON/2", [1, 3, 5]],
    // A step wider than the week still enumerates — to the range's first day.
    ["0 9 * * */7", [0]],
    ["0 9 * * 1,3-5", [1, 3, 4, 5]],
    // The backend drops empty list elements, so a stray comma is Mon–Fri to it,
    // not a syntax error. Refusing it here would grey out the controls on a
    // schedule the server runs perfectly happily.
    ["0 9 * * 1-5,", [1, 2, 3, 4, 5]],
    ["0 9 * * 1,,5", [1, 5]],
    ["0 9 * * ,1,5", [1, 5]],
  ])("expands dow %s to chips %j", (expr, days) => {
    expect(structured(expr).days).toEqual({ kind: "weekly", daysOfWeek: days });
  });
});

describe("parseCron — advanced-only fallback", () => {
  it.each([
    // minute field
    ["minute range", "0-30 9 * * *"],
    ["minute list", "0,30 9 * * *"],
    ["minute anchored step", "15/5 * * * *"],
    ["minute out of range", "60 9 * * *"],
    ["negative minute", "-5 9 * * *"],
    ["overflowing minute", "99999999999999999999 9 * * *"],
    // hour field
    ["hour list", "0 9,12,15 * * *"],
    ["hour out of range", "0 24 * * *"],
    ["hour wraparound range", "0 21-9 * * *"],
    ["minute step with hour step", "*/10 */2 * * *"],
    // A wildcard minute is a step of 1 — still a minute-dimension step, so a
    // real hour step alongside it exceeds the single-step model all the same.
    ["every minute with hour step", "* */2 * * *"],
    ["minute step with stepped window", "*/10 9-18/2 * * *"],
    // Both wildcards, so both are read — and then there are two steps, which is
    // one more than the model's interval carries. A real limit, not a refusal to
    // normalize: the day-of-week "?/2" beside them is structured fine on its own.
    ["question-mark steps in both time fields", "?/2 ?/2 * * ?/2"],
    // dom / month
    ["dom list", "0 9 1,15 * *"],
    ["dom range", "0 9 1-15 * *"],
    ["dom step", "0 9 */2 * *"],
    ["dom zero", "0 9 0 * *"],
    ["dom out of range", "0 9 32 * *"],
    ["dom plus dow (cron OR semantics)", "0 9 15 * 1"],
    ["pinned month", "0 9 * 6 *"],
    ["month name", "0 9 * JAN *"],
    // dow
    ["dow out of range", "0 9 * * 7"],
    ["dow wraparound range", "0 9 * * 5-1"],
    // structure & unsupported grammar
    ["four fields", "0 9 * *"],
    ["six fields (seconds cron)", "0 0 9 * * *"],
    ["free text", "not a cron"],
    ["empty string", ""],
    ["whitespace only", "   "],
    ["@daily descriptor", "@daily"],
    ["@every descriptor", "@every 1h"],
    ["L in dom", "0 9 L * *"],
    ["L in dow", "0 9 * * 5L"],
    ["hash nth-weekday", "0 9 * * 1#2"],
    ["W nearest-weekday", "0 9 15W * *"],
  ])("%s → raw preserved verbatim", (_label, expr) => {
    const p = parseCron(expr, TZ);
    expect(p.raw).toBe(expr);
    expect(p.timezone).toBe(TZ);
  });
});

describe("cronFields — the five-field serialization", () => {
  const base = getDefaultScheduleConfig(TZ);

  it.each<[string, ScheduleConfig, string]>([
    [
      "fixed daily time",
      { ...base, time: { kind: "at", time: "09:00" }, days: { kind: "every" } },
      "0 9 * * *",
    ],
    [
      "weekday range collapses",
      {
        ...base,
        time: { kind: "at", time: "18:30" },
        days: { kind: "weekly", daysOfWeek: [1, 2, 3, 4, 5] },
      },
      "30 18 * * 1-5",
    ],
    [
      "non-consecutive days stay a list",
      {
        ...base,
        time: { kind: "at", time: "09:00" },
        days: { kind: "weekly", daysOfWeek: [1, 3, 5] },
      },
      "0 9 * * 1,3,5",
    ],
    [
      "mixed runs and singletons",
      {
        ...base,
        time: { kind: "at", time: "09:00" },
        days: { kind: "weekly", daysOfWeek: [0, 1, 2, 4] },
      },
      "0 9 * * 0-2,4",
    ],
    [
      "flagship compound",
      {
        ...base,
        time: {
          kind: "every",
          unit: "hours",
          interval: 2,
          minute: 0,
          window: { from: "09:00", to: "21:00" },
        },
        days: { kind: "weekly", daysOfWeek: [2, 3, 4] },
      },
      "0 9-21/2 * * 2-4",
    ],
    [
      "hourly with minute offset",
      {
        ...base,
        time: { kind: "every", unit: "hours", interval: 1, minute: 15, window: null },
        days: { kind: "every" },
      },
      "15 * * * *",
    ],
    [
      "every N hours all day",
      {
        ...base,
        time: { kind: "every", unit: "hours", interval: 3, minute: 0, window: null },
        days: { kind: "every" },
      },
      "0 */3 * * *",
    ],
    [
      "hour window without step",
      {
        ...base,
        time: {
          kind: "every",
          unit: "hours",
          interval: 1,
          minute: 30,
          window: { from: "09:30", to: "21:30" },
        },
        days: { kind: "every" },
      },
      "30 9-21 * * *",
    ],
    [
      "minute interval with window",
      {
        ...base,
        time: {
          kind: "every",
          unit: "minutes",
          interval: 10,
          minute: 0,
          window: { from: "09:00", to: "18:59" },
        },
        days: { kind: "every" },
      },
      "*/10 9-18 * * *",
    ],
    [
      "minute interval single hour",
      {
        ...base,
        time: {
          kind: "every",
          unit: "minutes",
          interval: 15,
          minute: 0,
          window: { from: "09:00", to: "09:59" },
        },
        days: { kind: "every" },
      },
      "*/15 9 * * *",
    ],
    [
      "every minute",
      {
        ...base,
        time: { kind: "every", unit: "minutes", interval: 1, minute: 0, window: null },
        days: { kind: "every" },
      },
      "* * * * *",
    ],
    [
      "monthly fixed time",
      {
        ...base,
        time: { kind: "at", time: "10:30" },
        days: { kind: "monthly", dayOfMonth: 15 },
      },
      "30 10 15 * *",
    ],
    // The day dimension has to survive an interval time pattern. Every other
    // `every` case above pins days to "every day", where the day-of-month field
    // is legitimately `*` — so only these pin it down.
    [
      "hourly interval on a day of the month",
      {
        ...base,
        time: { kind: "every", unit: "hours", interval: 2, minute: 0, window: null },
        days: { kind: "monthly", dayOfMonth: 15 },
      },
      "0 */2 15 * *",
    ],
    [
      "windowed hourly interval on a day of the month",
      {
        ...base,
        time: {
          kind: "every",
          unit: "hours",
          interval: 2,
          minute: 30,
          window: { from: "09:30", to: "21:30" },
        },
        days: { kind: "monthly", dayOfMonth: 15 },
      },
      "30 9-21/2 15 * *",
    ],
    [
      "windowed minute interval on a day of the month",
      {
        ...base,
        time: {
          kind: "every",
          unit: "minutes",
          interval: 10,
          minute: 0,
          window: { from: "09:00", to: "18:59" },
        },
        days: { kind: "monthly", dayOfMonth: 1 },
      },
      "*/10 9-18 1 * *",
    ],
    [
      "the whole week selected stays an explicit range",
      {
        ...base,
        time: { kind: "at", time: "09:00" },
        days: { kind: "weekly", daysOfWeek: [0, 1, 2, 3, 4, 5, 6] },
      },
      "0 9 * * 0-6",
    ],
  ])("%s", (_label, cfg, expected) => {
    expect(cronFields(cfg)).toBe(expected);
  });

  it("returns raw verbatim in advanced mode", () => {
    expect(cronFields({ ...base, raw: "0 9 1,15 * *" })).toBe("0 9 1,15 * *");
  });

  it("serialises weekly with no days to Monday as a safety fallback", () => {
    expect(
      cronFields({
        ...base,
        time: { kind: "at", time: "09:00" },
        days: { kind: "weekly", daysOfWeek: [] },
      }),
    ).toBe("0 9 * * 1");
  });
});

describe("bidirectional invariants", () => {
  const base = getDefaultScheduleConfig(TZ);
  const CANONICAL = [
    "0 9 * * *",
    "30 18 * * 1-5",
    "15 * * * *",
    "0 */2 * * *",
    "30 9-21 * * *",
    "0 9-21/2 * * 2-4",
    "*/10 9-18 * * 1-5",
    "*/15 9 * * *",
    "30 10 15 * *",
    "* * * * *",
    "0 9 * * 1,3,5",
    "0 9 * * 0-2,4",
    // The day dimension crossed with an interval one — the pairing the toCron
    // table above pins, held to the round-trip standard too.
    "0 */2 15 * *",
    "30 9-21/2 15 * *",
    "*/10 9-18 1 * *",
    // Every weekday selected: a set, not a wildcard, and it must come back as one.
    "0 9 * * 0-6",
    // A window one hour wide. The editor produces it whenever a dragged start
    // meets the end, so parseCron has to take it back — at any interval, not
    // only at 1, or the step and the unit are gone on the way back.
    "0 9-9 * * *",
    "0 9-9/3 * * *",
    "59 23 * * *",
  ];

  it.each(CANONICAL)("canonical form %s survives a verbatim round-trip", (expr) => {
    expect(cronFields(parseCron(expr, TZ))).toBe(expr);
  });

  const NORMALIZING: Array<[string, string]> = [
    ["05 09 * * *", "5 9 * * *"],
    ["0/5 * * * *", "*/5 * * * *"],
    // A step of 1 selects every hour: it is the plain field spelled out, not a
    // second step dimension, so it stays structurable.
    ["*/10 */1 * * *", "*/10 * * * *"],
    // An hour range that spans the day is all day: the model holds that one way,
    // and the reset control cannot be lit on a window there is nothing to reset.
    ["*/10 0-23/1 * * *", "*/10 * * * *"],
    ["*/10 0-23 * * *", "*/10 * * * *"],
    ["0 0-23 * * *", "0 * * * *"],
    ["0 0-23/3 * * *", "0 */3 * * *"],
    ["15 0-23/2 * * 1-5", "15 */2 * * 1-5"],
    // A step of 1 inside a window keeps the window: dropping it would run the
    // schedule around the clock instead of inside 09:00–18:59.
    ["*/10 9-18/1 * * *", "*/10 9-18 * * *"],
    // The hours-unit twin of the same rule: the /1 is spelled-out syntax, the
    // window survives it.
    ["0 9-21/1 * * *", "0 9-21 * * *"],
    ["0 9 * * 5,1,5", "0 9 * * 1,5"],
    ["0 9 * * MON-FRI", "0 9 * * 1-5"],
    // Names at both ends of the map, spanning the whole week.
    ["0 9 * * sun-sat", "0 9 * * 0-6"],
    // A stray comma normalizes away rather than dragging the schedule into the
    // raw-cron box — in every field, not just day-of-week.
    ["0 9 * * 1-5,", "0 9 * * 1-5"],
    ["0 9 * * 1,,5", "0 9 * * 1,5"],
    ["0, 9 * * *", "0 9 * * *"],
    ["0 9, * * *", "0 9 * * *"],
    ["0 9 15, * *", "0 9 15 * *"],
    [",0 ,9 * * *", "0 9 * * *"],
    // The backend reads numbers with strconv.Atoi: leading zeros and plus
    // signs are the same value to it, in any field and any position.
    ["009 09 * * *", "9 9 * * *"],
    ["+30 14 * * *", "30 14 * * *"],
    ["00/05 9-18 * * *", "*/5 9-18 * * *"],
    ["0 09-021/02 * * *", "0 9-21/2 * * *"],
    ["0 9 015 * *", "0 9 15 * *"],
    ["0 9 * * 001", "0 9 * * 1"],
    ["0 9 * * MON-05", "0 9 * * 1-5"],
    // {0,2,4,6} has no consecutive run, so run-collapse keeps it a list.
    ["0 9 * * */2", "0 9 * * 0,2,4,6"],
    // A bare hour with a step is the range it stands for, written out.
    ["0 9/2 * * *", "0 9-23/2 * * *"],
    ["0 ?/2 * * *", "0 */2 * * *"],
    // A step past the model's interval bound has no window form left.
    ["0 9-9/24 * * *", "0 9 * * *"],
  ];

  it.each(NORMALIZING)("%s re-serialises to the semantically equal %s", (expr, normalized) => {
    expect(cronFields(parseCron(expr, TZ))).toBe(normalized);
  });

  it.each([...CANONICAL, ...NORMALIZING.map(([e]) => e)])(
    "parse ∘ toCron ∘ parse is idempotent for %s",
    (expr) => {
      const once = parseCron(expr, TZ);
      const twice = parseCron(toCron(once), TZ);
      expect(twice).toEqual(once);
    },
  );

  it("advanced expressions round-trip verbatim", () => {
    const p = parseCron("0 9 1,15 * *", TZ);
    expect(p.raw).toBe("0 9 1,15 * *");
    expect(cronFields(p)).toBe("0 9 1,15 * *");
    expect(parseCron(toCron(p), TZ)).toEqual(p);
  });

  // The other direction, and the one the grammar suite cannot see: it enumerates
  // cron STRINGS, so a config the editor can build, save, and fail to read back
  // is invisible to it. Every case here is a config the controls actually
  // produce — a dragged window collapsed onto a single hour is the one that was
  // coming back as a fixed time, interval and unit and window silently dropped.
  const EDITOR_CONFIGS: Array<[string, ScheduleConfig]> = [
    [
      "a window dragged shut at interval 3",
      { ...base, time: { kind: "every", unit: "hours", interval: 3, minute: 0, window: { from: "09:00", to: "09:00" } } },
    ],
    [
      // clampWindow keeps both ends on the firing minute, so this is the shape
      // the controls hand over — not a hand-written one.
      "the same window with a firing minute",
      { ...base, time: { kind: "every", unit: "hours", interval: 2, minute: 30, window: { from: "22:30", to: "22:30" } } },
    ],
    [
      "a minute-step window one hour wide",
      { ...base, time: { kind: "every", unit: "minutes", interval: 10, minute: 0, window: { from: "09:00", to: "09:59" } } },
    ],
    [
      // The widest window the controls can now hand over: one hour short of the
      // day, because a window that spans the day IS all day and the editor
      // commits it as such.
      "a window that stops one hour short of the day",
      { ...base, time: { kind: "every", unit: "hours", interval: 1, minute: 0, window: { from: "00:00", to: "22:00" } } },
    ],
    [
      "an all-day interval carrying a firing minute",
      { ...base, time: { kind: "every", unit: "hours", interval: 3, minute: 15, window: null } },
    ],
  ];

  it.each(EDITOR_CONFIGS)("a config the editor can build survives cron and comes back: %s", (_label, config) => {
    expect(parseCron(toCron(config), TZ)).toEqual(config);
  });
});

describe("toCron — the wire expression", () => {
  const base = getDefaultScheduleConfig(TZ);

  it("carries the timezone as a TZ= prefix on every serialization", () => {
    expect(toCron({ ...base, time: { kind: "at", time: "09:00" } })).toBe(
      "TZ=Asia/Shanghai 0 9 * * *",
    );
    expect(toCron({ ...base, raw: "0 9 1,15 * *" })).toBe("TZ=Asia/Shanghai 0 9 1,15 * *");
  });

  it("round-trips through parseCron with no timezone fallback needed", () => {
    const config = { ...base, time: { kind: "at", time: "09:00" } } as const;
    // "Test/Sentinel" would only surface if the fallback were consulted: the
    // wire form must carry the zone itself.
    expect(parseCron(toCron(config), "Test/Sentinel")).toEqual(config);
  });

  it("never stacks a second prefix on a raw that carries its own", () => {
    // A self-prefixed raw is a typed expression awaiting the server's verdict.
    // robfig strips ONE prefix and reads what follows as fields, so prefixing
    // it again would quietly change what the server judges.
    for (const raw of ["TZ=Local 0 9 * * *", "CRON_TZ=Bogus/Zone 0 9 * * *", "TZ=UTC"]) {
      expect(toCron({ ...base, raw })).toBe(raw);
    }
  });

  it("degrades to the bare fields when the zone could not ride in a prefix", () => {
    // robfig ends the zone name at the first space, so a spaced (or empty)
    // name cannot be embedded; the timezone column still covers these.
    expect(toCron({ ...base, timezone: "" })).toBe("0 9 * * *");
    expect(toCron({ ...base, timezone: "Bad Zone" })).toBe("0 9 * * *");
  });
});

describe("parseCron — timezone prefix extraction", () => {
  // robfig reads a `TZ=` / `CRON_TZ=` prefix off the expression and lets it
  // OVERRIDE the schedule's timezone column, so the editor extracts it into
  // the config's timezone — the picker shows the zone that actually governs
  // the schedule, and toCron writes it back as the prefix of the wire form:
  // the expression IS the pair (timezone, fields) in both directions.
  it("extracts TZ= into the timezone and structures the rest", () => {
    const p = parseCron("TZ=Asia/Tokyo 0 9 * * *", TZ);
    expect(p.raw).toBeNull();
    expect(p.timezone).toBe("Asia/Tokyo");
    expect(p.time).toEqual({ kind: "at", time: "09:00" });
    expect(toCron(p)).toBe("TZ=Asia/Tokyo 0 9 * * *");
  });

  it("extracts CRON_TZ= the same way, and re-serialises it as TZ=", () => {
    const p = parseCron("CRON_TZ=America/New_York 30 */3 * * 1-5", TZ);
    expect(p.raw).toBeNull();
    expect(p.timezone).toBe("America/New_York");
    expect(toCron(p)).toBe("TZ=America/New_York 30 */3 * * 1-5");
  });

  it("reads an empty zone as the UTC it loads as", () => {
    // LoadLocation("") is UTC server-side; the picker needs the name, not the
    // spelling.
    expect(parseCron("TZ= 0 9 * * *", TZ).timezone).toBe("UTC");
  });

  it("canonicalizes the zone's spelling to the one the picker's list uses", () => {
    // Intl reads names case-insensitively, so "asia/shanghai" is a real zone —
    // but seating that spelling in the config would show a duplicate beside
    // the list's own "Asia/Shanghai". The canonical name is also the only
    // spelling the server's LoadLocation loads on every platform; lowercase
    // works there only by the grace of a case-insensitive filesystem.
    expect(parseCron("TZ=asia/shanghai 0 9 * * *", "UTC").timezone).toBe("Asia/Shanghai");
    expect(parseCron("CRON_TZ=UTC 0 9 * * *", "Asia/Tokyo").timezone).toBe("UTC");
  });

  it("extracts over an advanced-only body too, stripping the prefix from raw", () => {
    // The zone must not stay buried in `raw`, where it would silently overrule
    // the picker: the pair (raw, timezone) is the schedule either way.
    const p = parseCron("TZ=Asia/Tokyo ?/2 ?/2 * * ?/2", TZ);
    expect(p.raw).toBe("?/2 ?/2 * * ?/2");
    expect(p.timezone).toBe("Asia/Tokyo");
  });

  it.each([
    // The picker cannot hold these zones — "Local" is legal server-side (the
    // SERVER host's zone) but no browser zone; the others fail LoadLocation
    // too. Extraction would put a name in a control that cannot offer it, so
    // the whole expression stays verbatim and keeps meaning what it meant.
    ["a zone only the server knows", "TZ=Local 0 9 * * *"],
    ["a zone nobody knows", "TZ=Bogus/Zone 0 9 * * *"],
    ["a tab-ridden zone", "TZ=UTC\t 0 9 * * *"],
    // robfig v3.0.1 panics on a prefix with no space after it (parser.go:99);
    // the server guards that into a rejection. Verbatim advanced is the only
    // honest echo.
    ["prefix without a schedule", "TZ=UTC"],
    ["CRON_TZ prefix without a schedule", "CRON_TZ=Asia/Tokyo"],
    ["bare TZ=", "TZ="],
    ["prefix with only trailing space", "TZ=UTC "],
    // robfig strips ONE prefix and reads the second as a field — a rejection
    // extraction would quietly turn into an accepted schedule.
    ["a second prefix behind the first", "TZ=UTC TZ=UTC 0 9 * * *"],
    ["a CRON_TZ prefix behind a TZ one", "TZ=UTC CRON_TZ=Asia/Tokyo 0 9 * * *"],
    // Not prefixes to robfig at all: detection is case-sensitive and untrimmed.
    ["lowercase tz=", "tz=UTC 0 9 * * *"],
    ["leading space before the prefix", " TZ=UTC 0 9 * * *"],
  ])("%s stays verbatim in advanced mode", (_label, expr) => {
    const p = parseCron(expr, TZ);
    expect(p.raw).toBe(expr);
    expect(p.timezone).toBe(TZ);
  });
});
