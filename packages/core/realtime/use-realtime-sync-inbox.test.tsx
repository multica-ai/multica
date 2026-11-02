/** @vitest-environment jsdom */
import { QueryClientProvider, useQuery } from "@tanstack/react-query";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { WSClient } from "../api/ws-client";
import {
  deduplicateInboxItems,
  inboxKeys,
  inboxListOptions,
  useInboxUnreadCount,
} from "../inbox/queries";
import { createQueryClient } from "../query-client";
import type { InboxItem } from "../types";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => "ws-1",
  getCurrentSlug: () => "test-ws",
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));
vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

it("refreshes an inactive Inbox after inbox:new then issue:updated (MUL-7286)", async () => {
  const qc = createQueryClient();
  const oldItem: InboxItem = {
    id: "n1",
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "u1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue 1",
    body: null,
    issue_status: "todo",
    read: true,
    archived: false,
    created_at: "2026-09-11T00:00:00Z",
    details: null,
  };
  let rows = [oldItem];
  const listInbox = vi.fn(async () => rows);
  setApiInstance({
    listInbox,
    getInboxUnreadSummary: async () => [
      {
        workspace_id: "ws-1",
        count: deduplicateInboxItems(rows).filter((item) => !item.read).length,
      },
    ],
  } as unknown as ApiClient);

  const handlers: Record<string, (payload: unknown) => void> = {};
  const ws = {
    on: (event: string, handler: (payload: unknown) => void) => {
      handlers[event] = handler;
      return () => {
        delete handlers[event];
      };
    },
    onAny: () => () => {},
    onReconnect: () => () => {},
  } as unknown as WSClient;
  const stores = {
    authStore: { getState: () => ({ user: { id: "u1" } }) },
  } as unknown as RealtimeSyncStores;
  function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  }
  vi.spyOn(document, "hasFocus").mockReturnValue(true);

  const shell = renderHook(
    () => {
      useRealtimeSync(ws, stores);
      return useInboxUnreadCount("ws-1");
    },
    { wrapper: Wrapper },
  );
  const mountInbox = () =>
    renderHook(() => useQuery(inboxListOptions("ws-1")), { wrapper: Wrapper });
  let page = mountInbox();
  try {
    await waitFor(() => expect(page.result.current.data).toEqual([oldItem]));
    // Switching tabs unmounts the page; its cache stays (tab titles hold
    // disabled observers on it), so the return reuses it.
    page.unmount();
    const newItem = {
      ...oldItem,
      id: "n2",
      read: false,
      created_at: "2026-09-11T00:01:00Z",
    };
    rows = [newItem, oldItem];
    await act(async () => {
      handlers["inbox:new"]!({ item: newItem });
    });
    await waitFor(() => expect(shell.result.current).toBe(1));
    expect(qc.getQueryState(inboxKeys.list("ws-1"))?.isInvalidated).toBe(true);
    expect(listInbox).toHaveBeenCalledTimes(1);

    // Any later issue update patches status, without re-reading notifications.
    rows = rows.map((item) => ({ ...item, issue_status: "in_progress" }));
    act(() => {
      handlers["issue:updated"]!({
        issue: { id: "issue-1", status: "in_progress", revision: 2 },
      });
    });
    page = mountInbox();
    await waitFor(() => expect(listInbox).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(page.result.current.data).toEqual(rows));
    expect(
      deduplicateInboxItems(page.result.current.data!).filter((item) => !item.read),
    ).toHaveLength(1);
    expect(shell.result.current).toBe(1);
  } finally {
    page.unmount();
    shell.unmount();
    qc.clear();
  }
});
