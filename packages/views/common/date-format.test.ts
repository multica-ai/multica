import { describe, expect, it } from "vitest";
import { COMPACT_INSTANT_FORMAT, formatInstant } from "./date-format";

describe("formatInstant", () => {
  it("formats with the requested locale and viewing timezone", () => {
    const formatted = formatInstant("2026-01-01T00:30:00Z", {
      locale: "zh-Hans",
      timeZone: "Asia/Shanghai",
      options: COMPACT_INSTANT_FORMAT,
    });

    expect(formatted).toContain("1月1日");
    expect(formatted).toContain("08:30");
  });

  it("falls back to UTC instead of throwing for an invalid timezone", () => {
    expect(() =>
      formatInstant("2026-01-01T00:30:00Z", {
        locale: "en",
        timeZone: "Not/A_Zone",
      }),
    ).not.toThrow();
  });
});
