import type { ReactNode } from "react";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
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
  snooze: vi.fn(() => Promise.resolve()),
  unsnooze: vi.fn(() => Promise.resolve()),
}));

const apiMock = vi.hoisted(() => ({ listInbox: vi.fn() }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
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
  useUnarchiveInbox: () => ({ mutateAsync: state.unarchive }),
  useMarkAllInboxRead: () => ({ mutate: state.markAllRead }),
  useArchiveAllInbox: () => ({ mutateAsync: async () => (await state.archiveAll()) ?? { count: 1 } }),
  useArchiveAllReadInbox: () => ({ mutateAsync: async () => (await state.archiveAllRead()) ?? { count: 1 } }),
  useArchiveCompletedInbox: () => ({ mutateAsync: async () => (await state.archiveCompleted()) ?? { count: 1 } }),
  useMarkInboxRead: () => ({ mutate: state.markRead }),
  useMarkInboxUnread: () => ({ mutate: state.markUnread }),
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
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Leo Park", getActorInitials: () => "LP", getActorAvatarUrl: () => null }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
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

function Harness({ activeIssueId, pathname, openKey }: { activeIssueId: string | null; pathname: string; openKey: string | null }) {
  const rows = useInboxSectionRows("ws-1", openKey);
  return <InboxSection wsId="ws-1" rows={rows} pathname={pathname} activeIssueId={activeIssueId} />;
}

function renderSection(
  items: InboxItem[],
  activeIssueId: string | null = null,
  url = "/acme/issues",
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
  // As the Work panel keys it: the issue opened from the section, or the Inbox page's notification.
  const openKey = nav.pathname === "/acme/inbox" ? nav.searchParams.get("issue") : activeIssueId;
  const result = renderWithI18n(
    <QueryClientProvider client={client}>
      <NavigationProvider value={nav}>
        <Harness activeIssueId={activeIssueId} pathname={nav.pathname} openKey={openKey} />
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
  useSidebarTreeStore.setState({ inboxCollapsed: false, inboxSettings: DEFAULT_SIDEBAR_INBOX_SETTINGS });
  useInboxFilterStore.getState().clearFilters("ws-1");
  useInboxSelectionStore.setState({ landing: null, heldRows: {}, leftRowKeyByWorkspace: {} });
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

  it("opens the issue at the comment it is about, and an issue-less notice on the Inbox page", () => {
    renderSection([
      item({ id: "n1", issue_id: "issue-a", title: "Commented", details: { comment_id: "c-1" } }),
      item({ id: "n2", issue_id: "issue-b", title: "Assigned", type: "issue_assigned", created_at: "2026-11-09T08:00:00Z" }),
      item({ id: "n3", issue_id: null, title: "Autopilot paused", type: "autopilot_paused", created_at: "2026-11-08T08:00:00Z" }),
    ]);

    expect(screen.getByRole("link", { name: /^Commented/ })).toHaveAttribute("href", "/acme/issues/issue-a#comment-c-1");
    expect(screen.getByRole("link", { name: /^Assigned/ })).toHaveAttribute("href", "/acme/issues/issue-b");
    expect(screen.getByRole("link", { name: /^Autopilot paused/ })).toHaveAttribute("href", "/acme/inbox?issue=n3");
  });

  it("opens the issue at its oldest unread comment, or its newest once read", () => {
    renderSection([
      item({ id: "n1", issue_id: "issue-a", title: "Unread thread", details: { comment_id: "c-2" }, created_at: "2026-11-10T09:00:00Z" }),
      item({ id: "n2", issue_id: "issue-a", title: "Unread thread", details: { comment_id: "c-1" }, created_at: "2026-11-10T08:00:00Z" }),
      item({ id: "n3", issue_id: "issue-b", title: "Read thread", read: true, details: { comment_id: "c-4" }, created_at: "2026-11-09T09:00:00Z" }),
      item({ id: "n4", issue_id: "issue-b", title: "Read thread", read: true, details: { comment_id: "c-3" }, created_at: "2026-11-09T08:00:00Z" }),
    ]);

    expect(screen.getByRole("link", { name: /^Unread thread/ })).toHaveAttribute("href", "/acme/issues/issue-a#comment-c-1");
    expect(screen.getByRole("link", { name: /^Read thread/ })).toHaveAttribute("href", "/acme/issues/issue-b#comment-c-4");
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

  it("opens an app's request on its issue at that request", () => {
    renderSection([
      item({ id: "n1", issue_id: "issue-a", title: "Confirm deploy", type: "app_action_requested", details: { request_id: "r-1" } }),
    ]);

    expect(screen.getByRole("link", { name: /^Confirm deploy/ })).toHaveAttribute("href", "/acme/issues/issue-a#app-request-r-1");
  });

  it("lands an app's request over an older comment notification on the same issue", () => {
    renderSection([
      item({ id: "app-n", issue_id: "issue-a", title: "Confirm deploy", type: "app_action_requested", details: { request_id: "r-1" }, created_at: "2026-11-10T09:00:00Z" }),
      item({ id: "comment-n", issue_id: "issue-a", title: "Older comment", type: "new_comment", details: { comment_id: "c-1" }, created_at: "2026-11-10T08:00:00Z" }),
    ]);

    expect(screen.getByRole("link", { name: /^Confirm deploy/ })).toHaveAttribute("href", "/acme/issues/issue-a#app-request-r-1");
  });

  it("asks the issue page to land again on every click, even on the issue already open", () => {
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
    expect(screen.getByRole("button", { name: "Filter inbox" })).toBeInTheDocument();
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

    expect(actions.getByRole("link", { name: "Open" })).toHaveAttribute("href", "/acme/issues/issue-a");
    fireEvent.click(actions.getByRole("button", { name: "Mark as read" }));
    expect(state.markRead).toHaveBeenCalledWith("n1");
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
    expect(nav.push).toHaveBeenCalledWith("/acme/issues/issue-b");
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

    fireEvent.click(screen.getByRole("button", { name: "Show 2 more…" }));
    expect(rows()).toHaveLength(7);
  });

  it("lights only the row of the issue opened from it", () => {
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

  it("collapses from the section label", () => {
    renderSection([item({ id: "n1", issue_id: "issue-a" })]);
    fireEvent.click(screen.getByRole("button", { name: "Inbox" }));
    expect(useSidebarTreeStore.getState().inboxCollapsed).toBe(true);
    expect(rows()).toHaveLength(0);
  });

  it("is the whole list on a wide screen: no link out to another one, and a plain line when empty", () => {
    const { unmount } = renderSection([item({ id: "n1", issue_id: "issue-a", title: "Only one" })]);
    expect(screen.queryByRole("link", { name: "All notifications" })).toBeNull();
    expect(screen.getAllByRole("link").map((link) => link.getAttribute("href"))).toEqual(["/acme/issues/issue-a"]);
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
    expect(state.archiveAll).toHaveBeenCalledTimes(1);
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
    expect(nav.push).toHaveBeenCalledWith("/acme/issues/issue-c");
  });

  it("keeps the issue open with nothing lit once a bulk archive takes every row", async () => {
    const { nav } = renderSection([item({ id: "n1", issue_id: "issue-a", title: "Open one" })], "issue-a");
    fireEvent.click(screen.getByRole("button", { name: "Inbox actions" }));
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitem", { name: "Archive all" }));
    expect(state.archiveAll).toHaveBeenCalledTimes(1);
    expect(nav.push).not.toHaveBeenCalled();
    expect(useInboxSelectionStore.getState().leftRowKeyByWorkspace["ws-1"]).toBe("issue-a");
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

  it("opens the snoozed list from the section header, with its count", async () => {
    const until = "2099-01-01T09:00:00Z";
    const { nav } = renderSection([item({ id: "n1", issue_id: "issue-a" })], null, "/acme/issues", [], [
      { issue_id: "issue-z", item_id: "z1", snoozed_until: until, snoozed_at: "2026-11-10T09:00:00Z", updates: 0 },
    ]);

    fireEvent.click(screen.getByRole("button", { name: "Inbox actions" }));
    const entry = within(await screen.findByRole("menu")).getByRole("menuitem", { name: /View snoozed/ });
    expect(entry).toHaveTextContent("1");
    fireEvent.click(entry);
    expect(nav.push).toHaveBeenCalledWith("/acme/issues?view=snoozed");
  });

  it("opens the archive from the section header", async () => {
    const { nav } = renderSection([item({ id: "n1", issue_id: "issue-a" })]);

    fireEvent.click(screen.getByRole("button", { name: "Inbox actions" }));
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitem", { name: "View archived" }));
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?view=archived");
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

  function renderInPanel(count: number, activeIssueId: string | null = null) {
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
        <NavigationProvider value={adapter("/acme/issues")}>
          <PanelBody>
            <Harness activeIssueId={activeIssueId} pathname="/acme/issues" openKey={activeIssueId} />
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

  it("keeps a short list fully mounted", () => {
    useSidebarTreeStore.setState({ inboxSettings: { ...DEFAULT_SIDEBAR_INBOX_SETTINGS, limit: 5 } });
    renderInPanel(30);
    fireEvent.click(screen.getByRole("button", { name: "Show 25 more…" }));
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
          <Harness activeIssueId={null} pathname={nav.pathname} openKey={null} />
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
