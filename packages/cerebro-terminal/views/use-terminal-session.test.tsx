import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { renderHook, act } from "@testing-library/react";

// --- API mock --------------------------------------------------------------

const mockGetActiveTerminalSession = vi.hoisted(() => vi.fn());
const mockTerminalAttachUrl = vi.hoisted(() =>
  vi.fn((path: string) => `ws://test.local${path}`),
);
const mockGetToken = vi.hoisted(() => vi.fn(() => null));

vi.mock("@multica/core/api", () => ({
  api: {
    getActiveTerminalSession: mockGetActiveTerminalSession,
    terminalAttachUrl: mockTerminalAttachUrl,
    getToken: mockGetToken,
  },
}));

// --- WebSocket mock --------------------------------------------------------

type MockSocketInstance = {
  url: string;
  readyState: number;
  onopen: ((ev?: Event) => void) | null;
  onmessage: ((ev: MessageEvent) => void) | null;
  onerror: ((ev?: Event) => void) | null;
  onclose: ((ev?: CloseEvent) => void) | null;
  close: () => void;
  send: (data: string) => void;
};

const createdSockets: MockSocketInstance[] = [];

class MockWebSocket implements MockSocketInstance {
  static OPEN = 1;
  static CLOSED = 3;
  url: string;
  readyState = 0;
  onopen: ((ev?: Event) => void) | null = null;
  onmessage: ((ev: MessageEvent) => void) | null = null;
  onerror: ((ev?: Event) => void) | null = null;
  onclose: ((ev?: CloseEvent) => void) | null = null;
  constructor(url: string) {
    this.url = url;
    createdSockets.push(this);
  }
  close() {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.();
  }
  send(_data: string) {}
}

// Hoist the class onto globalThis so the hook's `new WebSocket(...)` picks it up.
(globalThis as unknown as { WebSocket: typeof MockWebSocket }).WebSocket =
  MockWebSocket;
// Also expose the readyState constants the hook expects on the constructor.
Object.assign(MockWebSocket, { CONNECTING: 0, OPEN: 1, CLOSING: 2, CLOSED: 3 });

// Import after mocks are installed.
import { useTerminalSession } from "./use-terminal-session";

describe("useTerminalSession", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    mockGetActiveTerminalSession.mockReset();
    mockTerminalAttachUrl.mockClear();
    mockGetToken.mockReset();
    mockGetToken.mockReturnValue(null);
    createdSockets.length = 0;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("transitions waiting_for_agent → connected when a session appears", async () => {
    // First poll: no session. Second poll: a session.
    mockGetActiveTerminalSession
      .mockResolvedValueOnce(null)
      .mockResolvedValueOnce({
        session_id: "sess-1",
        attach_path: "/api/cerebro/terminal/sessions/sess-1/ws",
        created_at: "2026-05-14T00:00:00Z",
      });

    const { result } = renderHook(() =>
      useTerminalSession({ runtimeId: "rt-1" }),
    );

    // Drain the first poll (microtask resolves null, then schedules a timer).
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.status).toBe("waiting_for_agent");

    // Advance the poll interval; the second poll resolves a session and opens WS.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1600);
    });

    // A WebSocket was constructed against the returned attach_path.
    expect(createdSockets).toHaveLength(1);
    expect(createdSockets[0]!.url).toContain("/sessions/sess-1/ws");

    // Simulate the WS opening.
    await act(async () => {
      const ws = createdSockets[0]!;
      ws.readyState = MockWebSocket.OPEN;
      ws.onopen?.();
    });

    expect(result.current.status).toBe("connected");
  });

  it("delivers decoded stdout to subscribers, then closes on exit frame", async () => {
    mockGetActiveTerminalSession.mockResolvedValue({
      session_id: "sess-2",
      attach_path: "/api/cerebro/terminal/sessions/sess-2/ws",
      created_at: "2026-05-14T00:00:00Z",
    });

    const { result } = renderHook(() =>
      useTerminalSession({ runtimeId: "rt-2" }),
    );

    // Flush the initial poll → WS open.
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(createdSockets).toHaveLength(1);

    await act(async () => {
      const ws = createdSockets[0]!;
      ws.readyState = MockWebSocket.OPEN;
      ws.onopen?.();
    });

    const chunks: Uint8Array[] = [];
    act(() => {
      result.current.onOutput((c) => chunks.push(c));
    });

    // base64("hello") → "aGVsbG8="
    await act(async () => {
      const ws = createdSockets[0]!;
      ws.onmessage?.({
        data: JSON.stringify({ type: "stdout", data: "aGVsbG8=" }),
      } as MessageEvent);
    });

    expect(chunks).toHaveLength(1);
    expect(new TextDecoder().decode(chunks[0])).toBe("hello");

    // Exit frame.
    await act(async () => {
      const ws = createdSockets[0]!;
      ws.onmessage?.({
        data: JSON.stringify({ type: "exit", code: 0 }),
      } as MessageEvent);
    });
    expect(result.current.status).toBe("closed");
    expect(result.current.exitCode).toBe(0);
  });

  it("sendInput is a no-op in v1 (logs once, doesn't throw)", async () => {
    mockGetActiveTerminalSession.mockResolvedValue(null);
    const debugSpy = vi.spyOn(console, "debug").mockImplementation(() => {});

    const { result } = renderHook(() =>
      useTerminalSession({ runtimeId: "rt-3" }),
    );

    act(() => {
      result.current.sendInput("hello");
      result.current.sendInput("world");
    });

    // Should not throw, and should warn only once.
    expect(debugSpy).toHaveBeenCalledTimes(1);
    debugSpy.mockRestore();
  });
});
