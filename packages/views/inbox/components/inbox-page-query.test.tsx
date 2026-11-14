import { useEffect, type ReactNode } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { EMPTY_INBOX_FILTERS, filterInboxItems, useInboxFilterStore, type InboxFilters } from "@multica/core/inbox/filter-store";
import { archivedInboxLookupOptions, inboxKeys } from "@multica/core/inbox/queries";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { onInboxIssueDeleted, patchInboxIssueProjection } from "@multica/core/inbox/ws-updaters";
import type { InboxSections } from "@multica/core/inbox/sections";
import type { ArchivedInboxPage } from "@multica/core/types/inbox";
import type { InboxItem } from "@multica/core/types";
import { InboxPage } from "./inbox-page";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
let workspaceId = "workspace-1";
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => workspaceId }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    inbox: () => "/acme/inbox",
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));
vi.mock("../../modals/use-issue-limit-upgrade-prompt", () => ({
  useIssueLimitUpgradePrompt: () => vi.fn(),
}));

const detailUnmounted = vi.hoisted(() => vi.fn());
vi.mock("../../issues/components/issue-detail", () => ({
  IssueDetail: ({ issueId }: { issueId: string }) => {
    useEffect(() => () => detailUnmounted(issueId), [issueId]);
    return <div data-testid="detail">{issueId}</div>;
  },
  issueHighlightMementoKey: (issueId: string) => `highlight:${issueId}`,
}));
const replace = vi.fn();
let searchParams = new URLSearchParams();
vi.mock("../../navigation", () => ({
  useNavigation: () => ({ searchParams, replace }),
  useReportNavigating: () => {},
}));
vi.mock("@multica/ui/hooks/use-mobile", () => ({ useIsCompact: () => false }));
vi.mock("./inbox-list", () => ({
  InboxList: ({ items, onSelect }: { items: InboxItem[]; onSelect: (item: InboxItem) => void }) => (
    <div>{items.map((row) => (
      <button key={row.id} data-read={row.read} onClick={() => onSelect(row)}>{row.id}</button>
    ))}</div>
  ),
}));
vi.mock("./inbox-sectioned-list", () => ({
  InboxSectionedList: ({ sections, onSelect }: { sections: InboxSections; onSelect: (item: InboxItem) => void }) => (
    <div>{[...sections.needsYou, ...sections.updates, ...sections.snoozed].map(({ item: row }) => (
      <button key={row.id} data-read={row.read} onClick={() => onSelect(row)}>{row.id}</button>
    ))}</div>
  ),
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "member-1" } }),
}));
const filterMenuUnmounted = vi.hoisted(() => vi.fn());
vi.mock("./inbox-filter-menu", () => ({
  InboxFilterMenu: () => {
    useEffect(() => () => filterMenuUnmounted(), []);
    return <div data-testid="filter-menu" />;
  },
}));
vi.mock("./inbox-list-item", () => ({ useTimeAgo: () => vi.fn() }));
let rowActions: { onAction: (id: string) => void };
vi.mock("./inbox-context-menu", () => ({
  InboxContextMenuProvider: ({ actions, children }: { actions: typeof rowActions; children: ReactNode }) => {
    rowActions = actions;
    return children;
  },
}));
vi.mock("./inbox-detail-label", () => ({ useTypeLabels: () => ({}) }));
vi.mock("../../i18n", () => {
  const t = () => "Inbox";
  return { useT: () => ({ t }), useLocale: () => "en" };
});

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-a", workspace_id: "workspace-1", recipient_type: "member",
    recipient_id: "member-1", actor_type: "agent", actor_id: "agent-1",
    type: "new_comment", severity: "info", issue_id: "issue-a",
    title: "Issue title", body: null, issue_status: "todo", issue_priority: "high",
    read: false, archived: false, created_at: "2026-06-15T08:00:00Z", details: null,
    ...overrides,
  };
}

let qc: QueryClient;
beforeEach(() => {
  workspaceId = "workspace-1";
  useInboxSelectionStore.setState({ lastResolvedKeyByWorkspace: {} });
  qc = new QueryClient({ defaultOptions: {
    queries: { retry: false, staleTime: Infinity, gcTime: 0 },
    mutations: { retry: false },
  } });
  searchParams = new URLSearchParams();
  replace.mockClear();
  detailUnmounted.mockClear();
  filterMenuUnmounted.mockClear();
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
  useInboxFilterStore.getState().toggleUnreadOnly("workspace-1");
});
afterEach(() => { cleanup(); qc.clear(); });

