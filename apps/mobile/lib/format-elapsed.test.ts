// @vitest-environment node
import { describe, expect, it } from "vitest";
import { formatMessageTime } from "./format-elapsed";

// Dates are built from local components and `now` is injected, so the
// expectations hold in any host timezone.
const local = (y: number, month: number, day: number, h: number, m: number) =>
  new Date(y, month, day, h, m).toISOString();

describe("formatMessageTime", () => {
  it("shows clock time for a message from today", () => {
    expect(
      formatMessageTime(local(2026, 10, 22, 14, 32), "en-GB", new Date(2026, 10, 22, 20, 0)),
    ).toBe("14:32");
  });

  it("shows date + clock time for an earlier day in the same year", () => {
    expect(
      formatMessageTime(local(2026, 10, 21, 14, 32), "en-GB", new Date(2026, 10, 22, 20, 0)),
    ).toBe("21/11, 14:32");
  });

  it("adds the year for a message from an earlier year", () => {
    expect(
      formatMessageTime(local(2025, 11, 31, 23, 59), "en-GB", new Date(2026, 10, 22, 20, 0)),
    ).toBe("31/12/2025, 23:59");
  });

  it("returns an empty label for a missing or unparseable timestamp", () => {
    expect(formatMessageTime("", "en-GB")).toBe("");
    expect(formatMessageTime("not-a-date", "en-GB")).toBe("");
  });
});
