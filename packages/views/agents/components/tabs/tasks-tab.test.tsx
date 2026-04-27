// @vitest-environment jsdom

import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentTask } from "@multica/core/types";

const mockListAgentTasks = vi.hoisted(() => vi.fn());
const mockCancelTask = vi.hoisted(() => vi.fn());
const mockBatchUpdateIssues = vi.hoisted(() => vi.fn());
const toastState = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

vi.mock("@multica/core/api", () => ({
  api: {
    listAgentTasks: (...args: unknown[]) => mockListAgentTasks(...args),
    cancelTask: (...args: unknown[]) => mockCancelTask(...args),
  },
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useBatchUpdateIssues: () => ({
    mutateAsync: (...args: unknown[]) => mockBatchUpdateIssues(...args),
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["workspaces", "ws-1", "agents"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("../../../navigation", () => ({
  AppLink: ({ children, href, ...props }: any) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));

vi.mock("sonner", () => ({
  toast: toastState,
}));

import { TasksTab } from "./tasks-tab";

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_env: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: null,
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderTasksTab(tasks: AgentTask[]) {
  mockListAgentTasks.mockResolvedValue(tasks);
  mockCancelTask.mockResolvedValue({
    id: "task-1",
    issue_id: "issue-1",
    status: "cancelled",
  });
  mockBatchUpdateIssues.mockResolvedValue(undefined);

  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <TasksTab agent={agent} />
    </QueryClientProvider>,
  );
}

describe("TasksTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    cleanup();
  });

  it("uses workspace-scoped issue detail paths when issue data is loaded", async () => {
    renderTasksTab(
      [
        {
          id: "task-1",
          agent_id: "agent-1",
          runtime_id: "runtime-1",
          issue_id: "issue-1",
          status: "queued",
          priority: 1,
          dispatched_at: null,
          started_at: null,
          completed_at: null,
          result: null,
          error: null,
          created_at: "2026-04-16T00:00:00Z",
          trigger_source: "message",
          trigger_excerpt: "Please fix agent task routing",
          issue_identifier: "MUL-1",
          issue_title: "Fix agent task routing",
        },
      ],
    );

    const title = await screen.findByText("Please fix agent task routing");
    const link = title.closest("a");

    expect(link?.getAttribute("href")).toBe("/test/issues/issue-1");
  });

  it("does not link task rows when the issue cannot be resolved in the workspace", async () => {
    renderTasksTab(
      [
        {
          id: "task-2",
          agent_id: "agent-1",
          runtime_id: "runtime-1",
          issue_id: "12345678-fallback",
          status: "completed",
          priority: 1,
          dispatched_at: null,
          started_at: null,
          completed_at: "2026-04-16T01:00:00Z",
          result: null,
          error: null,
          created_at: "2026-04-16T00:00:00Z",
        },
      ],
    );

    await waitFor(() => {
      expect(mockListAgentTasks).toHaveBeenCalledWith("agent-1");
    });

    const title = await screen.findByText("Linked issue unavailable");
    const link = title.closest("a");

    expect(link).toBeNull();
  });

  it("shows queue metadata and supports single-task cancel", async () => {
    renderTasksTab([
      {
        id: "task-3",
        agent_id: "agent-1",
        runtime_id: "runtime-1",
        issue_id: "issue-queue",
        status: "queued",
        priority: 2,
        dispatched_at: null,
        started_at: null,
        completed_at: null,
        result: null,
        error: null,
        created_at: "2026-04-16T00:00:00Z",
        trigger_source: "issue",
        issue_identifier: "MUL-7",
        issue_title: "Queued execution",
        queue_position: 2,
        queue_ahead_count: 1,
      },
    ]);

    await screen.findByText("Issue-triggered execution");
    expect(screen.getByText("MUL-7 Queued execution")).toBeInTheDocument();
    expect(screen.getByText("Queue #2 • 1 ahead")).toBeInTheDocument();

    const cancelButton = screen.getByRole("button", { name: "Cancel task" });
    expect(cancelButton).toHaveTextContent("Cancel");
    expect(cancelButton).toHaveClass("border-destructive/30");

    const bulkSelect = screen.getByRole("checkbox", {
      name: "Select task for bulk actions",
    });
    expect(bulkSelect.closest("a")).toBeNull();

    fireEvent.click(cancelButton);

    await waitFor(() => {
      expect(mockCancelTask).toHaveBeenCalledWith("issue-queue", "task-3");
    });
    expect(toastState.success).toHaveBeenCalledWith("Task cancelled");
  });

  it("keeps failed tasks selected after partial bulk cancel failure", async () => {
    renderTasksTab([
      {
        id: "task-ok",
        agent_id: "agent-1",
        runtime_id: "runtime-1",
        issue_id: "issue-ok",
        status: "running",
        priority: 2,
        dispatched_at: "2026-04-16T00:00:00Z",
        started_at: "2026-04-16T00:01:00Z",
        completed_at: null,
        result: null,
        error: null,
        created_at: "2026-04-16T00:00:00Z",
        issue_identifier: "MUL-8",
        issue_title: "Successful cancel",
      },
      {
        id: "task-fail",
        agent_id: "agent-1",
        runtime_id: "runtime-1",
        issue_id: "issue-fail",
        status: "queued",
        priority: 2,
        dispatched_at: null,
        started_at: null,
        completed_at: null,
        result: null,
        error: null,
        created_at: "2026-04-16T00:00:00Z",
        issue_identifier: "MUL-9",
        issue_title: "Failed cancel",
      },
    ]);
    mockCancelTask.mockImplementation((_issueId: string, taskId: string) =>
      taskId === "task-fail"
        ? Promise.reject(new Error("cancel failed"))
        : Promise.resolve({ id: taskId, status: "cancelled" }),
    );

    const checkboxes = await screen.findAllByRole("checkbox", {
      name: "Select task for bulk actions",
    });
    fireEvent.click(checkboxes[0]!);
    fireEvent.click(checkboxes[1]!);

    expect(await screen.findByText("2 selected")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /^Cancel$/ }));

    await waitFor(() => {
      expect(mockCancelTask).toHaveBeenCalledWith("issue-ok", "task-ok");
      expect(mockCancelTask).toHaveBeenCalledWith("issue-fail", "task-fail");
    });
    await screen.findByText("1 selected");
    expect(toastState.success).toHaveBeenCalledWith("Cancelled 1 task");
    expect(toastState.error).toHaveBeenCalledWith("1 task could not be cancelled");
  });

  it("renders tasks with empty issue_id as inert rows and does not fetch issue detail", async () => {
    // Tasks persisted with NULL issue_id — autopilot run_only runs and
    // chat-spawned tasks — arrive here with issue_id === "". The tab used
    // to feed that empty id into `/api/issues/`, which crashed the whole
    // page after the list-cache paginate refactor (#1422). It must now:
    //   - skip the detail fetch entirely,
    //   - render a neutral label instead of an "Issue ..." stub, and
    //   - NOT wrap the row in an anchor.
    renderTasksTab(
      [
        {
          id: "task-no-issue",
          agent_id: "agent-1",
          runtime_id: "runtime-1",
          issue_id: "",
          status: "completed",
          priority: 1,
          dispatched_at: null,
          started_at: null,
          completed_at: "2026-04-16T01:00:00Z",
          result: null,
          error: null,
          created_at: "2026-04-16T00:00:00Z",
        },
      ],
    );

    const label = await screen.findByText("Task without linked issue");
    expect(label.closest("a")).toBeNull();
  });

  it("labels chat-spawned tasks as 'Chat session'", async () => {
    renderTasksTab(
      [
        {
          id: "task-chat",
          agent_id: "agent-1",
          runtime_id: "runtime-1",
          issue_id: "",
          chat_session_id: "chat-42",
          status: "running",
          priority: 1,
          dispatched_at: "2026-04-16T00:30:00Z",
          started_at: "2026-04-16T00:31:00Z",
          completed_at: null,
          result: null,
          error: null,
          created_at: "2026-04-16T00:00:00Z",
        },
      ],
    );

    const label = await screen.findByText("Chat session");
    expect(label.closest("a")).toBeNull();
  });

  it("labels autopilot-spawned tasks as 'Autopilot run'", async () => {
    renderTasksTab(
      [
        {
          id: "task-autopilot",
          agent_id: "agent-1",
          runtime_id: "runtime-1",
          issue_id: "",
          autopilot_run_id: "run-7",
          status: "completed",
          priority: 1,
          dispatched_at: null,
          started_at: null,
          completed_at: "2026-04-16T01:00:00Z",
          result: null,
          error: null,
          created_at: "2026-04-16T00:00:00Z",
        },
      ],
    );

    const label = await screen.findByText("Autopilot run");
    expect(label.closest("a")).toBeNull();
  });
});
