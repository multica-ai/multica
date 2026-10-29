// @vitest-environment node
import { describe, expect, it } from "vitest";
import { QueryClient, type InfiniteData } from "@tanstack/react-query";
import {
  conversationKeys,
  directCounterpart,
  findDirectConversation,
  flattenConversationMessages,
  markConversationReadInCache,
  OPTIMISTIC_SEQ,
  upsertConversationMessage,
} from "./queries";
import type { Conversation, ConversationMessage, ConversationMessagesPage } from "../types";

const msg = (seq: number, extra: Partial<ConversationMessage> = {}): ConversationMessage => ({
  id: `m${seq}`,
  conversation_id: "c1",
  seq,
  author_type: "member",
  author_id: "u1",
  kind: "message",
  content: `message ${seq}`,
  task_id: null,
  client_message_id: null,
  created_at: "2026-10-29T00:00:00Z",
  ...extra,
});

const conv = (extra: Partial<Conversation>): Conversation => ({
  id: "c1",
  workspace_id: "ws",
  kind: "direct",
  name: "",
  about: "",
  visibility: "private",
  created_by: "u1",
  last_message_seq: 5,
  last_message_at: null,
  archived_at: null,
  created_at: "",
  updated_at: "",
  is_member: true,
  last_read_seq: 1,
  unread_count: 4,
  members: [],
  ...extra,
});

function seed(qc: QueryClient, pages: ConversationMessage[][]) {
  qc.setQueryData<InfiniteData<ConversationMessagesPage>>(conversationKeys.messages("ws", "c1"), {
    pages: pages.map((messages) => ({ messages, has_more: false })),
    pageParams: pages.map(() => null),
  });
}

function read(qc: QueryClient) {
  return flattenConversationMessages(
    qc.getQueryData<InfiniteData<ConversationMessagesPage>>(conversationKeys.messages("ws", "c1")),
  );
}

describe("conversation message cache", () => {
  it("flattens newest-first pages into one oldest-first timeline", () => {
    const qc = new QueryClient();
    seed(qc, [[msg(3), msg(4)], [msg(1), msg(2)]]);
    expect(read(qc).map((m) => m.seq)).toEqual([1, 2, 3, 4]);
  });

  it("replaces the optimistic copy by client id and keeps seq order", () => {
    const qc = new QueryClient();
    seed(qc, [[msg(1)]]);
    upsertConversationMessage(qc, "ws", msg(OPTIMISTIC_SEQ, { id: "optimistic:k", client_message_id: "k" }));
    upsertConversationMessage(qc, "ws", msg(3, { author_type: "agent", author_id: "a1" }));
    expect(read(qc).map((m) => m.id)).toEqual(["m1", "m3", "optimistic:k"]);

    upsertConversationMessage(qc, "ws", msg(2, { client_message_id: "k" }));
    expect(read(qc).map((m) => m.id)).toEqual(["m1", "m2", "m3"]);

    // The realtime copy of the same message arriving again is a no-op.
    upsertConversationMessage(qc, "ws", msg(2, { client_message_id: "k" }));
    expect(read(qc)).toHaveLength(3);
  });

  it("keeps a message's thread summary when a newer copy omits it", () => {
    const qc = new QueryClient();
    seed(qc, [[msg(1, { thread: { id: "t1", reply_count: 2, last_reply_at: null } })]]);
    upsertConversationMessage(qc, "ws", msg(1));
    expect(read(qc)[0]!.thread?.id).toBe("t1");
  });

  it("leaves a timeline nobody has loaded alone", () => {
    const qc = new QueryClient();
    upsertConversationMessage(qc, "ws", msg(1));
    expect(qc.getQueryData(conversationKeys.messages("ws", "c1"))).toBeUndefined();
  });

  it("clears unread up to the read seq", () => {
    const qc = new QueryClient();
    qc.setQueryData(conversationKeys.list("ws"), [conv({})]);
    markConversationReadInCache(qc, "ws", "c1", 5);
    expect(qc.getQueryData<Conversation[]>(conversationKeys.list("ws"))![0]).toMatchObject({
      last_read_seq: 5,
      unread_count: 0,
    });
  });
});

describe("direct conversations", () => {
  const members = (...ids: string[]) => ids.map((member_id) => ({ member_type: "member" as const, member_id }));

  it("finds the one conversation between two people, or notes to self", () => {
    const pair = conv({ id: "pair", members: members("me", "you") });
    const notes = conv({ id: "notes", members: members("me") });
    const channel = conv({ id: "ch", kind: "channel", members: members("me", "you") });
    const list = [channel, pair, notes];
    expect(findDirectConversation(list, "me", "you")?.id).toBe("pair");
    expect(findDirectConversation(list, "you", "me")?.id).toBe("pair");
    expect(findDirectConversation(list, "me", "me")?.id).toBe("notes");
    expect(findDirectConversation(list, "me", "them")).toBeNull();
    expect(directCounterpart(pair, "me")).toBe("you");
    expect(directCounterpart(notes, "me")).toBe("me");
  });
});
