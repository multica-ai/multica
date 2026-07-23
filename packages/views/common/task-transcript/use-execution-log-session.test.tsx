// @vitest-environment jsdom

import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  type RenderResult,
} from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { api } from "@multica/core/api";
import {
  chatKeys,
  mergeTaskMessagesBySeq,
} from "@multica/core/chat/queries";
import type { AgentTask } from "@multica/core/types/agent";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ExecutionLogTrigger } from "./execution-log-trigger";
import {
  useExecutionLogSession,
  type ExecutionLogActor,
} from "./use-execution-log-session";

vi.mock("@multica/core/api", () => ({
  api: {
    listTaskMessages: vi.fn(),
  },
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Codex",
  }),
}));

vi.mock("./execution-log-dialog", () => ({
  ExecutionLogDialog: ({
    open,
    onOpenChange,
    task,
    actorType,
    actorId,
    isLive,
    liveMessages,
  }: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    task: AgentTask;
    actorType?: string;
    actorId?: string;
    isLive?: boolean;
    liveMessages?: TaskMessagePayload[];
  }) =>
    open ? (
      <div
        role="dialog"
        data-testid="execution-log-dialog"
        data-task-id={task.id}
        data-status={task.status}
        data-live={String(!!isLive)}
        data-actor-type={actorType}
        data-actor-id={actorId}
      >
        <button type="button" onClick={() => onOpenChange(false)}>
          Close
        </button>
        {(liveMessages ?? []).map((message) => (
          <div key={message.seq} data-testid="event" data-seq={message.seq} />
        ))}
      </div>
    ) : null,
}));

const LIVE_TASK_ID = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";

const baseTask: AgentTask = {
  id: LIVE_TASK_ID,
  agent_id: "agent-1",
  runtime_id: "",
  issue_id: "issue-1",
  status: "running",
  priority: 0,
  dispatched_at: "2026-05-15T10:00:05.000Z",
  started_at: "2026-05-15T10:00:06.000Z",
  completed_at: null,
  result: null,
  error: null,
  created_at: "2026-05-15T10:00:00.000Z",
};

const msg = (seq: number, tool: string): TaskMessagePayload => ({
  task_id: LIVE_TASK_ID,
  issue_id: "issue-1",
  seq,
  type: "tool_use",
  tool,
  input: { i: String(seq) },
});

function newClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function renderWith(qc: QueryClient, ui: React.ReactNode): RenderResult {
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

function SessionSurface({
  tasks,
  activeTriggersOnly = false,
  actor,
}: {
  tasks: AgentTask[];
  activeTriggersOnly?: boolean;
  actor?: ExecutionLogActor;
}) {
  const { openExecutionLog, executionLogDialog } =
    useExecutionLogSession(tasks);
  const triggerTasks = activeTriggersOnly
    ? tasks.filter(
        (task) =>
          task.status !== "completed" &&
          task.status !== "failed" &&
          task.status !== "cancelled",
      )
    : tasks;

  return (
    <>
      <div data-testid="task-bucket">
        {triggerTasks.map((task) => (
          <ExecutionLogTrigger
            key={task.id}
            task={task}
            onOpen={openExecutionLog}
            actor={actor}
          />
        ))}
      </div>
      {executionLogDialog}
    </>
  );
}

const listTaskMessages = vi.mocked(api.listTaskMessages);

beforeEach(() => {
  listTaskMessages.mockReset();
  listTaskMessages.mockResolvedValue([]);
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("useExecutionLogSession", () => {
  it("keeps the dialog mounted when an active row moves to the hidden past bucket", async () => {
    const qc = newClient();
    qc.setQueryData(chatKeys.taskMessages(LIVE_TASK_ID), [msg(1, "Bash")]);
    listTaskMessages.mockResolvedValue([msg(1, "Bash")]);

    const { rerender } = renderWith(
      qc,
      <SessionSurface tasks={[baseTask]} activeTriggersOnly />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "View execution log" }),
    );
    await waitFor(() => expect(listTaskMessages).toHaveBeenCalledTimes(1));
    const dialogBefore = screen.getByTestId("execution-log-dialog");
    expect(dialogBefore).toHaveAttribute("data-live", "true");

    // The active query can lose the task before the historical query receives
    // it. The session uses its last snapshot during that hand-off.
    rerender(
      <QueryClientProvider client={qc}>
        <SessionSurface tasks={[]} activeTriggersOnly />
      </QueryClientProvider>,
    );
    expect(screen.queryByTestId("execution-log-trigger")).not.toBeInTheDocument();
    expect(screen.getByTestId("execution-log-dialog")).toBe(dialogBefore);

    const completedTask: AgentTask = {
      ...baseTask,
      status: "completed",
      completed_at: "2026-05-15T10:00:10.000Z",
    };
    rerender(
      <QueryClientProvider client={qc}>
        <SessionSurface tasks={[completedTask]} activeTriggersOnly />
      </QueryClientProvider>,
    );

    const dialogAfter = screen.getByTestId("execution-log-dialog");
    expect(screen.queryByTestId("execution-log-trigger")).not.toBeInTheDocument();
    expect(dialogAfter).toBe(dialogBefore);
    expect(dialogAfter).toHaveAttribute("data-status", "completed");
    expect(dialogAfter).toHaveAttribute("data-live", "false");
    expect(listTaskMessages).toHaveBeenCalledTimes(1);
  });

  it("continues reading final websocket messages after the task completes", async () => {
    const qc = newClient();
    qc.setQueryData(chatKeys.taskMessages(LIVE_TASK_ID), [msg(1, "Bash")]);
    listTaskMessages.mockResolvedValue([msg(1, "Bash")]);

    const { rerender } = renderWith(
      qc,
      <SessionSurface tasks={[baseTask]} />,
    );
    fireEvent.click(
      screen.getByRole("button", { name: "View execution log" }),
    );
    await waitFor(() => expect(screen.getAllByTestId("event")).toHaveLength(1));

    rerender(
      <QueryClientProvider client={qc}>
        <SessionSurface
          tasks={[
            {
              ...baseTask,
              status: "completed",
              completed_at: "2026-05-15T10:00:10.000Z",
            },
          ]}
        />
      </QueryClientProvider>,
    );

    act(() => {
      qc.setQueryData<TaskMessagePayload[]>(
        chatKeys.taskMessages(LIVE_TASK_ID),
        (old = []) => mergeTaskMessagesBySeq(old, [msg(2, "Read")]),
      );
    });

    await waitFor(() => expect(screen.getAllByTestId("event")).toHaveLength(2));
    expect(listTaskMessages).toHaveBeenCalledTimes(1);
  });

  it("preserves an explicit actor identity without fetching the legacy full message array", async () => {
    const qc = newClient();
    renderWith(
      qc,
      <SessionSurface
        actor={{ type: "squad", id: "squad-1" }}
        tasks={[
          {
            ...baseTask,
            status: "completed",
            completed_at: "2026-05-15T10:00:10.000Z",
          },
        ]}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "View execution log" }),
    );

    expect(await screen.findByRole("dialog")).toHaveAttribute(
      "data-live",
      "false",
    );
    expect(screen.getByRole("dialog")).toHaveAttribute(
      "data-actor-type",
      "squad",
    );
    expect(screen.getByRole("dialog")).toHaveAttribute(
      "data-actor-id",
      "squad-1",
    );
    expect(listTaskMessages).not.toHaveBeenCalled();
  });

  it("closes the session when desktop navigation starts", async () => {
    const qc = newClient();
    renderWith(
      qc,
      <SessionSurface
        tasks={[
          {
            ...baseTask,
            status: "completed",
            completed_at: "2026-05-15T10:00:10.000Z",
          },
        ]}
      />,
    );

    fireEvent.click(
      screen.getByRole("button", { name: "View execution log" }),
    );
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    act(() => {
      window.dispatchEvent(
        new CustomEvent("multica:navigate", {
          detail: { path: "/acme/inbox?issue=MUL-123" },
        }),
      );
    });

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });
});
