import { useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { EMPTY_ISSUE_VIEW_PREFERENCE } from "@multica/core/api/schemas";
import { useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { issueViewPreferenceOptions } from "@multica/core/issue-views/preferences";
import { issueViewEverywhereOptions } from "@multica/core/issue-views/queries";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { issueDetailOptions, issueKeys } from "@multica/core/issues/queries";
import { issueUnreadOptions } from "@multica/core/issues/unread";
import { NO_PROJECT_SIDEBAR_SOURCE } from "@multica/core/issues/sidebar-list";
import { SIDEBAR_PROJECTS_SCOPE } from "@multica/core/issues/sidebar-project-prefs";
import { useSidebarKeptStore } from "@multica/core/issues/stores/sidebar-kept-store";
import {
  DEFAULT_SIDEBAR_PROJECT_SETTINGS,
  useSidebarTreeStore,
} from "@multica/core/issues/stores/sidebar-tree-store";
import { spaceForPath } from "@multica/core/paths";
import { pinListOptions } from "@multica/core/pins/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import type {
  InboxItem,
  Issue,
  IssueTableRowsRequest,
  IssueTableRowsResponse,
  PinnedItem,
  Project,
} from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { IssueInboxActions } from "../../issues/actions/issue-inbox-actions";
import { KeptRowsRelease, WorkPanel } from "./work-panel";

const apiMock = vi.hoisted(() => ({ listIssueTableRows: vi.fn(), getIssue: vi.fn() }));
const actions = vi.hoisted(() => ({
  sidebar: {
    items: [
      { id: "builtin:my_work", kind: "builtin", key: "my_work" },
      { id: "builtin:all_issues", kind: "builtin", key: "all_issues" },
    ],
    reorder: vi.fn(),
    unstar: vi.fn(),
  },
  canManage: () => true,
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
vi.mock("../../issue-views/use-view-actions", () => ({ useIssueViewActions: () => actions }));
vi.mock("../../issue-views/delete-view-dialog", () => ({ DeleteViewDialog: () => null }));
vi.mock("../../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: { children: ReactNode }) => <>{children}</>,
  PointerSensor: vi.fn(),
  closestCenter: vi.fn(),
  useSensor: vi.fn(),
  useSensors: vi.fn(),
}));
vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: ReactNode }) => <>{children}</>,
  useSortable: () => ({ listeners: {}, setNodeRef: vi.fn(), setActivatorNodeRef: vi.fn() }),
  verticalListSortingStrategy: vi.fn(),
  arrayMove: vi.fn(),
}));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("acme"),
    useCurrentWorkspace: () => ({ id: "ws-1" }),
  };
});
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/auth", async (importOriginal) => {
  const state = { user: { id: "u1" } };
  return {
    ...(await importOriginal<typeof import("@multica/core/auth")>()),
    useAuthStore: Object.assign((selector: (s: typeof state) => unknown) => selector(state), {
      getState: () => state,
    }),
  };
});
vi.mock("@multica/core/issue-statuses/hooks", async () => {
  const { buildIssueStatusCatalog } = await import("@multica/core/issue-statuses/queries");
  const catalog = buildIssueStatusCatalog([
    {
      id: "s1",
      workspace_id: "ws-1",
      key: "todo",
      name: "Todo",
      description: "",
      category: "unstarted",
      color: "",
      is_system: true,
      position: 1,
      archived_at: null,
      created_at: "",
      updated_at: "",
    },
  ]);
  return { useIssueStatuses: () => catalog };
});

const ISSUE = {
  id: "0a3f0a8e-6a52-4c1e-9f5e-1b2c3d4e5f60",
  identifier: "MUL-1",
  title: "Fix the sidebar",
  status: "todo",
} as Issue;
// An Inbox row's link also reads out who did what, after the title.
const INBOX_ROW_NAME = new RegExp(`^${ISSUE.title}`);
const OTHER = {
  id: "7b9e2c41-0d3a-4f6b-8e7c-2a1b0c9d8e7f",
  identifier: "MUL-2",
  title: "Polish the header",
  status: "todo",
} as Issue;
const PIN: PinnedItem = {
  id: "pin-1",
  workspace_id: "ws-1",
  user_id: "u1",
  item_type: "issue",
  item_id: ISSUE.id,
  position: 0,
  created_at: "",
};

const PROJECT = {
  id: "5d4c3b2a-1f0e-4d9c-8b7a-6f5e4d3c2b1a",
  title: "Launch",
  status: "in_progress",
  lead_type: null,
  lead_id: null,
  issue_count: 0,
  done_count: 0,
  created_at: "",
  updated_at: "",
} as unknown as Project;

