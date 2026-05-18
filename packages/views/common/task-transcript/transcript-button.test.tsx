import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { issueKeys } from "@multica/core/issues/queries";
import type { AgentTask } from "@multica/core/types/agent";
import type { TaskMessagePayload } from "@multica/core/types/events";
import type { TimelineItem } from "./build-timeline";
import { TranscriptButton } from "./transcript-button";

const mockApi = vi.hoisted(() => ({
  listTaskMessages: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApi,
}));

vi.mock("./agent-transcript-dialog", () => ({
  AgentTranscriptDialog: ({ items }: { items: TimelineItem[] }) => (
    <div data-testid="transcript-dialog">
      {items.length}:{items.map((item) => item.content ?? item.output ?? "").join("|")}
    </div>
  ),
}));

function makeQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
}

function makeTask(overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id: "task-1",
    agent_id: "agent-1",
    runtime_id: "runtime-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: "2026-01-01T00:00:00Z",
    started_at: "2026-01-01T00:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function makeMessage(seq: number, content: string): TaskMessagePayload {
  return {
    task_id: "task-1",
    issue_id: "issue-1",
    seq,
    type: "final",
    content,
  };
}

function renderButton(
  queryClient: QueryClient,
  props: Partial<React.ComponentProps<typeof TranscriptButton>> = {},
) {
  render(
    <QueryClientProvider client={queryClient}>
      <TranscriptButton task={makeTask()} agentName="Codex" {...props} />
    </QueryClientProvider>,
  );
}

describe("TranscriptButton", () => {
  beforeEach(() => {
    mockApi.listTaskMessages.mockReset();
  });

  it("keeps lazy transcripts subscribed to the shared task message cache", async () => {
    const queryClient = makeQueryClient();
    mockApi.listTaskMessages.mockResolvedValueOnce([makeMessage(1, "first")]);

    renderButton(queryClient);
    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));

    await waitFor(() => {
      expect(screen.getByTestId("transcript-dialog")).toHaveTextContent("1:first");
    });

    queryClient.setQueryData<TaskMessagePayload[]>(
      issueKeys.taskMessages("task-1"),
      (old = []) => [...old, makeMessage(2, "second")],
    );

    await waitFor(() => {
      expect(screen.getByTestId("transcript-dialog")).toHaveTextContent("2:first|second");
    });
  });

  it("uses provided live items without fetching", async () => {
    const queryClient = makeQueryClient();

    renderButton(queryClient, {
      items: [{ seq: 1, type: "final", content: "provided" }],
      isLive: true,
    });
    fireEvent.click(screen.getByRole("button", { name: "View transcript" }));

    expect(screen.getByTestId("transcript-dialog")).toHaveTextContent("1:provided");
    expect(mockApi.listTaskMessages).not.toHaveBeenCalled();
  });
});
