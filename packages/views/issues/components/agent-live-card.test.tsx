import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import type { AgentTask } from "@multica/core/types/agent";

// ---------------------------------------------------------------------------
// Mocks — must be before imports that use them
// ---------------------------------------------------------------------------

const mockRetryTask = vi.fn();
const mockListTasksByIssue = vi.fn();
const mockGetActiveTasksForIssue = vi.fn();
const mockListTaskMessages = vi.fn();
const mockCancelTask = vi.fn();

vi.mock("@multica/core/api", () => ({
  api: {
    retryTask: (...args: any[]) => mockRetryTask(...args),
    listTasksByIssue: (...args: any[]) => mockListTasksByIssue(...args),
    getActiveTasksForIssue: (...args: any[]) => mockGetActiveTasksForIssue(...args),
    listTaskMessages: (...args: any[]) => mockListTaskMessages(...args),
    cancelTask: (...args: any[]) => mockCancelTask(...args),
  },
}));

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: vi.fn(),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Test Agent",
    getMemberName: () => "Test User",
    getAgentName: () => "Test Agent",
    getActorInitials: () => "TA",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div data-testid="actor-avatar" />,
}));

vi.mock("./agent-transcript-dialog", () => ({
  AgentTranscriptDialog: () => null,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

// Import after mocks
import { TaskRunHistory } from "./agent-live-card";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "failed",
    priority: 0,
    dispatched_at: "2026-04-16T02:28:00Z",
    started_at: "2026-04-16T02:28:01Z",
    completed_at: "2026-04-16T02:28:13Z",
    result: null,
    error: "Sandbox timeout after 150s",
    retried_from_id: null,
    created_at: "2026-04-16T02:28:00Z",
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("TaskRunEntry retry button", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetActiveTasksForIssue.mockResolvedValue({ tasks: [] });
    mockListTaskMessages.mockResolvedValue([]);
  });

  it("shows retry button for failed tasks", async () => {
    const failedTask = makeTask({ status: "failed" });
    mockListTasksByIssue.mockResolvedValue([failedTask]);

    render(<TaskRunHistory issueId="issue-1" />);
    await waitFor(() => expect(screen.getByText(/Execution history/)).toBeInTheDocument());

    // Expand the history
    fireEvent.click(screen.getByText(/Execution history/));

    await waitFor(() => expect(screen.getByText("Retry")).toBeInTheDocument());
  });

  it("shows retry button for cancelled tasks", async () => {
    const cancelledTask = makeTask({ status: "cancelled" });
    mockListTasksByIssue.mockResolvedValue([cancelledTask]);

    render(<TaskRunHistory issueId="issue-1" />);
    await waitFor(() => expect(screen.getByText(/Execution history/)).toBeInTheDocument());
    fireEvent.click(screen.getByText(/Execution history/));

    await waitFor(() => expect(screen.getByText("Retry")).toBeInTheDocument());
  });

  it("does not show retry button for completed tasks", async () => {
    const completedTask = makeTask({ status: "completed", error: null });
    mockListTasksByIssue.mockResolvedValue([completedTask]);

    render(<TaskRunHistory issueId="issue-1" />);
    await waitFor(() => expect(screen.getByText(/Execution history/)).toBeInTheDocument());
    fireEvent.click(screen.getByText(/Execution history/));

    await waitFor(() => expect(screen.getByText("completed")).toBeInTheDocument());
    expect(screen.queryByText("Retry")).not.toBeInTheDocument();
  });

  it("hides retry button when active task exists on issue", async () => {
    const failedTask = makeTask({ id: "task-1", status: "failed" });
    const runningTask = makeTask({ id: "task-2", status: "running", error: null });
    mockListTasksByIssue.mockResolvedValue([runningTask, failedTask]);

    render(<TaskRunHistory issueId="issue-1" />);
    await waitFor(() => expect(screen.getByText(/Execution history/)).toBeInTheDocument());
    fireEvent.click(screen.getByText(/Execution history/));

    // Failed task is visible but Retry button is hidden (active task running)
    await waitFor(() => expect(screen.getByText("failed")).toBeInTheDocument());
    expect(screen.queryByText("Retry")).not.toBeInTheDocument();
  });

  it("hides retry button when task was already retried", async () => {
    const originalTask = makeTask({ id: "task-1", status: "failed" });
    const retryTask = makeTask({ id: "task-2", status: "completed", error: null, retried_from_id: "task-1" });
    mockListTasksByIssue.mockResolvedValue([retryTask, originalTask]);

    render(<TaskRunHistory issueId="issue-1" />);
    await waitFor(() => expect(screen.getByText(/Execution history/)).toBeInTheDocument());
    fireEvent.click(screen.getByText(/Execution history/));

    // The original task should not have a Retry button (already retried)
    await waitFor(() => expect(screen.getByText("failed")).toBeInTheDocument());
    expect(screen.queryByText("Retry")).not.toBeInTheDocument();
  });

  it("calls api.retryTask on click, hides button, and refreshes list", async () => {
    const failedTask = makeTask({ status: "failed" });
    mockListTasksByIssue.mockResolvedValue([failedTask]);
    mockRetryTask.mockResolvedValue(makeTask({ id: "task-2", status: "queued" }));

    render(<TaskRunHistory issueId="issue-1" />);
    await waitFor(() => expect(screen.getByText(/Execution history/)).toBeInTheDocument());
    fireEvent.click(screen.getByText(/Execution history/));

    await waitFor(() => expect(screen.getByText("Retry")).toBeInTheDocument());
    fireEvent.click(screen.getByText("Retry"));

    // Shows "Retrying..." during the request
    await waitFor(() => expect(screen.getByText("Retrying...")).toBeInTheDocument());

    // After success, button disappears and task list refreshes
    await waitFor(() => expect(screen.queryByText("Retry")).not.toBeInTheDocument());
    await waitFor(() => expect(screen.queryByText("Retrying...")).not.toBeInTheDocument());
    expect(mockListTasksByIssue).toHaveBeenCalledWith("issue-1");
  });

  it("reverts to Retry button on failure", async () => {
    const failedTask = makeTask({ status: "failed" });
    mockListTasksByIssue.mockResolvedValue([failedTask]);
    mockRetryTask.mockRejectedValue(new Error("an active task already exists"));

    render(<TaskRunHistory issueId="issue-1" />);
    await waitFor(() => expect(screen.getByText(/Execution history/)).toBeInTheDocument());
    fireEvent.click(screen.getByText(/Execution history/));

    await waitFor(() => expect(screen.getByText("Retry")).toBeInTheDocument());
    fireEvent.click(screen.getByText("Retry"));

    // After failure, button reverts back to "Retry"
    await waitFor(() => expect(screen.getByText("Retry")).toBeInTheDocument());
  });

  it("shows user-friendly toast for closed issue", async () => {
    const { toast } = await import("sonner");
    const failedTask = makeTask({ status: "failed" });
    mockListTasksByIssue.mockResolvedValue([failedTask]);
    mockRetryTask.mockRejectedValue(new Error("cannot retry: issue is closed"));

    render(<TaskRunHistory issueId="issue-1" />);
    await waitFor(() => expect(screen.getByText(/Execution history/)).toBeInTheDocument());
    fireEvent.click(screen.getByText(/Execution history/));

    await waitFor(() => expect(screen.getByText("Retry")).toBeInTheDocument());
    fireEvent.click(screen.getByText("Retry"));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(
      "This issue is closed — reopen it to retry"
    ));
  });

  it("shows user-friendly toast for active task conflict", async () => {
    const { toast } = await import("sonner");
    const failedTask = makeTask({ status: "failed" });
    mockListTasksByIssue.mockResolvedValue([failedTask]);
    mockRetryTask.mockRejectedValue(new Error("an active task already exists for this issue"));

    render(<TaskRunHistory issueId="issue-1" />);
    await waitFor(() => expect(screen.getByText(/Execution history/)).toBeInTheDocument());
    fireEvent.click(screen.getByText(/Execution history/));

    await waitFor(() => expect(screen.getByText("Retry")).toBeInTheDocument());
    fireEvent.click(screen.getByText("Retry"));

    await waitFor(() => expect(toast.error).toHaveBeenCalledWith(
      "A task is already running — wait for it to finish"
    ));
  });
});
