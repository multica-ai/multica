// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import type { TaskMessagePayload } from "@multica/core/types";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "@/data/api";
import { chatKeys, runTranscriptMessagesOptions } from "@/data/queries/chat";
import { issueKeys } from "@/data/queries/issue-keys";
import type { WSSubscriptionSetup } from "@/lib/use-ws-subscriptions";
import type { WSClient } from "./ws-client";
import { useRunTranscriptRealtime } from "./use-run-transcript-realtime";

const state = vi.hoisted(() => ({
  qc: undefined as unknown as QueryClient,
  setup: undefined as WSSubscriptionSetup | undefined,
}));
vi.mock("@/data/api", () => ({ api: { listTaskMessages: vi.fn() } }));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => state.qc,
}));
vi.mock("@/lib/use-ws-subscriptions", () => ({
  useWSSubscriptions: (setup: WSSubscriptionSetup) => { state.setup = setup; },
}));

const wsId = "workspace-1";
const issueId = "issue-1";
const taskId = "123e4567-e89b-12d3-a456-426614174000";
const key = chatKeys.taskMessages(taskId);
const tasksKey = issueKeys.tasks(wsId, issueId);
const cleanups: (() => void)[] = [];

function message(seq: number): TaskMessagePayload {
  return { task_id: taskId, issue_id: issueId, seq, type: "text", content: `row ${seq}` };
}

function connect() {
  useRunTranscriptRealtime(issueId, taskId);
  const handlers = new Map<string, (payload: unknown) => void>();
  let reconnect = () => {};
  const ws = {
    on: (event: string, handler: (payload: unknown) => void) => {
      handlers.set(event, handler);
      return () => { handlers.delete(event); };
    },
    onReconnect: (handler: () => void) => {
      reconnect = handler;
      return () => { reconnect = () => {}; };
    },
  };
  const cleanup = () => { for (const unsubscribe of unsubscribes) unsubscribe(); };
  const unsubscribes = state.setup!(ws as unknown as WSClient, wsId) ?? [];
  cleanups.push(cleanup);
  return {
    emit: (event: string, payload: unknown) => { handlers.get(event)?.(payload); },
    reconnect: () => reconnect(),
    cleanup,
  };
}

function open() {
  const observer = new QueryObserver(state.qc, runTranscriptMessagesOptions(taskId));
  cleanups.push(observer.subscribe(() => {}));
  return observer;
}

async function expectRows(seqs: number[]) {
  await vi.waitFor(() => {
    expect(state.qc.getQueryState(key)?.fetchStatus).toBe("idle");
    expect(state.qc.getQueryData<TaskMessagePayload[]>(key)?.map((m) => m.seq)).toEqual(seqs);
  });
}

