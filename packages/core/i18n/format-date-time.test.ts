import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  formatInstant,
  formatInstantWithOffset,
  shortOffsetToken,
} from "./format-date-time";

// 2026-03-01T06:30:45Z == 14:30:45 in Asia/Shanghai (+8), 22:30:45 prev day in LA.
const VALUE = "2026-03-01T06:30:45Z";

describe("formatInstant", () => {
  it("renders the same instant differently per timezone", () => {
    const sh = formatInstant(VALUE, {
      locale: "en",
      timeZone: "Asia/Shanghai",
    });
    const la = formatInstant(VALUE, {
      locale: "en",
      timeZone: "America/Los_Angeles",
    });
    expect(sh).toBe("Mar 1, 2026, 02:30 PM");
    expect(la).toContain("Feb 28, 2026");
    expect(sh).not.toBe(la);
  });

  it("localizes the wall-clock rendering", () => {
    expect(
      formatInstant(VALUE, { locale: "zh-Hans", timeZone: "Asia/Shanghai" }),
    ).toBe("2026年3月1日 14:30");
  });

  it("supports time-only mode", () => {
    expect(
      formatInstant(VALUE, {
        locale: "en",
        timeZone: "Asia/Shanghai",
        mode: "time",
      }),
    ).toBe("02:30:45 PM");
  });

  it("returns empty string for empty/unparseable values", () => {
    expect(formatInstant(null, { locale: "en", timeZone: "UTC" })).toBe("");
    expect(formatInstant("not-a-date", { locale: "en", timeZone: "UTC" })).toBe(
      "",
    );
  });

  it("falls back to UTC instead of throwing on an invalid timezone", () => {
    const utc = formatInstant(VALUE, { locale: "en", timeZone: "UTC" });
    expect(() =>
      formatInstant(VALUE, { locale: "en", timeZone: "Not/AZone" }),
    ).not.toThrow();
    expect(formatInstant(VALUE, { locale: "en", timeZone: "Not/AZone" })).toBe(
      utc,
    );
  });

  it("falls back to a neutral locale instead of throwing on an invalid locale", () => {
    expect(() =>
      formatInstant(VALUE, { locale: "not-a-locale!", timeZone: "UTC" }),
    ).not.toThrow();
    expect(
      formatInstant(VALUE, { locale: "not-a-locale!", timeZone: "UTC" }),
    ).toBe(formatInstant(VALUE, { locale: "en", timeZone: "UTC" }));
  });

  it("keeps the viewer's timezone when only the locale is unsupported", () => {
    // A bad Language locale must not drag the timeZone to UTC: the instant
    // still renders in the requested zone (Shanghai), just in the neutral
    // language — not hours off in UTC.
    const out = formatInstant(VALUE, {
      locale: "not-a-locale!",
      timeZone: "Asia/Shanghai",
    });
    expect(out).toBe(formatInstant(VALUE, { locale: "en", timeZone: "Asia/Shanghai" }));
    expect(out).not.toBe(formatInstant(VALUE, { locale: "en", timeZone: "UTC" }));
  });
});

// Date mode drops the year when the instant is in the viewer's CURRENT calendar
// year (evaluated in the viewer's timezone), and keeps it otherwise so other
// years stay unambiguous. Only "date" mode is affected — datetime/time/tooltip
// keep the year. "Now" is mocked so the assertions are stable across real time.
describe("formatInstant date mode — current-year suppression", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-06-26T12:00:00Z"));
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("omits the year for an instant in the current year", () => {
    expect(
      formatInstant("2026-03-01T06:30:45Z", {
        locale: "en",
        timeZone: "Asia/Shanghai",
        mode: "date",
      }),
    ).toBe("Mar 1");
  });

  it("keeps the year for a past year", () => {
    expect(
      formatInstant("2025-12-20T06:30:45Z", {
        locale: "en",
        timeZone: "Asia/Shanghai",
        mode: "date",
      }),
    ).toBe("Dec 20, 2025");
  });

  it("keeps the year for a future year", () => {
    expect(
      formatInstant("2027-01-05T06:30:45Z", {
        locale: "en",
        timeZone: "Asia/Shanghai",
        mode: "date",
      }),
    ).toBe("Jan 5, 2027");
  });

  it("omits the year in zh-Hans too", () => {
    expect(
      formatInstant("2026-03-01T06:30:45Z", {
        locale: "zh-Hans",
        timeZone: "Asia/Shanghai",
        mode: "date",
      }),
    ).toBe("3月1日");
  });

  it("evaluates the current year in the viewer's timezone, not UTC", () => {
    // now is 2026-01-01T02:00:00Z == 2025-12-31 21:00 in America/New_York, so
    // the current year THERE is 2025. An instant that is also 2025-12-31 in NY
    // shares that year → the year is dropped. (A UTC comparison would see 2026
    // vs 2025 and wrongly keep the year.)
    vi.setSystemTime(new Date("2026-01-01T02:00:00Z"));
    expect(
      formatInstant("2026-01-01T02:30:00Z", {
        locale: "en",
        timeZone: "America/New_York",
        mode: "date",
      }),
    ).toBe("Dec 31");
  });

  it("does not affect datetime mode", () => {
    expect(
      formatInstant("2026-03-01T06:30:45Z", {
        locale: "en",
        timeZone: "Asia/Shanghai",
      }),
    ).toBe("Mar 1, 2026, 02:30 PM");
  });
});

