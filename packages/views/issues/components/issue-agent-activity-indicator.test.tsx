// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";

const runningTask: AgentTask = {
  id: "task-1",
  agent_id: "agent-1",
  runtime_id: "runtime-1",
  issue_id: "issue-1",
  status: "running",
  priority: 0,
  dispatched_at: null,
  started_at: "2026-07-23T00:00:00Z",
  completed_at: null,
  result: null,
  error: null,
  created_at: "2026-07-23T00:00:00Z",
};

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: () => ({ queryKey: ["agent-tasks"] }),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({
    select,
  }: {
    select: (snapshot: AgentTask[]) => unknown;
  }) => ({ data: select([runningTask]) }),
}));

vi.mock("@multica/ui/components/ui/hover-card", () => ({
  HoverCard: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  HoverCardTrigger: ({ children }: { children: React.ReactNode }) => (
    <span>{children}</span>
  ),
  HoverCardContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("../../agents/components/agent-avatar-stack", () => ({
  AgentAvatarStack: () => <span data-testid="agent-avatar" />,
}));

vi.mock("../../agents/components/agent-activity-hover-content", () => ({
  AgentActivityHoverContent: () => null,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "Working" }),
}));

import { IssueAgentActivityIndicator } from "./issue-agent-activity-indicator";

describe("IssueAgentActivityIndicator", () => {
  it("gives the shimmering label enough line height for descenders", () => {
    render(<IssueAgentActivityIndicator issueId="issue-1" />);

    expect(screen.getByText("Working")).toHaveClass(
      "text-[10px]",
      "leading-[12px]",
      "animate-chat-text-shimmer",
    );
  });
});
