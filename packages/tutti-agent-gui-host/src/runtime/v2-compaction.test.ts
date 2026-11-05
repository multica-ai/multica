// @vitest-environment node
import {
ConversationModel,
TranscriptModel
} from "@multica/conversation-client/models";
import {
localV2ServerMessageSchema,
localV2SnapshotSchema
} from "@multica/core/local-conversation/v2";
import { normalizeAgentActivitySession } from "@tutti-os/agent-activity-core";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { describe,expect,it } from "vitest";
import { projectAgentActivitySessionToConversationVM } from "../../../../vendor/tutti/packages/agent/gui/shared/agentConversation/projection/workspaceAgentMessageProjection";
import { CommittedConversationMessages } from "../../../../vendor/tutti/packages/agent/gui/state/CommittedConversationMessages";
import { readNativeSessionTurns } from "../../../../vendor/tutti/packages/agent/gui/state/nativeSessionPresentation";

const at = "2026-10-29T12:00:00Z";
const fixture = localV2ServerMessageSchema.parse(
  JSON.parse(
    readFileSync(
      join(
        __dirname,
        "../../../../server/internal/localconversation/protocol/testdata/local-v2/valid/server_message__compaction.json"
      ),
      "utf8"
    )
  )
);
if (fixture.type !== "conversation.events")
  throw new Error("Expected compaction events");
const events = fixture.events;

function snapshot(provider: string, history = events.slice(0, 1)) {
  return localV2SnapshotSchema.parse({
    version: 2,
    store_generation: "g1",
    snapshot_seq: history.at(-1)?.seq ?? 0,
    conversation: {
      conversation_id: "conv-1",
      summary_seq: 1,
      provider,
      title: "test",
      status: "running",
      pinned: false,
      archived: false,
      updated_at: at
    },
    turns: [
      {
        id: "turn-1",
        turn_index: 0,
        request_id: "r1",
        status: "running",
        started_at: at,
        messages: [],
        events: history
      }
    ]
  });
}

const project = (conversation: ConversationModel) => ({
  ...readNativeSessionTurns(conversation, { agentSessionId: "gui-1" }),
  messages: new CommittedConversationMessages(
    new TranscriptModel(),
    "ws",
    "gui-1",
    "en"
  ).update(conversation, "all")
});

function sharedRows(conversation: ConversationModel) {
  const view = project(conversation);
  const session = normalizeAgentActivitySession({
    workspaceId: "ws",
    agentSessionId: "gui-1",
    provider: conversation.summary.provider,
    title: "test",
    cwd: "",
    activeTurnId: view.activeTurnId,
    activeTurn: view.turns.find((turn) => turn.turnId === view.activeTurnId),
    latestTurnInteractions: [],
    pendingInteractions: []
  });
  const vm = projectAgentActivitySessionToConversationVM({
    agentSessionId: "gui-1",
    sessionTurns: view.turns,
    activitySnapshot: { workspaceId: "ws", sessions: [session], presences: [] },
    messages: view.messages.map((message, index) => ({
      ...message,
      sequence: index + 1,
      version: index + 1
    }))
  });
  expect(vm).not.toBeNull();
  expect(vm!.rows.some((row) => row.kind === "processing")).toBe(false);
  return (
    vm?.rows
      .filter((row) => row.kind === "message")
      .flatMap((row) => row.messages) ?? []
  );
}

describe.each(["codex", "claude"])("%s compaction", (provider) => {
  it("replaces live progress with one durable divider and replays without duplication", () => {
    const conversation = ConversationModel.fromSnapshot(snapshot(provider));
    expect(project(conversation).messages).toMatchObject([
      {
        messageId: "compact-1",
        status: "running",
        semantics: {
          noticeCommand: "compact",
          noticeCommandStatus: "running",
          turnSettling: false
        }
      }
    ]);
    // The shared GUI treats this as specific progress, suppressing its generic processing hint.
    expect(sharedRows(conversation)).toMatchObject([
      {
        presentationKind: "specific-progress",
        systemNotice: { command: "compact", commandStatus: "running" }
      }
    ]);
    expect(project(conversation).activeTurnId).toBe("turn-1");

    expect(conversation.applyEvents(events.slice(1))).toBe("applied");
    expect(conversation.applyEvents(events)).toBe("applied");
    const completed = project(conversation);
    expect(completed.messages).toHaveLength(1);
    expect(completed.messages[0]).toMatchObject({
      messageId: "compact-1",
      occurredAtUnixMs: Date.parse(at),
      status: "completed",
      semantics: { noticeCommandStatus: "completed", turnSettling: false }
    });
    expect(sharedRows(conversation)).toMatchObject([
      {
        presentationKind: "turn-boundary",
        systemNotice: { commandStatus: "completed" }
      }
    ]);
    const restored = ConversationModel.fromSnapshot(snapshot(provider, events));
    expect(project(restored)).toEqual(completed);
    expect(project(restored).activeTurnId).toBe("turn-1");
  });

  it.each([
    ["failed", "failed"],
    ["cancelled", "canceled"]
  ])("projects %s compaction and its error", (status, commandStatus) => {
    const conversation = ConversationModel.fromSnapshot(snapshot(provider));
    conversation.applyEvents([
      {
        ...events[1]!,
        payload: {
          kind: "compaction",
          status,
          error: { kind: "compaction_failed", message: "exhausted" }
        }
      }
    ]);
    expect(project(conversation).messages).toMatchObject([
      {
        status: commandStatus,
        payload: { detail: "exhausted" },
        semantics: {
          noticeCommand: "compact",
          noticeCommandStatus: commandStatus,
          userVisibleAssistantResponse: false,
          turnSettling: false
        }
      }
    ]);
    expect(sharedRows(conversation)).toMatchObject([
      {
        presentationKind: "turn-boundary",
        systemNotice: { commandStatus, detail: "exhausted" }
      }
    ]);
  });
});
