// @vitest-environment jsdom

import { StrictMode } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../locales/en/common.json";
import enChat from "../locales/en/chat.json";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "../navigation";

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat } };

// These tests target the page-level URL wiring (`?agent=` / `?session=`), so
// the conversation internals are stubbed and the controller is replaced with
// a ref-driven fake the tests can steer. The thread-list stub stays
// interactive: selecting a thread is the user action that must supersede a
// pending `?agent=` intent.
vi.mock("./components/chat-message-list", () => ({
  ChatMessageList: () => <div>chat-message-list</div>,
  ChatMessageSkeleton: () => <div>chat-message-skeleton</div>,
}));
vi.mock("./components/chat-input", () => ({
  ChatInput: () => <div>chat-input</div>,
}));
vi.mock("./components/chat-thread-list", () => ({
  ChatThreadList: ({
    onSelectSession,
  }: {
    onSelectSession: (s: { id: string; agent_id: string }) => void;
  }) => (
    <button
      type="button"
      onClick={() => onSelectSession({ id: "session-9", agent_id: "agent-9" })}
    >
      select-thread
    </button>
  ),
}));
vi.mock("./components/chat-session-header", () => ({
  ChatSessionHeader: () => <div>chat-session-header</div>,
}));
vi.mock("./components/chat-empty-state", () => ({
  EmptyState: () => <div>chat-empty-state</div>,
}));
vi.mock("./components/new-chat-button", () => ({
  NewChatButton: () => <div>new-chat-button</div>,
}));
vi.mock("./components/offline-banner", () => ({
  OfflineBanner: () => null,
}));
vi.mock("./components/no-agent-banner", () => ({
  NoAgentBanner: () => null,
}));
vi.mock("./components/archived-agent-banner", () => ({
  ArchivedAgentBanner: () => null,
}));
vi.mock("react-resizable-panels", () => ({
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
}));
vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  ResizablePanel: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  ResizableHandle: () => null,
}));
vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => false,
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ chat: () => "/acme/chat" }),
}));

// The store mock is REACTIVE like real Zustand: setActiveSession replaces the
// snapshot and notifies subscribers, and the controller mock subscribes via
// useSyncExternalStore. A plain mutable ref would let React bail out of
// committing the post-effect re-render (no React state changed), leaving the
// DOM frozen on the first commit — which silently hides exactly the class of
// bug the StrictMode regression below exists to catch.
const storeRef = vi.hoisted(() => ({
  current: { activeSessionId: null as string | null },
}));
const storeListeners = vi.hoisted(() => new Set<() => void>());
const availableAgentsRef = vi.hoisted(() => ({ current: [] as Agent[] }));
const agentsSettledRef = vi.hoisted(() => ({ current: true }));
const mockStartNewChat = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockSetActiveSession = vi.hoisted(() =>
  vi.fn((id: string | null) => {
    storeRef.current = { ...storeRef.current, activeSessionId: id };
    storeListeners.forEach((l) => l());
  }),
);
const subscribeToStore = vi.hoisted(() => (cb: () => void) => {
  storeListeners.add(cb);
  return () => storeListeners.delete(cb);
});

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: mockToastError },
}));

vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector?: (s: { activeSessionId: string | null }) => unknown) =>
      selector ? selector(storeRef.current) : storeRef.current,
    { getState: () => storeRef.current },
  ),
}));

vi.mock("./components/use-chat-controller", async () => {
  const { useSyncExternalStore } = await import("react");
  return {
    useChatController: () => ({
      wsId: "ws-1",
      user: { id: "user-1" },
      agents: availableAgentsRef.current,
      availableAgents: availableAgentsRef.current,
      agentsSettled: agentsSettledRef.current,
      sessions: [],
      activeSessionId: useSyncExternalStore(
        subscribeToStore,
        () => storeRef.current,
      ).activeSessionId,
      selectedAgentId: null,
      currentSession: null,
      isSessionArchived: false,
      isAgentArchived: false,
      activeAgent: availableAgentsRef.current[0] ?? null,
      noAgent: false,
      availability: "online",
      messages: [],
      pendingTask: null,
      pendingTaskId: null,
      showSkeleton: false,
      hasMessages: false,
      firstItemIndex: 0,
      hasOlderMessages: false,
      isFetchingOlderMessages: false,
      fetchOlderMessages: vi.fn(),
      restoreDraftRequest: null,
      handleRestoreDraftConsumed: vi.fn(),
      focusInputRequest: 0,
      handleSend: vi.fn(),
      handleStop: vi.fn(),
      handleUploadFile: vi.fn(),
      handleNewChat: vi.fn(),
      handleStartNewChat: mockStartNewChat,
      handleSelectSession: vi.fn(),
      advanceSelectionAfterArchive: vi.fn(),
      archiveSession: vi.fn(),
      setActiveSession: mockSetActiveSession,
      setSelectedAgentId: vi.fn(),
    }),
  };
});

import { ChatPage } from "./chat-page";

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Lambda",
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
  model: "",
  owner_id: "user-2",
  skills: [],
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

