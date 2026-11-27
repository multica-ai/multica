// @vitest-environment jsdom
import { InfiniteQueryObserver, QueryClient, QueryObserver } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import type { ChatPendingTask } from "../types/chat";
import { chatMessagesPageOptions, pendingChatTaskOptions } from "./queries";

vi.mock("../api", () => ({
  api: { getPendingChatTask: vi.fn(), listChatMessagesPage: vi.fn() },
}));

const sessionId = "chat-session";
const first = { task_id: "first-turn", status: "running" };
const second = { task_id: "second-turn", status: "running" };
const prompt = {
  id: "prompt",
  chat_session_id: sessionId,
  task_id: first.task_id,
  role: "user" as const,
  content: "Hello",
  created_at: "2026-11-26T00:00:00Z",
};
const reply = {
  ...prompt,
  id: "reply",
  role: "assistant" as const,
  content: "Saved reply",
};
const pendingOptions = pendingChatTaskOptions(sessionId);
const historyOptions = chatMessagesPageOptions(sessionId);

describe("pending chat recovery without realtime events", () => {
  let client: QueryClient;
  let unsubscribe: Array<() => void>;

  beforeEach(() => {
    vi.useFakeTimers();
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    unsubscribe = [];
    vi.mocked(api.getPendingChatTask).mockResolvedValue(first);
    vi.mocked(api.listChatMessagesPage).mockResolvedValue({
      messages: [prompt], limit: 50, has_more: false,
    });
  });

  afterEach(() => {
    unsubscribe.forEach((stop) => stop());
    client.clear();
    vi.resetAllMocks();
    vi.useRealTimers();
  });

  function openChat() {
    const history = new InfiniteQueryObserver(client, historyOptions);
    const pending = new QueryObserver(client, pendingOptions);
    unsubscribe.push(history.subscribe(() => {}), pending.subscribe(() => {}));
    return { history, pending };
  }

  it("recovers the saved reply and stops polling after the pending turn finishes", async () => {
    const { history, pending } = openChat();
    await vi.advanceTimersByTimeAsync(10_000);
    expect(pending.getCurrentResult().data).toEqual(first);
    expect(history.getCurrentResult().data?.pages[0]?.messages).toEqual([prompt]);
    expect(api.listChatMessagesPage).toHaveBeenCalledTimes(1);

    // Both completion frames are lost; only the server snapshots advance.
    vi.mocked(api.getPendingChatTask).mockResolvedValue({});
    vi.mocked(api.listChatMessagesPage).mockResolvedValue({
      messages: [prompt, reply], limit: 50, has_more: false,
    });
    await vi.advanceTimersByTimeAsync(10_000);

    expect(history.getCurrentResult().data?.pages[0]?.messages).toEqual([prompt, reply]);
    expect(pending.getCurrentResult().data?.task_id).toBeUndefined();
    await vi.advanceTimersByTimeAsync(20_000);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(3);
    expect(api.listChatMessagesPage).toHaveBeenCalledTimes(2);
  });

  it("recovers the previous reply when the server advances to a new turn", async () => {
    const { history, pending } = openChat();
    await vi.advanceTimersByTimeAsync(0);
    vi.mocked(api.getPendingChatTask).mockResolvedValue(second);
    vi.mocked(api.listChatMessagesPage).mockResolvedValue({
      messages: [prompt, reply], limit: 50, has_more: false,
    });
    await vi.advanceTimersByTimeAsync(10_000);

    expect(history.getCurrentResult().data?.pages[0]?.messages).toEqual([prompt, reply]);
    expect(pending.getCurrentResult().data).toEqual(second);
  });

  it("keeps a newer turn accepted while an older pending request is in flight", async () => {
    const { pending } = openChat();
    await vi.advanceTimersByTimeAsync(0);
    let resolvePending!: (value: ChatPendingTask) => void;
    vi.mocked(api.getPendingChatTask).mockReturnValueOnce(new Promise((resolve) => {
      resolvePending = resolve;
    }));
    await vi.advanceTimersByTimeAsync(10_000);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(2);

    client.setQueryData(pendingOptions.queryKey, second);
    resolvePending({});
    await vi.advanceTimersByTimeAsync(0);

    expect(pending.getCurrentResult().data).toEqual(second);
  });

  it("retries recovery if the saved history cannot be loaded", async () => {
    const { history, pending } = openChat();
    await vi.advanceTimersByTimeAsync(0);
    vi.mocked(api.getPendingChatTask).mockResolvedValue({});
    vi.mocked(api.listChatMessagesPage)
      .mockRejectedValueOnce(new Error("History temporarily unavailable"))
      .mockResolvedValue({ messages: [prompt, reply], limit: 50, has_more: false });

    await vi.advanceTimersByTimeAsync(10_000);
    expect(pending.getCurrentResult().data).toEqual(first);

    await vi.advanceTimersByTimeAsync(10_000);
    expect(history.getCurrentResult().data?.pages[0]?.messages).toEqual([prompt, reply]);
    expect(pending.getCurrentResult().data?.task_id).toBeUndefined();
  });
});
