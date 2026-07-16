// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { HookTestHistory } from "./hook-test-history";

describe("HookTestHistory", () => {
  it("explains the policy, scope, conditions, failure mode, and remediation", () => {
    render(<HookTestHistory directory={{ issue: [{ value: "FIR-3321", label: "Hooks UX", description: "FIR-3321" }] }} runs={[{
      id: "run-1", created_at: "2026-07-16T10:00:00Z",
      policy_id: "policy-1", policy_version: 3,
      source: "before.task.complete · issue FIR-3321",
      source_scope: { kind: "issue", id: "FIR-3321" },
      matched_conditions: ["issue.status eq in_review"],
      matched_steps: ["Trigger", "Scope", "Filter", "Decision"],
      decision: "require", would_action: "Add delivery evidence",
      fail_mode: "closed", remediation: ["Add delivery evidence"],
      side_effects: false, latency_ms: 14,
    }]} />);

    expect(screen.getByText("Policy version 3")).toBeInTheDocument();
    expect(screen.getByText("Source scope: Hooks UX")).toBeInTheDocument();
    expect(screen.getByText("issue.status eq in_review")).toBeInTheDocument();
    expect(screen.getByText("Fail closed")).toBeInTheDocument();
    expect(screen.getByText("Add delivery evidence")).toBeInTheDocument();
  });
});
