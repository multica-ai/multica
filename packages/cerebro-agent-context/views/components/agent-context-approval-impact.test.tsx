// @vitest-environment jsdom

// FIR-3212 Approval slice (mockup M3) — the approval screen must show what the
// change MEANS, not only which fields differ.
//
// The honesty rules under test:
//   - a change the engine drops SILENTLY outranks one it drops with a log line —
//     the approver has no way to discover a silent drop after the fact;
//   - an instruction that still arrives but loses system-prompt semantics
//     (native → prepend) must be stated as such, not shown as "takes effect"
//     with no qualifier;
//   - a field Multica applies itself must not be credited to the engine;
//   - an engine we have no matrix entry for renders as "we cannot say", never as
//     "this proposal does nothing".

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import type { ReactNode } from "react";

const mockGetAgentCapabilityApproval = vi.hoisted(() => vi.fn());

vi.mock("@multica/cerebro-agent-capabilities", () => ({
  getAgentCapabilityApproval: mockGetAgentCapabilityApproval,
}));

import { AgentContextApprovalImpact } from "./agent-context-approval-impact";

const agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "rt-hermes",
  name: "Kathrine",
} as unknown as Agent;

function runtime(provider: string) {
  return {
    status: "known",
    provider,
    cli_version: "1.0.0",
    runtime_id: "rt-1",
    exec_options: [],
    silently_ignored: [],
    system_prompt: { native: false, modes: [] },
  };
}

function wrapper({ children }: { children: ReactNode }) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
}

beforeEach(() => {
  mockGetAgentCapabilityApproval.mockReset();
});

describe("AgentContextApprovalImpact", () => {
  it("asks the server about exactly the fields the diff says changed", async () => {
    mockGetAgentCapabilityApproval.mockResolvedValue({
      agent_id: "agent-1",
      runtime: runtime("claude"),
      impact: {
        status: "known",
        provider: "claude",
        fields: [
          {
            field: "model",
            delivered_by: "engine",
            exec_field: "model",
            handling: "honoured",
            consequence: "takes_effect",
            silent: false,
          },
        ],
        effective: ["model"],
        ineffective: [],
        silently_ineffective: [],
      },
    });

    render(
      <AgentContextApprovalImpact agent={agent} changedFields={["model"]} />,
      { wrapper },
    );

    await waitFor(() => {
      expect(mockGetAgentCapabilityApproval).toHaveBeenCalledWith("agent-1", [
        "model",
      ]);
    });
  });

  // The reason this panel exists: the field diff renders a large, confident
  // instructions change; hermes discards the system prompt, so approving it
  // changes nothing. The approver must see that BEFORE they click Approve.
  it("says a change will not take effect when the engine drops it", async () => {
    mockGetAgentCapabilityApproval.mockResolvedValue({
      agent_id: "agent-1",
      runtime: runtime("hermes"),
      impact: {
        status: "known",
        provider: "hermes",
        fields: [
          {
            field: "instructions",
            delivered_by: "engine",
            exec_field: "system_prompt",
            handling: "ignored_logged",
            consequence: "no_effect_logged",
            silent: false,
          },
        ],
        effective: [],
        ineffective: ["instructions"],
        silently_ineffective: [],
        system_prompt: { native: false, modes: [], delivery: "ignored" },
      },
    });

    render(
      <AgentContextApprovalImpact
        agent={agent}
        changedFields={["instructions"]}
      />,
      { wrapper },
    );

    const row = await screen.findByTestId("approval-ineffective-instructions");
    expect(row).toHaveAttribute("data-severity", "logged");
    expect(
      screen.getByText(/will not work on the selected engine/i),
    ).toBeInTheDocument();
    expect(screen.getByText("Role and instructions")).toBeInTheDocument();
  });

  // Severity must be in the DOM, not only in a colour: a silent drop is the one
  // the approver can never discover afterwards.
  it("ranks a silent drop above a logged one", async () => {
    mockGetAgentCapabilityApproval.mockResolvedValue({
      agent_id: "agent-1",
      runtime: runtime("openclaw"),
      impact: {
        status: "known",
        provider: "openclaw",
        fields: [
          {
            field: "instructions",
            delivered_by: "engine",
            exec_field: "system_prompt",
            handling: "ignored_logged",
            consequence: "no_effect_logged",
            silent: false,
          },
          {
            field: "mcp_config",
            delivered_by: "engine",
            exec_field: "mcp_config",
            handling: "ignored_silent",
            consequence: "no_effect_silent",
            silent: true,
          },
        ],
        effective: [],
        ineffective: ["instructions", "mcp_config"],
        silently_ineffective: ["mcp_config"],
      },
    });

    render(
      <AgentContextApprovalImpact
        agent={agent}
        changedFields={["instructions", "mcp_config"]}
      />,
      { wrapper },
    );

    await screen.findByTestId("approval-ineffective-mcp_config");
    const rows = screen.getAllByTestId(/^approval-ineffective-/);
    expect(rows[0]).toHaveAttribute("data-severity", "silent");
    expect(rows[1]).toHaveAttribute("data-severity", "logged");
  });

  it("states that a prepended instruction lands without system-prompt authority", async () => {
    mockGetAgentCapabilityApproval.mockResolvedValue({
      agent_id: "agent-1",
      runtime: runtime("kiro"),
      impact: {
        status: "known",
        provider: "kiro",
        fields: [
          {
            field: "instructions",
            delivered_by: "engine",
            exec_field: "system_prompt",
            handling: "honoured",
            consequence: "takes_effect",
            silent: false,
          },
        ],
        effective: ["instructions"],
        ineffective: [],
        silently_ineffective: [],
        system_prompt: {
          native: false,
          modes: ["prepend"],
          delivery: "prepended",
        },
      },
    });

    render(
      <AgentContextApprovalImpact
        agent={agent}
        changedFields={["instructions"]}
      />,
      { wrapper },
    );

    expect(
      await screen.findByText(/receives the role as part of the task/i),
    ).toBeInTheDocument();
  });

  it("says we cannot say for an engine with no matrix entry", async () => {
    mockGetAgentCapabilityApproval.mockResolvedValue({
      agent_id: "agent-1",
      runtime: runtime("mystery"),
      impact: {
        status: "unknown",
        provider: "mystery",
        fields: [],
        effective: [],
        ineffective: [],
        silently_ineffective: [],
      },
    });

    render(
      <AgentContextApprovalImpact agent={agent} changedFields={["model"]} />,
      { wrapper },
    );

    expect(await screen.findByText(/cannot confirm/i)).toBeInTheDocument();
    expect(screen.queryByTestId(/^approval-ineffective-/)).toBeNull();
  });

  // An unreachable endpoint must not take the field diff and the Approve button
  // down with it — the panel is an aid to the approval, not a gate on it.
  it("degrades quietly when the impact cannot be loaded", async () => {
    mockGetAgentCapabilityApproval.mockRejectedValue(new Error("boom"));

    render(
      <AgentContextApprovalImpact agent={agent} changedFields={["model"]} />,
      { wrapper },
    );

    expect(await screen.findByText(/unavailable right now/i)).toBeInTheDocument();
  });

  it("renders nothing when the proposal changes no fields", () => {
    const { container } = render(
      <AgentContextApprovalImpact agent={agent} changedFields={[]} />,
      { wrapper },
    );
    expect(container).toBeEmptyDOMElement();
    expect(mockGetAgentCapabilityApproval).not.toHaveBeenCalled();
  });
});
