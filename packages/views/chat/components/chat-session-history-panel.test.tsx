import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { chatKeys } from "@multica/core/chat/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { Agent, ChatSession, PendingChatTasksResponse } from "@multica/core/types";
import enChat from "../../locales/en/chat.json";

const TEST_RESOURCES = { en: { chat: enChat } };
const setActiveSession = vi.fn();
const setSelectedAgentId = vi.fn();

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/chat", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/chat")>();
  return {
    ...actual,
    useChatStore: (selector: (state: {
      activeSessionId: string | null;
      selectedAgentId: string | null;
      setActiveSession: typeof setActiveSession;
      setSelectedAgentId: typeof setSelectedAgentId;
    }) => unknown) =>
      selector({
        activeSessionId: "session-1",
        selectedAgentId: "agent-1",
        setActiveSession,
        setSelectedAgentId,
      }),
  };
});

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

import { ChatSessionHistoryPanel } from "./chat-window";

function makeAgent(): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    owner_id: "user-1",
    name: "Multica",
    runtime_id: "runtime-1",
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
  };
}

function makeSession(): ChatSession {
  return {
    id: "session-1",
    workspace_id: "ws-1",
    agent_id: "agent-1",
    creator_id: "user-1",
    title: "Current chat",
    status: "active",
    has_unread: false,
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
  };
}

function renderPanel() {
  const qc = new QueryClient();
  qc.setQueryData(chatKeys.sessions("ws-1"), [makeSession()]);
  qc.setQueryData(agentListOptions("ws-1").queryKey, [makeAgent()]);
  qc.setQueryData<PendingChatTasksResponse>(chatKeys.pendingTasks("ws-1"), { tasks: [] });

  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={qc}>
        <ChatSessionHistoryPanel />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("ChatSessionHistoryPanel", () => {
  it("exposes a visible new chat action in the history rail", () => {
    renderPanel();

    const button = screen.getByRole("button", { name: "New chat" });
    button.click();

    expect(setActiveSession).toHaveBeenCalledWith(null);
  });
});