function renderPage() {
  return render(<QueryClientProvider client={qc}><InboxPage /></QueryClientProvider>);
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const page = (items: InboxItem[]): ArchivedInboxPage => ({ items, hasMore: false, nextCursor: null });

it("stays in Inbox after a failed first load and opens the selection when retry succeeds", async () => {
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
  searchParams.set("issue", "issue-a");
  const listInbox = vi.fn()
    .mockRejectedValueOnce(new Error("Inbox unavailable"))
    .mockResolvedValue([item({ read: true })]);
  setApiInstance({ listInbox, markInboxRead: async () => item({ read: true }), getInboxUnreadSummary: async () => [] } as unknown as ApiClient);

  renderPage();
  await waitFor(() => expect(qc.getQueryState(inboxKeys.list("workspace-1"))?.status).toBe("error"));
  expect(replace).not.toHaveBeenCalled();

  await act(async () => { await qc.refetchQueries({ queryKey: inboxKeys.list("workspace-1") }); });
  expect(await screen.findByTestId("detail")).toHaveTextContent("issue-a");
  expect(replace).not.toHaveBeenCalled();
});

it("clears a remembered selection after leaving Inbox, archiving the read notification and evicting its cache", async () => {
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
  let rows = [item({ read: true })];
  setApiInstance({ listInbox: async () => rows, markInboxRead: async () => rows[0]!, getInboxUnreadSummary: async () => [] } as unknown as ApiClient);
  const first = renderPage();
  fireEvent.click(await screen.findByText("inbox-a"));
  expect(await screen.findByTestId("detail")).toHaveTextContent("issue-a");
  await waitFor(() => expect(qc.isMutating()).toBe(0));
  first.unmount();

  // Cmd+K archive-all-read outside Inbox, or an archive on another client.
  rows = [];
  qc.removeQueries({ queryKey: inboxKeys.list("workspace-1") });
  searchParams = new URLSearchParams("issue=issue-a");
  replace.mockClear();
  renderPage();

  await waitFor(() => expect(replace).toHaveBeenLastCalledWith("/acme/inbox"));
  expect(replace).not.toHaveBeenCalledWith("/acme/issues/issue-a");
});

it.each(["inbox", "archived"])("preserves an unknown shared link in %s", async (view) => {
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
  searchParams = new URLSearchParams(view === "archived" ? "view=archived" : "");
  searchParams.set("issue", "shared-issue");
  setApiInstance({
    listInbox: async () => [],
    listArchivedInboxPage: async () => page([]),
    getInboxUnreadSummary: async () => [],
  } as unknown as ApiClient);
  renderPage();

  await waitFor(() => expect(replace).toHaveBeenLastCalledWith("/acme/issues/shared-issue"));
});

it("remembers workspace A after visiting workspace B and losing A's cached notifications", async () => {
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
  let rows = [item({ read: true })];
  setApiInstance({ listInbox: async () => rows, markInboxRead: async () => rows[0]!, getInboxUnreadSummary: async () => [] } as unknown as ApiClient);
  const first = renderPage();
  fireEvent.click(await screen.findByText("inbox-a"));
  expect(await screen.findByTestId("detail")).toHaveTextContent("issue-a");
  await waitFor(() => expect(qc.isMutating()).toBe(0));
  first.unmount();

  workspaceId = "workspace-2";
  rows = [item({ id: "inbox-b", issue_id: "issue-b", workspace_id: workspaceId, read: true })];
  const second = renderPage();
  fireEvent.click(await screen.findByText("inbox-b"));
  expect(await screen.findByTestId("detail")).toHaveTextContent("issue-b");
  await waitFor(() => expect(qc.isMutating()).toBe(0));
  second.unmount();

  // Switching workspaces lets A's inactive query expire after ten minutes.
  qc.removeQueries({ queryKey: inboxKeys.list("workspace-1") });
  workspaceId = "workspace-1";
  rows = [];
  searchParams = new URLSearchParams("issue=issue-a");
  replace.mockClear();
  renderPage();

  await waitFor(() => expect(replace).toHaveBeenLastCalledWith("/acme/inbox"));
  expect(replace).not.toHaveBeenCalledWith("/acme/issues/issue-a");
});

it("does not reuse another workspace's recognition for an unknown shared link", async () => {
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
  let rows = [item({ read: true })];
  setApiInstance({ listInbox: async () => rows, markInboxRead: async () => rows[0]!, getInboxUnreadSummary: async () => [] } as unknown as ApiClient);
  const first = renderPage();
  fireEvent.click(await screen.findByText("inbox-a"));
  expect(await screen.findByTestId("detail")).toHaveTextContent("issue-a");
  await waitFor(() => expect(qc.isMutating()).toBe(0));
  first.unmount();

  workspaceId = "workspace-2";
  rows = [];
  searchParams = new URLSearchParams("issue=issue-a");
  replace.mockClear();
  renderPage();

  await waitFor(() => expect(replace).toHaveBeenLastCalledWith("/acme/issues/issue-a"));
});

it.each([false, true])("clears an older unread assignment when opening a read row via URL=%s", async (viaUrl) => {
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
  if (viaUrl) searchParams.set("issue", "issue-a");
  let rows = [
    item({ id: "comment", read: true, created_at: "2026-06-15T08:01:00Z" }),
    item({ id: "assigned", type: "issue_assigned", severity: "action_required" }),
  ];
  const listInbox = vi.fn(async () => rows);
  const markInboxRead = vi.fn(async () => {
    rows = rows.map((row) => ({ ...row, read: true }));
    return rows[0]!;
  });
  setApiInstance({ listInbox, markInboxRead, getInboxUnreadSummary: async () => [] } as unknown as ApiClient);

  renderPage();
  const row = await screen.findByText("comment");
  if (!viaUrl) fireEvent.click(row);

  expect(await screen.findByTestId("detail")).toHaveTextContent("issue-a");
  await waitFor(() => expect(markInboxRead).toHaveBeenCalledExactlyOnceWith("comment"));
  await waitFor(() => expect(listInbox).toHaveBeenCalledTimes(2));
  expect(qc.getQueryData<InboxItem[]>(inboxKeys.list("workspace-1"))?.every((row) => row.read)).toBe(true);
});

function archivedFixture() {
  searchParams = new URLSearchParams("view=archived");
  const readGate = deferred<void>();
  const lookup = deferred<ArchivedInboxPage>();
  const a = item({ archived: true });
  let rows = [a, item({ id: "inbox-b", issue_id: "issue-b", archived: true })];
  const listArchivedInboxPage = vi.fn(async (filters: InboxFilters, options: { groupId?: string }) => {
    if (options.groupId === "issue-a") return lookup.promise;
    if (options.groupId) return page(rows.filter((row) => row.issue_id === options.groupId));
    return page(rows.filter((row) => !filters.unreadOnly || !row.read));
  });
  const markInboxRead = vi.fn(async (id: string) => {
    if (id === a.id) await readGate.promise;
    rows = rows.map((row) => row.id === id ? { ...row, read: true } : row);
    return rows.find((row) => row.id === id)!;
  });
  const unarchiveInbox = vi.fn(async () => {
    rows = rows.filter((row) => row.id !== a.id);
    return { ...a, read: true, archived: false };
  });
  setApiInstance({ listArchivedInboxPage, markInboxRead, unarchiveInbox,
    getInboxUnreadSummary: async () => [],
  } as unknown as ApiClient);
  return { a, readGate, lookup, listArchivedInboxPage, unarchiveInbox,
    remove: () => { rows = rows.filter((row) => row.id !== a.id); },
  };
}

async function openArchived(fixture: ReturnType<typeof archivedFixture>, viaUrl = false) {
  if (viaUrl) searchParams.set("issue", "issue-a");
  renderPage();
  const row = await screen.findByText("inbox-a");
  if (!viaUrl) fireEvent.click(row);
  const detail = await screen.findByTestId("detail");
  await waitFor(() => expect(screen.getByText("inbox-a")).toHaveAttribute("data-read", "true"));
  await act(async () => fixture.readGate.resolve());
  await waitFor(() => expect(fixture.listArchivedInboxPage.mock.calls.some((call) => call[1].groupId === "issue-a")).toBe(true));
  return detail;
}

it.each(["click", "url"])("keeps the archived row and detail mounted through refetch and slow lookup after %s selection, then releases the pin", async (selection) => {
  const fixture = archivedFixture();
  const detail = await openArchived(fixture, selection === "url");
  expect(screen.getByText("inbox-a")).toHaveAttribute("data-read", "true");
  expect(screen.getByTestId("detail")).toBe(detail);
  expect(detailUnmounted).not.toHaveBeenCalled();
  await act(async () => fixture.lookup.resolve(page([{ ...fixture.a, read: true }])));
  expect(screen.getByText("inbox-a")).toHaveAttribute("data-read", "true");
  expect(screen.getByTestId("detail")).toBe(detail);
  expect(detailUnmounted).not.toHaveBeenCalled();
  expect(replace).not.toHaveBeenCalledWith("/acme/inbox?view=archived");
  fireEvent.click(screen.getByText("inbox-b"));
  await waitFor(() => expect(screen.getByTestId("detail")).toHaveTextContent("issue-b"));
  expect(screen.queryByText("inbox-a")).not.toBeInTheDocument();
});

it("keeps an older archived notification open through auto-read when the lookup carries its type filter", async () => {
  searchParams = new URLSearchParams("view=archived");
  useInboxFilterStore.getState().toggleFilterValue("workspace-1", "type", "mention");
  const gate = deferred<void>();
  let rows = [
    item({ id: "newer-change", type: "status_changed", archived: true, read: false, issue_label_ids: [] }),
    item({ id: "older-mention", type: "mentioned", archived: true, issue_label_ids: [], created_at: "2026-06-15T07:00:00Z" }),
  ];
  const listArchivedInboxPage = vi.fn(async (filters: InboxFilters) =>
    page(filterInboxItems(rows, { ...EMPTY_INBOX_FILTERS, ...filters })));
  const markInboxRead = vi.fn(async (id: string) => {
    rows = rows.map((row) => row.id === id ? { ...row, read: true } : row);
    await gate.promise;
    return rows.find((row) => row.id === id)!;
  });
  setApiInstance({ listArchivedInboxPage, markInboxRead, getInboxUnreadSummary: async () => [] } as unknown as ApiClient);
  renderPage();
  fireEvent.click(await screen.findByText("older-mention"));
  expect(await screen.findByTestId("detail")).toHaveTextContent("issue-a");
  await waitFor(() => expect(markInboxRead).toHaveBeenCalledWith("older-mention"));
  await act(async () => gate.resolve());
  await waitFor(() => expect(listArchivedInboxPage.mock.calls.length).toBeGreaterThanOrEqual(3));
  await waitFor(() => expect(qc.isFetching()).toBe(0));
  expect(replace).not.toHaveBeenCalledWith("/acme/inbox?view=archived");
  expect(screen.getByTestId("detail")).toHaveTextContent("issue-a");
});

it("asks the lookup with the view's filters except unread, and clears a filtered-out deep link instead of redirecting", async () => {
  searchParams = new URLSearchParams("view=archived&issue=issue-a");
  useInboxFilterStore.getState().toggleFilterValue("workspace-1", "type", "mention");
  const rows = [item({ id: "comment-only", archived: true, issue_label_ids: [] })];
  const listArchivedInboxPage = vi.fn(async (filters: InboxFilters, options: { groupId?: string }) => {
    const matches = filterInboxItems(rows, { ...EMPTY_INBOX_FILTERS, ...filters });
    return page(options.groupId ? matches.filter((row) => row.issue_id === options.groupId) : matches);
  });
  setApiInstance({ listArchivedInboxPage, getInboxUnreadSummary: async () => [] } as unknown as ApiClient);
  renderPage();

  await waitFor(() => expect(replace).toHaveBeenLastCalledWith("/acme/inbox?view=archived"));
  const lookups = listArchivedInboxPage.mock.calls.filter(([, options]) => options.groupId === "issue-a");
  expect(lookups[0]![0]).toMatchObject({ types: ["mention"], unreadOnly: false });
  expect(lookups.at(-1)![0]).toMatchObject({ types: [], unreadOnly: false });
  expect(replace).not.toHaveBeenCalledWith(expect.stringContaining("/issues/"));
});

it("keeps reading through lookup failure and clears a confirmed missing notification", async () => {
  const fixture = archivedFixture();
  const detail = await openArchived(fixture);
  await act(async () => fixture.lookup.reject(new Error("offline")));
  await waitFor(() => expect(qc.getQueryState(archivedInboxLookupOptions("workspace-1", "issue-a").queryKey)?.status).toBe("error"));
  expect(screen.getByText("inbox-a")).toBeInTheDocument();
  expect(screen.getByTestId("detail")).toBe(detail);
  expect(detailUnmounted).not.toHaveBeenCalled();
  fixture.listArchivedInboxPage.mockImplementation(async () => page([]));
  await act(async () => { await qc.invalidateQueries({ queryKey: inboxKeys.archived("workspace-1") }); });
  await waitFor(() => expect(screen.queryByTestId("detail")).not.toBeInTheDocument());
  expect(screen.queryByText("inbox-a")).not.toBeInTheDocument();
  expect(replace).toHaveBeenLastCalledWith("/acme/inbox?view=archived");
});

it("removes a pinned archived notification when its issue is deleted", async () => {
  const fixture = archivedFixture();
  await openArchived(fixture);
  fixture.remove();
  fixture.listArchivedInboxPage.mockImplementation(async () => page([]));
  await act(async () => { await onInboxIssueDeleted(qc, "workspace-1", "issue-a"); });
  await waitFor(() => expect(screen.queryByTestId("detail")).not.toBeInTheDocument());
  expect(screen.queryByText("inbox-a")).not.toBeInTheDocument();
  expect(replace).toHaveBeenLastCalledWith("/acme/inbox?view=archived");
});

it("restores a pinned notification without keeping it in the archived list", async () => {
  const fixture = archivedFixture();
  await openArchived(fixture);
  await act(async () => fixture.lookup.resolve(page([{ ...fixture.a, read: true }])));
  act(() => rowActions.onAction("inbox-a"));
  await waitFor(() => expect(fixture.unarchiveInbox).toHaveBeenCalledOnce());
  await waitFor(() => expect(screen.getByTestId("detail")).toHaveTextContent("issue-b"));
  expect(screen.queryByText("inbox-a")).not.toBeInTheDocument();
});

it.each(["status", "priority", "actor"] as const)("still applies the %s filter to a pinned archived row", async (filter) => {
  const fixture = archivedFixture();
  await openArchived(fixture);
  act(() => {
    const store = useInboxFilterStore.getState();
    if (filter === "status") {
      store.toggleStatusFilter("workspace-1", "todo");
      patchInboxIssueProjection(qc, "workspace-1", "issue-a", { status: "done" });
    } else if (filter === "priority") {
      store.togglePriorityFilter("workspace-1", "high");
      patchInboxIssueProjection(qc, "workspace-1", "issue-a", { priority: "low" });
    } else {
      store.toggleActorFilter("workspace-1", "agent:someone-else");
    }
  });
  await waitFor(() => expect(screen.queryByTestId("detail")).not.toBeInTheDocument());
  expect(screen.queryByText("inbox-a")).not.toBeInTheDocument();
  expect(replace).toHaveBeenLastCalledWith("/acme/inbox?view=archived");
});

it("keeps the archived list and its filter menu mounted while a new filter loads", async () => {
  searchParams = new URLSearchParams("view=archived");
  const next = deferred<ArchivedInboxPage>();
  const listArchivedInboxPage = vi.fn(async (filters: InboxFilters) =>
    filters.statuses.length ? next.promise : page([item({ archived: true })]));
  setApiInstance({ listArchivedInboxPage, getInboxUnreadSummary: async () => [] } as unknown as ApiClient);
  renderPage();
  await screen.findByText("inbox-a");
  const menu = screen.getByTestId("filter-menu");
  act(() => useInboxFilterStore.getState().toggleStatusFilter("workspace-1", "todo"));
  await waitFor(() => expect(listArchivedInboxPage).toHaveBeenCalledTimes(2));
  expect(screen.getByTestId("filter-menu")).toBe(menu);
  expect(screen.getByText("inbox-a")).toBeInTheDocument();
  act(() => useInboxFilterStore.getState().toggleStatusFilter("workspace-1", "in_progress"));
  await act(async () => next.resolve(page([item({ archived: true })])));
  expect(screen.getByTestId("filter-menu")).toBe(menu);
  expect(filterMenuUnmounted).not.toHaveBeenCalled();
});
