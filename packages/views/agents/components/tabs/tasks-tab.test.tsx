import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { WorkspaceIdProvider } from "@multica/core/hooks";
import { TasksTab } from "./tasks-tab";

const mockListIssues = vi.hoisted(() => vi.fn());
const mockListAgentTasks = vi.hoisted(() => vi.fn());
const mockBatchMutateAsync = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    listIssues: (...args: any[]) => mockListIssues(...args),
    listAgentTasks: (...args: any[]) => mockListAgentTasks(...args),
  },
  getApi: () => ({
    listIssues: (...args: any[]) => mockListIssues(...args),
    listAgentTasks: (...args: any[]) => mockListAgentTasks(...args),
  }),
  setApiInstance: vi.fn(),
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useBatchUpdateIssues: () => ({
    mutateAsync: mockBatchMutateAsync,
    isPending: false,
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mockToastSuccess,
    error: mockToastError,
  },
}));

vi.mock("@multica/views/navigation", () => ({
  AppLink: ({ href, children, ...props }: any) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));

vi.mock("@multica/views/issues/components", () => ({
  AssigneePicker: ({ onUpdate, trigger, allowedTypes }: any) => (
    <button
      type="button"
      onClick={() =>
        onUpdate(
          allowedTypes?.[0] === "agent"
            ? { assignee_type: "agent", assignee_id: "agent-2" }
            : { assignee_type: "member", assignee_id: "member-2" },
        )
      }
    >
      {trigger}
    </button>
  ),
}));

function renderTasksTab() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });

  return render(
    <QueryClientProvider client={client}>
      <WorkspaceIdProvider wsId="ws-1">
        <TasksTab
          agent={{
            id: "agent-1",
            name: "CEO",
          } as any}
        />
      </WorkspaceIdProvider>
    </QueryClientProvider>,
  );
}

describe("TasksTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();

    mockListIssues.mockImplementation(async (params?: any) => {
      if (params?.open_only) {
        return {
          issues: [
            {
              id: "issue-1",
              identifier: "MC-1",
              title: "Failed issue",
              blocked_by_count: 0,
            },
            {
              id: "issue-2",
              identifier: "MC-2",
              title: "Long runner",
              blocked_by_count: 0,
            },
          ],
          total: 2,
        };
      }

      return { issues: [], total: 0 };
    });

    mockListAgentTasks.mockResolvedValue([
      {
        id: "task-1",
        issue_id: "issue-1",
        status: "failed",
        error: "Runtime disconnected during tool execution",
        started_at: null,
        dispatched_at: null,
        completed_at: "2026-06-09T11:50:00.000Z",
        created_at: "2026-06-09T11:40:00.000Z",
      },
      {
        id: "task-2",
        issue_id: "issue-2",
        status: "running",
        error: null,
        started_at: "2026-06-09T11:00:00.000Z",
        dispatched_at: null,
        completed_at: null,
        created_at: "2026-06-09T10:55:00.000Z",
      },
    ]);

    mockBatchMutateAsync.mockResolvedValue(undefined);
  });

  it("shows review items and routes batch moves to member and agent flows", async () => {
    const nowSpy = vi.spyOn(Date, "now").mockReturnValue(Date.parse("2026-06-09T12:00:00.000Z"));
    const user = userEvent.setup();

    renderTasksTab();

    expect(await screen.findByText("Needs review")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Move to member/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Move to agent/i })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /Move to member/i }));

    await waitFor(() => {
      expect(mockBatchMutateAsync).toHaveBeenCalledWith({
        ids: ["issue-1", "issue-2"],
        updates: { assignee_type: "member", assignee_id: "member-2" },
      });
    });
    expect(mockToastSuccess).toHaveBeenCalledWith("Moved 2 review issues to member");

    await user.click(screen.getByRole("button", { name: /Move to agent/i }));

    await waitFor(() => {
      expect(mockBatchMutateAsync).toHaveBeenCalledWith({
        ids: ["issue-1", "issue-2"],
        updates: { assignee_type: "agent", assignee_id: "agent-2" },
      });
    });
    expect(mockToastSuccess).toHaveBeenCalledWith("Moved 2 review issues to agent");
    nowSpy.mockRestore();
  });
});
