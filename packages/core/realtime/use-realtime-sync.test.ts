import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { chatKeys } from "../chat/queries";
import {
  agentActivityKeys,
  agentRunCountsKeys,
  agentTaskSnapshotKeys,
  agentTasksKeys,
} from "../agents/queries";
import { inboxKeys } from "../inbox/queries";
import type { ChatDonePayload, ChatMessage, ChatPendingTask } from "../types";
import {
  applyChatDoneToCache,
  invalidateTaskLifecycleQueries,
} from "./use-realtime-sync";

const sessionId = "session-1";
const taskId = "task-1";
const messagesKey = chatKeys.messages(sessionId);
const pendingKey = chatKeys.pendingTask(sessionId);
const wsId = "ws-1";

function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
    },
  });
}

function userMessage(): ChatMessage {
  return {
    id: "msg-user",
    chat_session_id: sessionId,
    role: "user",
    content: "hello",
    task_id: null,
    created_at: "2026-05-13T05:00:00Z",
    responded_at: null,
  };
}

function donePayload(overrides: Partial<ChatDonePayload> = {}): ChatDonePayload {
  return {
    chat_session_id: sessionId,
    task_id: taskId,
    message_id: "msg-assistant",
    content: "done",
    elapsed_ms: 1234,
    created_at: "2026-05-13T05:00:02Z",
    ...overrides,
  };
}

describe("applyChatDoneToCache", () => {
  it("writes the assistant message before clearing pending task", () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatMessage[]>(messagesKey, [userMessage()]);
    qc.setQueryData<ChatPendingTask>(pendingKey, {
      task_id: taskId,
      status: "running",
    });

    const setQueryData = vi.spyOn(qc, "setQueryData");

    applyChatDoneToCache(qc, donePayload());

    expect(setQueryData.mock.calls[0]?.[0]).toEqual(messagesKey);
    expect(setQueryData.mock.calls[1]?.[0]).toEqual(pendingKey);
    expect(qc.getQueryData<ChatPendingTask>(pendingKey)).toEqual({});
    expect(qc.getQueryData<ChatMessage[]>(messagesKey)).toEqual([
      userMessage(),
      {
        id: "msg-assistant",
        chat_session_id: sessionId,
        role: "assistant",
        content: "done",
        task_id: taskId,
        created_at: "2026-05-13T05:00:02Z",
        responded_at: null,
        elapsed_ms: 1234,
      },
    ]);
  });

  it("does not duplicate a replayed chat done event", () => {
    const qc = createQueryClient();
    const assistant: ChatMessage = {
      id: "msg-assistant",
      chat_session_id: sessionId,
      role: "assistant",
      content: "done",
      task_id: taskId,
      created_at: "2026-05-13T05:00:02Z",
      responded_at: null,
      elapsed_ms: 1234,
    };
    qc.setQueryData<ChatMessage[]>(messagesKey, [userMessage(), assistant]);
    qc.setQueryData<ChatPendingTask>(pendingKey, {
      task_id: taskId,
      status: "running",
    });

    applyChatDoneToCache(qc, donePayload());

    expect(qc.getQueryData<ChatMessage[]>(messagesKey)).toEqual([
      userMessage(),
      assistant,
    ]);
    expect(qc.getQueryData<ChatPendingTask>(pendingKey)).toEqual({});
  });

  it("falls back to invalidation-only when older servers omit message fields", () => {
    const qc = createQueryClient();
    qc.setQueryData<ChatMessage[]>(messagesKey, [userMessage()]);
    qc.setQueryData<ChatPendingTask>(pendingKey, {
      task_id: taskId,
      status: "running",
    });

    applyChatDoneToCache(
      qc,
      donePayload({ message_id: undefined, content: undefined }),
    );

    expect(qc.getQueryData<ChatMessage[]>(messagesKey)).toEqual([
      userMessage(),
    ]);
    expect(qc.getQueryData<ChatPendingTask>(pendingKey)).toEqual({});
  });
});

describe("invalidateTaskLifecycleQueries", () => {
  it("invalidates the inbox active issue tasks query with other task lifecycle caches", () => {
    const qc = createQueryClient();

    for (const key of [
      agentTaskSnapshotKeys.list(wsId),
      agentActivityKeys.last30d(wsId),
      agentRunCountsKeys.last30d(wsId),
      agentTasksKeys.all(wsId),
      ["issues", "tasks"] as const,
      inboxKeys.activeIssueTasks(wsId),
    ]) {
      qc.setQueryData(key, []);
    }

    invalidateTaskLifecycleQueries(qc, wsId);

    expect(qc.getQueryState(agentTaskSnapshotKeys.list(wsId))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(agentActivityKeys.last30d(wsId))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(agentRunCountsKeys.last30d(wsId))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(agentTasksKeys.all(wsId))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(["issues", "tasks"])?.isInvalidated).toBe(true);
    expect(qc.getQueryState(inboxKeys.activeIssueTasks(wsId))?.isInvalidated).toBe(true);
  });
});
