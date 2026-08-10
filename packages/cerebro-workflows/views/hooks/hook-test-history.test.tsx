// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it } from "vitest";
import { HookTestHistory } from "./hook-test-history";

afterEach(cleanup);

const run = {
  id: "run-1", created_at: "2026-07-16T10:00:00Z",
  policy_id: "policy-1", policy_version: 3,
  source: "before.task.complete · issue FIR-3321",
  source_scope: { kind: "issue", id: "FIR-3321" },
  matched_conditions: ["issue.status eq in_review"],
  matched_steps: ["Trigger", "Scope", "Filter", "Decision"],
  decision: "require" as const, would_action: "Add delivery evidence",
  fail_mode: "closed" as const, remediation: ["Add delivery evidence"],
  dry_run: true, side_effects: false as const, latency_ms: 14,
  event: { event_type: "before.task.complete", agent_id: "agent-1", issue_id: "FIR-3321" },
};

const directory = {
  issue: [{ value: "FIR-3321", label: "Hooks UX", description: "FIR-3321" }],
  agent: [{ value: "agent-1", label: "Lone" }],
};

describe("HookTestHistory", () => {
  it("names who the run belonged to, in plain words, without opening details", () => {
    render(<HookTestHistory directory={directory} runs={[run]} />);

    expect(screen.getByText("Before task completes")).toBeInTheDocument();
    expect(screen.getByText("Lone")).toBeInTheDocument();
    expect(screen.getByText("Would have: Required an outcome. Action: Add delivery evidence.")).toBeInTheDocument();
    expect(screen.getByText(/Hooks UX/)).toBeInTheDocument();
    expect(screen.getByText("To satisfy it:").parentElement).toHaveTextContent("Add delivery evidence");
  });

  it("keeps the raw matched conditions and failure mode behind Show details", async () => {
    const user = userEvent.setup();
    render(<HookTestHistory directory={directory} runs={[run]} />);

    expect(screen.queryByText(/Matched:/)).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Show details" }));
    expect(screen.getByText("Matched:").parentElement).toHaveTextContent("issue.status eq in_review");
    expect(screen.getByText("If the check cannot run:").parentElement).toHaveTextContent("stop");
  });

  it("says an event without an agent has none instead of inventing one", () => {
    render(<HookTestHistory directory={directory} runs={[{ ...run, event: { event_type: "before.task.complete" } }]} />);

    expect(screen.getByText("No agent on this event")).toBeInTheDocument();
  });
});
