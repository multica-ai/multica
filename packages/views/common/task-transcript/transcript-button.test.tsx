// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types/agent";
import { renderWithI18n } from "../../test/i18n";
import { TranscriptButton } from "./transcript-button";

const mockApi = vi.hoisted(() => ({
  listTaskMessages: vi.fn(),
  getTaskAccess: vi.fn(),
  getAgent: vi.fn(),
  listRuntimes: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApi,
}));

vi.mock("../actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

// CEREBRO-PATCH(transcript-revamp-port): FIR-3782 — the ported dialog virtualizes
// its event list, and real react-virtuoso renders no rows in jsdom's zero-height
// viewport. Flat-render stub, same as the upstream dialog test uses.
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({
    data,
    itemContent,
    computeItemKey,
  }: {
    data: unknown[];
    itemContent: (i: number, item: never) => React.ReactNode;
    computeItemKey: (i: number, item: never) => number;
  }) => (
    <div>
      {data.map((item, i) => (
        <div key={computeItemKey(i, item as never)}>{itemContent(i, item as never)}</div>
      ))}
    </div>
  ),
}));

// CEREBRO-PATCH(transcript-dialog-runprompt-stub): the dialog hard-imports the
// cerebro RunPromptDisclosure (@multica/cerebro-sessions), which pulls in the
// feature-flag + workspace data layer (TanStack Query + workspace route). This
// upstream test only exercises the dialog's message/error flow, so it stubs the
// cerebro child the same way it stubs ActorAvatar — keeping the test isolated
// from cerebro's workspace-scoped runtime. See docs/cerebro-patches.md.
vi.mock("@multica/cerebro-sessions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/cerebro-sessions")>()),
  RunPromptDisclosure: () => null,
}));

// CEREBRO-PATCH(transcript-run-retry-actions): FIR-4073 — same reason as the stub
// above: the dialog's Resume / Start over pair reads the failed-run list through
// TanStack Query, which this button-focused test does not mount.
vi.mock("@multica/cerebro-runtime/views/components/run-retry-actions", () => ({
  TranscriptRunRetryActions: () => null,
}));

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "failed",
    priority: 0,
    dispatched_at: "2026-06-05T19:01:00Z",
    started_at: "2026-06-05T19:02:00Z",
    completed_at: "2026-06-05T19:03:00Z",
    result: null,
    error: "Run failed",
    created_at: "2026-06-05T19:00:00Z",
    ...overrides,
  };
}

describe("TranscriptButton", () => {
  beforeEach(() => {
    mockApi.listTaskMessages.mockResolvedValue([
      {
        task_id: "task-1",
        issue_id: "issue-1",
        seq: 1,
        type: "text",
        content: "Starter run",
      },
      {
        task_id: "task-1",
        issue_id: "issue-1",
        seq: 2,
        type: "error",
        content: "Command failed with exit code 1",
      },
    ]);
    mockApi.getAgent.mockResolvedValue({ id: "agent-1", name: "Charlene" });
    mockApi.listRuntimes.mockResolvedValue([]);
    mockApi.getTaskAccess.mockResolvedValue({
      task_id: "task-1",
      agent_id: "agent-1",
      allowed_tools: ["tools:Read", "firtal_registry"],
      issued_at: "2026-06-05T19:02:00Z",
      expires_at: "2026-06-05T20:02:00Z",
      status: "expired",
    });
  });

  afterEach(() => {
    cleanup();
    vi.clearAllMocks();
  });

  it("opens the transcript dialog with fetched run messages and errors", async () => {
    const stopPropagation = vi.fn();
    const preventDefault = vi.fn();

    renderWithI18n(
      <div onClick={stopPropagation}>
        <TranscriptButton
          task={makeTask()}
          agentName="Charlene"
          title="Vis run-detaljer"
        />
      </div>,
    );

    const button = screen.getByRole("button", { name: "Vis run-detaljer" });
    fireEvent.click(button, { preventDefault, stopPropagation });

    await waitFor(() => {
      expect(mockApi.listTaskMessages).toHaveBeenCalledWith("task-1");
    });

    expect(await screen.findByRole("dialog", { name: "Agent Execution Transcript" })).toBeInTheDocument();
    expect(screen.getByText("Starter run")).toBeInTheDocument();
    expect(screen.getByText("Command failed with exit code 1")).toBeInTheDocument();
    expect(await screen.findByText("Task access · 2 allowed")).toBeInTheDocument();
    expect(stopPropagation).not.toHaveBeenCalled();
    fireEvent.click(screen.getByText("Task access · 2 allowed"));
    expect(await screen.findByText("tools:Read")).toBeInTheDocument();
    expect(screen.getByText(/Every tool call is checked against this exact list/)).toBeInTheDocument();
  });
});
