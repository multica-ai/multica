// @vitest-environment node
import {
ConversationModel,
TranscriptModel
} from "@multica/conversation-client/models";
import {
localV2SnapshotSchema,
type LocalV2Event
} from "@multica/core/local-conversation/v2";
import { describe,expect,it,vi } from "vitest";
import {
CommittedConversationMessages,
mergeConversationMessageChanges
} from "../../../../vendor/tutti/packages/agent/gui/state/CommittedConversationMessages";

const at = (minute: number) =>
  `2026-11-04T10:${String(minute).padStart(2, "0")}:00Z`;

function event(
  seq: number,
  turn: string | null,
  type: string,
  payload: Record<string, unknown>,
  item: string | null = null,
  minute = 0
): LocalV2Event {
  return {
    seq,
    turn_id: turn,
    turn_seq: seq,
    item_id: item,
    type,
    final: false,
    created_at: at(minute),
    payload
  } as LocalV2Event;
}

const reply = (
  seq: number,
  turn: string,
  item: string,
  text: string,
  minute = 0
) =>
  event(
    seq,
    turn,
    "item.completed",
    { kind: "assistant_text", status: "completed", text },
    item,
    minute
  );

function turn(
  index: number,
  fields: {
    id?: string;
    prompt?: number;
    events?: LocalV2Event[];
    minute?: number;
    status?: string;
  } = {}
) {
  const id = fields.id ?? `t${index}`;
  return {
    id,
    turn_index: index,
    request_id: "",
    status: fields.status ?? "completed",
    started_at: at(fields.minute ?? index),
    messages:
      fields.prompt === undefined
        ? []
        : [
            {
              id: `${id}-q`,
              role: "user",
              content: "q",
              attachment_ids: [],
              steered: false,
              failed: false,
              created_at: at(fields.prompt)
            }
          ],
    events: fields.events ?? []
  };
}

function model(turns: ReturnType<typeof turn>[], seq = 100) {
  return ConversationModel.fromSnapshot(
    localV2SnapshotSchema.parse({
      version: 2,
      store_generation: "g",
      snapshot_seq: seq,
      has_older_turns: true,
      turns,
      conversation: {
        conversation_id: "c",
        summary_seq: 1,
        provider: "claude",
        title: "c",
        status: "idle",
        pinned: false,
        archived: false,
        updated_at: at(0)
      }
    })
  );
}

const fresh = () => ({
  transcript: new CommittedConversationMessages(
    new TranscriptModel(),
    "w",
    "s",
    "en"
  )
});
const ids = (messages: { messageId: string }[]) =>
  messages.map((message) => message.messageId);