describe("run transcript cache lifecycle", () => {
  beforeEach(() => {
    state.qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: Infinity } } });
    state.setup = undefined;
    vi.mocked(api.listTaskMessages).mockReset().mockResolvedValue([message(1)]);
    state.qc.setQueryData(tasksKey, []);
  });
  afterEach(() => {
    for (const cleanup of cleanups.splice(0)) cleanup();
    state.qc.clear();
  });

  it("backfills every open even when a cancelled run has an Infinity-fresh cache", async () => {
    state.qc.setQueryData(key, [message(1)]);
    vi.mocked(api.listTaskMessages).mockResolvedValue([message(1), message(2)]);
    open();
    await expectRows([1, 2]);
    expect(api.listTaskMessages).toHaveBeenCalledWith(taskId, { signal: expect.any(AbortSignal) });
  });

  it("backfills a warm cache when the task lookup resolves after the screen mounts", async () => {
    state.qc.setQueryData(key, [message(1)]);
    vi.mocked(api.listTaskMessages).mockResolvedValue([message(1), message(2)]);
    const observer = new QueryObserver(state.qc, runTranscriptMessagesOptions(undefined));
    cleanups.push(observer.subscribe(() => {}));
    expect(api.listTaskMessages).not.toHaveBeenCalled();
    observer.setOptions(runTranscriptMessagesOptions(taskId));
    await expectRows([1, 2]);
  });

  it("accepts the final flush after cancellation, in order and without duplicates", async () => {
    const ws = connect();
    open();
    await expectRows([1]);
    ws.emit("task:cancelled", { task_id: taskId, issue_id: issueId });
    await expectRows([1]);
    ws.emit("task:message", message(3));
    ws.emit("task:message", message(2));
    ws.emit("task:message", message(2));
    await expectRows([1, 2, 3]);
  });

  it.each(["task:completed", "task:failed", "task:cancelled", "reconnect"])(
    "backfills messages and run metadata on %s", async (event) => {
      const ws = connect();
      open();
      await expectRows([1]);
      vi.mocked(api.listTaskMessages).mockResolvedValue([message(1), message(2)]);
      if (event === "reconnect") ws.reconnect();
      else ws.emit(event, { task_id: taskId, issue_id: issueId });
      await expectRows([1, 2]);
      expect(state.qc.getQueryState(tasksKey)?.isInvalidated).toBe(true);
    },
  );

  it("keeps live rows when an older HTTP snapshot finishes afterward", async () => {
    let resolve!: (rows: TaskMessagePayload[]) => void;
    vi.mocked(api.listTaskMessages).mockImplementationOnce(() => new Promise((done) => { resolve = done; }));
    const ws = connect();
    open();
    ws.emit("task:message", message(2));
    resolve([message(1)]);
    await expectRows([1, 2]);

    // Persisted data wins for an existing seq, while absent seqs survive.
    const persisted = { ...message(2), content: "persisted correction" };
    vi.mocked(api.listTaskMessages).mockResolvedValue([persisted]);
    ws.reconnect();
    await vi.waitFor(() => expect(state.qc.getQueryData<TaskMessagePayload[]>(key)?.[1]).toEqual(persisted));
    await expectRows([1, 2]);
  });

  it("starts a fresh request if a terminal event arrives during the first load", async () => {
    let resolveOld!: (rows: TaskMessagePayload[]) => void;
    vi.mocked(api.listTaskMessages)
      .mockImplementationOnce(() => new Promise((done) => { resolveOld = done; }))
      .mockResolvedValue([message(1), message(2)]);
    const ws = connect();
    open();
    ws.emit("task:completed", { task_id: taskId, issue_id: issueId });
    resolveOld([message(1)]);
    await expectRows([1, 2]);
    expect(api.listTaskMessages).toHaveBeenCalledTimes(2);
  });

  it("preserves realtime rows and avoids an error flash when cancelling an old fetch", async () => {
    vi.mocked(api.listTaskMessages)
      .mockImplementationOnce(() => new Promise(() => {}))
      .mockResolvedValue([message(1)]);
    const ws = connect();
    const observer = open();
    const statuses: string[] = [];
    cleanups.push(observer.subscribe((result) => { statuses.push(result.status); }));
    ws.emit("task:message", message(2));
    ws.emit("task:completed", { task_id: taskId, issue_id: issueId });
    await expectRows([1, 2]);
    expect(statuses).not.toContain("error");
    expect(api.listTaskMessages).toHaveBeenCalledTimes(2);
  });

  it("ignores other runs/issues and stops responding after cleanup", async () => {
    const ws = connect();
    open();
    await expectRows([1]);
    for (const payload of [
      { ...message(2), task_id: "other-task" },
      { ...message(2), issue_id: "other-issue" },
    ]) {
      ws.emit("task:message", payload);
      ws.emit("task:cancelled", payload);
    }
    expect(api.listTaskMessages).toHaveBeenCalledTimes(1);
    expect(state.qc.getQueryState(tasksKey)?.isInvalidated).toBe(false);
    ws.cleanup();
    ws.emit("task:message", message(2));
    ws.emit("task:completed", message(2));
    ws.reconnect();
    await expectRows([1]);
    expect(api.listTaskMessages).toHaveBeenCalledTimes(1);
  });
});
