import { describe, expect, it } from "vitest";
import { formatRunResult } from "./autopilot-detail-page";

describe("formatRunResult", () => {
  it("extracts the final output captured from an agent task", () => {
    expect(formatRunResult({ output: "  Weekly report complete.  " })).toBe(
      "Weekly report complete.",
    );
  });

  it("supports plain and structured results", () => {
    expect(formatRunResult("  Done. ")).toBe("Done.");
    expect(formatRunResult({ count: 3 })).toBe('{"count":3}');
  });

  it("keeps missing or empty output explicit", () => {
    expect(formatRunResult(null)).toBeNull();
    expect(formatRunResult({ output: "  " })).toBeNull();
    expect(formatRunResult({})).toBeNull();
  });
});
