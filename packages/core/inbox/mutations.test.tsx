/**
 * @vitest-environment jsdom
 */
import { beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { InboxItem, InboxWorkspaceUnread } from "../types";
import { useMarkInboxRead, useMarkInboxUnread, useUnarchiveInbox } from "./mutations";
import { inboxKeys } from "./queries";

vi.mock("../hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

const WORKSPACE_ID = "workspace-1";

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: WORKSPACE_ID,
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: true,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    ...overrides,
  };
}

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

function archivedCache(qc: QueryClient) {
  return qc.getQueryData<InboxItem[]>(inboxKeys.archived(WORKSPACE_ID)) ?? [];
}

function listCache(qc: QueryClient) {
  return qc.getQueryData<InboxItem[]>(inboxKeys.list(WORKSPACE_ID)) ?? [];
}

function summaryCount(qc: QueryClient) {
  const summary = qc.getQueryData<InboxWorkspaceUnread[]>(
    inboxKeys.unreadSummary(),
  );
  return summary?.find((e) => e.workspace_id === WORKSPACE_ID)?.count;
}

describe("useMarkInboxUnread", () => {
  let queryClient: QueryClient;
  let markInboxUnread: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    markInboxUnread = vi.fn(async (id: string) => item({ id, read: false }));
    setApiInstance({ markInboxUnread } as unknown as ApiClient);
  });

  it("flips only the targeted item unread, in both lists", async () => {
    // Item-level, mirroring mark-read: the list shows one row per issue
    // carrying that group's newest item, so flipping siblings would resurrect
    // notifications the user already dealt with without changing the row.
    queryClient.setQueryData<InboxItem[]>(inboxKeys.list(WORKSPACE_ID), [
      item({ id: "inbox-1", read: true, archived: false }),
      item({ id: "sibling", issue_id: "issue-1", read: true, archived: false }),
    ]);
    queryClient.setQueryData<InboxItem[]>(inboxKeys.archived(WORKSPACE_ID), [
      item({ id: "inbox-1", read: true }),
    ]);

    const { result } = renderHook(() => useMarkInboxUnread(), {
      wrapper: createWrapper(queryClient),
    });
    result.current.mutate("inbox-1");

    await waitFor(() => {
      expect(
        listCache(queryClient).find((i) => i.id === "inbox-1")?.read,
      ).toBe(false);
    });
    expect(listCache(queryClient).find((i) => i.id === "sibling")?.read).toBe(true);
    // Actioned from either list, patched in both — otherwise a view switch
    // shows two different read states for one notification.
    expect(archivedCache(queryClient)[0]?.read).toBe(false);
  });

  it("refreshes the cross-workspace summary so the switcher dot lights again", async () => {
    queryClient.setQueryData<InboxItem[]>(inboxKeys.list(WORKSPACE_ID), [
      item({ id: "inbox-1", read: true, archived: false }),
    ]);
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    const { result } = renderHook(() => useMarkInboxUnread(), {
      wrapper: createWrapper(queryClient),
    });
    result.current.mutate("inbox-1");

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxKeys.unreadSummary(),
    });
  });

  it("rolls both lists back when the request fails", async () => {
    markInboxUnread.mockRejectedValue(new Error("boom"));
    const active = [item({ id: "inbox-1", read: true, archived: false })];
    const archived = [item({ id: "inbox-1", read: true })];
    queryClient.setQueryData<InboxItem[]>(inboxKeys.list(WORKSPACE_ID), active);
    queryClient.setQueryData<InboxItem[]>(inboxKeys.archived(WORKSPACE_ID), archived);

    const { result } = renderHook(() => useMarkInboxUnread(), {
      wrapper: createWrapper(queryClient),
    });
    result.current.mutate("inbox-1");

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(listCache(queryClient)).toEqual(active);
    expect(archivedCache(queryClient)).toEqual(archived);
  });
});

describe("useUnarchiveInbox", () => {
  let queryClient: QueryClient;
  let unarchiveInbox: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    unarchiveInbox = vi.fn(async (id: string) => item({ id, archived: false }));
    setApiInstance({ unarchiveInbox } as unknown as ApiClient);
  });

  it("drops the whole issue group out of the archived list optimistically", async () => {
    // Archiving is issue-level, so restoring has to bring every sibling back —
    // leaving one behind would keep the issue in the archived list.
    queryClient.setQueryData<InboxItem[]>(inboxKeys.archived(WORKSPACE_ID), [
      item({ id: "sibling-a", issue_id: "issue-1" }),
      item({ id: "sibling-b", issue_id: "issue-1" }),
      item({ id: "other-issue", issue_id: "issue-2" }),
    ]);

    const { result } = renderHook(() => useUnarchiveInbox(), {
      wrapper: createWrapper(queryClient),
    });
    result.current.mutate("sibling-a");

    await waitFor(() => {
      const stillArchived = archivedCache(queryClient).filter((i) => i.archived);
      expect(stillArchived.map((i) => i.id)).toEqual(["other-issue"]);
    });
  });

  it("preserves unread state and refreshes the badge sources, so the badge rises again", async () => {
    // Restoring an item that was archived while unread legitimately RAISES the
    // unread badge: the count only ever included non-archived items. Two halves
    // make that work, and this covers the client's half — never touch `read`,
    // and re-pull both the workspace list (the Inbox nav count) and the
    // cross-workspace summary (the switcher dot). The server's half — that
    // UnarchiveInboxItem leaves `read` alone — is pinned by
    // TestUnarchiveInboxPreservesUnread in the Go suite.
    queryClient.setQueryData<InboxItem[]>(inboxKeys.archived(WORKSPACE_ID), [
      item({ id: "inbox-1", read: false }),
    ]);
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    const { result } = renderHook(() => useUnarchiveInbox(), {
      wrapper: createWrapper(queryClient),
    });
    result.current.mutate("inbox-1");

    await waitFor(() => expect(result.current.isSuccess).toBe(true));

    // No cache write may flip `read` — an unread item must come back unread.
    expect(archivedCache(queryClient).every((i) => i.read === false)).toBe(true);
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxKeys.all(WORKSPACE_ID),
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxKeys.unreadSummary(),
    });
  });

  it("rolls the archived list back when the request fails", async () => {
    unarchiveInbox.mockRejectedValue(new Error("boom"));
    const original = [item({ id: "inbox-1" })];
    queryClient.setQueryData<InboxItem[]>(
      inboxKeys.archived(WORKSPACE_ID),
      original,
    );

    const { result } = renderHook(() => useUnarchiveInbox(), {
      wrapper: createWrapper(queryClient),
    });
    result.current.mutate("inbox-1");

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(archivedCache(queryClient)).toEqual(original);
  });
});

