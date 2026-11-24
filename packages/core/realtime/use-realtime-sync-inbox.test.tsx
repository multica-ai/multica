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

// A notification on an issue the list holds is written into it; one on any
// other issue leaves the list owing a re-read, which a later issue event's
// patch must not settle (MUL-7286).
it.each([
  ["a new issue", "issue-2", { written: false, requests: 2 }],
  ["an issue it lists", "issue-1", { written: true, requests: 1 }],
] as const)("brings an inactive Inbox up to date after inbox:new on %s, then issue:updated", async (_label, issueId, expected) => {
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
      issue_id: issueId,
      read: false,
      created_at: "2026-09-11T00:01:00Z",
    };
    rows = [newItem, oldItem];
    await act(async () => {
      handlers["inbox:new"]!({ item: newItem });
    });
    await waitFor(() => expect(shell.result.current).toBe(1));
    expect(qc.getQueryState(inboxKeys.list("ws-1"))?.isInvalidated).toBe(!expected.written);
    expect(qc.getQueryData<InboxItem[]>(inboxKeys.list("ws-1"))?.map((item) => item.id)).toEqual(
      expected.written ? ["n2", "n1"] : ["n1"],
    );
    expect(listInbox).toHaveBeenCalledTimes(1);

    // Any later issue update patches status, without re-reading notifications.
    rows = rows.map((item) => (item.issue_id === "issue-1" ? { ...item, issue_status: "in_progress" } : item));
    act(() => {
      handlers["issue:updated"]!({
        issue: { id: "issue-1", status: "in_progress", revision: 2 },
      });
    });
    page = mountInbox();
    await waitFor(() => expect(page.result.current.data).toEqual(rows));
    expect(listInbox).toHaveBeenCalledTimes(expected.requests);
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

it("re-reads the active Inbox for an inbox:new that carries no item", async () => {
  const qc = createQueryClient();
  const listInbox = vi.fn(async (): Promise<InboxItem[]> => []);
  const getInboxUnreadSummary = vi.fn(async () => []);
  setApiInstance({ listInbox, getInboxUnreadSummary } as unknown as ApiClient);
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
  const page = renderHook(() => useQuery(inboxListOptions("ws-1")), { wrapper: Wrapper });
  try {
    await waitFor(() => expect(page.result.current.isSuccess).toBe(true));
    await waitFor(() => expect(getInboxUnreadSummary).toHaveBeenCalledTimes(1));

    await act(async () => {
      handlers["inbox:new"]!({});
    });

    await waitFor(() => expect(listInbox).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(getInboxUnreadSummary).toHaveBeenCalledTimes(2));
  } finally {
    page.unmount();
    shell.unmount();
    qc.clear();
  }
});
