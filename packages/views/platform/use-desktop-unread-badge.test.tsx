import type { ReactNode } from "react";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, focusManager } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { chatKeys } from "@multica/core/chat/queries";
import type { ChatSession } from "@multica/core/types";
import { useDesktopUnreadBadge } from "./use-desktop-unread-badge";

afterEach(() => {
  cleanup(); focusManager.setFocused(undefined);
  delete (window as unknown as Record<string, unknown>).desktopAPI;
  setApiInstance(undefined as unknown as ApiClient);
});

it("adds Chat messages to Inbox unread while backgrounded and clears read/logout counts", async () => {
  let chatUnread = 0;
  const listChatSessions = vi.fn(async () => [{ id: "chat-a", unread_count: chatUnread }] as ChatSession[]);
  setApiInstance({
    getInboxUnreadSummary: async () => [{ workspace_id: "ws-a", count: 2 }],
    listChatSessions,
  } as unknown as ApiClient);
  const setUnreadBadge = vi.fn();
  Object.defineProperty(window, "desktopAPI", { configurable: true, value: { setUnreadBadge } });
  focusManager.setFocused(false);
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  const view = renderHook(({ wsId }: { wsId: string | null }) => useDesktopUnreadBadge(wsId), {
    wrapper, initialProps: { wsId: "ws-a" as string | null },
  });
  await waitFor(() => expect(setUnreadBadge).toHaveBeenLastCalledWith(2));
  chatUnread = 3;
  await act(async () => { await qc.invalidateQueries({ queryKey: chatKeys.sessions("ws-a") }); });
  await waitFor(() => expect(setUnreadBadge).toHaveBeenLastCalledWith(5));
  chatUnread = 0;
  await act(async () => { await qc.invalidateQueries({ queryKey: chatKeys.sessions("ws-a") }); });
  await waitFor(() => expect(setUnreadBadge).toHaveBeenLastCalledWith(2));
  view.rerender({ wsId: null });
  await waitFor(() => expect(setUnreadBadge).toHaveBeenLastCalledWith(0));
  qc.clear();
});