/**
 * The unread badge reads the server-computed cross-workspace summary rather
 * than counting the inbox list (MUL-6967), so a mutation that would have moved
 * the badge through its list patch has to move the summary cache too — or the
 * number sits still until the round-trip lands.
 */
describe("optimistic unread summary", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    setApiInstance({
      markInboxRead: vi.fn(async (id: string) => item({ id, read: true })),
      markInboxUnread: vi.fn(async (id: string) => item({ id, read: false })),
    } as unknown as ApiClient);
  });

  function seed(items: InboxItem[], summary: InboxWorkspaceUnread[]) {
    queryClient.setQueryData<InboxItem[]>(inboxKeys.list(WORKSPACE_ID), items);
    queryClient.setQueryData<InboxWorkspaceUnread[]>(
      inboxKeys.unreadSummary(),
      summary,
    );
  }

  it("drops the workspace entry once its last unread group is read", async () => {
    seed(
      [item({ id: "inbox-1", read: false, archived: false })],
      [
        { workspace_id: WORKSPACE_ID, count: 1 },
        { workspace_id: "workspace-2", count: 4 },
      ],
    );

    const { result } = renderHook(() => useMarkInboxRead(), {
      wrapper: createWrapper(queryClient),
    });
    result.current.mutate("inbox-1");

    // Zero is expressed as an absent entry, mirroring the server response.
    await waitFor(() => expect(summaryCount(queryClient)).toBeUndefined());
    // Other workspaces are untouched — this patch is scoped to one entry.
    expect(
      queryClient
        .getQueryData<InboxWorkspaceUnread[]>(inboxKeys.unreadSummary())
        ?.find((e) => e.workspace_id === "workspace-2")?.count,
    ).toBe(4);
  });

  it("counts issue groups, not rows, like the list the user sees", async () => {
    // Two unread notifications on one issue plus one on another: the inbox
    // renders two rows, so the badge must read 2 — not 3.
    seed(
      [
        item({ id: "a1", issue_id: "issue-1", read: false, archived: false }),
        item({ id: "a2", issue_id: "issue-1", read: false, archived: false, created_at: "2026-06-15T09:00:00Z" }),
        item({ id: "b1", issue_id: "issue-2", read: false, archived: false }),
        item({ id: "c1", issue_id: "issue-3", read: false, archived: false }),
      ],
      [{ workspace_id: WORKSPACE_ID, count: 3 }],
    );

    const { result } = renderHook(() => useMarkInboxRead(), {
      wrapper: createWrapper(queryClient),
    });
    result.current.mutate("c1");

    await waitFor(() => expect(summaryCount(queryClient)).toBe(2));
  });

  it("adds the workspace back when a notification is flipped unread", async () => {
    seed([item({ id: "inbox-1", read: true, archived: false })], []);

    const { result } = renderHook(() => useMarkInboxUnread(), {
      wrapper: createWrapper(queryClient),
    });
    result.current.mutate("inbox-1");

    await waitFor(() => expect(summaryCount(queryClient)).toBe(1));
  });

  it("leaves the summary alone when the inbox list was never loaded", async () => {
    // At app start only the badge is mounted, so there is no list to derive
    // from — the server value must stand rather than being reset to zero.
    queryClient.setQueryData<InboxWorkspaceUnread[]>(inboxKeys.unreadSummary(), [
      { workspace_id: WORKSPACE_ID, count: 9 },
    ]);

    const { result } = renderHook(() => useMarkInboxRead(), {
      wrapper: createWrapper(queryClient),
    });
    result.current.mutate("inbox-1");

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(summaryCount(queryClient)).toBe(9);
  });

  it("restores the previous count when the request fails", async () => {
    setApiInstance({
      markInboxRead: vi.fn().mockRejectedValue(new Error("boom")),
    } as unknown as ApiClient);
    seed(
      [
        item({ id: "inbox-1", issue_id: "issue-1", read: false, archived: false }),
        item({ id: "inbox-2", issue_id: "issue-2", read: false, archived: false }),
      ],
      [{ workspace_id: WORKSPACE_ID, count: 2 }],
    );

    const { result } = renderHook(() => useMarkInboxRead(), {
      wrapper: createWrapper(queryClient),
    });
    result.current.mutate("inbox-1");

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(summaryCount(queryClient)).toBe(2);
  });
});