function renderPanel(
  initialPath: string,
  pins: PinnedItem[] = [],
  projects: Project[] = [],
  unreadIssueIds: string[] = [],
  inbox: InboxItem[] = [],
  /** As on web, a push lands only once the next page has loaded ("Finish loading"). */
  deferPush = false,
  /** As on desktop: links can open tabs, and an issue page shows its header. */
  desktop = false,
) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(inboxListOptions("ws-1").queryKey, inbox);
  qc.setQueryData(projectListOptions("ws-1").queryKey, { projects, total: projects.length });
  qc.setQueryData(pinListOptions("ws-1", "u1").queryKey, pins);
  qc.setQueryData(issueViewEverywhereOptions("ws-1").queryKey, []);
  qc.setQueryData(issueViewPreferenceOptions("ws-1", SIDEBAR_PROJECTS_SCOPE).queryKey, EMPTY_ISSUE_VIEW_PREFERENCE);
  qc.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, []);
  qc.setQueryData(
    issueUnreadOptions("ws-1").queryKey,
    unreadIssueIds.map((issue_id) => ({
      issue_id,
      outcome: "completed" as const,
      agent_id: null,
      task_id: null,
      comment_id: null,
      created_at: "2026-01-01T00:00:00Z",
    })),
  );

  function Harness() {
    const [pathname, setPathname] = useState(initialPath);
    // A collapsed sidebar mounts a second panel as its hover peek.
    const [peek, setPeek] = useState(false);
    // A compact sheet unmounts the panel whenever it closes.
    const [sheetOpen, setSheetOpen] = useState(true);
    const [loading, setLoading] = useState<string | null>(null);
    const [newTab, setNewTab] = useState<string | null>(null);
    const openTab = (path: string) => setPathname(new URL(path, "http://x").pathname);
    const adapter: NavigationAdapter = {
      openInNewTab: desktop
        ? (path, _title, opts) => {
            setNewTab(path);
            if (opts?.activate) openTab(path);
          }
        : undefined,
      push: deferPush ? setLoading : setPathname,
      replace: setPathname,
      back: vi.fn(),
      pathname,
      searchParams: new URLSearchParams(),
      hash: "",
      getShareableUrl: (path) => path,
    };
    return (
      <NavigationProvider value={adapter}>
        <KeptRowsRelease />
        {/* Each desktop tab mounts its own page. */}
        {desktop && pathname.startsWith("/acme/issues/") && (
          <div data-testid="issue-header">
            <IssueInboxActions key={pathname} issueId={pathname.split("/").at(-1)!} onMarkDone={() => {}} />
          </div>
        )}
        {sheetOpen && spaceForPath(pathname) === "work" && <WorkPanel />}
        <button type="button" onClick={() => newTab && openTab(newTab)}>
          Switch to new tab
        </button>
        <button type="button" onClick={() => setPathname("/acme/agents")}>
          Leave Work
        </button>
        <button type="button" onClick={() => setPathname(`/acme/issues/${ISSUE.id}`)}>
          Return to issue
        </button>
        <button type="button" onClick={() => loading && setPathname(new URL(loading, "http://x").pathname)}>
          Finish loading
        </button>
        <button type="button" onClick={() => setSheetOpen((open) => !open)}>
          Toggle sheet
        </button>
        <button type="button" onClick={() => setPeek(true)}>
          Open peek
        </button>
        {peek && (
          <div data-testid="peek">
            <WorkPanel />
          </div>
        )}
      </NavigationProvider>
    );
  }

  renderWithI18n(
    <QueryClientProvider client={qc}>
      <Harness />
    </QueryClientProvider>,
  );
  return qc;
}

const issueRow = () => screen.getByRole("link", { name: ISSUE.title });
const allIssues = () => screen.getByRole("link", { name: "All issues" });
const noProjectLocated = () => !!screen.getByRole("button", { name: "No project" }).closest("[data-located]");

beforeEach(() => {
  useSidebarTreeStore.setState({
    expanded: ["builtin:my_work"],
    viewsCollapsed: false,
    pinnedCollapsed: false,
    projectsCollapsed: false,
    projectSettings: DEFAULT_SIDEBAR_PROJECT_SETTINGS,
  });
  useActiveIssueViewStore.setState({ active: {} });
  useSidebarKeptStore.setState({ held: {} });
  useInboxSelectionStore.setState({
    sidebarIssueByWorkspace: {},
    sidebarTabIssuesByWorkspace: {},
    heldRows: {},
    leftRowKeyByWorkspace: {},
  });
  apiMock.listIssueTableRows.mockResolvedValue({
    rows: [ISSUE, OTHER].map((issue) => ({ issue })),
    total: 2,
    next_cursor: null,
  } as unknown as IssueTableRowsResponse);
  // The issue page never finishes loading, so the panel cannot lean on it.
  apiMock.getIssue.mockReturnValue(new Promise<Issue>(() => {}));
});

