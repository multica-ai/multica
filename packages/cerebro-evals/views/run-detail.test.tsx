// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { RunDetail } from "./run-detail";
import type { EvalRun } from "../types";

function makeRun(overrides: Partial<EvalRun> = {}): EvalRun {
  return {
    id: "run-1", workspace_id: "w1", eval_id: "e1", eval_version: "1.0.0",
    target_version: "abc123", status: "failed", results: {}, cost_cents: 12,
    latency_ms: 3400, created_at: "2026-07-18T06:00:00Z", ...overrides,
  };
}

const fullResults = {
  cases: [
    { case_id: "refund", situation: "Angry refund request", expected: "offer refund", produced: "offered refund", passed: true, critical: true, reason: "matched policy" },
    { case_id: "spam", situation: "Spam message", expected: "ignore", produced: "replied politely", passed: false, critical: false, reason: "should have ignored the message" },
  ],
  outcome: { total: 2, passed: 1, pass_rate: 0.5, critical_total: 1, critical_failed: 0, min_pass_rate: 0.8, threshold_met: false, critical_rule_met: true, status: "failed" },
  cost_cents: 12,
  latency_ms: 3400,
};

describe("RunDetail", () => {
  it("shows per-task answer, grader reason, and pass/fail verdicts", () => {
    render(<RunDetail run={makeRun({ results: fullResults })} onClose={() => {}} />);
    expect(screen.getByText("Angry refund request")).toBeInTheDocument();
    expect(screen.getByText("offered refund")).toBeInTheDocument();
    expect(screen.getByText("matched policy")).toBeInTheDocument();
    expect(screen.getByText("should have ignored the message")).toBeInTheDocument();
    expect(screen.getByText("Pass")).toBeInTheDocument();
    expect(screen.getByText("Fail")).toBeInTheDocument();
    // Outcome summary shows the trustworthy verdict, not a told-from-outside number.
    expect(screen.getByText("1/2 · 50%")).toBeInTheDocument();
  });

  it("surfaces a per-task error instead of a produced answer", () => {
    const run = makeRun({ results: { cases: [{ case_id: "c1", situation: "s", expected: "e", passed: false, critical: false, reason: "grader failed to score the answer", error: "grader timeout" }], outcome: { total: 1, passed: 0, pass_rate: 0, critical_total: 0, critical_failed: 0, min_pass_rate: 0.8, threshold_met: false, critical_rule_met: true, status: "failed" }, cost_cents: 0, latency_ms: 5 } });
    render(<RunDetail run={run} onClose={() => {}} />);
    expect(screen.getByText("grader timeout")).toBeInTheDocument();
    expect(screen.getByText("Error")).toBeInTheDocument();
  });

  it("degrades gracefully for runs with no per-task detail", () => {
    render(<RunDetail run={makeRun({ results: {} })} onClose={() => {}} />);
    expect(screen.getByText(/No per-task detail recorded/i)).toBeInTheDocument();
  });

  it("opens the evidence artifact when present", () => {
    const onOpenEvidence = vi.fn();
    render(<RunDetail run={makeRun({ results: fullResults, evidence_artifact_id: "art-1" })} onClose={() => {}} onOpenEvidence={onOpenEvidence} />);
    fireEvent.click(screen.getByText("Evidence"));
    expect(onOpenEvidence).toHaveBeenCalledWith("art-1");
  });

  it("does not offer evidence when the run has none", () => {
    render(<RunDetail run={makeRun({ results: fullResults })} onClose={() => {}} onOpenEvidence={() => {}} />);
    expect(screen.queryByText("Evidence")).not.toBeInTheDocument();
  });
});
