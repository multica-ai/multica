// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createChatStore, registerChatStore, useChatStore } from "@multica/core/chat";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { Agent, ChatSession } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat } };

const memory = new Map<string, string>();
registerChatStore(
  createChatStore({
    storage: {
      getItem: (k) => memory.get(k) ?? null,
      setItem: (k, v) => void memory.set(k, v),
      removeItem: (k) => void memory.delete(k),
    },
  }),
);

// The header and the send wiring are what's under test, so the conversation
// body, composer and network layer are stubbed; requests are recorded and stay
// pending forever.
const apiCalls = vi.hoisted(() => [] as { method: string; args: unknown[] }[]);
const composer = vi.hoisted(() => ({
  onSend: null as ((content: string) => Promise<boolean>) | null,
}));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: new Proxy(
    {},
    {
      get:
        (_, method: string) =>
        (...args: unknown[]) => {
          apiCalls.push({ method, args });
          return new Promise(() => {});
        },
    },
  ),
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (sel: (s: { user: { id: string } }) => unknown) =>
    sel({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/agents", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents")>()),
  useAgentPresenceDetail: () => "loading",
  useWorkspaceAgentAvailability: () => "loading",
}));
vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => false,
  useIsCompact: () => false,
}));
const resize = vi.hoisted(() => ({ isAtMax: false }));
vi.mock("./use-chat-resize", () => ({
  useChatResize: () => ({
    renderWidth: 380,
    renderHeight: 600,
    isAtMax: resize.isAtMax,
    boundsReady: true,
    isDragging: false,
    toggleExpand: () => {},
    startDrag: () => {},
  }),
}));
vi.mock("./use-chat-context-items", () => ({ useChatContextItems: () => [] }));
vi.mock("./chat-window-messages", () => ({ ChatWindowMessages: () => null }));
vi.mock("../../common/app-requests", () => ({ ChatAppRequests: () => null }));
vi.mock("./chat-input", () => ({
  ChatInput: ({ onSend }: { onSend: (content: string) => Promise<boolean> }) => {
    composer.onSend = onSend;
    return null;
  },
}));
vi.mock("./chat-queue", () => ({ ChatQueue: () => null }));
vi.mock("./chat-empty-state", () => ({ EmptyState: () => null }));
vi.mock("./session-history-list", () => ({ SessionHistoryList: () => null }));
vi.mock("./chat-resize-handles", () => ({ ChatResizeHandles: () => null }));
vi.mock("./offline-banner", () => ({ OfflineBanner: () => null }));
vi.mock("./no-agent-banner", () => ({ NoAgentBanner: () => null }));
vi.mock("./archived-agent-banner", () => ({ ArchivedAgentBanner: () => null }));
vi.mock("./agent-access-revoked-banner", () => ({ AgentAccessRevokedBanner: () => null }));
vi.mock("./runtime-required-banner", () => ({ RuntimeRequiredBanner: () => null }));

import { ChatWindow } from "./chat-window";

// With no session open the session picker's visible label is also "New chat";
// an open session keeps the header button the only control with that name.
const session: ChatSession = {
  id: "session-1",
  workspace_id: "ws-1",
  agent_id: "agent-1",
  creator_id: "user-1",
  title: "Roadmap",
  status: "active",
  has_unread: false,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-01T00:00:00Z",
};

const agent = {
  id: "agent-1",
  name: "Agent",
  owner_id: "user-1",
  runtime_id: "runtime-1",
  runtime_bound: true,
  archived_at: null,
} as Agent;

function renderWindow(agents: Agent[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(chatSessionsOptions("ws-1").queryKey, [session]);
  if (agents.length > 0) qc.setQueryData(agentListOptions("ws-1").queryKey, agents);
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatWindow />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  resize.isAtMax = false;
  apiCalls.length = 0;
  composer.onSend = null;
  act(() => {
    useChatStore.setState({ isOpen: true, isExpanded: false, activeSessionId: session.id });
  });
});

describe("ChatWindow header buttons", () => {
  it("names the new chat, expand and minimize buttons after their tooltips", () => {
    renderWindow();

    expect(screen.getByRole("button", { name: enChat.window.new_chat_tooltip })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: enChat.window.expand_tooltip })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: enChat.window.minimize_tooltip })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: enChat.window.restore_tooltip }),
    ).not.toBeInTheDocument();
  });

  it("names the toggle Restore while the window is expanded", () => {
    act(() => {
      useChatStore.setState({ isExpanded: true });
    });
    renderWindow();

    expect(screen.getByRole("button", { name: enChat.window.restore_tooltip })).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: enChat.window.expand_tooltip }),
    ).not.toBeInTheDocument();
  });

  it("names the toggle Restore when a manual resize already fills the bounds", () => {
    resize.isAtMax = true;
    renderWindow();

    expect(screen.getByRole("button", { name: enChat.window.restore_tooltip })).toBeInTheDocument();
  });
});

describe("ChatWindow first message", () => {
  // The page attached to a message goes ahead of it as a mention link. Cut
  // raw, that link was the whole title: "[DEV-43](mention://issue/…".
  it("titles the new chat after what was written, not the page sent with it", async () => {
    act(() => {
      useChatStore.setState({ activeSessionId: null });
    });
    renderWindow([agent]);

    await act(async () => {
      void composer.onSend?.("[DEV-43](mention://issue/issue-1)\n\nWhy is this failing?");
    });

    await waitFor(() =>
      expect(apiCalls.find((call) => call.method === "createChatSession")?.args[0]).toMatchObject({
        agent_id: "agent-1",
        title: "Why is this failing?",
      }),
    );
  });
});
