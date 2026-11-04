// @vitest-environment node
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { localV2ServerMessageSchema, localV2SnapshotSchema, type LocalV2Event } from "@multica/core/local-conversation/v2";
import { V2Conversation } from "@multica/core/local-conversation/conversation";
import { projectV2Conversation } from "./v2-projection";
import { mergeWorkspaceAgentActivityDurableAndOverlayMessages } from "../../../../vendor/tutti/packages/agent/gui/shared/workspaceAgentMessageOverlay";

const samples = join(__dirname, "../../../../server/internal/localconversation/protocol/testdata/local-v2/valid");
const at = "2026-10-29T12:00:00Z";

function sampleEvents(name: string): LocalV2Event[] {
  const message = localV2ServerMessageSchema.parse(JSON.parse(readFileSync(join(samples, `server_message__${name}.json`), "utf8")));
  if (message.type !== "conversation.events") throw new Error(`${name} is not an event batch`);
  return message.events;
}

function empty(seq = 0): V2Conversation {
  return V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({
    version: 2, store_generation: "g1", snapshot_seq: seq, turns: [],
    conversation: { conversation_id: "conv-1", summary_seq: 1, provider: "claude", title: "", status: "idle", pinned: false, archived: false, updated_at: at },
  }));
}

/** Applies a sample whose sequence starts later, as if earlier events were seen. */
function withSample(name: string): V2Conversation {
  const events = sampleEvents(name);
  const conversation = empty(events[0]!.seq - 1);
  conversation.turns.set("turn-1", { id: "turn-1", index: 0, requestId: "r1", status: "running", startedAt: at, completedAt: null, error: null });
  expect(conversation.apply(events)).toBe("applied");
  return conversation;
}

function event(seq: number, type: string, payload: Record<string, unknown>, extra: Partial<LocalV2Event> = {}): LocalV2Event {
  return { seq, turn_id: "turn-1", turn_seq: seq, item_id: null, type, final: type === "item.completed" || type === "turn.completed", created_at: at, payload, ...extra };
}

const project = (conversation: V2Conversation) => projectV2Conversation(conversation, { workspaceId: "ws", agentSessionId: "gui-1", locale: "en" });

