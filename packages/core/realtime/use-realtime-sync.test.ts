import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { chatKeys } from "../chat/queries";
import { issueKeys } from "../issues/queries";
import { workspaceKeys } from "../workspace/queries";
import type {
  ChatDonePayload,
  ChatMessage,
  ChatPendingTask,
  Workspace,
} from "../types";
import {
  applyChatDoneToCache,
  applyWorkspaceUpdatedToCache,
} from "./use-realtime-sync";

const sessionId = "session-1";
const taskId = "task-1";
const messagesKey = chatKeys.messages(sessionId);
const pendingKey = chatKeys.pendingTask(sessionId);

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

describe("applyWorkspaceUpdatedToCache", () => {
  const wsId = "ws-1";

  function workspace(overrides: Partial<Workspace> = {}): Workspace {
    return {
      id: wsId,
      name: "Test",
      slug: "test",
      description: null,
      context: null,
      settings: {},
      repos: [],
      issue_prefix: "TES",
      created_at: "2026-05-18T00:00:00Z",
      updated_at: "2026-05-18T00:00:00Z",
      ...overrides,
    };
  }

  it("invalidates issue cache when issue_prefix changes", () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [
      workspace({ issue_prefix: "TES" }),
    ]);
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    applyWorkspaceUpdatedToCache(qc, {
      workspace: workspace({ issue_prefix: "NEW" }),
    });

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: issueKeys.all(wsId),
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: workspaceKeys.list(),
    });
  });

  it("does not invalidate issue cache when only non-prefix fields change", () => {
    const qc = createQueryClient();
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [
      workspace({ issue_prefix: "TES", name: "Old name" }),
    ]);
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    applyWorkspaceUpdatedToCache(qc, {
      workspace: workspace({ issue_prefix: "TES", name: "New name" }),
    });

    expect(invalidate).not.toHaveBeenCalledWith({
      queryKey: issueKeys.all(wsId),
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: workspaceKeys.list(),
    });
  });

  it("invalidates issue cache when the workspace isn't in the cached list yet", () => {
    // Conservative: a workspace appearing for the first time may correspond
    // to issue queries that were primed without ever seeing the (possibly
    // changing) prefix. Erring on the side of refresh keeps identifiers
    // accurate at minimal cost.
    const qc = createQueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    applyWorkspaceUpdatedToCache(qc, {
      workspace: workspace({ issue_prefix: "NEW" }),
    });

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: issueKeys.all(wsId),
    });
  });
});
