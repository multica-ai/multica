import { cleanup, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";

const mockState = vi.hoisted(() => ({
  snapshot: [] as unknown[],
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: (wsId: string) => ({
    queryKey: ["agents", "task-snapshot", wsId],
  }),
}));

vi.mock("../../agents/components/agent-avatar-stack", () => ({
  AgentAvatarStack: ({
    agentIds,
    opacity,
  }: {
    agentIds: string[];
    opacity?: string;
  }) => (
    <div data-testid="agent-avatar-stack" data-opacity={opacity}>
      {agentIds.length}
    </div>
  ),
}));

vi.mock("../../agents/components/agent-activity-hover-content", () => ({
  AgentActivityHoverContent: ({ tasks }: { tasks: AgentTask[] }) => (
    <div data-testid="activity-hover">{tasks.length}</div>
  ),
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
  return {
    ...actual,
    useQuery: (opts: {
      queryKey?: readonly unknown[];
      select?: (data: unknown) => unknown;
    }) => {
      if (opts.queryKey?.[1] === "task-snapshot") {
        return {
          data: opts.select
            ? opts.select(mockState.snapshot)
            : mockState.snapshot,
        };
      }
      return { data: undefined };
    },
  };
});

import { SubIssuesAgentWorkingChip } from "./sub-issues-agent-working-chip";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "child-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-06-08T08:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-06-08T08:00:00Z",
    ...overrides,
  } as AgentTask;
}

const CHILD_IDS = ["child-1", "child-2"];

beforeEach(() => {
  cleanup();
  mockState.snapshot = [makeTask()];
});

describe("SubIssuesAgentWorkingChip", () => {
  it("shows a working count aggregated across the given sub-issues", () => {
    mockState.snapshot = [
      makeTask({ id: "t1", agent_id: "agent-1", issue_id: "child-1" }),
      makeTask({ id: "t2", agent_id: "agent-2", issue_id: "child-2" }),
      // Second task by an already-counted agent — counts agents, not tasks.
      makeTask({ id: "t3", agent_id: "agent-2", issue_id: "child-1" }),
      // Unrelated issue — must not leak into the aggregate.
      makeTask({ id: "t4", agent_id: "agent-3", issue_id: "other-issue" }),
    ];

    render(<SubIssuesAgentWorkingChip issueIds={CHILD_IDS} />);

    expect(screen.getByText("agent_activity.chip_agents_working:2")).not.toBeNull();
    expect(screen.getByTestId("agent-avatar-stack").textContent).toBe("2");
  });

  it("falls back to a muted queued state when nothing is running", () => {
    mockState.snapshot = [
      makeTask({ id: "t1", agent_id: "agent-1", status: "queued" }),
    ];

    render(<SubIssuesAgentWorkingChip issueIds={CHILD_IDS} />);

    expect(screen.getByText("agent_activity.hover_header_queued:1")).not.toBeNull();
    expect(screen.getByTestId("agent-avatar-stack").getAttribute("data-opacity")).toBe(
      "half",
    );
  });

  it("wraps the chip in a hover card listing every active task", () => {
    mockState.snapshot = [
      makeTask({ id: "t1", agent_id: "agent-1", issue_id: "child-1" }),
      makeTask({ id: "t2", agent_id: "agent-2", issue_id: "child-2", status: "queued" }),
    ];

    render(<SubIssuesAgentWorkingChip issueIds={CHILD_IDS} />);

    expect(screen.getByTestId("hover-card")).not.toBeNull();
    expect(screen.getByTestId("activity-hover").textContent).toBe("2");
  });

  it("renders nothing when no agent is on any sub-issue", () => {
    mockState.snapshot = [
      makeTask({ id: "t1", issue_id: "other-issue" }),
      // Terminal statuses belong to history, not the live chip.
      makeTask({ id: "t2", issue_id: "child-1", status: "completed" }),
    ];

    const { container } = render(
      <SubIssuesAgentWorkingChip issueIds={CHILD_IDS} />,
    );

    expect(container.firstChild).toBeNull();
  });
});