describe("formatInstantWithOffset", () => {
  it("appends a spaced half-width GMT offset for en", () => {
    expect(
      formatInstantWithOffset(VALUE, {
        locale: "en",
        timeZone: "Asia/Shanghai",
      }),
    ).toBe("Mar 1, 2026, 02:30:45 PM (GMT+8)");
  });

  it("appends a full-width GMT offset for zh", () => {
    expect(
      formatInstantWithOffset(VALUE, {
        locale: "zh-Hans",
        timeZone: "Asia/Shanghai",
      }),
    ).toBe("2026年3月1日 14:30:45（GMT+8）");
  });

  it("reflects the offset of the chosen timezone", () => {
    expect(
      formatInstantWithOffset(VALUE, {
        locale: "en",
        timeZone: "America/Los_Angeles",
      }),
    ).toContain("(GMT-8)");
    expect(
      formatInstantWithOffset(VALUE, { locale: "en", timeZone: "UTC" }),
    ).toContain("(GMT+0)");
  });

  it("renders the base time without a misleading offset on an invalid timezone", () => {
    expect(() =>
      formatInstantWithOffset(VALUE, { locale: "en", timeZone: "Not/AZone" }),
    ).not.toThrow();
    const out = formatInstantWithOffset(VALUE, {
      locale: "en",
      timeZone: "Not/AZone",
    });
    // Visible time degrades to UTC (must not white-screen), but the unknown
    // zone has no knowable offset, so NO "(GMT…)" suffix is appended — never a
    // misleading "(GMT+0)". The UTC variant is exactly this base plus "(GMT+0)".
    expect(`${out} (GMT+0)`).toBe(
      formatInstantWithOffset(VALUE, { locale: "en", timeZone: "UTC" }),
    );
    expect(out).not.toContain("GMT");
  });

  it("renders the base time (no offset) without throwing when the engine lacks shortOffset support", async () => {
    // Older engines (Safari < 15.4 / old WebViews) support Intl but throw
    // RangeError on the timeZoneName value "shortOffset" — regardless of
    // locale/timeZone. Must degrade to the plain time, never white-screen.
    vi.resetModules(); // fresh formatter cache so the mock is exercised
    const RealDTF = Intl.DateTimeFormat;
    const spy = vi.spyOn(Intl, "DateTimeFormat").mockImplementation(
      // Must be a `function` (not an arrow) so it works under `new`.
      function (locale?: unknown, options?: Intl.DateTimeFormatOptions) {
        if (options && "timeZoneName" in options) {
          throw new RangeError("unsupported timeZoneName value");
        }
        return new RealDTF(locale as string, options);
      } as unknown as typeof Intl.DateTimeFormat,
    );
    try {
      const { formatInstantWithOffset: format } = await import(
        "./format-date-time"
      );
      let out = "";
      expect(() => {
        out = format(VALUE, { locale: "en", timeZone: "Europe/Berlin" });
      }).not.toThrow();
      // Base time still rendered; just no "(GMT…)" suffix.
      expect(out).toContain("2026");
      expect(out).not.toContain("GMT");
    } finally {
      spy.mockRestore();
    }
  });
});

describe("shortOffsetToken", () => {
  const at = new Date(VALUE);

  it("extracts the GMT offset token for a timezone", () => {
    expect(shortOffsetToken(at, "en", "Asia/Shanghai")).toBe("GMT+8");
    expect(shortOffsetToken(at, "en", "UTC")).toBe("GMT+0");
    expect(shortOffsetToken(at, "en", "America/Los_Angeles")).toBe("GMT-8");
  });

  it("returns empty (not a misleading GMT+0) for an unknown timezone", () => {
    expect(() => shortOffsetToken(at, "en", "Not/AZone")).not.toThrow();
    // A stale/ICU-unsupported zone has no knowable offset; must degrade to ""
    // so callers fall back to the raw zone name, never a wrong "GMT+0".
    expect(shortOffsetToken(at, "en", "Not/AZone")).toBe("");
  });

  it("keeps the zone's offset when only the locale is unsupported", () => {
    expect(shortOffsetToken(at, "not-a-locale!", "Asia/Shanghai")).toBe(
      "GMT+8",
    );
  });
});
