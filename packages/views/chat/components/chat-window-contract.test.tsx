// @vitest-environment jsdom

import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({ isOpen: false, reducedMotion: false }));
const noop = vi.hoisted(() => vi.fn());
const store = vi.hoisted(() => ({
  isOpen: false,
  activeSessionId: null,
  selectedAgentId: null,
  selectedProjectId: null,
  isExpanded: false,
  setOpen: noop,
  setActiveSession: noop,
  setSelectedAgentId: noop,
  setSelectedProjectId: noop,
}));

vi.mock("motion/react", () => ({
  useReducedMotion: () => state.reducedMotion,
  motion: {
    div: ({
      children,
      initial,
      animate,
      transition,
      ...props
    }: React.ComponentProps<"div"> & {
      initial: unknown;
      animate: unknown;
      transition: unknown;
    }) => (
      <div
        {...props}
        data-testid="chat-window"
        data-initial={JSON.stringify(initial)}
        data-animate={JSON.stringify(animate)}
        data-transition={JSON.stringify(transition)}
      >
        {children}
      </div>
    ),
  },
}));

vi.mock("@multica/core/chat", () => ({
  useChatStore: Object.assign(
    (selector: (value: typeof store) => unknown) => selector({ ...store, isOpen: state.isOpen }),
    { getState: () => ({ ...store, isOpen: state.isOpen }) },
  ),
}));
vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: [], isSuccess: true }),
  useInfiniteQuery: () => ({
    data: { pages: [] },
    isLoading: false,
    fetchNextPage: noop,
    hasNextPage: false,
    isFetchingNextPage: false,
  }),
  useQueryClient: () => ({ getQueryData: () => null, setQueryData: noop }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-a" }));
vi.mock("@multica/core/auth", () => ({ useAuthStore: (selector: (s: { user: null }) => unknown) => selector({ user: null }) }));
vi.mock("@multica/core/workspace/queries", () => ({ agentListOptions: noop, memberListOptions: noop }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: noop }));
vi.mock("@multica/views/issues/components", () => ({ canAssignAgent: () => true }));
vi.mock("@multica/core/api", () => ({ api: {}, dispatchReasonCode: noop }));
vi.mock("@multica/core/agents", () => ({
  isAgentRuntimeBound: () => true,
  useAgentPresenceDetail: () => "loading",
  useWorkspaceAgentAvailability: () => "none",
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("../../common/use-app-foreground", () => ({ useAppForeground: () => true }));
vi.mock("../../common/row-actions-menu", () => ({ RowActionsMenu: () => null, handleRowActivationKey: noop }));
vi.mock("../../issues/components/pickers/property-picker", () => ({
  PickerEmpty: () => null,
  PickerItem: () => null,
  PickerSection: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PropertyPicker: ({ children }: { children?: React.ReactNode }) => <>{children}</>,
}));
vi.mock("../../editor/extensions/pinyin-match", () => ({ matchesPinyin: () => false }));
vi.mock("./offline-banner", () => ({ OfflineBanner: () => null }));
vi.mock("./no-agent-banner", () => ({ NoAgentBanner: () => null }));
vi.mock("./archived-agent-banner", () => ({ ArchivedAgentBanner: () => null }));
vi.mock("./runtime-required-banner", () => ({ RuntimeRequiredBanner: () => null }));
vi.mock("@multica/core/chat/queries", () => ({
  chatSessionsOptions: noop,
  chatMessagesPageOptions: noop,
  pendingChatTaskOptions: noop,
  pendingChatTasksOptions: noop,
  chatKeys: { messages: () => [] },
  isTaskMessageTaskId: () => false,
  chatQuickActionsPendingOptions: noop,
}));
vi.mock("@multica/core/chat/mutations", () => ({
  useCreateChatSession: () => ({ mutateAsync: noop }),
  useMarkChatSessionRead: () => ({ mutate: noop }),
  useRegenerateChatQuickActions: () => ({ mutateAsync: noop }),
  useSetChatSessionArchived: () => ({ mutate: noop }),
  useSetChatSessionProject: () => ({ mutate: noop, isPending: false }),
  useUpdateChatSession: () => ({ mutate: noop }),
}));
vi.mock("@multica/core/chat/message-cache", () => ({ upsertChatMessageToCaches: noop }));
vi.mock("@multica/core/chat/use-quick-actions-pending-timeout", () => ({ useQuickActionsPendingTimeout: noop }));
vi.mock("./use-quick-actions-failure-toast", () => ({ useQuickActionsFailureToast: noop }));
vi.mock("@multica/core/chat/pending", () => ({ hideQueuedChatMessages: (messages: unknown[]) => messages }));
vi.mock("@multica/core/realtime", () => ({ removeChatMessageFromCaches: noop }));
vi.mock("./use-chat-draft-restore", () => ({
  useChatDraftRestore: () => ({ restoreDraftRequest: null, enqueueLocalRestore: noop, handleRestoreDraftApplied: noop }),
}));
vi.mock("./use-chat-task-actions", () => ({
  useChatTaskActions: () => ({
    cancelChatTask: noop,
    handleEditQueuedTask: noop,
    handleRemoveQueuedTask: noop,
    handleClearQueuedTasks: noop,
    handleSendQueuedTaskNow: noop,
  }),
}));
vi.mock("./chat-message-list", () => ({ ChatMessageList: () => null, ChatMessageSkeleton: () => null }));
vi.mock("./chat-input", () => ({ ChatInput: () => <textarea aria-label="Composer" /> }));
vi.mock("./chat-queue", () => ({ ChatQueue: () => null }));
vi.mock("./chat-resize-handles", () => ({ ChatResizeHandles: () => null }));
vi.mock("./use-chat-context-items", () => ({ useChatContextItems: () => [] }));
vi.mock("./use-chat-resize", () => ({
  useChatResize: () => ({
    renderWidth: 420,
    renderHeight: 600,
    isAtMax: false,
    boundsReady: true,
    isDragging: false,
    toggleExpand: noop,
    startDrag: noop,
  }),
}));
vi.mock("./use-visual-viewport-keyboard", () => ({ useVisualViewportKeyboard: () => null }));
vi.mock("@multica/ui/hooks/use-mobile", () => ({ useIsMobile: () => false }));
vi.mock("./use-chat-controller", () => ({
  hasInFlightPendingTask: () => false,
  isStillOnComposeTarget: () => true,
  planProjectContextChange: () => ({ kind: "setDraftProject", projectId: null }),
  seedAcceptedPendingTask: noop,
}));
vi.mock("./use-chat-project-context-support", () => ({ useChatProjectContextSupport: () => true }));
vi.mock("@multica/core/logger", () => ({ createLogger: () => ({ info: noop, warn: noop }) }));
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "Chat" }) }));
vi.mock("sonner", () => ({ toast: { error: noop } }));
vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({ children, ...props }: React.ComponentProps<"button">) => <button {...props}>{children}</button>,
}));
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children, render }: { children?: React.ReactNode; render?: React.ReactElement }) => render ?? <button>{children}</button>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <span>{children}</span>,
}));
vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ children, render }: { children?: React.ReactNode; render?: React.ReactElement }) => render ?? <button>{children}</button>,
  PopoverContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));

import { ChatWindow } from "./chat-window";

beforeEach(() => {
  state.isOpen = false;
  state.reducedMotion = false;
});
afterEach(cleanup);

describe("ChatWindow presentation contract", () => {
  it("removes collapsed content from focus and the accessibility tree", () => {
    render(<ChatWindow />);

    const window = screen.getByTestId("chat-window");
    expect(window.getAttribute("aria-hidden")).toBe("true");
    expect(window.hasAttribute("inert")).toBe(true);
  });

  it("does not scale or animate when reduced motion is requested", () => {
    state.reducedMotion = true;
    state.isOpen = true;
    render(<ChatWindow />);

    const window = screen.getByTestId("chat-window");
    expect(JSON.parse(window.dataset.initial ?? "{}").scale).toBe(1);
    expect(JSON.parse(window.dataset.animate ?? "{}").scale).toBe(1);
    expect(JSON.parse(window.dataset.transition ?? "{}")).toEqual({ duration: 0 });
  });
});
