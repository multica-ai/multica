import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";

// Capture URL passed to WebSocket so we can assert the connect-time
// query string.  We don't simulate the full WS lifecycle here — only the
// upgrade URL construction, which is what carries client identity.
class FakeWebSocket {
  static lastUrl: string | null = null;
  static lastInstance: FakeWebSocket | null = null;
  static instanceCount = 0;
  // Fields read by WSClient.connect()/disconnect(), all no-op here.
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0;
  constructor(url: string) {
    FakeWebSocket.lastUrl = url;
    FakeWebSocket.lastInstance = this;
    FakeWebSocket.instanceCount++;
  }
  close() {}
  send() {}
}

describe("WSClient", () => {
  beforeEach(() => {
    FakeWebSocket.lastUrl = null;
    FakeWebSocket.lastInstance = null;
    FakeWebSocket.instanceCount = 0;
    vi.stubGlobal("WebSocket", FakeWebSocket as unknown as typeof WebSocket);
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
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

  describe("reconnect backoff", () => {
    function setup() {
      const logger = {
        debug: vi.fn(),
        info: vi.fn(),
        warn: vi.fn(),
        error: vi.fn(),
      };
      const ws = new WSClient("ws://example.test/ws", { logger });

      // Extract the delay in seconds from the last "reconnecting in Xs" warn log.
      const getLastReconnectDelay = (): number => {
        const calls = logger.warn.mock.calls.filter((c: unknown[]) =>
          (c[0] as string).includes("reconnecting"),
        );
        const lastWarn = calls[calls.length - 1];
        if (!lastWarn) throw new Error("no reconnect warn log found");
        const match = (lastWarn[0] as string).match(/reconnecting in (\d+)s/);
        if (!match?.[1]) throw new Error("could not parse delay from: " + lastWarn[0]);
        return parseInt(match[1], 10);
      };

      // Simulate a full disconnect→reconnect cycle without authenticating.
      // This lets reconnectAttempts grow, exercising the backoff curve.
      const reconnectWithoutAuth = () => {
        FakeWebSocket.lastInstance!.onclose?.();
        const delay = getLastReconnectDelay();
        vi.advanceTimersByTime(delay * 1000 + 2000);
      };

      return { ws, logger, getLastReconnectDelay, reconnectWithoutAuth };
    }

    it("uses exponential backoff on successive disconnects", () => {
      const { ws, getLastReconnectDelay, reconnectWithoutAuth } = setup();
      ws.setAuth("tok", "acme");
      ws.connect();

      // Disconnect #1 — attempt 0: 3000 * 2^0 = 3000ms → ~3s (plus jitter).
      reconnectWithoutAuth();
      const delay1 = getLastReconnectDelay();
      expect(delay1).toBeGreaterThanOrEqual(3);
      expect(delay1).toBeLessThanOrEqual(5);

      // Disconnect #2 — attempt 1: 3000 * 2^1 = 6000ms → ~6s.
      reconnectWithoutAuth();
      const delay2 = getLastReconnectDelay();
      expect(delay2).toBeGreaterThanOrEqual(6);
      expect(delay2).toBeLessThanOrEqual(8);

      // Disconnect #3 — attempt 2: 3000 * 2^2 = 12000ms → ~12s.
      reconnectWithoutAuth();
      const delay3 = getLastReconnectDelay();
      expect(delay3).toBeGreaterThanOrEqual(12);
      expect(delay3).toBeLessThanOrEqual(14);

      // Delays must be strictly increasing.
      expect(delay2).toBeGreaterThan(delay1);
      expect(delay3).toBeGreaterThan(delay2);

      ws.disconnect();
    });

    it("caps backoff at 30 seconds", () => {
      const { ws, getLastReconnectDelay, reconnectWithoutAuth } = setup();
      ws.setAuth("tok", "acme");
      ws.connect();

      // Run through enough cycles to exceed the 30s cap.
      // attempt 4: 3000 * 2^4 = 48000 → capped at 30000.
      for (let i = 0; i < 6; i++) {
        reconnectWithoutAuth();
      }

      const cappedDelay = getLastReconnectDelay();
      expect(cappedDelay).toBeGreaterThanOrEqual(30);
      expect(cappedDelay).toBeLessThanOrEqual(32);

      ws.disconnect();
    });

    it("resets backoff counter after successful authentication", () => {
      const { ws, getLastReconnectDelay, reconnectWithoutAuth } = setup();
      ws.setAuth("tok", "acme");
      ws.connect();

      // Build up backoff: 3 disconnect/reconnect cycles without auth.
      for (let i = 0; i < 3; i++) {
        reconnectWithoutAuth();
      }
      // At this point reconnectAttempts == 3, delay would be ~12s.

      // Simulate a successful reconnect: onopen sends the auth frame, then
      // the server replies with auth_ack which triggers onAuthenticated().
      FakeWebSocket.lastInstance!.onopen?.();
      FakeWebSocket.lastInstance!.onmessage?.({
        data: JSON.stringify({ type: "auth_ack" }),
      });

      // Now disconnect — the delay should reset to ~3s.
      FakeWebSocket.lastInstance!.onclose?.();
      const resetDelay = getLastReconnectDelay();
      expect(resetDelay).toBeGreaterThanOrEqual(3);
      expect(resetDelay).toBeLessThanOrEqual(5);

      ws.disconnect();
    });
  });
});