const NO_ACCESS_MSG = "You don't have access to chat with this agent.";

function renderPage(search: string, { strict = false } = {}) {
  const replace = vi.fn();
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace,
    back: vi.fn(),
    pathname: "/acme/chat",
    searchParams: new URLSearchParams(search),
    getShareableUrl: (path) => path,
  };
  // A fresh element per render — reusing one element object lets React bail
  // out of re-rendering, which would make the rerender-based tests vacuous.
  const makeUi = () => {
    const page = (
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <NavigationProvider value={navigation}>
          <ChatPage />
        </NavigationProvider>
      </I18nProvider>
    );
    return strict ? <StrictMode>{page}</StrictMode> : page;
  };
  const view = render(makeUi());
  return { replace, rerender: () => view.rerender(makeUi()) };
}

beforeEach(() => {
  vi.clearAllMocks();
  storeRef.current = { activeSessionId: null };
  storeListeners.clear();
  availableAgentsRef.current = [agent];
  agentsSettledRef.current = true;
});

describe("ChatPage ?agent= deep link", () => {
  it("starts a new chat with the linked agent and strips the param", () => {
    const { replace } = renderPage("agent=agent-1");
    expect(mockStartNewChat).toHaveBeenCalledTimes(1);
    expect(mockStartNewChat).toHaveBeenCalledWith(agent);
    expect(replace).toHaveBeenCalledWith("/acme/chat");
    // composingNew opened the conversation pane instead of the neutral prompt.
    expect(screen.getByText("chat-input")).toBeInTheDocument();
  });

  it("consumes the intent only once even while the param is still in the URL", () => {
    // navigation.replace is async in real adapters — the param outlives the
    // consuming render. The real controller also allocates a fresh
    // availableAgents array every render (it filters), so the effect re-runs;
    // only the consumed-intent guard stops a second chat from starting.
    const { rerender } = renderPage("agent=agent-1");
    availableAgentsRef.current = [agent];
    rerender();
    expect(mockStartNewChat).toHaveBeenCalledTimes(1);
  });

  it("stays pending without a toast while the agent/member queries load", () => {
    availableAgentsRef.current = [];
    agentsSettledRef.current = false;
    const { replace } = renderPage("agent=agent-1");
    expect(mockStartNewChat).not.toHaveBeenCalled();
    expect(mockToastError).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
  });

  it("waits for the agent list to resolve before consuming the intent", () => {
    availableAgentsRef.current = [];
    agentsSettledRef.current = false;
    const { replace, rerender } = renderPage("agent=agent-1");
    expect(mockStartNewChat).not.toHaveBeenCalled();
    availableAgentsRef.current = [agent];
    agentsSettledRef.current = true;
    rerender();
    expect(mockStartNewChat).toHaveBeenCalledWith(agent);
    expect(replace).toHaveBeenCalledWith("/acme/chat");
  });

  it("toasts and strips the param once the list settles without the agent", () => {
    // Revoked access, archived agent, or a bad id: a settled miss must
    // explain itself and consume the intent so a later refetch that surfaces
    // the agent cannot auto-start a chat without a fresh click.
    const { replace, rerender } = renderPage("agent=other-agent");
    expect(mockStartNewChat).not.toHaveBeenCalled();
    expect(mockToastError).toHaveBeenCalledWith(NO_ACCESS_MSG);
    expect(replace).toHaveBeenCalledWith("/acme/chat");
    expect(screen.queryByText("chat-input")).not.toBeInTheDocument();
    rerender();
    expect(mockToastError).toHaveBeenCalledTimes(1);
  });

  it("lets an explicit thread selection supersede a still-pending intent", () => {
    // Deferred-intent race (review P1): the user picks a thread while the
    // agent/member queries are still loading; when they settle, the stale
    // deep link must NOT fire and clobber that selection.
    availableAgentsRef.current = [];
    agentsSettledRef.current = false;
    const { replace, rerender } = renderPage("agent=agent-1");
    fireEvent.click(screen.getByRole("button", { name: "select-thread" }));
    availableAgentsRef.current = [agent];
    agentsSettledRef.current = true;
    rerender();
    expect(mockStartNewChat).not.toHaveBeenCalled();
    expect(mockToastError).not.toHaveBeenCalled();
    expect(replace).not.toHaveBeenCalled();
  });

  it("opens the compose pane under StrictMode with a persisted previous session", () => {
    // StrictMode replays mount effects with render-captured values (review
    // P2): the composingNew reset must read the LIVE store value, or the
    // stale persisted session re-closes the pane the intent just opened —
    // while the consumed-intent guard rightly refuses to fire again.
    storeRef.current = { activeSessionId: "old-session" };
    const { replace } = renderPage("agent=agent-1", { strict: true });
    expect(mockStartNewChat).toHaveBeenCalledTimes(1);
    expect(replace).toHaveBeenCalledWith("/acme/chat");
    expect(screen.getByText("chat-input")).toBeInTheDocument();
  });
});
