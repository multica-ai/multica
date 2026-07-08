import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { chatKeys } from "@multica/core/chat/queries";
import type { Agent, ChatSession } from "@multica/core/types";
import enChat from "../../locales/en/chat.json";

// ActorAvatar fetches agent presence/photo — stub it to a marker span so the
// list renders without a QueryClient round-trip, and so we can assert the
// archived dimming class lands on the avatar.
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId, className }: { actorId: string; className?: string }) => (
    <span data-testid={`avatar-${actorId}`} className={className} />
  ),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/agents", () => ({
  useWorkspacePresenceMap: () => ({ byAgent: new Map() }),
}));

vi.mock("@multica/core/chat", () => {
  const state = { setActiveSession: vi.fn() };
  return {
    useChatStore: Object.assign(
      (selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state),
      { getState: () => state },
    ),
  };
});

vi.mock("@multica/core/chat/mutations", () => ({
  useDeleteChatSession: () => ({ mutate: vi.fn(), isPending: false }),
  useSetChatSessionPinned: () => ({ mutate: vi.fn() }),
}));

import { ChatThreadList } from "./chat-thread-list";

const TEST_RESOURCES = { en: { chat: enChat } };

function makeAgent(overrides: Partial<Agent> & Pick<Agent, "id" | "name">): Agent {
  return {
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    owner_id: "user-1",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    permission_mode: "public_to",
    invocation_targets: [{ target_type: "workspace", target_id: null }],
    status: "idle",
    max_concurrent_tasks: 1,
    model: "sonnet",
    skills: [],
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    archived_at: null,
    archived_by: null,
    ...overrides,
  } as Agent;
}

function makeSession(overrides: Partial<ChatSession> & Pick<ChatSession, "id" | "agent_id">): ChatSession {
  return {
    workspace_id: "ws-1",
    title: "A conversation",
    status: "active",
    pinned: false,
    unread_count: 0,
    has_unread: false,
    last_message: {
      content: "hello there",
      role: "assistant",
      created_at: "2026-07-08T00:00:00Z",
      failure_reason: null,
    },
    created_at: "2026-07-08T00:00:00Z",
    updated_at: "2026-07-08T00:00:00Z",
    ...overrides,
  } as ChatSession;
}

function renderList(sessions: ChatSession[], agents: Agent[]) {
  const qc = new QueryClient();
  qc.setQueryData(chatKeys.pendingTasks("ws-1"), { tasks: [] });
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ChatThreadList
          sessions={sessions}
          agents={agents}
          activeSessionId={null}
          onSelectSession={vi.fn()}
        />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("ChatThreadList archived-agent handling (MUL-4265)", () => {
  it("marks a session whose agent is archived and dims its avatar", () => {
    const archived = makeAgent({
      id: "agent-archived",
      name: "Retired Bot",
      archived_at: "2026-07-01T00:00:00Z",
    });
    renderList(
      [makeSession({ id: "s-1", agent_id: "agent-archived", title: "Old chat" })],
      [archived],
    );

    // The "Archived" tag renders on the row.
    expect(screen.getByText(enChat.list.archived)).toBeInTheDocument();
    // The avatar is desaturated/dimmed.
    expect(screen.getByTestId("avatar-agent-archived").className).toContain("grayscale");
  });

  it("does not mark a session whose agent is active", () => {
    const active = makeAgent({ id: "agent-active", name: "Active Bot" });
    renderList(
      [makeSession({ id: "s-2", agent_id: "agent-active", title: "Live chat" })],
      [active],
    );

    expect(screen.queryByText(enChat.list.archived)).not.toBeInTheDocument();
    expect(screen.getByTestId("avatar-agent-active").className).not.toContain("grayscale");
  });

  it("suppresses the live 'typing' indicator for an archived agent even with a pending task", () => {
    const qc = new QueryClient();
    // A stray pending task on the archived agent's session must NOT render as
    // 'typing…' — a retired agent can never be running.
    qc.setQueryData(chatKeys.pendingTasks("ws-1"), {
      tasks: [{ task_id: "t-1", chat_session_id: "s-3", status: "running" }],
    });
    const archived = makeAgent({
      id: "agent-archived",
      name: "Retired Bot",
      archived_at: "2026-07-01T00:00:00Z",
    });
    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <QueryClientProvider client={qc}>
          <ChatThreadList
            sessions={[makeSession({ id: "s-3", agent_id: "agent-archived" })]}
            agents={[archived]}
            activeSessionId={null}
            onSelectSession={vi.fn()}
          />
        </QueryClientProvider>
      </I18nProvider>,
    );

    expect(screen.queryByText(enChat.list.typing)).not.toBeInTheDocument();
    // Falls back to the last-message preview instead.
    expect(screen.getByText(/hello there/)).toBeInTheDocument();
  });
});