function inboxItem(issue: Issue): InboxItem {
  return {
    id: `inbox-${issue.id}`,
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "u1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: issue.id,
    title: issue.title,
    body: null,
    issue_status: issue.status,
    issue_priority: "none",
    read: false,
    archived: false,
    created_at: "2026-11-10T08:00:00Z",
    details: null,
  };
}

describe("WorkPanel sections", () => {
  const sections = () => Array.from(document.querySelectorAll<HTMLElement>("[data-mc='panel-section']"));
  const labels = () => sections().map((section) => section.querySelector("h3")?.textContent);

  it("orders Pinned, Inbox, Views, Projects", () => {
    renderPanel("/acme/my-issues", [PIN], [PROJECT]);
    expect(labels()).toEqual(["Pinned", "Inbox", "Views", "Projects"]);
  });

  // The panel body drops the top margin of a section that is its first child.
  it("starts the panel with its first section, with or without pins", () => {
    renderPanel("/acme/my-issues", [PIN], [PROJECT]);
    expect(sections()[0]!.parentElement!.firstElementChild).toBe(sections()[0]);
    cleanup();

    renderPanel("/acme/my-issues", [], [PROJECT]);
    expect(labels()[0]).toBe("Inbox");
    expect(sections()[0]!.parentElement!.firstElementChild).toBe(sections()[0]);
  });
});

describe("WorkPanel Inbox highlight", () => {
  const inInbox = () => document.querySelector('[data-sidebar-list="inbox"]') as HTMLElement;
  const inMyIssues = () => document.querySelector('[data-sidebar-list="builtin:my_work"]') as HTMLElement;

  it("lights an issue opened from the Inbox there only", async () => {
    renderPanel("/acme/my-issues", [], [], [], [inboxItem(ISSUE)]);
    await within(await waitFor(inMyIssues)).findByRole("link", { name: ISSUE.title });
    fireEvent.click(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME }));

    expect(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME })).toHaveAttribute("aria-current", "page");
    expect(within(inMyIssues()).getByRole("link", { name: ISSUE.title })).not.toHaveAttribute("aria-current");
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it("lights an issue opened from the Inbox at its identifier URL", async () => {
    const qc = renderPanel("/acme/my-issues", [], [], [], [{ ...inboxItem(ISSUE), issue_identifier: ISSUE.identifier }]);
    await within(await waitFor(inMyIssues)).findByRole("link", { name: ISSUE.title });
    const row = within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME });
    expect(row).toHaveAttribute("href", `/acme/issues/${ISSUE.identifier}`);
    fireEvent.click(row);
    // The issue page loads it by its identifier.
    act(() => qc.setQueryData(issueDetailOptions("ws-1", ISSUE.identifier).queryKey, ISSUE));

    await waitFor(() =>
      expect(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME })).toHaveAttribute("aria-current", "page"),
    );
    expect(within(inMyIssues()).getByRole("link", { name: ISSUE.title })).not.toHaveAttribute("aria-current");
  });

  it("moves the light to the list the issue is clicked in next", async () => {
    renderPanel("/acme/my-issues", [], [], [], [inboxItem(ISSUE)]);
    await within(await waitFor(inMyIssues)).findByRole("link", { name: ISSUE.title });
    fireEvent.click(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME }));
    fireEvent.click(within(inMyIssues()).getByRole("link", { name: ISSUE.title }));

    expect(within(inMyIssues()).getByRole("link", { name: ISSUE.title })).toHaveAttribute("aria-current", "page");
    expect(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME })).not.toHaveAttribute("aria-current");
  });

  it("lights the issue the issue header moved on to from the Inbox", async () => {
    useInboxSelectionStore.setState({ sidebarIssueByWorkspace: { "ws-1": ISSUE.id } });
    renderPanel(`/acme/issues/${ISSUE.id}`, [], [], [], [inboxItem(ISSUE)]);

    await waitFor(() =>
      expect(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME })).toHaveAttribute("aria-current", "page"),
    );
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it("keeps the open row lit in place once its notification is archived some other way", async () => {
    const qc = renderPanel("/acme/my-issues", [], [], [], [inboxItem(ISSUE)]);
    await within(await waitFor(inMyIssues)).findByRole("link", { name: ISSUE.title });
    fireEvent.click(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME }));
    act(() => {
      qc.setQueryData(inboxListOptions("ws-1").queryKey, []);
    });

    const row = await within(inInbox()).findByRole("link", { name: /Leaves the Inbox when you move on/ });
    expect(row).toHaveAttribute("aria-current", "page");
    expect(within(inMyIssues()).getByRole("link", { name: ISSUE.title })).not.toHaveAttribute("aria-current");
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it("lights nothing else once an Inbox action took the open row with nothing left to open", async () => {
    const qc = renderPanel("/acme/my-issues", [], [], [], [inboxItem(ISSUE)]);
    await within(await waitFor(inMyIssues)).findByRole("link", { name: ISSUE.title });
    fireEvent.click(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME }));
    act(() => {
      useInboxSelectionStore.getState().leaveRow("ws-1", ISSUE.id);
      qc.setQueryData(inboxListOptions("ws-1").queryKey, []);
    });

    await waitFor(() => expect(inInbox()).toBeNull());
    expect(within(inMyIssues()).getByRole("link", { name: ISSUE.title })).not.toHaveAttribute("aria-current");
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it("lights an issue opened from a row's hover card in the Inbox", async () => {
    renderPanel("/acme/my-issues", [], [], [], [inboxItem(ISSUE)]);
    await within(await waitFor(inMyIssues)).findByRole("link", { name: ISSUE.title });
    fireEvent.mouseEnter(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME }).parentElement!);
    fireEvent.click(await screen.findByRole("link", { name: "Open" }, { timeout: 2000 }));

    expect(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME })).toHaveAttribute("aria-current", "page");
    expect(within(inMyIssues()).getByRole("link", { name: ISSUE.title })).not.toHaveAttribute("aria-current");
  });

  it("keeps the open row lit while newer notifications push it past the limit", async () => {
    const others = Array.from({ length: 5 }, (_, i) => ({
      ...OTHER,
      id: `00000000-0000-4000-8000-00000000000${i}`,
      title: `Other ${i}`,
    }));
    const notified = (issue: Issue, minute: number) => ({
      ...inboxItem(issue),
      created_at: `2026-11-10T08:0${minute}:00Z`,
    });
    const older = [notified(ISSUE, 0), ...others.slice(0, 4).map((issue, i) => notified(issue, i + 1))];
    const qc = renderPanel("/acme/my-issues", [], [], [], older);
    await within(await waitFor(inMyIssues)).findByRole("link", { name: ISSUE.title });
    fireEvent.click(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME }));
    act(() => {
      qc.setQueryData(inboxListOptions("ws-1").queryKey, [...older, notified(others[4]!, 6)]);
    });

    await within(inInbox()).findByRole("link", { name: /^Other 4/ });
    expect(within(inInbox()).getByRole("link", { name: INBOX_ROW_NAME })).toHaveAttribute("aria-current", "page");
    expect(within(inMyIssues()).getByRole("link", { name: ISSUE.title })).not.toHaveAttribute("aria-current");
  });
});

