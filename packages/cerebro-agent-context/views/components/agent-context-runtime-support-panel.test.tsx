// @vitest-environment jsdom

// FIR-3212 — Setup screen, capability-driven fields: the runtime support panel
// tells the operator which settings the agent's engine actually honours, which
// it drops silently, and how it accepts a system prompt. The honesty rules
// under test: unknown support must never render as "supports nothing", and a
// prepend-only engine must never be presented as having system-prompt
// semantics.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import type { ReactNode } from "react";

const mockGetAgentCapabilities = vi.hoisted(() => vi.fn());

vi.mock("@multica/cerebro-agent-capabilities", () => ({
  getAgentCapabilities: mockGetAgentCapabilities,
}));

import { AgentContextRuntimeSupportPanel } from "./agent-context-runtime-support-panel";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
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
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function card(runtimeOptions: Record<string, unknown>) {
  return {
    agent_id: "agent-1",
    runtime_options: runtimeOptions,
  };
}

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return render(<AgentContextRuntimeSupportPanel agent={baseAgent} />, {
    wrapper,
  });
}

beforeEach(() => {
  mockGetAgentCapabilities.mockReset();
});

describe("AgentContextRuntimeSupportPanel", () => {
  it("shows provider, CLI version, honoured/silent settings and native prompt modes", async () => {
    mockGetAgentCapabilities.mockResolvedValue(
      card({
        status: "known",
        provider: "claude",
        cli_version: "2.1.209",
        runtime_id: "runtime-1",
        exec_options: [
          { field: "model", handling: "honoured", effective: true },
          { field: "max_turns", handling: "ignored_logged", effective: false },
          {
            field: "disallowed_mcp_tools",
            handling: "ignored_silent",
            effective: false,
          },
        ],
        silently_ignored: ["disallowed_mcp_tools"],
        system_prompt: { native: true, modes: ["append", "replace"] },
      }),
    );

    renderPanel();

    await waitFor(() => {
      expect(screen.getByText("claude")).toBeInTheDocument();
    });
    expect(screen.getByText("2.1.209")).toBeInTheDocument();
    // The engine drives the fields — the operator must be told that.
    expect(
      screen.getByText(/fields below come from this engine/i),
    ).toBeInTheDocument();
    // Silent drops are the security-relevant part; they must be called out.
    expect(
      screen.getByText(/drops these settings silently/i),
    ).toBeInTheDocument();
    expect(screen.getAllByText("disallowed_mcp_tools").length).toBeGreaterThan(
      0,
    );
    // Native system-prompt modes render as supported.
    expect(screen.getByText("Replace")).toBeInTheDocument();
    expect(screen.getByText("Append")).toBeInTheDocument();
    // Per-field handling labels.
    expect(screen.getByText("Honoured")).toBeInTheDocument();
    expect(screen.getByText("Ignored (logged)")).toBeInTheDocument();
    expect(screen.getByText("Ignored silently")).toBeInTheDocument();
  });

  it("treats unknown support as 'cannot say', never as 'supports nothing'", async () => {
    mockGetAgentCapabilities.mockResolvedValue(
      card({
        status: "unknown",
        provider: "some-future-engine",
        cli_version: "",
        runtime_id: "runtime-1",
        exec_options: [],
        silently_ignored: [],
      }),
    );

    renderPanel();

    await waitFor(() => {
      expect(
        screen.getByText(/no authoritative support data/i),
      ).toBeInTheDocument();
    });
    // It must NOT claim anything is ignored or unsupported.
    expect(screen.queryByText(/drops these settings silently/i)).toBeNull();
    expect(screen.queryByText(/ignores a custom system prompt/i)).toBeNull();
  });

  it("labels a prepend-only engine honestly instead of claiming system-prompt semantics", async () => {
    mockGetAgentCapabilities.mockResolvedValue(
      card({
        status: "known",
        provider: "opencode",
        cli_version: "1.17.11",
        runtime_id: "runtime-1",
        exec_options: [
          { field: "system_prompt", handling: "honoured", effective: true },
        ],
        silently_ignored: [],
        system_prompt: { native: false, modes: ["prepend"] },
      }),
    );

    renderPanel();

    await waitFor(() => {
      expect(
        screen.getByText(/no native system-prompt channel/i),
      ).toBeInTheDocument();
    });
  });

  it("says when the engine ignores a custom system prompt entirely", async () => {
    mockGetAgentCapabilities.mockResolvedValue(
      card({
        status: "known",
        provider: "gemini",
        cli_version: "0.44.1",
        runtime_id: "runtime-1",
        exec_options: [
          { field: "system_prompt", handling: "ignored_silent", effective: false },
        ],
        silently_ignored: ["system_prompt"],
        system_prompt: { native: false, modes: [] },
      }),
    );

    renderPanel();

    await waitFor(() => {
      expect(
        screen.getByText(/ignores a custom system prompt/i),
      ).toBeInTheDocument();
    });
  });
});
