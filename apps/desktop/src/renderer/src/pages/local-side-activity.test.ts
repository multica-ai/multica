// @vitest-environment node
import { describe, expect, it } from "vitest";
import { localV2SnapshotSchema } from "@multica/core/local-conversation/v2";
import { V2Conversation } from "@multica/core/local-conversation/conversation";
import { localSideActivity } from "./local-side-activity";

const at = "2026-10-27T09:00:00Z";
type Item = { id: string; kind: string; status?: string; state?: Record<string, unknown> };

/** A Side whose last turn is `current`; an earlier turn holds a previous answer. */
function conversation(status: string, items: Item[] = [], turns = true) {
  let seq = 0;
  const events = (turnId: string, list: Item[]) => list.flatMap((item) => [
    { seq: ++seq, turn_id: turnId, item_id: item.id, type: "item.started", final: false, created_at: at, payload: { kind: item.kind } },
    ...(item.status && item.status !== "in_progress" ? [{ seq: ++seq, turn_id: turnId, item_id: item.id, type: "item.completed", final: true, created_at: at, payload: { kind: item.kind, status: item.status, ...item.state } }]
      : item.state ? [{ seq: ++seq, turn_id: turnId, item_id: item.id, type: "item.updated", final: false, created_at: at, payload: { kind: item.kind, status: "in_progress", ...item.state } }] : []),
  ]);
  const old = events("old", [{ id: "old-text", kind: "assistant_text", status: "completed", state: { text: "Previous result" } }]);
  const current = events("current", items);
  return V2Conversation.fromSnapshot(localV2SnapshotSchema.parse({
    version: 2, store_generation: "g", snapshot_seq: seq,
    conversation: { conversation_id: "side", summary_seq: 1, provider: "claude", title: "Source title is not a work summary", status: status === "running" ? "running" : "idle", pinned: false, archived: false, updated_at: at },
    turns: turns ? [
      { id: "old", turn_index: 0, request_id: "r0", status: "completed", started_at: "2026-10-27T08:00:00Z", completed_at: at, messages: [], events: old },
      { id: "current", turn_index: 1, request_id: "r1", status, started_at: at, ...(status === "running" ? {} : { completed_at: at }), messages: [], events: current },
    ] : [],
  }));
}

describe("side activity", () => {
  it.each(["running", "completed", "failed", "cancelled"] as const)("uses the actual %s turn status without borrowing previous output", (state) => {
    expect(localSideActivity(conversation(state))).toMatchObject({ state, spinning: state === "running", output: "" });
  });
  it("has no invented summary for an empty conversation", () => {
    expect(localSideActivity()).toMatchObject({ state: "idle", output: "" });
    expect(localSideActivity(conversation("completed", [], false))).toMatchObject({ state: "idle", output: "" });
  });
  it("shows tool progress, but never tool arguments, results, or reasoning content", () => {
    const tools: Item[] = [
      { id: "read", kind: "tool_call", status: "completed", state: { tool: "Read", input: { file_path: "/secret" }, output: "sensitive output" } },
      { id: "bash", kind: "command", state: { command: "secret command" } },
    ];
    expect(localSideActivity(conversation("running", tools))).toMatchObject({ tool: "Bash", completedTools: 1, totalTools: 2, output: "" });
    const thinking = conversation("running", [...tools, { id: "think", kind: "reasoning", state: { text: "private reasoning" } }]);
    expect(localSideActivity(thinking)).toMatchObject({ thinking: true, tool: undefined, output: "" });
    expect(JSON.stringify(localSideActivity(thinking))).not.toMatch(/secret|private|sensitive/);
  });
  it("shows the current turn's answer", () => {
    expect(localSideActivity(conversation("running", [{ id: "t", kind: "assistant_text", state: { text: "Checked the layout." } }])).output).toBe("Checked the layout.");
    expect(localSideActivity(conversation("completed", [{ id: "t", kind: "assistant_text", status: "completed", state: { text: "Fixed the layout." } }])).output).toBe("Fixed the layout.");
  });
  it("distinguishes an unanswered question from active processing", () => {
    const session = conversation("running");
    session.interactions.set("q", { id: "q", turnId: "current", kind: "question", request: {}, status: "pending", answer: null, createdAt: at, updatedAt: at });
    expect(localSideActivity(session)).toMatchObject({ state: "waiting", spinning: false });
  });
  it("keeps running background work visible after its turn finished", () => {
    const session = conversation("completed", [{ id: "bg", kind: "background_task", state: { description: "Verify dark theme" } }]);
    expect(localSideActivity(session)).toMatchObject({ state: "running", spinning: true, background: "Verify dark theme" });
    session.items.get("bg")!.status = "completed";
    expect(localSideActivity(session)).toMatchObject({ state: "completed", spinning: false });
  });
  it("does not display the last turn as the activity of a newly dispatching prompt", () => {
    const session = conversation("completed", [{ id: "read", kind: "tool_call", status: "completed", state: { tool: "Read" } }, { id: "t", kind: "assistant_text", status: "completed", state: { text: "Old result" } }]);
    session.queue = { revision: 1, paused: false, items: [], dispatching_request_id: "next" };
    expect(localSideActivity(session)).toMatchObject({ state: "running", output: "", tool: undefined, totalTools: 0 });
  });
  it("retains errors and partial text after a failure or cancellation", () => {
    const failed = conversation("failed", [{ id: "t", kind: "assistant_text", state: { text: "Partial result" } }]);
    failed.turns.get("current")!.error = { kind: "provider", message: "Connection interrupted" };
    expect(localSideActivity(failed)).toMatchObject({ state: "failed", spinning: false, error: "Connection interrupted", output: "Partial result" });
    failed.turns.get("current")!.status = "cancelled";
    expect(localSideActivity(failed)).toMatchObject({ state: "cancelled", spinning: false, error: undefined, output: "Partial result" });
  });
});