describe("WorkPanel Inbox highlight while the next page loads", () => {
  const inInbox = () => document.querySelector('[data-sidebar-list="inbox"]') as HTMLElement;
  const inMyIssues = () => document.querySelector('[data-sidebar-list="builtin:my_work"]') as HTMLElement;
  const inboxRow = (issue: Issue) => within(inInbox()).getByRole("link", { name: new RegExp(`^${issue.title}`) });
  const finishLoading = () => fireEvent.click(screen.getByRole("button", { name: "Finish loading" }));

  // Under the collapsed Projects section, an issue without a project that no
  // other row marks lights All issues.
  async function openFromInbox(expanded: string[]) {
    useSidebarTreeStore.setState({ expanded, projectsCollapsed: true });
    const qc = renderPanel("/acme/my-issues", [], [], [], [inboxItem(ISSUE), inboxItem(OTHER)], true);
    act(() => {
      qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, ISSUE);
    });
    fireEvent.click(inboxRow(ISSUE));
    finishLoading();
    expect(inboxRow(ISSUE)).toHaveAttribute("aria-current", "page");
    return qc;
  }

  it("keeps the open issue lit in the Inbox until the next notification's page arrives", async () => {
    await openFromInbox([]);
    fireEvent.click(inboxRow(OTHER));

    expect(inboxRow(ISSUE)).toHaveAttribute("aria-current", "page");
    expect(allIssues()).not.toHaveAttribute("aria-current");

    finishLoading();
    expect(inboxRow(OTHER)).toHaveAttribute("aria-current", "page");
    expect(inboxRow(ISSUE)).not.toHaveAttribute("aria-current");
  });

  it("keeps the open issue lit in the Inbox until an issue clicked in a view arrives", async () => {
    await openFromInbox(["builtin:my_work"]);
    fireEvent.click(await within(await waitFor(inMyIssues)).findByRole("link", { name: OTHER.title }));

    expect(inboxRow(ISSUE)).toHaveAttribute("aria-current", "page");
    expect(within(inMyIssues()).getByRole("link", { name: ISSUE.title })).not.toHaveAttribute("aria-current");
    expect(allIssues()).not.toHaveAttribute("aria-current");

    finishLoading();
    expect(within(inMyIssues()).getByRole("link", { name: OTHER.title })).toHaveAttribute("aria-current", "page");
    expect(inboxRow(ISSUE)).not.toHaveAttribute("aria-current");
  });

  it.each([{ metaKey: true }, { ctrlKey: true }, { metaKey: true, shiftKey: true }])(
    "keeps the open issue's records when another notification opens in a new tab (%o)",
    async (modifiers) => {
      await openFromInbox([]);
      fireEvent.click(inboxRow(OTHER), modifiers);

      expect(inboxRow(ISSUE)).toHaveAttribute("aria-current", "page");
      expect(useInboxSelectionStore.getState().sidebarIssueByWorkspace["ws-1"]).toBe(ISSUE.id);
      // A browser tab has a store of its own.
      expect(useInboxSelectionStore.getState().sidebarTabIssuesByWorkspace["ws-1"]).toBeUndefined();

      // Nothing was left pending for a later page to settle.
      fireEvent.click(screen.getByRole("button", { name: "Leave Work" }));
      fireEvent.click(screen.getByRole("button", { name: "Return to issue" }));
      expect(inboxRow(ISSUE)).toHaveAttribute("aria-current", "page");
    },
  );

  it("lights nothing else while the issue header moves on from an archived notification", async () => {
    const qc = await openFromInbox([]);
    act(() => {
      qc.setQueryData(inboxListOptions("ws-1").queryKey, [{ ...inboxItem(ISSUE), archived: true }, inboxItem(OTHER)]);
      useInboxSelectionStore.getState().leaveRow("ws-1", ISSUE.id);
      useInboxSelectionStore.getState().rememberSidebarIssue("ws-1", OTHER.id);
    });

    await waitFor(() => expect(within(inInbox()).queryByRole("link", { name: INBOX_ROW_NAME })).toBeNull());
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });
});

