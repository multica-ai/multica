// @vitest-environment jsdom

import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/agents", () => ({
  agentTaskSnapshotOptions: () => ({}),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({
    data: {
      running: [{ agent_id: "agent-1" }],
      queued: [],
    },
  }),
}));

vi.mock("@multica/ui/components/ui/hover-card", () => ({
  HoverCard: ({ children }: { children: React.ReactNode }) => children,
  HoverCardTrigger: ({ children }: { children: React.ReactNode }) => children,
  HoverCardContent: ({ children }: { children: React.ReactNode }) => children,
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
  it("gives the shimmer enough line height to paint descenders", () => {
    render(<IssueAgentActivityIndicator issueId="issue-1" />);

    const label = screen.getByText("Working");
    expect(label.classList.contains("leading-4")).toBe(true);
    expect(label.classList.contains("leading-none")).toBe(false);
  });
});
