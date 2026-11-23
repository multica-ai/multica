import { useState, type ReactNode } from "react";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { useAppearanceStore } from "@multica/core/appearance";
import { createQueryClient } from "@multica/core/query-client";
import {
  DEFAULT_SIDEBAR_INBOX_SETTINGS,
  useSidebarTreeStore,
} from "@multica/core/issues/stores/sidebar-tree-store";
import { useInboxFilterStore } from "@multica/core/inbox/filter-store";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import type { Agent, AgentTask, InboxItem, SnoozedInboxIssue } from "@multica/core/types";
import { agentListOptions } from "@multica/core/workspace/queries";
import { snoozedInboxOptions } from "@multica/core/inbox/queries";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { PanelBody } from "../panel-nav";
import { InboxSection, useInboxSectionRows } from "./inbox-section";

const state = vi.hoisted(() => ({
  unread: 0,
  compact: false,
  archive: vi.fn(),
  unarchive: vi.fn(() => Promise.resolve()),
  unsubscribeArchive: vi.fn(),
  markAllRead: vi.fn(),
  archiveAll: vi.fn(),
  archiveAllRead: vi.fn(),
  archiveCompleted: vi.fn(),
  markRead: vi.fn(),
  markUnread: vi.fn(),
  deleteInbox: vi.fn(() => Promise.resolve()),
  snooze: vi.fn(() => Promise.resolve()),
  unsnooze: vi.fn(() => Promise.resolve()),
}));

const apiMock = vi.hoisted(() => ({ listInbox: vi.fn(), listArchivedInboxPage: vi.fn() }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/permissions", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/permissions")>()),
  useIsGuest: () => false,
}));
vi.mock("@multica/core/inbox/queries", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/inbox/queries")>()),
  useInboxUnreadCount: () => state.unread,
}));
vi.mock("@multica/core/inbox/mutations", () => ({
  useArchiveInbox: () => ({
    mutate: state.archive,
    mutateAsync: (id: string) => {
      state.archive(id);
      return Promise.resolve();
    },
  }),
  useUnarchiveInbox: () => ({ mutateAsync: state.unarchive, mutate: state.unarchive }),
  useMarkAllInboxRead: () => ({ mutate: state.markAllRead }),
  useArchiveAllInbox: () => ({ mutateAsync: async () => (await state.archiveAll()) ?? { count: 1 } }),
  useArchiveAllReadInbox: () => ({ mutateAsync: async () => (await state.archiveAllRead()) ?? { count: 1 } }),
  useArchiveCompletedInbox: () => ({ mutateAsync: async () => (await state.archiveCompleted()) ?? { count: 1 } }),
  useMarkInboxRead: () => ({ mutate: state.markRead }),
  useMarkInboxUnread: () => ({ mutate: state.markUnread }),
  useDeleteInbox: () => ({ mutateAsync: state.deleteInbox, isPending: false }),
  useUnsubscribeArchiveInbox: () => ({ mutate: state.unsubscribeArchive }),
  undoUnsubscribeArchiveInbox: vi.fn(),
  useSnoozeInbox: () => ({ mutateAsync: state.snooze }),
  useUnsnoozeInbox: () => ({ mutateAsync: state.unsnooze }),
  cachedInboxSnoozedUntil: () => null,
}));
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/notification-preferences/mutations", () => ({
  useUpdateNotificationPreferences: () => ({ mutate: vi.fn() }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Leo Park", getActorInitials: () => "LP", getActorAvatarUrl: () => null }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
const contextMenuActionsSeen = vi.hoisted(() => [] as unknown[]);
vi.mock("../../inbox/components/inbox-context-menu", async (importOriginal) => {
  const actual = await importOriginal<typeof import("../../inbox/components/inbox-context-menu")>();
  return {
    ...actual,
    InboxContextMenuProvider: (props: React.ComponentProps<typeof actual.InboxContextMenuProvider>) => {
      contextMenuActionsSeen.push(props.actions);
      return actual.InboxContextMenuProvider(props);
    },
  };
});
vi.mock("../../agents/components/agent-avatar-stack", () => ({
  AgentAvatarStack: ({ agentIds }: { agentIds: string[] }) => <span data-testid="agents">{agentIds.join(",")}</span>,
}));
vi.mock("@multica/ui/hooks/use-mobile", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/ui/hooks/use-mobile")>()),
  useIsCompact: () => state.compact,
}));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

function item(overrides: Partial<InboxItem> & { id: string }): InboxItem {
  return {
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: null,
    title: "Issue title",
    body: null,
    issue_status: "in_progress",
    issue_priority: "none",
    read: false,
    archived: false,
    created_at: "2026-11-10T08:00:00Z",
    details: null,
    ...overrides,
  };
}

function adapter(url: string): NavigationAdapter {
  const [pathname, search = ""] = url.split("?");
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: pathname!,
    searchParams: new URLSearchParams(search),
    hash: "",
    getShareableUrl: (path) => path,
  };
}

function Harness({ pathname, openKey }: { pathname: string; openKey: string | null }) {
  const rows = useInboxSectionRows("ws-1", openKey);
  return <InboxSection wsId="ws-1" rows={rows} pathname={pathname} />;
}

/** `openIssueId`: the notification the Inbox page has open, beside the section. */
function renderSection(
  items: InboxItem[],
  openIssueId: string | null = null,
  url = openIssueId ? `/acme/inbox?issue=${openIssueId}` : "/acme/issues",
  tasks: Partial<AgentTask>[] = [],
  snoozed: SnoozedInboxIssue[] = [],
  agents: Pick<Agent, "id" | "owner_id">[] = [],
) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(["inbox", "ws-1", "list"], items);
  client.setQueryData(snoozedInboxOptions("ws-1").queryKey, snoozed);
  client.setQueryData(agentListOptions("ws-1").queryKey, agents as Agent[]);
  client.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, tasks as AgentTask[]);
  const nav = adapter(url);
  // As the Work panel keys it: the Inbox page's notification.
  const openKey = nav.pathname === "/acme/inbox" ? nav.searchParams.get("issue") : null;
  const result = renderWithI18n(
    <QueryClientProvider client={client}>
      <NavigationProvider value={nav}>
        <Harness pathname={nav.pathname} openKey={openKey} />
      </NavigationProvider>
    </QueryClientProvider>,
  );
  return { ...result, nav, client };
}

const rows = () => Array.from(document.querySelectorAll<HTMLElement>("[data-inbox-row]"));

beforeEach(() => {
  state.unread = 0;
  state.compact = false;
  for (const spy of [
    state.archive,
    state.markAllRead,
    state.archiveAll,
    state.archiveAllRead,
    state.archiveCompleted,
    state.markRead,
    state.markUnread,
  ]) {
    spy.mockReset();
  }
  // Rows with the actor line; the compact default has tests of its own.
  useSidebarTreeStore.setState({ inboxCollapsed: false, inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, compact: false } });
  useInboxFilterStore.getState().clearFilters("ws-1");
  useInboxSelectionStore.setState({
    landing: null,
    heldRows: {},
    leftRowKeyByWorkspace: {},
    sectionTabByWorkspace: {},
    sectionTabSyncedUrlByWorkspace: {},
    transitionByWorkspace: {},
  });
});

