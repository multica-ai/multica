import { describe, it, expect } from "vitest";
import {
  toDateOnly,
  todayDateOnly,
  addDaysDateOnly,
  dateOnlyToUTCDate,
  dateOnlyToLocalDate,
  formatDateOnly,
  isPastDateOnly,
} from "./date";

describe("issue date-only helpers", () => {
  it("serializes a picked local day to YYYY-MM-DD with the local calendar", () => {
    // A calendar picker hands back local midnight of the clicked day.
    expect(toDateOnly(new Date(2026, 2, 1))).toBe("2026-03-01");
    expect(toDateOnly(new Date(2026, 0, 5))).toBe("2026-01-05"); // zero-padded
  });

  it("formats a date-only string timezone-safely (no day shift)", () => {
    // The bug: a calendar day must render as the same day in every timezone.
    expect(
      formatDateOnly("2026-03-01", { month: "short", day: "numeric" }, "en-US"),
    ).toBe("Mar 1");
    expect(formatDateOnly("2026-03-01", undefined, "en-US")).toBe("Mar 1");
    expect(formatDateOnly(null)).toBe("");
    expect(formatDateOnly("")).toBe("");
  });

  it("round-trips a picked day back to the same displayed day", () => {
    const picked = new Date(2026, 2, 1); // user clicks March 1 locally
    const stored = toDateOnly(picked);
    expect(stored).toBe("2026-03-01");
    expect(formatDateOnly(stored, { month: "short", day: "numeric" }, "en-US")).toBe(
      "Mar 1",
    );
  });

  it("anchors a date-only value at UTC midnight", () => {
    expect(dateOnlyToUTCDate("2026-03-01")?.toISOString()).toBe(
      "2026-03-01T00:00:00.000Z",
    );
    expect(dateOnlyToUTCDate(null)).toBeNull();
  });

  it("tolerates a legacy RFC3339 instant by reading its UTC day", () => {
    // Old clients stored local-midnight-as-UTC; read the stored UTC calendar day.
    expect(dateOnlyToUTCDate("2026-02-28T16:00:00Z")?.toISOString()).toBe(
      "2026-02-28T00:00:00.000Z",
    );
  });

  it("builds a local-midnight Date for the picker's selected day", () => {
    const d = dateOnlyToLocalDate("2026-03-01");
    expect(d?.getFullYear()).toBe(2026);
    expect(d?.getMonth()).toBe(2);
    expect(d?.getDate()).toBe(1);
    expect(dateOnlyToLocalDate(null)).toBeUndefined();
  });

  it("detects past calendar days relative to today", () => {
    expect(isPastDateOnly(addDaysDateOnly(-1))).toBe(true);
    expect(isPastDateOnly(todayDateOnly())).toBe(false);
    expect(isPastDateOnly(addDaysDateOnly(1))).toBe(false);
    expect(isPastDateOnly(null)).toBe(false);
  });
});
