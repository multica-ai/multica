import { describe, expect, it } from "vitest";
import {
  nextMondayAtNine,
  parseScheduledMessage,
  tomorrowAtNine,
} from "./scheduled-message";

describe("scheduled message time presets", () => {
  it("returns tomorrow at 09:00 in the user's local timezone", () => {
    const now = new Date(2026, 6, 14, 13, 25);
    expect(tomorrowAtNine(now)).toEqual(new Date(2026, 6, 15, 9, 0));
  });

  it("returns the next Monday at 09:00, never today", () => {
    const monday = new Date(2026, 6, 13, 8, 0);
    expect(nextMondayAtNine(monday)).toEqual(new Date(2026, 6, 20, 9, 0));
  });
});

describe("scheduled message API contract", () => {
  it("rejects a malformed response instead of trusting it", () => {
    expect(() => parseScheduledMessage({ id: 42, send_at: "soon" })).toThrow();
  });
});