describe("V2Conversation", () => {
  it.each(["events", "running snapshot", "completed snapshot"])("replaces a queued prompt's optimistic message from %s", (source) => {
    const input = event(1, "user.message", { message_id: "m1", request_id: "submit-1", content: "hi", attachment_ids: ["att-1"] }, { final: true });
    let conversation = empty();
    if (source === "events") {
      conversation.apply([input]);
    } else {
      conversation = V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({
        version: 2, store_generation: "g1", snapshot_seq: 1,
        conversation: { conversation_id: "conv-1", summary_seq: 1, provider: "codex", title: "hi", status: "idle", pinned: false, archived: false, updated_at: at },
        turns: [{
          id: "turn-1", turn_index: 0, request_id: "submit-1", status: source === "running snapshot" ? "running" : "completed", started_at: at,
          messages: [{ id: "m1", role: "user", content: "hi", attachment_ids: ["att-1"], created_at: at }],
          events: [input],
        }],
      }));
    }
    const durable = project(conversation).messages.map((message, index) => ({ ...message, sequence: index + 1, version: index + 1 }));
    const optimistic = {
      ...durable[0]!, messageId: "client-submit:user:submit-1", turnId: "pending:submit-1",
      payload: { clientSubmitId: "submit-1", __agentGuiOptimisticPrompt: true, text: "hi", content: [{ type: "text", text: "hi" }] },
    };
    expect(mergeWorkspaceAgentActivityDurableAndOverlayMessages({ durableMessages: durable, localMessages: [optimistic] })).toEqual(durable);
    expect(durable).toHaveLength(1);
    expect(durable[0]!.payload).toMatchObject({ clientSubmitId: "submit-1", attachments: [{ attachmentId: "att-1" }] });
  });

  it("keeps identical prompts with distinct submit ids, including input added to the same turn", () => {
    const conversation = empty();
    conversation.apply([
      event(1, "user.message", { message_id: "m1", request_id: "submit-1", content: "hi" }),
      event(2, "turn.started", { request_id: "submit-1" }),
      event(3, "user.message", { message_id: "m2", request_id: "submit-2", content: "hi", steered: true }),
    ]);
    const durable = project(conversation).messages.map((message, index) => ({ ...message, sequence: index + 1, version: index + 1 }));
    expect(durable.map((message) => message.payload.clientSubmitId)).toEqual(["submit-1", "submit-2"]);
    const optimistic = {
      ...durable[0]!, messageId: "client-submit:user:submit-3", turnId: "pending:submit-3",
      payload: { clientSubmitId: "submit-3", __agentGuiOptimisticPrompt: true, text: "hi" },
    };
    expect(mergeWorkspaceAgentActivityDurableAndOverlayMessages({ durableMessages: durable, localMessages: [optimistic] })).toHaveLength(3);
  });

  it("does not assign a turn's submit id to a message whose request id is unknown", () => {
    const conversation = empty();
    conversation.apply([
      event(1, "user.message", { message_id: "imported-message", content: "hi" }),
      event(2, "turn.started", { request_id: "submit-1" }),
    ]);
    expect(project(conversation).messages[0]!.payload).not.toHaveProperty("clientSubmitId");
  });

  it("streams text and lets the final event replace it", () => {
    const conversation = empty();
    expect(conversation.apply([
      event(1, "user.message", { message_id: "m1", content: "hi" }),
      event(2, "turn.started", { request_id: "r1" }),
      event(3, "item.started", { kind: "assistant_text" }, { item_id: "it-1" }),
      event(4, "item.delta", { kind: "assistant_text", field: "text", text: "Hel" }, { item_id: "it-1" }),
      event(5, "item.delta", { kind: "assistant_text", field: "text", text: "lo" }, { item_id: "it-1" }),
    ])).toBe("applied");
    expect(conversation.items.get("it-1")?.state.text).toBe("Hello");
    let view = project(conversation);
    expect(view.activeTurnId).toBe("turn-1");
    expect(view.turns[0]?.phase).toBe("running");
    expect(view.messages.find((message) => message.messageId === "it-1")?.semantics?.turnSettling).toBe(false);

    conversation.apply([
      event(6, "item.completed", { kind: "assistant_text", status: "completed", text: "Hello!" }, { item_id: "it-1" }),
      event(7, "turn.completed", { status: "completed" }),
    ]);
    view = project(conversation);
    const answer = view.messages.find((message) => message.messageId === "it-1")!;
    expect(answer.payload.content).toBe("Hello!");
    expect(answer.semantics?.turnSettling).toBe(true);
    expect(view.turns[0]).toMatchObject({ phase: "settled", outcome: "completed" });
    expect(view.activeTurnId).toBeNull();
    expect(view.messages.find((message) => message.messageId === "m1")?.role).toBe("user");
  });

  it("skips duplicates and reports a gap without applying past it", () => {
    const conversation = empty(2);
    expect(conversation.apply([event(2, "turn.started", { request_id: "old" })])).toBe("applied");
    expect(conversation.turns.size).toBe(0);
    expect(conversation.apply([event(3, "turn.started", { request_id: "r1" }), event(5, "turn.completed", { status: "completed" })])).toBe("gap");
    expect(conversation.seq).toBe(3);
    expect(conversation.turns.get("turn-1")?.status).toBe("running");
  });

  it("decodes every golden event batch", () => {
    for (const name of ["assistant_text_stream", "background_task", "command_item", "conversation_level", "file_change_and_tool", "interaction", "turn_lifecycle"]) {
      const events = sampleEvents(name);
      const conversation = empty(events[0]!.seq - 1);
      expect(conversation.apply(events), name).toBe("applied");
      expect(conversation.seq).toBe(events.at(-1)!.seq);
      expect(() => project(conversation)).not.toThrow();
    }
  });

  it("projects tools, files, commands and images into the GUI's tool calls", () => {
    const view = project(withSample("file_change_and_tool"));
    const byId = new Map(view.messages.map((message) => [message.messageId, message]));
    expect(byId.get("it-4")).toMatchObject({ kind: "tool_call", status: "completed", payload: { toolName: "Edit" } });
    expect(JSON.parse(byId.get("it-4")!.payload.output as string)[0].path).toBe("src/a.ts");
    expect(byId.get("it-5")).toMatchObject({ kind: "tool_call", status: "failed", payload: { toolName: "mcp__multica__search_issues", output: "timeout" } });
    expect(byId.get("it-6")!.payload.attachments).toEqual([{ attachmentId: "att-1", mimeType: "image/png" }]);

    const command = project(withSample("command_item")).messages.find((message) => message.kind === "tool_call")!;
    expect(command.payload.toolName).toBe("Bash");
    expect((command.payload.input as { command: string }).command).not.toBe("");
  });

  it("shows background task progress and its final state", () => {
    const conversation = empty(39);
    conversation.turns.set("turn-1", { id: "turn-1", index: 0, requestId: "r1", status: "running", startedAt: at, completedAt: null, error: null });
    const events = sampleEvents("background_task");
    conversation.apply(events.slice(0, 2));
    expect(project(conversation).messages[0]).toMatchObject({ status: "running", payload: { noticeKind: "background_task", title: "Background task running · watch build" } });
    expect(project(conversation).messages[0]!.payload.content).toContain("compiling");
    conversation.apply(events.slice(2));
    expect(project(conversation).messages[0]).toMatchObject({ status: "cancelled", payload: { title: "Background task stopped · watch build" } });
  });

  it("shows a question as pending until it is answered and hides the raw tool call", () => {
    const conversation = empty();
    const question = { request_id: "q1", questions: [{ id: "go", question: "Continue?", options: [{ label: "Yes" }], multiSelect: false, allowFreeText: false }] };
    conversation.apply([
      event(1, "turn.started", { request_id: "r1" }),
      event(2, "item.started", { kind: "tool_call", tool: "ask_user_question", server: "multica_desktop_context", input: question }, { item_id: "call-1" }),
      event(3, "interaction.requested", { interaction_id: "question:turn-1:q1", kind: "question", request: question }),
    ]);
    let view = project(conversation);
    expect(view.turns[0]?.phase).toBe("waiting");
    expect(view.interactions).toMatchObject([{ requestId: "question:turn-1:q1", status: "pending", input: { requestId: "q1" } }]);
    expect(view.messages.map((message) => message.messageId)).toEqual(["question:turn-1:q1"]);

    conversation.apply([event(4, "interaction.resolved", { interaction_id: "question:turn-1:q1", status: "answered", answer: { go: "Yes" } })]);
    view = project(conversation);
    expect(view.turns[0]?.phase).toBe("running");
    expect(view.interactions[0]).toMatchObject({ status: "answered", output: { answersByQuestionId: { go: "Yes" }, answers: ["Yes"] } });
  });

  it("restores pending questions and turns from a snapshot", () => {
    const conversation = V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({
      version: 2, store_generation: "g1", snapshot_seq: 9, has_older_turns: true,
      conversation: { conversation_id: "conv-1", summary_seq: 3, provider: "codex", title: "t", status: "running", pinned: false, archived: false, updated_at: at },
      turns: [{
        id: "turn-1", turn_index: 0, request_id: "r1", status: "running", started_at: at,
        messages: [{ id: "m1", role: "user", content: "hi", attachment_ids: ["att-9"], created_at: at }],
        events: [
          event(2, "item.started", { kind: "reasoning" }, { item_id: "it-1" }),
          event(3, "item.delta", { kind: "reasoning", field: "text", text: "thinking" }, { item_id: "it-1" }),
        ],
      }],
      pending_interactions: [{ interaction_id: "question:turn-1:q1", turn_id: "turn-1", kind: "question", request: { request_id: "q1", questions: [] }, created_at: at }],
    }));
    expect(conversation.seq).toBe(9);
    expect(conversation.hasOlderTurns).toBe(true);
    const view = project(conversation);
    expect(view.turns[0]?.phase).toBe("waiting");
    expect(view.messages.find((message) => message.messageId === "it-1")).toMatchObject({ kind: "reasoning", payload: { content: "thinking" } });
    expect(view.messages.find((message) => message.messageId === "m1")!.payload.attachments).toEqual([{ attachmentId: "att-9" }]);
    expect(conversation.apply([event(9, "turn.completed", { status: "completed" })])).toBe("applied");
    expect(conversation.turns.get("turn-1")?.status).toBe("running");
  });

  it("shows reasoning as running until it ends, with a label while it has no text", () => {
    const conversation = V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({
      version: 2, store_generation: "g1", snapshot_seq: 2, has_older_turns: false,
      conversation: { conversation_id: "conv-1", summary_seq: 1, provider: "codex", title: "t", status: "running", pinned: false, archived: false, updated_at: at },
      turns: [{ id: "turn-1", turn_index: 0, request_id: "r1", status: "running", started_at: at, messages: [], events: [
        event(2, "item.started", { kind: "reasoning" }, { item_id: "it-1" }),
      ] }],
    }));
    const reasoning = (locale: "en" | "zh-CN" = "en") => projectV2Conversation(conversation, { workspaceId: "w", agentSessionId: "s", locale }).messages.find((message) => message.messageId === "it-1");
    expect(reasoning()).toMatchObject({ kind: "reasoning", status: "running", payload: { content: "Thinking" } });
    expect(reasoning("zh-CN")).toMatchObject({ payload: { content: "思考中" } });

    conversation.apply([event(3, "item.delta", { kind: "reasoning", field: "text", text: "plan" }, { item_id: "it-1" })]);
    expect(reasoning()).toMatchObject({ status: "running", payload: { content: "plan" } });

    conversation.apply([event(4, "item.completed", { kind: "reasoning", status: "completed", text: "plan\ncheck" }, { item_id: "it-1", final: true })]);
    expect(reasoning()).toMatchObject({ payload: { content: "plan\ncheck" } });
    expect(reasoning()?.status).toBeUndefined();
  });

  it("leaves reasoning without text empty once it has ended", () => {
    const conversation = V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({
      version: 2, store_generation: "g1", snapshot_seq: 3, has_older_turns: false,
      conversation: { conversation_id: "conv-1", summary_seq: 1, provider: "codex", title: "t", status: "idle", pinned: false, archived: false, updated_at: at },
      turns: [{ id: "turn-1", turn_index: 0, request_id: "r1", status: "running", started_at: at, messages: [], events: [
        event(2, "item.started", { kind: "reasoning" }, { item_id: "it-1" }),
        event(3, "item.completed", { kind: "reasoning", status: "completed", text: "" }, { item_id: "it-1", final: true }),
      ] }],
    }));
    const message = projectV2Conversation(conversation, { workspaceId: "w", agentSessionId: "s", locale: "en" }).messages.find((entry) => entry.messageId === "it-1");
    expect(message?.payload.content).toBe("");
    expect(message?.status).toBeUndefined();
  });

  it("keeps assistant message rows out of the user messages", () => {
    // A fork copies messages under new ids, so an assistant row no longer shares its item's id.
    const conversation = V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({
      version: 2, store_generation: "g1", snapshot_seq: 4, has_older_turns: false,
      conversation: { conversation_id: "fork-1", summary_seq: 1, provider: "claude", title: "t · Fork", status: "idle", pinned: false, archived: false, updated_at: at },
      turns: [{
        id: "turn-1", turn_index: 0, request_id: "r1", status: "completed", started_at: at, completed_at: at,
        messages: [
          { id: "fork~m1", role: "user", content: "hi", attachment_ids: [], created_at: at },
          { id: "fork~a1", role: "assistant", content: "READY", attachment_ids: [], created_at: at },
        ],
        events: [event(2, "item.completed", { kind: "assistant_text", status: "completed", text: "READY" }, { item_id: "a1", final: true })],
      }],
      pending_interactions: [],
    }));
    expect([...conversation.messages.keys()]).toEqual(["fork~m1"]);
    expect(project(conversation).messages.filter((message) => JSON.stringify(message.payload).includes("READY"))).toHaveLength(1);
  });

  it.each([
    ["a stopped turn", "cancelled", null, true],
    ["a turn cut off by a restart", "failed", "daemon_restarted", true],
    ["a turn whose provider exited", "failed", "provider_exited", true],
    ["a failed turn", "failed", "provider_error", false],
    ["a completed turn", "completed", null, false],
    ["a running turn", "running", null, false],
  ])("continues %s: %s", (_name, status, kind, expected) => {
    const turn = (id: string, startedAt: string, value: string, error: string | null) => ({
      id, turn_index: 0, request_id: id, status: value, started_at: startedAt, messages: [], events: [],
      ...(value === "running" ? {} : { completed_at: startedAt }), ...(error ? { error: { kind: error, message: "x" } } : {}),
    });
    const conversation = V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({
      version: 2, store_generation: "g1", snapshot_seq: 1, has_older_turns: false, pending_interactions: [],
      conversation: { conversation_id: "conv-1", summary_seq: 1, provider: "codex", title: "t", status: "idle", pinned: false, archived: false, updated_at: at },
      // Only the latest turn decides.
      turns: [turn("old", "2026-10-29T00:00:00Z", "cancelled", null), turn("latest", "2026-10-29T01:00:00Z", status as string, kind as string | null)],
    }));
    expect(conversation.continuable()).toBe(expected);
  });
});

