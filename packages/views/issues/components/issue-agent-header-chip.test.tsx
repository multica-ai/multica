// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@multica/core/api";
import { chatKeys } from "@multica/core/chat/queries";
import type { AgentTask } from "@multica/core/types";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  tasks: [] as unknown[],
  taskMessagesOptions: vi.fn(),
  // Captures the props the chip passes to PopoverTrigger so a test can assert
  // the card is wired to open on hover, not only on click.
  triggerProps: undefined as Record<string, unknown> | undefined,
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listTaskMessages: vi.fn(),
  },
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

vi.mock("@multica/core/chat/queries", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/chat/queries")>(
    "@multica/core/chat/queries",
  );
  return {
    ...actual,
    taskMessagesOptions: (...args: Parameters<typeof actual.taskMessagesOptions>) => {
      mockState.taskMessagesOptions(...args);
      return actual.taskMessagesOptions(...args);
    },
  };
});

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
  ActiveTaskRow: ({
    task,
    onTranscriptOpenChange,
  }: {
    task: AgentTask;
    transcriptOpen?: boolean;
    onTranscriptOpenChange?: (open: boolean) => void;
  }) => (
    <div data-testid="active-task-row">
      <span>{task.id}</span>
      <button
        type="button"
        aria-label={`open transcript ${task.id}`}
        onClick={() => onTranscriptOpenChange?.(true)}
      >
        Open transcript
      </button>
    </div>
  ),
}));

vi.mock("../../common/task-transcript/agent-transcript-dialog", () => ({
  AgentTranscriptDialog: ({
    open,
    items,
  }: {
    open: boolean;
    items: Array<{ seq: number }>;
  }) =>
    open ? (
      <div role="dialog">
        {items.map((item) => (
          <div key={item.seq} data-testid="event" data-seq={item.seq} />
        ))}
      </div>
    ) : null,
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
      return actual.useQuery(opts as Parameters<typeof actual.useQuery>[0]);
    },
  };
});

import { IssueAgentHeaderChip } from "./issue-agent-header-chip";

const listTaskMessages = vi.mocked(api.listTaskMessages);

const LIVE_TASK_ID = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";

const msg = (seq: number): TaskMessagePayload => ({
  task_id: LIVE_TASK_ID,
  issue_id: "issue-1",
  seq,
  type: "tool_use",
  tool: `Tool ${seq}`,
  input: { i: String(seq) },
});

function newClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function renderChip(qc: QueryClient, issueId = "issue-1") {
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <IssueAgentHeaderChip issueId={issueId} />
    </QueryClientProvider>,
  );
}

function makeTask(overrides: Partial<AgentTask>): AgentTask {
  return {
    id: LIVE_TASK_ID,
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
  listTaskMessages.mockReset();
  listTaskMessages.mockResolvedValue([]);
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

  it("keeps the live transcript open after the clicked task row disappears from the active list", async () => {
    const qc = newClient();
    mockState.tasks = [makeTask({ id: LIVE_TASK_ID })];
    qc.setQueryData(chatKeys.taskMessages(LIVE_TASK_ID), [msg(1)]);
    listTaskMessages.mockResolvedValue([msg(1)]);

    const { rerender } = renderChip(qc);

    fireEvent.click(screen.getByRole("button", { name: `open transcript ${LIVE_TASK_ID}` }));

    await waitFor(() => {
      expect(screen.getByRole("dialog")).toBeInTheDocument();
    });
    await waitFor(() => {
      expect(screen.getAllByTestId("event")).toHaveLength(1);
    });
    expect(screen.getByTestId("active-task-row")).toHaveTextContent(LIVE_TASK_ID);
    expect(listTaskMessages).toHaveBeenCalledWith(LIVE_TASK_ID);

    mockState.tasks = [];
    rerender(
      <QueryClientProvider client={qc}>
        <IssueAgentHeaderChip issueId="issue-2" />
      </QueryClientProvider>,
    );

    expect(screen.queryByTestId("active-task-row")).not.toBeInTheDocument();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
    expect(screen.getAllByTestId("event")).toHaveLength(1);
  });

  it("refreshes the transcript when the opened task completes", async () => {
    const qc = newClient();
    mockState.tasks = [makeTask({ id: LIVE_TASK_ID, status: "running" })];
    qc.setQueryData(chatKeys.taskMessages(LIVE_TASK_ID), [msg(1)]);
    listTaskMessages.mockResolvedValue([msg(1)]);

    const { rerender } = renderChip(qc);

    fireEvent.click(screen.getByRole("button", { name: `open transcript ${LIVE_TASK_ID}` }));

    await waitFor(() => {
      expect(listTaskMessages).toHaveBeenCalledTimes(1);
    });

    mockState.tasks = [
      makeTask({
        id: LIVE_TASK_ID,
        status: "completed",
        completed_at: "2026-06-08T08:05:00Z",
      }),
    ];
    rerender(
      <QueryClientProvider client={qc}>
        <IssueAgentHeaderChip issueId="issue-2" />
      </QueryClientProvider>,
    );

    await waitFor(() => {
      expect(listTaskMessages).toHaveBeenCalledTimes(2);
    });
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

  it("hides the visible label on mobile while preserving a larger tap target and accessible name", () => {
    mockState.tasks = [makeTask({ status: "queued" })];

    renderWithI18n(<IssueAgentHeaderChip issueId="issue-1" />);

    const trigger = screen.getByRole("button", { name: "Walt is queued" });
    expect(trigger.querySelector("[data-slot='avatar']")).not.toBeNull();
    expect(trigger.className).toContain("h-9 min-w-9");
    expect(trigger.className).toContain("md:h-7 md:min-w-0");
    const label = screen.getByText("Walt is queued");
    expect(label.className).toContain("hidden");
    expect(label.className).toContain("md:inline");
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
