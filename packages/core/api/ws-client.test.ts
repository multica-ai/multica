import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";

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

  // PUL-40: connection state is observable so the UI can render a
  // "Reconnecting…" indicator. Initial state is "connecting"; flips to
  // "open" only after the auth handshake completes; flips to "reconnecting"
  // only after a close that follows at least one successful "open".
  describe("connection state", () => {
    // Drivable fake — same shape as FakeWebSocket above but exposes the
    // attached handler refs so the test can simulate the lifecycle.
    let lastFake: DrivableFakeWebSocket | null;
    class DrivableFakeWebSocket {
      onopen: (() => void) | null = null;
      onmessage: ((ev: { data: string }) => void) | null = null;
      onclose: (() => void) | null = null;
      onerror: (() => void) | null = null;
      readyState = 0;
      constructor(_url: string) {
        // Test-only: capture the most recently constructed fake for the
        // surrounding describe block. Not a true `self = this` aliasing
        // pattern (lastFake is module-scoped, not a local rebind), so
        // @typescript-eslint/no-this-alias is a false positive here.
        // eslint-disable-next-line @typescript-eslint/no-this-alias
        lastFake = this;
      }
      close() {}
      send() {}
    }

    beforeEach(() => {
      lastFake = null;
      vi.stubGlobal(
        "WebSocket",
        DrivableFakeWebSocket as unknown as typeof WebSocket,
      );
    });

    it("starts as 'connecting' and flips to 'open' after auth_ack", () => {
      const ws = new WSClient("ws://example.test/ws", { cookieAuth: true });
      ws.setAuth(null, "acme");

      const seen: string[] = [];
      ws.onConnectionStateChange((s) => seen.push(s));

      expect(ws.getConnectionState()).toBe("connecting");
      ws.connect();
      // Cookie-auth path: onopen → onAuthenticated → "open" immediately,
      // no first-message round-trip needed.
      lastFake!.onopen!();
      expect(ws.getConnectionState()).toBe("open");
      expect(seen).toEqual(["open"]);
    });

    it("flips to 'reconnecting' on close after successful connect", () => {
      vi.useFakeTimers();
      const ws = new WSClient("ws://example.test/ws", { cookieAuth: true });
      ws.setAuth(null, "acme");

      ws.connect();
      lastFake!.onopen!(); // -> "open"
      expect(ws.getConnectionState()).toBe("open");

      lastFake!.onclose!(); // socket dropped
      expect(ws.getConnectionState()).toBe("reconnecting");

      vi.useRealTimers();
    });

    it("does NOT flip to 'reconnecting' if close fires before initial 'open'", () => {
      vi.useFakeTimers();
      const ws = new WSClient("ws://example.test/ws", { cookieAuth: true });
      ws.setAuth(null, "acme");

      ws.connect();
      // Simulate a close before auth_ack (e.g. server immediately rejects
      // the handshake). The UI should not flash "Reconnecting…" during the
      // initial connect attempt — that is just connection in progress.
      lastFake!.onclose!();
      expect(ws.getConnectionState()).toBe("connecting");

      vi.useRealTimers();
    });
  });
});