describe("paging and retained conversations", () => {
  const at = "2026-11-03T10:00:00Z";
  const summary = { conversation_id: "c", summary_seq: 1, provider: "codex", title: "c", status: "idle", pinned: false, archived: false, updated_at: at };
  const event = (seq: number, type: string, fields: Record<string, unknown> = {}) => ({ seq, turn_id: "t5", turn_seq: seq, item_id: "x", type, final: type === "item.completed", created_at: at, payload: { kind: "background_task" }, ...fields });
  const turn = (index: number, events: unknown[] = []) => ({ id: `t${index}`, turn_index: index, request_id: "", status: "completed", started_at: at, completed_at: at, messages: [], events });
  const snapshot = (fields: Record<string, unknown> = {}) => localV2SnapshotSchema.parse({ version: 2, store_generation: "g", snapshot_seq: 100, conversation: summary, turns: [turn(5)], has_older_turns: true, pending_interactions: [], ...fields });

  it("does not apply an item's events twice when a late page repeats what the stream delivered", () => {
    const conversation = V2Conversation.fromSnapshot(snapshot());
    conversation.apply([event(101, "item.started"), event(102, "item.delta", { payload: { field: "output", text: "ab" } })] as never);
    conversation.mergeHistory({ store_generation: "g", has_older_turns: false, turns: [turn(4, [event(101, "item.started"), event(102, "item.delta", { payload: { field: "output", text: "ab" } })])] } as never);
    expect(conversation.items.get("x")!.state.output).toBe("ab");
  });

  it("keeps a final state when an older event of the item arrives after it", () => {
    const conversation = V2Conversation.fromSnapshot(snapshot());
    conversation.apply([event(101, "item.completed", { payload: { kind: "background_task", status: "completed", output: "done" } })] as never);
    conversation.mergeHistory({ store_generation: "g", has_older_turns: false, turns: [turn(4, [event(50, "item.delta", { payload: { field: "output", text: "partial" } })])] } as never);
    expect(conversation.items.get("x")!.state.output).toBe("done");
  });

  it("orders turns by index and never moves the cursor when paging", () => {
    const conversation = V2Conversation.fromSnapshot(snapshot());
    conversation.mergeHistory({ store_generation: "g", has_older_turns: true, turns: [turn(3), turn(4)] } as never);
    expect([...conversation.turns.keys()]).toEqual(["t3", "t4", "t5"]);
    expect(conversation.seq).toBe(100);
    expect(conversation.oldestTurnIndex()).toBe(3);
    expect(conversation.hasOlderTurns).toBe(true);
  });

  it("learns an item's kind from a later event when its first was a delta", () => {
    const conversation = V2Conversation.fromSnapshot(snapshot());
    conversation.apply([event(101, "item.delta", { payload: { field: "output", text: "a" } }), event(102, "item.updated", { payload: { kind: "background_task", status: "in_progress" } })] as never);
    expect(conversation.items.get("x")!.kind).toBe("background_task");
  });

  it("keeps an item's start from a settled turn's final event", () => {
    const conversation = V2Conversation.fromSnapshot(snapshot({ turns: [turn(5, [event(9, "item.completed", { first_seq: 3, first_at: "2026-11-03T09:00:00Z" })])] }));
    expect(conversation.items.get("x")).toMatchObject({ order: 3, startedAt: "2026-11-03T09:00:00Z" });
  });

  it("retains paged history only when the new snapshot continues it", () => {
    const paged = V2Conversation.fromSnapshot(snapshot());
    paged.mergeHistory({ store_generation: "g", has_older_turns: false, turns: [turn(3), turn(4)] } as never);
    const continuing = V2Conversation.fromSnapshot(snapshot({ turns: [turn(5), turn(6)] }));
    continuing.retainHistoryFrom(paged);
    expect([...continuing.turns.keys()]).toEqual(["t3", "t4", "t5", "t6"]);
    expect(continuing.hasOlderTurns).toBe(true);
    const distant = V2Conversation.fromSnapshot(snapshot({ turns: [turn(40)] }));
    distant.retainHistoryFrom(paged);
    expect([...distant.turns.keys()]).toEqual(["t40"]);
    const otherStore = V2Conversation.fromSnapshot(snapshot({ store_generation: "other", turns: [turn(5)] }));
    otherStore.retainHistoryFrom(paged);
    expect([...otherStore.turns.keys()]).toEqual(["t5"]);
  });

  it("drops a retained turn with an unfinished item, which could never finish", () => {
    const paged = V2Conversation.fromSnapshot(snapshot());
    paged.mergeHistory({ store_generation: "g", has_older_turns: false, turns: [turn(3), turn(4, [{ ...event(10, "item.started"), turn_id: "t4" }])] } as never);
    const next = V2Conversation.fromSnapshot(snapshot());
    next.retainHistoryFrom(paged);
    expect([...next.turns.keys()]).toEqual(["t5"]);
    expect(next.hasOlderTurns).toBe(true);
  });

  it("keeps queue revisions monotonic and marks items stale until they are read", () => {
    const conversation = V2Conversation.fromSnapshot(snapshot({ queue: { revision: 5, paused: false, error: "old", dispatching_request_id: "r", items: [{ request_id: "a" }] } }));
    expect(conversation.settings).toMatchObject({ permission_mode: "default" });
    const queued = (seq: number, payload: Record<string, unknown>) => ({ seq, turn_id: null, turn_seq: null, item_id: null, type: "queue.updated", final: false, created_at: at, payload });
    conversation.apply([queued(101, { revision: 4, paused: true, count: 9 })] as never);
    expect(conversation.queue).toMatchObject({ revision: 5, paused: false });
    conversation.apply([queued(102, { revision: 6, paused: false, count: 0 })] as never);
    expect(conversation.queue.error).toBeUndefined();
    expect(conversation.queueItemsStale()).toBe(true);
    conversation.receiveQueue({ revision: 6, paused: false, items: [] });
    expect(conversation.queueItemsStale()).toBe(false);
    expect(conversation.queue.items).toEqual([]);
    conversation.receiveQueue({ revision: 2, paused: true, items: [{ request_id: "stale" }] });
    expect(conversation.queue.items).toEqual([]);
  });

  it("clears the dispatching flag when the turn starts, since no other event does", () => {
    const conversation = V2Conversation.fromSnapshot(snapshot({ queue: { revision: 5, paused: false, dispatching_request_id: "r", items: [] } }));
    conversation.apply([{ seq: 101, turn_id: "t6", turn_seq: 1, item_id: null, type: "turn.started", final: false, created_at: at, payload: {} }] as never);
    expect(conversation.queue.dispatching_request_id).toBeUndefined();
    expect(conversation.turns.get("t6")!.index).toBe(6);
  });
});
