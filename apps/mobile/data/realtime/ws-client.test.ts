// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";

class MockWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: MockWebSocket[] = [];

  readyState = MockWebSocket.CONNECTING;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: (() => void) | null = null;
  readonly sent: string[] = [];

  constructor(readonly url: string) {
    MockWebSocket.instances.push(this);
  }

  open() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.();
  }

  receive(frame: unknown) {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }

  send(frame: string) {
    this.sent.push(frame);
  }

  close() {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.();
  }
}

function connectAuthenticatedClient() {
  const client = new WSClient({
    url: "wss://example.test/ws",
    token: "token",
    workspaceSlug: "workspace",
  });
  client.connect();
  const socket = MockWebSocket.instances[0];
  socket.open();
  socket.receive({ type: "auth_ack" });
  return { client, socket };
}

describe("WSClient application heartbeat", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", MockWebSocket);
    MockWebSocket.instances = [];
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("reconnects a stale OPEN socket through the jittered backoff path", () => {
    vi.spyOn(Math, "random").mockReturnValue(0);
    const { client, socket } = connectAuthenticatedClient();

    expect(socket.sent.map((frame) => JSON.parse(frame))).toEqual([
      { type: "auth", payload: { token: "token" } },
      { type: "ping" },
    ]);

    // No pong arrives even though the JS-visible readyState remains OPEN.
    vi.advanceTimersByTime(10_000);
    vi.advanceTimersByTime(1);

    expect(MockWebSocket.instances).toHaveLength(2);
    client.disconnect();
  });

  it("keeps a healthy socket connected when its pong arrives", () => {
    const { client, socket } = connectAuthenticatedClient();
    socket.receive({ type: "pong" });

    vi.advanceTimersByTime(10_000);

    expect(MockWebSocket.instances).toHaveLength(1);
    client.disconnect();
  });
});

// A phone reconnects constantly — after every backgrounding, every network
// switch. By the time it does, a sliding session may have been renewed, and
// the token captured when this client was built is on its way out (MUL-7436).
describe("WSClient session renewal", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", MockWebSocket);
    MockWebSocket.instances = [];
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("authenticates each connection with the current token", () => {
    let current = "token-v1";
    const client = new WSClient({
      url: "wss://example.test/ws",
      token: "token-v1",
      workspaceSlug: "workspace",
      getToken: () => current,
    });

    client.connect();
    MockWebSocket.instances[0].open();
    expect(JSON.parse(MockWebSocket.instances[0].sent[0])).toEqual({
      type: "auth",
      payload: { token: "token-v1" },
    });

    current = "token-v2";
    client.forceReconnect();
    const reconnected = MockWebSocket.instances[MockWebSocket.instances.length - 1];
    reconnected.open();

    expect(JSON.parse(reconnected.sent[0])).toEqual({
      type: "auth",
      payload: { token: "token-v2" },
    });
  });

  it("falls back to the constructor token when no reader is supplied", () => {
    const client = new WSClient({
      url: "wss://example.test/ws",
      token: "token-only",
      workspaceSlug: "workspace",
    });

    client.connect();
    MockWebSocket.instances[0].open();

    expect(JSON.parse(MockWebSocket.instances[0].sent[0])).toEqual({
      type: "auth",
      payload: { token: "token-only" },
    });
  });
});

describe("WSClient frame validation", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", MockWebSocket);
    MockWebSocket.instances = [];
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it.each([
    null,
    false,
    42,
    "event",
    [],
    {},
    { error: "unauthorized" },
    { type: null },
    { type: 42 },
    { type: {} },
    { type: "" },
  ])("ignores invalid envelopes and still delivers the next event: %j", (frame) => {
    const { client, socket } = connectAuthenticatedClient();
    const onAny = vi.fn();
    const onRunning = vi.fn();
    client.onAny(onAny);
    client.on("task:running", onRunning);

    expect(() => socket.receive(frame)).not.toThrow();
    expect(onAny).not.toHaveBeenCalled();
    expect(onRunning).not.toHaveBeenCalled();

    const payload = { task_id: "task-1", agent_id: "agent-1", issue_id: "issue-1" };
    const message = { type: "task:running", payload, actor_id: "actor-1" };
    socket.receive(message);

    expect(onRunning).toHaveBeenCalledExactlyOnceWith(payload, "actor-1");
    expect(onAny).toHaveBeenCalledExactlyOnceWith(message);
    client.disconnect();
  });
});

describe("WSClient foreground recovery", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", MockWebSocket);
    MockWebSocket.instances = [];
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it.each(["paused", "active"])("opens one fresh socket on returning from %s", (state) => {
    const { client, socket } = connectAuthenticatedClient();
    const onReconnect = vi.fn();
    client.onReconnect(onReconnect);
    if (state === "paused") client.pause();

    client.resume();

    expect(MockWebSocket.instances).toHaveLength(2);
    expect(socket.readyState).toBe(MockWebSocket.CLOSED);
    expect(onReconnect).not.toHaveBeenCalled();
    const resumed = MockWebSocket.instances[1];
    resumed.open();
    resumed.receive({ type: "auth_ack" });
    resumed.receive({ type: "pong" });
    expect(onReconnect).toHaveBeenCalledTimes(1);

    vi.advanceTimersByTime(10_001);
    expect(MockWebSocket.instances).toHaveLength(2);
    client.disconnect();
  });

  it("cancels a pending reconnect when returning to the foreground", () => {
    vi.spyOn(Math, "random").mockReturnValue(0.5);
    const { client, socket } = connectAuthenticatedClient();
    socket.close();

    client.resume();

    expect(MockWebSocket.instances).toHaveLength(2);
    vi.advanceTimersByTime(2_000);
    expect(MockWebSocket.instances).toHaveLength(2);
    client.disconnect();
  });

  it("does not resume after disconnect or wake a paused client on a network edge", () => {
    const { client } = connectAuthenticatedClient();
    client.pause();
    client.forceReconnect();
    vi.advanceTimersByTime(60_000);
    expect(MockWebSocket.instances).toHaveLength(1);

    client.disconnect();
    client.resume();
    client.forceReconnect();
    vi.advanceTimersByTime(60_000);
    expect(MockWebSocket.instances).toHaveLength(1);
  });
});
