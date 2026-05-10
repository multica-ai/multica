import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// ---------------------------------------------------------------------------
// JEH-654 regression guard. handleSend's optimistic pendingTask write
// MUST NOT clobber a running task that's already in cache — otherwise
// the chat window flips its pendingTaskId to the queued successor mid-
// stream and the live timeline disappears until the running task settles.
//
// We mock ChatInput so a click triggers handleSend with a fixed payload,
// and stub api.sendChatMessage to return a queued successor task. The
// assertion: after send, the cache still points at the original running
// task. The follow-up arrives via WS invalidate → refetch (not in scope
// for this test).
// ---------------------------------------------------------------------------

// vi.hoisted because vi.mock() below is hoisted above this file's
// top-level statements; a plain const would be in the TDZ when the mock
// factories run. Constants used inside vi.mock factories live here for
// the same reason.
const { SESSION_ID, RUNNING_TASK_ID, sendChatMessage } = vi.hoisted(() => ({
  SESSION_ID: "session-1",
  RUNNING_TASK_ID: "task-running",
  sendChatMessage: vi.fn(
    async (_sessionId: string, _content: string) => ({
      message_id: "msg-2",
      task_id: "task-successor",
    }),
  ),
}));

vi.mock("@multica/cerebro-chat/views", () => ({
  ChatStatusLine: () => <div data-testid="chat-status-line" />,
  ChatSessionHeader: () => <div data-testid="chat-session-header" />,
  getToolSummary: () => null,
}));

vi.mock("./chat-message-list", () => ({
  ChatMessageList: () => <div data-testid="chat-message-list" />,
  ChatMessageSkeleton: () => <div data-testid="chat-message-skeleton" />,
}));

vi.mock("./chat-input", () => ({
  ChatInput: ({ onSend }: { onSend: (content: string) => void }) => (
    <button type="button" data-testid="send" onClick={() => onSend("msg-2")}>
      send
    </button>
  ),
}));

vi.mock("./context-anchor", () => ({
  ContextAnchorButton: () => null,
  ContextAnchorCard: () => null,
  buildAnchorMarkdown: () => "",
  useRouteAnchorCandidate: () => ({ candidate: null, isResolving: false }),
}));

vi.mock("./chat-session-history", () => ({
  ChatSessionHistory: () => null,
}));

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (key: unknown) =>
      typeof key === "function" ? "" : String(key ?? ""),
  }),
}));

vi.mock("./chat-resize-handles", () => ({
  ChatResizeHandles: () => null,
}));

vi.mock("./use-chat-resize", () => ({
  useChatResize: () => ({
    renderWidth: 420,
    renderHeight: 600,
    isAtMax: false,
    boundsReady: true,
    isDragging: false,
    toggleExpand: () => {},
    startDrag: () => {},
  }),
}));

vi.mock("@multica/core/chat", () => {
  const chatState = {
    isOpen: true,
    activeSessionId: SESSION_ID,
    selectedAgentId: "agent-1",
    hideFloatingChat: false,
    setOpen: vi.fn(),
    setActiveSession: vi.fn(),
    setSelectedAgentId: vi.fn(),
  };
  return {
    useChatStore: Object.assign(
      (selector?: (s: typeof chatState) => unknown) =>
        selector ? selector(chatState) : chatState,
      { getState: () => chatState },
    ),
    DRAFT_NEW_SESSION: "__new__",
  };
});

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: { user: { id: string } }) => unknown) => {
      const state = { user: { id: "user-1" } };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: { id: "user-1" } }) },
  ),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["agents"],
    queryFn: async () => [
      { id: "agent-1", name: "Tester", archived_at: null, owner_id: "user-1" },
    ],
  }),
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => [] }),
}));

vi.mock("@multica/views/issues/components", () => ({
  canAssignAgent: () => true,
}));

vi.mock("@multica/core/chat/queries", () => ({
  chatSessionsOptions: () => ({ queryKey: ["sessions"], queryFn: async () => [] }),
  allChatSessionsOptions: () => ({ queryKey: ["sessions", "all"], queryFn: async () => [] }),
  chatMessagesOptions: () => ({ queryKey: ["messages", SESSION_ID], queryFn: async () => [] }),
  // queryFn returns the running task — simulates the server confirming
  // there's an in-flight task. Without this, useQuery would overwrite
  // the pre-seeded cache value on first fetch and the optimistic write
  // wouldn't see anything to preserve.
  pendingChatTaskOptions: () => ({
    queryKey: ["pending-task", SESSION_ID],
    queryFn: async () => ({ task_id: RUNNING_TASK_ID, status: "running" }),
    staleTime: Infinity,
  }),
  pendingChatTasksOptions: () => ({
    queryKey: ["pending-tasks"],
    queryFn: async () => ({ tasks: [] }),
  }),
  chatKeys: {
    messages: (id: string) => ["messages", id],
    pendingTask: (id: string) => ["pending-task", id],
  },
}));

vi.mock("@multica/core/chat/mutations", () => ({
  useCreateChatSession: () => ({ mutateAsync: vi.fn() }),
  useDeleteChatSession: () => ({ mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false }),
  useMarkChatSessionRead: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    sendChatMessage,
    cancelTaskById: vi.fn(),
  },
}));

vi.mock("@multica/core/logger", () => ({
  createLogger: () => ({
    debug: () => {},
    info: () => {},
    warn: () => {},
    error: () => {},
  }),
}));

import { ChatWindow } from "./chat-window";

function renderWithRunningTask() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  // Pre-seed: the prior turn is still streaming.
  qc.setQueryData(["pending-task", SESSION_ID], {
    task_id: RUNNING_TASK_ID,
    status: "running",
  });
  qc.setQueryData(["messages", SESSION_ID], []);
  const utils = render(
    <QueryClientProvider client={qc}>
      <ChatWindow />
    </QueryClientProvider>,
  );
  return { qc, ...utils };
}

beforeEach(() => {
  sendChatMessage.mockClear();
});

describe("ChatWindow handleSend (JEH-654)", () => {
  it("does not overwrite a running pending-task with the queued successor", async () => {
    const user = userEvent.setup();
    const { qc, getByTestId } = renderWithRunningTask();

    await user.click(getByTestId("send"));

    // sendChatMessage was called — the request still went out.
    await waitFor(() => expect(sendChatMessage).toHaveBeenCalledTimes(1));

    // After the optimistic write window settles, the cached pending-task
    // is still the running one. The queued successor will surface via WS
    // invalidate → refetch (not driven by this code path).
    await waitFor(() => {
      expect(qc.getQueryData(["pending-task", SESSION_ID])).toEqual({
        task_id: RUNNING_TASK_ID,
        status: "running",
      });
    });
  });
});
