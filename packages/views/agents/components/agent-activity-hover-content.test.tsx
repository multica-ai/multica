// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

// The hover card renders one row per task and counts tasks, so its header
// must describe tasks — not agents. A single agent can run several tasks at
// once (e.g. the workspace chip reads "2 working" for two unique agents while
// the card lists three task rows). An agent-worded header here would print
// "3 agents working" for those two agents, contradicting the chip. MUL-3872.

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) =>
      ({ "agent-1": "Niko", "agent-2": "J" })[id] ?? "Unknown Agent",
    getActorInitials: (_type: string, id: string) =>
      ({ "agent-1": "NI", "agent-2": "J" })[id] ?? "UA",
    getActorAvatarUrl: () => null,
  }),
}));

// The card only reads these query results for avatars / availability, never
// for the header count, so empty lists keep the row chrome inert while the
// header still derives from the task array.
vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@multica/core/agents", () => ({
  deriveAgentAvailability: () => "online",
}));

vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: ({ name }: { name: string }) => (
    <span data-testid="actor-avatar">{name}</span>
  ),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return { ...actual, useQuery: () => ({ data: [] }) };
});

import { AgentActivityHoverContent } from "./agent-activity-hover-content";

function makeTask(overrides: Partial<AgentTask>): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-08T08:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-06-08T08:00:00Z",
    ...overrides,
  };
}

afterEach(cleanup);

describe("AgentActivityHoverContent", () => {
  // Two agents, three running tasks (Niko runs two at once). The header must
  // count the three task rows, not the two agents.
  const threeTasksTwoAgents = [
    makeTask({ id: "t1", agent_id: "agent-1" }),
    makeTask({ id: "t2", agent_id: "agent-1" }),
    makeTask({ id: "t3", agent_id: "agent-2" }),
  ];

  it("counts tasks, not agents, in the header", () => {
    renderWithI18n(<AgentActivityHoverContent tasks={threeTasksTwoAgents} />);

    expect(screen.getByText("3 tasks working")).toBeInTheDocument();
    // The old agent-worded copy would have read "3 agents working" here and
    // disagreed with the chip's unique-agent count.
    expect(screen.queryByText(/agents? working/)).not.toBeInTheDocument();
    // One row per task — three avatars for three tasks.
    expect(screen.getAllByTestId("actor-avatar")).toHaveLength(3);
  });

  it("uses the singular task copy for a single task", () => {
    renderWithI18n(<AgentActivityHoverContent tasks={[makeTask({})]} />);

    expect(screen.getByText("1 task working")).toBeInTheDocument();
  });

  it("renders the requested Chinese task copy", () => {
    renderWithI18n(<AgentActivityHoverContent tasks={threeTasksTwoAgents} />, {
      locale: "zh-Hans",
    });

    expect(screen.getByText("3 个 task 工作中")).toBeInTheDocument();
  });
});
