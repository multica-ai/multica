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

vi.mock("@multica/ui/hooks/use-sticky-bottom", () => ({
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
      <ChatMessageList messages={[]} pendingTaskId={TASK_ID} isWaiting={true} />,
      qc,
    );

    // Tool names + summaries are visible without any clicks because the
    // live group renders open by default.
    expect(screen.getByText("Bash")).toBeInTheDocument();
    expect(screen.getByText("ls")).toBeInTheDocument();
    expect(screen.getByText("Read")).toBeInTheDocument();
    expect(screen.getByText(".../foo/bar.ts")).toBeInTheDocument();
  });

  it("keeps non-final tool groups collapsed in completed assistant messages", () => {
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
      },
    ];

    withQuery(
      <ChatMessageList messages={messages} pendingTaskId={null} isWaiting={false} />,
      qc,
    );

    // Last group is open (current default), early group stays collapsed —
    // EarlyTool is hidden, LateTool is visible.
    expect(screen.queryByText("EarlyTool")).not.toBeInTheDocument();
    expect(screen.getByText("LateTool")).toBeInTheDocument();
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
