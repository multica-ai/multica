// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { Agent, ChatSession, MemberWithUser } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enAgents from "../../../locales/en/agents.json";
import enCommon from "../../../locales/en/common.json";

const TEST_RESOURCES = { en: { agents: enAgents, common: enCommon } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("../../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

vi.mock("../../../chat/components/chat-message-list", () => ({
  ChatMessageList: ({ messages }: { messages: { id: string }[] }) => (
    <div data-testid="transcript">{messages.length} messages</div>
  ),
}));

const sessions: ChatSession[] = [
  {
    id: "s1",
    workspace_id: "ws-1",
    agent_id: "agent-1",
    creator_id: "user-1",
    title: "How do I deploy?",
    status: "active",
    has_unread: false,
    unread_count: 0,
    last_message: {
      content: "Here are the steps",
      role: "assistant",
      created_at: "2026-07-08T03:00:00Z",
    },
    pinned: false,
    created_at: "2026-07-08T02:00:00Z",
    updated_at: "2026-07-08T03:00:00Z",
  },
  {
    id: "s2",
    workspace_id: "ws-1",
    agent_id: "agent-1",
    creator_id: "user-2",
    title: "",
    status: "archived",
    has_unread: false,
    unread_count: 0,
    last_message: null,
    pinned: false,
    created_at: "2026-07-07T02:00:00Z",
    updated_at: "2026-07-07T02:00:00Z",
  },
];

const messages = [{ id: "m1" }, { id: "m2" }];

vi.mock("@multica/core/chat/queries", () => ({
  agentChatSessionsOptions: vi.fn(() => ({
    queryKey: ["agent-sessions"],
    queryFn: vi.fn(),
  })),
  agentChatMessagesPageOptions: vi.fn(() => ({
    queryKey: ["agent-messages"],
    queryFn: vi.fn(),
  })),
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQuery: () => ({ data: sessions, isLoading: false }),
    useInfiniteQuery: () => ({
      data: { pages: [{ messages }] },
      isLoading: false,
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isFetchingNextPage: false,
    }),
  };
});

import { agentChatSessionsOptions } from "@multica/core/chat/queries";

import { AgentChatsTab } from "./chats-tab";

const agent = {
  id: "agent-1",
  name: "Alpha",
} as unknown as Agent;

const members: MemberWithUser[] = [
  {
    id: "mem-1",
    workspace_id: "ws-1",
    user_id: "user-1",
    role: "member",
    created_at: "2026-01-01T00:00:00Z",
    name: "Alice",
    email: "alice@multica.test",
    avatar_url: null,
  },
  {
    id: "mem-2",
    workspace_id: "ws-1",
    user_id: "user-2",
    role: "member",
    created_at: "2026-01-01T00:00:00Z",
    name: "Bob",
    email: "bob@multica.test",
    avatar_url: null,
  },
];

function renderTab() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AgentChatsTab agent={agent} members={members} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  vi.mocked(agentChatSessionsOptions).mockClear();
});

describe("AgentChatsTab", () => {
  it("lists every member's conversations with the member's name", () => {
    renderTab();
    // Alice appears in both the list row and the selected header; Bob only in
    // the list row until selected.
    expect(screen.getAllByText("Alice").length).toBeGreaterThan(0);
    expect(screen.getByText("Bob")).toBeInTheDocument();
    expect(screen.getAllByText("How do I deploy?").length).toBeGreaterThan(0);
  });

  it("auto-selects the most recent conversation and renders its transcript", () => {
    renderTab();
    expect(screen.getByTestId("transcript")).toHaveTextContent("2 messages");
  });

  it("switches the selected conversation on click", () => {
    renderTab();
    fireEvent.click(screen.getByText("Bob"));
    // Both rows still render; the selected header shows Bob.
    expect(screen.getAllByText("Bob").length).toBeGreaterThan(1);
  });

  it("requests archived sessions once the toggle is on", () => {
    renderTab();
    expect(agentChatSessionsOptions).toHaveBeenLastCalledWith("ws-1", "agent-1", false);
    fireEvent.click(screen.getByLabelText(/show archived/i));
    expect(agentChatSessionsOptions).toHaveBeenLastCalledWith("ws-1", "agent-1", true);
  });
});
