import { describe, expect, it, beforeEach, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Agent, ChatSession } from "@multica/core/types";
import type { UploadResult } from "@multica/core/hooks/use-file-upload";
import enCommon from "../../locales/en/common.json";
import enChat from "../../locales/en/chat.json";
import enIssues from "../../locales/en/issues.json";

const mocks = vi.hoisted(() => {
  const chatState = {
    isOpen: true,
    isExpanded: false,
    activeSessionId: null as string | null,
    selectedAgentId: null as string | null,
    setOpen: vi.fn(),
    setActiveSession: vi.fn(),
    setSelectedAgentId: vi.fn(),
  };

  return {
    queryState: {
      agents: [] as unknown[],
      members: [] as unknown[],
      sessions: [] as unknown[],
      pendingTask: null as unknown,
      availability: "loading",
    },
    chatState,
    chatInput: {
      lastProps: null as null | Record<string, unknown>,
      fileCardsInserted: 0,
    },
    queryClient: {
      setQueryData: vi.fn(),
      invalidateQueries: vi.fn(),
      cancelQueries: vi.fn(),
    },
    createSession: vi.fn(),
    deleteSession: {
      mutate: vi.fn(),
      isPending: false,
    },
    markRead: {
      mutate: vi.fn(),
    },
    updateSession: {
      mutate: vi.fn(),
      isPending: false,
    },
    uploadWithToast: vi.fn(),
    fetchOlderMessages: vi.fn(),
  };
});

