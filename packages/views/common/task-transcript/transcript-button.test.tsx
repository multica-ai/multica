// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { AgentTask } from "@multica/core/types/agent";
import { TranscriptButton } from "./transcript-button";
import type { TimelineItem } from "./build-timeline";

vi.mock("@multica/core/api", () => ({
  api: {
    listTaskMessages: vi.fn(),
    getAgent: vi.fn().mockResolvedValue({
      id: "agent-1",
      description: "Code Review",
    }),
    listRuntimes: vi.fn().mockResolvedValue([]),
  },
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: (helpers: {
      transcript: {
        status_running: string;
        status_completed: string;
        status_failed: string;
        dialog_title: string;
        filter: string;
        clear_filters: string;
        copied: string;
        copy_filtered: string;
        copy_all: string;
        tool_calls: string;
        events_filtered: string;
        events: string;
        waiting_events: string;
        no_data: string;
      };
    }) => string) =>
      selector({
        transcript: {
          status_running: "Running",
          status_completed: "Completed",
          status_failed: "Failed",
          dialog_title: "Transcript",
          filter: "Filter",
          clear_filters: "Clear filters",
          copied: "Copied",
          copy_filtered: "Copy filtered",
          copy_all: "Copy all",
          tool_calls: "Tool calls",
          events_filtered: "Events filtered",
          events: "Events",
          waiting_events: "Waiting events",
          no_data: "No data",
        },
      }),
  }),
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
  issue_id: "issue-1",
  agent_id: "agent-1",
  runtime_id: "",
  status: "completed",
  priority: 0,
  dispatched_at: "2026-05-15T10:00:05.000Z",
  started_at: "2026-05-15T10:00:06.000Z",
  completed_at: "2026-05-15T10:00:10.000Z",
  result: null,
  error: null,
  failure_reason: "",
  attempt: 1,
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
  it("closes the transcript dialog when a global desktop navigation event fires", () => {
    render(
      <TranscriptButton
        task={task}
        agentName="Codex"
        items={items}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    act(() => {
      window.dispatchEvent(
        new CustomEvent("multica:navigate", {
          detail: { path: "/acme/inbox?issue=MUL-123" },
        }),
      );
    });

    return waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });
});
