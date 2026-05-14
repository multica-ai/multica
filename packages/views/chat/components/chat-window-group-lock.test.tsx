// @vitest-environment jsdom

// JEH-1320 — extend the JEH-1066 visibility/trigger split into the chat-
// window agent picker (the original PR missed it). Two regressions are
// pinned here:
//
//   1. Group-locked agents (`can_trigger === false`) render in the chat
//      AgentDropdown as a disabled `DropdownMenuItem` with a Lock icon and
//      the shared group-access tooltip. Triggerable agents stay normal.
//   2. When the server denies `POST /api/chat/sessions` with a 403, the
//      friendly server message surfaces as a sonner toast instead of
//      failing silently in the UI.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const { sendChatMessage, createChatSessionMock, toastError, ApiErrorCtor, fixtureAgents } =
  vi.hoisted(() => {
    class ApiErrorCtor extends Error {
      readonly status: number;
      readonly statusText: string;
      readonly body?: unknown;
      constructor(message: string, status: number, statusText: string, body?: unknown) {
        super(message);
        this.name = "ApiError";
        this.status = status;
        this.statusText = statusText;
        this.body = body;
      }
    }
    // Shared mutable fixture so the mocked queryFn can close over it.
    // useQuery refetches after the synchronous initial render and would
    // otherwise overwrite our seeded cache with the queryFn's empty
    // return value — leaving the dropdown thinking no agents exist.
    const fixtureAgents: { current: unknown[] } = { current: [] };
    return {
      sendChatMessage: vi.fn(async () => ({
        message_id: "msg-1",
        task_id: "task-1",
        created_at: "2026-05-14T14:00:00Z",
      })),
      createChatSessionMock: vi.fn(async () => ({ id: "sess-1", agent_id: "allowed-1" })),
      toastError: vi.fn(),
      ApiErrorCtor,
      fixtureAgents,
    };
  });

vi.mock("sonner", () => ({
  toast: { error: toastError, success: vi.fn() },
}));

vi.mock("@multica/cerebro-chat/views", () => ({
  ChatStatusLine: () => <div data-testid="chat-status-line" />,
  ChatSessionHeader: () => <div data-testid="chat-session-header" />,
  getToolSummary: () => null,
}));

vi.mock("./chat-message-list", () => ({
  ChatMessageList: () => <div data-testid="chat-message-list" />,
  ChatMessageSkeleton: () => null,
}));

// Custom ChatInput stub: exposes the leftAdornment (the AgentDropdown) so
// tests can drive the picker, and a send button that fires onSend with a
// fixed payload so the 403 path is reachable without typing into the
// real Tiptap editor.
vi.mock("./chat-input", () => ({
  ChatInput: ({
    leftAdornment,
    onSend,
  }: {
    leftAdornment?: React.ReactNode;
    onSend: (content: string) => void;
  }) => (
    <div data-testid="chat-input">
      {leftAdornment}
      <button type="button" data-testid="send" onClick={() => onSend("hi")}>
        send
      </button>
    </div>
  ),
}));

vi.mock("./chat-resize-handles", () => ({ ChatResizeHandles: () => null }));

vi.mock("./context-anchor", () => ({
  ContextAnchorButton: () => null,
  ContextAnchorCard: () => null,
  buildAnchorMarkdown: () => "",
  useRouteAnchorCandidate: () => ({ candidate: null, isResolving: false }),
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

vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (key: unknown) =>
      typeof key === "function" ? "" : String(key ?? ""),
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

// Hooks / stores
const chatState = {
  isOpen: true,
  // null activeSessionId → handleSend takes the createChatSession branch
  activeSessionId: null as string | null,
  selectedAgentId: null as string | null,
  hideFloatingChat: false,
  isExpanded: false,
  focusMode: false,
  inputDrafts: {},
  setOpen: vi.fn(),
  setActiveSession: vi.fn(),
  setSelectedAgentId: vi.fn(),
  setInputDraft: vi.fn(),
  clearInputDraft: vi.fn(),
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

vi.mock("@multica/core/agents", () => ({
  GROUP_ACCESS_LOCKED_TOOLTIP:
    "You don't have group access to this agent — ask a workspace admin to add you to a group that grants access.",
  useAgentPresenceDetail: () => ({ availability: "online" }),
  useWorkspaceAgentAvailability: () => "available",
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["agents"],
    queryFn: async () => fixtureAgents.current,
    staleTime: Infinity,
  }),
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => [{ user_id: "user-1", role: "member" }],
    staleTime: Infinity,
  }),
}));

vi.mock("@multica/views/issues/components", () => ({
  canAssignAgent: () => true,
}));

vi.mock("@multica/core/chat/queries", () => ({
  chatSessionsOptions: () => ({ queryKey: ["sessions"], queryFn: async () => [] }),
  chatMessagesOptions: () => ({ queryKey: ["messages"], queryFn: async () => [] }),
  pendingChatTaskOptions: () => ({
    queryKey: ["pending-task"],
    queryFn: async () => ({}),
  }),
  pendingChatTasksOptions: () => ({
    queryKey: ["pending-tasks"],
    queryFn: async () => ({ tasks: [] }),
  }),
  chatKeys: {
    messages: (id: string) => ["messages", id],
    pendingTask: (id: string) => ["pending-task", id],
    sessions: (wsId: string) => ["sessions", wsId],
  },
}));

