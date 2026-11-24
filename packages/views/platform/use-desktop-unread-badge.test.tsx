import type { ReactNode } from "react";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, focusManager } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { chatKeys } from "@multica/core/chat/queries";
import { useDesktopUnreadBadge } from "./use-desktop-unread-badge";

vi.mock("@multica/core/permissions", () => ({ useIsGuest: () => false }));

afterEach(() => {
  cleanup(); focusManager.setFocused(undefined);
  delete (window as unknown as Record<string, unknown>).desktopAPI;
  setApiInstance(undefined as unknown as ApiClient);
});

it("adds Chat messages to Inbox unread while backgrounded and clears read/logout counts", async () => {
  let chatUnread = 0;
  const listChatSessions = vi.fn(async () => []);
  const getChatSessionSummary = vi.fn(async () => ({
    active_count: 1,
    archived_count: 0,
    unread_by_session: chatUnread > 0 ? { "chat-a": chatUnread } : {},
  }));
  setApiInstance({
    getInboxUnreadSummary: async () => [{ workspace_id: "ws-a", count: 2 }],
    listChatSessions,
    getChatSessionSummary,
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
  expect(getChatSessionSummary).toHaveBeenCalledWith({ workspaceId: "ws-a" });
  expect(listChatSessions).not.toHaveBeenCalled();
  chatUnread = 3;
  await act(async () => { await qc.invalidateQueries({ queryKey: chatKeys.sessionSummary("ws-a") }); });
  await waitFor(() => expect(setUnreadBadge).toHaveBeenLastCalledWith(5));
  chatUnread = 0;
  await act(async () => { await qc.invalidateQueries({ queryKey: chatKeys.sessionSummary("ws-a") }); });
  await waitFor(() => expect(setUnreadBadge).toHaveBeenLastCalledWith(2));
  view.rerender({ wsId: null });
  await waitFor(() => expect(setUnreadBadge).toHaveBeenLastCalledWith(0));
  qc.clear();
});
