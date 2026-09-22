import { cleanup, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import en from "../../locales/en/issues.json";
import type { AgentTask } from "@multica/core/types";

const mockState = vi.hoisted(() => ({
  snapshot: [] as unknown[],
  summaries: [] as unknown[],
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
    t: (
      selector: (strings: typeof en) => string,
      params: Record<string, unknown> = {},
    ) =>
      selector(en).replace(/{{(\w+)}}/g, (_, key: string) =>
        String(params[key] ?? ""),
      ),
  }),
  useLocale: () => "en",
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
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
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
      if (opts.queryKey?.[0] === "issue-wakeup-summaries")
        return {
          data: opts.select
            ? opts.select(mockState.summaries)
            : mockState.summaries,
        };
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
  mockState.summaries = [];
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
});

const eventSummary = {
  id: "wake-1",
  issue_id: "issue-1",
  agent_id: "agent-1",
  agent_name: "Emacs",
  kind: "event",
  mode: "continuous",
  event_types: ["task.completed"],
  timezone: "UTC",
  next_fire_at: null,
  active_count: 2,
  event_count: 2,
};
it("shows waiting and aggregate count even without an active run", () => {
  mockState.snapshot = [];
  mockState.summaries = [eventSummary];
  render(<IssueAgentActivityIndicator issueId="issue-1" hoverCard={false} />);
  expect(screen.getByText("Waiting for trigger")).toBeInTheDocument();
  expect(screen.getByText("+1")).toBeInTheDocument();
  expect(screen.queryByTestId("agent-avatar-stack")).toBeNull();
});
it("prioritizes active runs, marking wakeup origin separately from future count", () => {
  mockState.snapshot = [
    makeTask(),
    makeTask({ id: "task-2", status: "deferred", wakeup_id: "wake-2" }),
  ];
  mockState.summaries = [eventSummary];
  render(<IssueAgentActivityIndicator issueId="issue-1" hoverCard={false} />);
  expect(screen.getByText("Working")).toBeInTheDocument();
  expect(screen.getByLabelText("Triggered by wakeup")).toBeInTheDocument();
  expect(screen.getByText("+2")).toBeInTheDocument();
  expect(screen.queryByText("Waiting for trigger")).toBeNull();
});
it("does not expose another issue's wakeups", () => {
  mockState.snapshot = [];
  mockState.summaries = [{ ...eventSummary, issue_id: "other-issue" }];
  const { container } = render(
    <IssueAgentActivityIndicator issueId="issue-1" />,
  );
  expect(container.firstChild).toBeNull();
});
it("shows deferred wakeup retries as queued", () => {
  mockState.snapshot = [makeTask({ status: "deferred", wakeup_id: "wake-1" })];
  render(<IssueAgentActivityIndicator issueId="issue-1" hoverCard={false} />);
  expect(screen.getByText("Queued")).toBeInTheDocument();
});
