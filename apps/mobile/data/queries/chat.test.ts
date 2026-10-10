// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// QueryObserver's native timer path is enabled in a mobile runtime. Supply the
// same non-server environment without loading RN or rendering a DOM.
vi.hoisted(() => { vi.stubGlobal("window", {}); });
vi.mock("@/data/api", () => ({
  api: { getPendingChatTask: vi.fn(), listChatMessages: vi.fn() },
}));

import { focusManager, onlineManager, QueryClient, QueryObserver } from "@tanstack/react-query";
import { api } from "@/data/api";
import { chatKeys, chatMessagesOptions, pendingChatTaskOptions } from "./chat";
import { beginChatSend } from "@/data/chat-send-lifecycle";
import { seedAcceptedPendingTask } from "@/data/realtime/chat-ws-updaters";
import type { ChatMessage, ChatPendingTask } from "@multica/core/types";

const TASK = "00000000-0000-4000-8000-000000000001";
const NEXT = "00000000-0000-4000-8000-000000000002";

describe("chat snapshot recovery", () => {
  let qc: QueryClient;
  const cleanup: Array<() => void> = [];

  beforeEach(() => {
    vi.useFakeTimers();
    vi.mocked(api.getPendingChatTask).mockReset();
    vi.mocked(api.listChatMessages).mockReset();
    qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
    qc.mount();
    focusManager.setFocused(true);
    onlineManager.setOnline(true);
  });

  afterEach(() => {
    cleanup.splice(0).forEach((unsub) => unsub());
    qc.unmount();
    qc.clear();
    focusManager.setFocused(undefined);
    vi.useRealTimers();
  });

  it("rechecks warm messages and pending state on foreground despite Infinity staleTime", async () => {
    qc.setQueryData(chatKeys.messages("A"), []);
    qc.setQueryData(chatKeys.pendingTask("A"), { task_id: TASK });
    vi.mocked(api.listChatMessages).mockResolvedValue([]);
    vi.mocked(api.getPendingChatTask).mockResolvedValue({ task_id: TASK });
    const messages = new QueryObserver(qc, chatMessagesOptions("A"));
    const pending = new QueryObserver(qc, pendingChatTaskOptions("A"));
    cleanup.push(messages.subscribe(() => {}), pending.subscribe(() => {}));
    await vi.advanceTimersByTimeAsync(0);
    vi.mocked(api.listChatMessages).mockClear();
    vi.mocked(api.getPendingChatTask).mockClear();

    focusManager.setFocused(false);
    focusManager.setFocused(true);
    await vi.advanceTimersByTimeAsync(0);

    expect(api.listChatMessages).toHaveBeenCalledTimes(1);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(1);
  });

  it("polls only an observed pending task and fetches the lost final reply when it finishes", async () => {
    qc.setQueryData(chatKeys.pendingTask("A"), { task_id: TASK });
    qc.setQueryData(chatKeys.messages("A"), []);
    vi.mocked(api.getPendingChatTask)
      .mockResolvedValueOnce({ task_id: TASK })
      .mockResolvedValue({});
    const pending = new QueryObserver(qc, pendingChatTaskOptions("A"));
    cleanup.push(pending.subscribe(() => {}));
    await vi.advanceTimersByTimeAsync(0);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(2);
    expect(qc.getQueryData(chatKeys.pendingTask("A"))).toEqual({});
    expect(qc.getQueryState(chatKeys.messages("A"))?.isInvalidated).toBe(true);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(2);
  });

  it("rechecks a warm pending snapshot when connectivity returns without a WS event", async () => {
    qc.setQueryData(chatKeys.pendingTask("A"), { task_id: TASK });
    vi.mocked(api.getPendingChatTask).mockResolvedValue({ task_id: TASK });
    const pending = new QueryObserver(qc, pendingChatTaskOptions("A"));
    cleanup.push(pending.subscribe(() => {}));
    await vi.advanceTimersByTimeAsync(0);
    vi.mocked(api.getPendingChatTask).mockClear();
    onlineManager.setOnline(false);
    vi.mocked(api.getPendingChatTask).mockResolvedValue({});
    onlineManager.setOnline(true);
    await vi.advanceTimersByTimeAsync(0);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(1);
    expect(qc.getQueryData(chatKeys.pendingTask("A"))).toEqual({});
  });

  it("does not poll in the background or after the session observer leaves", async () => {
    vi.mocked(api.getPendingChatTask).mockResolvedValue({ task_id: TASK });
    const pending = new QueryObserver(qc, pendingChatTaskOptions("A"));
    const unsub = pending.subscribe(() => {});
    cleanup.push(unsub);
    await vi.advanceTimersByTimeAsync(0);
    focusManager.setFocused(false);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(1);
    unsub();
    focusManager.setFocused(true);
    await vi.advanceTimersByTimeAsync(60_000);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(1);
  });

  it("recovers messages when the server advances to a queued successor", async () => {
    qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask("A"), {
      task_id: TASK, queued_tasks: [{ task_id: NEXT, status: "queued", created_at: "2026-10-06T00:00:00Z" }],
    });
    qc.setQueryData(chatKeys.messages("A"), []);
    vi.mocked(api.getPendingChatTask).mockResolvedValue({ task_id: NEXT, status: "running" });
    const pending = new QueryObserver(qc, pendingChatTaskOptions("A"));
    cleanup.push(pending.subscribe(() => {}));
    await vi.advanceTimersByTimeAsync(0);
    expect(qc.getQueryData<ChatPendingTask>(chatKeys.pendingTask("A"))?.task_id).toBe(NEXT);
    expect(qc.getQueryState(chatKeys.messages("A"))?.isInvalidated).toBe(true);
  });

  it("keeps retrying a visible pending task after a snapshot request fails", async () => {
    qc.setQueryData(chatKeys.pendingTask("A"), { task_id: TASK });
    vi.mocked(api.getPendingChatTask).mockRejectedValueOnce(new Error("offline")).mockResolvedValue({});
    const pending = new QueryObserver(qc, pendingChatTaskOptions("A"));
    cleanup.push(pending.subscribe(() => {}));
    await vi.advanceTimersByTimeAsync(0);
    expect(qc.getQueryData(chatKeys.pendingTask("A"))).toEqual({ task_id: TASK });
    await vi.advanceTimersByTimeAsync(30_000);
    expect(qc.getQueryData(chatKeys.pendingTask("A"))).toEqual({});
  });

  it("keeps a new session's unacknowledged send visible across mount, focus and connectivity recovery", async () => {
    const finishSend = beginChatSend("A");
    cleanup.push(finishSend);
    const optimistic: ChatMessage = {
      id: "optimistic-message", chat_session_id: "A", role: "user", content: "first message",
      created_at: "2026-10-06T00:00:00Z", task_id: null,
    };
    qc.setQueryData(chatKeys.messages("A"), [optimistic]);
    qc.setQueryData(chatKeys.pendingTask("A"), { task_id: "optimistic-task", status: "queued" });
    vi.mocked(api.listChatMessages).mockResolvedValue([]);
    vi.mocked(api.getPendingChatTask).mockResolvedValue({});
    const messages = new QueryObserver(qc, chatMessagesOptions("A"));
    const pending = new QueryObserver(qc, pendingChatTaskOptions("A"));
    cleanup.push(messages.subscribe(() => {}), pending.subscribe(() => {}));
    focusManager.setFocused(false);
    focusManager.setFocused(true);
    onlineManager.setOnline(false);
    onlineManager.setOnline(true);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(api.listChatMessages).not.toHaveBeenCalled();
    expect(api.getPendingChatTask).not.toHaveBeenCalled();
    expect(qc.getQueryData(chatKeys.messages("A"))).toEqual([optimistic]);
    expect(qc.getQueryData<ChatPendingTask>(chatKeys.pendingTask("A"))?.task_id).toBe("optimistic-task");

    // The existing send response replaces the placeholders, then invalidates.
    finishSend();
    const accepted = { ...optimistic, id: "server-message", task_id: TASK };
    qc.setQueryData(chatKeys.messages("A"), [accepted]);
    qc.setQueryData(chatKeys.pendingTask("A"), { task_id: TASK, status: "running" });
    vi.mocked(api.listChatMessages).mockResolvedValue([accepted]);
    vi.mocked(api.getPendingChatTask).mockResolvedValue({ task_id: TASK, status: "running" });
    await qc.invalidateQueries({ queryKey: chatKeys.messages("A") });
    await qc.invalidateQueries({ queryKey: chatKeys.pendingTask("A") });
    expect(api.listChatMessages).toHaveBeenCalledTimes(1);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(2);
  });

  it("does not interpret a local placeholder as a completed server task", async () => {
    const finishSend = beginChatSend("A");
    cleanup.push(finishSend);
    qc.setQueryData(chatKeys.pendingTask("A"), { task_id: "optimistic-task" });
    qc.setQueryData(chatKeys.messages("A"), []);
    vi.mocked(api.getPendingChatTask).mockResolvedValue({});
    await qc.fetchQuery({ ...pendingChatTaskOptions("A"), staleTime: 0 });
    expect(qc.getQueryState(chatKeys.messages("A"))?.isInvalidated).toBe(false);
  });

  it("recovers another device's user message when a server task appears", async () => {
    qc.setQueryData(chatKeys.pendingTask("A"), {});
    qc.setQueryData(chatKeys.messages("A"), []);
    vi.mocked(api.getPendingChatTask).mockResolvedValue({ task_id: TASK });
    await qc.fetchQuery({ ...pendingChatTaskOptions("A"), staleTime: 0 });
    expect(qc.getQueryState(chatKeys.messages("A"))?.isInvalidated).toBe(true);
  });

  it("rechecks an authoritative queue-only snapshot until the queue is drained", async () => {
    qc.setQueryData<ChatPendingTask>(chatKeys.pendingTask("A"), {
      queued_tasks: [{ task_id: NEXT, status: "queued", created_at: "2026-10-06T00:00:00Z" }],
    });
    qc.setQueryData(chatKeys.messages("A"), []);
    vi.mocked(api.getPendingChatTask).mockRejectedValueOnce(new Error("offline")).mockResolvedValue({});
    const pending = new QueryObserver(qc, pendingChatTaskOptions("A"));
    cleanup.push(pending.subscribe(() => {}));
    await vi.advanceTimersByTimeAsync(0);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(2);
    expect(qc.getQueryState(chatKeys.messages("A"))?.isInvalidated).toBe(true);
    await vi.advanceTimersByTimeAsync(30_000);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(2);
  });

  it("recovers an accepted queued send after two snapshot failures even if its placeholder remains", async () => {
    const finishSend = beginChatSend("A");
    cleanup.push(finishSend);
    qc.setQueryData(chatKeys.pendingTask("A"), {
      task_id: "optimistic-task", status: "queued", created_at: "2026-10-06T00:00:00Z",
    });
    seedAcceptedPendingTask(qc, {
      chat_session_id: "A", task_id: NEXT, queued: true, supports_queue: true,
      optimistic_task_id: "optimistic-task", created_at: "2026-10-06T00:00:00Z",
    });
    // Server accepted a follow-up to a predecessor this cache has not loaded.
    expect(qc.getQueryData<ChatPendingTask>(chatKeys.pendingTask("A"))?.task_id).toBe("optimistic-task");
    finishSend();
    vi.mocked(api.getPendingChatTask)
      .mockRejectedValueOnce(new Error("4G request failed"))
      .mockRejectedValueOnce(new Error("4G retry failed"))
      .mockResolvedValue({ task_id: TASK, queued_tasks: [{ task_id: NEXT, status: "queued", created_at: "2026-10-06T00:00:00Z" }] });
    const pending = new QueryObserver(qc, { ...pendingChatTaskOptions("A"), retry: 1, retryDelay: 0 });
    cleanup.push(pending.subscribe(() => {}));
    await vi.advanceTimersByTimeAsync(0);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(2);
    expect(qc.getQueryData<ChatPendingTask>(chatKeys.pendingTask("A"))?.task_id).toBe("optimistic-task");
    await vi.advanceTimersByTimeAsync(30_000);
    expect(api.getPendingChatTask).toHaveBeenCalledTimes(3);
    expect(qc.getQueryData<ChatPendingTask>(chatKeys.pendingTask("A"))?.task_id).toBe(TASK);
  });
});
