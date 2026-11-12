// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider, QueryObserver } from "@tanstack/react-query";
import { ApiClient } from "../api/client";
import { activeSyncCauses, getRealtimeObserver, holdSyncCauses, runWithSyncCauses } from "../api/realtime-observer";
import { WSClient } from "../api/ws-client";
import { chatKeys } from "../chat/queries";
import { issueKeys } from "../issues/queries";
import { useRealtimeSync, type RealtimeSyncStores } from "../realtime/use-realtime-sync";
import type { WSMessage } from "../types/events";
import { workspaceKeys } from "../workspace/queries";
import {
  dropDevConnection,
  getDevRecording,
  installDevRecorder,
  recordDevSyncStatus,
  replayDevEvent,
  resetDevRecorder,
} from "./dev-recorder";

vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => "ws-1",
  getCurrentSlug: () => "acme",
  subscribeToCurrentSlug: () => () => {},
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));

vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

class FakeSocket {
  static last: FakeSocket | null = null;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 1;
  constructor() {
    FakeSocket.last = this;
  }
  send() {}
  close() {
    this.onclose?.();
  }
}

const stores = {
  authStore: Object.assign(() => ({}), {
    getState: () => ({ user: { id: "u1" } }),
    subscribe: () => () => {},
  }),
} as unknown as RealtimeSyncStores;

function emit(msg: WSMessage) {
  FakeSocket.last!.onmessage!({ data: JSON.stringify(msg) });
}

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  installDevRecorder(qc);
  const client = new WSClient("ws://example.test/ws", { cookieAuth: true });
  client.setAuth(null, "acme");
  client.connect();
  FakeSocket.last!.onopen!();
  renderHook(() => useRealtimeSync(client, stores), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={qc}>{children}</QueryClientProvider>
    ),
  });
  return { qc, client };
}

function hash(key: readonly unknown[]) {
  return JSON.stringify(key);
}

