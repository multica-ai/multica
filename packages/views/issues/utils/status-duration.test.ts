import { describe, expect, it } from "vitest";
import { statusDurationParts } from "./status-duration";

describe("statusDurationParts", () => {
  it("picks the largest unit that yields a whole value", () => {
    expect(statusDurationParts(9)).toEqual({ value: 9, unit: "seconds" });
    expect(statusDurationParts(56 * 60)).toEqual({ value: 56, unit: "minutes" });
    expect(statusDurationParts(3 * 3600)).toEqual({ value: 3, unit: "hours" });
    expect(statusDurationParts(11 * 86400)).toEqual({ value: 11, unit: "days" });
  });

  it("truncates rather than rounds, so time that has not elapsed is never credited", () => {
    // The whole point of a coarse unit is that it must not overstate. 59
    // minutes reading "1h" would let a status claim time it never held.
    expect(statusDurationParts(59 * 60 + 59)).toEqual({
      value: 59,
      unit: "minutes",
    });
    expect(statusDurationParts(23 * 3600 + 3599)).toEqual({
      value: 23,
      unit: "hours",
    });
  });

  it("switches unit exactly at each boundary", () => {
    expect(statusDurationParts(59)).toEqual({ value: 59, unit: "seconds" });
    expect(statusDurationParts(60)).toEqual({ value: 1, unit: "minutes" });
    expect(statusDurationParts(3599)).toEqual({ value: 59, unit: "minutes" });
    expect(statusDurationParts(3600)).toEqual({ value: 1, unit: "hours" });
    expect(statusDurationParts(86399)).toEqual({ value: 23, unit: "hours" });
    expect(statusDurationParts(86400)).toEqual({ value: 1, unit: "days" });
  });

  it("clamps corrupt input to 0s instead of rendering it", () => {
    // A negative or NaN duration means a bad timestamp upstream. "0s" is a
    // sane thing to show a user; "-3s" or "NaNd" is not.
    for (const bad of [-1, -10_000, Number.NaN, Number.POSITIVE_INFINITY * 0]) {
      expect(statusDurationParts(bad)).toEqual({ value: 0, unit: "seconds" });
    }
    expect(statusDurationParts(0)).toEqual({ value: 0, unit: "seconds" });
  });

  it("keeps a sub-second visit visible as 0s rather than dropping it", () => {
    // The row exists because the issue really passed through that status.
    expect(statusDurationParts(0.4)).toEqual({ value: 0, unit: "seconds" });
  });

  it("does not overflow into a larger unit for very long durations", () => {
    expect(statusDurationParts(365 * 86400)).toEqual({
      value: 365,
      unit: "days",
    });
  });
});
