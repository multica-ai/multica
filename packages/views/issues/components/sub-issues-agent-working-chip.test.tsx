import { cleanup, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { WorkspaceWorkingAgent } from "@multica/core/types";

const mockState = vi.hoisted(() => ({
  agents: [] as WorkspaceWorkingAgent[],
  optionsCalls: [] as unknown[][],
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/agents", () => ({
  workspaceWorkingAgentsOptions: (...args: unknown[]) => {
    mockState.optionsCalls.push(args);
    return { queryKey: ["working-agents", ...args] };
  },
}));

vi.mock("../../agents/components/agent-avatar-stack", () => ({
  AgentAvatarStack: ({ agentIds }: { agentIds: string[] }) => (
    <div data-testid="agent-avatar-stack">{agentIds.join(",")}</div>
  ),
}));

vi.mock("./workspace-agent-working-chip", () => ({
  WorkingAgentsHoverContent: ({
    agents,
  }: {
    agents: readonly WorkspaceWorkingAgent[];
  }) => <div data-testid="hover-body">{agents.length}</div>,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (
      selector: (keys: Record<string, Record<string, string>>) => string,
      options?: { count?: number },
    ) => {
      const key = selector(
        new Proxy(
          {},
          {
            get: (_t, ns: string) =>
              new Proxy({}, { get: (_n, k: string) => `${ns}.${k}` }),
          },
        ) as Record<string, Record<string, string>>,
      );
      return options?.count !== undefined ? `${key}:${options.count}` : key;
    },
  }),
}));

// The hover card only portals its content once open, so absence of the body
// cannot distinguish "closed" from "not wired up". Mock the primitive and
// assert on the wrapper itself (same approach as the row indicator's test).
vi.mock("@multica/ui/components/ui/hover-card", () => ({
  HoverCard: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="hover-card">{children}</div>
  ),
  HoverCardTrigger: ({ children }: { children: React.ReactNode }) => (
    <span data-testid="hover-card-trigger">{children}</span>
  ),
  HoverCardContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return { ...actual, useQuery: () => ({ data: mockState.agents }) };
});

import { SubIssuesAgentWorkingChip } from "./sub-issues-agent-working-chip";

function makeAgent(
  overrides: Partial<WorkspaceWorkingAgent> = {},
): WorkspaceWorkingAgent {
  return {
    id: "agent-1",
    name: "Agent One",
    avatar_url: null,
    running_task_count: 1,
    issue_ids: ["child-1"],
    ...overrides,
  };
}

beforeEach(() => {
  cleanup();
  mockState.agents = [];
  mockState.optionsCalls = [];
});

describe("SubIssuesAgentWorkingChip", () => {
  it("asks the server to narrow the projection to the parent's children", () => {
    mockState.agents = [makeAgent()];

    render(<SubIssuesAgentWorkingChip parentIssueId="parent-1" />);

    // type=issue plus the parent id, and no My Issues relation. Getting this
    // wrong silently widens the chip to the whole workspace.
    expect(mockState.optionsCalls).toEqual([
      ["ws-1", "issue", undefined, "parent-1"],
    ]);
  });

  it("counts the agents the server returned", () => {
    mockState.agents = [
      makeAgent({ id: "agent-1", issue_ids: ["child-1"] }),
      makeAgent({ id: "agent-2", issue_ids: ["child-2"] }),
    ];

    render(<SubIssuesAgentWorkingChip parentIssueId="parent-1" />);

    expect(
      screen.getByText("agent_activity.chip_agents_working:2"),
    ).not.toBeNull();
    expect(screen.getByTestId("agent-avatar-stack").textContent).toBe(
      "agent-1,agent-2",
    );
  });

  it("hands the same agents to the hover body as it counted", () => {
    mockState.agents = [
      makeAgent({ id: "agent-1" }),
      makeAgent({ id: "agent-2" }),
      makeAgent({ id: "agent-3" }),
    ];

    render(<SubIssuesAgentWorkingChip parentIssueId="parent-1" />);

    // The number and the hover body must never disagree — they are the same
    // list, not two derivations of one snapshot.
    expect(
      screen.getByText("agent_activity.chip_agents_working:3"),
    ).not.toBeNull();
    expect(screen.getByTestId("hover-body").textContent).toBe("3");
  });

  it("renders nothing when no agent is working on a sub-issue", () => {
    const { container } = render(
      <SubIssuesAgentWorkingChip parentIssueId="parent-1" />,
    );

    expect(container.firstChild).toBeNull();
  });
});
