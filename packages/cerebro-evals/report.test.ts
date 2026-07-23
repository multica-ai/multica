import { describe, expect, it } from "vitest";
import { parseRunReport, passLabel } from "./report";
import type { EvalReport } from "./types";

function expectReport(results: Record<string, unknown>): EvalReport {
  const report = parseRunReport(results);
  if (!report) throw new Error("expected a parsed report");
  return report;
}

describe("parseRunReport", () => {
  it("parses a full runner.Report payload", () => {
    const report = expectReport({
      cases: [
        { case_id: "c1", situation: "Refund request", expected: "offer refund", produced: "offered refund", passed: true, critical: true, reason: "matched policy" },
        { case_id: "c2", situation: "Spam", expected: "ignore", produced: "replied", passed: false, critical: false, reason: "should have ignored" },
      ],
      outcome: { total: 2, passed: 1, pass_rate: 0.5, critical_total: 1, critical_failed: 0, min_pass_rate: 0.8, threshold_met: false, critical_rule_met: true, status: "failed" },
      cost_cents: 12,
      latency_ms: 3400,
    });
    expect(report.cases).toHaveLength(2);
    expect(report.cases[0]?.passed).toBe(true);
    expect(report.outcome.status).toBe("failed");
    expect(report.outcome.pass_rate).toBe(0.5);
    expect(report.cost_cents).toBe(12);
  });

  it("returns null for runs with no cases key (pre-engine runs)", () => {
    expect(parseRunReport({})).toBeNull();
    expect(parseRunReport(undefined)).toBeNull();
    expect(parseRunReport(null)).toBeNull();
    expect(parseRunReport({ some: "legacy shape" })).toBeNull();
  });

  it("fills defaults for a partially-drifted payload rather than crashing", () => {
    const report = expectReport({ cases: [{ case_id: "c1" }] });
    expect(report.cases[0]?.situation).toBe("");
    expect(report.cases[0]?.passed).toBe(false);
    expect(report.outcome.status).toBe("failed");
  });

  it("preserves a per-task error", () => {
    const report = expectReport({ cases: [{ case_id: "c1", passed: false, error: "grader failed to score the answer" }] });
    expect(report.cases[0]?.error).toBe("grader failed to score the answer");
  });
});

describe("passLabel", () => {
  it("labels error over pass/fail", () => {
    expect(passLabel(false, "boom")).toBe("Error");
    expect(passLabel(true, "boom")).toBe("Error");
  });
  it("labels pass and fail", () => {
    expect(passLabel(true)).toBe("Pass");
    expect(passLabel(false)).toBe("Fail");
  });
});
