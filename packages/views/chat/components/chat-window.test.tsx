// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createChatStore, registerChatStore, useChatStore } from "@multica/core/chat";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import type { ChatSession } from "@multica/core/types";
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

// The header is what's under test, so the conversation body, composer and
// network layer are stubbed; requests stay pending forever.
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: new Proxy({}, { get: () => () => new Promise(() => {}) }),
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
const chatInputProps = vi.hoisted(() => ({ last: null as { hidden?: boolean } | null }));
vi.mock("./chat-input", () => ({
  ChatInput: (props: { hidden?: boolean }) => {
    chatInputProps.last = props;
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

function renderWindow() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData(chatSessionsOptions("ws-1").queryKey, [session]);
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
  act(() => {
    useChatStore.setState({ isOpen: true, isExpanded: false, activeSessionId: session.id });
  });
});

describe("ChatWindow header buttons", () => {
  it("names the new chat, expand, minimize and close buttons after their tooltips", () => {
    renderWindow();

    expect(screen.getByRole("button", { name: enChat.window.new_chat_tooltip })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: enChat.window.expand_tooltip })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: enChat.window.minimize_tooltip })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: enChat.window.close_tooltip })).toBeInTheDocument();
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

describe("ChatWindow minimize and close", () => {
  beforeEach(() => {
    act(() => {
      useChatStore.setState({ chatBarKept: [session.id], chatBarDismissed: {} });
    });
  });

  it("minimize hides the window and keeps the chat's chip", () => {
    renderWindow();

    fireEvent.click(screen.getByRole("button", { name: enChat.window.minimize_tooltip }));

    expect(useChatStore.getState().isOpen).toBe(false);
    expect(useChatStore.getState().chatBarKept).toEqual([session.id]);
  });

  it("close hides the window and closes the chat's chip", () => {
    renderWindow();

    fireEvent.click(screen.getByRole("button", { name: enChat.window.close_tooltip }));

    const state = useChatStore.getState();
    expect(state.isOpen).toBe(false);
    expect(state.chatBarKept).toEqual([]);
    expect(state.chatBarDismissed).toEqual({ [session.id]: session.updated_at });
  });

  it("turns off voice input in the composer while the window is closed", () => {
    renderWindow();
    expect(chatInputProps.last?.hidden).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: enChat.window.minimize_tooltip }));

    expect(chatInputProps.last?.hidden).toBe(true);
  });
});