describe("the order of a conversation's messages", () => {
  it("keeps a turn together after its prompt, whatever its output's clock says", () => {
    const conversation = model([
      turn(0, { prompt: 10, events: [reply(1, "t0", "late", "a", 30)] }),
      turn(1, {
        prompt: 20,
        events: [
          reply(2, "t1", "backfilled", "b", 5),
          reply(3, "t1", "next", "c", 40)
        ]
      })
    ]);
    expect(ids(fresh().transcript.update(conversation, "all"))).toEqual([
      "t0-q",
      "late",
      "t1-q",
      "backfilled",
      "next"
    ]);
  });

  it("gives every message a positive safe sequence, increasing through the conversation", () => {
    const messages = fresh().transcript.update(
      model([
        turn(0, { prompt: 0 }),
        turn(100_000, { prompt: 1, events: [reply(1, "t100000", "x", "x")] })
      ]),
      "all"
    );
    const sequences = messages.map((message) => message.sequence!);
    expect(
      sequences.every((value) => Number.isSafeInteger(value) && value > 0)
    ).toBe(true);
    expect(sequences).toEqual([...sequences].sort((a, b) => a - b));
  });

  it("does not move or rebuild anything when older turns are paged in", () => {
    const conversation = model([
      turn(5, { prompt: 5, events: [reply(1, "t5", "a", "a")] })
    ]);
    const { transcript } = fresh();
    const before = transcript.update(conversation, "all");
    conversation.mergeHistory({
      store_generation: "g",
      has_older_turns: false,
      turns: [turn(4, { prompt: 4 })]
    } as never);
    const after = transcript.update(conversation, "all");
    expect(ids(after)).toEqual(["t4-q", "t5-q", "a"]);
    expect(after[1]).toBe(before[0]);
    expect(after[2]).toBe(before[1]);
  });

  it("shows a prompt before its turn starts, then inside it", () => {
    const conversation = model([turn(0, { prompt: 0 })], 0);
    const { transcript } = fresh();
    transcript.update(conversation, "all");
    conversation.applyEvents([
      event(
        1,
        "t1",
        "user.message",
        { message_id: "next-q", content: "next" },
        null,
        9
      )
    ]);
    const waiting = transcript.update(
      conversation,
      mergeConversationMessageChanges(
        undefined,
        conversation.turns.has("t1") ? [] : [event(1, "t1", "user.message", {})]
      )
    );
    expect(ids(waiting)).toEqual(["t0-q", "next-q"]);
    const started = [
      event(2, "t1", "turn.started", { turn_index: 1 }, null, 9),
      reply(3, "t1", "answer", "a", 10)
    ];
    conversation.applyEvents(started);
    const running = transcript.update(
      conversation,
      mergeConversationMessageChanges(undefined, started)
    );
    expect(ids(running)).toEqual(["t0-q", "next-q", "answer"]);
    expect(running[1]!.sequence).toBeLessThan(running[2]!.sequence!);
  });

  it("places a question of a turn not paged in by its time", () => {
    const conversation = ConversationModel.fromSnapshot(
      localV2SnapshotSchema.parse({
        version: 2,
        store_generation: "g",
        snapshot_seq: 10,
        turns: [turn(5, { prompt: 5, minute: 5 })],
        conversation: {
          conversation_id: "c",
          summary_seq: 1,
          provider: "claude",
          title: "c",
          status: "idle",
          pinned: false,
          archived: false,
          updated_at: at(0)
        },
        pending_interactions: [
          {
            interaction_id: "q",
            turn_id: "old",
            kind: "question",
            request: {},
            created_at: at(1)
          }
        ]
      })
    );
    expect(ids(fresh().transcript.update(conversation, "all"))).toEqual([
      "q",
      "t5-q"
    ]);
  });
});

