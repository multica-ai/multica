import { describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import type {
  ChatDonePayload,
  ChatMessage,
  ChatQuickActionsPayload,
} from "@multica/core/types";

// chat-ws-updaters imports chatKeys from data/queries/chat, which transitively
// imports the native fetch client. Mock it so the Node test never loads RN
// modules — chatKeys itself is a pure key factory and needs nothing from api.
vi.mock("@/data/api", () => ({ api: {} }));

import {
  applyChatDoneToCache,
  applyChatQuickActionsToCache,
} from "./chat-ws-updaters";
import { chatKeys } from "@/data/queries/chat";

const SESSION = "session-1";

function donePayload(over: Partial<ChatDonePayload> = {}): ChatDonePayload {
  return {
    chat_session_id: SESSION,
    task_id: "task-1",
    message_id: "msg-1",
    content: "here is the chart",
    created_at: "2026-07-09T00:00:00Z",
    elapsed_ms: 1200,
    ...over,
  };
}

describe("applyChatDoneToCache", () => {
  it("patches the assistant bubble inline AND invalidates messages so bound attachments refetch", () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatMessage[]>(chatKeys.messages(SESSION), []);
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    applyChatDoneToCache(qc, donePayload());

    // Inline patch: the bubble lands immediately (no flicker) — but without
    // attachments, because the event payload never carries them.
    const msgs = qc.getQueryData<ChatMessage[]>(chatKeys.messages(SESSION));
    expect(msgs).toHaveLength(1);
    expect(msgs?.[0].id).toBe("msg-1");
    expect(msgs?.[0].attachments).toBeUndefined();

    // Refetch: the authoritative message list (with attachments) is pulled in.
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: chatKeys.messages(SESSION),
    });
    // pendingTask cleared so the status pill unmounts.
    expect(qc.getQueryData(chatKeys.pendingTask(SESSION))).toEqual({});
  });

  it("invalidates even when the payload lacks an inline message (legacy shape)", () => {
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    applyChatDoneToCache(
      qc,
      donePayload({ message_id: undefined, content: undefined, created_at: undefined }),
    );

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: chatKeys.messages(SESSION),
    });
  });

  it("carries quick actions into the instant mobile bubble", () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatMessage[]>(chatKeys.messages(SESSION), []);
    const quickActions = [
      { label: "Build a checklist", prompt: "Build the complete checklist", primary: true },
    ];

    applyChatDoneToCache(qc, donePayload({ quick_actions: quickActions }));

    expect(
      qc.getQueryData<ChatMessage[]>(chatKeys.messages(SESSION))?.[0]?.quick_actions,
    ).toEqual(quickActions);
  });

  it("does not duplicate an echoed message on reconnect", () => {
    const qc = new QueryClient();
    const existing: ChatMessage = {
      id: "msg-1",
      chat_session_id: SESSION,
      role: "assistant",
      content: "here is the chart",
      task_id: "task-1",
      created_at: "2026-07-09T00:00:00Z",
    };
    qc.setQueryData<ChatMessage[]>(chatKeys.messages(SESSION), [existing]);

    applyChatDoneToCache(qc, donePayload());

    expect(qc.getQueryData<ChatMessage[]>(chatKeys.messages(SESSION))).toHaveLength(1);
  });
});

function assistantMsg(over: Partial<ChatMessage> = {}): ChatMessage {
  return {
    id: "msg-1",
    chat_session_id: SESSION,
    role: "assistant",
    content: "here is the plan",
    task_id: "task-1",
    created_at: "2026-07-09T00:00:00Z",
    ...over,
  };
}

function quickActionsPayload(
  over: Partial<ChatQuickActionsPayload> = {},
): ChatQuickActionsPayload {
  return {
    chat_session_id: SESSION,
    task_id: "task-1",
    message_id: "msg-1",
    quick_actions: [
      { label: "Draft it", prompt: "Draft the complete plan", primary: true },
    ],
    ...over,
  };
}

describe("applyChatQuickActionsToCache", () => {
  it("patches the async supplement onto the targeted assistant message", () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatMessage[]>(chatKeys.messages(SESSION), [assistantMsg()]);

    applyChatQuickActionsToCache(qc, quickActionsPayload());

    expect(
      qc.getQueryData<ChatMessage[]>(chatKeys.messages(SESSION))?.[0]?.quick_actions,
    ).toEqual([
      { label: "Draft it", prompt: "Draft the complete plan", primary: true },
    ]);
  });

  it("only patches the message whose id matches, leaving others untouched", () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatMessage[]>(chatKeys.messages(SESSION), [
      assistantMsg({ id: "msg-0", content: "earlier turn" }),
      assistantMsg(),
    ]);

    applyChatQuickActionsToCache(qc, quickActionsPayload());

    const msgs = qc.getQueryData<ChatMessage[]>(chatKeys.messages(SESSION));
    expect(msgs?.[0]?.quick_actions).toBeUndefined();
    expect(msgs?.[1]?.quick_actions).toHaveLength(1);
  });

  it("empty supplement is a terminal no-op (no chips added)", () => {
    const qc = new QueryClient();
    qc.setQueryData<ChatMessage[]>(chatKeys.messages(SESSION), [assistantMsg()]);

    applyChatQuickActionsToCache(qc, quickActionsPayload({ quick_actions: [] }));

    expect(
      qc.getQueryData<ChatMessage[]>(chatKeys.messages(SESSION))?.[0]?.quick_actions,
    ).toBeUndefined();
  });
});
