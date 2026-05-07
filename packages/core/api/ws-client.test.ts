import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";

// Shared FakeWebSocket — combines main's liveness-monitor needs (instances
// array, fake timers, close/send mocks) with HEAD's identity-URL inspection
// (every constructed instance captures its url so we can assert query params).
class FakeWebSocket {
  static CONNECTING = 0;
  static OPEN = 1;
  static CLOSING = 2;
  static CLOSED = 3;
  readyState = FakeWebSocket.OPEN;
  onopen: ((e: unknown) => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  send = vi.fn();
  close = vi.fn(() => {
    if (this.readyState === FakeWebSocket.CLOSED) return;
    this.readyState = FakeWebSocket.CLOSED;
    this.onclose?.();
  });
  constructor(public url: string) {
    instances.push(this);
  }
}

let instances: FakeWebSocket[] = [];

describe("WSClient identity URL", () => {
  beforeEach(() => {
    instances = [];
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

    const url = new URL(instances[0]!.url);
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

    const url = new URL(instances[0]!.url);
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

    const url = new URL(instances[0]!.url);
    expect(url.searchParams.get("client_platform")).toBe("cli");
    expect(url.searchParams.has("client_version")).toBe(false);
    expect(url.searchParams.has("client_os")).toBe(false);
  });
});

describe("WSClient liveness monitor", () => {
  beforeEach(() => {
    instances = [];
    vi.stubGlobal("WebSocket", FakeWebSocket);
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  function makeClient() {
    const client = new WSClient("ws://test/ws", { cookieAuth: true });
    client.setAuth(null, "ws-slug");
    return client;
  }

  it("force-reconnects when no messages arrive within the stale threshold", () => {
    const client = makeClient();
    client.connect();

    expect(instances).toHaveLength(1);
    const first = instances[0]!;
    first.onopen?.(null);

    // Just under the 50s threshold — no reconnect yet.
    vi.advanceTimersByTime(40_000);
    expect(instances).toHaveLength(1);

    // Cross the threshold — the next interval tick triggers a force-reconnect.
    vi.advanceTimersByTime(20_000);
    expect(first.close).toHaveBeenCalled();
    expect(instances).toHaveLength(2);
  });

  it("does not reconnect while messages keep arriving", () => {
    const client = makeClient();
    client.connect();
    const ws = instances[0]!;
    ws.onopen?.(null);

    for (let i = 0; i < 6; i++) {
      vi.advanceTimersByTime(20_000);
      ws.onmessage?.({ data: '{"type":"server:ping","payload":{}}' });
    }

    expect(ws.close).not.toHaveBeenCalled();
    expect(instances).toHaveLength(1);
  });

  it("rechecks immediately when the tab returns to the foreground", () => {
    // Wrapped in a ref so TS can't narrow the assigned-in-closure variable
    // down to literal `null` and reject the call sites below.
    const listenerRef: { current: (() => void) | null } = { current: null };
    let visibilityState: "hidden" | "visible" = "visible";
    vi.stubGlobal("document", {
      get visibilityState() {
        return visibilityState;
      },
      addEventListener: (type: string, handler: () => void) => {
        if (type === "visibilitychange") listenerRef.current = handler;
      },
      removeEventListener: (type: string, handler: () => void) => {
        if (type === "visibilitychange" && listenerRef.current === handler)
          listenerRef.current = null;
      },
    });

    const baseline = Date.now();
    const client = makeClient();
    client.connect();
    const ws = instances[0]!;
    ws.onopen?.(null);

    visibilityState = "hidden";
    listenerRef.current?.();
    expect(ws.close).not.toHaveBeenCalled();

    // iOS suspends the JS clock while a PWA is backgrounded — setSystemTime
    // jumps the clock forward without firing the periodic interval, the same
    // way the periodic check is starved on a real device.
    vi.setSystemTime(baseline + 5 * 60_000);

    visibilityState = "visible";
    listenerRef.current?.();
    expect(ws.close).toHaveBeenCalled();
    expect(instances).toHaveLength(2);
  });

  it("cleans up the interval and visibility listener on disconnect", () => {
    const removeSpy = vi.fn();
    vi.stubGlobal("document", {
      visibilityState: "visible",
      addEventListener: vi.fn(),
      removeEventListener: removeSpy,
    });

    const client = makeClient();
    client.connect();
    instances[0]!.onopen?.(null);

    client.disconnect();

    expect(removeSpy).toHaveBeenCalledWith("visibilitychange", expect.any(Function));

    // After disconnect, the periodic check must not spawn a new socket even
    // when a long stretch of fake time elapses.
    vi.advanceTimersByTime(10 * 60_000);
    expect(instances).toHaveLength(1);
  });
});
