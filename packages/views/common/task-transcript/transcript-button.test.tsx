// @vitest-environment jsdom

import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { AgentTask } from "@multica/core/types/agent";
import { afterEach, describe, expect, it, vi } from "vitest";
import { TranscriptButton } from "./transcript-button";
import type { TimelineItem } from "./build-timeline";

vi.mock("@multica/core/api", () => ({
  api: {
    listTaskMessages: vi.fn().mockResolvedValue([]),
  },
}));

// Per-user preference gate. Default: visual view off (raw dialog). Individual
// tests flip `visualPref` before rendering.
let visualPref = false;
vi.mock("@multica/core/auth", () => {
  const state = () => ({ user: { visual_execution_history: visualPref } });
  const useAuthStore = Object.assign(
    (sel?: (s: ReturnType<typeof state>) => unknown) =>
      sel ? sel(state()) : state(),
    { getState: state },
  );
  return { useAuthStore };
});

vi.mock("./agent-transcript-dialog", () => ({
  AgentTranscriptDialog: ({
    open,
    onOpenChange,
  }: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
  }) =>
    open ? (
      <div role="dialog" data-testid="raw-dialog">
        <button type="button" onClick={() => onOpenChange(false)}>
          Close
        </button>
      </div>
    ) : null,
}));

vi.mock("../../chat/components/visual-run-history", () => ({
  VisualRunHistory: ({
    open,
    onViewRawLog,
  }: {
    open: boolean;
    onViewRawLog?: () => void;
  }) =>
    open ? (
      <div role="dialog" data-testid="visual-dialog">
        <button type="button" onClick={() => onViewRawLog?.()}>
          View raw log
        </button>
      </div>
    ) : null,
}));

afterEach(() => {
  visualPref = false;
});

const task: AgentTask = {
  id: "task-1",
  agent_id: "agent-1",
  runtime_id: "",
  issue_id: "issue-1",
  status: "completed",
  priority: 0,
  dispatched_at: "2026-05-15T10:00:05.000Z",
  started_at: "2026-05-15T10:00:06.000Z",
  completed_at: "2026-05-15T10:00:10.000Z",
  result: null,
  error: null,
  created_at: "2026-05-15T10:00:00.000Z",
};

const items: TimelineItem[] = [
  {
    seq: 1,
    type: "text",
    content: "hello world",
  },
];

describe("TranscriptButton", () => {
  it("closes the transcript dialog when desktop navigation starts", async () => {
    render(<TranscriptButton task={task} agentName="Codex" items={items} />);

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));
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

  it("opens the raw event-log dialog when the visual preference is off", () => {
    visualPref = false;
    render(<TranscriptButton task={task} agentName="Codex" items={items} />);

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));

    expect(screen.getByTestId("raw-dialog")).toBeInTheDocument();
    expect(screen.queryByTestId("visual-dialog")).not.toBeInTheDocument();
  });

  it("opens the visual run view when the visual preference is on", () => {
    visualPref = true;
    render(<TranscriptButton task={task} agentName="Codex" items={items} />);

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));

    expect(screen.getByTestId("visual-dialog")).toBeInTheDocument();
    expect(screen.queryByTestId("raw-dialog")).not.toBeInTheDocument();
  });

  it("swaps from the visual view to the raw log via the escape hatch", async () => {
    visualPref = true;
    render(<TranscriptButton task={task} agentName="Codex" items={items} />);

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));
    expect(screen.getByTestId("visual-dialog")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "View raw log" }));

    await waitFor(() => {
      expect(screen.getByTestId("raw-dialog")).toBeInTheDocument();
    });
    expect(screen.queryByTestId("visual-dialog")).not.toBeInTheDocument();
  });
});
