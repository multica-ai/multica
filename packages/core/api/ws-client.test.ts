import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";
import type { WSMessage } from "../types/events";

// Capture URL passed to WebSocket so we can assert the connect-time
// query string.  We don't simulate the full WS lifecycle here — only the
// upgrade URL construction, which is what carries client identity.
class FakeWebSocket {
  static lastUrl: string | null = null;
  static lastInstance: FakeWebSocket | null = null;
  // Constants of the real WebSocket class. WSClient branches on
  // WebSocket.OPEN; heartbeat tests also need CLOSED to emulate the
  // browser having completed a close so repeated probes stay no-ops.
  static readonly OPEN = 1;
  static readonly CLOSED = 3;
  // Fields read by WSClient.connect()/disconnect(), all no-op here.
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0;
  constructor(url: string) {
    FakeWebSocket.lastUrl = url;
    FakeWebSocket.lastInstance = this;
  }
  close() {}
  sent: string[] = [];
  send(frame: string) {
    this.sent.push(frame);
  }
}

describe("WSClient", () => {
  beforeEach(() => {
    FakeWebSocket.lastUrl = null;
    FakeWebSocket.lastInstance = null;
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

  it("truncates the logged payload when an unparseable frame is large", () => {
    const logger = {
      debug: vi.fn(),
      info: vi.fn(),
      warn: vi.fn(),
      error: vi.fn(),
    };
    const ws = new WSClient("ws://example.test/ws", { logger });
    ws.connect();

    const huge = "x".repeat(5000);
    FakeWebSocket.lastInstance!.onmessage?.({ data: huge });

    expect(logger.warn).toHaveBeenCalledTimes(1);
    const [, summary] = logger.warn.mock.calls[0] as [string, string];
    expect(summary.length).toBeLessThan(huge.length);
    expect(summary).toContain("truncated");
    expect(summary).toContain("5000");
    expect(summary.startsWith("x".repeat(200))).toBe(true);
  });

  it("logs and skips malformed frames without breaking later messages", () => {
    const logger = {
      debug: vi.fn(),
      info: vi.fn(),
      warn: vi.fn(),
      error: vi.fn(),
    };
    const ws = new WSClient("ws://example.test/ws", { logger });
    const handler = vi.fn();
    ws.on("issue:updated", handler);
    ws.connect();

    expect(() => {
      FakeWebSocket.lastInstance!.onmessage?.({ data: `{"type":"issue` });
    }).not.toThrow();

    FakeWebSocket.lastInstance!.onmessage?.({
      data: JSON.stringify({
        type: "issue:updated",
        payload: { id: "issue-1" },
      }),
    });

    expect(logger.warn).toHaveBeenCalledWith(
      "ws: received unparseable message",
      `{"type":"issue`,
    );
    expect(handler).toHaveBeenCalledWith(
      { id: "issue-1" },
      undefined,
      undefined,
    );
  });

  it("drops frames without a string type without throwing, and keeps dispatching", () => {
    // Regression for MUL-3418: a frame whose parsed JSON lacks a string `type`
    // (an out-of-protocol frame, or a bare JSON primitive) used to throw an
    // uncaught TypeError out of onmessage via `msg.type.split(...)` in a
    // downstream onAny handler, flooding `$exception` telemetry.
    const logger = {
      debug: vi.fn(),
      info: vi.fn(),
      warn: vi.fn(),
      error: vi.fn(),
    };
    const ws = new WSClient("ws://example.test/ws", { logger });

    // A downstream consumer that assumes a string type, exactly like the
    // realtime sync's onAny dispatcher.
    const anyHandler = vi.fn((msg: WSMessage) => msg.type.split(":")[0]);
    ws.onAny(anyHandler);
    const issueHandler = vi.fn();
    ws.on("issue:updated", issueHandler);
    ws.connect();

    const badFrames = [
      JSON.stringify({ payload: {} }), // object, no type
      "42", // bare number
      "true", // bare bool
      "[]", // array
    ];
    for (const data of badFrames) {
      expect(() => {
        FakeWebSocket.lastInstance!.onmessage?.({ data });
      }).not.toThrow();
    }

    // Bad frames never reached any handler.
    expect(anyHandler).not.toHaveBeenCalled();
    expect(issueHandler).not.toHaveBeenCalled();

    // A valid frame after the bad ones still dispatches normally.
    FakeWebSocket.lastInstance!.onmessage?.({
      data: JSON.stringify({ type: "issue:updated", payload: { id: "i-1" } }),
    });
    expect(issueHandler).toHaveBeenCalledWith({ id: "i-1" }, undefined, undefined);
    expect(anyHandler).toHaveBeenCalledTimes(1);

    // The drop is logged at most once per connection despite four bad frames.
    expect(logger.warn).toHaveBeenCalledTimes(1);
    expect(logger.warn.mock.calls[0]?.[0]).toBe(
      "ws: dropping frame without a string type",
    );
  });

  it("passes actor_id and actor_type to event handlers", () => {
    const ws = new WSClient("ws://example.test/ws");
    ws.setAuth("tok", "acme");
    ws.connect();

    const handler = vi.fn();
    ws.on("issue:created", handler);

    const fakeWs = (ws as any).ws as FakeWebSocket;
    fakeWs.onmessage?.({
      data: JSON.stringify({
        type: "issue:created",
        payload: { id: "issue-1" },
        actor_id: "user-123",
        actor_type: "user",
      }),
    });

    expect(handler).toHaveBeenCalledWith(
      { id: "issue-1" },
      "user-123",
      "user",
    );
  });

  // ── Reconnect backoff tests ────────────────────────────────────────

  describe("reconnect backoff", () => {
    let setTimeoutSpy: ReturnType<typeof vi.spyOn>;

    beforeEach(() => {
      vi.useFakeTimers();
      setTimeoutSpy = vi.spyOn(globalThis, "setTimeout");
    });

    afterEach(() => {
      setTimeoutSpy.mockRestore();
      vi.useRealTimers();
    });

    /** Return the delay (ms) passed to the most recent setTimeout call. */
    function lastTimerDelay(): number {
      const calls = setTimeoutSpy.mock.calls;
      return calls[calls.length - 1]?.[1] as number;
    }

    function simulateDisconnect() {
      FakeWebSocket.lastInstance!.onclose?.();
    }

    /** Simulate the server acknowledging the auth token. In token mode the
     *  client sends `{type:"auth"}` on open, and the server replies with
     *  `{type:"auth_ack"}`. Only then does `onAuthenticated()` fire and
     *  reset the reconnect counter. */
    function simulateAuthAck() {
      FakeWebSocket.lastInstance!.onmessage?.({
        data: JSON.stringify({ type: "auth_ack" }),
      });
    }

    it("uses ~1000ms base delay for the first reconnect attempt", () => {
      // Pin Math.random so jitter is deterministic: random()=0.5 → jitter=0.
      vi.stubGlobal(
        "Math",
        new Proxy(Math, {
          get(target, prop) {
            if (prop === "random") return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient("ws://example.test/ws");
      ws.connect();
      simulateDisconnect();

      expect(lastTimerDelay()).toBe(1000);
    });

    it("doubles the base delay on consecutive failures (exponential)", () => {
      vi.stubGlobal(
        "Math",
        new Proxy(Math, {
          get(target, prop) {
            if (prop === "random") return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient("ws://example.test/ws");
      ws.connect();

      // Attempt 1: base = 1000 * 2^0 = 1000
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(1000);

      // Fire the timer → connect() → new FakeWebSocket → simulate disconnect
      vi.advanceTimersByTime(1000);
      // Attempt 2: base = 1000 * 2^1 = 2000
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(2000);

      vi.advanceTimersByTime(2000);
      // Attempt 3: base = 1000 * 2^2 = 4000
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(4000);

      vi.advanceTimersByTime(4000);
      // Attempt 4: base = 1000 * 2^3 = 8000
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(8000);
    });

    it("caps the delay at 30s even after many failures", () => {
      vi.stubGlobal(
        "Math",
        new Proxy(Math, {
          get(target, prop) {
            if (prop === "random") return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient("ws://example.test/ws");
      ws.connect();

      // Drive through enough failures to exceed the cap:
      // 2^5 = 32000 > 30000, so attempt 6 should be capped.
      const delays = [1000, 2000, 4000, 8000, 16000];
      for (const d of delays) {
        simulateDisconnect();
        expect(lastTimerDelay()).toBe(d);
        vi.advanceTimersByTime(d);
      }

      // Attempt 6: 1000 * 2^5 = 32000 → capped to 30000
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(30000);
    });

    it("applies jitter so delays vary with Math.random", () => {
      // Stub Math.random to alternate between 0 (jitter = -20%) and
      // 1 (jitter = +20%), producing deterministic min/max delays.
      let callCount = 0;
      vi.stubGlobal(
        "Math",
        new Proxy(Math, {
          get(target, prop) {
            if (prop === "random") return () => (callCount++ % 2 === 0 ? 0 : 1);
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient("ws://example.test/ws");

      // First disconnect: random()=0 → jitter = 1000 * 0.2 * (0*2-1) = -200 → 800ms
      ws.connect();
      simulateDisconnect();
      const delay1 = lastTimerDelay();

      // Second disconnect: random()=1 → jitter = 1000 * 0.2 * (1*2-1) = +200 → 1200ms
      vi.clearAllTimers();
      ws.disconnect();
      ws.connect();
      simulateDisconnect();
      const delay2 = lastTimerDelay();

      // The two delays must differ and fall within [800, 1200].
      expect(delay1).toBe(800);
      expect(delay2).toBe(1200);
    });

    it("resets the attempt counter on successful authentication", () => {
      vi.stubGlobal(
        "Math",
        new Proxy(Math, {
          get(target, prop) {
            if (prop === "random") return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient("ws://example.test/ws");
      ws.setAuth("tok", "acme");
      ws.connect();

      // Two failures: delays 1000, 2000
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(1000);
      vi.advanceTimersByTime(1000);

      simulateDisconnect();
      expect(lastTimerDelay()).toBe(2000);
      vi.advanceTimersByTime(2000);

      // Successful connection resets the counter.
      simulateAuthAck();

      // Next failure should be back to the base delay.
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(1000);
    });

    it("notifies reconnect when the initial connection succeeds only after a retry", () => {
      const ws = new WSClient("ws://example.test/ws");
      ws.setAuth("tok", "acme");
      const onReconnect = vi.fn();
      ws.onReconnect(onReconnect);
      ws.connect();

      simulateDisconnect();
      vi.runOnlyPendingTimers();
      simulateAuthAck();

      expect(onReconnect).toHaveBeenCalledTimes(1);
    });

    it("keeps retrying indefinitely with capped delay", () => {
      vi.stubGlobal(
        "Math",
        new Proxy(Math, {
          get(target, prop) {
            if (prop === "random") return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const logger = {
        debug: vi.fn(),
        info: vi.fn(),
        warn: vi.fn(),
        error: vi.fn(),
      };
      const ws = new WSClient("ws://example.test/ws", { logger });
      ws.connect();

      // Drive past the old 20-attempt limit — all should schedule a reconnect.
      for (let i = 0; i < 25; i++) {
        simulateDisconnect();
        const delay = lastTimerDelay();
        // Once past attempt 5 (base > 30000), delay should be capped at 30s.
        if (i >= 5) {
          expect(delay).toBe(30000);
        }
        vi.advanceTimersByTime(delay);
      }

      // The 26th disconnect should STILL schedule a reconnect (no give-up).
      const timerCountBefore = setTimeoutSpy.mock.calls.length;
      simulateDisconnect();
      expect(setTimeoutSpy.mock.calls.length).toBe(timerCountBefore + 1);
      expect(lastTimerDelay()).toBe(30000);
      // No "giving up" error should have been logged.
      expect(logger.error).not.toHaveBeenCalled();
    });

    it("disconnect() cancels a pending reconnect and resets the counter", () => {
      vi.stubGlobal(
        "Math",
        new Proxy(Math, {
          get(target, prop) {
            if (prop === "random") return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const ws = new WSClient("ws://example.test/ws");
      ws.connect();

      // Two failures to bump the counter.
      simulateDisconnect();
      vi.advanceTimersByTime(1000);
      simulateDisconnect();
      // A reconnect timer is now pending.

      // Explicit disconnect should cancel the pending timer.
      ws.disconnect();
      vi.advanceTimersByTime(10_000);
      // No new WebSocket should have been created after the disconnect.
      // The last URL should still be from the second connect() call.

      // A fresh connect() after disconnect should start from attempt 0.
      ws.connect();
      simulateDisconnect();
      expect(lastTimerDelay()).toBe(1000);
    });
  });

  // ── Half-open heartbeat tests ───────────────────────────────────────
  //
  // A half-open socket is one where the browser still reports OPEN and
  // onclose never fires, but nothing the server sends ever arrives. Only
  // inbound frames prove the path alive, so every test below drives the
  // fake clock frame-by-frame: 45s ticks send {"type":"ping"}, and a socket
  // silent for 135s must be force-closed into the normal reconnect path.

  describe("half-open heartbeat", () => {
    interface OpenSocket {
      client: WSClient;
      ws: FakeWebSocket;
    }

    /** Connect and fully open the socket. With no token set, onopen
     *  authenticates immediately, so startHeartbeat() is running. */
    function openSocket(): OpenSocket {
      const client = new WSClient("ws://localhost:8080/ws");
      client.connect();
      const ws = FakeWebSocket.lastInstance!;
      ws.readyState = FakeWebSocket.OPEN;
      ws.onopen?.();
      return { client, ws };
    }

    /** Count app-level ping frames the client has sent. */
    function pingCount(ws: FakeWebSocket): number {
      return ws.sent.filter((frame) => frame === JSON.stringify({ type: "ping" }))
        .length;
    }

    /** Spy on close() that also emulates the browser completing the close —
     *  a real socket leaves OPEN synchronously, so a repeated checkHealth()
     *  must not close twice. */
    function spyClose(ws: FakeWebSocket) {
      return vi
        .spyOn(ws, "close")
        .mockImplementation(() => (ws.readyState = FakeWebSocket.CLOSED));
    }

    beforeEach(() => {
      vi.useFakeTimers();
    });

    afterEach(() => {
      vi.useRealTimers();
    });

    it("sends an app-level ping every 45s on an authenticated socket", () => {
      const { ws } = openSocket();
      expect(pingCount(ws)).toBe(0);
      vi.advanceTimersByTime(45_000);
      expect(pingCount(ws)).toBe(1);
      vi.advanceTimersByTime(45_000);
      expect(pingCount(ws)).toBe(2);
    });

    it("force-closes a silent socket after 135s and reconnects via the normal path", () => {
      // Pin jitter so the first reconnect delay is exactly the 1000ms base
      // (800–1200ms otherwise, see scheduleReconnect) — same approach as the
      // pending-reconnect test above.
      vi.stubGlobal(
        "Math",
        new Proxy(Math, {
          get(target, prop) {
            if (prop === "random") return () => 0.5;
            return (target as any)[prop];
          },
        }),
      );

      const { ws } = openSocket();
      const closeSpy = spyClose(ws);

      // Two heartbeat ticks pass with no inbound frames: still tolerated.
      vi.advanceTimersByTime(90_000);
      expect(closeSpy).not.toHaveBeenCalled();

      // The 135s tick hits the silence window — force-close.
      vi.advanceTimersByTime(45_000);
      expect(closeSpy).toHaveBeenCalledTimes(1);

      // The forced close must hand over to the standard recovery chain:
      // onclose → backoff → connect. The reconnect creates a new socket.
      ws.onclose?.();
      vi.advanceTimersByTime(1_000);
      expect(FakeWebSocket.lastInstance).not.toBe(ws);
    });

    it("keeps a healthy connection alive while business frames keep arriving", () => {
      const { ws } = openSocket();
      const closeSpy = spyClose(ws);

      // Busy socket: a frame every 40s keeps resetting the silence clock,
      // well past the 135s wall-clock window with no forced close.
      for (let i = 0; i < 4; i++) {
        vi.advanceTimersByTime(40_000);
        ws.onmessage?.({
          data: JSON.stringify({ type: "issue:updated", payload: {} }),
        });
      }
      expect(closeSpy).not.toHaveBeenCalled();
    });

    it("counts pong replies alone as proof of life on an otherwise idle socket", () => {
      const { ws } = openSocket();
      const closeSpy = spyClose(ws);

      // Idle socket: no business traffic, only the ping → pong round trips
      // the heartbeat itself generates.
      for (let i = 0; i < 4; i++) {
        vi.advanceTimersByTime(45_000);
        ws.onmessage?.({ data: JSON.stringify({ type: "pong" }) });
      }
      expect(closeSpy).not.toHaveBeenCalled();
    });

    it("stops probing once disconnect() has been called", () => {
      const { client, ws } = openSocket();
      client.disconnect();
      const sentAfterDisconnect = ws.sent.slice();

      // No pings, no forced close, no reconnect attempt: the interval is
      // gone and the socket was already detached and closed.
      vi.advanceTimersByTime(300_000);
      expect(ws.sent).toEqual(sentAfterDisconnect);
      expect(FakeWebSocket.lastInstance).toBe(ws);
    });

    it("checkHealth() is a safe manual probe: silent inside the window, closes past it", () => {
      const { client, ws } = openSocket();
      const closeSpy = spyClose(ws);

      vi.advanceTimersByTime(130_000);
      client.checkHealth();
      expect(closeSpy).not.toHaveBeenCalled();

      // Cross the window: the t=135s tick fires it.
      vi.advanceTimersByTime(6_000);
      expect(closeSpy).toHaveBeenCalledTimes(1);

      // Idempotent: once the socket has left OPEN, more probes are no-ops.
      client.checkHealth();
      expect(closeSpy).toHaveBeenCalledTimes(1);
    });
  });
});

// A socket can be reconnecting hours after the provider built this client —
// long enough for a sliding session to have been renewed. Sending the token
// captured at construction would authenticate the new connection with a
// credential on its way out (MUL-7436).
describe("WSClient session renewal", () => {
  beforeEach(() => {
    FakeWebSocket.lastUrl = null;
    FakeWebSocket.lastInstance = null;
    vi.stubGlobal("WebSocket", FakeWebSocket as unknown as typeof WebSocket);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("reads the current token on every connect", () => {
    let current = "token-v1";
    const ws = new WSClient("ws://example.test/ws", {
      getToken: () => current,
    });
    ws.setAuth("token-v1", "acme");

    ws.connect();
    FakeWebSocket.lastInstance!.onopen!();
    expect(JSON.parse(FakeWebSocket.lastInstance!.sent[0]!)).toEqual({
      type: "auth",
      payload: { token: "token-v1" },
    });

    // Session renewed between connections.
    current = "token-v2";

    ws.connect();
    FakeWebSocket.lastInstance!.onopen!();
    expect(JSON.parse(FakeWebSocket.lastInstance!.sent[0]!)).toEqual({
      type: "auth",
      payload: { token: "token-v2" },
    });
  });

  it("sends no auth frame in cookie mode, where the cookie rides the upgrade", () => {
    const ws = new WSClient("ws://example.test/ws", {
      cookieAuth: true,
      getToken: () => "should-not-be-used",
    });
    ws.setAuth(null, "acme");

    ws.connect();
    FakeWebSocket.lastInstance!.onopen!();

    expect(FakeWebSocket.lastInstance!.sent).toEqual([]);
  });
});
