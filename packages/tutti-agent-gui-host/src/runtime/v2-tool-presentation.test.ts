// @vitest-environment node
import { ConversationModel, TranscriptModel } from "@multica/conversation-client/models";
import { localV2EventSchema, localV2SnapshotSchema, type LocalV2Event } from "@multica/core/local-conversation/v2";
import { readFileSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { CommittedConversationMessages } from "../../../../vendor/tutti/packages/agent/gui/state/CommittedConversationMessages";
import { projectWorkspaceAgentMessagesToTimelineItems } from "../../../../vendor/tutti/packages/agent/gui/shared/agentConversation/projection/workspaceAgentMessageProjection";
import { toolCallView } from "../../../../vendor/tutti/packages/agent/gui/shared/workspaceAgentTimelineProjectionHelpers";
import { projectAgentToolCall } from "../../../../vendor/tutti/packages/agent/gui/shared/agentConversation/projection/agentToolProjection";
import { getCommandRenderData, getFileChangeRenderData, getImageGenerationRenderData, getMcpRenderData } from "../../../../vendor/tutti/packages/agent/gui/shared/agentConversation/components/tool-renderers/render-data/agentToolRenderData";

const at = "2026-11-07T06:00:00Z";

function event(seq: number, itemId: string, type: string, payload: Record<string, unknown>): LocalV2Event {
  return { seq, turn_id: "turn", turn_seq: seq, item_id: itemId, type, final: type === "item.completed", created_at: at, payload };
}

function restore(events: LocalV2Event[]) {
  return ConversationModel.fromSnapshot(localV2SnapshotSchema.parse({
    version: 2, store_generation: "g", snapshot_seq: events.at(-1)?.seq ?? 0,
    conversation: { conversation_id: "c", summary_seq: 1, provider: "codex", title: "fixture", status: "running", pinned: false, archived: false, updated_at: at },
    turns: [{ id: "turn", turn_index: 0, request_id: "request", status: "running", started_at: at, messages: [], events }],
  }));
}

function tools(conversation: ConversationModel) {
  const messages = new CommittedConversationMessages(new TranscriptModel(), "workspace", "session", "en").update(conversation, "all");
  return projectWorkspaceAgentMessagesToTimelineItems(messages)
    .filter((item) => item.callId)
    .map((item) => ({ callId: item.callId!, call: projectAgentToolCall(toolCallView(item)) }));
}

describe("v2 tools through the GUI presentation boundary", () => {
  it.each(["stream", "final snapshot"])("keeps remote tool commands from Go-normalized output-only results in %s", (source) => {
    // Go's TestRemoteToolEventFixture checks these events against raw cloud
    // messages whose tool_result supplies no input, including across polls.
    const fixture = JSON.parse(readFileSync(new URL(
      "../../../../server/internal/localconversation/service/testdata/remote-tool-events.json", import.meta.url,
    ), "utf8"));
    const events = localV2EventSchema.array().parse(fixture.events);
    const conversation = source === "stream" ? restore([]) : restore(events.filter((event) => event.final));
    if (source === "stream") {
      conversation.applyEvents(events.slice(0, 2));
      expect(tools(conversation).map(({ call }) => getCommandRenderData(call).command)).toEqual(["fixture-inspect", "fixture-second"]);
      conversation.applyEvents(events.slice(2));
    }
    const calls = new Map(tools(conversation).map(({ callId, call }) => [callId, getCommandRenderData(call)]));
    expect(calls.get("remote-turn-a")).toMatchObject({ command: "fixture-inspect", cwd: "/repo", stdout: "First output\n", status: "completed" });
    expect(calls.get("remote-turn-b")).toMatchObject({ command: "fixture-second", cwd: "/other", stdout: "Second output\n", status: "completed" });
  });

  it.each(["command", "tool_call"])("renders %s output while streaming and after reopening", (kind) => {
    const input = kind === "command" ? { command: "fixture", cwd: "/repo" } : { tool: "Bash", input: { command: "fixture", cwd: "/repo" } };
    const events = [
      event(1, "call", "item.started", { kind, ...input }),
      event(2, "call", "item.delta", { field: "output", text: "partial\n" }),
    ];
    const live = restore([]);
    expect(live.applyEvents(events)).toBe("applied");
    expect(getCommandRenderData(tools(live)[0]!.call)).toMatchObject({ command: "fixture", cwd: "/repo", stdout: "partial\n", status: "running" });

    const output = "partial\nfinal\n[output truncated]\n";
    const completed = event(3, "call", "item.completed", { kind, ...input, status: "completed", output, exit_code: 0, duration_ms: 120 });
    live.applyEvents([completed]);
    const data = getCommandRenderData(tools(live)[0]!.call);
    expect(data).toMatchObject({ command: "fixture", stdout: output, status: "completed" });
    if (kind === "command") expect(data).toMatchObject({ exitCode: 0, durationMs: 120 });
    expect(getCommandRenderData(tools(restore([...events, completed]))[0]!.call)).toEqual(data);
  });

  it("keeps concurrent results and failure details with their call IDs", () => {
    const conversation = restore([]);
    conversation.applyEvents([
      event(1, "a", "item.started", { kind: "command", command: "fixture-a" }),
      event(2, "b", "item.started", { kind: "command", command: "fixture-b" }),
      event(3, "b", "item.completed", { kind: "command", command: "fixture-b", status: "failed", output: "B\n", exit_code: 2, error: { kind: "tool_error", message: "fixture failed" } }),
      event(4, "a", "item.delta", { field: "output", text: "A\n" }),
    ]);
    const calls = new Map(tools(conversation).map(({ callId, call }) => [callId, call]));
    expect(getCommandRenderData(calls.get("a")!)).toMatchObject({ command: "fixture-a", stdout: "A\n", status: "running" });
    expect(getCommandRenderData(calls.get("b")!)).toMatchObject({ command: "fixture-b", stdout: "B\n", exitCode: 2, status: "failed" });
    expect(calls.get("b")!.error).toMatchObject({ message: "fixture failed" });
  });

  it("passes file changes, MCP text, and generated images to their existing renderers", () => {
    const conversation = restore([
      event(1, "file", "item.completed", { kind: "file_change", status: "completed", changes: [{ path: "a.ts", kind: "update", diff: "@@ -1 +1 @@\n-a\n+b\n" }] }),
      event(2, "mcp", "item.completed", { kind: "tool_call", status: "completed", server: "fixture", tool: "search", input: {}, output: '{"matches":[1,2]}' }),
      event(3, "image", "item.completed", { kind: "tool_call", status: "completed", tool: "ImageGeneration", input: { prompt: "fixture" }, output: JSON.stringify({ savedPath: "/repo/image.png", imageMimeType: "image/png" }) }),
    ]);
    const calls = new Map(tools(conversation).map(({ callId, call }) => [callId, call]));
    expect(getFileChangeRenderData(calls.get("file")!)).toMatchObject([{ path: "a.ts", added: 1, removed: 1 }]);
    expect(getMcpRenderData(calls.get("mcp")!)).toMatchObject({ output: '{"matches":[1,2]}' });
    expect(getImageGenerationRenderData(calls.get("image")!)).toEqual({ prompt: "fixture", imageUri: "/repo/image.png", mimeType: "image/png" });
  });
});
