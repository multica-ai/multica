// @vitest-environment node
import { QueryClient, type InfiniteData } from "@tanstack/react-query";
import type { InboxItem, InboxPage } from "@multica/core/types";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "@/data/api";
import { inboxKeys } from "@/data/queries/inbox";
import {
  useArchiveInbox,
  useMarkAllInboxRead,
  useMarkInboxRead,
} from "./inbox";

const state = vi.hoisted(() => ({
  qc: undefined as unknown as QueryClient,
}));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQueryClient: () => state.qc,
    // Drive the hook's options through a real MutationObserver so the test
    // runs the same onMutate → onError → onSettled lifecycle as the app.
    useMutation: (
      options: ConstructorParameters<typeof actual.MutationObserver>[1],
    ) => {
      const observer = new actual.MutationObserver(state.qc, options);
      return { mutateAsync: (variables?: unknown) => observer.mutate(variables) };
    },
  };
});

vi.mock("@/data/api", () => ({
  api: {
    markInboxRead: vi.fn(),
    archiveInbox: vi.fn(),
    markAllInboxRead: vi.fn(),
  },
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (
    selector: (s: { currentWorkspaceId: string }) => unknown,
  ) => selector({ currentWorkspaceId: "workspace-1" }),
}));

const wsId = "workspace-1";

function item(id: string, issueId: string | null): InboxItem {
  return {
    id,
    workspace_id: wsId,
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: issueId,
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-09-01T08:00:00Z",
    details: null,
  };
}

// Group issue-a sits on both pages (it moved between two page fetches) and in
// a lookup, so a patch that misses any cache would leave a stale copy.
function seed() {
  state.qc.setQueryData<InfiniteData<InboxPage, string | null>>(
    inboxKeys.listPages(wsId),
    {
      pages: [
        { items: [item("n1", "issue-a")], nextCursor: "cursor-1", hasMore: true },
        { items: [item("n2", "issue-b"), item("n3", "issue-a")], nextCursor: null, hasMore: false },
      ],
      pageParams: [null, "cursor-1"],
    },
  );
  state.qc.setQueryData<InboxPage>(inboxKeys.listLookup(wsId, "issue-a"), {
    items: [item("n1", "issue-a")],
    nextCursor: null,
    hasMore: false,
  });
}

function rows(pick: (i: InboxItem) => unknown) {
  return {
    pages: state.qc
      .getQueryData<InfiniteData<InboxPage>>(inboxKeys.listPages(wsId))
      ?.pages.map((page) => page.items.map(pick)),
    lookup: state.qc
      .getQueryData<InboxPage>(inboxKeys.listLookup(wsId, "issue-a"))
      ?.items.map(pick),
  };
}

describe("inbox mutations on the paged caches", () => {
  beforeEach(() => {
    state.qc = new QueryClient();
    vi.mocked(api.markInboxRead).mockReset();
    vi.mocked(api.archiveInbox).mockReset();
    vi.mocked(api.markAllInboxRead).mockReset();
    seed();
  });

  it("marks one notification read on every page and lookup holding it", async () => {
    vi.mocked(api.markInboxRead).mockResolvedValue(item("n1", "issue-a"));

    await useMarkInboxRead().mutateAsync("n1");

    expect(rows((i) => i.read)).toEqual({
      pages: [[true], [false, false]],
      lookup: [true],
    });
  });

  it("restores the unread row when mark-read fails", async () => {
    // The patch lands before the request so the iOS push transition captures
    // a read row; the rollback must still have the state from before it.
    vi.mocked(api.markInboxRead).mockRejectedValue(new Error("offline"));

    await expect(useMarkInboxRead().mutateAsync("n1")).rejects.toThrow("offline");

    expect(rows((i) => i.read)).toEqual({
      pages: [[false], [false, false]],
      lookup: [false],
    });
  });

  it("archives the whole group across pages and lookups", async () => {
    // The single archive endpoint archives every row of the issue, and the
    // group can appear more than once in the caches.
    vi.mocked(api.archiveInbox).mockResolvedValue(item("n3", "issue-a"));

    await useArchiveInbox().mutateAsync("n3");

    expect(rows((i) => i.archived)).toEqual({
      pages: [[true], [false, true]],
      lookup: [true],
    });
  });

  it("restores the group when archive fails", async () => {
    vi.mocked(api.archiveInbox).mockRejectedValue(new Error("offline"));

    await expect(useArchiveInbox().mutateAsync("n3")).rejects.toThrow("offline");

    expect(rows((i) => i.archived)).toEqual({
      pages: [[false], [false, false]],
      lookup: [false],
    });
  });

  it("marks every active row read and rolls back on failure", async () => {
    vi.mocked(api.markAllInboxRead).mockResolvedValue({ count: 3 });
    await useMarkAllInboxRead().mutateAsync();
    expect(rows((i) => i.read)).toEqual({
      pages: [[true], [true, true]],
      lookup: [true],
    });

    seed();
    vi.mocked(api.markAllInboxRead).mockRejectedValue(new Error("offline"));
    await expect(useMarkAllInboxRead().mutateAsync()).rejects.toThrow("offline");
    expect(rows((i) => i.read)).toEqual({
      pages: [[false], [false, false]],
      lookup: [false],
    });
  });

  it("refreshes the paged list after the server answers", async () => {
    vi.mocked(api.markInboxRead).mockResolvedValue(item("n1", "issue-a"));
    const invalidate = vi.spyOn(state.qc, "invalidateQueries");

    await useMarkInboxRead().mutateAsync("n1");
    // onSettled does not await the refresh; let its cancel settle first.
    await vi.waitFor(() =>
      expect(invalidate).toHaveBeenCalledWith({ queryKey: inboxKeys.list(wsId) }),
    );
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxKeys.unreadSummary(),
    });
  });
});
