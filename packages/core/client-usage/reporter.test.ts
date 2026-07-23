import { describe, expect, it } from "vitest";
import { usageReportMarker, utcDay } from "./reporter";

describe("usageReportMarker", () => {
  it("is identical for the same day and version", () => {
    expect(usageReportMarker("2026-07-23", "v1")).toBe(
      usageReportMarker("2026-07-23", "v1"),
    );
  });

  it("changes when the version changes on the same day", () => {
    // A same-day client upgrade (e.g. a new Vercel deployment) must produce a
    // new marker so the reporter re-reports instead of skipping until tomorrow.
    expect(usageReportMarker("2026-07-23", "deploy-a")).not.toBe(
      usageReportMarker("2026-07-23", "deploy-b"),
    );
  });

  it("changes when the day changes", () => {
    expect(usageReportMarker("2026-07-23", "v1")).not.toBe(
      usageReportMarker("2026-07-24", "v1"),
    );
  });

  it("falls back to a stable token when version is missing", () => {
    expect(usageReportMarker("2026-07-23")).toBe("2026-07-23:unknown");
  });
});

describe("utcDay", () => {
  it("formats the UTC calendar date", () => {
    expect(utcDay(new Date("2026-07-23T23:59:59Z"))).toBe("2026-07-23");
  });
});
