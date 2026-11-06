// @vitest-environment node
import { expect, it, vi } from "vitest";
import { localV2SummarySchema } from "@multica/core/local-conversation/v2";
import type { CompletionReadRecord } from "../models";
import { createFakeTransport } from "../testing/fake-transport";
import { ConversationService } from "./conversation-service";

const at = "2026-11-06T12:00:00Z";
const summary = (seq: number, running: boolean, count = 1) => localV2SummarySchema.parse({
  conversation_id: "c", summary_seq: seq, provider: "codex", title: "c", status: running ? "running" : "idle",
  active_turn_id: running ? `t${count - 1}` : undefined, turn_count: count, pinned: false, archived: false, updated_at: at,
});
const turn = (index: number, status = "completed") => ({ id: `t${index}`, turn_index: index, request_id: `r${index}`, status, started_at: at, completed_at: at, messages: [], events: [] });

function fixture(options: { saved?: Map<string, CompletionReadRecord>; initial?: ReturnType<typeof summary>; generation?: string } = {}) {
  const saved = options.saved ?? new Map<string, CompletionReadRecord>();
  const state = { summary: options.initial ?? summary(1, true), generation: options.generation ?? "g", turns: [turn(0)], older: false, history: [] as ReturnType<typeof turn>[] };
  const requests = vi.fn((_method: string, path: string) => {
    if (path.includes("/snapshot")) return { version: 2, store_generation: state.generation, snapshot_seq: state.summary.summary_seq, conversation: state.summary, turns: state.turns, has_older_turns: state.older };
    if (path.includes("/history")) return { store_generation: state.generation, turns: state.history, has_older_turns: false };
    return { scope_seq: state.summary.summary_seq, summaries: [state.summary], has_more: false };
  });
  const transport = createFakeTransport({ userId: "u", workspaceId: "w" }, requests);
  transport.storeGeneration = async () => state.generation;
  const storage = {
    read: vi.fn(async (id: string) => saved.get(id) ?? null),
    write: vi.fn(async (id: string, record: CompletionReadRecord) => { saved.set(id, { ...record }); }),
  };
  const service = new ConversationService(transport, { userId: "u", workspaceId: "w" }, {
    completionStorage: storage, deviceId: "local", snapshots: null, onGeneration: () => {}, onUsed: () => {},
  });
  const idle = () => service.completions!.settled();
  const load = async () => { await service.loadSummaries(); await idle(); };
  const update = async (next: ReturnType<typeof summary>, turns = [turn(next.turn_count - 1)]) => {
    state.summary = next; state.turns = turns;
    service.receive({ version: 2, type: "scope.summaries", scope_seq: next.summary_seq, summaries: [next] });
    await idle();
  };
  return { saved, state, service, storage, requests, load, update, idle };
}

it("creates a background completion with no content view or content subscription", async () => {
  const f = fixture();
  await f.load();
  expect(f.requests).toHaveBeenCalledTimes(1);
  expect(f.service.completions!.pending("c")).toBeUndefined();
  await f.update(summary(2, false));
  expect(f.service.completions!.pending("c")).toMatchObject({ completedTurnId: "t0", completedTurnIndex: 0 });
  expect(f.service.conversation("c")).toBeUndefined();
  f.service.dispose();
});

it("does not read old history or treat a rename as a new result", async () => {
  const f = fixture({ initial: summary(1, false, 100) });
  await f.load();
  await f.update({ ...summary(2, false, 100), title: "Renamed" });
  expect(f.requests).toHaveBeenCalledTimes(1);
  expect(f.service.completions!.pending("c")).toBeUndefined();
  f.service.dispose();
});

it("clears only the displayed completion; duplicate, stale and later summaries cannot revive it", async () => {
  const f = fixture();
  await f.load();
  await f.update(summary(2, false));
  const shown = f.service.completions!.pending("c")!;
  f.service.completions!.markRead("c", shown);
  await f.idle();
  await f.update(summary(2, false));
  await f.update(summary(1, true));
  expect(f.service.completions!.pending("c")).toBeUndefined();
  await f.update(summary(3, true, 2), [turn(0), turn(1, "running")]);
  await f.update(summary(4, false, 2));
  f.service.completions!.markRead("c", shown);
  expect(f.service.completions!.pending("c")?.completedTurnId).toBe("t1");
  await f.idle(); f.service.dispose();
});

it("retains both unread and read positions over restarts", async () => {
  const first = fixture();
  await first.load(); await first.update(summary(2, false)); first.service.dispose();
  const second = fixture({ saved: first.saved, initial: summary(2, false) });
  await second.load();
  expect(second.service.completions!.pending("c")?.completedTurnId).toBe("t0");
  second.service.completions!.markRead("c", second.service.completions!.pending("c")!);
  await second.idle(); second.service.dispose();
  const third = fixture({ saved: first.saved, initial: summary(2, false) });
  await third.load();
  expect(third.service.completions!.pending("c")).toBeUndefined();
  third.service.dispose();
});

it.each([true, false])("reconciles a completion missed while closed (previously running: %s)", async (running) => {
  const first = fixture({ initial: summary(1, running, running ? 1 : 0) });
  await first.load(); first.service.dispose();
  const second = fixture({ saved: first.saved, initial: summary(3, false) });
  await second.load();
  expect(second.service.completions!.pending("c")?.completedTurnId).toBe("t0");
  second.service.dispose();
});

it("detects a finished queued turn when the next turn is already running", async () => {
  const f = fixture();
  await f.load();
  await f.update(summary(3, true, 2), [turn(0), turn(1, "running")]);
  expect(f.service.completions!.pending("c")?.completedTurnId).toBe("t0");
  f.service.dispose();
});

it.each(["failed", "cancelled"])("does not count a %s turn as a successful new result", async (status) => {
  const f = fixture();
  await f.load(); await f.update(summary(2, false), [turn(0, status)]);
  expect(f.service.completions!.pending("c")).toBeUndefined();
  f.service.dispose();
});

it("finds a missed success before a window of failed turns without crossing the baseline", async () => {
  const first = fixture(); await first.load(); first.service.dispose();
  const f = fixture({ saved: first.saved, initial: summary(20, false, 8) });
  f.state.turns = [turn(6, "failed"), turn(7, "failed")]; f.state.older = true;
  f.state.history = [turn(0), turn(1, "failed")];
  await f.load();
  expect(f.service.completions!.pending("c")?.completedTurnId).toBe("t0");
  expect(f.requests.mock.calls.some(([, path]) => path.includes("/history"))).toBe(true);
  f.service.dispose();
});

it("discards old positions when the daemon database generation changes", async () => {
  const first = fixture(); await first.load(); await first.update(summary(2, false)); first.service.dispose();
  const f = fixture({ saved: first.saved, initial: summary(1, false), generation: "replacement" });
  await f.load();
  expect(f.service.completions!.pending("c")).toBeUndefined();
  expect(f.saved.get("c")?.generation).toBe("replacement");
  f.service.dispose();
});

it("records storage failures instead of claiming a durable read position", async () => {
  const warn = vi.spyOn(console, "warn").mockImplementation(() => {});
  const f = fixture(); f.storage.write.mockRejectedValueOnce(new Error("Disk unavailable"));
  await f.load();
  expect(f.service.list.completions.error).toBe("Disk unavailable");
  expect(f.saved.size).toBe(0);
  await f.update(summary(1, true));
  expect(f.saved.get("c")?.summarySeq).toBe(1);
  expect(warn).toHaveBeenCalled(); warn.mockRestore(); f.service.dispose();
});
