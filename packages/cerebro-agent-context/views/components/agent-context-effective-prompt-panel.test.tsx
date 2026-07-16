// @vitest-environment jsdom

// FIR-3212 — Setup effective prompt evidence. The Setup screen must describe
// the latest prompt the selected engine actually read, using recorded run
// evidence rather than estimating or reconstructing a prompt from form fields.

import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";

const mockListAgentPromptSnapshots = vi.hoisted(() => vi.fn());
const mockGetAgentPromptSnapshot = vi.hoisted(() => vi.fn());

vi.mock("@multica/cerebro-agent-prompt", () => ({
  listAgentPromptSnapshots: mockListAgentPromptSnapshots,
  getAgentPromptSnapshot: mockGetAgentPromptSnapshot,
}));

import { AgentContextEffectivePromptPanel } from "./agent-context-effective-prompt-panel";

const agent = {
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
  persona_sandbox: "",
} satisfies Agent;

const latest = {
  task_id: "task-latest",
  issue_id: "issue-1",
  agent_context_version: "v1.4.0",
  agent_context_version_id: "version-1",
  provider: "claude",
  model: "claude-sonnet-5",
  runtime_version: "2.1.209",
  system_prompt_mode: "replace",
  sha256_original: "a".repeat(64),
  sha256_redacted: "b".repeat(64),
  total_bytes: 6350,
  redacted: false,
  created_at: "2026-07-16T12:00:00Z",
};

function renderPanel() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>
      <AgentContextEffectivePromptPanel agent={agent} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mockListAgentPromptSnapshots.mockReset();
  mockGetAgentPromptSnapshot.mockReset();
});

describe("AgentContextEffectivePromptPanel", () => {
  it("shows the latest recorded pre-task prompt and its real layer sizes", async () => {
    mockListAgentPromptSnapshots.mockResolvedValue([latest]);
    mockGetAgentPromptSnapshot.mockResolvedValue({
      ...latest,
      layers: [
        {
          name: "runtime_brief",
          delivery: "system_prompt",
          byte_size: 5900,
          sha256_original: "c".repeat(64),
          sha256_redacted: "c".repeat(64),
          content_redacted: "Recorded runtime brief",
        },
        {
          name: "task_prompt",
          delivery: "user_prompt",
          byte_size: 450,
          sha256_original: "d".repeat(64),
          sha256_redacted: "d".repeat(64),
          content_redacted: "Do the task",
        },
      ],
    });

    renderPanel();

    expect(await screen.findByText("5,900 bytes before the task")).toBeInTheDocument();
    expect(screen.getByText("Runtime brief")).toBeInTheDocument();
    expect(screen.getByText("5,900 bytes")).toBeInTheDocument();
    expect(screen.queryByText("450 bytes")).toBeNull();
    expect(screen.getAllByText(/captured from the latest run/i).length).toBeGreaterThan(0);
    expect(screen.getByText(/claude 2\.1\.209/i)).toBeInTheDocument();
    expect(screen.getByText(/v1\.4\.0/i)).toBeInTheDocument();
  });

  it("does not invent an effective prompt when no run evidence exists", async () => {
    mockListAgentPromptSnapshots.mockResolvedValue([]);

    renderPanel();

    expect(
      await screen.findByText(/no recorded prompt for this agent yet/i),
    ).toBeInTheDocument();
    expect(mockGetAgentPromptSnapshot).not.toHaveBeenCalled();
  });
});
