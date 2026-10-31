import { useEffect, type ReactNode } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { useInboxFilterStore, type InboxFilters } from "@multica/core/inbox/filter-store";
import { archivedInboxLookupOptions, inboxKeys } from "@multica/core/inbox/queries";
import { onInboxIssueDeleted, patchInboxIssueProjection } from "@multica/core/inbox/ws-updaters";
import type { ArchivedInboxPage } from "@multica/core/types/inbox";
import type { InboxItem } from "@multica/core/types";
import { InboxPage } from "./inbox-page";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
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
vi.mock("./inbox-filter-menu", () => ({ InboxFilterMenu: () => null }));
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
  return { useT: () => ({ t }) };
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
  qc = new QueryClient({ defaultOptions: {
    queries: { retry: false, staleTime: Infinity, gcTime: 0 },
    mutations: { retry: false },
  } });
  searchParams = new URLSearchParams();
  replace.mockClear();
  detailUnmounted.mockClear();
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

it("keeps the active read selection until moving to another notification", async () => {
  let rows = [item(), item({ id: "inbox-b", issue_id: "issue-b" })];
  const listInbox = vi.fn(async () => rows);
  const markInboxRead = vi.fn(async (id: string) => {
    rows = rows.map((row) => row.id === id ? { ...row, read: true } : row);
    return rows.find((row) => row.id === id)!;
  });
  setApiInstance({ listInbox, markInboxRead, getInboxUnreadSummary: async () => [] } as unknown as ApiClient);
  renderPage();
  fireEvent.click(await screen.findByText("inbox-a"));
  await waitFor(() => expect(listInbox).toHaveBeenCalledTimes(2));
  expect(screen.getByText("inbox-a")).toHaveAttribute("data-read", "true");
  expect(screen.getByTestId("detail")).toHaveTextContent("issue-a");
  fireEvent.click(screen.getByText("inbox-b"));
  await waitFor(() => expect(listInbox).toHaveBeenCalledTimes(3));
  expect(screen.getByTestId("detail")).toHaveTextContent("issue-b");
  expect(screen.queryByText("inbox-a")).not.toBeInTheDocument();
  expect(screen.getByText("inbox-b")).toHaveAttribute("data-read", "true");
});

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
