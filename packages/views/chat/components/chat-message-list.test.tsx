import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { TaskMessagePayload, ChatMessage } from "@multica/core/types";
import { chatKeys } from "@multica/core/chat/queries";

vi.mock("../../common/markdown", () => ({
  Markdown: ({ children }: { children: string }) => (
    <div data-testid="md">{children}</div>
  ),
}));

vi.mock("@multica/ui/hooks/use-scroll-fade", () => ({
  useScrollFade: () => ({}),
}));

vi.mock("@multica/cerebro-ui/hooks/use-sticky-bottom", () => ({
  useStickyBottom: () => ({ hasNewBelow: false, scrollToBottom: () => {} }),
}));

import { ChatMessageList } from "./chat-message-list";
import { ChatStatusLine } from "@multica/cerebro-chat/views";

const TASK_ID = "task-live-1";
const COMPLETED_TASK_ID = "task-done-1";

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });
}

function withQuery(ui: React.ReactNode, qc: QueryClient) {
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

function makeTimeline(items: Partial<TaskMessagePayload>[]): TaskMessagePayload[] {
  return items.map((p, i) => ({
    task_id: TASK_ID,
    issue_id: "issue-1",
    seq: i + 1,
    type: "tool_use",
    ...p,
  })) as TaskMessagePayload[];
}

describe("ChatMessageList", () => {
  it("renders the live timeline with tool groups expanded by default while pendingTaskId is set", () => {
    const qc = createTestQueryClient();
    qc.setQueryData(
      chatKeys.taskMessages(TASK_ID),
      makeTimeline([
        { type: "tool_use", tool: "Bash", input: { command: "ls" }, seq: 1 },
        { type: "tool_use", tool: "Read", input: { file_path: "/a/b/c/foo/bar.ts" }, seq: 2 },
      ]),
    );

    withQuery(
      <ChatMessageList
        messages={[]}
        pendingTask={{ task_id: TASK_ID, status: "running" }}
        availability={undefined}
      />,
      qc,
    );

    // Tool names + summaries are visible without any clicks because the
    // live group renders open by default.
    expect(screen.getByText("Bash")).toBeInTheDocument();
    expect(screen.getByText("ls")).toBeInTheDocument();
    expect(screen.getByText("Read")).toBeInTheDocument();
    expect(screen.getByText(".../foo/bar.ts")).toBeInTheDocument();
  });

  it("keeps tool groups collapsed in completed assistant messages", () => {
    // After upstream PR #2151, intermediate tool steps in a completed
    // assistant message render inside a single OuterProcessFold that is
    // collapsed by default (defaultOpen = isStreaming, and isStreaming is
    // false for a persisted message). We assert the fold renders closed
    // and that no tool names leak into the document until the user opens
    // it manually.
    const qc = createTestQueryClient();
    qc.setQueryData(
      chatKeys.taskMessages(COMPLETED_TASK_ID),
      makeTimeline([
        { type: "tool_use", tool: "EarlyTool", input: { command: "early" }, seq: 1 },
        { type: "text", content: "between", seq: 2 },
        { type: "tool_use", tool: "LateTool", input: { command: "late" }, seq: 3 },
      ]),
    );

    const messages: ChatMessage[] = [
      {
        id: "m1",
        chat_session_id: "s1",
        role: "assistant",
        content: "ignored — taskMessages drives rendering",
        task_id: COMPLETED_TASK_ID,
        created_at: "2026-05-02T00:00:00Z",
        responded_at: null,
      },
    ];

    withQuery(
      <ChatMessageList messages={messages} pendingTask={null} availability={undefined} />,
      qc,
    );

    // Outer fold is closed → neither early nor late tool visible.
    expect(screen.queryByText("EarlyTool")).not.toBeInTheDocument();
    expect(screen.queryByText("LateTool")).not.toBeInTheDocument();
  });
});

describe("MessageBubble user → waiting indicator (JEH-654)", () => {
  function userMessage(opts: { id: string; content: string; respondedAt: string | null }): ChatMessage {
    return {
      id: opts.id,
      chat_session_id: "s1",
      role: "user",
      content: opts.content,
      task_id: null,
      created_at: "2026-05-07T10:00:00Z",
      responded_at: opts.respondedAt,
    };
  }

  it("shows a waiting indicator on user messages with responded_at == null", () => {
    const qc = createTestQueryClient();
    withQuery(
      <ChatMessageList
        messages={[userMessage({ id: "u-pending", content: "still in queue", respondedAt: null })]}
        pendingTask={null}
        availability={undefined}
      />,
      qc,
    );
    expect(screen.getByLabelText("Waiting for agent")).toBeInTheDocument();
  });

  it("does not show the waiting indicator once responded_at is set", () => {
    const qc = createTestQueryClient();
    withQuery(
      <ChatMessageList
        messages={[
          userMessage({
            id: "u-answered",
            content: "already answered",
            respondedAt: "2026-05-07T10:00:30Z",
          }),
        ]}
        pendingTask={null}
        availability={undefined}
      />,
      qc,
    );
    expect(screen.queryByLabelText("Waiting for agent")).not.toBeInTheDocument();
  });
});

describe("ChatStatusLine", () => {
  it("renders nothing when no pending task", () => {
    const qc = createTestQueryClient();
    const { container } = withQuery(<ChatStatusLine pendingTaskId={null} />, qc);
    expect(container).toBeEmptyDOMElement();
  });

  it("shows the latest tool_use's name and summary", () => {
    const qc = createTestQueryClient();
    qc.setQueryData(
      chatKeys.taskMessages(TASK_ID),
      makeTimeline([
        { type: "thinking", content: "considering options", seq: 1 },
        { type: "tool_use", tool: "Bash", input: { command: "pnpm test" }, seq: 2 },
      ]),
    );

    withQuery(<ChatStatusLine pendingTaskId={TASK_ID} />, qc);

    expect(screen.getByRole("status")).toBeInTheDocument();
    expect(screen.getByText("Bash")).toBeInTheDocument();
    expect(screen.getByText("pnpm test")).toBeInTheDocument();
  });

  it("falls back to 'Thinking…' when only thinking events have arrived", () => {
    const qc = createTestQueryClient();
    qc.setQueryData(
      chatKeys.taskMessages(TASK_ID),
      makeTimeline([{ type: "thinking", content: "hmm…", seq: 1 }]),
    );

    withQuery(<ChatStatusLine pendingTaskId={TASK_ID} />, qc);

    expect(screen.getByText("Thinking…")).toBeInTheDocument();
  });

  it("hides itself once the agent is emitting plain text", () => {
    const qc = createTestQueryClient();
    qc.setQueryData(
      chatKeys.taskMessages(TASK_ID),
      makeTimeline([
        { type: "tool_use", tool: "Bash", input: { command: "ls" }, seq: 1 },
        { type: "text", content: "Here is what I found…", seq: 2 },
      ]),
    );

    const { container } = withQuery(<ChatStatusLine pendingTaskId={TASK_ID} />, qc);
    expect(container).toBeEmptyDOMElement();
  });
});