vi.mock("motion/react", () => ({
  motion: {
    div: ({ children, initial: _initial, animate: _animate, transition: _transition, ...props }: any) => (
      <div {...props}>{children}</div>
    ),
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  queryOptions: (options: unknown) => options,
  infiniteQueryOptions: (options: unknown) => options,
  useQueryClient: () => mocks.queryClient,
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = JSON.stringify(options.queryKey ?? []);
    if (key.includes('"agents"')) return { data: mocks.queryState.agents };
    if (key.includes('"members"')) return { data: mocks.queryState.members };
    if (key.includes('"sessions"')) return { data: mocks.queryState.sessions };
    if (key.includes('"pending-tasks"')) return { data: { tasks: [] } };
    if (key.includes('"pending-task"')) return { data: mocks.queryState.pendingTask };
    return { data: undefined };
  },
  useInfiniteQuery: () => ({
    data: {
      pages: [{
        messages: [],
        limit: 50,
        has_more: false,
        next_cursor: null,
      }],
      pageParams: [null],
    },
    isLoading: false,
    fetchNextPage: mocks.fetchOlderMessages,
    hasNextPage: false,
    isFetchingNextPage: false,
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/agents", () => ({
  useAgentPresenceDetail: () => "loading",
  useWorkspaceAgentAvailability: () => mocks.queryState.availability,
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({
    uploadWithToast: mocks.uploadWithToast,
  }),
}));

vi.mock("@multica/core/chat", () => ({
  useChatStore: (selector: (state: typeof mocks.chatState) => unknown) =>
    selector(mocks.chatState),
}));

vi.mock("@multica/core/chat/mutations", () => ({
  useCreateChatSession: () => ({
    mutateAsync: mocks.createSession,
  }),
  useDeleteChatSession: () => mocks.deleteSession,
  useMarkChatSessionRead: () => mocks.markRead,
  useUpdateChatSession: () => mocks.updateSession,
}));

vi.mock("@multica/views/issues/components", () => ({
  canAssignAgent: () => true,
}));

vi.mock("@multica/core/api", () => ({
  api: {
    cancelTaskById: vi.fn(),
  },
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

vi.mock("./chat-message-list", () => ({
  ChatMessageList: () => <div data-testid="chat-message-list" />,
  ChatMessageSkeleton: () => <div data-testid="chat-message-skeleton" />,
}));

vi.mock("./chat-resize-handles", () => ({
  ChatResizeHandles: () => null,
}));

vi.mock("./offline-banner", () => ({
  OfflineBanner: () => null,
}));

vi.mock("./no-agent-banner", () => ({
  NoAgentBanner: () => null,
}));

vi.mock("./use-chat-context-items", () => ({
  useChatContextItems: () => [],
}));

vi.mock("./use-chat-resize", () => ({
  useChatResize: () => ({
    renderWidth: 420,
    renderHeight: 520,
    isAtMax: false,
    boundsReady: true,
    isDragging: false,
    toggleExpand: vi.fn(),
    startDrag: vi.fn(),
  }),
}));

vi.mock("./chat-input", () => ({
  ChatInput: (props: {
    onUploadFile?: (file: File) => Promise<UploadResult | null>;
  }) => {
    mocks.chatInput.lastProps = props as unknown as Record<string, unknown>;
    return (
      <button
        type="button"
        data-testid="mock-chat-upload"
        disabled={!props.onUploadFile}
        onClick={() => {
          if (!props.onUploadFile) return;
          mocks.chatInput.fileCardsInserted += 1;
          void props.onUploadFile(
            new File(["pdf"], "brief.pdf", { type: "application/pdf" }),
          );
        }}
      >
        Upload
      </button>
    );
  },
}));

import { ChatWindow } from "./chat-window";

const TEST_RESOURCES = { en: { common: enCommon, chat: enChat, issues: enIssues } };

function makeAgent(overrides: Partial<Agent> & Pick<Agent, "id" | "name" | "owner_id">): Agent {
  return {
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "sonnet",
    skills: [],
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    archived_at: null,
    archived_by: null,
    ...overrides,
    id: overrides.id,
    name: overrides.name,
    owner_id: overrides.owner_id,
  };
}

function makeUpload(): UploadResult {
  return {
    id: "att-1",
    workspace_id: "ws-1",
    issue_id: null,
    comment_id: null,
    chat_session_id: "session-1",
    chat_message_id: null,
    uploader_type: "member",
    uploader_id: "user-1",
    filename: "brief.pdf",
    url: "https://cdn.example/brief.pdf",
    download_url: "https://cdn.example/brief.pdf",
    markdown_url: "/api/attachments/att-1/download",
    content_type: "application/pdf",
    size_bytes: 3,
    created_at: new Date(0).toISOString(),
    link: "https://cdn.example/brief.pdf",
    markdownLink: "/api/attachments/att-1/download",
  };
}

function makeSession(overrides: Partial<ChatSession> = {}): ChatSession {
  return {
    id: "session-1",
    workspace_id: "ws-1",
    agent_id: "agent-1",
    creator_id: "user-1",
    title: "",
    status: "active",
    has_unread: false,
    created_at: new Date(0).toISOString(),
    updated_at: new Date(0).toISOString(),
    ...overrides,
  };
}

function renderChatWindow() {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <ChatWindow />
    </I18nProvider>,
  );
}

beforeEach(() => {
  mocks.queryState.agents = [];
  mocks.queryState.members = [{ user_id: "user-1", role: "owner" }];
  mocks.queryState.sessions = [];
  mocks.queryState.pendingTask = null;
  mocks.queryState.availability = "loading";
  mocks.chatState.isOpen = true;
  mocks.chatState.isExpanded = false;
  mocks.chatState.activeSessionId = null;
  mocks.chatState.selectedAgentId = null;
  mocks.chatInput.lastProps = null;
  mocks.chatInput.fileCardsInserted = 0;
  mocks.queryClient.setQueryData.mockClear();
  mocks.queryClient.invalidateQueries.mockClear();
  mocks.queryClient.cancelQueries.mockClear();
  mocks.createSession.mockReset();
  mocks.createSession.mockResolvedValue(makeSession());
  mocks.deleteSession.mutate.mockClear();
  mocks.markRead.mutate.mockClear();
  mocks.updateSession.mutate.mockClear();
  mocks.uploadWithToast.mockReset();
  mocks.uploadWithToast.mockResolvedValue(makeUpload());
  mocks.fetchOlderMessages.mockClear();
  mocks.chatState.setOpen.mockClear();
  mocks.chatState.setActiveSession.mockClear();
  mocks.chatState.setSelectedAgentId.mockClear();
});

describe("ChatWindow upload readiness", () => {
  it("does not expose PDF upload while activeAgent is unavailable", async () => {
    renderChatWindow();

    const uploadButton = await screen.findByTestId("mock-chat-upload");
    expect(uploadButton).toBeDisabled();
    expect(mocks.chatInput.lastProps?.onUploadFile).toBeUndefined();

    fireEvent.click(uploadButton);

    expect(mocks.chatInput.fileCardsInserted).toBe(0);
    expect(mocks.createSession).not.toHaveBeenCalled();
    expect(mocks.uploadWithToast).not.toHaveBeenCalled();
  });

  it("keeps PDF upload available after an activeAgent resolves", async () => {
    const view = renderChatWindow();

    expect(await screen.findByTestId("mock-chat-upload")).toBeDisabled();

    mocks.queryState.agents = [
      makeAgent({ id: "agent-1", name: "Multica", owner_id: "user-1" }),
    ];
    mocks.queryState.availability = "available";
    view.rerender(
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <ChatWindow />
      </I18nProvider>,
    );

    const uploadButton = await screen.findByTestId("mock-chat-upload");
    await waitFor(() => {
      expect(uploadButton).not.toBeDisabled();
      expect(mocks.chatInput.lastProps?.onUploadFile).toEqual(expect.any(Function));
    });

    fireEvent.click(uploadButton);

    await waitFor(() => {
      expect(mocks.createSession).toHaveBeenCalledWith({
        agent_id: "agent-1",
        title: "",
      });
    });
    expect(mocks.uploadWithToast).toHaveBeenCalledWith(
      expect.any(File),
      { chatSessionId: "session-1" },
    );
    expect(mocks.chatState.setActiveSession).toHaveBeenCalledWith("session-1");
  });
});
