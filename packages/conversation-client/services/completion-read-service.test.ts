// @vitest-environment node
import { expect, it, vi } from "vitest";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import { CompletionReadModel } from "../models/completion-read-model";
import { CompletionReadService } from "./completion-read-service";

const summary = (seq: number, completed = 0, read = -1) => localV2SummarySchema.parse({
  conversation_id: "c", summary_seq: seq, provider: "codex", title: "c", status: "idle",
  turn_count: completed + 1, pinned: false, archived: false, updated_at: "now",
  completion: { turn_id: `t${completed}`, turn_index: completed, completion_seq: completed + 1 }, read_turn_index: read, read_completion_seq: read + 1,
  has_unread_completion: completed > read,
});
function fixture() {
  const model = new CompletionReadModel();
  const state = { generation: "g", response: summary(2, 0, 0) };
  const read = vi.fn(async () => state.response);
  const service = new CompletionReadService(model, { generation: async () => state.generation, read });
  const observe = async (value: ReturnType<typeof summary>) => { service.observe(value); await service.settled(); };
  return { model, state, read, service, observe };
}

it("projects authoritative read positions received from another client without fetching history", async () => {
  const f = fixture();
  await f.observe(summary(1));
  expect(f.service.pending("c")?.completedTurnId).toBe("t0");
  await f.observe(summary(2, 0, 0));
  expect(f.service.pending("c")).toBeUndefined();
  expect(f.read).not.toHaveBeenCalled();
  f.service.dispose();
});

it("waits for acknowledgement and ignores an older response after a newer completion", async () => {
  const f = fixture();
  await f.observe(summary(1));
  const shown = f.service.pending("c")!;
  f.service.markRead("c", shown);
  expect(f.service.pending("c")).toBeDefined();
  f.service.observe(summary(3, 1, 0));
  await f.service.settled();
  expect(f.read).toHaveBeenCalledWith("c", shown);
  expect(f.service.pending("c")?.completedTurnId).toBe("t1");
  f.service.markRead("c", shown);
  await f.service.settled();
  expect(f.read).toHaveBeenCalledTimes(1);
  f.service.dispose();
});

it("keeps the unread result after failure and permits a later retry", async () => {
  const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  const f = fixture(); await f.observe(summary(1));
  const shown = f.service.pending("c")!;
  f.read.mockRejectedValueOnce(new Error("offline"));
  f.service.markRead("c", shown); await f.service.settled();
  expect(f.service.pending("c")).toBeDefined();
  expect(f.model.error).toBe("offline");
  f.service.markRead("c", shown); await f.service.settled();
  expect(f.service.pending("c")).toBeUndefined();
  f.service.dispose(); warn.mockRestore();
});

it("rejects positions from another store generation and does not infer unread on old responses", async () => {
  const f = fixture(); await f.observe(summary(1));
  const shown = f.service.pending("c")!;
  f.service.markRead("c", { ...shown, generation: "old" });
  await f.service.settled(); expect(f.read).not.toHaveBeenCalled();
  f.service.reset(); f.state.generation = "replacement";
  const old = summary(1); delete old.completion; delete old.read_turn_index; delete old.has_unread_completion;
  await f.observe(old);
  expect(f.service.pending("c")).toBeUndefined();
  f.service.dispose();
});

it("retains a lower-index turn that completes after the previously read turn", async () => {
  const f = fixture();
  await f.observe({ ...summary(5, 0, 1), completion: {turn_id: "t0", turn_index: 0, completion_seq: 4}, read_completion_seq: 3 });
  expect(f.service.pending("c")?.completedTurnId).toBe("t0");
  f.service.dispose();
});
