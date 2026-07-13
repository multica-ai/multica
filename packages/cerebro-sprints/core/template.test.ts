import { describe, expect, it } from "vitest";

import {
  applyNameTemplate,
  computeEnd,
  computeNextStart,
  formatDateOnly,
  formatDateOnlyShort,
  formatSprintDateRange,
  parseDateOnly,
} from "./template";

// These mirror server/internal/cerebro/sprints/service_test.go so the manual
// "New sprint" dialog stays byte-for-byte consistent with the server's
// auto-create naming and cadence math.

describe("computeNextStart", () => {
  it("weekly snaps to start weekday (next day)", () => {
    const prevEnd = parseDateOnly("2026-01-04")!; // Sunday
    expect(formatDateOnly(computeNextStart(prevEnd, "week", 1))).toBe("2026-01-05");
  });

  it("weekly snaps forward across the week", () => {
    const prevEnd = parseDateOnly("2026-01-06")!; // Tuesday
    expect(formatDateOnly(computeNextStart(prevEnd, "week", 1))).toBe("2026-01-12");
  });

  it("monthly snaps to the first of next month", () => {
    const prevEnd = parseDateOnly("2026-01-31")!;
    expect(formatDateOnly(computeNextStart(prevEnd, "month", 1))).toBe("2026-02-01");
  });

  it("daily is exactly the next day", () => {
    const prevEnd = parseDateOnly("2026-01-15")!;
    expect(formatDateOnly(computeNextStart(prevEnd, "day", 1))).toBe("2026-01-16");
  });
});

describe("computeEnd", () => {
  it("two-week sprint is inclusive end (14 days)", () => {
    const start = parseDateOnly("2026-01-05")!;
    expect(formatDateOnly(computeEnd(start, "week", 2))).toBe("2026-01-18");
  });

  it("one-month sprint is inclusive end", () => {
    const start = parseDateOnly("2026-02-01")!;
    expect(formatDateOnly(computeEnd(start, "month", 1))).toBe("2026-02-28");
  });

  it("coerces non-positive count to 1", () => {
    const start = parseDateOnly("2026-01-05")!;
    expect(formatDateOnly(computeEnd(start, "day", 0))).toBe("2026-01-05");
  });
});

describe("applyNameTemplate", () => {
  it("substitutes {n}", () => {
    expect(applyNameTemplate("Sprint {n}", 5)).toBe("Sprint 5");
    expect(applyNameTemplate("S{n}", 12)).toBe("S12");
  });

  it("appends sequence when no placeholder", () => {
    expect(applyNameTemplate("Cycle", 3)).toBe("Cycle 3");
  });

  it("falls back to 'Sprint N' for empty template", () => {
    expect(applyNameTemplate("", 4)).toBe("Sprint 4");
  });

  it("substitutes {n}, {start}, {end} together", () => {
    expect(
      applyNameTemplate("Sprint {n}: {start} - {end}", 7, "2026-06-01", "2026-06-14"),
    ).toBe("Sprint 7: 2026-06-01 - 2026-06-14");
  });

  it("leaves {start}/{end} untouched when no valid date supplied", () => {
    // No date → the placeholder is not a recognized substitution, so the
    // template counts as having no replacement and the sequence is appended.
    expect(applyNameTemplate("Sprint {start}", 2)).toBe("Sprint {start} 2");
  });
});

describe("parseDateOnly", () => {
  it("rejects malformed and rolled-over dates", () => {
    expect(parseDateOnly("2026-13-01")).toBeNull();
    expect(parseDateOnly("2026-02-31")).toBeNull();
    expect(parseDateOnly("not-a-date")).toBeNull();
    expect(parseDateOnly("2026-06-01")).not.toBeNull();
  });
});

describe("formatDateOnlyShort", () => {
  it("reformats YYYY-MM-DD to DD-MM", () => {
    expect(formatDateOnlyShort("2026-07-09")).toBe("09-07");
    expect(formatDateOnlyShort("2026-12-01")).toBe("01-12");
  });

  it("passes malformed input through unchanged", () => {
    expect(formatDateOnlyShort("not-a-date")).toBe("not-a-date");
  });
});

describe("formatSprintDateRange", () => {
  it("joins two dates as DD-MM - DD-MM", () => {
    expect(formatSprintDateRange("2026-07-09", "2026-07-22")).toBe("09-07 - 22-07");
  });
});
