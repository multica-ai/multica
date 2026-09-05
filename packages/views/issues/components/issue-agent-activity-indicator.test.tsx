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
  AgentAvatarStack: ({ agentIds }: { agentIds: string[] }) => (
    <div data-testid="agent-avatar-stack">{agentIds.length}</div>
  ),
}));

vi.mock("../../agents/components/agent-activity-hover-content", () => ({
  AgentActivityHoverContent: () => <div data-testid="activity-hover" />,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: (v: Record<string, unknown>) => unknown, vars?: Record<string, unknown>) => {
      const base = {
        agent_activity: {
          status_running: "Working",
          status_queued: "Queued",
          status_waiting: "Waiting on sub-issues",
          waiting_detail: vars ? `${(vars as Record<string, unknown>).done}/${(vars as Record<string, unknown>).total} complete` : "0/1 complete",
        },
      };
      try {
        const picked = (selector as (v: typeof base) => unknown)(base);
        return typeof picked === "string" ? picked : String(picked ?? "");
      } catch {
        return "Working";
      }
    },
  }),
}));

// The hover card only portals its content once open, so absence of the body
// cannot distinguish "closed" from "not wired up". Mock the primitive instead
// and assert on the wrapper itself.
vi.mock("@multica/ui/components/ui/hover-card", () => ({
  HoverCard: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="hover-card">{children}</div>
  ),
  HoverCardTrigger: ({
    children,
    delay,
    closeDelay,
  }: {
    children: React.ReactNode;
    delay?: number;
    closeDelay?: number;
  }) => (
    <span
      data-testid="hover-card-trigger"
      data-delay={String(delay)}
      data-close-delay={String(closeDelay)}
    >
      {children}
    </span>
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

import { IssueAgentActivityIndicator } from "./issue-agent-activity-indicator";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
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
  mockState.snapshot = [makeTask()];
});

describe("IssueAgentActivityIndicator", () => {
  it("wraps the badge in a hover card by default", () => {
    render(<IssueAgentActivityIndicator issueId="issue-1" />);

    expect(screen.getByTestId("hover-card")).not.toBeNull();
    expect(screen.getByTestId("agent-avatar-stack")).not.toBeNull();
  });

  it("opens the card on a deliberate dwell, not on pointer travel", () => {
    render(<IssueAgentActivityIndicator issueId="issue-1" />);

    const trigger = screen.getByTestId("hover-card-trigger");
    expect(Number(trigger.getAttribute("data-delay"))).toBeGreaterThan(600);
    expect(Number(trigger.getAttribute("data-close-delay"))).toBeLessThan(300);
  });

  it("renders the badge without a hover card when hoverCard is false", () => {
    render(<IssueAgentActivityIndicator issueId="issue-1" hoverCard={false} />);

    expect(screen.queryByTestId("hover-card")).toBeNull();
    expect(screen.queryByTestId("hover-card-trigger")).toBeNull();
    // The cue itself survives — only the popup behind it is dropped.
    expect(screen.getByTestId("agent-avatar-stack")).not.toBeNull();
    expect(screen.getByText("Working")).not.toBeNull();
  });

  it("renders nothing when no agent is on the issue", () => {
    mockState.snapshot = [];
    const { container } = render(
      <IssueAgentActivityIndicator issueId="issue-1" hoverCard={false} />,
    );

    expect(container.firstChild).toBeNull();
  });

  // MUL-7925: derived waiting state (no new query, no status write)
  it("shows Working when running even with unfinished children", () => {
    mockState.snapshot = [makeTask({ status: "running" })];
    const { container } = render(
      <IssueAgentActivityIndicator
        issueId="issue-1"
        statusCategory="in_progress"
        childProgress={{ done: 0, total: 1 }}
        hoverCard={false}
      />,
    );
    expect(container.textContent).toContain("Working");
    expect(container.textContent).not.toContain("Waiting on sub-issues");
  });

  it("shows Queued when queued even with unfinished children", () => {
    mockState.snapshot = [makeTask({ status: "queued" })];
    const { container } = render(
      <IssueAgentActivityIndicator
        issueId="issue-1"
        statusCategory="in_progress"
        childProgress={{ done: 0, total: 1 }}
        hoverCard={false}
      />,
    );
    expect(container.textContent).toContain("Queued");
    expect(container.textContent).not.toContain("Waiting on sub-issues");
  });

  it("shows Waiting on sub-issues when in_progress with unfinished children and no active task", () => {
    mockState.snapshot = [];
    const { container } = render(
      <IssueAgentActivityIndicator
        issueId="issue-1"
        statusCategory="in_progress"
        childProgress={{ done: 0, total: 1 }}
        hoverCard={false}
      />,
    );
    expect(container.textContent).toContain("Waiting on sub-issues");
  });

  it("shows nothing when all children are done even if in_progress and idle", () => {
    mockState.snapshot = [];
    const { container } = render(
      <IssueAgentActivityIndicator
        issueId="issue-1"
        statusCategory="in_progress"
        childProgress={{ done: 1, total: 1 }}
        hoverCard={false}
      />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("shows nothing when unfinished children but status is not in_progress", () => {
    mockState.snapshot = [];
    const { container } = render(
      <IssueAgentActivityIndicator
        issueId="issue-1"
        statusCategory="todo"
        childProgress={{ done: 0, total: 1 }}
        hoverCard={false}
      />,
    );
    expect(container.firstChild).toBeNull();
  });

  it("waiting wraps in a hover card with detail when hoverCard=true", () => {
    mockState.snapshot = [];
    render(
      <IssueAgentActivityIndicator
        issueId="issue-1"
        statusCategory="in_progress"
        childProgress={{ done: 0, total: 1 }}
      />,
    );
    expect(screen.getByTestId("hover-card")).not.toBeNull();
    expect(screen.getAllByText("Waiting on sub-issues").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("0/1 complete")).not.toBeNull();
  });
});
