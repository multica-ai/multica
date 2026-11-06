// @vitest-environment node
import { ConversationListModel } from "@multica/conversation-client/models";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { autorun } from "mobx";
import { expect, it } from "vitest";
import { LocalConversationListModel } from "./local-conversation-list-model";

it("reacts to new sends while keeping late output in its original position", () => {
  const source = new ConversationListModel();
  const list = new LocalConversationListModel(source);
  const row = (id: string, seq: number, sent: string, activity = sent) => localV2SummarySchema.parse({
    conversation_id: id, summary_seq: seq, provider: "codex", title: id, status: "idle", pinned: false, archived: false,
    turn_count: 1, last_turn_sent_at: sent, updated_at: activity,
  });
  let visible: string[] = [];
  const stop = autorun(() => { visible = list.sessions.map((entry) => entry.id); });
  try {
    source.upsert([row("a", 1, "2026-11-06T09:00:00Z"), row("b", 2, "2026-11-06T09:01:00Z")]);
    expect(visible).toEqual(["b", "a"]);
    source.upsert([row("a", 3, "2026-11-06T09:00:00Z", "2026-11-06T09:02:00Z")]);
    expect(visible).toEqual(["b", "a"]);
    source.upsert([row("a", 4, "2026-11-06T09:03:00Z")]);
    expect(visible).toEqual(["a", "b"]);
    source.upsert([row("a", 3, "2026-11-06T09:00:00Z")]);
    expect(visible).toEqual(["a", "b"]);
  } finally {
    stop();
  }
});