describe("InboxSection", () => {
  it("lists read and unread notifications, one row per issue, newest first", () => {
    renderSection([
      item({ id: "n1", issue_id: "issue-a", title: "Older issue", read: true, created_at: "2026-11-09T08:00:00Z" }),
      item({ id: "n2", issue_id: "issue-b", title: "Newer issue", created_at: "2026-11-10T09:00:00Z" }),
      item({ id: "n3", issue_id: "issue-b", title: "Newer issue", created_at: "2026-11-10T08:00:00Z" }),
      item({ id: "n4", issue_id: "issue-c", title: "Archived issue", archived: true }),
    ]);

    expect(rows().map((row) => row.textContent)).toEqual([
      expect.stringContaining("Newer issue"),
      expect.stringContaining("Older issue"),
    ]);
    expect(rows().map((row) => row.dataset.read)).toEqual(["false", "true"]);
    expect(rows()[0]!.querySelector("[data-mc='unread-dot']")).not.toBeNull();
    expect(rows()[1]!.querySelector("[data-mc='unread-dot']")).toBeNull();
  });

  it("opens every notification on the Inbox page, an issue's by the issue and an issue-less one by its own id", () => {
    renderSection([
      item({ id: "n1", issue_id: "issue-a", title: "Commented", details: { comment_id: "c-1" } }),
      item({ id: "n2", issue_id: "issue-b", title: "Assigned", type: "issue_assigned", created_at: "2026-11-09T08:00:00Z" }),
      item({ id: "n3", issue_id: null, title: "Autopilot paused", type: "autopilot_paused", created_at: "2026-11-08T08:00:00Z" }),
    ]);

    expect(screen.getByRole("link", { name: /^Commented/ })).toHaveAttribute("href", "/acme/inbox?issue=issue-a");
    expect(screen.getByRole("link", { name: /^Assigned/ })).toHaveAttribute("href", "/acme/inbox?issue=issue-b");
    expect(screen.getByRole("link", { name: /^Autopilot paused/ })).toHaveAttribute("href", "/acme/inbox?issue=n3");
  });

  it("marks issues an agent is working on or queued for", () => {
    renderSection(
      [
        item({ id: "n1", issue_id: "issue-a", title: "Working one" }),
        item({ id: "n2", issue_id: "issue-b", title: "Queued one", created_at: "2026-11-09T08:00:00Z" }),
        item({ id: "n3", issue_id: "issue-c", title: "Idle one", created_at: "2026-11-08T08:00:00Z" }),
      ],
      null,
      "/acme/issues",
      [
        { id: "t1", issue_id: "issue-a", agent_id: "a1", status: "running" },
        { id: "t2", issue_id: "issue-b", agent_id: "a2", status: "queued" },
      ],
    );

    const [working, queued, idle] = rows();
    expect(working).toHaveTextContent("Working");
    expect(working!.querySelector(".shimmer-text")).not.toBeNull();
    expect(queued).toHaveTextContent("Queued");
    expect(queued!.querySelector(".shimmer-text")).toBeNull();
    expect(idle!.querySelector("[data-testid=agents]")).toBeNull();
  });

  it("asks the Inbox page to land again on every click, even on the issue already open", () => {
    renderSection([item({ id: "n1", issue_id: "issue-a", title: "Commented", details: { comment_id: "c-1" } })], "issue-a");

    fireEvent.click(screen.getByRole("link", { name: /^Commented/ }));
    const first = useInboxSelectionStore.getState().landing;
    fireEvent.click(screen.getByRole("link", { name: /^Commented/ }));

    expect(first?.issueId).toBe("issue-a");
    expect(useInboxSelectionStore.getState().landing).toEqual({ issueId: "issue-a", token: first!.token + 1, landed: false });
  });

  it("lists under the Inbox filters, keeping the open row once reading it", () => {
    useInboxFilterStore.getState().toggleUnreadOnly("ws-1");
    renderSection(
      [
        item({ id: "n1", issue_id: "issue-a", title: "Open one", read: true }),
        item({ id: "n2", issue_id: "issue-b", title: "Read one", read: true, created_at: "2026-11-09T08:00:00Z" }),
        item({ id: "n3", issue_id: "issue-c", title: "Unread one", created_at: "2026-11-08T08:00:00Z" }),
      ],
      "issue-a",
    );

    expect(rows().map((row) => row.textContent)).toEqual([
      expect.stringContaining("Open one"),
      expect.stringContaining("Unread one"),
    ]);
    expect(screen.getByRole("button", { name: "1 active filter" })).toBeInTheDocument();
  });

  it("says when the filters hide everything, and clears them from there", () => {
    useInboxFilterStore.getState().toggleFilterValue("ws-1", "type", "mentions");
    renderSection([item({ id: "n1", issue_id: "issue-a", title: "Commented" })]);

    expect(rows()).toHaveLength(0);
    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));

    expect(rows()).toHaveLength(1);
    // Unfiltered, the filter waits in the actions menu.
    expect(screen.queryByRole("button", { name: "Filter inbox" })).toBeNull();
  });

  it("puts who did what under the title, or keeps one line when compact", () => {
    const items = [item({ id: "n1", issue_id: "issue-a", title: "Commented", body: "Ship it on Friday" })];
    const { unmount } = renderSection(items);
    expect(rows()[0]).toHaveTextContent("Leo Park Ship it on Friday");
    unmount();

    useSidebarTreeStore.setState({ inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, compact: true } });
    renderSection(items);
    expect(rows()[0]).toHaveTextContent("Commented");
    expect(rows()[0]).not.toHaveTextContent("Leo Park");
    expect(rows()[0]!.querySelector("[data-mc='unread-dot']")).not.toBeNull();
  });

  it("leads an issue's row with its status, and an issue-less notice with who sent it", () => {
    const items = [
      item({ id: "n1", issue_id: "issue-a", title: "Commented", issue_status: "in_review" }),
      item({ id: "n2", issue_id: null, title: "Autopilot paused", type: "autopilot_paused", issue_status: null, created_at: "2026-11-09T08:00:00Z" }),
    ];
    const { unmount } = renderSection(items);
    const lead = () => rows().map((row) => row.querySelector("[data-mc='nav-item']")!.firstElementChild);
    expect(lead()[0]).toHaveAttribute("data-inbox-status", "in_review");
    expect(screen.getByRole("link", { name: /^Commented, In Review/ })).toBeInTheDocument();
    expect(lead()[1]).not.toHaveAttribute("data-inbox-status");
    unmount();

    useSidebarTreeStore.setState({ inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, compact: true } });
    renderSection(items);
    expect(lead()[0]).toHaveAttribute("data-inbox-status", "in_review");
    expect(lead()[1]).toHaveAttribute("title", "Autopilot paused");
  });

  it("moves the time under the title while an agent holds its place, and keeps the unread dot when compact", () => {
    const items = [item({ id: "n1", issue_id: "issue-a", title: "Working one" })];
    const tasks = [{ id: "t1", issue_id: "issue-a", agent_id: "a1", status: "running" as const }];
    const { unmount } = renderSection(items, null, "/acme/issues", tasks);
    const [titleLine, actorLine] = Array.from(screen.getByRole("link", { name: /^Working one/ }).children);
    expect(within(titleLine as HTMLElement).getByTestId("agents")).toBeInTheDocument();
    expect(actorLine).toHaveTextContent(/\d+d$/);
    unmount();

    useSidebarTreeStore.setState({ inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, compact: true } });
    renderSection(items, null, "/acme/issues", tasks);
    expect(within(rows()[0]!).getByTestId("agents")).toBeInTheDocument();
    expect(rows()[0]!.querySelector("[data-mc='unread-dot']")).not.toBeNull();
  });

  it("shows the whole notification on hover, with its actions", async () => {
    renderSection([item({ id: "n1", issue_id: "issue-a", title: "Commented", body: "Ship it on Friday" })]);
    fireEvent.mouseEnter(rows()[0]!.firstElementChild!);
    const card = await screen.findByText("Ship it on Friday", { selector: "[data-inbox-card] p" }, { timeout: 2000 });
    const actions = within(card.closest<HTMLElement>("[data-inbox-card]")!);

    expect(actions.getByRole("link", { name: "Open" })).toHaveAttribute("href", "/acme/inbox?issue=issue-a");
    fireEvent.click(actions.getByRole("button", { name: "Mark as read" }));
    expect(state.markRead).toHaveBeenCalledWith("n1", expect.any(Object));
    fireEvent.click(actions.getByRole("button", { name: "Archive" }));
    expect(state.archive).toHaveBeenCalledWith("n1");
  });

  it("offers unsubscribe & archive on the card only for issues that are not the viewer's", async () => {
    renderSection(
      [
        item({ id: "n1", issue_id: "issue-a", title: "Theirs", body: "Their comment", issue_assignee_type: "agent", issue_assignee_id: "agent-x" }),
        item({ id: "n2", issue_id: "issue-b", title: "Mine", body: "My comment", issue_assignee_type: "agent", issue_assignee_id: "agent-mine" }),
      ],
      null,
      "/acme/issues",
      [],
      [],
      [{ id: "agent-x", owner_id: "someone" }, { id: "agent-mine", owner_id: "member-1" }],
    );
    const label = "Unsubscribe & archive";

    fireEvent.mouseEnter(rows()[1]!.firstElementChild!);
    const mine = await screen.findByText("My comment", { selector: "[data-inbox-card] p" }, { timeout: 2000 });
    expect(within(mine.closest<HTMLElement>("[data-inbox-card]")!).queryByRole("button", { name: label })).toBeNull();
    fireEvent.mouseLeave(rows()[1]!.firstElementChild!);

    fireEvent.mouseEnter(rows()[0]!.firstElementChild!);
    const theirs = await screen.findByText("Their comment", { selector: "[data-inbox-card] p" }, { timeout: 2000 });
    fireEvent.click(within(theirs.closest<HTMLElement>("[data-inbox-card]")!).getByRole("button", { name: label }));
    expect(state.unsubscribeArchive).toHaveBeenCalledWith({ id: "n1", issueId: "issue-a", scope: "issue" });
  });

  it("closes the card when the row is pressed, and keeps a pending hover from reopening it", async () => {
    renderSection([item({ id: "n1", issue_id: "issue-a", title: "Commented", body: "Ship it on Friday" })]);
    const trigger = rows()[0]!.firstElementChild!;
    const card = () => document.querySelector("[data-inbox-card]");

    fireEvent.mouseEnter(trigger);
    await screen.findByText("Ship it on Friday", { selector: "[data-inbox-card] p" }, { timeout: 2000 });
    fireEvent.pointerDown(trigger);
    await waitFor(() => expect(card()).toBeNull());

    // Pressed before the hover delay ran out: the card stays shut.
    fireEvent.mouseLeave(trigger);
    fireEvent.pointerLeave(trigger);
    fireEvent.mouseEnter(trigger);
    fireEvent.pointerDown(trigger);
    await new Promise((resolve) => setTimeout(resolve, 700));
    expect(card()).toBeNull();

    // Leaving the row lets the next hover show it again.
    fireEvent.mouseLeave(trigger);
    fireEvent.pointerLeave(trigger);
    fireEvent.mouseEnter(trigger);
    await screen.findByText("Ship it on Friday", { selector: "[data-inbox-card] p" }, { timeout: 2000 });
  });

  it("archives a row from its hover button", () => {
    renderSection([item({ id: "n1", issue_id: "issue-a", title: "Commented" })]);
    fireEvent.click(within(rows()[0]!).getByRole("button", { name: "Archive" }));
    expect(state.archive).toHaveBeenCalledWith("n1");
  });

  it("opens the next row once the open one is archived from its row, and stays put for any other", () => {
    const { nav } = renderSection(
      [
        item({ id: "n1", issue_id: "issue-a", title: "Open one" }),
        item({ id: "n2", issue_id: "issue-b", title: "Next one", created_at: "2026-11-09T08:00:00Z" }),
        item({ id: "n3", issue_id: "issue-c", title: "Last one", created_at: "2026-11-08T08:00:00Z" }),
      ],
      "issue-a",
    );
    fireEvent.click(within(rows()[2]!).getByRole("button", { name: "Archive" }));
    expect(state.archive).toHaveBeenCalledWith("n3");
    expect(nav.push).not.toHaveBeenCalled();

    fireEvent.click(within(rows()[0]!).getByRole("button", { name: "Archive" }));
    expect(state.archive).toHaveBeenCalledWith("n1");
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=issue-b");
    expect(useInboxSelectionStore.getState().leftRowKeyByWorkspace["ws-1"]).toBe("issue-a");
  });

  it("says when the open row comes back once another window snoozed it, rather than that it was archived", async () => {
    const open = item({ id: "n1", issue_id: "issue-a", title: "Open one" });
    const other = item({ id: "n2", issue_id: "issue-b", title: "Other one", created_at: "2026-11-09T08:00:00Z" });
    const { client } = renderSection([open, other], "issue-a");
    const until = new Date(Date.now() + 26 * 60 * 60 * 1000).toISOString();
    act(() => {
      client.setQueryData(snoozedInboxOptions("ws-1").queryKey, [
        { issue_id: "issue-a", item_id: "n1", snoozed_until: until, snoozed_at: new Date().toISOString(), updates: 0 },
      ]);
      client.setQueryData(["inbox", "ws-1", "list"], [other]);
    });

    const row = screen.getByRole("link", { name: /^Open one/ });
    await waitFor(() => expect(row).toHaveTextContent(/Back .* · Leaves the Inbox when you move on/));
    expect(row).toHaveAttribute("aria-current", "page");
    expect(row).not.toHaveTextContent("Archived");
  });

  it("caps the list at the chosen number of rows, with the rest behind Show more", () => {
    useSidebarTreeStore.setState({ inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, limit: 5 } });
    renderSection(
      Array.from({ length: 7 }, (_, i) =>
        item({ id: `n${i}`, issue_id: `issue-${i}`, title: `Issue ${i}`, created_at: `2026-11-0${i + 1}T08:00:00Z` }),
      ),
    );
    expect(rows()).toHaveLength(5);

    fireEvent.click(screen.getByRole("button", { name: "Show 2 more..." }));
    expect(rows()).toHaveLength(7);
  });

  it("lights only the row of the issue the Inbox page has open", () => {
    renderSection(
      [
        item({ id: "n1", issue_id: "issue-a", title: "Open one" }),
        item({ id: "n2", issue_id: "issue-b", title: "Other one", created_at: "2026-11-09T08:00:00Z" }),
      ],
      "issue-a",
    );
    expect(screen.getByRole("link", { name: /^Open one/ })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: /^Other one/ })).not.toHaveAttribute("aria-current");
  });

  it("shows the unread count beside the label unless turned off", () => {
    state.unread = 3;
    const { unmount } = renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    const header = () => screen.getByRole("button", { name: "Inbox" }).closest("div")!;
    expect(header()).toHaveTextContent("3");
    unmount();

    useSidebarTreeStore.setState({ inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, showCount: false } });
    renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    expect(header()).not.toHaveTextContent("3");
  });

  it("collapses from the section label", async () => {
    renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    fireEvent.click(screen.getByRole("button", { name: "Inbox" }));
    expect(useSidebarTreeStore.getState().inboxCollapsed).toBe(true);
    await waitFor(() => expect(rows()).toHaveLength(0));
  });

  it("collapses from its open tab, and a closed tab opens the section on that list", () => {
    renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    const inbox = screen.getByRole("tab", { name: /^Inbox/ });
    expect(inbox).toHaveAttribute("aria-expanded", "true");
    fireEvent.click(inbox);
    expect(useSidebarTreeStore.getState().inboxCollapsed).toBe(true);
    expect(inbox).toHaveAttribute("aria-expanded", "false");

    fireEvent.click(screen.getByRole("tab", { name: /^Snoozed/ }));
    expect(useSidebarTreeStore.getState().inboxCollapsed).toBe(false);
    expect(screen.getByRole("tab", { name: /^Snoozed/ })).toHaveAttribute("aria-selected", "true");
  });

  it("names only the open tab on screen, and keeps focus on the tab the arrow keys move to", () => {
    renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    const tablist = screen.getByRole("tablist");
    // A closed tab's name is folded away, still naming the tab for a screen reader.
    const label = (name: string) => within(tablist).getByText(name).closest("[data-tab-label]");
    expect(label("Inbox")).not.toHaveClass("opacity-0");
    expect(label("Snoozed")).toHaveClass("w-0", "opacity-0");

    const inbox = screen.getByRole("tab", { name: /^Inbox/ });
    inbox.focus();
    fireEvent.keyDown(inbox, { key: "ArrowRight" });
    const snoozed = screen.getByRole("tab", { name: /^Snoozed/ });
    expect(snoozed).toHaveAttribute("aria-selected", "true");
    expect(snoozed).toHaveFocus();
    expect(label("Snoozed")).not.toHaveClass("opacity-0");
    expect(label("Inbox")).toHaveClass("w-0", "opacity-0");
  });

  it("opens the display settings from their own button, as the other sections do", async () => {
    renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    fireEvent.click(screen.getByRole("button", { name: "Inbox display" }));
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
  });

  it("opens the filter from the actions menu", async () => {
    renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    expect(screen.queryByRole("button", { name: "Filter inbox" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: "Inbox actions" }));
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitem", { name: "Filter" }));
    expect(await screen.findByPlaceholderText("Filter by field or value...")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("menuitem", { name: /^Read state/ }));
    fireEvent.click(await screen.findByRole("menuitemcheckbox", { name: /^Unread/ }));
    expect(useInboxFilterStore.getState().filtersByWorkspace["ws-1"]?.unreadOnly).toBe(true);
    expect(screen.getByPlaceholderText("Filter by field or value...")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "1 active filter" })).toBeInTheDocument();
  });

  it("is the whole list on a wide screen: no link out to another one, and a plain line when empty", () => {
    const { unmount } = renderSection([item({ id: "n1", issue_id: "issue-a", title: "Only one" })]);
    expect(screen.queryByRole("link", { name: "All notifications" })).toBeNull();
    expect(screen.getAllByRole("link").map((link) => link.getAttribute("href"))).toEqual(["/acme/inbox?issue=issue-a"]);
    unmount();

    renderSection([]);
    expect(rows()).toHaveLength(0);
    expect(screen.getByText("No notifications")).not.toHaveAttribute("href");
    expect(screen.queryByRole("link")).toBeNull();
  });

  it("links to the full list on a phone, where the sidebar sits behind a drawer", () => {
    state.compact = true;
    const { unmount } = renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    expect(screen.getByRole("link", { name: "All notifications" })).toHaveAttribute("href", "/acme/inbox");
    unmount();

    renderSection([], null, "/acme/inbox");
    expect(screen.getByRole("link", { name: "All notifications" })).toHaveAttribute("aria-current", "page");
  });

  it("lights the row the Inbox page has open", () => {
    renderSection(
      [
        item({ id: "n1", issue_id: null, title: "Autopilot paused", type: "autopilot_paused" }),
        item({ id: "n2", issue_id: "issue-b", title: "Other one", created_at: "2026-11-09T08:00:00Z" }),
      ],
      null,
      "/acme/inbox?issue=n1",
    );
    expect(screen.getByRole("link", { name: /^Autopilot paused/ })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: /^Other one/ })).not.toHaveAttribute("aria-current");
  });

  it("offers the bulk actions from the section header", async () => {
    renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    const open = async () => {
      fireEvent.click(screen.getByRole("button", { name: "Inbox actions" }));
      return screen.findByRole("menu");
    };

    fireEvent.click(within(await open()).getByRole("menuitem", { name: "Mark all as read" }));
    expect(state.markAllRead).toHaveBeenCalledTimes(1);
    fireEvent.click(within(await open()).getByRole("menuitem", { name: "Archive all read" }));
    expect(state.archiveAllRead).toHaveBeenCalledTimes(1);
    fireEvent.click(within(await open()).getByRole("menuitem", { name: "Archive completed" }));
    expect(state.archiveCompleted).toHaveBeenCalledTimes(1);
    fireEvent.click(within(await open()).getByRole("menuitem", { name: "Archive all" }));
    expect(state.archiveAll).not.toHaveBeenCalled();
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Archive all" }));
    expect(state.archiveAll).toHaveBeenCalledTimes(1);
  });

  it("cancels archive all without mutating when dismissed from the confirmation dialog", async () => {
    renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    fireEvent.click(screen.getByRole("button", { name: "Inbox actions" }));
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitem", { name: "Archive all" }));
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(state.archiveAll).not.toHaveBeenCalled();
  });

  it("shows the unfiltered active row count in the archive-all dialog even when filters narrow the list", async () => {
    useInboxFilterStore.getState().toggleUnreadOnly("ws-1");
    renderSection([
      item({ id: "n1", issue_id: "issue-a", read: false }),
      item({ id: "n2", issue_id: "issue-b", read: true }),
      item({ id: "n3", issue_id: "issue-c", read: true }),
      // Duplicate notification for issue-c should not double-count.
      item({ id: "n4", issue_id: "issue-c", read: true, created_at: "2026-11-10T07:00:00Z" }),
      // Snoozed issue is left untouched by archive-all and must not be counted.
      item({ id: "n5", issue_id: "issue-d", read: false, snoozed_until: "2099-01-01T09:00:00Z" }),
    ]);

    expect(rows()).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Inbox actions" }));
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitem", { name: "Archive all" }));
    const dialog = await screen.findByRole("alertdialog");
    expect(dialog).toHaveTextContent("All 3 active notifications in your inbox will move to Archived.");
  });

  it("keeps InboxContextMenuProvider actions referentially stable across navigation re-renders", () => {
    contextMenuActionsSeen.length = 0;
    const items = [
      item({ id: "n1", issue_id: "issue-a", title: "Row A" }),
      item({ id: "n2", issue_id: "issue-b", title: "Row B", created_at: "2026-11-10T07:00:00Z" }),
    ];
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    client.setQueryData(["inbox", "ws-1", "list"], items);
    client.setQueryData(snoozedInboxOptions("ws-1").queryKey, []);
    client.setQueryData(agentListOptions("ws-1").queryKey, []);
    client.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, []);

    const nav = adapter("/acme/issues");
    let setPathname!: (next: string) => void;
    function NavigatingHarness() {
      const [pathname, updatePathname] = useState("/acme/issues");
      setPathname = updatePathname;
      return <Harness pathname={pathname} openKey={null} />;
    }

    renderWithI18n(
      <QueryClientProvider client={client}>
        <NavigationProvider value={nav}>
          <NavigatingHarness />
        </NavigationProvider>
      </QueryClientProvider>,
    );
    expect(rows()).toHaveLength(2);
    const initialActions = contextMenuActionsSeen.at(-1);
    expect(initialActions).toBeDefined();

    act(() => setPathname("/acme/projects"));
    expect(contextMenuActionsSeen.length).toBeGreaterThanOrEqual(2);
    expect(contextMenuActionsSeen.at(-1)).toBe(initialActions);
  });

  it("offers right-click mark unread and delete on desktop sidebar rows", async () => {
    state.deleteInbox.mockClear();
    useInboxSelectionStore.setState({ manualUnreadIdByWorkspace: {} });
    const { nav } = renderSection(
      [
        item({ id: "n1", issue_id: "issue-a", title: "Open one", read: true }),
        item({ id: "n2", issue_id: "issue-b", title: "Next one", read: true, created_at: "2026-11-09T08:00:00Z" }),
      ],
      "issue-a",
      "/acme/inbox?issue=issue-a",
    );

    fireEvent.contextMenu(rows()[0]!.firstElementChild!);
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitem", { name: "Mark as unread" }));
    expect(state.markUnread).toHaveBeenCalledWith("n1", expect.any(Object));
    expect(useInboxSelectionStore.getState().manualUnreadIdByWorkspace["ws-1"]).toBe("n1");

    fireEvent.contextMenu(rows()[0]!.firstElementChild!);
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitem", { name: "Delete" }));
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(state.deleteInbox).toHaveBeenCalledWith("n1"));
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=issue-b");
  });

  it("offers right-click unarchive and delete on archived sidebar rows", async () => {
    state.deleteInbox.mockClear();
    apiMock.listArchivedInboxPage.mockResolvedValue({
      items: [item({ id: "a1", issue_id: "issue-a", title: "Archived one", archived: true })],
      nextCursor: null,
      hasMore: false,
    });
    renderSection([], null, "/acme/inbox?view=archived");
    const archivedRow = await screen.findByRole("link", { name: /^Archived one/ });
    fireEvent.contextMenu(archivedRow.parentElement!);

    const menu = await screen.findByRole("menu");
    expect(within(menu).queryByRole("menuitem", { name: "Mark as unread" })).toBeNull();
    expect(within(menu).getByRole("menuitem", { name: "Unarchive" })).toBeInTheDocument();
    fireEvent.click(within(menu).getByRole("menuitem", { name: "Delete" }));
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(state.deleteInbox).toHaveBeenCalledWith("a1"));
  });

  it("opens the next row a bulk archive leaves when it takes the open one", async () => {
    const { nav } = renderSection(
      [
        item({ id: "n1", issue_id: "issue-a", title: "Open one", read: true }),
        item({ id: "n2", issue_id: "issue-b", title: "Read one", read: true, created_at: "2026-11-09T08:00:00Z" }),
        item({ id: "n3", issue_id: "issue-c", title: "Unread one", created_at: "2026-11-08T08:00:00Z" }),
      ],
      "issue-a",
    );
    fireEvent.click(screen.getByRole("button", { name: "Inbox actions" }));
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitem", { name: "Archive all read" }));
    expect(state.archiveAllRead).toHaveBeenCalledTimes(1);
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=issue-c");
  });

  it("moves to Inbox Zero once a bulk archive takes every row", async () => {
    const { nav } = renderSection([item({ id: "n1", issue_id: "issue-a", title: "Open one" })], "issue-a");
    fireEvent.click(screen.getByRole("button", { name: "Inbox actions" }));
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitem", { name: "Archive all" }));
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Archive all" }));
    expect(state.archiveAll).toHaveBeenCalledTimes(1);
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox");
    expect(useInboxSelectionStore.getState().leftRowKeyByWorkspace["ws-1"]).toBe("issue-a");
    expect(useInboxSelectionStore.getState().lastClearedByWorkspace["ws-1"]).toMatchObject({
      kind: "batch_archived",
      count: 1,
      itemIds: ["n1"],
    });
  });

  it("keeps a bulk archive that spares the open row from moving it", async () => {
    const { nav } = renderSection(
      [
        item({ id: "n1", issue_id: "issue-a", title: "Open one" }),
        item({ id: "n2", issue_id: "issue-b", title: "Read one", read: true, created_at: "2026-11-09T08:00:00Z" }),
      ],
      "issue-a",
    );
    fireEvent.click(screen.getByRole("button", { name: "Inbox actions" }));
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitem", { name: "Archive all read" }));
    expect(nav.push).not.toHaveBeenCalled();
    expect(useInboxSelectionStore.getState().leftRowKeyByWorkspace["ws-1"]).toBeUndefined();
  });

  it("says a row is back from a snooze, with what arrived meanwhile, until it is read", () => {
    const snooze = { snoozed_until: "2026-11-12T09:00:00Z", snoozed_at: "2026-11-10T09:00:00Z" };
    const items = [
      item({ id: "n3", issue_id: "issue-a", created_at: "2026-11-11T10:00:00Z", ...snooze }),
      item({ id: "n2", issue_id: "issue-a", created_at: "2026-11-11T08:00:00Z", ...snooze }),
      item({ id: "n1", issue_id: "issue-a", created_at: "2026-11-10T08:00:00Z", ...snooze }),
      item({ id: "n4", issue_id: "issue-b", title: "Newer", created_at: "2026-11-11T12:00:00Z" }),
    ];
    const { unmount } = renderSection(items);
    // Back at 09:00 on the 12th, so it sorts above the newer notification.
    expect(rows()[0]).toHaveTextContent("Snooze ended · 2 updates while snoozed");
    expect(rows()[1]).toHaveTextContent("Newer");
    unmount();

    renderSection(items.map((row) => ({ ...row, read: true })));
    expect(rows()[0]).not.toHaveTextContent("Snooze ended");
  });

  it("snoozes a row from its hover button", async () => {
    renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    fireEvent.click(screen.getByRole("button", { name: "Snooze" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /^1 day/ }));
    expect(state.snooze).toHaveBeenCalledWith({ id: "n1", until: expect.any(Date) });
  });

  it("puts the snoozed issues in a tab of their own, with their count, each opening on the Inbox page", async () => {
    const until = "2099-01-01T09:00:00Z";
    renderSection([item({ id: "n1", issue_id: "issue-a" })], null, "/acme/issues", [], [
      { issue_id: "issue-z", issue_identifier: "MUL-26", item_id: "z1", snoozed_until: until, snoozed_at: "2026-11-10T09:00:00Z", updates: 0 },
    ]);

    const tab = screen.getByRole("tab", { name: /^Snoozed/ });
    expect(tab).toHaveTextContent("1");
    fireEvent.click(tab);
    expect(tab).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByRole("link", { name: /^MUL-26/ })).toHaveAttribute("href", "/acme/inbox?view=snoozed&issue=issue-z");
    expect(rows()).toHaveLength(0);
    // The bulk actions act on the live Inbox and snoozed issues have no filter: no actions menu here.
    expect(screen.queryByRole("button", { name: "Inbox actions" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Bring back now" }));
    expect(state.unsnooze).toHaveBeenCalledWith(expect.objectContaining({ id: "z1" }));
  });

  it("swaps the lists at once with reduced motion", () => {
    useAppearanceStore.getState().set({ reduceMotion: true });
    renderSection([item({ id: "n1", issue_id: "issue-a" })], null, "/acme/issues", [], [
      { issue_id: "issue-z", issue_identifier: "MUL-26", item_id: "z1", snoozed_until: "2099-01-01T09:00:00Z", snoozed_at: "2026-11-10T09:00:00Z", updates: 0 },
    ]);
    try {
      fireEvent.click(screen.getByRole("tab", { name: /^Snoozed/ }));
      expect(rows()).toHaveLength(0);
      expect(screen.getByRole("link", { name: /^MUL-26/ })).toBeInTheDocument();
    } finally {
      useAppearanceStore.getState().reset();
    }
  });

  it("lets go of the list's height when another tab opens while the archive is still loading", async () => {
    apiMock.listArchivedInboxPage.mockReturnValue(new Promise(() => {}));
    renderSection([item({ id: "n1", issue_id: "issue-a" })], null, "/acme/issues", [], [
      { issue_id: "issue-z", issue_identifier: "MUL-26", item_id: "z1", snoozed_until: "2099-01-01T09:00:00Z", snoozed_at: "2026-11-10T09:00:00Z", updates: 0 },
    ]);
    const body = () => document.querySelector<HTMLElement>("[data-inbox-body]")!;

    fireEvent.click(screen.getByRole("tab", { name: /^Archived/ }));
    await waitFor(() => expect(rows()).toHaveLength(0));
    expect(body()).toHaveClass("overflow-hidden");

    fireEvent.click(screen.getByRole("tab", { name: /^Snoozed/ }));
    expect(await screen.findByRole("link", { name: /^MUL-26/ })).toBeInTheDocument();
    await waitFor(() => expect(body()).not.toHaveClass("overflow-hidden"));
    expect(body().style.height).not.toMatch(/px$/);
  });

  it("lists the archive in a tab, each row opening there and restoring from its button", async () => {
    apiMock.listArchivedInboxPage.mockResolvedValue({
      items: [item({ id: "a1", issue_id: "issue-x", title: "Done with this", archived: true })],
      nextCursor: null,
      hasMore: false,
    });
    renderSection([item({ id: "n1", issue_id: "issue-a" })]);

    fireEvent.click(screen.getByRole("tab", { name: /^Archived/ }));
    const link = await screen.findByRole("link", { name: /^Done with this/ });
    expect(link).toHaveAttribute("href", "/acme/inbox?view=archived&issue=issue-x");
    fireEvent.click(screen.getByRole("button", { name: "Move back to Inbox" }));
    expect(state.unarchive).toHaveBeenCalledWith("a1");
  });

  it("lands again on every click of an archived row, and moves the page on when the open one is restored", async () => {
    apiMock.listArchivedInboxPage.mockResolvedValue({
      items: [
        item({ id: "a1", issue_id: "issue-x", title: "Done with this", archived: true }),
        item({ id: "a2", issue_id: "issue-y", title: "And this", archived: true }),
      ],
      nextCursor: null,
      hasMore: false,
    });
    const { nav } = renderSection([], null, "/acme/inbox?view=archived&issue=issue-x");

    const link = await screen.findByRole("link", { name: /^Done with this/ });
    fireEvent.click(link);
    const first = useInboxSelectionStore.getState().landing;
    fireEvent.click(link);
    expect(first?.issueId).toBe("issue-x");
    expect(useInboxSelectionStore.getState().landing).toEqual({ issueId: "issue-x", token: first!.token + 1, landed: false });

    const [restoreOpen] = screen.getAllByRole("button", { name: "Move back to Inbox" });
    fireEvent.click(restoreOpen!);
    expect(nav.replace).toHaveBeenCalledWith("/acme/inbox?view=archived&issue=issue-y");
    expect(state.unarchive).toHaveBeenCalledWith("a1");
    expect(useInboxSelectionStore.getState().leftRowKeyByWorkspace["ws-1"]).toBe("issue-x");
    expect(useInboxSelectionStore.getState().transitionByWorkspace["ws-1"]).toMatchObject({
      targetKey: "issue-y",
      direction: "next",
    });
  });

  it("follows the Inbox page's tab there, and switches the page's tab from the header when nothing is open", () => {
    const { nav } = renderSection([item({ id: "n1", issue_id: "issue-a" })], null, "/acme/inbox?view=archived");
    expect(screen.getByRole("tab", { name: /^Archived/ })).toHaveAttribute("aria-selected", "true");

    fireEvent.click(screen.getByRole("tab", { name: /^Inbox/ }));
    expect(nav.replace).toHaveBeenCalledWith("/acme/inbox");
    expect(useInboxSelectionStore.getState().sectionTabByWorkspace["ws-1"]).toBe("inbox");
  });

  it("keeps an open archived issue on screen when switching sidebar tabs, and keeps it lit when switching back", async () => {
    useAppearanceStore.getState().set({ reduceMotion: true });
    apiMock.listArchivedInboxPage.mockResolvedValue({
      items: [item({ id: "a1", issue_id: "issue-x", title: "Archived one", archived: true })],
      nextCursor: null,
      hasMore: false,
    });
    try {
      const { nav } = renderSection(
        [item({ id: "n1", issue_id: "issue-a", title: "Live one" })],
        null,
        "/acme/inbox?view=archived&issue=issue-x",
      );
      expect(await screen.findByRole("link", { name: /^Archived one/ })).toHaveAttribute("aria-current", "page");

      fireEvent.click(screen.getByRole("tab", { name: /^Inbox/ }));
      expect(nav.replace).not.toHaveBeenCalled();
      expect(screen.getByRole("tab", { name: /^Inbox/ })).toHaveAttribute("aria-selected", "true");
      expect(screen.getByRole("link", { name: /^Live one/ })).not.toHaveAttribute("aria-current");

      fireEvent.click(screen.getByRole("tab", { name: /^Archived/ }));
      expect(nav.replace).not.toHaveBeenCalled();
      expect(screen.getByRole("tab", { name: /^Archived/ })).toHaveAttribute("aria-selected", "true");
      expect(await screen.findByRole("link", { name: /^Archived one/ })).toHaveAttribute("aria-current", "page");
    } finally {
      useAppearanceStore.getState().reset();
    }
  });

  it("keeps an open live Inbox or Snoozed issue on screen when switching sidebar tabs", async () => {
    useAppearanceStore.getState().set({ reduceMotion: true });
    apiMock.listArchivedInboxPage.mockResolvedValue({
      items: [item({ id: "a1", issue_id: "issue-x", title: "Archived one", archived: true })],
      nextCursor: null,
      hasMore: false,
    });
    try {
      const first = renderSection([item({ id: "n1", issue_id: "issue-a", title: "Live one" })], "issue-a");
      fireEvent.click(screen.getByRole("tab", { name: /^Archived/ }));
      expect(first.nav.replace).not.toHaveBeenCalled();
      expect(await screen.findByRole("link", { name: /^Archived one/ })).toBeInTheDocument();
      first.unmount();

      useInboxSelectionStore.setState({ sectionTabByWorkspace: {}, sectionTabSyncedUrlByWorkspace: {} });
      const second = renderSection(
        [item({ id: "n1", issue_id: "issue-a", title: "Live one" })],
        null,
        "/acme/inbox?view=snoozed&issue=issue-z",
        [],
        [
          {
            issue_id: "issue-z",
            issue_identifier: "MUL-26",
            item_id: "z1",
            snoozed_until: "2099-01-01T09:00:00Z",
            snoozed_at: "2026-11-10T09:00:00Z",
            updates: 0,
          },
        ],
      );
      fireEvent.click(screen.getByRole("tab", { name: /^Inbox/ }));
      expect(second.nav.replace).not.toHaveBeenCalled();
      expect(screen.getByRole("link", { name: /^Live one/ })).toBeInTheDocument();
    } finally {
      useAppearanceStore.getState().reset();
    }
  });

  it("keeps an open archived row visible past the limit and offers Load more when more pages exist within the limit", async () => {
    useSidebarTreeStore.setState({ inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, limit: 5 } });
    apiMock.listArchivedInboxPage.mockResolvedValue({
      items: [
        ...Array.from({ length: 5 }, (_, i) =>
          item({ id: `a${i + 1}`, issue_id: `issue-${i + 1}`, title: `Row ${i + 1}`, archived: true }),
        ),
        item({ id: "a6", issue_id: "issue-6", title: "Open archived", archived: true }),
      ],
      nextCursor: "c2",
      hasMore: true,
    });
    const first = renderSection([], null, "/acme/inbox?view=archived&issue=issue-6");
    expect(await screen.findByRole("link", { name: /^Open archived/ })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("button", { name: "Show 1 more..." })).toBeInTheDocument();
    first.unmount();

    useSidebarTreeStore.setState({ inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, limit: 10 } });
    apiMock.listArchivedInboxPage.mockResolvedValue({
      items: [item({ id: "a1", issue_id: "issue-1", title: "Only page row", archived: true })],
      nextCursor: "c2",
      hasMore: true,
    });
    renderSection([], null, "/acme/inbox?view=archived");
    expect(await screen.findByRole("button", { name: "Load more" })).toBeInTheDocument();
  });

  it("says when filters hide every archived row and offers to clear them", async () => {
    useInboxFilterStore.getState().toggleFilterValue("ws-1", "type", "mentions");
    apiMock.listArchivedInboxPage.mockResolvedValue({ items: [], nextCursor: null, hasMore: false });
    renderSection([], null, "/acme/inbox?view=archived");

    expect(await screen.findByText("No notifications match your filters")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Clear filters" }));
    expect(useInboxFilterStore.getState().filtersByWorkspace["ws-1"]).toBeUndefined();
  });
});

