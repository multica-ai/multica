// @vitest-environment jsdom

import { act, fireEvent, render, screen } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";

const h = vi.hoisted(() => ({
  createSession: vi.fn(),
  sendChatMessage: vi.fn(),
  setActiveSession: vi.fn(),
  queryClient: {
    getQueryData: vi.fn(),
    setQueryData: vi.fn(),
    invalidateQueries: vi.fn(),
  },
  store: {
    isOpen: true,
    isExpanded: false,
    activeSessionId: null as string | null,
    selectedAgentId: "agent-1",
    selectedProjectId: null as string | null,
    setOpen: vi.fn(),
    setActiveSession: vi.fn(),
    setSelectedAgentId: vi.fn(),
    setSelectedProjectId: vi.fn(),
  },
}));

const agent = {
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
  owner_id: "user-1",
  skills: [],
  created_at: "2026-08-05T00:00:00Z",
  updated_at: "2026-08-05T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

const existingSession = {
  id: "existing-session",
  workspace_id: "ws-1",
  agent_id: "agent-1",
  title: "Previous chat",
  status: "active",
  project_id: null,
  has_unread: false,
  pinned: false,
  created_at: "2026-08-05T00:00:00Z",
  updated_at: "2026-08-05T00:00:00Z",
  last_message: null,
};

vi.mock("motion/react", () => ({
  motion: {
    div: ({ children, ...props }: React.HTMLAttributes<HTMLDivElement>) => (
      <div {...props}>{children}</div>
    ),
  },
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQueryClient: () => h.queryClient,
    useInfiniteQuery: () => ({
      data: { pages: [] },
      isLoading: false,
      fetchNextPage: vi.fn(),
      hasNextPage: false,
      isFetchingNextPage: false,
    }),
    useQuery: (options: { queryKey?: readonly unknown[] }) => {
      const key = options.queryKey ?? [];
      if (key[0] === "workspaces" && key[2] === "agents") return { data: [agent] };
      if (key[0] === "workspaces" && key[2] === "members") return { data: [] };
      if (key[0] === "projects") return { data: [], isSuccess: true };
      if (key[0] === "chat" && key[2] === "sessions") {
        return { data: [existingSession], isSuccess: true };
      }
      if (key[0] === "chat" && key[1] === "pending-task") {
        return { data: null, isLoading: false };
      }
      if (key[0] === "chat" && key[2] === "pending-tasks") {
        return { data: { tasks: [] } };
      }
      return { data: null };
    },
  };
});

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (state: typeof h.store) => unknown) => selector(h.store),
    { getState: () => h.store },
  ),
}));
vi.mock("@multica/core/api", () => ({
  api: { sendChatMessage: h.sendChatMessage },
  dispatchReasonCode: () => "unknown",
}));
vi.mock("@multica/core/agents", () => ({
  isAgentRuntimeBound: () => true,
  useWorkspaceAgentAvailability: () => "available",
  useAgentPresenceDetail: () => ({ availability: "online" }),
}));
vi.mock("@multica/core/chat/mutations", () => ({
  useCreateChatSession: () => ({ mutateAsync: h.createSession }),
  useMarkChatSessionRead: () => ({ mutate: vi.fn() }),
  useRegenerateChatQuickActions: () => ({ mutateAsync: vi.fn() }),
  useSetChatSessionArchived: () => ({ mutateAsync: vi.fn() }),
  useSetChatSessionProject: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useUpdateChatSession: () => ({ mutateAsync: vi.fn() }),
}));
vi.mock("@multica/core/chat/use-quick-actions-pending-timeout", () => ({
  useQuickActionsPendingTimeout: vi.fn(),
}));
vi.mock("@multica/views/issues/components", () => ({ canAssignAgent: () => true }));
vi.mock("@multica/core/logger", () => ({
  createLogger: () => ({ info: vi.fn(), warn: vi.fn(), error: vi.fn() }),
}));
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("../../common/use-app-foreground", () => ({ useAppForeground: () => true }));
vi.mock("./use-quick-actions-failure-toast", () => ({
  useQuickActionsFailureToast: vi.fn(),
}));
vi.mock("./use-chat-draft-restore", () => ({
  useChatDraftRestore: () => ({
    restoreDraftRequest: null,
    enqueueLocalRestore: vi.fn(),
    handleRestoreDraftApplied: vi.fn(),
  }),
}));
vi.mock("./use-chat-task-actions", () => ({
  useChatTaskActions: () => ({
    cancelChatTask: vi.fn(),
    handleEditQueuedTask: vi.fn(),
    handleRemoveQueuedTask: vi.fn(),
    handleClearQueuedTasks: vi.fn(),
    handleSendQueuedTaskNow: vi.fn(),
  }),
}));
vi.mock("./use-chat-input-focus", () => ({
  useChatInputFocus: () => ({ focusRequest: 0, requestInputFocus: vi.fn() }),
}));
vi.mock("./use-chat-context-items", () => ({ useChatContextItems: () => [] }));
vi.mock("./use-chat-resize", () => ({
  useChatResize: () => ({
    renderWidth: 480,
    renderHeight: 640,
    isAtMax: false,
    boundsReady: true,
    isDragging: false,
    toggleExpand: vi.fn(),
    startDrag: vi.fn(),
  }),
}));
vi.mock("./use-chat-project-context-support", () => ({
  useChatProjectContextSupport: () => true,
}));
vi.mock("./chat-message-list", () => ({
  ChatMessageList: () => null,
  ChatMessageSkeleton: () => null,
}));
vi.mock("./chat-input", () => ({ ChatInput: () => null }));
vi.mock("./chat-queue", () => ({ ChatQueue: () => null }));
vi.mock("./chat-resize-handles", () => ({ ChatResizeHandles: () => null }));
vi.mock("./offline-banner", () => ({ OfflineBanner: () => null }));
vi.mock("./no-agent-banner", () => ({ NoAgentBanner: () => null }));
vi.mock("./archived-agent-banner", () => ({ ArchivedAgentBanner: () => null }));
vi.mock("./runtime-required-banner", () => ({ RuntimeRequiredBanner: () => null }));

import { ChatWindow } from "./chat-window";

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat } };
const sendResult = {
  message_id: "message-1",
  task_id: "task-1",
  created_at: "2026-08-05T00:00:01Z",
  supports_queue: true,
  queued: false,
  attachment_ids: [],
};

beforeEach(() => {
  vi.clearAllMocks();
  h.store.activeSessionId = null;
  h.store.setActiveSession = h.setActiveSession;
  h.createSession.mockImplementation(async () => ({
    id: `new-session-${h.createSession.mock.calls.length}`,
  }));
});

afterEach(() => {
  vi.useRealTimers();
});

describe("ChatWindow starter prompts", () => {
  it("debounces rapid starter prompt clicks into one send", async () => {
    let resolveSend!: (value: typeof sendResult) => void;
    const pendingSend = new Promise<typeof sendResult>((resolve) => {
      resolveSend = resolve;
    });
    h.sendChatMessage.mockReturnValue(pendingSend);

    render(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatWindow />
      </I18nProvider>,
    );

    const prompt = screen.getByRole("button", {
      name: "📝Summarize what I did today",
    });
    vi.useFakeTimers();

    fireEvent.click(prompt);
    fireEvent.click(prompt);

    expect(h.createSession).not.toHaveBeenCalled();
    expect(h.sendChatMessage).not.toHaveBeenCalled();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(300);
      await Promise.resolve();
    });

    expect(h.createSession).toHaveBeenCalledTimes(1);
    expect(h.sendChatMessage).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveSend(sendResult);
      await pendingSend;
    });
  });
});
