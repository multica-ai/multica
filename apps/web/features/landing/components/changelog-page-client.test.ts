import { describe, expect, it } from "vitest";
import { fullDateLabel, monthYearLabel } from "./changelog-page-client";

describe("changelog date labels", () => {
  it("formats month labels for each landing locale", () => {
    expect(monthYearLabel(2026, 1, "en")).toBe("January 2026");
    expect(monthYearLabel(2026, 1, "zh-Hans")).toBe("2026年1月");
    expect(monthYearLabel(2026, 1, "ko")).toBe("2026년 1월");
    expect(monthYearLabel(2026, 1, "ja")).toBe("2026年1月");
  });

  it("formats full dates for each landing locale", () => {
    expect(fullDateLabel("2026-01-15", "en")).toBe("January 15, 2026");
    expect(fullDateLabel("2026-01-15", "zh-Hans")).toBe("2026年1月15日");
    expect(fullDateLabel("2026-01-15", "ko")).toBe("2026년 1월 15일");
    expect(fullDateLabel("2026-01-15", "ja")).toBe("2026年1月15日");
  });

  it("keeps invalid release dates unchanged", () => {
    expect(fullDateLabel("not-a-date", "ko")).toBe("not-a-date");
  });
});