describe("InboxSection with every row shown", () => {
  const ROW_HEIGHT = 50;
  const VIEWPORT_HEIGHT = 600;

  /**
   * jsdom has no layout: the virtualizer reads the panel's and each row's
   * height from `offsetHeight`, and the list's offset from `getBoundingClientRect`.
   */
  function installLayout() {
    const offsetHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetHeight")!;
    const offsetWidth = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "offsetWidth")!;
    const rect = Element.prototype.getBoundingClientRect;
    Object.defineProperty(HTMLElement.prototype, "offsetHeight", {
      configurable: true,
      get(this: HTMLElement) {
        return this.matches("[data-inbox-row]") ? ROW_HEIGHT : VIEWPORT_HEIGHT;
      },
    });
    Object.defineProperty(HTMLElement.prototype, "offsetWidth", { configurable: true, get: () => 280 });
    Element.prototype.getBoundingClientRect = () =>
      ({ width: 280, height: ROW_HEIGHT, top: 0, left: 0, right: 280, bottom: ROW_HEIGHT, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect;
    return () => {
      Object.defineProperty(HTMLElement.prototype, "offsetHeight", offsetHeight);
      Object.defineProperty(HTMLElement.prototype, "offsetWidth", offsetWidth);
      Element.prototype.getBoundingClientRect = rect;
    };
  }

  function renderInPanel(count: number, openIssueId: string | null = null, above: ReactNode = null) {
    const items = Array.from({ length: count }, (_, i) =>
      item({
        id: `n${i}`,
        issue_id: `issue-${i}`,
        title: `Issue ${i}`,
        created_at: new Date(Date.UTC(2026, 10, 10) - i * 60_000).toISOString(),
      }),
    );
    const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    client.setQueryData(["inbox", "ws-1", "list"], items);
    client.setQueryData(snoozedInboxOptions("ws-1").queryKey, []);
    client.setQueryData(agentListOptions("ws-1").queryKey, []);
    client.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, []);
    return renderWithI18n(
      <QueryClientProvider client={client}>
        <NavigationProvider value={adapter(openIssueId ? `/acme/inbox?issue=${openIssueId}` : "/acme/issues")}>
          <PanelBody>
            {above}
            <Harness pathname={openIssueId ? "/acme/inbox" : "/acme/issues"} openKey={openIssueId} />
          </PanelBody>
        </NavigationProvider>
      </QueryClientProvider>,
    );
  }

  it("mounts only the rows near the panel's viewport, and the rest as the panel scrolls", async () => {
    const restore = installLayout();
    try {
      const { container } = renderInPanel(300, "issue-3");
      fireEvent.click(screen.getByRole("button", { name: /^Show \d+ more/ }));

      await waitFor(() => expect(rows().length).toBeGreaterThan(0));
      expect(rows().length).toBeLessThan(40);
      expect(screen.getByRole("link", { name: /^Issue 0/ })).toBeInTheDocument();
      expect(screen.getByRole("link", { name: /^Issue 3/ })).toHaveAttribute("aria-current", "page");
      expect(screen.queryByRole("link", { name: /^Issue 299/ })).toBeNull();
      // The rows not mounted still take their room, so the panel scrolls as far.
      const list = container.querySelector<HTMLElement>("[data-sidebar-list]")!;
      expect(parseFloat(list.style.paddingBottom)).toBeGreaterThan((300 - 40) * ROW_HEIGHT);

      const scroller = container.firstElementChild as HTMLElement;
      act(() => {
        // jsdom keeps scrollTop at 0 unless it is defined.
        Object.defineProperty(scroller, "scrollTop", { configurable: true, value: 300 * (ROW_HEIGHT + 2) });
        fireEvent.scroll(scroller);
      });
      await waitFor(() => expect(screen.getByRole("link", { name: /^Issue 299/ })).toBeInTheDocument());
      expect(screen.queryByRole("link", { name: /^Issue 0/ })).toBeNull();
      expect(rows().length).toBeLessThan(40);
    } finally {
      restore();
    }
  });

  it("follows a section above it that mounts or changes height", async () => {
    const restore = installLayout();
    const rect = Element.prototype.getBoundingClientRect;
    // Where the list starts below the panel's top, as the section above it sets.
    let listTop = 0;
    Element.prototype.getBoundingClientRect = function (this: Element) {
      const top = this.matches("[data-sidebar-list]") ? listTop : 0;
      return { width: 280, height: ROW_HEIGHT, top, left: 0, right: 280, bottom: top + ROW_HEIGHT, x: 0, y: top, toJSON: () => ({}) } as DOMRect;
    };
    const observers: { callback: ResizeObserverCallback; targets: Set<Element> }[] = [];
    vi.stubGlobal(
      "ResizeObserver",
      class {
        private entry: { callback: ResizeObserverCallback; targets: Set<Element> };
        constructor(callback: ResizeObserverCallback) {
          this.entry = { callback, targets: new Set() };
          observers.push(this.entry);
        }
        observe(target: Element) {
          this.entry.targets.add(target);
        }
        unobserve(target: Element) {
          this.entry.targets.delete(target);
        }
        disconnect() {
          this.entry.targets.clear();
        }
      },
    );
    const resize = (target: Element) => {
      for (const { callback, targets } of observers) {
        if (targets.has(target)) callback([], {} as ResizeObserver);
      }
    };
    const showAbove: { set?: (shown: boolean) => void } = {};
    function Above() {
      const [shown, setShown] = useState(false);
      showAbove.set = setShown;
      return shown ? <section data-testid="above" /> : null;
    }
    try {
      renderInPanel(300, null, <Above />);
      fireEvent.click(screen.getByRole("button", { name: /^Show \d+ more/ }));
      await waitFor(() => expect(rows().length).toBeGreaterThan(0));
      const atTop = rows().length;

      // A section mounts above the list and pushes it most of a viewport down.
      listTop = 500;
      act(() => showAbove.set!(true));
      await waitFor(() => expect(rows().length).toBeLessThan(atTop));

      // It collapses: the panel keeps its size, the section above shrinks.
      listTop = 0;
      act(() => resize(screen.getByTestId("above")));
      await waitFor(() => expect(rows()).toHaveLength(atTop));
    } finally {
      vi.unstubAllGlobals();
      Element.prototype.getBoundingClientRect = rect;
      restore();
    }
  });

  it("keeps a short list fully mounted", () => {
    useSidebarTreeStore.setState({ inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, limit: 5 } });
    renderInPanel(30);
    fireEvent.click(screen.getByRole("button", { name: "Show 25 more..." }));
    expect(rows()).toHaveLength(30);
  });
});

