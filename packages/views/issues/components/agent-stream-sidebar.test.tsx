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
  return { ...result, onHighlightComment };
}

beforeEach(() => {
  vi.clearAllMocks();
  mockApi.listTaskInteractions.mockResolvedValue([]);
  mockApi.listTaskTrace.mockResolvedValue({ lines: [] });
});

describe("AgentStreamSidebar", () => {
  it("renders runs in created_at ascending order even when the API returns newest first", async () => {
    mockApi.listTasksByIssue.mockResolvedValue([
      makeTask({
        id: "task-new",
        created_at: "2026-01-01T00:03:00Z",
        completed_at: "2026-01-01T00:04:00Z",
        trigger_summary: "Newest run",
      }),
      makeTask({
        id: "task-old",
        created_at: "2026-01-01T00:01:00Z",
        completed_at: "2026-01-01T00:02:00Z",
        trigger_summary: "Oldest run",
      }),
    ]);

    renderSidebar();

    await waitFor(() => expect(screen.getByText("Oldest run")).toBeInTheDocument());
    fireEvent.click(screen.getByRole("button", { name: /runs/i }));

    const rows = screen.getAllByRole("button").filter((button) =>
      button.textContent?.includes("run"),
    );
    expect(rows[0]).toHaveTextContent("Oldest run");
    expect(rows[1]).toHaveTextContent("Newest run");
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
