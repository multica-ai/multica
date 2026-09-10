// @vitest-environment jsdom
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { WSClient } from "../api/ws-client";
import { useTaskMessages } from "../chat/queries";
import type { TaskMessagePayload } from "../types/events";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

vi.mock("../api", () => ({ api: { listTaskMessages: vi.fn() } }));
vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => "ws-1", getCurrentSlug: () => "test-ws",
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));
vi.mock("../paths", () => ({ useHasOnboarded: () => true, resolvePostAuthDestination: () => "/" }));

class FakeWebSocket {
  static last: FakeWebSocket;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onclose: (() => void) | null = null;
  onerror: (() => void) | null = null;
  constructor() { FakeWebSocket.last = this; }
  close() {}
  send() {}
}

const id = "11111111-1111-4111-8111-111111111111";
const msg = (seq: number): TaskMessagePayload => ({ task_id: id, issue_id: "issue-1", seq, type: "text", content: `fragment ${seq}` });
const stores = { authStore: Object.assign(() => ({}), {
  getState: () => ({ user: { id: "u1" } }), subscribe: () => () => {}, setState: () => {},
}) } as unknown as RealtimeSyncStores;

afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.clearAllMocks(); });

describe("timeline repair on connect", () => {
  it("recovers a row persisted between the transcript fetch and the first handshake", async () => {
    // The fetch and the subscription are not ordered. seq 2 is written while
    // the socket is still connecting, so the response never saw it and its
    // broadcast reached nobody here. `staleTime: Infinity` means nothing would
    // fetch it again, and the run stays live and visible throughout — so no
    // mount or liveness transition fires either. Only the connection itself
    // marks the point where this client's view could have a hole in it.
    vi.stubGlobal("WebSocket", FakeWebSocket);
    const ws = new WSClient("ws://example.test/ws", { cookieAuth: true });
    ws.connect();
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false, refetchOnWindowFocus: false } } });
    const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;

    vi.mocked(api.listTaskMessages).mockResolvedValue([msg(1)]);
    const { result, unmount } = renderHook(
      () => { useRealtimeSync(ws, stores); return useTaskMessages(id, true); },
      { wrapper },
    );
    await waitFor(() => expect(result.current.data?.map((m) => m.seq)).toEqual([1]));

    vi.mocked(api.listTaskMessages).mockResolvedValue([msg(1), msg(2), msg(3)]);
    act(() => {
      FakeWebSocket.last.onopen?.();
      FakeWebSocket.last.onmessage?.({ data: JSON.stringify({ type: "task:message", payload: msg(3) }) });
    });

    await waitFor(() => expect(result.current.data?.map((m) => m.seq)).toEqual([1, 2, 3]));
    unmount(); ws.disconnect(); qc.clear();
  });
});
