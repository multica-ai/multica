// @vitest-environment jsdom
import "@testing-library/jest-dom/vitest";
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ActiveRulesPanel } from "./active-rules-panel";

afterEach(cleanup);
beforeEach(() => {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: vi.fn().mockReturnValue({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }),
  });
});

describe("ActiveRulesPanel", () => {
  it("lets a person choose an agent and issue and read the applicable contracts", () => {
    render(<ActiveRulesPanel
      directory={{
        agent: [{ value: "agent-1", label: "Lone" }],
        issue: [{ value: "issue-1", label: "FIR-4797" }],
      }}
      agentId="agent-1"
      issueId="issue-1"
      onAgentChange={vi.fn()}
      onIssueChange={vi.fn()}
      rules={[{
        id: "rule-1",
        name: "Require a next step",
        contract_rule: "Runs must leave a visible next step.",
        contract_satisfy: "Register a continuation before stopping.",
        events: ["before.task.complete"],
        scope: { kind: "agent", value: "agent-1" },
      }]}
    />);

    expect(screen.getByLabelText("Agent")).toBeInTheDocument();
    expect(screen.getByLabelText("Issue")).toBeInTheDocument();
    expect(screen.getByText("Runs must leave a visible next step.")).toBeInTheDocument();
    expect(screen.getByText("Register a continuation before stopping.")).toBeInTheDocument();
  });

  it("shows a visible error instead of claiming no contracts apply", () => {
    render(<ActiveRulesPanel
      directory={{
        agent: [{ value: "agent-1", label: "Lone" }],
        issue: [{ value: "issue-1", label: "FIR-4797" }],
      }}
      agentId="agent-1"
      issueId="issue-1"
      onAgentChange={vi.fn()}
      onIssueChange={vi.fn()}
      rules={[]}
      error
    />);

    expect(screen.getByRole("alert")).toHaveTextContent("Applicable rules could not be loaded.");
    expect(screen.queryByText("No live Workflow hook contracts apply.")).not.toBeInTheDocument();
  });
});