describe("dev recorder: realtime events", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", FakeSocket as unknown as typeof WebSocket);
  });

  afterEach(() => {
    resetDevRecorder();
    vi.unstubAllGlobals();
    vi.useRealTimers();
  });

  it("tags each frame with the cache keys its handling wrote or invalidated, debounced refreshes included", () => {
    const { qc } = setup();
    qc.setQueryData(chatKeys.sessions("ws-1"), [{ id: "s1", title: "old" }]);
    qc.setQueryData(issueKeys.timeline("issue-1"), []);
    qc.setQueryData(workspaceKeys.members("ws-1"), []);

    emit({ type: "chat:session_updated", payload: { chat_session_id: "s1", title: "new" } });
    emit({ type: "comment:created", payload: { comment: { id: "c1", issue_id: "issue-1" } } });
    emit({ type: "member:updated", payload: { member: { user_id: "u2" } } });
    vi.advanceTimersByTime(100);

    const [renamed, commented, member] = getDevRecording().events;
    expect(renamed).toMatchObject({ kind: "frame", type: "chat:session_updated" });
    expect(renamed!.cache.written).toEqual([hash(chatKeys.sessions("ws-1"))]);
    expect(commented).toMatchObject({ type: "comment:created", issueId: "issue-1" });
    expect(commented!.cache.invalidated).toContain(hash(issueKeys.timeline("issue-1")));
    expect(commented!.cache.written).toEqual([]);
    // The member prefix refresh runs 100ms later, on a timer.
    expect(member!.cache.invalidated).toEqual([hash(workspaceKeys.members("ws-1"))]);
  });

  it("credits an issue edit with the issue detail it wrote", () => {
    const { qc } = setup();
    const before = { id: "issue-1", identifier: "MUL-1", title: "Old", revision: 1, status: "todo" };
    qc.setQueryData(issueKeys.detail("ws-1", "issue-1"), before);
    emit({
      type: "issue:updated",
      payload: { issue: { ...before, title: "New", revision: 2 }, title_changed: true },
    });
    const [edit] = getDevRecording().events;
    expect(edit!.cache.written).toContain(hash(issueKeys.detail("ws-1", "issue-1")));
  });

  it("keeps the issue key and redacts credentials in the payload", () => {
    setup();
    emit({
      type: "issue:updated",
      payload: { issue: { id: "issue-1", identifier: "MUL-1", title: "Fix" } },
      actor_type: "member",
      actor_id: "u2",
    });
    emit({ type: "agent:created", payload: { agent: { id: "a1", custom_env: { OPENAI_API_KEY: "fake-key-for-test" } } } });

    const [issue, agent] = getDevRecording().events;
    expect(issue).toMatchObject({ issueId: "issue-1", issueKey: "MUL-1", actor: "member u2" });
    expect(agent!.payload).not.toContain("fake-key-for-test");
    expect(JSON.parse(agent!.payload!)).toEqual({ agent: { id: "a1", custom_env: "[redacted]" } });
  });

  it("marks a dropped connection's reconnect with its downtime and the queries it refetched", async () => {
    const { qc } = setup();
    const unsubscribe = new QueryObserver(qc, {
      queryKey: workspaceKeys.list(),
      queryFn: () => Promise.resolve([]),
    }).subscribe(() => {});
    await vi.runOnlyPendingTimersAsync();
    recordDevSyncStatus("connected");

    expect(dropDevConnection()).toBe(true);
    recordDevSyncStatus("reconnecting");
    vi.advanceTimersByTime(1_500);
    FakeSocket.last!.onopen!();

    const reconnect = getDevRecording().events.at(-1)!;
    expect(reconnect.kind).toBe("reconnect");
    expect(reconnect.downMs).toBe(1_500);
    expect(reconnect.cache.invalidated).toContain(hash(workspaceKeys.list()));
    expect(reconnect.cache.refetched).toEqual([hash(workspaceKeys.list())]);
    unsubscribe();
  });

  it("replays a frame through realtime sync alone, as a new replay event", () => {
    const { qc, client } = setup();
    const otherSubscriber = vi.fn();
    client.on("comment:created", otherSubscriber);
    qc.setQueryData(issueKeys.timeline("issue-1"), []);
    emit({ type: "comment:created", payload: { comment: { id: "c1", issue_id: "issue-1" } } });
    const original = getDevRecording().events[0]!;
    qc.setQueryData(issueKeys.timeline("issue-1"), []);

    expect(replayDevEvent(original.id)).toBe(true);
    const replay = getDevRecording().events[1]!;
    expect(replay).toMatchObject({ kind: "replay", type: "comment:created", issueId: "issue-1" });
    expect(replay.cache.invalidated).toContain(hash(issueKeys.timeline("issue-1")));
    expect(otherSubscriber).toHaveBeenCalledOnce();
  });

  it("refuses write requests made while a replayed event is handled, and only then", async () => {
    setup();
    const fetchMock = vi.fn((_url: string, _init?: RequestInit) => Promise.resolve(new Response("{}", { status: 200 })));
    vi.stubGlobal("fetch", fetchMock);
    const api = new ApiClient("http://localhost:18080");
    emit({ type: "comment:created", payload: { comment: { id: "c1", issue_id: "issue-1" } } });
    replayDevEvent(getDevRecording().events[0]!.id);
    const replayId = getDevRecording().events[1]!.id;

    await expect(runWithSyncCauses([replayId], () => api.createComment("issue-1", "hi"))).rejects.toThrow(
      "POST not sent: a replayed realtime event runs locally only",
    );
    await runWithSyncCauses([replayId], () => api.listMembers("ws-1"));
    await api.createComment("issue-1", "hi");
    expect(fetchMock.mock.calls.map(([, init]) => init?.method ?? "GET")).toEqual(["GET", "POST"]);
  });

  it("forgets events that aged out of the window, and can no longer replay them", () => {
    setup();
    emit({ type: "comment:created", payload: { comment: { id: "c1", issue_id: "issue-1" } } });
    const id = getDevRecording().events[0]!.id;

    vi.advanceTimersByTime(6 * 60_000);
    expect(getDevRecording().events).toEqual([]);
    expect(replayDevEvent(id)).toBe(false);
  });

  it("tags nothing and holds no causes when the recorder is not installed", () => {
    const client = new WSClient("ws://example.test/ws", { cookieAuth: true });
    client.connect();
    FakeSocket.last!.onopen!();
    let seen: unknown[] = ["unset"];
    client.on("comment:created", () => {
      seen = [...activeSyncCauses(), ...holdSyncCauses()];
    });
    emit({ type: "comment:created", payload: {} });

    expect(getRealtimeObserver()).toBeNull();
    expect(seen).toEqual([]);
  });
});
