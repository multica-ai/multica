import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentTask } from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { AgentStreamSidebar } from "./agent-stream-sidebar";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

const mockApi = vi.hoisted(() => ({
  listTasksByIssue: vi.fn(),
  listTaskInteractions: vi.fn(),
  listTaskTrace: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApi,
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: vi.fn(),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getMemberName: (id: string) => (id === "user-1" ? "Ada Lovelace" : "Unknown"),
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => <span data-testid="actor-avatar">{actorId}</span>,
}));

vi.mock("./task-trace-output", () => ({
  TaskTraceOutput: ({ task }: { task: AgentTask }) => (
    <div data-testid="task-trace-output">{task.id}</div>
  ),
}));

function makeTask(overrides: Partial<AgentTask>): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: "2026-01-01T00:01:00Z",
    result: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
    trigger_summary: "Run summary",
    ...overrides,
  };
}

function renderSidebar(onHighlightComment = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
  const result = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <AgentStreamSidebar issueId="issue-1" onHighlightComment={onHighlightComment} />
      </QueryClientProvider>
    </I18nProvider>,
  );
  return { ...result, onHighlightComment, queryClient };
}

beforeEach(() => {
  mockApi.listTasksByIssue.mockReset();
  mockApi.listTaskInteractions.mockReset();
  mockApi.listTaskTrace.mockReset();
  mockApi.listTaskInteractions.mockResolvedValue([]);
  mockApi.listTaskTrace.mockResolvedValue({ lines: [] });
});

describe("AgentStreamSidebar", () => {
  it("renders runs in created_at descending order (newest first)", async () => {
    mockApi.listTasksByIssue.mockResolvedValue([
      makeTask({
        id: "task-old",
        created_at: "2026-01-01T00:01:00Z",
        completed_at: "2026-01-01T00:02:00Z",
        trigger_summary: "Oldest run",
      }),
      makeTask({
        id: "task-new",
        created_at: "2026-01-01T00:03:00Z",
        completed_at: "2026-01-01T00:04:00Z",
        trigger_summary: "Newest run",
      }),
    ]);

    renderSidebar();

    await waitFor(() => expect(screen.getByText("Newest run")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /runs/i }));

    const rows = screen.getAllByRole("button").filter((button) =>
      button.textContent?.includes("run"),
    );
    expect(rows[0]).toHaveTextContent("Newest run");
    expect(rows[1]).toHaveTextContent("Oldest run");
  });

  it("switches from a recent run to a new active run", async () => {
    const recentRun = makeTask({
      id: "task-recent",
      status: "completed",
      created_at: "2026-01-01T00:01:00Z",
      completed_at: "2026-01-01T00:02:00Z",
      trigger_summary: "Completed run",
    });
    const activeRun = makeTask({
      id: "task-active",
      status: "running",
      created_at: "2026-01-01T00:03:00Z",
      started_at: "2026-01-01T00:03:30Z",
      completed_at: null,
      trigger_summary: "Active run",
    });
    mockApi.listTasksByIssue
      .mockResolvedValueOnce([recentRun])
      .mockResolvedValueOnce([recentRun, activeRun]);

    const { queryClient } = renderSidebar();

    await waitFor(() => expect(screen.getByTestId("task-trace-output")).toHaveTextContent("task-recent"));

    await queryClient.invalidateQueries({ queryKey: ["issues", "tasks", "issue-1"] });

    await waitFor(() => expect(screen.getByTestId("task-trace-output")).toHaveTextContent("task-active"));
  });

  it("switches to another active run when the selected active run completes", async () => {
    const firstActive = makeTask({
      id: "task-active-old",
      status: "running",
      created_at: "2026-01-01T00:01:00Z",
      started_at: "2026-01-01T00:01:30Z",
      completed_at: null,
      trigger_summary: "First active run",
    });
    const completedFirst = {
      ...firstActive,
      status: "completed" as const,
      completed_at: "2026-01-01T00:05:00Z",
    };
    const secondActive = makeTask({
      id: "task-active-new",
      status: "running",
      created_at: "2026-01-01T00:03:00Z",
      started_at: "2026-01-01T00:03:30Z",
      completed_at: null,
      trigger_summary: "Second active run",
    });
    mockApi.listTasksByIssue
      .mockResolvedValueOnce([firstActive])
      .mockResolvedValueOnce([completedFirst, secondActive]);

    const { queryClient } = renderSidebar();

    await waitFor(() => expect(screen.getByTestId("task-trace-output")).toHaveTextContent("task-active-old"));

    await queryClient.invalidateQueries({ queryKey: ["issues", "tasks", "issue-1"] });

    await waitFor(() => expect(screen.getByTestId("task-trace-output")).toHaveTextContent("task-active-new"));
  });

  it("defaults to the newest recent run when there are no active runs", async () => {
    mockApi.listTasksByIssue.mockResolvedValue([
      makeTask({
        id: "task-old",
        status: "completed",
        created_at: "2026-01-01T00:01:00Z",
        completed_at: "2026-01-01T00:02:00Z",
        trigger_summary: "Older completed run",
      }),
      makeTask({
        id: "task-new",
        status: "completed",
        created_at: "2026-01-01T00:03:00Z",
        completed_at: "2026-01-01T00:04:00Z",
        trigger_summary: "Newest completed run",
      }),
    ]);

    renderSidebar();

    await waitFor(() => expect(screen.getByTestId("task-trace-output")).toHaveTextContent("task-new"));
  });

  it("jumps using the task trigger_comment_id", async () => {
    mockApi.listTasksByIssue.mockResolvedValue([
      makeTask({
        id: "task-comment",
        trigger_summary: "Triggered from exact comment",
        trigger_comment_id: "comment-13",
      }),
    ]);
    const onHighlightComment = vi.fn();

    renderSidebar(onHighlightComment);

    const row = await screen.findByText("Triggered from exact comment");
    fireEvent.click(
      within(row.closest("div") as HTMLElement).getByRole("button", {
        name: "Jump to comment",
      }),
    );

    expect(onHighlightComment).toHaveBeenCalledWith("comment-13");
  });
});
