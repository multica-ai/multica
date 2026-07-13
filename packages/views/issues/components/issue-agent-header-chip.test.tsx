// @vitest-environment jsdom

import { cleanup, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  tasks: [] as unknown[],
  taskMessagesOptions: vi.fn(),
  // Captures the props the chip passes to PopoverTrigger so a test can assert
  // the card is wired to open on hover, not only on click.
  triggerProps: undefined as Record<string, unknown> | undefined,
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_type: string, id: string) =>
      ({
        "agent-1": "Walt",
        "agent-2": "Gus",
      })[id] ?? "Unknown Agent",
    getActorInitials: (_type: string, id: string) =>
      ({
        "agent-1": "WA",
        "agent-2": "GU",
      })[id] ?? "UA",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@multica/core/chat/queries", () => ({
  taskMessagesOptions: mockState.taskMessagesOptions,
}));

vi.mock("@multica/ui/components/ui/popover", async () => {
  const React = await vi.importActual<typeof import("react")>("react");
  return {
    Popover: ({ children }: { children: React.ReactNode }) => (
      <div data-testid="agent-popover">{children}</div>
    ),
    PopoverTrigger: ({
      render,
      children,
      ...props
    }: {
      render: React.ReactElement;
      children: React.ReactNode;
    } & Record<string, unknown>) => {
      mockState.triggerProps = props;
      return React.cloneElement(render, undefined, children);
    },
    PopoverContent: ({ children }: { children: React.ReactNode }) => (
      <div data-testid="agent-popover-content">{children}</div>
    ),
  };
});

vi.mock("./execution-log-section", () => ({
  ActiveTaskRow: ({ task }: { task: AgentTask }) => (
    <div data-testid="active-task-row">{task.id}</div>
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
      // Per-issue task list: issueKeys.tasks(issueId) === ["issues","tasks",id]
      if (opts.queryKey?.[0] === "issues" && opts.queryKey?.[1] === "tasks") {
        return { data: mockState.tasks };
      }
      return { data: undefined };
    },
  };
});

import { IssueAgentHeaderChip } from "./issue-agent-header-chip";

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
  mockState.tasks = [];
  mockState.triggerProps = undefined;
});

describe("IssueAgentHeaderChip", () => {
  it("shows the active agent name without event count or elapsed time", () => {
    mockState.tasks = [makeTask({})];

    renderWithI18n(<IssueAgentHeaderChip issueId="issue-1" />);

    expect(
      screen.getByRole("button", { name: "Walt is working" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Walt is working")).toBeInTheDocument();
    expect(screen.queryByText(/events?/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/\d+[smh]/i)).not.toBeInTheDocument();
    expect(mockState.taskMessagesOptions).not.toHaveBeenCalled();
  });

  it("keeps the header popover card with active task rows", () => {
    mockState.tasks = [makeTask({ id: "task-running" })];

    renderWithI18n(<IssueAgentHeaderChip issueId="issue-1" />);

    expect(screen.getByTestId("agent-popover-content")).toBeInTheDocument();
    expect(screen.getByTestId("active-task-row")).toHaveTextContent(
      "task-running",
    );
    expect(mockState.taskMessagesOptions).not.toHaveBeenCalled();
  });

  it("opens the activity card on hover, not only on click", () => {
    mockState.tasks = [makeTask({})];

    renderWithI18n(<IssueAgentHeaderChip issueId="issue-1" />);

    // Base UI gates hover-to-open on `openOnHover` on the trigger. Without it
    // the chip would be click-only, which is the behavior MUL-3507 replaces.
    // The trigger stays a real <button>, so click/keyboard access is retained.
    expect(mockState.triggerProps?.openOnHover).toBe(true);
    expect(
      screen.getByRole("button", { name: "Walt is working" }),
    ).toBeInTheDocument();
  });

  it("uses the concise multi-agent working label", () => {
    mockState.tasks = [
      makeTask({ id: "task-1", agent_id: "agent-1" }),
      makeTask({ id: "task-2", agent_id: "agent-2" }),
    ];

    renderWithI18n(<IssueAgentHeaderChip issueId="issue-1" />);

    expect(
      screen.getByRole("button", { name: "2 agents working" }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("2 agents working")).toHaveLength(2);
    expect(screen.getAllByTestId("active-task-row")).toHaveLength(2);
    expect(mockState.taskMessagesOptions).not.toHaveBeenCalled();
  });

  it("uses the requested Chinese single-agent copy", () => {
    mockState.tasks = [makeTask({})];

    renderWithI18n(<IssueAgentHeaderChip issueId="issue-1" />, {
      locale: "zh-Hans",
    });

    expect(screen.getByText("Walt 在工作")).toBeInTheDocument();
  });

  it("does not render when the issue has only terminal tasks", () => {
    // The list is issue-scoped by the endpoint, so the chip's only job is to
    // ignore terminal statuses (those are the execution log's story).
    mockState.tasks = [
      makeTask({
        id: "task-done",
        status: "completed",
        completed_at: "2026-06-08T08:05:00Z",
      }),
      makeTask({
        id: "task-cancelled",
        status: "cancelled",
        completed_at: "2026-06-08T08:06:00Z",
      }),
    ];

    renderWithI18n(<IssueAgentHeaderChip issueId="issue-1" />);

    expect(screen.queryByRole("button")).not.toBeInTheDocument();
  });
});