describe("InboxSection while the Inbox loads", () => {
  // The app's own client, so the inbox retry policy is the one under test.
  function renderUnloaded() {
    const client = createQueryClient();
    client.setQueryData(snoozedInboxOptions("ws-1").queryKey, []);
    client.setQueryData(agentListOptions("ws-1").queryKey, []);
    client.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, []);
    const nav = adapter("/acme/issues");
    renderWithI18n(
      <QueryClientProvider client={client}>
        <NavigationProvider value={nav}>
          <Harness pathname={nav.pathname} openKey={null} />
        </NavigationProvider>
      </QueryClientProvider>,
    );
    return client;
  }
  const busy = () => new ApiError("inbox reads are busy", 503, "Service Unavailable", undefined, 1000);
  const advance = (ms: number) => act(() => vi.advanceTimersByTimeAsync(ms));

  beforeEach(() => {
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    apiMock.listInbox.mockReset();
  });
  afterEach(() => vi.useRealTimers());

  it("shows the notifications once a busy server answers", async () => {
    apiMock.listInbox
      .mockRejectedValueOnce(busy())
      .mockResolvedValue([item({ id: "n1", issue_id: "issue-a", title: "After the wait" })]);
    renderUnloaded();
    await advance(0);
    // Waiting out Retry-After is not an empty Inbox.
    expect(screen.queryByText("No notifications")).toBeNull();
    expect(screen.queryByRole("alert")).toBeNull();

    await advance(2000);
    expect(apiMock.listInbox).toHaveBeenCalledTimes(2);
    expect(rows().map((row) => row.textContent)).toEqual([expect.stringContaining("After the wait")]);
  });

  it("says the Inbox could not load, with a retry, when the server stays busy", async () => {
    apiMock.listInbox.mockRejectedValue(busy());
    renderUnloaded();
    await advance(30_000);

    expect(apiMock.listInbox).toHaveBeenCalledTimes(4);
    expect(screen.getByRole("alert")).toHaveTextContent("Failed to load notifications");
    expect(screen.queryByText("No notifications")).toBeNull();

    apiMock.listInbox.mockResolvedValue([item({ id: "n1", issue_id: "issue-a", title: "Back again" })]);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await advance(0);
    expect(screen.queryByRole("alert")).toBeNull();
    expect(rows().map((row) => row.textContent)).toEqual([expect.stringContaining("Back again")]);
  });

  it("keeps saying it could not load while a retry waits on a busy server", async () => {
    apiMock.listInbox.mockRejectedValue(busy());
    renderUnloaded();
    await advance(30_000);
    expect(apiMock.listInbox).toHaveBeenCalledTimes(4);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    await advance(0);
    expect(apiMock.listInbox).toHaveBeenCalledTimes(5);
    expect(screen.getByRole("alert")).toHaveTextContent("Failed to load notifications");
    expect(screen.getByRole("button", { name: "Retry" })).toBeDisabled();

    await advance(30_000);
    expect(apiMock.listInbox).toHaveBeenCalledTimes(8);
    expect(screen.getByRole("button", { name: "Retry" })).toBeEnabled();
  });

  it("says No notifications when the server answers with none", async () => {
    apiMock.listInbox.mockResolvedValue([]);
    renderUnloaded();
    await advance(0);
    expect(screen.getByText("No notifications")).toBeInTheDocument();
    expect(screen.queryByRole("alert")).toBeNull();
  });

  it("keeps the rows it has when a refresh fails", async () => {
    apiMock.listInbox.mockResolvedValueOnce([item({ id: "n1", issue_id: "issue-a", title: "Still here" })]);
    const client = renderUnloaded();
    await advance(0);
    expect(rows()).toHaveLength(1);

    apiMock.listInbox.mockRejectedValue(busy());
    await act(async () => {
      void client.invalidateQueries({ queryKey: ["inbox", "ws-1", "list"] });
    });
    await advance(30_000);
    expect(apiMock.listInbox).toHaveBeenCalledTimes(5);
    expect(rows().map((row) => row.textContent)).toEqual([expect.stringContaining("Still here")]);
    expect(screen.queryByRole("alert")).toBeNull();
  });
});

describe("InboxSection kept open row while an agent works", () => {
  it("shows the running agent status on the kept row instead of generic Archived", async () => {
    const first = item({ id: "n1", issue_id: "issue-a", title: "Run agent on this", read: true });
    const second = item({ id: "n2", issue_id: "issue-b", title: "Next item", read: true });
    const { client } = renderSection(
      [first, second],
      "issue-a",
      "/acme/inbox?issue=issue-a",
      [{ id: "t1", issue_id: "issue-a", agent_id: "agent-1", status: "running" }],
    );
    // The open issue is auto-archived while its agent runs.
    act(() => client.setQueryData(["inbox", "ws-1", "list"], [second]));

    expect(
      await screen.findByText("Leo Park is working · Leaves the Inbox when you move on"),
    ).toBeInTheDocument();
  });
});
