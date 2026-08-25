// @vitest-environment node

import { describe, expect, it } from "vitest";
import { formatElapsedMs, formatElapsedSecs } from "./format-elapsed";

describe("elapsed formatting", () => {
  it("formats seconds and mixed minute durations", () => {
    expect(formatElapsedSecs(59)).toBe("59s");
    expect(formatElapsedSecs(61)).toBe("1m 1s");
    expect(formatElapsedSecs(120)).toBe("2m");
  });

  it("rounds milliseconds to the nearest second", () => {
    expect(formatElapsedMs(61_400)).toBe("1m 1s");
  });
});