describe("WorkPanel Inbox across desktop tabs", () => {
  const inInbox = () => document.querySelector('[data-sidebar-list="inbox"]') as HTMLElement;
  const inMyIssues = () => document.querySelector('[data-sidebar-list="builtin:my_work"]') as HTMLElement;
  const inboxRow = (issue: Issue) => within(inInbox()).getByRole("link", { name: new RegExp(`^${issue.title}`) });
  const offersInboxActions = () => !!within(screen.getByTestId("issue-header")).queryByRole("button", { name: "Archive" });
  const switchToNewTab = () => fireEvent.click(screen.getByRole("button", { name: "Switch to new tab" }));
  const switchBack = () => fireEvent.click(screen.getByRole("button", { name: "Return to issue" }));

  async function openFromInbox() {
    useSidebarTreeStore.setState({ expanded: ["builtin:my_work"], projectsCollapsed: true });
    const qc = renderPanel("/acme/my-issues", [], [], [], [inboxItem(ISSUE), inboxItem(OTHER)], false, true);
    act(() => {
      qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, ISSUE);
      qc.setQueryData(issueDetailOptions("ws-1", OTHER.id).queryKey, OTHER);
    });
    fireEvent.click(inboxRow(ISSUE));
    expect(offersInboxActions()).toBe(true);
  }

  it.each([
    ["Cmd-click", (link: HTMLElement) => fireEvent.click(link, { metaKey: true })],
    ["Ctrl-click", (link: HTMLElement) => fireEvent.click(link, { ctrlKey: true })],
    [
      "middle click",
      (link: HTMLElement) => fireEvent(link, new MouseEvent("auxclick", { bubbles: true, cancelable: true, button: 1 })),
    ],
  ])("keeps both issues opened from the Inbox after a %s opens a background tab", async (_, open) => {
    await openFromInbox();
    open(inboxRow(OTHER));

    expect(offersInboxActions()).toBe(true);
    expect(inboxRow(ISSUE)).toHaveAttribute("aria-current", "page");

    switchToNewTab();
    expect(offersInboxActions()).toBe(true);
    expect(inboxRow(OTHER)).toHaveAttribute("aria-current", "page");

    switchBack();
    expect(offersInboxActions()).toBe(true);
    expect(inboxRow(ISSUE)).toHaveAttribute("aria-current", "page");
  });

  it("keeps both issues opened from the Inbox after a Cmd+Shift-click opens a foreground tab", async () => {
    await openFromInbox();
    fireEvent.click(inboxRow(OTHER), { metaKey: true, shiftKey: true });

    expect(offersInboxActions()).toBe(true);
    expect(inboxRow(OTHER)).toHaveAttribute("aria-current", "page");

    switchBack();
    expect(offersInboxActions()).toBe(true);
    expect(inboxRow(ISSUE)).toHaveAttribute("aria-current", "page");
  });

  it("stops counting a tab's issue as opened from the Inbox once it is opened from another list", async () => {
    await openFromInbox();
    fireEvent.click(inboxRow(OTHER), { metaKey: true });
    switchToNewTab();
    fireEvent.click(await within(inMyIssues()).findByRole("link", { name: OTHER.title }));

    expect(offersInboxActions()).toBe(false);
    expect(within(inMyIssues()).getByRole("link", { name: OTHER.title })).toHaveAttribute("aria-current", "page");
  });
});

