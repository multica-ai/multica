import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useState, type ReactElement } from "react";
import { QueryClient, QueryClientProvider, useInfiniteQuery } from "@tanstack/react-query";
import { chatKeys, chatMessagesPageOptions } from "@multica/core/chat/queries";
import { useCommentDraftStore } from "@multica/core/issues/stores";
import type { ChatMessage, ChatMessagesPage } from "@multica/core/types";
import { toast } from "sonner";
import { renderWithI18n } from "../../test/i18n";
import { ChatMessageList } from "./chat-message-list";
import { useChatAnnotations } from "./use-chat-annotations";

vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));

const listChatMessagesPage = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", async (original) => {
  const actual = await original<typeof import("@multica/core/api")>();
  return { ...actual, api: { ...actual.api, listChatMessagesPage } };
});

// jsdom never lays out Virtuoso rows; render every row like the list tests do.
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({ data, itemContent, computeItemKey }: {
    data: unknown[];
    itemContent: (i: number, item: unknown) => ReactElement;
    computeItemKey: (i: number, item: unknown) => string;
  }) => <div>{data.map((item, i) => <div key={computeItemKey(i, item)}>{itemContent(i, item)}</div>)}</div>,
}));

vi.mock("../../editor/content-editor", async () => {
  const { forwardRef, useRef, useImperativeHandle } = await import("react");
  return { ContentEditor: forwardRef(function Editor({ defaultValue, ariaLabel }: {
    defaultValue?: string; ariaLabel?: string;
  }, ref) {
    const input = useRef<HTMLTextAreaElement>(null);
    useImperativeHandle(ref, () => ({
      getMarkdown: () => input.current?.value ?? "",
      focus: () => input.current?.focus(), hasActiveUploads: () => false,
    }));
    return <textarea ref={input} aria-label={ariaLabel} defaultValue={defaultValue} />;
  }) };
});

const SESSION = "session-1";
const key = `chat:${SESSION}` as const;

function assistant(id: string, content: string, createdAt: string): ChatMessage {
  return { id, chat_session_id: SESSION, role: "assistant", content, task_id: null, created_at: createdAt } as ChatMessage;
}

const newest: ChatMessagesPage = {
  messages: [assistant("msg-new", "Newest answer", "2026-11-10T02:00:00Z")],
  limit: 50,
  has_more: true,
  next_cursor: { created_at: "2026-11-10T02:00:00Z", id: "msg-new" },
};
const older: ChatMessagesPage = {
  messages: [assistant("msg-old", "Old answer text", "2026-11-10T01:00:00Z")],
  limit: 50,
  has_more: false,
};

function Harness() {
  const annotations = useChatAnnotations({ sessionId: SESSION, agentName: "Agent", enabled: true });
  const { data } = useInfiniteQuery(chatMessagesPageOptions(SESSION));
  const messages = [...(data?.pages ?? [])].reverse().flatMap((page) => page.messages);
  return <>
    <ChatMessageList messages={messages} pendingTask={null} availability="online" annotations={annotations} />
    {annotations.composer?.render()}
    <button onClick={() => annotations.editAnnotation("ann-old", true)}>Open saved annotation</button>
  </>;
}

function renderHarness() {
  function Root() {
    const [client] = useState(() => {
      const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      qc.setQueryData(chatKeys.messagesPage(SESSION), { pages: [newest], pageParams: [null] });
      return qc;
    });
    return <QueryClientProvider client={client}><Harness /></QueryClientProvider>;
  }
  return renderWithI18n(<Root />);
}

describe("useChatAnnotations", () => {
  beforeEach(() => {
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(10, 10, 600, 400));
    Range.prototype.getBoundingClientRect = vi.fn(() => new DOMRect(10, 10, 120, 20));
    Element.prototype.scrollIntoView = vi.fn();
    listChatMessagesPage.mockReset();
    vi.mocked(toast.error).mockClear();
    // A restored draft: the quoted reply is not in the newest page.
    useCommentDraftStore.setState({ drafts: {} });
    useCommentDraftStore.getState().addAnnotation(key, {
      id: "ann-old", sourceCommentId: "msg-old", sourceActorName: "Agent",
      quote: "Old answer", note: "Saved note", start: 0, prefix: "", suffix: " text",
    });
  });
  afterEach(() => vi.restoreAllMocks());

  it("loads the older page holding a restored annotation's source, then opens its note", async () => {
    listChatMessagesPage.mockResolvedValue(older);
    renderHarness();
    expect(await screen.findByText("Newest answer")).toBeInTheDocument();
    expect(screen.queryByText("Old answer text")).not.toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Open saved annotation" }));

    // jsdom gives the list's scroll container a zero clip rect, so the popup
    // stays visibility:hidden and has no accessible name; query by label.
    expect(await screen.findByLabelText("Comment")).toHaveValue("Saved note");
    expect(listChatMessagesPage).toHaveBeenCalledTimes(1);
    expect(listChatMessagesPage).toHaveBeenCalledWith(SESSION, expect.objectContaining({ before: newest.next_cursor }));
    expect(screen.getByText("Old answer text")).toBeInTheDocument();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("reports the source as unavailable once history runs out without it", async () => {
    listChatMessagesPage.mockResolvedValue({ ...older, messages: [assistant("msg-other", "Other", "2026-11-10T01:00:00Z")] });
    renderHarness();
    await screen.findByText("Newest answer");

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "Open saved annotation" }));
    });

    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
    expect(screen.queryByLabelText("Comment")).not.toBeInTheDocument();
  });
});