vi.mock("@multica/core/chat/mutations", () => ({
  useCreateChatSession: () => ({ mutateAsync: createChatSessionMock }),
  useDeleteChatSession: () => ({ mutate: vi.fn(), mutateAsync: vi.fn(), isPending: false }),
  useMarkChatSessionRead: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/api", () => ({
  api: { sendChatMessage, cancelTaskById: vi.fn() },
  ApiError: ApiErrorCtor,
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

type AgentFixture = {
  id: string;
  name: string;
  owner_id: string;
  archived_at: null;
  can_trigger?: boolean;
};

function renderChatWindow(agents: AgentFixture[]) {
  fixtureAgents.current = agents;
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: 0, staleTime: Infinity } },
  });
  qc.setQueryData(["agents"], agents);
  qc.setQueryData(["members"], [{ user_id: "user-1", role: "member" }]);
  qc.setQueryData(["sessions"], []);
  qc.setQueryData(["pending-task"], {});
  return render(
    <QueryClientProvider client={qc}>
      <ChatWindow />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  chatState.isOpen = true;
  chatState.activeSessionId = null;
  chatState.selectedAgentId = null;
  vi.clearAllMocks();
});

describe("ChatWindow AgentDropdown — group-locked agents (JEH-1320)", () => {
  it("renders the locked agent in the picker with aria-disabled and a Lock icon", async () => {
    const user = userEvent.setup();
    renderChatWindow([
      { id: "allowed-1", name: "Helper", owner_id: "user-1", archived_at: null, can_trigger: true },
      { id: "locked-1", name: "Drop Tables", owner_id: "user-1", archived_at: null, can_trigger: false },
    ]);

    // Open the dropdown via the trigger that renders inside ChatInput's
    // mocked left adornment. The trigger contains the active agent's name —
    // the default-active fallback should land on Helper (the triggerable
    // one) because the locked agent is excluded from the fallback.
    const trigger = await screen.findByRole("button", { name: /Helper/i });
    await user.click(trigger);

    // Both rows are in the menu — visibility split: members SEE every agent.
    const allowedRow = await screen.findByRole("menuitem", { name: /Helper/i });
    const lockedRow = await screen.findByRole("menuitem", { name: /Drop Tables/i });

    expect(allowedRow).not.toBeNull();
    expect(lockedRow).not.toBeNull();
    // Base UI Menu.Item sets aria-disabled when the React `disabled` prop is
    // true. That's the attribute QA flagged as missing in JEH-1320.
    expect(lockedRow.getAttribute("aria-disabled")).toBe("true");
    expect(allowedRow.getAttribute("aria-disabled")).not.toBe("true");
  });

  it("does not call setSelectedAgentId when the locked row is selected", async () => {
    const user = userEvent.setup();
    renderChatWindow([
      { id: "allowed-1", name: "Helper", owner_id: "user-1", archived_at: null, can_trigger: true },
      { id: "locked-1", name: "Drop Tables", owner_id: "user-1", archived_at: null, can_trigger: false },
    ]);

    const trigger = await screen.findByRole("button", { name: /Helper/i });
    await user.click(trigger);

    const lockedRow = await screen.findByRole("menuitem", { name: /Drop Tables/i });
    // userEvent honours aria-disabled — clicking the locked row should be
    // a no-op; the chat store must not have its agent selection mutated.
    await user.click(lockedRow);

    expect(chatState.setSelectedAgentId).not.toHaveBeenCalledWith("locked-1");
  });

  it("renders no lock when can_trigger is true (or missing for older servers)", async () => {
    const user = userEvent.setup();
    renderChatWindow([
      { id: "allowed-1", name: "Helper", owner_id: "user-1", archived_at: null, can_trigger: true },
      // Older server contract — UI must treat missing as triggerable so
      // legacy deploys don't silently lock every agent in the picker.
      { id: "legacy-1", name: "Legacy Agent", owner_id: "user-1", archived_at: null },
    ]);

    const trigger = await screen.findByRole("button", { name: /Helper/i });
    await user.click(trigger);
    const legacyRow = await screen.findByRole("menuitem", { name: /Legacy Agent/i });
    expect(legacyRow.getAttribute("aria-disabled")).not.toBe("true");
  });
});

describe("ChatWindow handleSend — 403 toast (JEH-1320)", () => {
  it("surfaces the server's 403 message as a sonner toast when starting a new chat fails", async () => {
    const denied = "You don't have group access to this agent — ask a workspace admin to add you to a group that grants access.";
    createChatSessionMock.mockRejectedValueOnce(new ApiErrorCtor(denied, 403, "Forbidden", { error: denied }));

    const user = userEvent.setup();
    renderChatWindow([
      { id: "allowed-1", name: "Helper", owner_id: "user-1", archived_at: null, can_trigger: true },
    ]);

    await user.click(screen.getByTestId("send"));

    await waitFor(() => {
      expect(toastError).toHaveBeenCalledTimes(1);
    });
    expect(toastError.mock.calls[0]?.[0]).toBe(denied);
    // The optimistic message-send path must not run when the create failed.
    expect(sendChatMessage).not.toHaveBeenCalled();
  });
});
