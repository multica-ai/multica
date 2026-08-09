// @vitest-environment jsdom

import { createContext, cloneElement, useContext, type ReactElement, type ReactNode } from "react";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask, TerminalSessionMetadata } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";

const mockState = vi.hoisted(() => ({
  cancelTask: vi.fn(),
  waitForCancellationAck: vi.fn(),
  rerunIssue: vi.fn(),
  createComment: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  terminalMetadata: {
    available: false,
    protocol_version: 1,
    task_id: "task-1",
  } as TerminalSessionMetadata,
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getTaskTerminal: vi.fn(() => Promise.resolve(mockState.terminalMetadata)),
  },
  dispatchReasonCode: () => undefined,
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useCreateComment: () => ({ mutateAsync: mockState.createComment }),
  useCancelIssueTask: () => ({
    mutateAsync: mockState.cancelTask,
    waitForAcknowledgement: mockState.waitForCancellationAck,
    isPending: false,
  }),
  useRerunIssueTask: () => ({
    mutateAsync: mockState.rerunIssue,
    isPending: false,
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mockState.success,
    warning: mockState.warning,
    error: mockState.error,
  },
}));

vi.mock("../../common/task-transcript", () => ({
  TranscriptButton: ({
    variant,
    headerActions,
    terminalSlot,
  }: {
    variant?: string;
    headerActions?: ReactNode | ((context: { agentName: string }) => ReactNode);
    terminalSlot?: ReactNode;
  }) => (
    <div data-testid="transcript" data-variant={variant}>
      {terminalSlot}
      {typeof headerActions === "function"
        ? headerActions({ agentName: "Quantization Engineer" })
        : headerActions}
    </div>
  ),
}));

vi.mock("./agent-terminal", () => ({
  AgentTerminal: () => <div>PTY terminal active</div>,
}));

const PopoverContext = createContext<{
  open: boolean;
  onOpenChange: (open: boolean) => void;
} | null>(null);

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({
    open,
    onOpenChange,
    children,
  }: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    children: ReactNode;
  }) => (
    <PopoverContext.Provider value={{ open, onOpenChange }}>
      {children}
    </PopoverContext.Provider>
  ),
  PopoverTrigger: ({
    render,
    children,
  }: {
    render: ReactElement<{ onClick?: () => void; children?: ReactNode }>;
    children: ReactNode;
  }) => {
    const context = useContext(PopoverContext)!;
    return cloneElement(render, {
      onClick: () => context.onOpenChange(!context.open),
      children,
    });
  },
  PopoverContent: ({ children }: { children: ReactNode }) => {
    const context = useContext(PopoverContext)!;
    return context.open ? <div>{children}</div> : null;
  },
}));

vi.mock("./terminate-task-confirm-dialog", () => ({
  TerminateTaskConfirmDialog: ({
    open,
    onConfirm,
  }: {
    open: boolean;
    onConfirm: () => void;
  }) => open ? <button onClick={onConfirm}>Confirm stop</button> : null,
}));

import { AgentCockpitLauncher, AgentCockpitSession } from "./agent-cockpit";

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: "2026-08-08T08:00:00Z",
    started_at: "2026-08-08T08:00:01Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-08-08T08:00:00Z",
    ...overrides,
  };
}

function renderSession(
  task: AgentTask = makeTask(),
  allowTerminalContinuation = true,
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <AgentCockpitSession
        task={task}
        issueId="issue-1"
        open
        onOpenChange={vi.fn()}
        allowTerminalContinuation={allowTerminalContinuation}
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockState.cancelTask.mockResolvedValue(makeTask({ status: "cancelled" }));
  mockState.waitForCancellationAck.mockResolvedValue(undefined);
  mockState.rerunIssue.mockResolvedValue(makeTask({ id: "task-2", status: "queued" }));
  mockState.createComment.mockResolvedValue({
    trigger_outcomes: [{ target_type: "agent", target_id: "agent-1", status: "queued" }],
  });
  mockState.terminalMetadata = {
    available: false,
    protocol_version: 1,
    task_id: "task-1",
  };
});

