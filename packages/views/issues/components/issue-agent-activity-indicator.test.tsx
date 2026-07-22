// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  running: [] as AgentTask[],
  queued: [] as AgentTask[],
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: () => ({
    queryKey: ["agents", "task-snapshot", "ws-1"],
  }),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual =
    await vi.importActual<typeof import("@tanstack/react-query")>(
      "@tanstack/react-query",
    );
  return {
    ...actual,
    useQuery: () => ({
      data: { running: mockState.running, queued: mockState.queued },
    }),
  };
});

vi.mock("@multica/ui/components/ui/hover-card", async () => {
  const React = await vi.importActual<typeof import("react")>("react");
  return {
    HoverCard: ({ children }: { children: React.ReactNode }) => <>{children}</>,
    HoverCardTrigger: ({
      render,
      children,
    }: {
      render: React.ReactElement;
      children: React.ReactNode;
    }) => React.cloneElement(render, undefined, children),
    HoverCardContent: ({ children }: { children: React.ReactNode }) => (
      <div>{children}</div>
    ),
  };
});

vi.mock("../../agents/components/agent-avatar-stack", () => ({
  AgentAvatarStack: () => <span data-testid="agent-avatar" />,
}));

vi.mock("../../agents/components/agent-activity-hover-content", () => ({
  AgentActivityHoverContent: () => null,
}));

import { IssueAgentActivityIndicator } from "./issue-agent-activity-indicator";

function makeTask(): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-07-23T08:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-07-23T08:00:00Z",
  };
}

beforeEach(() => {
  cleanup();
  vi.clearAllMocks();
  mockState.running = [];
  mockState.queued = [];
});

describe("IssueAgentActivityIndicator", () => {
  it("leaves enough line height for descenders in the running label", () => {
    mockState.running = [makeTask()];

    renderWithI18n(<IssueAgentActivityIndicator issueId="issue-1" />);

    const label = screen.getByText("Working");
    expect(label).toHaveClass("leading-4");
    expect(label).not.toHaveClass("leading-none");
  });
});
