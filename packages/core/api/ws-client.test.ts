import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";

// Constants must match ws-client.ts — if upstream changes pongWait (60 s) or
// pingPeriod (54 s), update HEARTBEAT_TIMEOUT (currently 75 s) accordingly.
const HEARTBEAT_CHECK_INTERVAL = 30_000;
const HEARTBEAT_TIMEOUT = 75_000;

// Capture URL passed to WebSocket so we can assert the connect-time
// query string.  We don't simulate the full WS lifecycle here — only the
// upgrade URL construction, which is what carries client identity.
class FakeWebSocket {
  static lastUrl: string | null = null;
  // Fields read by WSClient.connect()/disconnect(), all no-op here.
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0;
  constructor(url: string) {
    FakeWebSocket.lastUrl = url;
  }
  close() {}
  send() {}
}

describe("WSClient", () => {
  beforeEach(() => {
    FakeWebSocket.lastUrl = null;
    vi.stubGlobal("WebSocket", FakeWebSocket as unknown as typeof WebSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("includes client identity in the upgrade URL when configured", () => {
    const ws = new WSClient("ws://example.test/ws", {
      identity: { platform: "desktop", version: "1.2.3", os: "macos" },
    });
    ws.setAuth("tok", "acme");
    ws.connect();

    const url = new URL(FakeWebSocket.lastUrl!);
    expect(url.searchParams.get("workspace_slug")).toBe("acme");
    expect(url.searchParams.get("client_platform")).toBe("desktop");
    expect(url.searchParams.get("client_version")).toBe("1.2.3");
    expect(url.searchParams.get("client_os")).toBe("macos");
    // Token must never appear in the URL — it is delivered as the first
    // WS message in token mode.
    expect(url.searchParams.has("token")).toBe(false);
  });

  it("omits client_* params when identity is not configured", () => {
    const ws = new WSClient("ws://example.test/ws");
    ws.setAuth("tok", "acme");
    ws.connect();

    const url = new URL(FakeWebSocket.lastUrl!);
    expect(url.searchParams.has("client_platform")).toBe(false);
    expect(url.searchParams.has("client_version")).toBe(false);
    expect(url.searchParams.has("client_os")).toBe(false);
  });

  it("only includes the identity fields that are set", () => {
    const ws = new WSClient("ws://example.test/ws", {
      identity: { platform: "cli" },
    });
    ws.setAuth("tok", "acme");
    ws.connect();

    const url = new URL(FakeWebSocket.lastUrl!);
    expect(url.searchParams.get("client_platform")).toBe("cli");
    expect(url.searchParams.has("client_version")).toBe(false);
    expect(url.searchParams.has("client_os")).toBe(false);
  });
});

// ─── Heartbeat detection ───────────────────────────────────────────────────

// Full-lifecycle fake — tracks close() calls and lets tests inject messages.
class HeartbeatFakeWebSocket {
  static instance: HeartbeatFakeWebSocket | null = null;
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  close = vi.fn(() => {
    // Mirror real browser behavior: fire onclose so reconnect logic runs.
    this.onclose?.();
  });
  send = vi.fn();
  constructor(_url: string) {
    HeartbeatFakeWebSocket.instance = this;
  }
  simulateOpen() {
    this.onopen?.();
  }
  simulateMessage(data: string) {
    this.onmessage?.({ data });
  }
}

describe("WSClient heartbeat detection", () => {
  beforeEach(() => {
    HeartbeatFakeWebSocket.instance = null;
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", HeartbeatFakeWebSocket as unknown as typeof WebSocket);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  function makeConnected(): { client: WSClient; socket: HeartbeatFakeWebSocket } {
    // cookieAuth=true skips the first-message auth handshake so onAuthenticated
    // fires immediately on open, keeping tests simple.
    const client = new WSClient("ws://example.test/ws", { cookieAuth: true });
    client.setAuth(null, "acme");
    client.connect();
    const socket = HeartbeatFakeWebSocket.instance!;
    socket.simulateOpen();
    return { client, socket };
  }

  it("force-closes the socket after HEARTBEAT_TIMEOUT with no messages", () => {
    const { client, socket } = makeConnected();
    // Advance past one full check interval beyond the timeout.
    // Interval fires at 30 s and 60 s (both < 75 s, no close), then at 90 s
    // (90 s > 75 s → close).
    vi.advanceTimersByTime(HEARTBEAT_TIMEOUT + HEARTBEAT_CHECK_INTERVAL);
    expect(socket.close).toHaveBeenCalledTimes(1);
    client.disconnect();
  });

  it("does not close when a message arrives within the timeout window", () => {
    const { client, socket } = makeConnected();
    // Advance to just before the first interval tick.
    vi.advanceTimersByTime(HEARTBEAT_CHECK_INTERVAL - 1_000);
    // Simulate any inbound event — resets lastMessageTime.
    socket.simulateMessage(JSON.stringify({ type: "issue:updated", payload: {} }));
    // Advance another full check interval; the window was just reset so
    // Date.now() - lastMessageTime ≪ HEARTBEAT_TIMEOUT.
    vi.advanceTimersByTime(HEARTBEAT_CHECK_INTERVAL);
    expect(socket.close).not.toHaveBeenCalled();
    client.disconnect();
  });

  it("heartbeat frames reset lastMessageTime and prevent timeout", () => {
    const { client, socket } = makeConnected();
    // Drift to just inside the timeout without any messages.
    vi.advanceTimersByTime(HEARTBEAT_TIMEOUT - 1_000);
    // Server sends an app-level heartbeat frame.
    socket.simulateMessage(JSON.stringify({ type: "heartbeat" }));
    // Advance one more check interval; elapsed since heartbeat is well under 75 s.
    vi.advanceTimersByTime(HEARTBEAT_CHECK_INTERVAL);
    expect(socket.close).not.toHaveBeenCalled();
    client.disconnect();
  });

  it("heartbeat frames are discarded — no event handlers called", () => {
    const { client, socket } = makeConnected();
    const anyHandler = vi.fn();
    client.onAny(anyHandler);
    socket.simulateMessage(JSON.stringify({ type: "heartbeat" }));
    expect(anyHandler).not.toHaveBeenCalled();
    client.disconnect();
  });

  it("stops the heartbeat timer on disconnect — no close after teardown", () => {
    const { client, socket } = makeConnected();
    // Disconnect cancels the interval.
    client.disconnect();
    const callsAfterDisconnect = socket.close.mock.calls.length;
    // Advance well past HEARTBEAT_TIMEOUT — timer must not fire.
    vi.advanceTimersByTime(HEARTBEAT_TIMEOUT + HEARTBEAT_CHECK_INTERVAL);
    expect(socket.close).toHaveBeenCalledTimes(callsAfterDisconnect);
  });
});