describe("AgentCockpit", () => {
  it("hides Redirect/Continue when PTY is active while keeping Stop Agent", async () => {
    mockState.terminalMetadata = {
      available: true,
      protocol_version: 1,
      task_id: "task-1",
    };
    renderSession();

    expect(await screen.findByText("PTY terminal active")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Redirect" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Stop Agent" })).toBeInTheDocument();
  });
  it("renders a visible Issue-page terminal launcher", () => {
    const onOpen = vi.fn();
    renderWithI18n(<AgentCockpitLauncher runningCount={2} onOpen={onOpen} />);

    fireEvent.click(screen.getByRole("button", { name: /Open Agent Terminal/i }));
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("waits for the daemon stop acknowledgement before posting a targeted continuation", async () => {
    let acknowledgeStop!: () => void;
    mockState.waitForCancellationAck.mockReturnValueOnce(
      new Promise<void>((resolve) => {
        acknowledgeStop = resolve;
      }),
    );
    renderSession();
    expect(screen.getByTestId("transcript")).toHaveAttribute("data-variant", "cockpit");

    fireEvent.click(screen.getByRole("button", { name: "Redirect" }));
    fireEvent.change(screen.getByPlaceholderText(/Keep the current approach/i), {
      target: { value: "Keep PTQ design. Only modify calibration." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Stop and continue" }));

    await waitFor(() => expect(mockState.cancelTask).toHaveBeenCalledWith("task-1"));
    expect(mockState.waitForCancellationAck).toHaveBeenCalledWith("task-1", "running");
    expect(mockState.createComment).not.toHaveBeenCalled();

    acknowledgeStop();
    await waitFor(() => expect(mockState.createComment).toHaveBeenCalledTimes(1));
    expect(mockState.cancelTask).toHaveBeenCalledWith("task-1");
    expect(mockState.createComment).toHaveBeenCalledWith({
      content:
        "[@Quantization Engineer](mention://agent/agent-1) Keep PTQ design. Only modify calibration.",
    });
    expect(mockState.cancelTask.mock.invocationCallOrder[0]).toBeLessThan(
      mockState.createComment.mock.invocationCallOrder[0]!,
    );
    expect(mockState.success).toHaveBeenCalledWith("New instruction queued");
  });

  it("continues a legacy structured run without cancelling it again", async () => {
    renderSession(makeTask({ status: "cancelled", completed_at: "2026-08-08T08:10:00Z" }));

    fireEvent.click(screen.getByRole("button", { name: "Continue" }));
    fireEvent.change(screen.getByPlaceholderText(/Keep the current approach/i), {
      target: { value: "Continue from the last checkpoint." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Continue with instruction" }));

    await waitFor(() => expect(mockState.createComment).toHaveBeenCalledTimes(1));
    expect(mockState.cancelTask).not.toHaveBeenCalled();
  });

  it("offers restart after a cancelled run", async () => {
    renderSession(makeTask({ status: "cancelled", completed_at: "2026-08-08T08:10:00Z" }));

    fireEvent.click(screen.getByRole("button", { name: "Restart" }));
    await waitFor(() =>
      expect(mockState.rerunIssue).toHaveBeenCalledWith("task-1"),
    );
  });

  it("offers Restart but never Continue for a historical PTY run", async () => {
    mockState.terminalMetadata = {
      available: false,
      protocol_version: 1,
      task_id: "task-1",
      session_id: "terminal-session-1",
      mode: "pty",
    };
    renderSession(makeTask({ status: "cancelled", completed_at: "2026-08-08T08:10:00Z" }));

    await waitFor(() =>
      expect(screen.queryByRole("button", { name: "Continue" })).not.toBeInTheDocument(),
    );
    expect(screen.getByRole("button", { name: "Restart" })).toBeInTheDocument();
  });

  it("does not offer Continue from an older run whose session is not selected", () => {
    renderSession(
      makeTask({ status: "cancelled", completed_at: "2026-08-08T08:10:00Z" }),
      false,
    );

    expect(screen.queryByRole("button", { name: "Continue" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Restart" })).toBeInTheDocument();
  });
});
