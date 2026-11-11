// @vitest-environment jsdom
import { afterEach, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { ApiError, setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { useChatSessionList, useCurrentChatSession } from "./use-session-list";
import type { ChatSession } from "../types";
import { chatKeys } from "./queries";

let qc: QueryClient;
function wrapper({ children }: { children: ReactNode }) { return <QueryClientProvider client={qc}>{children}</QueryClientProvider>; }
afterEach(() => qc.clear());
function setup() { qc = new QueryClient({ defaultOptions: { queries: { retry: false } } }); }
const row = (id: string): ChatSession => ({ id, workspace_id: "ws", agent_id: "agent", creator_id: "user", title: id, status: "active", has_unread: false, created_at: "", updated_at: "2026-01-01T00:00:00Z" });

it("fetches archive pages only while enabled, including after invalidation", async () => {
  setup();
  const list = vi.fn().mockResolvedValue({ sessions: [], has_more: false, next_cursor: null });
  setApiInstance({ listChatSessionsPage: list } as unknown as ApiClient);
  const { rerender } = renderHook(({ enabled }) => useChatSessionList("ws", "archived", enabled), { wrapper, initialProps: { enabled: false } });
  await act(async () => { await qc.invalidateQueries({ queryKey: chatKeys.sessions("ws") }); });
  expect(list).not.toHaveBeenCalled();
  rerender({ enabled: true });
  await waitFor(() => expect(list).toHaveBeenCalledTimes(1));
  rerender({ enabled: false });
  await act(async () => { await qc.invalidateQueries({ queryKey: chatKeys.sessions("ws") }); });
  expect(list).toHaveBeenCalledTimes(1);
});

it("deduplicates rows when activity changes across cursor pages", async () => {
  setup();
  const list = vi.fn().mockResolvedValueOnce({ sessions: [row("one")], has_more: true, next_cursor: "next" }).mockResolvedValueOnce({ sessions: [row("one"), row("two")], has_more: false, next_cursor: null });
  setApiInstance({ listChatSessionsPage: list } as unknown as ApiClient);
  const { result } = renderHook(() => useChatSessionList("ws"), { wrapper });
  await waitFor(() => expect(result.current.data).toHaveLength(1));
  await act(async () => { await result.current.fetchNextPage(); });
  await waitFor(() => expect(result.current.data.map((s) => s.id)).toEqual(["one", "two"]));
});

it("resolves an old deep link independently of the loaded page", async () => {
  setup();
  setApiInstance({ getChatSession: vi.fn().mockResolvedValue(row("old")) } as unknown as ApiClient);
  const { result } = renderHook(() => useCurrentChatSession("ws", "old", [row("first")]), { wrapper });
  await waitFor(() => expect(result.current.data?.id).toBe("old"));
  expect(result.current.unavailable).toBe(false);
});

it.each([403, 404, 500])("only clears inaccessible details, not transient HTTP %i errors", async (status) => {
  setup();
  setApiInstance({ getChatSession: vi.fn().mockRejectedValue(new ApiError("demo failure", status, "Error")) } as unknown as ApiClient);
  const { result } = renderHook(() => useCurrentChatSession("ws", "old", []), { wrapper });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.unavailable).toBe(status === 403 || status === 404);
});
