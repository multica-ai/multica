// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  snapshot: [] as unknown[],
  // Captures the agent ids handed to the avatar stack so a test can assert
  // the stack still reflects distinct agents even when the count counts issues.
  avatarAgentIds: undefined as string[] | undefined,
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
  AgentAvatarStack: ({ agentIds }: { agentIds: string[] }) => {
    mockState.avatarAgentIds = agentIds;
    return <div data-testid="agent-avatar-stack">{agentIds.length}</div>;
  },
}));

vi.mock("../../agents/components/agent-activity-hover-content", () => ({
  AgentActivityHoverContent: ({ tasks }: { tasks: AgentTask[] }) => (
    <div data-testid="activity-hover">{tasks.length}</div>
  ),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: (opts: { queryKey?: readonly unknown[] }) => {
      if (opts.queryKey?.[1] === "task-snapshot") {
        return { data: mockState.snapshot };
      }
      return { data: undefined };
    },
  };
});

import { WorkspaceAgentWorkingChip } from "./workspace-agent-working-chip";

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

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockState.snapshot = [];
  mockState.avatarAgentIds = undefined;
});

describe("WorkspaceAgentWorkingChip", () => {
  it("counts distinct active issues, not running agents", () => {
    // Two agents working the SAME issue: the count is about issues, so it
    // must read "1", not "2" (the old unique-agent behavior). MUL-3875.
    mockState.snapshot = [
      makeTask({ id: "t-1", agent_id: "agent-1", issue_id: "issue-1" }),
      makeTask({ id: "t-2", agent_id: "agent-2", issue_id: "issue-1" }),
    ];

    renderWithI18n(
      <WorkspaceAgentWorkingChip value={false} onToggle={() => {}} />,
    );

    expect(
      screen.getByRole("button", { name: /working/i }),
    ).toHaveTextContent("1");
    // The avatar stack still shows both distinct agents behind that work.
    expect(mockState.avatarAgentIds).toEqual(["agent-1", "agent-2"]);
  });

  it("counts each distinct issue once when agents span several issues", () => {
    mockState.snapshot = [
      makeTask({ id: "t-1", agent_id: "agent-1", issue_id: "issue-1" }),
      makeTask({ id: "t-2", agent_id: "agent-2", issue_id: "issue-2" }),
      makeTask({ id: "t-3", agent_id: "agent-1", issue_id: "issue-3" }),
    ];

    renderWithI18n(
      <WorkspaceAgentWorkingChip value={false} onToggle={() => {}} />,
    );

    expect(
      screen.getByRole("button", { name: /working/i }),
    ).toHaveTextContent("3");
  });

  it("ignores non-running tasks and respects scopedIssueIds", () => {
    mockState.snapshot = [
      makeTask({ id: "t-1", issue_id: "issue-1", status: "running" }),
      makeTask({ id: "t-2", issue_id: "issue-2", status: "queued" }),
      makeTask({ id: "t-3", issue_id: "issue-3", status: "running" }),
    ];

    renderWithI18n(
      <WorkspaceAgentWorkingChip
        value={false}
        onToggle={() => {}}
        scopedIssueIds={new Set(["issue-1"])}
      />,
    );

    // Only the running task within scope counts → "1".
    expect(
      screen.getByRole("button", { name: /working/i }),
    ).toHaveTextContent("1");
  });

  it("shows 0 when no agents are running", () => {
    mockState.snapshot = [];

    renderWithI18n(
      <WorkspaceAgentWorkingChip value={false} onToggle={() => {}} />,
    );

    expect(
      screen.getByRole("button", { name: /working/i }),
    ).toHaveTextContent("0");
  });
});
