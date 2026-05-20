// @vitest-environment jsdom

import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { AgentTask } from "@multica/core/types/agent";
import { describe, expect, it, vi } from "vitest";
import { TranscriptButton } from "./transcript-button";
import type { TimelineItem } from "./build-timeline";

vi.mock("@multica/core/api", () => ({
  api: {
    listTaskMessages: vi.fn(),
  },
}));

vi.mock("./agent-transcript-dialog", () => ({
  AgentTranscriptDialog: ({
    open,
    onOpenChange,
  }: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
  }) =>
    open ? (
      <div role="dialog">
        <button type="button" onClick={() => onOpenChange(false)}>
          Close
        </button>
      </div>
    ) : null,
}));

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
});
