import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mockCreateSession = vi.hoisted(() => vi.fn());

const chatState = vi.hoisted(() => ({
  selectedAgentId: "agent-lando",
  setSelectedAgentId: vi.fn(),
  isOpen: false,
  setOpen: vi.fn(),
  setHideFloatingChat: vi.fn(),
}));
const mockAgents = vi.hoisted(() => [
  { id: "agent-lando", name: "Lando - Ecommerce analyst", archived_at: null },
  { id: "agent-sara", name: "Sara - CTO", archived_at: null },
]);
const mockMembers = vi.hoisted(() => [{ user_id: "user-1", role: "owner" }]);

vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector?: (s: typeof chatState) => unknown) =>
      selector ? selector(chatState) : chatState,
    { getState: () => chatState },
  ),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => mockAgents }),
  memberListOptions: () => ({ queryKey: ["members"], queryFn: async () => mockMembers }),
}));

vi.mock("@multica/core/chat/queries", () => ({
  allChatSessionsOptions: () => ({ queryKey: ["sessions", "all"], queryFn: async () => [] }),
  chatMessagesOptions: (id: string) => ({ queryKey: ["messages", id], queryFn: async () => [] }),
  pendingChatTaskOptions: (id: string) => ({ queryKey: ["pending-task", id], queryFn: async () => null }),
  chatKeys: {
    messages: (id: string) => ["messages", id],
    pendingTask: (id: string) => ["pending-task", id],
  },
}));

vi.mock("@multica/core/chat/mutations", () => ({
  useCreateChatSession: () => ({ mutateAsync: mockCreateSession }),
  useMarkChatSessionRead: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    sendChatMessage: vi.fn(async () => ({ task_id: "task-1" })),
    cancelTaskById: vi.fn(),
  },
}));

vi.mock("@multica/views/issues/components", () => ({
  canAssignAgent: () => true,
}));

vi.mock("@multica/views/chat", () => ({
  ChatMessageList: () => <div data-testid="message-list" />,
  ChatMessageSkeleton: () => <div data-testid="message-skeleton" />,
  ChatInput: ({
    agentName,
    onSend,
  }: {
    agentName?: string;
    onSend: (content: string) => void;
  }) => (
    <div>
      <div data-testid="chat-input-agent">{agentName}</div>
      <button type="button" onClick={() => onSend("hello")}>
        Send
      </button>
    </div>
  ),
}));

vi.mock("@multica/cerebro-chat/views", () => ({
  ChatStatusLine: () => null,
}));

vi.mock("@multica/ui/components/ui/avatar", () => ({
  Avatar: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AvatarFallback: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
  AvatarImage: () => null,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuLabel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuSeparator: () => null,
  DropdownMenuTrigger: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

import { InboxChatPanel } from "./inbox-chat-panel";

function renderPanel(initialAgentId: string | null) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0 } },
  });
  qc.setQueryData(["agents"], mockAgents);
  qc.setQueryData(["members"], mockMembers);
  qc.setQueryData(["sessions", "all"], []);

  return render(
    <QueryClientProvider client={qc}>
      <InboxChatPanel sessionId={null} initialAgentId={initialAgentId} />
    </QueryClientProvider>,
  );
}

describe("InboxChatPanel", () => {
  beforeEach(() => {
    chatState.selectedAgentId = "agent-lando";
    chatState.setSelectedAgentId.mockClear();
    chatState.setOpen.mockClear();
    chatState.setHideFloatingChat.mockClear();
    mockCreateSession.mockReset();
    mockCreateSession.mockResolvedValue({ id: "session-sara" });
  });

  it("uses the sidebar-selected URL agent instead of the persisted fallback agent", async () => {
    renderPanel("agent-sara");

    expect(screen.getByTestId("chat-input-agent")).toHaveTextContent("Sara - CTO");
    await waitFor(() => {
      expect(chatState.setSelectedAgentId).toHaveBeenCalledWith("agent-sara");
    });

    await userEvent.click(screen.getByRole("button", { name: "Send" }));

    await waitFor(() => {
      expect(mockCreateSession).toHaveBeenCalledWith({
        agent_id: "agent-sara",
        title: "hello",
      });
    });
  });
});