describe("WorkPanel issue highlight", () => {
  it("lights a clicked tree issue at once, never All issues first", async () => {
    renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));

    expect(issueRow()).toHaveAttribute("aria-current", "page");
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it.each(["MUL-1", "mul-1"])("finds an issue opened by its identifier (%s) in the tree", async (segment) => {
    renderPanel(`/acme/issues/${segment}`);

    expect(await screen.findByRole("link", { name: ISSUE.title })).toHaveAttribute("aria-current", "page");
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it("keeps a pinned issue lit on the tree row it was clicked from", async () => {
    renderPanel("/acme/my-issues", [PIN]);
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));

    expect(issueRow()).toHaveAttribute("aria-current", "page");
  });

  it("marks No project for an issue without a project no expanded row lists", async () => {
    const qc = renderPanel("/acme/issues/9c8d7e6f-5a4b-4c3d-8e2f-1a0b9c8d7e6f");
    act(() => {
      qc.setQueryData(issueDetailOptions("ws-1", "9c8d7e6f-5a4b-4c3d-8e2f-1a0b9c8d7e6f").queryKey, {
        ...OTHER,
        id: "9c8d7e6f-5a4b-4c3d-8e2f-1a0b9c8d7e6f",
      });
    });

    await screen.findByRole("link", { name: ISSUE.title });
    await waitFor(() => expect(noProjectLocated()).toBe(true));
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it("keeps an issue its list drops lit in place until another issue opens", async () => {
    const qc = renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));
    qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, { ...ISSUE, title: "Fixed the sidebar", status: "done" });
    apiMock.listIssueTableRows.mockResolvedValue({ rows: [{ issue: OTHER }], total: 1, next_cursor: null });
    await act(() => qc.invalidateQueries({ queryKey: issueKeys.sidebarAll("ws-1") }));

    expect(await screen.findByRole("link", { name: "Fixed the sidebar" })).toHaveAttribute("aria-current", "page");
    expect(allIssues()).not.toHaveAttribute("aria-current");

    fireEvent.click(screen.getByRole("link", { name: OTHER.title }));
    expect(screen.queryByRole("link", { name: "Fixed the sidebar" })).toBeNull();
  });

  it("drops a kept issue once it is deleted", async () => {
    const qc = renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));
    qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, ISSUE);
    apiMock.listIssueTableRows.mockResolvedValue({ rows: [{ issue: OTHER }], total: 1, next_cursor: null });
    await act(async () => {
      qc.removeQueries({ queryKey: issueKeys.detail("ws-1", ISSUE.id) });
      await qc.invalidateQueries({ queryKey: issueKeys.sidebarAll("ws-1") });
    });

    await waitFor(() => expect(screen.queryByRole("link", { name: ISSUE.title })).toBeNull());
  });

  it("keeps the dropped issue lit in a peek opened afterwards", async () => {
    const qc = renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));
    qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, { ...ISSUE, title: "Fixed the sidebar", status: "done" });
    apiMock.listIssueTableRows.mockResolvedValue({ rows: [{ issue: OTHER }], total: 1, next_cursor: null });
    await act(() => qc.invalidateQueries({ queryKey: issueKeys.sidebarAll("ws-1") }));
    await screen.findByRole("link", { name: "Fixed the sidebar" });
    fireEvent.click(screen.getByRole("button", { name: "Open peek" }));

    const peek = within(screen.getByTestId("peek"));
    expect(peek.getByRole("link", { name: "Fixed the sidebar" })).toHaveAttribute("aria-current", "page");
    expect(peek.getByRole("link", { name: "All issues" })).not.toHaveAttribute("aria-current");
  });

  it("keeps the dropped issue while the panel remounts on the same page", async () => {
    const qc = renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));
    qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, { ...ISSUE, title: "Fixed the sidebar", status: "done" });
    apiMock.listIssueTableRows.mockResolvedValue({ rows: [{ issue: OTHER }], total: 1, next_cursor: null });
    await act(() => qc.invalidateQueries({ queryKey: issueKeys.sidebarAll("ws-1") }));
    await screen.findByRole("link", { name: "Fixed the sidebar" });

    fireEvent.click(screen.getByRole("button", { name: "Toggle sheet" }));
    fireEvent.click(screen.getByRole("button", { name: "Toggle sheet" }));

    expect(await screen.findByRole("link", { name: "Fixed the sidebar" })).toHaveAttribute("aria-current", "page");
  });

  it("ends the keep after leaving Work and coming back to the issue", async () => {
    const qc = renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));
    qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, { ...ISSUE, title: "Fixed the sidebar", status: "done" });
    apiMock.listIssueTableRows.mockResolvedValue({ rows: [{ issue: OTHER }], total: 1, next_cursor: null });
    await act(() => qc.invalidateQueries({ queryKey: issueKeys.sidebarAll("ws-1") }));
    await screen.findByRole("link", { name: "Fixed the sidebar" });

    fireEvent.click(screen.getByRole("button", { name: "Leave Work" }));
    fireEvent.click(screen.getByRole("button", { name: "Return to issue" }));

    expect(await screen.findByRole("link", { name: OTHER.title })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Fixed the sidebar" })).toBeNull();
    expect(noProjectLocated()).toBe(true);
  });

  it("keeps the open project in the tree after it leaves the projects filter", async () => {
    const qc = renderPanel(`/acme/projects/${PROJECT.id}`, [], [PROJECT]);
    expect(screen.getByRole("link", { name: PROJECT.title })).toHaveAttribute("aria-current", "page");

    act(() => {
      qc.setQueryData(projectListOptions("ws-1").queryKey, {
        projects: [{ ...PROJECT, title: "Launched", status: "completed" }],
        total: 1,
      });
    });
    expect(await screen.findByRole("link", { name: "Launched" })).toHaveAttribute("aria-current", "page");

    fireEvent.click(allIssues());
    expect(screen.queryByRole("link", { name: "Launched" })).toBeNull();
  });
});

