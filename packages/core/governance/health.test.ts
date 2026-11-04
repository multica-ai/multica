// @vitest-environment node
import { describe, expect, it } from "vitest";
import { EMPTY_HEALTH_REPORT, parseHealthReport } from "./health";

describe("parseHealthReport", () => {
  it("reads a full report", () => {
    const report = parseHealthReport({
      checked_at: "2026-11-03T10:00:00Z",
      jobs: [{ name: "brief_catchup", label: "Brief catch-up", cadence_seconds: 3600, last_success_at: "2026-11-03T09:00:00Z", lag_seconds: 3600, stuck: false, stuck_reason: "" }],
      delivery_failures: 1,
      knowledge: { offered: true, state: "ready", reason: "" },
      budget_gate: { reported: true, cache_age_seconds: 5 },
      risks: ["one"],
    });
    expect(report.jobs[0]?.last_failed_at).toBeNull();
    expect(report.jobs[0]?.last_success_at).toBe("2026-11-03T09:00:00Z");
    expect(report.budget_gate.cache_age_seconds).toBe(5);
    expect(report.risks).toEqual(["one"]);
  });

  it("fills defaults for missing fields and keeps unknown fields", () => {
    const report = parseHealthReport({ jobs: [{ name: "x" }], future_field: true });
    expect(report.jobs[0]).toMatchObject({ name: "x", stuck: false, lag_seconds: -1, last_success_at: null });
    expect(report.delivery_failures).toBe(0);
    expect(report.risks).toEqual([]);
    expect((report as unknown as { future_field: boolean }).future_field).toBe(true);
  });

  it("degrades a malformed response to a report that claims nothing", () => {
    expect(parseHealthReport(null)).toEqual(EMPTY_HEALTH_REPORT);
    expect(parseHealthReport("nonsense")).toEqual(EMPTY_HEALTH_REPORT);
    expect(parseHealthReport({ jobs: "not a list" })).toEqual(EMPTY_HEALTH_REPORT);
    expect(EMPTY_HEALTH_REPORT.budget_gate.reported).toBe(false);
  });
});