describe("committing what events changed", () => {
  const streaming = () => {
    const conversation = model(
      [
        turn(0, { prompt: 0, events: [reply(1, "t0", "old", "old")] }),
        turn(1, {
          prompt: 1,
          status: "running",
          events: [
            reply(2, "t1", "done", "done"),
            event(3, "t1", "item.started", { kind: "assistant_text" }, "live")
          ]
        })
      ],
      3
    );
    const fixture = fresh();
    return {
      conversation,
      ...fixture,
      first: fixture.transcript.update(conversation, "all")
    };
  };
  const find = <T extends { messageId: string }>(messages: T[], id: string) =>
    messages.find((message) => message.messageId === id);

  it("keeps every other message's object and version", () => {
    const { conversation, transcript, first } = streaming();
    const events = [
      event(4, "t1", "item.delta", { field: "text", text: "hel" }, "live")
    ];
    conversation.applyEvents(events);
    const next = transcript.update(
      conversation,
      mergeConversationMessageChanges(undefined, events)
    );
    expect(find(next, "live")).toMatchObject({ payload: { content: "hel" } });
    expect(find(next, "live")!.version).toBeGreaterThan(
      find(first, "live")!.version
    );
    for (const id of ["t0-q", "old", "t1-q", "done"])
      expect(find(next, id)).toBe(find(first, id));
  });

  it("rebuilds no row of the changed turn that did not change", () => {
    const { conversation, transcript, first } = streaming();
    const events = [
      event(4, "t1", "item.delta", { field: "text", text: "x" }, "live")
    ];
    conversation.applyEvents(events);
    const next = transcript.update(
      conversation,
      mergeConversationMessageChanges(undefined, events)
    );
    expect(find(next, "done")).toBe(find(first, "done"));
    expect(find(next, "live")).not.toBe(find(first, "live"));
  });

  it("reads nothing but the changed turn, and hashes nothing", () => {
    const { conversation, transcript } = streaming();
    const otherTurn = vi.spyOn(conversation.turns.get("t0")!, "messages");
    const events = [
      event(4, "t1", "item.delta", { field: "text", text: "hel" }, "live")
    ];
    conversation.applyEvents(events);
    transcript.update(
      conversation,
      mergeConversationMessageChanges(undefined, events)
    );
    expect(otherTurn).not.toHaveBeenCalled();
  });

  it("gives the same text at the same place as reading everything", () => {
    const { conversation, transcript } = streaming();
    const events = [
      event(4, "t1", "item.delta", { field: "text", text: "hel" }, "live"),
      event(5, "t1", "turn.completed", { status: "completed" })
    ];
    conversation.applyEvents(events);
    const next = transcript.update(
      conversation,
      mergeConversationMessageChanges(undefined, events)
    );
    const whole = fresh().transcript.update(conversation, "all");
    const shape = (messages: typeof next) =>
      messages.map(({ version: _version, ...rest }) => rest);
    expect(shape(next)).toEqual(shape(whole));
  });

  it("updates an earlier reply's settling when the turn ends", () => {
    const { conversation, transcript, first } = streaming();
    const events = [event(4, "t1", "turn.completed", { status: "completed" })];
    conversation.applyEvents(events);
    const next = transcript.update(
      conversation,
      mergeConversationMessageChanges(undefined, events)
    );
    expect(find(first, "live")!.semantics).toMatchObject({
      turnSettling: false
    });
    expect(find(next, "live")!.semantics).toMatchObject({ turnSettling: true });
    expect(find(next, "t1-q")).toBe(find(first, "t1-q"));
  });

  it("drops a message its item no longer produces", () => {
    const { conversation, transcript } = streaming();
    const added = [
      event(4, "t1", "item.started", { kind: "tool_call", tool: "x" }, "tool")
    ];
    conversation.applyEvents(added);
    transcript.update(
      conversation,
      mergeConversationMessageChanges(undefined, added)
    );
    const hidden = [
      event(
        5,
        "t1",
        "item.completed",
        {
          kind: "tool_call",
          status: "completed",
          server: "multica_desktop_context",
          tool: "ask_user_question"
        },
        "tool"
      )
    ];
    conversation.applyEvents(hidden);
    expect(
      ids(
        transcript.update(
          conversation,
          mergeConversationMessageChanges(undefined, hidden)
        )
      )
    ).not.toContain("tool");
  });

  it("reads everything again after a change that was not events", () => {
    expect(mergeConversationMessageChanges(undefined, [])).toBe("all");
    expect(mergeConversationMessageChanges(new Set(["t"]), [])).toBe("all");
    expect(
      mergeConversationMessageChanges("all", [event(1, "t", "item.delta", {})])
    ).toBe("all");
    expect(
      mergeConversationMessageChanges(undefined, [
        event(1, null, "user.message", {})
      ])
    ).toBe("all");
    expect(
      mergeConversationMessageChanges(new Set(["a"]), [
        event(1, "b", "item.delta", {}),
        event(2, null, "queue.updated", {})
      ])
    ).toEqual(new Set(["a", "b"]));
  });

  it("starts over on another model instance", () => {
    const { conversation, transcript } = streaming();
    const other = model([turn(0, { prompt: 0 })]);
    transcript.update(conversation, "all");
    expect(ids(transcript.update(other, new Set()))).toEqual(["t0-q"]);
  });
});

describe("legacy submission identity", () => {
  it("adds an old accepted receipt without overriding a native submission id", () => {
    const conversation = model([turn(0, { prompt: 0 })]);
    let receipt: string | undefined;
    const transcript = new CommittedConversationMessages(
      new TranscriptModel(),
      "w",
      "s",
      "en",
      () => receipt
    );
    transcript.update(conversation, "all");
    receipt = "submit-1";
    const next = transcript.update(conversation, new Set(["t0"]));
    expect(next[0]!.payload).toMatchObject({ clientSubmitId: "submit-1" });
    expect(transcript.update(conversation, new Set(["t0"]))[0]).toBe(next[0]);
  });
});