describe("WorkPanel and the open issue's project", () => {
  const PROJECT_SOURCE = `project:${PROJECT.id}`;
  const IN_PROJECT = { ...ISSUE, project_id: PROJECT.id } as Issue;
  const projectLocated = () => !!screen.getByRole("link", { name: PROJECT.title }).closest("[data-located]");
  const litIssueRows = () =>
    screen.queryAllByRole("link", { name: ISSUE.title }).filter((link) => link.getAttribute("aria-current") === "page");
  const openInProject = (qc: QueryClient) =>
    act(() => {
      qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, IN_PROJECT);
    });
  // Query observers hear of cache writes on a timer.
  const settle = () => act(() => new Promise((resolve) => setTimeout(resolve, 10)));

  beforeEach(() => {
    useSidebarTreeStore.setState({ locate: null });
  });

  it("leaves the project closed when its issue is clicked in a view", async () => {
    const qc = renderPanel("/acme/my-issues", [], [PROJECT]);
    await openInProject(qc);
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));

    expect(useSidebarTreeStore.getState().expanded).not.toContain(PROJECT_SOURCE);
    expect(litIssueRows()).toHaveLength(1);
    expect(projectLocated()).toBe(false);
  });

  it("lights the issue only in the list it was clicked in", async () => {
    useSidebarTreeStore.setState({ expanded: ["builtin:my_work", PROJECT_SOURCE] });
    const qc = renderPanel("/acme/my-issues", [], [PROJECT]);
    await openInProject(qc);
    await waitFor(() => expect(screen.getAllByRole("link", { name: ISSUE.title })).toHaveLength(2));
    const inMyIssues = document.querySelector('[data-sidebar-list="builtin:my_work"]') as HTMLElement;
    fireEvent.click(within(inMyIssues).getByRole("link", { name: ISSUE.title }));

    expect(litIssueRows()).toEqual([within(inMyIssues).getByRole("link", { name: ISSUE.title })]);
  });

  it("marks the closed project of an issue no list shows, instead of All issues", async () => {
    useSidebarTreeStore.setState({ expanded: [] });
    const qc = renderPanel(`/acme/issues/${ISSUE.id}`, [], [PROJECT]);
    await openInProject(qc);

    await waitFor(() => expect(projectLocated()).toBe(true));
    expect(allIssues()).not.toHaveAttribute("aria-current");
    expect(useSidebarTreeStore.getState().expanded).toEqual([]);
  });

  it("marks No project for an issue without a project that no list shows", async () => {
    useSidebarTreeStore.setState({ expanded: [] });
    const qc = renderPanel(`/acme/issues/${ISSUE.id}`, [], [PROJECT]);
    act(() => {
      qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, ISSUE);
    });

    await waitFor(() => expect(noProjectLocated()).toBe(true));
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it("lights the issue in the expanded No project row", async () => {
    useSidebarTreeStore.setState({ expanded: [NO_PROJECT_SIDEBAR_SOURCE] });
    const qc = renderPanel(`/acme/issues/${ISSUE.id}`, [], [PROJECT]);
    act(() => {
      qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, ISSUE);
    });

    const list = await waitFor(
      () => document.querySelector(`[data-sidebar-list="${NO_PROJECT_SIDEBAR_SOURCE}"]`) as HTMLElement,
    );
    await waitFor(() =>
      expect(within(list).getByRole("link", { name: ISSUE.title })).toHaveAttribute("aria-current", "page"),
    );
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it("lights All issues for an issue without a project once No project is hidden", async () => {
    useSidebarTreeStore.setState({
      expanded: [],
      projectSettings: { ...DEFAULT_SIDEBAR_PROJECT_SETTINGS, showNoProject: false },
    });
    const qc = renderPanel(`/acme/issues/${ISSUE.id}`, [], [PROJECT]);
    act(() => {
      qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, ISSUE);
    });

    await waitFor(() => expect(allIssues()).toHaveAttribute("aria-current", "page"));
    expect(screen.queryByRole("button", { name: "No project" })).toBeNull();
  });

  it("does not light All issues for an issue under the collapsed Projects section", async () => {
    useSidebarTreeStore.setState({ expanded: [], projectsCollapsed: true });
    const qc = renderPanel(`/acme/issues/${ISSUE.id}`, [], [PROJECT]);
    await openInProject(qc);
    await settle();

    expect(allIssues()).not.toHaveAttribute("aria-current");
    expect(document.querySelector("[data-located]")).toBeNull();
  });

  it("does not light All issues for an issue in a project the sidebar filters out", async () => {
    useSidebarTreeStore.setState({ expanded: [] });
    const done = { ...PROJECT, status: "completed" } as Project;
    const qc = renderPanel(`/acme/issues/${ISSUE.id}`, [], [done]);
    await openInProject(qc);
    await settle();

    expect(screen.queryByRole("link", { name: PROJECT.title })).toBeNull();
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it("lists an issue lit in a view under its project once the project is expanded", async () => {
    // The open issue is on the project's second page.
    const builtinRows = await apiMock.listIssueTableRows();
    apiMock.listIssueTableRows.mockImplementation(async ({ query }: IssueTableRowsRequest) =>
      query.scope.kind === "project" ? { rows: [{ issue: OTHER }], total: 2, next_cursor: null } : builtinRows,
    );
    const qc = renderPanel("/acme/my-issues", [], [PROJECT]);
    await openInProject(qc);
    const inMyIssues = await waitFor(() => {
      const element = document.querySelector('[data-sidebar-list="builtin:my_work"]') as HTMLElement;
      expect(within(element).getByRole("link", { name: ISSUE.title })).toBeInTheDocument();
      return element;
    });
    fireEvent.click(within(inMyIssues).getByRole("link", { name: ISSUE.title }));

    act(() => {
      useSidebarTreeStore.getState().setExpanded(PROJECT_SOURCE, true);
    });
    const list = await waitFor(() => {
      const element = document.querySelector(`[data-sidebar-list="${PROJECT_SOURCE}"]`) as HTMLElement;
      expect(within(element).getAllByRole("link").map((link) => link.textContent)).toEqual([OTHER.title, ISSUE.title]);
      return element;
    });
    expect(within(list).getByRole("link", { name: ISSUE.title })).not.toHaveAttribute("aria-current");
    expect(litIssueRows()).toEqual([within(inMyIssues).getByRole("link", { name: ISSUE.title })]);
  });

  it("lists the open issue last under its expanded project when no page has it", async () => {
    useSidebarTreeStore.setState({ expanded: [PROJECT_SOURCE] });
    apiMock.listIssueTableRows.mockResolvedValue({ rows: [{ issue: OTHER }], total: 1, next_cursor: null });
    const qc = renderPanel(`/acme/issues/${ISSUE.id}`, [], [PROJECT]);
    await openInProject(qc);

    const list = await waitFor(() => {
      const element = document.querySelector(`[data-sidebar-list="${PROJECT_SOURCE}"]`) as HTMLElement;
      expect(within(element).getByRole("link", { name: OTHER.title })).toBeInTheDocument();
      return element;
    });
    const links = within(list).getAllByRole("link");
    expect(links.map((link) => link.textContent)).toEqual([OTHER.title, ISSUE.title]);
    expect(links[1]).toHaveAttribute("aria-current", "page");
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it("opens the project and lights the issue there when asked to show it", async () => {
    const qc = renderPanel(`/acme/issues/${ISSUE.id}`, [], [PROJECT]);
    await openInProject(qc);
    await screen.findByRole("link", { name: ISSUE.title });

    act(() => {
      useSidebarTreeStore.getState().requestLocate({ issueId: ISSUE.id, source: PROJECT_SOURCE });
    });

    expect(useSidebarTreeStore.getState().expanded).toContain(PROJECT_SOURCE);
    const list = await waitFor(() => {
      const element = document.querySelector(`[data-sidebar-list="${PROJECT_SOURCE}"]`) as HTMLElement;
      expect(within(element).getByRole("link", { name: ISSUE.title })).toHaveAttribute("aria-current", "page");
      return element;
    });
    expect(litIssueRows()).toEqual([within(list).getByRole("link", { name: ISSUE.title })]);
    await waitFor(() => expect(useSidebarTreeStore.getState().locate).toBeNull());
  });
});
