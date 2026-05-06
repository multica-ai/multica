import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient } from "./ws-client";

// Capture URL passed to WebSocket so we can assert the connect-time
// query string.  We don't simulate the full WS lifecycle here — only the
// upgrade URL construction, which is what carries client identity.
class FakeWebSocket {
  static lastUrl: string | null = null;
  static instances: FakeWebSocket[] = [];
  // Fields read by WSClient.connect()/disconnect(), all no-op here.
  onopen: (() => void) | null = null;
  onmessage: ((ev: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readyState = 0;
  constructor(url: string) {
    FakeWebSocket.lastUrl = url;
    FakeWebSocket.instances.push(this);
  }
  close() {}
  send() {}
}

describe("WSClient", () => {
  beforeEach(() => {
    FakeWebSocket.lastUrl = null;
    FakeWebSocket.instances = [];
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

  it("falls back to an authenticated HTTP SSE stream when the initial WebSocket closes before auth", async () => {
    const fetchMock = vi.fn(() => new Promise<Response>(() => {}));
    vi.stubGlobal("fetch", fetchMock);

    const ws = new WSClient("wss://api.example.test/ws", {
      identity: { platform: "web" },
    });
    ws.setAuth("secret-token", "acme");
    ws.connect();

    FakeWebSocket.instances[0]!.onclose?.();

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const [url, init] = fetchMock.mock.calls[0]! as unknown as [
      string,
      RequestInit,
    ];
    const sseURL = new URL(url);
    expect(sseURL.protocol).toBe("https:");
    expect(sseURL.pathname).toBe("/sse");
    expect(sseURL.searchParams.get("workspace_slug")).toBe("acme");
    expect(sseURL.searchParams.get("client_platform")).toBe("web");
    expect(init.headers).toMatchObject({
      Accept: "text/event-stream",
      Authorization: "Bearer secret-token",
    });
    expect(init.credentials).toBe("same-origin");

    ws.disconnect();
  });

  it("uses cookie credentials for the SSE fallback in cookie-auth mode", async () => {
    const fetchMock = vi.fn(() => new Promise<Response>(() => {}));
    vi.stubGlobal("fetch", fetchMock);

    const ws = new WSClient("ws://api.example.test/ws", { cookieAuth: true });
    ws.setAuth(null, "acme");
    ws.connect();

    FakeWebSocket.instances[0]!.onclose?.();

    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));
    const [url, init] = fetchMock.mock.calls[0]! as unknown as [
      string,
      RequestInit,
    ];
    const sseURL = new URL(url);
    expect(sseURL.protocol).toBe("http:");
    expect(sseURL.pathname).toBe("/sse");
    expect(init.headers).toMatchObject({ Accept: "text/event-stream" });
    expect(init.headers).not.toHaveProperty("Authorization");
    expect(init.credentials).toBe("include");

    ws.disconnect();
  });

  it("warns instead of silently dropping sends while using receive-only SSE", async () => {
    const fetchMock = vi.fn(() => new Promise<Response>(() => {}));
    vi.stubGlobal("fetch", fetchMock);
    const warn = vi.fn();

    const ws = new WSClient("wss://api.example.test/ws", {
      logger: {
        debug: vi.fn(),
        info: vi.fn(),
        warn,
        error: vi.fn(),
      },
    });
    ws.setAuth("secret-token", "acme");
    ws.connect();

    FakeWebSocket.instances[0]!.onclose?.();
    await vi.waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    ws.send({ type: "subscribe", payload: {} } as never);
    expect(warn).toHaveBeenCalledWith(
      "cannot send realtime frame while using receive-only SSE fallback",
      "subscribe",
    );

    ws.disconnect();
  });
});
