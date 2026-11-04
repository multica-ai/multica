// @vitest-environment node
import { readFileSync } from "node:fs";
import { join } from "node:path";
import { expect, it } from "vitest";
import { localV2ServerMessageSchema, localV2SnapshotSchema } from "@multica/core/local-conversation/v2";
import { V2Conversation } from "@multica/core/local-conversation/conversation";
import { projectV2Conversation } from "./v2-projection";
import { normalizeAgentActivitySession } from "@tutti-os/agent-activity-core";
import { projectAgentActivitySessionToConversationVM } from "../../../../vendor/tutti/packages/agent/gui/shared/agentConversation/projection/workspaceAgentMessageProjection";

const fixture = localV2ServerMessageSchema.parse(JSON.parse(readFileSync(join(__dirname, "../../../../server/internal/localconversation/protocol/testdata/local-v2/valid/server_message__subagent.json"), "utf8")));
if (fixture.type !== "conversation.events") throw new Error("Expected event fixture");
const events = fixture.events;
const at = events[0]!.created_at;
const summary = { conversation_id: "conv-1", summary_seq: 1, provider: "claude", title: "test", status: "idle", pinned: false, archived: false, updated_at: at };
const project = (conversation: V2Conversation) => projectV2Conversation(conversation, { workspaceId: "ws", agentSessionId: "gui-1", locale: "en" });

it("projects a live and replayed child transcript inside its task without settling the root", () => {
  const conversation = V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({ version: 2, store_generation: "g", snapshot_seq: 0, conversation: summary, turns: [] }));
  conversation.apply(events.slice(0, 5));
  const live = project(conversation);
  expect(live.activeTurnId).toBe("turn-1");
  expect(live.messages.map((m) => m.messageId)).toEqual(["agent-a", "task:a"]);
  expect(live.messages[1]).toMatchObject({
    semantics: { userVisibleAssistantResponse: false, turnSettling: false },
    payload: { subagentTranscript: { prompt: "Review daemon changes", items: [
      { kind: "assistant_text", text: "Reading the source" },
      { kind: "command", status: "in_progress", input: { command: "git diff" } },
    ] } },
  });
  conversation.apply(events.slice(5));
  conversation.apply(events);
  const completed = project(conversation);
  expect(completed.messages).toHaveLength(3);
  expect(completed.messages[1]).toMatchObject({ status: "completed", payload: { subagentTranscript: { items: [
    { kind: "assistant_text", text: "Reading the source" },
    { tool: "Bash", status: "completed", output: "diff contents" },
  ] } } });
  expect(completed.messages[2]).toMatchObject({ messageId: "answer", semantics: { turnSettling: true } });
  const replay = V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({
    version: 2, store_generation: "g", snapshot_seq: events.length, conversation: summary,
    turns: [{ id: "turn-1", turn_index: 0, status: "completed", started_at: at, completed_at: at, request_id: "r1", messages: [], events: events.filter((e) => e.final) }],
  }));
  expect(project(replay).messages.find((m) => m.messageId === "task:a")).toEqual(completed.messages[1]);
  expect(project(replay).activeTurnId).toBeNull();
  const vm = projectAgentActivitySessionToConversationVM({
    agentSessionId: "gui-1", sessionTurns: completed.turns,
    activitySnapshot: {
      workspaceId: "ws", presences: [],
      sessions: [normalizeAgentActivitySession({ workspaceId: "ws", agentSessionId: "gui-1", provider: "claude", title: "test", cwd: "", activeTurnId: null, latestTurnInteractions: [], pendingInteractions: [] })],
      sessionMessagesById: { "gui-1": completed.messages.map((m, i) => ({ ...m, sequence: i + 1, version: i + 1 })) },
    },
  });
  const card = vm?.rows.filter((row) => row.kind === "message").flatMap((row) => row.messages).find((m) => m.systemNotice?.noticeKind === "background_task");
  expect(card?.sourceTimelineItems?.[0]?.payload?.subagentTranscript).toMatchObject({ items: [{ text: "Reading the source" }, { output: "diff contents" }] });
});

it("keeps parallel task transcripts separate even when their tool names match", () => {
  const conversation = V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({ version: 2, store_generation: "g", snapshot_seq: 0, conversation: summary, turns: [] }));
  conversation.apply(events.slice(0, 5));
  conversation.apply([
    { ...events[2]!, seq: 6, item_id: "task:b", payload: { kind: "background_task", task_id: "b", tool_use_id: "agent-b", description: "Review clients" } },
    { ...events[3]!, seq: 7, item_id: "child-b", payload: { kind: "assistant_text", text: "Review clients only", status: "completed", parent_tool_use_id: "agent-b" } },
  ]);
  const messages = project(conversation).messages;
  expect(messages.find((m) => m.messageId === "task:a")?.payload.subagentTranscript).toMatchObject({ items: [{ text: "Reading the source" }, { tool: "Bash" }] });
  expect(messages.find((m) => m.messageId === "task:b")?.payload.subagentTranscript).toMatchObject({ items: [{ text: "Review clients only" }] });
});

it("keeps background shell output in its existing details without a subagent transcript", () => {
  const conversation = V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({ version: 2, store_generation: "g", snapshot_seq: 0, conversation: summary, turns: [] }));
  conversation.apply([
    events[0]!,
    { ...events[2]!, seq: 2, payload: { kind: "background_task", task_id: "a", task_type: "local_bash", tool_use_id: "shell-call", description: "Build" } },
    { ...events[6]!, seq: 3, payload: { kind: "background_task", task_id: "a", task_type: "local_bash", tool_use_id: "shell-call", description: "Build", status: "completed", output: "Build passed" } },
  ]);
  expect(project(conversation).messages[0]?.payload).toMatchObject({ detail: "Output:\nBuild passed" });
  expect(project(conversation).messages[0]?.payload).not.toHaveProperty("subagentTranscript");
});
