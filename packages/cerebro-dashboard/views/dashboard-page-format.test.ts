import { describe, expect, it } from "vitest";
import { formatPeriodLabel } from "./dashboard-page";

describe("formatPeriodLabel", () => {
  it("uses the mockup day-before-month order independently of machine locale", () => {
    expect(formatPeriodLabel("2026-06-12T00:00:00Z", "2026-07-11T00:00:00Z")).toBe("12 Jun – 11 Jul");
  });
});
