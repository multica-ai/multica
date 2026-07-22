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
  HoverCardTrigger: ({
    children,
    render,
  }: {
    children: React.ReactNode;
    render: React.ReactElement<{ className?: string }>;
  }) => <span className={render.props.className}>{children}</span>,
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

  it("renders a quiet status token without an avatar or shimmer", () => {
    render(
      <IssueAgentActivityIndicator issueId="issue-1" variant="status" />,
    );

    const label = screen.getByText("Working");
    const trigger = label.parentElement;
    expect(trigger?.className).toContain("text-xs");
    expect(trigger?.className).toContain("leading-4");
    expect(trigger?.querySelector('[aria-hidden="true"]')?.className).toContain(
      "bg-brand",
    );
    expect(label.className).not.toContain("animate-chat-text-shimmer");
    expect(screen.queryByTestId("agent-avatar")).toBeNull();
  });
});
