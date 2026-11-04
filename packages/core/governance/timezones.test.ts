// @vitest-environment node
import { describe, expect, it } from "vitest";
import { FALLBACK_TIMEZONES, timezoneOptions } from "./timezones";

describe("timezoneOptions", () => {
  it("sorts the supported zones and keeps the current one", () => {
    const options = timezoneOptions("Asia/Tokyo", () => ["Europe/Paris", "America/New_York"]);
    expect(options).toEqual(["America/New_York", "Asia/Tokyo", "Europe/Paris"]);
  });

  it("adds a saved zone that the runtime does not list", () => {
    expect(timezoneOptions("Mars/Olympus", () => ["UTC"])).toEqual(["Mars/Olympus", "UTC"]);
  });

  it("does not duplicate a zone that is both listed and current", () => {
    expect(timezoneOptions("UTC", () => ["UTC", "Europe/Paris"])).toEqual(["Europe/Paris", "UTC"]);
  });

  it("uses the runtime list when Intl exposes one", () => {
    const options = timezoneOptions("UTC");
    expect(options.length).toBeGreaterThan(0);
    expect(options).toContain("UTC");
  });

  it("falls back to a short list when the runtime cannot list zones", () => {
    const original = Intl.supportedValuesOf;
    try {
      (Intl as { supportedValuesOf?: unknown }).supportedValuesOf = undefined;
      const options = timezoneOptions("UTC");
      expect(options).toEqual([...FALLBACK_TIMEZONES].sort((a, b) => a.localeCompare(b)));
    } finally {
      Intl.supportedValuesOf = original;
    }
  });
});
