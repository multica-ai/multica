// @vitest-environment jsdom

import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AgentTask } from "@multica/core/types/agent";
import { renderWithI18n } from "../../test/i18n";
import { TranscriptButton } from "./transcript-button";

const mockApi = vi.hoisted(() => ({
  listTaskMessages: vi.fn(),
  getAgent: vi.fn(),
  listRuntimes: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApi,
}));

vi.mock("../actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
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
    expect(stopPropagation).not.toHaveBeenCalled();
  });
});
