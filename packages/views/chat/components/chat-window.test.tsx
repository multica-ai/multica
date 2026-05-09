import { describe, it, expect, vi } from "vitest";
import { render } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// ---- Lightweight mocks ------------------------------------------------------
//
// We don't need ChatWindow's heavy children to actually render — we just need
// to count how many ChatStatusLine instances make it into the tree. A previous
// review caught a paste-duplicate of `<ChatStatusLine />` in this file, and
// no test guarded against it. This test exists to make that regression loud.

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
  ChatInput: () => <div data-testid="chat-input" />,
}));

vi.mock("./chat-resize-handles", () => ({
  ChatResizeHandles: () => null,
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

const chatState = {
  isOpen: true,
  activeSessionId: "session-1",
  selectedAgentId: "agent-1",
  hideFloatingChat: false,
  setOpen: vi.fn(),
  setActiveSession: vi.fn(),
  setSelectedAgentId: vi.fn(),
};

vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector?: (s: typeof chatState) => unknown) =>
      selector ? selector(chatState) : chatState,
    { getState: () => chatState },
  ),
  DRAFT_NEW_SESSION: "__new__",
}));

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
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [] }),
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => [] }),
}));

vi.mock("@multica/views/issues/components", () => ({
  canAssignAgent: () => true,
}));

vi.mock("@multica/core/chat/queries", () => ({
  chatSessionsOptions: () => ({ queryKey: ["sessions"], queryFn: async () => [] }),
  allChatSessionsOptions: () => ({ queryKey: ["sessions", "all"], queryFn: async () => [] }),
  chatMessagesOptions: () => ({ queryKey: ["messages"], queryFn: async () => [] }),
  pendingChatTaskOptions: () => ({
    queryKey: ["pending-task"],
    queryFn: async () => ({ task_id: "task-live", status: "running" }),
  }),
  pendingChatTasksOptions: () => ({
    queryKey: ["pending-tasks"],
    queryFn: async () => ({ tasks: [] }),
  }),
  chatKeys: { messages: (id: string) => ["messages", id], pendingTask: (id: string) => ["pending-task", id] },
}));

vi.mock("@multica/core/chat/mutations", () => ({
  useCreateChatSession: () => ({ mutateAsync: vi.fn() }),
  useMarkChatSessionRead: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({
  api: { sendChatMessage: vi.fn(), cancelTaskById: vi.fn() },
}));

vi.mock("@multica/core/logger", () => ({
  createLogger: () => ({
    debug: () => {},
    info: () => {},
    warn: () => {},
    error: () => {},
  }),
}));

// Pre-seed the pending-task cache so the status line is visible during render.
import { ChatWindow } from "./chat-window";

function renderChatWindow() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  qc.setQueryData(["pending-task"], { task_id: "task-live", status: "running" });
  return render(
    <QueryClientProvider client={qc}>
      <ChatWindow />
    </QueryClientProvider>,
  );
}

describe("ChatWindow", () => {
  it("renders exactly one ChatStatusLine (regression: duplicated block)", () => {
    const { queryAllByTestId } = renderChatWindow();
    expect(queryAllByTestId("chat-status-line")).toHaveLength(1);
  });
});
