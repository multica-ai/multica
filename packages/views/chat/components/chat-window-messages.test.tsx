import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AnimatePresence } from "motion/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { chatKeys } from "@multica/core/chat/queries";
import type { ChatMessage } from "@multica/core/types";
import { ChatWindowMessages } from "./chat-window-messages";

const fetchMessages = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", () => ({ api: { listChatMessagesPage: fetchMessages } }));
vi.mock("@multica/core/chat/mutations", () => ({
  useRegenerateChatQuickActions: () => ({ mutateAsync: vi.fn() }),
}));
const listProps = vi.hoisted(() => ({
  current: null as null | { revealKey?: string | null; onRevealed?: () => void },
}));
vi.mock("./chat-message-list", () => ({
  ChatMessageList: ({
    active,
    messages,
    revealKey,
    onRevealed,
  }: {
    active: boolean;
    messages: ChatMessage[];
    revealKey?: string | null;
    onRevealed?: () => void;
  }) => {
    listProps.current = { revealKey, onRevealed };
    return (
      <div data-testid="messages" data-active={active}>
        {messages.map((message) => <p key={message.id}>{message.content}</p>)}
      </div>
    );
  },
  ChatMessageSkeleton: () => <div data-testid="skeleton" />,
}));

function message(id: string): ChatMessage {
  return {
    id, chat_session_id: "session-1", role: "user", content: id,
    task_id: null, created_at: "2026-10-20T00:00:00Z",
  };
}

function pages(messages: ChatMessage[]) {
  return { pages: [{ messages, limit: 50, has_more: false }], pageParams: [null] };
}

function renderWindow(
  open: boolean,
  {
    sessionId = "session-1",
    cached = true,
    onReady,
  }: { sessionId?: string | null; cached?: boolean; onReady?: () => void } = {},
) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const key = chatKeys.messagesPage("session-1");
  if (cached) qc.setQueryData(key, pages([message("initial message")]));
  const content = (isOpen: boolean) => (
    <QueryClientProvider client={qc}>
      <AnimatePresence initial={false}>
        {isOpen && (
          <ChatWindowMessages
            sessionId={sessionId}
            pendingTask={undefined}
            pendingTaskLoading={false}
            availability={undefined}
            onQuickAction={vi.fn()}
            quickActionsDisabled={false}
            quickActionsPendingMessageId={null}
            onReady={onReady}
          >
            <div>empty</div>
          </ChatWindowMessages>
        )}
      </AnimatePresence>
    </QueryClientProvider>
  );
  const view = render(content(open));
  return {
    qc,
    key,
    setOpen: (isOpen: boolean) => view.rerender(content(isOpen)),
    observers: () => qc.getQueryCache().find({ queryKey: key })?.getObserversCount() ?? 0,
  };
}

beforeEach(() => {
  fetchMessages.mockReset();
  listProps.current = null;
});

// MUL-8417: the window fades in around this area, so it must say when the area
// has something to show rather than an empty, hidden list.
describe("floating chat message readiness", () => {
  it("is ready at once for the empty state of a new chat", () => {
    const onReady = vi.fn();
    renderWindow(true, { sessionId: null, onReady });
    expect(screen.getByText("empty")).toBeInTheDocument();
    expect(onReady).toHaveBeenCalled();
  });

  it("is ready at once for the skeleton of an uncached session", () => {
    fetchMessages.mockReturnValue(new Promise(() => {}));
    const onReady = vi.fn();
    renderWindow(true, { cached: false, onReady });
    expect(screen.getByTestId("skeleton")).toBeInTheDocument();
    expect(onReady).toHaveBeenCalled();
  });

  it("leaves a list's readiness to the list, named by its session", () => {
    const onReady = vi.fn();
    renderWindow(true, { onReady });
    expect(screen.getByTestId("messages")).toBeInTheDocument();
    expect(onReady).not.toHaveBeenCalled();
    expect(listProps.current?.revealKey).toBe("session-1");
    expect(listProps.current?.onRevealed).toBe(onReady);
  });

  it("does not report ready while the area is leaving", () => {
    const onReady = vi.fn();
    const { setOpen } = renderWindow(true, { sessionId: null, onReady });
    onReady.mockClear();
    setOpen(false);
    expect(onReady).not.toHaveBeenCalled();
  });
});

describe("floating chat message lifetime", () => {
  it("keeps message observers absent when the window starts closed", () => {
    const { observers } = renderWindow(false);
    expect(screen.queryByTestId("messages")).not.toBeInTheDocument();
    expect(observers()).toBe(0);
    expect(fetchMessages).not.toHaveBeenCalled();
  });

  it("follows immediately on open, stops during exit, then reads new data on reopen", async () => {
    const { qc, key, setOpen, observers } = renderWindow(true);
    expect(screen.getByTestId("messages")).toHaveAttribute("data-active", "true");
    expect(observers()).toBe(1);

    setOpen(false);
    expect(screen.getByTestId("messages")).toHaveAttribute("data-active", "false");
    await waitFor(() => expect(screen.queryByTestId("messages")).not.toBeInTheDocument());
    expect(observers()).toBe(0);

    act(() => qc.setQueryData(key, pages([message("arrived while closed")])));
    setOpen(true);
    expect(screen.getByText("arrived while closed")).toBeInTheDocument();
    expect(screen.getByTestId("messages")).toHaveAttribute("data-active", "true");
    expect(observers()).toBe(1);
  });

  it("cancels an interrupted exit without losing the live list", async () => {
    const { setOpen, observers } = renderWindow(true);
    const list = screen.getByTestId("messages");
    setOpen(false);
    setOpen(true);
    expect(screen.getByTestId("messages")).toBe(list);
    expect(list).toHaveAttribute("data-active", "true");
    await waitFor(() => expect(list.parentElement?.style.opacity).toBe("1"));
    expect(observers()).toBe(1);
  });

  it("fetches the full message window when its inactive cache was collected", async () => {
    const { qc, key, setOpen } = renderWindow(true);
    setOpen(false);
    await waitFor(() => expect(screen.queryByTestId("messages")).not.toBeInTheDocument());
    qc.removeQueries({ queryKey: key });
    fetchMessages.mockResolvedValue({ messages: [message("refetched after collection")], limit: 50, has_more: false });

    setOpen(true);

    expect(await screen.findByText("refetched after collection")).toBeInTheDocument();
    expect(fetchMessages).toHaveBeenCalledTimes(1);
  });
});
