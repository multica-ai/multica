// @vitest-environment jsdom

// FIR-3212 Swap slice (mockup M2) — moving an agent to another engine must show
// its consequences before it is proposed, not after a run misbehaves.
//
// The honesty rules under test:
//   - a setting the target drops SILENTLY outranks one it drops with a log line
//     — the operator keeps believing a silent drop is enforced;
//   - a prompt that still arrives but loses system-prompt semantics (native →
//     prepend) must be stated as such, not shown as "retained";
//   - an engine we have no matrix entry for renders as "we cannot say", never as
//     a swap that loses everything.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import type { ReactNode } from "react";

const mockGetAgentCapabilitySwap = vi.hoisted(() => vi.fn());

vi.mock("@multica/cerebro-agent-capabilities", () => ({
  getAgentCapabilitySwap: mockGetAgentCapabilitySwap,
}));

import { AgentContextSwapPanel } from "./agent-context-swap-panel";

const baseAgent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "rt-claude",
  name: "Kathrine",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-07-16T00:00:00Z",
  updated_at: "2026-07-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
  persona_sandbox: "",
} as unknown as Agent;

const runtimes = [
  { id: "rt-claude", name: "Mac mini (claude)", provider: "claude" },
  { id: "rt-kiro", name: "Kiro box", provider: "kiro" },
] as unknown as AgentRuntime[];

const claudeToKiro = {
  agent_id: "agent-1",
  same_runtime: false,
  current: {
    status: "known",
    provider: "claude",
    cli_version: "2.1.209",
    runtime_id: "rt-claude",
    exec_options: [],
    silently_ignored: [],
    system_prompt: { native: true, modes: ["append", "replace"] },
  },
  target: {
    status: "known",
    provider: "kiro",
    cli_version: "0.9.0",
    runtime_id: "rt-kiro",
    exec_options: [
      { field: "model", handling: "honoured", effective: true },
      { field: "max_turns", handling: "ignored_logged", effective: false },
    ],
    silently_ignored: ["disallowed_tools"],
    system_prompt: { native: false, modes: ["prepend"] },
  },
  impact: {
    status: "known",
    from: "claude",
    to: "kiro",
    fields: [
      {
        field: "disallowed_tools",
        from_handling: "honoured",
        to_handling: "ignored_silent",
        outcome: "lost",
        silent_on_target: true,
      },
      {
        field: "max_turns",
        from_handling: "honoured",
        to_handling: "ignored_logged",
        outcome: "lost",
        silent_on_target: false,
      },
      {
        field: "model",
        from_handling: "honoured",
        to_handling: "honoured",
        outcome: "retained",
        silent_on_target: false,
      },
    ],
    lost: ["disallowed_tools", "max_turns"],
    gained: [],
    silently_lost: ["disallowed_tools"],
    system_prompt: {
      from_native: true,
      to_native: false,
      from_modes: ["append", "replace"],
      to_modes: ["prepend"],
      outcome: "downgraded_to_prepend",
    },
  },
};

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return render(
    <AgentContextSwapPanel agent={baseAgent} runtimes={runtimes} />,
    { wrapper },
  );
}

async function selectKiro() {
  await userEvent.selectOptions(
    screen.getByLabelText(/compare with another engine/i),
    "rt-kiro",
  );
}

beforeEach(() => {
  mockGetAgentCapabilitySwap.mockReset();
});

describe("AgentContextSwapPanel", () => {
  it("asks for a target before claiming anything", () => {
    renderPanel();
    expect(mockGetAgentCapabilitySwap).not.toHaveBeenCalled();
    expect(screen.getByLabelText(/compare with another engine/i)).toBeInTheDocument();
  });

  it("names every setting the target stops honouring, and why", async () => {
    mockGetAgentCapabilitySwap.mockResolvedValue(claudeToKiro);
    renderPanel();
    await selectKiro();

    await waitFor(() => {
      expect(mockGetAgentCapabilitySwap).toHaveBeenCalledWith(
        "agent-1",
        "rt-kiro",
      );
    });

    expect(await screen.findByText(/stops working on kiro/i)).toBeInTheDocument();
    expect(screen.getAllByText("disallowed_tools").length).toBeGreaterThan(0);
    expect(screen.getAllByText("max_turns").length).toBeGreaterThan(0);
    // The reason is the point of the panel — a bare field list teaches nothing.
    expect(screen.getByText(/without a trace/i)).toBeInTheDocument();
    expect(screen.getByText(/logs that it ignored/i)).toBeInTheDocument();
  });

  it("ranks a silent drop above a logged one", async () => {
    mockGetAgentCapabilitySwap.mockResolvedValue(claudeToKiro);
    renderPanel();
    await selectKiro();

    const rows = await screen.findAllByTestId("swap-lost-field");
    // Warning hierarchy: the silently dropped deny-policy comes first.
    expect(rows[0]).toHaveTextContent("disallowed_tools");
    expect(rows[0]).toHaveAttribute("data-severity", "silent");
    expect(rows[1]).toHaveTextContent("max_turns");
    expect(rows[1]).toHaveAttribute("data-severity", "logged");
  });

  it("says a prepend-only engine strips system-prompt semantics", async () => {
    mockGetAgentCapabilitySwap.mockResolvedValue(claudeToKiro);
    renderPanel();
    await selectKiro();

    expect(
      await screen.findByText(/no longer carries system-prompt semantics/i),
    ).toBeInTheDocument();
  });

  it("treats an unknown target as 'cannot say', never as a total loss", async () => {
    mockGetAgentCapabilitySwap.mockResolvedValue({
      ...claudeToKiro,
      target: {
        ...claudeToKiro.target,
        status: "unknown",
        provider: "some-future-engine",
      },
      impact: {
        status: "unknown",
        from: "claude",
        to: "some-future-engine",
        fields: [],
        lost: [],
        gained: [],
        silently_lost: [],
      },
    });
    renderPanel();
    await selectKiro();

    expect(
      await screen.findByText(/cannot confirm what this engine does/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/stops working on/i)).toBeNull();
    expect(screen.queryAllByTestId("swap-lost-field")).toHaveLength(0);
  });

  it("says plainly when nothing changes", async () => {
    mockGetAgentCapabilitySwap.mockResolvedValue({
      ...claudeToKiro,
      target: claudeToKiro.current,
      same_runtime: true,
      impact: {
        status: "known",
        from: "claude",
        to: "claude",
        fields: [
          {
            field: "model",
            from_handling: "honoured",
            to_handling: "honoured",
            outcome: "retained",
            silent_on_target: false,
          },
        ],
        lost: [],
        gained: [],
        silently_lost: [],
        system_prompt: {
          from_native: true,
          to_native: true,
          from_modes: ["append"],
          to_modes: ["append"],
          outcome: "unchanged",
        },
      },
    });
    renderPanel();
    await selectKiro();

    expect(
      await screen.findByText(/nothing changes on this engine/i),
    ).toBeInTheDocument();
    expect(screen.queryAllByTestId("swap-lost-field")).toHaveLength(0);
  });

  it("does not offer the engine the agent already runs on as a target", () => {
    renderPanel();
    const select = screen.getByLabelText(
      /compare with another engine/i,
    ) as HTMLSelectElement;
    const values = Array.from(select.options).map((o) => o.value);
    expect(values).not.toContain("rt-claude");
    expect(values).toContain("rt-kiro");
  });
});
