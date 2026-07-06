// @vitest-environment jsdom

import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  type RenderResult,
} from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";
import { api } from "@multica/core/api";
import {
  chatKeys,
  mergeTaskMessagesBySeq,
} from "@multica/core/chat/queries";
import type { AgentTask } from "@multica/core/types/agent";
import type { TaskMessagePayload } from "@multica/core/types/events";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { TranscriptButton } from "./transcript-button";
import type { TimelineItem } from "./build-timeline";

vi.mock("@multica/core/api", () => ({
  api: {
    listTaskMessages: vi.fn(),
  },
}));

// Render the timeline items so tests can assert the dialog grows in place.
// `tool_use` / `tool_result` entries don't coalesce, so each message stays a
// distinct row — unlike adjacent text/thinking, which buildTimeline merges.
vi.mock("./agent-transcript-dialog", () => ({
  AgentTranscriptDialog: ({
    open,
    onOpenChange,
    items,
  }: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
    items: TimelineItem[];
  }) =>
    open ? (
      <div role="dialog" data-testid="transcript-dialog">
        <button type="button" onClick={() => onOpenChange(false)}>
          Close
        </button>
        {items.map((item) => (
          <div key={item.seq} data-testid="event" data-seq={item.seq} />
        ))}
      </div>
    ) : null,
}));

const LIVE_TASK_ID = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";

const baseTask: AgentTask = {
  id: LIVE_TASK_ID,
  agent_id: "agent-1",
  runtime_id: "",
  issue_id: "issue-1",
  status: "running",
  priority: 0,
  dispatched_at: "2026-05-15T10:00:05.000Z",
  started_at: "2026-05-15T10:00:06.000Z",
  completed_at: null,
  result: null,
  error: null,
  created_at: "2026-05-15T10:00:00.000Z",
};

const msg = (seq: number, tool: string): TaskMessagePayload => ({
  task_id: LIVE_TASK_ID,
  issue_id: "issue-1",
  seq,
  type: "tool_use",
  tool,
  input: { i: String(seq) },
});

function newClient() {
  return new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
}

function renderWith(qc: QueryClient, ui: React.ReactNode): RenderResult {
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const listTaskMessages = vi.mocked(api.listTaskMessages);

beforeEach(() => {
  listTaskMessages.mockReset();
  listTaskMessages.mockResolvedValue([]);
});

afterEach(() => {
  vi.clearAllMocks();
});

describe("TranscriptButton", () => {
  it("closes the transcript dialog when desktop navigation starts", async () => {
    const items: TimelineItem[] = [{ seq: 1, type: "text", content: "hello" }];
    const qc = newClient();
    renderWith(
      qc,
      <TranscriptButton
        task={{ ...baseTask, status: "completed" }}
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

    await waitFor(() => {
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    });
  });

  it("live mode: the open dialog grows as the shared cache receives new messages", async () => {
    const qc = newClient();
    qc.setQueryData(chatKeys.taskMessages(LIVE_TASK_ID), [msg(1, "Bash")]);
    listTaskMessages.mockResolvedValue([msg(1, "Bash")]);

    renderWith(qc, <TranscriptButton task={baseTask} agentName="Codex" isLive />);

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));
    await waitFor(() =>
      expect(screen.getAllByTestId("event")).toHaveLength(1),
    );

    // Simulate a WS `task:message` append into the shared cache.
    act(() => {
      qc.setQueryData<TaskMessagePayload[]>(
        chatKeys.taskMessages(LIVE_TASK_ID),
        (old = []) => mergeTaskMessagesBySeq(old, [msg(2, "Read")]),
      );
    });

    await waitFor(() =>
      expect(screen.getAllByTestId("event")).toHaveLength(2),
    );
  });

  it("live mode: forces a backfill on open even when the cache already has data", async () => {
    const qc = newClient();
    qc.setQueryData(chatKeys.taskMessages(LIVE_TASK_ID), [
      msg(1, "Bash"),
      msg(2, "Read"),
    ]);
    listTaskMessages.mockResolvedValue([msg(1, "Bash"), msg(2, "Read")]);

    renderWith(qc, <TranscriptButton task={baseTask} agentName="Codex" isLive />);

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));

    await waitFor(() =>
      expect(listTaskMessages).toHaveBeenCalledWith(LIVE_TASK_ID),
    );
  });

  it("terminal mode: fetches once on open and does not subscribe to the cache", async () => {
    const qc = newClient();
    listTaskMessages.mockResolvedValue([msg(1, "Bash")]);

    renderWith(
      qc,
      <TranscriptButton
        task={{ ...baseTask, status: "completed", completed_at: "2026-05-15T10:00:10.000Z" }}
        agentName="Codex"
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));
    await waitFor(() =>
      expect(screen.getAllByTestId("event")).toHaveLength(1),
    );

    // A later cache write must NOT reach the terminal dialog: it renders a
    // one-shot local snapshot, never an observer of the shared cache.
    act(() => {
      qc.setQueryData(chatKeys.taskMessages(LIVE_TASK_ID), [
        msg(1, "Bash"),
        msg(2, "Read"),
      ]);
    });

    expect(screen.getAllByTestId("event")).toHaveLength(1);
    expect(listTaskMessages).toHaveBeenCalledTimes(1);
  });

  it("running→terminal: keeps the dialog populated and takes a final backfill", async () => {
    const qc = newClient();
    qc.setQueryData(chatKeys.taskMessages(LIVE_TASK_ID), [msg(1, "Bash")]);
    listTaskMessages.mockResolvedValue([msg(1, "Bash")]);

    const { rerender } = renderWith(
      qc,
      <TranscriptButton task={baseTask} agentName="Codex" isLive />,
    );

    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));
    await waitFor(() =>
      expect(screen.getAllByTestId("event")).toHaveLength(1),
    );
    await waitFor(() => expect(listTaskMessages).toHaveBeenCalledTimes(1));

    // Task finishes: parent flips isLive→false and the status to terminal.
    rerender(
      <QueryClientProvider client={qc}>
        <TranscriptButton
          task={{ ...baseTask, status: "completed", completed_at: "2026-05-15T10:00:10.000Z" }}
          agentName="Codex"
          isLive={false}
        />
      </QueryClientProvider>,
    );

    // Dialog stays mounted (latched), and the terminal transition triggers a
    // second authoritative backfill rather than blanking to local state.
    expect(screen.getByTestId("transcript-dialog")).toBeInTheDocument();
    await waitFor(() => expect(listTaskMessages).toHaveBeenCalledTimes(2));

    // The final tail message still flows in through the shared cache.
    act(() => {
      qc.setQueryData<TaskMessagePayload[]>(
        chatKeys.taskMessages(LIVE_TASK_ID),
        (old = []) => mergeTaskMessagesBySeq(old, [msg(2, "Read")]),
      );
    });
    await waitFor(() =>
      expect(screen.getAllByTestId("event")).toHaveLength(2),
    );
  });
});
