import type { ReactNode } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_SIDEBAR_INBOX_SETTINGS,
  useSidebarTreeStore,
} from "@multica/core/issues/stores/sidebar-tree-store";
import { useInboxFilterStore } from "@multica/core/inbox/filter-store";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import type { InboxItem } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { InboxSection, useInboxSectionRows } from "./inbox-section";

const state = vi.hoisted(() => ({
  unread: 0,
  compact: false,
  archive: vi.fn(),
  markAllRead: vi.fn(),
  archiveAll: vi.fn(),
  archiveAllRead: vi.fn(),
  archiveCompleted: vi.fn(),
  markRead: vi.fn(),
  markUnread: vi.fn(),
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
  useArchiveInbox: () => ({ mutate: state.archive }),
  useMarkAllInboxRead: () => ({ mutate: state.markAllRead }),
  useArchiveAllInbox: () => ({ mutate: state.archiveAll }),
  useArchiveAllReadInbox: () => ({ mutate: state.archiveAllRead }),
  useArchiveCompletedInbox: () => ({ mutate: state.archiveCompleted }),
  useMarkInboxRead: () => ({ mutate: state.markRead }),
  useMarkInboxUnread: () => ({ mutate: state.markUnread }),
}));
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Leo Park", getActorInitials: () => "LP", getActorAvatarUrl: () => null }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
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

function renderSection(items: InboxItem[], activeIssueId: string | null = null, url = "/acme/issues") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(["inbox", "ws-1", "list"], items);
  client.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, []);
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
  return { ...result, nav };
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
  useInboxSelectionStore.setState({ landing: null });
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
    expect(useInboxSelectionStore.getState().landing).toEqual({ issueId: "issue-a", token: first!.token + 1 });
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

  it("archives a row from its hover button", () => {
    renderSection([item({ id: "n1", issue_id: "issue-a", title: "Commented" })]);
    fireEvent.click(within(rows()[0]!).getByRole("button", { name: "Archive" }));
    expect(state.archive).toHaveBeenCalledWith("n1");
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

  it("opens the archive from the section header", async () => {
    const { nav } = renderSection([item({ id: "n1", issue_id: "issue-a" })]);

    fireEvent.click(screen.getByRole("button", { name: "Inbox actions" }));
    fireEvent.click(within(await screen.findByRole("menu")).getByRole("menuitem", { name: "View archived" }));
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?view=archived");
  });
});
