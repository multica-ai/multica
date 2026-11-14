import type { ReactNode } from "react";
import { fireEvent, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  DEFAULT_SIDEBAR_INBOX_SETTINGS,
  useSidebarTreeStore,
} from "@multica/core/issues/stores/sidebar-tree-store";
import type { InboxItem } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { InboxSection, useInboxSectionRows } from "./inbox-section";

const state = vi.hoisted(() => ({
  unread: 0,
  archive: vi.fn(),
  markAllRead: vi.fn(),
  archiveAll: vi.fn(),
  archiveAllRead: vi.fn(),
  archiveCompleted: vi.fn(),
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

function Harness({ activeIssueId, pathname }: { activeIssueId: string | null; pathname: string }) {
  const rows = useInboxSectionRows("ws-1");
  return <InboxSection wsId="ws-1" rows={rows} pathname={pathname} activeIssueId={activeIssueId} />;
}

function renderSection(items: InboxItem[], activeIssueId: string | null = null, url = "/acme/issues") {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  client.setQueryData(["inbox", "ws-1", "list"], items);
  const nav = adapter(url);
  const result = renderWithI18n(
    <QueryClientProvider client={client}>
      <NavigationProvider value={nav}>
        <Harness activeIssueId={activeIssueId} pathname={nav.pathname} />
      </NavigationProvider>
    </QueryClientProvider>,
  );
  return { ...result, nav };
}

const rows = () => Array.from(document.querySelectorAll<HTMLElement>("[data-inbox-row]"));

beforeEach(() => {
  state.unread = 0;
  for (const spy of [state.archive, state.markAllRead, state.archiveAll, state.archiveAllRead, state.archiveCompleted]) {
    spy.mockReset();
  }
  useSidebarTreeStore.setState({ inboxCollapsed: false, inboxSettings: DEFAULT_SIDEBAR_INBOX_SETTINGS });
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

    expect(screen.getByRole("link", { name: "Commented" })).toHaveAttribute("href", "/acme/issues/issue-a#comment-c-1");
    expect(screen.getByRole("link", { name: "Assigned" })).toHaveAttribute("href", "/acme/issues/issue-b");
    expect(screen.getByRole("link", { name: "Autopilot paused" })).toHaveAttribute("href", "/acme/inbox?issue=n3");
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
    expect(screen.getByRole("link", { name: "Open one" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "Other one" })).not.toHaveAttribute("aria-current");
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

  it("is the whole list: no link out to another one, and a plain line when empty", () => {
    const { unmount } = renderSection([item({ id: "n1", issue_id: "issue-a", title: "Only one" })]);
    expect(screen.queryByRole("link", { name: "All notifications" })).toBeNull();
    expect(screen.getAllByRole("link").map((link) => link.getAttribute("href"))).toEqual(["/acme/issues/issue-a"]);
    unmount();

    renderSection([]);
    expect(rows()).toHaveLength(0);
    expect(screen.getByText("No notifications")).not.toHaveAttribute("href");
    expect(screen.queryByRole("link")).toBeNull();
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
    expect(screen.getByRole("link", { name: "Autopilot paused" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "Other one" })).not.toHaveAttribute("aria-current");
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
