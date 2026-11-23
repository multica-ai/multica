import { act, fireEvent, render, screen, within } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import type { AgentTask, InboxItem } from "@multica/core/types";
import { useInboxFilterStore } from "@multica/core/inbox/filter-store";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { useCommandBarContextStore } from "@multica/core/command-bar";
import { InboxPage } from "./inbox-page";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));


// The page runs two queries — the active list and the archived one. They are
// told apart by the queryKey their options carry, so each test can stock the
// two lists independently.
const listData: { active: InboxItem[]; archived: InboxItem[]; lookup?: InboxItem[]; tasks: AgentTask[] } = {
  active: [],
  archived: [],
  tasks: [],
};

const NO_SNOOZED: never[] = [];
const queryCalls: Array<{ queryKey: readonly unknown[]; enabled?: boolean }> = [];
const lookupState = { isLoading: false, isError: false, refetch: vi.fn() };
// The active list as a read that failed before anything loaded, and is
// possibly being tried again.
const activeFailure = { failed: false, retrying: false, refetch: vi.fn() };
const queryClient = vi.hoisted(() => ({ setQueryData: vi.fn(), getQueryData: vi.fn() }));
vi.mock("@tanstack/react-query", () => ({
  queryOptions: (options: unknown) => options,
  useQueryClient: () => queryClient,
  useQuery: (options: { queryKey: readonly unknown[]; enabled?: boolean }) => {
    queryCalls.push(options);
    return ({
    data: options.queryKey.includes("archived")
      ? { items: listData.lookup ?? listData.archived, hasMore: false, nextCursor: null }
      : options.queryKey.includes("agent-task-snapshot") ? listData.tasks
      : options.queryKey.includes("working-agents") ? []
      : options.queryKey.includes("snoozed") ? NO_SNOOZED : listData.active,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    ...(options.queryKey.includes("lookup") ? lookupState : {}),
    ...(options.queryKey.at(-1) === "list" && activeFailure.failed
      ? activeFailure.retrying
        ? { data: undefined, isError: false, isLoading: true, isFetching: true, errorUpdateCount: 1, refetch: activeFailure.refetch }
        : { data: undefined, isError: true, isFetching: false, errorUpdateCount: 1, refetch: activeFailure.refetch }
      : {}),
  }); },
  useInfiniteQuery: (options: { queryKey: readonly unknown[]; enabled?: boolean }) => {
    queryCalls.push(options);
    return ({
    data: { pages: [{ items: listData.archived, hasMore: false, nextCursor: null }] },
    isLoading: false, isError: false, hasNextPage: false,
    isFetchingNextPage: false, isFetchNextPageError: false,
    fetchNextPage: vi.fn(), refetch: vi.fn(),
  }); },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/permissions", () => ({
  useIsGuest: () => false,
}));

vi.mock("@multica/core/paths", () => ({
  issueRouteRef: (issue: { id: string; identifier?: string | null }) => issue.identifier || issue.id,
  useWorkspacePaths: () => ({
    inbox: () => "/acme/inbox",
    issues: () => "/acme/issues",
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));

const modalState: { modal: string | null; open: ReturnType<typeof vi.fn> } = {
  modal: null,
  open: vi.fn(),
};
vi.mock("@multica/core/modals", () => ({
  useModalStore: { getState: () => modalState },
}));

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: { getState: () => ({ setDraft: vi.fn() }) },
}));

vi.mock("@multica/core/inbox/queries", () => ({
  inboxListOptions: () => ({ queryKey: ["inbox", "workspace-1", "list"] }),
  archivedInboxPagesOptions: () => ({ queryKey: ["inbox", "workspace-1", "archived", "pages"] }),
  archivedInboxLookupOptions: () => ({ queryKey: ["inbox", "workspace-1", "archived", "lookup"] }),
  snoozedInboxOptions: () => ({ queryKey: ["inbox", "workspace-1", "snoozed"] }),
  deduplicateInboxItems: (items: InboxItem[]) => items.filter((i) => !i.archived),
  deduplicateArchivedInboxItems: (items: InboxItem[]) => items.filter((i) => i.archived),
  useInboxUnreadCount: () => 2,
  findSnoozedInboxIssue: (rows: Array<{ issue_id: string }> | undefined, issueId: string | null) =>
    rows?.find((row) => row.issue_id === issueId),
}));

// Stable spies: the auto-mark-read effect keys on the mutate identity, so a
// fresh `vi.fn()` per render would make the effect's deps churn.
const markReadMutate = vi.fn();
const markUnreadMutate = vi.fn();
const archiveMutate = vi.fn();
const unarchiveMutate = vi.fn();
const retrySourceContextMutateAsync = vi.fn();
const showAutopilotQuotaRecoveryPrompt = vi.hoisted(() => vi.fn());

vi.mock("../../modals/use-limit-recovery-prompt", () => ({
  useLimitRecoveryPrompt: () => showAutopilotQuotaRecoveryPrompt,
}));

vi.mock("@multica/core/inbox/mutations", () => {
  const mutation = () => ({ mutate: vi.fn(), mutateAsync: vi.fn(() => Promise.resolve()) });
  // The shared archive handlers await the request; settle it through the spy's callbacks.
  const settled = (spy: typeof archiveMutate) => (id: string) =>
    new Promise<void>((resolve, reject) => spy(id, { onSuccess: resolve, onError: reject }));
  return {
    useMarkInboxRead: () => ({ mutate: markReadMutate }),
    useMarkInboxUnread: () => ({ mutate: markUnreadMutate }),
    useArchiveInbox: () => ({ mutate: archiveMutate, mutateAsync: settled(archiveMutate) }),
    useUnarchiveInbox: () => ({ mutate: unarchiveMutate, mutateAsync: settled(unarchiveMutate) }),
    useSnoozeInbox: mutation,
    useUnsnoozeInbox: mutation,
    cachedInboxSnoozedUntil: () => null,
    useMarkAllInboxRead: mutation,
    useArchiveAllInbox: mutation,
    useArchiveAllReadInbox: mutation,
    useArchiveCompletedInbox: mutation,
    useUnsubscribeArchiveInbox: mutation,
    undoUnsubscribeArchiveInbox: vi.fn(),
    useRetrySourceContextQuickCreate: () => ({
      mutateAsync: retrySourceContextMutateAsync,
      isPending: false,
    }),
  };
});

// Render-time capture of the props IssueDetail receives, so tests can assert
// what the page threads into it (highlight replay token) without standing up
// the real detail.
const issueDetailProps = vi.hoisted(
  () => [] as Array<Record<string, unknown>>,
);
vi.mock("../../issues/components/issue-detail", () => ({
  IssueDetail: (props: Record<string, unknown>) => {
    issueDetailProps.push(props);
    return <div data-testid="issue-detail">{String(props.issueId)}</div>;
  },
  issueHighlightMementoKey: (issueId: string) => `highlight:${issueId}`,
}));

const replace = vi.fn();
const push = vi.fn();
let searchParams = new URLSearchParams();

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ searchParams, replace, push }),
  // Real hook: reports the detail-pane swap to the shell's progress bar.
  // Nothing here renders that bar, and the page reads nothing back from it.
  useReportNavigating: () => {},
}));

// Drive the layout from a viewport width so a test can name the device it
// cares about instead of a boolean, and the two breakpoints stay in one place.
// Phone-width by default: only there does the page keep its own list. From
// the compact breakpoint up it shows the detail of the notification picked in
// the Work panel, so desktop tests open one through the URL.
const PHONE = 390;
// iPhone Duo's inner screen: 669px wide held upright, 951px held sideways.
const FOLD_INNER_PORTRAIT = 669;
const FOLD_INNER_LANDSCAPE = 951;
const TABLET = 768;
const DESKTOP = 1440;
const layout = { width: PHONE };
vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => layout.width < 768,
  useIsCompact: () => layout.width < 768,
}));
vi.mock("./inbox-list", () => ({
  InboxList: ({
    items,
    view,
    onSelect,
    emptyLabel,
    emptyAction,
  }: {
    items: InboxItem[];
    view: string;
    onSelect: (item: InboxItem) => void;
    emptyLabel?: string;
    emptyAction?: React.ReactNode;
  }) => (
    <div data-testid="list" data-view={view}>
      {items.map((i) => (
        <button key={i.id} data-testid="row" onClick={() => onSelect(i)}>
          {i.id}
        </button>
      ))}
      {items.length === 0 && emptyLabel && <p>{emptyLabel}</p>}
      {items.length === 0 && emptyAction}
    </div>
  ),
}));
vi.mock("./inbox-filter-menu", () => ({
  InboxFilterMenu: () => <button type="button">Filter inbox</button>,
}));
vi.mock("./inbox-list-item", () => ({ useTimeAgo: () => vi.fn() }));
vi.mock("./inbox-filter-options", () => ({
  InboxFilterChipsBar: ({ filters }: { filters: { types: string[]; agent: string | null } }) => (
    <div data-testid="filter-chips" data-types={filters.types.join(",")} data-agent={filters.agent ?? ""} />
  ),
}));

// Capture the row actions the page hands the context menu, so the read/unread
// handlers can be driven without standing up Base UI's menu.
let rowActions: {
  onMarkRead: (id: string) => void;
  onMarkUnread: (id: string) => void;
  onAction: (id: string) => void;
} | null = null;
vi.mock("./inbox-context-menu", () => ({
  InboxContextMenuProvider: ({
    actions,
    children,
  }: {
    actions: NonNullable<typeof rowActions>;
    children: React.ReactNode;
  }) => {
    rowActions = actions;
    return children;
  },
}));
vi.mock("./inbox-detail-label", () => ({ useTypeLabels: () => ({}) }));
vi.mock("./autopilot-quota-notice", () => ({
  AutopilotQuotaNotice: ({
    onOpenRecovery,
  }: {
    onOpenRecovery: () => void;
  }) => (
    <button
      type="button"
      data-testid="autopilot-quota-recovery"
      onClick={onOpenRecovery}
    >
      Recover
    </button>
  ),
}));
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "Inbox" }), useLocale: () => "en" }));

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
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
    issue_priority: null,
    read: true,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    ...overrides,
  };
}

function reset() {
  useInboxSelectionStore.setState({
    lastResolvedKeyByWorkspace: {},
    heldRows: {},
    leftRowKeyByWorkspace: {},
    lastClearedByWorkspace: {},
  });
  listData.active = [];
  listData.archived = [];
  listData.lookup = undefined;
  listData.tasks = [];
  lookupState.isLoading = false;
  lookupState.isError = false;
  lookupState.refetch.mockClear();
  activeFailure.failed = false;
  activeFailure.retrying = false;
  activeFailure.refetch.mockClear();
  queryCalls.length = 0;
  searchParams = new URLSearchParams();
  replace.mockClear();
  markReadMutate.mockReset();
  markUnreadMutate.mockClear();
  archiveMutate.mockClear();
  unarchiveMutate.mockClear();
  retrySourceContextMutateAsync.mockReset();
  retrySourceContextMutateAsync.mockResolvedValue({});
  showAutopilotQuotaRecoveryPrompt.mockClear();
  modalState.modal = null;
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
  rowActions = null;
  issueDetailProps.length = 0;
  layout.width = PHONE;
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
}

function inboxCommands() {
  const entry = useCommandBarContextStore.getState().entries.find((e) => e.context.kind === "inbox");
  if (entry?.context.kind !== "inbox") throw new Error("the Inbox registered no command bar context");
  return entry.context;
}

describe("InboxPage", () => {
  it("chooses a fresh anchor after returning to the list and reopening the same issue", () => {
    reset();
    layout.width = PHONE;
    searchParams = new URLSearchParams("issue=issue-1");
    listData.active = [
      item({ id: "n1", read: false, created_at: "2026-11-15T08:00:00Z", details: { comment_id: "c1" } }),
      item({ id: "n2", read: false, created_at: "2026-11-15T08:02:00Z", details: { comment_id: "c2" } }),
    ];
    markReadMutate.mockImplementation(() => {
      listData.active = listData.active.map((current) => ({ ...current, read: true }));
    });
    const { rerender } = render(<InboxPage />);
    expect(issueDetailProps.at(-1)?.highlightCommentId).toBe("c1");
    rerender(<InboxPage />);
    const back = issueDetailProps.at(-1)?.leadingAction as { props: { onClick: () => void } };
    act(() => back.props.onClick());
    expect(screen.queryByTestId("issue-detail")).toBeNull();
    fireEvent.click(screen.getByTestId("row"));
    expect(issueDetailProps.at(-1)?.highlightCommentId).toBe("c2");
  });

  it("keeps the selected landing when opening the phone inbox auto-marks the issue read", () => {
    reset();
    layout.width = PHONE;
    searchParams = new URLSearchParams("issue=issue-1");
    listData.active = [
      item({ id: "n1", read: false, created_at: "2026-11-15T08:00:00Z", details: { comment_id: "c1" } }),
      item({ id: "n2", read: false, created_at: "2026-11-15T08:01:00Z", details: { comment_id: "c2" } }),
      item({ id: "n3", read: false, created_at: "2026-11-15T08:02:00Z", details: { comment_id: "c3" } }),
    ];
    markReadMutate.mockImplementation(() => {
      listData.active = listData.active.map((current) => ({ ...current, read: true }));
    });
    const { rerender } = render(<InboxPage />);
    expect(markReadMutate).toHaveBeenCalledWith("n3", expect.anything());
    expect(issueDetailProps.at(-1)?.highlightCommentId).toBe("c1");
    rerender(<InboxPage />);
    expect(issueDetailProps.at(-1)?.highlightCommentId).toBe("c1");
  });

  it("picks the landing again when the Work panel's row is clicked again", () => {
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("view=archived&issue=issue-1");
    listData.archived = [
      item({ id: "n1", archived: true, read: false, created_at: "2026-11-15T08:00:00Z", details: { comment_id: "c1" } }),
      item({ id: "n2", archived: true, read: false, created_at: "2026-11-15T08:02:00Z", details: { comment_id: "c2" } }),
    ];
    markReadMutate.mockImplementation(() => {
      listData.archived = listData.archived.map((current) => ({ ...current, read: true }));
    });
    const { rerender } = render(<InboxPage />);
    expect(issueDetailProps.at(-1)?.highlightCommentId).toBe("c1");
    rerender(<InboxPage />);
    expect(issueDetailProps.at(-1)?.highlightCommentId).toBe("c1");

    // Everything is read by now, so a fresh click opens at the newest comment.
    act(() => useInboxSelectionStore.getState().requestLanding("issue-1"));
    expect(issueDetailProps.at(-1)?.highlightCommentId).toBe("c2");
  });

  it("opens a notification the Work panel offered despite a type filter", () => {
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("issue=quick-failed");
    listData.active = [item({ id: "quick-failed", issue_id: null, type: "quick_create_failed", title: "Review quick-create failure" })];
    useInboxFilterStore.getState().toggleFilterValue("workspace-1", "type", "mentions");
    render(<InboxPage />);
    expect(replace).not.toHaveBeenCalledWith("/acme/inbox");
    expect(screen.getByText("Review quick-create failure")).toBeInTheDocument();
  });

  it("replays the comment landing, and holds the detail's memento off, until the issue lands", () => {
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("view=archived&issue=issue-1");
    listData.archived = [item({ archived: true, details: { comment_id: "comment-1" } })];
    render(<InboxPage />);
    const firstToken = issueDetailProps.at(-1)?.highlightRequestToken;
    act(() => useInboxSelectionStore.getState().requestLanding("issue-1"));
    expect(issueDetailProps.at(-1)?.highlightRequestToken).not.toBe(firstToken);
    expect(issueDetailProps.at(-1)?.highlightRequestPending).toBe(true);

    act(() => (issueDetailProps.at(-1)?.onHighlightLanded as () => void)());
    expect(issueDetailProps.at(-1)?.highlightRequestPending).toBe(false);
  });

  // Only the phone's page lists notifications; on desktop the Work panel does.
  it.each([false, true])("enabling idle closes an already busy notification with unread pin=%s", (unreadOnly) => {
    reset();
    layout.width = PHONE;
    searchParams = new URLSearchParams("issue=busy-issue");
    listData.active = [item({ id: "busy-row", issue_id: "busy-issue", read: !unreadOnly })];
    listData.tasks = [{ id: "task-a", agent_id: "agent-1", issue_id: "busy-issue", status: "running" }] as AgentTask[];
    if (unreadOnly) useInboxFilterStore.getState().toggleUnreadOnly("workspace-1");
    markReadMutate.mockImplementation(() => {
      listData.active = listData.active.map((current) => ({ ...current, read: true }));
    });
    const { rerender } = render(<InboxPage />);
    rerender(<InboxPage />);
    expect(screen.getByTestId("issue-detail")).toHaveTextContent("busy-issue");

    act(() => useInboxFilterStore.getState().setAgent("workspace-1", "idle"));

    expect(screen.queryByTestId("issue-detail")).toBeNull();
    expect(replace).toHaveBeenLastCalledWith("/acme/inbox");
  });

  it("enabling unread closes a read notification pinned by idle", () => {
    reset();
    layout.width = PHONE;
    searchParams = new URLSearchParams("issue=idle-issue");
    listData.active = [item({ id: "read-row", issue_id: "idle-issue", read: true })];
    useInboxFilterStore.getState().setAgent("workspace-1", "idle");
    render(<InboxPage />);
    expect(screen.getByTestId("issue-detail")).toHaveTextContent("idle-issue");

    act(() => useInboxFilterStore.getState().toggleUnreadOnly("workspace-1"));

    expect(screen.queryByTestId("issue-detail")).toBeNull();
  });

  it("re-enabling idle evaluates current activity after disabling it", () => {
    reset();
    layout.width = PHONE;
    searchParams = new URLSearchParams("issue=issue-a");
    listData.active = [item({ id: "was-idle", issue_id: "issue-a", read: true })];
    useInboxFilterStore.getState().setAgent("workspace-1", "idle");
    const { rerender } = render(<InboxPage />);
    act(() => useInboxFilterStore.getState().setAgent("workspace-1", "idle"));
    listData.tasks = [{ id: "task-a", agent_id: "agent-1", issue_id: "issue-a", status: "running" }] as AgentTask[];
    rerender(<InboxPage />);
    expect(screen.getByTestId("issue-detail")).toHaveTextContent("issue-a");

    act(() => useInboxFilterStore.getState().setAgent("workspace-1", "idle"));

    expect(screen.queryByTestId("issue-detail")).toBeNull();
  });

  // The Work panel's Inbox section is the list from the compact breakpoint up.
  it("shows no list of its own on desktop, only a prompt until a notification is picked", () => {
    reset();
    layout.width = DESKTOP;
    listData.active = [item({ id: "inbox-a", issue_id: "issue-a" })];

    const { container } = render(<InboxPage />);

    expect(screen.queryByTestId("list")).toBeNull();
    expect(container.querySelector("[data-slot='list-column']")).toBeNull();
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Inbox");
    expect(screen.queryByTestId("issue-detail")).toBeNull();
  });

  // A list that never loaded is not an empty Inbox (MUL-9635).
  it.each([
    ["a phone", PHONE],
    ["desktop", DESKTOP],
  ])("on %s, offers another try when the Inbox could not load, not an empty Inbox", (_device, width) => {
    reset();
    layout.width = width;
    activeFailure.failed = true;

    render(<InboxPage />);

    const alert = screen.getByRole("alert");
    expect(screen.queryByTestId("list")).toBeNull();
    fireEvent.click(within(alert).getByRole("button"));
    expect(activeFailure.refetch).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["a phone", PHONE],
    ["desktop", DESKTOP],
  ])("on %s, keeps the load error while it is tried again", (_device, width) => {
    reset();
    layout.width = width;
    activeFailure.failed = true;
    activeFailure.retrying = true;
    searchParams = new URLSearchParams("issue=issue-1");

    render(<InboxPage />);

    expect(within(screen.getByRole("alert")).getByRole("button")).toBeDisabled();
    // Still unknown, so the open notification is not sent to its issue page.
    expect(replace).not.toHaveBeenCalled();
  });

  // Every tab lists in the Work panel's Inbox section.
  it("shows no list of its own on desktop for the archive either", () => {
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("view=archived");
    listData.archived = [item({ id: "archived-1", archived: true })];

    const { container } = render(<InboxPage />);

    expect(screen.queryByTestId("list")).toBeNull();
    expect(container.querySelector("[data-slot='list-column']")).toBeNull();
  });

  it("leads the detail with the way back to the open tab on desktop, and leaves it to the back control on a phone", () => {
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("view=archived&issue=issue-9");
    listData.archived = [item({ id: "archived-1", issue_id: "issue-9", archived: true })];
    const host = () => issueDetailProps.at(-1)?.inbox as { crumb?: { href: string }; item?: InboxItem } | undefined;
    const { unmount } = render(<InboxPage />);
    expect(host()?.crumb?.href).toBe("/acme/inbox?view=archived");
    // Archived, so the issue's own notification actions stay out of it.
    expect(host()?.item).toBeUndefined();
    unmount();

    reset();
    searchParams = new URLSearchParams("issue=issue-1");
    listData.active = [item()];
    render(<InboxPage />);
    expect(host()?.crumb).toBeUndefined();
    expect(host()?.item?.id).toBe("inbox-1");
  });

  it("shows the active list by default", () => {
    reset();
    listData.active = [item({ id: "active-1" })];
    listData.archived = [item({ id: "archived-1", archived: true })];

    render(<InboxPage />);

    expect(screen.getByTestId("list").dataset.view).toBe("inbox");
    expect(screen.getByTestId("row").textContent).toBe("active-1");
  });

  it("filters the list by status and priority together", () => {
    reset();
    listData.active = [
      item({
        id: "todo-high",
        issue_id: "issue-1",
        issue_status: "todo",
        issue_priority: "high",
      }),
      item({
        id: "done-low",
        issue_id: "issue-2",
        issue_status: "done",
        issue_priority: "low",
      }),
      item({ id: "system", issue_id: null }),
    ];
    const filters = useInboxFilterStore.getState();
    filters.toggleStatusFilter("workspace-1", "done");
    filters.togglePriorityFilter("workspace-1", "low");

    render(<InboxPage />);

    expect(screen.getAllByTestId("row")).toHaveLength(1);
    expect(screen.getByTestId("row")).toHaveTextContent("done-low");
  });

  it("hides read notifications while the unread filter is on", () => {
    reset();
    listData.active = [
      item({ id: "unread-row", issue_id: "issue-1", read: false }),
      item({ id: "read-row", issue_id: "issue-2", read: true }),
    ];
    useInboxFilterStore.getState().toggleUnreadOnly("workspace-1");

    render(<InboxPage />);

    expect(screen.getAllByTestId("row")).toHaveLength(1);
    expect(screen.getByTestId("row")).toHaveTextContent("unread-row");
  });

  it("filters by the actor the row carries", () => {
    reset();
    listData.active = [
      item({ id: "from-alice", issue_id: "issue-1", actor_type: "member", actor_id: "alice" }),
      item({ id: "from-bob", issue_id: "issue-2", actor_type: "agent", actor_id: "bob" }),
    ];
    useInboxFilterStore.getState().toggleActorFilter("workspace-1", "member:alice");

    render(<InboxPage />);

    expect(screen.getAllByTestId("row")).toHaveLength(1);
    expect(screen.getByTestId("row")).toHaveTextContent("from-alice");
  });

  it("matches a type on any notification of an issue and shows the newest match", () => {
    reset();
    listData.active = [
      item({ id: "newest-comment", issue_id: "issue-1", type: "new_comment", created_at: "2026-06-15T09:00:00Z" }),
      item({ id: "older-mention", issue_id: "issue-1", type: "mentioned", created_at: "2026-06-15T08:00:00Z" }),
      item({ id: "other-comment", issue_id: "issue-2", type: "new_comment" }),
    ];
    useInboxFilterStore.getState().toggleFilterValue("workspace-1", "type", "mention");

    render(<InboxPage />);

    expect(screen.getAllByTestId("row")).toHaveLength(1);
    expect(screen.getByTestId("row")).toHaveTextContent("older-mention");
    expect(screen.getByTestId("filter-chips").dataset.types).toBe("mention");
  });

  it("keeps a type filtered out with is not", () => {
    reset();
    listData.active = [
      item({ id: "mention", issue_id: "issue-1", type: "mentioned" }),
      item({ id: "comment", issue_id: "issue-2", type: "new_comment" }),
    ];
    const filters = useInboxFilterStore.getState();
    filters.toggleFilterValue("workspace-1", "type", "mention");
    filters.setFilterNegated("workspace-1", "type", true);

    render(<InboxPage />);

    expect(screen.getAllByTestId("row")).toHaveLength(1);
    expect(screen.getByTestId("row")).toHaveTextContent("comment");
  });

  it("hides issues an agent is running or queued on while the idle filter is on", () => {
    reset();
    listData.active = [
      item({ id: "running", issue_id: "issue-1" }),
      item({ id: "queued", issue_id: "issue-2" }),
      item({ id: "finished", issue_id: "issue-3" }),
      item({ id: "untouched", issue_id: "issue-4" }),
    ];
    listData.tasks = [
      { id: "task-1", agent_id: "agent-1", issue_id: "issue-1", status: "running" },
      { id: "task-2", agent_id: "agent-1", issue_id: "issue-2", status: "queued" },
      { id: "task-3", agent_id: "agent-1", issue_id: "issue-3", status: "completed" },
    ] as AgentTask[];
    useInboxFilterStore.getState().setAgent("workspace-1", "idle");

    render(<InboxPage />);

    expect(screen.getAllByTestId("row").map((row) => row.textContent)).toEqual([
      "finished",
      "untouched",
    ]);
  });

  it("offers to clear filters when they hide every notification", () => {
    reset();
    listData.active = [
      item({
        id: "todo-high",
        issue_status: "todo",
        issue_priority: "high",
      }),
    ];
    useInboxFilterStore
      .getState()
      .togglePriorityFilter("workspace-1", "urgent");

    render(<InboxPage />);

    expect(screen.queryByTestId("row")).toBeNull();
    // The test's `useT` names every control "Inbox"; the header's batch menu
    // shares the name, so skip the menu trigger.
    const clearFilters = screen
      .getAllByRole("button", { name: "Inbox" })
      .find((button) => !button.hasAttribute("aria-haspopup"));
    fireEvent.click(clearFilters!);
    expect(screen.getByTestId("row")).toHaveTextContent("todo-high");
  });

  it("ignores a priority filter when a legacy response omits the projection", () => {
    reset();
    const legacyItem = item({
      id: "legacy-todo",
      issue_status: "todo",
    });
    delete legacyItem.issue_priority;
    listData.active = [legacyItem];
    useInboxFilterStore
      .getState()
      .togglePriorityFilter("workspace-1", "urgent");

    render(<InboxPage />);

    expect(screen.getByTestId("row")).toHaveTextContent("legacy-todo");
  });

  it("only enables the current inbox view's list", () => {
    reset();
    const main = render(<InboxPage />);
    expect(queryCalls.find((q) => q.queryKey.includes("list"))?.enabled).toBe(true);
    expect(queryCalls.find((q) => q.queryKey.includes("pages"))?.enabled).toBe(false);
    main.unmount();
    reset();
    searchParams = new URLSearchParams("view=archived");
    render(<InboxPage />);
    expect(queryCalls.find((q) => q.queryKey.includes("list"))?.enabled).toBe(false);
    expect(queryCalls.find((q) => q.queryKey.includes("pages"))?.enabled).toBe(true);
  });

  it("opens a deep-linked archive group outside the loaded pages with its comment anchor", () => {
    reset();
    searchParams = new URLSearchParams("view=archived&issue=old-issue");
    listData.archived = [item({ id: "recent", archived: true })];
    listData.lookup = [item({ id: "older", issue_id: "old-issue", archived: true, details: { comment_id: "old-comment" } })];
    render(<InboxPage />);
    expect(replace).not.toHaveBeenCalled();
    expect(issueDetailProps.at(-1)).toMatchObject({ issueId: "old-issue", highlightCommentId: "old-comment" });
    expect(queryCalls.find((q) => q.queryKey.includes("lookup"))?.enabled).toBe(true);
  });

  describe("archive deep links", () => {
    function setupLookup() {
      reset();
      searchParams = new URLSearchParams("view=archived&issue=old-issue");
      listData.archived = [item({ id: "recent", issue_id: "recent-issue", archived: true })];
      listData.lookup = [];
    }

    it("keeps loaded rows visible while resolving the selection, then opens its detail", () => {
      setupLookup();
      lookupState.isLoading = true;
      const { rerender } = render(<InboxPage />);
      expect(screen.getByTestId("row")).toHaveTextContent("recent");
      expect(replace).not.toHaveBeenCalled();
      expect(issueDetailProps).toHaveLength(0);

      lookupState.isLoading = false;
      listData.lookup = [item({ id: "older", issue_id: "old-issue", archived: true })];
      rerender(<InboxPage />);
      expect(issueDetailProps.at(-1)).toMatchObject({ issueId: "old-issue" });
      expect(replace).not.toHaveBeenCalled();
    });

    it("keeps the list usable on lookup failure and retries only the lookup", () => {
      setupLookup();
      lookupState.isError = true;
      render(<InboxPage />);
      expect(screen.getByTestId("row")).toHaveTextContent("recent");
      expect(replace).not.toHaveBeenCalled();
      fireEvent.click(screen.getByRole("alert").querySelector("button")!);
      expect(lookupState.refetch).toHaveBeenCalledTimes(1);

      fireEvent.click(screen.getByTestId("row"));
      expect(issueDetailProps.at(-1)).toMatchObject({ issueId: "recent-issue" });
      expect(screen.queryByRole("alert")).toBeNull();
    });

    it("only falls back to the issue after the lookup confirms the group is absent", () => {
      setupLookup();
      lookupState.isLoading = true;
      const { rerender } = render(<InboxPage />);
      expect(replace).not.toHaveBeenCalled();
      lookupState.isLoading = false;
      rerender(<InboxPage />);
      expect(replace).toHaveBeenCalledWith("/acme/issues/old-issue");
    });
  });

  it("renders the archived list when the URL asks for it", () => {
    // ?view=archived is what makes a refresh, a back/forward step, or a mobile
    // detail-back land in the archive instead of the main inbox.
    reset();
    searchParams = new URLSearchParams("view=archived");
    listData.active = [item({ id: "active-1" })];
    listData.archived = [item({ id: "archived-1", archived: true })];

    render(<InboxPage />);

    expect(screen.getByTestId("list").dataset.view).toBe("archived");
    expect(screen.getByTestId("row").textContent).toBe("archived-1");
  });

  it("switches between the tabs from a phone's header", () => {
    reset();
    listData.active = [item({ id: "active-1", issue_id: "issue-1" })];
    listData.archived = [item({ id: "archived-1", archived: true })];
    render(<InboxPage />);

    // The test's `useT` labels every tab "Inbox", so they are located by order.
    const [inboxTab, snoozedTab, archivedTab] = screen.getAllByRole("tab");
    expect(inboxTab).toHaveAttribute("aria-selected", "true");
    fireEvent.click(archivedTab!);
    expect(replace).toHaveBeenLastCalledWith("/acme/inbox?view=archived");
    expect(screen.getByTestId("list").dataset.view).toBe("archived");

    fireEvent.click(snoozedTab!);
    expect(replace).toHaveBeenLastCalledWith("/acme/inbox?view=snoozed");
    expect(screen.queryByTestId("list")).toBeNull();
  });

  it("hides the batch-actions menu in the archived view", () => {
    // Every batch action archives from the MAIN inbox; offering them over the
    // archived list would read as "archive all of these" and do the opposite.
    reset();
    listData.archived = [item({ id: "archived-1", archived: true })];
    const batchMenu = '[aria-haspopup="menu"]';
    const { container: mainView } = render(<InboxPage />);
    expect(mainView.querySelector(batchMenu)).not.toBeNull();

    searchParams = new URLSearchParams("view=archived");
    const { container: archivedView } = render(<InboxPage />);
    expect(archivedView.querySelector(batchMenu)).toBeNull();
  });

  it("ignores a carried-over idle selection in the archive", () => {
    reset();
    searchParams = new URLSearchParams("view=archived");
    listData.archived = [item({ id: "archived-busy", issue_id: "issue-1", archived: true })];
    listData.tasks = [
      { id: "task-1", agent_id: "agent-1", issue_id: "issue-1", status: "running" },
    ] as AgentTask[];
    useInboxFilterStore.getState().setAgent("workspace-1", "idle");

    render(<InboxPage />);

    expect(screen.getByTestId("row")).toHaveTextContent("archived-busy");
    expect(screen.getByTestId("filter-chips").dataset.agent).toBe("");
  });

  it("keeps the archive open when it is empty", () => {
    reset();
    searchParams = new URLSearchParams("view=archived");
    listData.archived = [];
    render(<InboxPage />);
    expect(replace).not.toHaveBeenCalled();
    expect(screen.getByTestId("list").dataset.view).toBe("archived");
  });

  it("keeps its own detail pane from reading the inbox, so a row marked unread stays unread", () => {
    // The detail reads an issue's notifications when it is opened anywhere
    // else (MUL-8588). Here the page reads the row it selected itself, and a
    // row the reader marks unread while it is open must not be read again.
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("issue=issue-1");
    listData.active = [item()];

    render(<InboxPage />);
    expect(issueDetailProps.at(-1)).toMatchObject({ readsInbox: false });
  });

  it("keeps the archived view in the URL when selecting an item there", () => {
    // A bare `?issue=` write would silently drop the user back to the main
    // inbox on the next refresh — both pieces of state travel together.
    reset();
    searchParams = new URLSearchParams("view=archived");
    listData.archived = [
      item({ id: "archived-1", issue_id: "issue-9", archived: true }),
    ];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));

    expect(replace).toHaveBeenCalledWith("/acme/inbox?view=archived&issue=issue-9");
  });

  it("writes a bare issue param when selecting in the main view", () => {
    reset();
    listData.active = [item({ id: "active-1", issue_id: "issue-3" })];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));

    expect(replace).toHaveBeenCalledWith("/acme/inbox?issue=issue-3");
  });

  // `InboxItem.issue_id` is nullable: a quick-create outcome is a notification,
  // not an issue, so `IssueDetail` never renders for it — and `IssueDetail` is
  // what carries the way back in its own header on a phone. This branch has to
  // supply its own bar or opening one of these is a dead end.
  it("keeps a way back to the list for a notification with no issue", () => {
    reset();
    listData.active = [
      item({ id: "inbox-note", issue_id: null, type: "quick_create_failed" }),
    ];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));

    // Mobile swaps the list out for the detail, so the row is gone…
    expect(screen.queryByTestId("row")).toBeNull();

    // …and the only thing that can bring it back is the bar this branch adds.
    // Located structurally: the test's `useT` returns one string for every key,
    // so every button in this detail shares an accessible name.
    const back = document.querySelector<HTMLButtonElement>(".h-12.border-b button");
    expect(back).not.toBeNull();

    fireEvent.click(back!);

    expect(screen.getByTestId("row")).toBeInTheDocument();
  });

  it("retries a failed quick-create with its original source context", async () => {
    reset();
    listData.active = [
      item({
        id: "source-context-failure",
        issue_id: null,
        type: "quick_create_failed",
        details: {
          task_id: "task-1",
          source_context_id: "context-1",
          original_prompt: "make a child",
        },
      }),
    ];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));
    fireEvent.click(screen.getByTestId("retry-source-context"));

    await act(async () => undefined);
    expect(retrySourceContextMutateAsync).toHaveBeenCalledWith("task-1");
    expect(toast.success).toHaveBeenCalledTimes(1);
  });

  it("opens quota-specific recovery from an autopilot quota notice", () => {
    reset();
    listData.active = [
      item({
        id: "autopilot-quota",
        issue_id: null,
        type: "autopilot_quota_exceeded",
      }),
    ];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));
    fireEvent.click(screen.getByTestId("autopilot-quota-recovery"));

    expect(showAutopilotQuotaRecoveryPrompt).toHaveBeenCalledTimes(1);
  });

  it("marks the opened notification read", () => {
    reset();
    listData.active = [
      item({ id: "inbox-a", issue_id: "issue-a", read: false }),
    ];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));

    expect(markReadMutate).toHaveBeenCalledWith("inbox-a", expect.anything());
  });

  it("keeps an unread-filtered notification open after it is marked read", () => {
    // Opening a row marks it read. With the unread filter on, that would drop
    // the row before its detail renders and clear the selection, so the click
    // looked like it only made the notification disappear.
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("issue=issue-a");
    listData.active = [
      item({ id: "inbox-a", issue_id: "issue-a", read: false }),
    ];
    useInboxFilterStore.getState().toggleUnreadOnly("workspace-1");
    markReadMutate.mockImplementation((id: string) => {
      listData.active = listData.active.map((current) =>
        current.id === id ? { ...current, read: true } : current,
      );
    });

    const { rerender } = render(<InboxPage />);
    rerender(<InboxPage />);

    expect(markReadMutate).toHaveBeenCalledWith("inbox-a", expect.anything());
    expect(screen.getByTestId("issue-detail")).toHaveTextContent("issue-a");
    expect(replace).not.toHaveBeenCalledWith("/acme/inbox");
  });

  it("keeps an idle-filtered notification open after an agent starts on it", () => {
    // Replying from the detail can start an agent on the open issue. With the
    // idle filter on, that would drop the row the user is working in.
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("issue=issue-a");
    listData.active = [
      item({ id: "inbox-a", issue_id: "issue-a", read: true }),
      item({ id: "inbox-b", issue_id: "issue-b", read: true }),
    ];
    useInboxFilterStore.getState().setAgent("workspace-1", "idle");

    const { rerender } = render(<InboxPage />);
    rerender(<InboxPage />);
    listData.tasks = [
      { id: "task-a", agent_id: "agent-1", issue_id: "issue-a", status: "queued" },
    ] as AgentTask[];
    rerender(<InboxPage />);

    expect(screen.getByTestId("issue-detail")).toHaveTextContent("issue-a");
    expect(replace).not.toHaveBeenCalledWith("/acme/inbox");
  });

  it("keeps a phone's open notification in place once it is archived some other way", () => {
    reset();
    searchParams = new URLSearchParams("issue=issue-a");
    listData.active = [
      item({ id: "inbox-a", issue_id: "issue-a" }),
      item({ id: "inbox-b", issue_id: "issue-b" }),
    ];
    const { rerender } = render(<InboxPage />);

    listData.active = [item({ id: "inbox-b", issue_id: "issue-b" })];
    rerender(<InboxPage />);

    expect(screen.getByTestId("issue-detail")).toHaveTextContent("issue-a");
    expect(replace).not.toHaveBeenCalledWith("/acme/inbox");
  });

  it("keeps an explicitly unread row unread while it stays open", () => {
    // Without the guard the auto-read effect fires on the very next commit and
    // silently undoes the user's "mark as unread" — the action looks like a
    // no-op.
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("issue=issue-a");
    listData.active = [
      item({ id: "inbox-a", issue_id: "issue-a", read: false }),
    ];

    const { rerender } = render(<InboxPage />);
    markReadMutate.mockClear();

    act(() => inboxCommands().markUnread("inbox-a"));
    rerender(<InboxPage />);

    expect(markUnreadMutate).toHaveBeenCalledWith("inbox-a", expect.anything());
    expect(markReadMutate).not.toHaveBeenCalled();
  });

  it("marks a parked row read again once it is re-opened", () => {
    // The guard is scoped to the row while it stays selected. Coming back to it
    // later is a fresh open and must behave like any other.
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("issue=issue-a");
    listData.active = [
      item({ id: "inbox-a", issue_id: "issue-a", read: false }),
      item({ id: "inbox-b", issue_id: "issue-b", read: false }),
    ];

    const { rerender } = render(<InboxPage />);
    act(() => inboxCommands().markUnread("inbox-a"));

    searchParams = new URLSearchParams("issue=issue-b");
    rerender(<InboxPage />);
    markReadMutate.mockClear();
    searchParams = new URLSearchParams("issue=issue-a");
    rerender(<InboxPage />);

    expect(markReadMutate).toHaveBeenCalledWith("inbox-a", expect.anything());
  });

  it("folds to a single column on a foldable's inner screen held upright", () => {
    // Below the compact breakpoint the list and detail cannot share the
    // width: opening a row replaces the list.
    reset();
    layout.width = FOLD_INNER_PORTRAIT;
    listData.active = [item({ id: "inbox-a", issue_id: "issue-a" })];

    render(<InboxPage />);
    expect(screen.queryByTestId("list")).not.toBeNull();

    fireEvent.click(screen.getByTestId("row"));

    expect(screen.queryByTestId("list")).toBeNull();
  });

  it.each([TABLET, FOLD_INNER_LANDSCAPE])("hands the list to the Work panel at %spx", (width) => {
    // 768px is the first width with the Work panel beside the page, so the
    // page shows the notification picked there and keeps no list of its own.
    reset();
    layout.width = width;
    searchParams = new URLSearchParams("issue=issue-a");
    listData.active = [item({ id: "inbox-a", issue_id: "issue-a" })];

    render(<InboxPage />);

    expect(screen.queryByTestId("list")).toBeNull();
    expect(screen.getByTestId("issue-detail")).toHaveTextContent("issue-a");
  });

  // Phone width, where the page's own list carries the row actions.
  function renderWithActiveItem() {
    reset();
    listData.active = [item({ id: "inbox-a", issue_id: "issue-a" })];
    return render(<InboxPage />);
  }

  // An issue's own bar takes E for its notification; the page keeps it for a
  // notification with no issue, and for the archive.
  function renderWithOpenItem() {
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("issue=inbox-a");
    listData.active = [item({ id: "inbox-a", issue_id: null, type: "quick_create_failed" })];
    return render(<InboxPage />);
  }

  describe("archive shortcut", () => {
    function pressArchiveKey(target: Element | Document = document) {
      fireEvent.keyDown(target, { key: "e" });
    }

    function typeArchiveKeyInto(element: Element) {
      document.body.appendChild(element);
      pressArchiveKey(element);
      element.remove();
    }

    it("archives the open notification", () => {
      renderWithOpenItem();
      pressArchiveKey();

      expect(archiveMutate).toHaveBeenCalledWith("inbox-a", expect.anything());
    });

    it("restores the open notification while reading the archive", () => {
      reset();
      layout.width = DESKTOP;
      searchParams = new URLSearchParams("view=archived&issue=issue-9");
      listData.archived = [
        item({ id: "archived-1", issue_id: "issue-9", archived: true }),
      ];

      render(<InboxPage />);
      pressArchiveKey();

      expect(unarchiveMutate).toHaveBeenCalledWith("archived-1", expect.anything());
      expect(archiveMutate).not.toHaveBeenCalled();
    });

    it("leaves the selection on the next notification", () => {
      reset();
      layout.width = DESKTOP;
      searchParams = new URLSearchParams("issue=inbox-a");
      listData.active = [
        item({ id: "inbox-a", issue_id: null, type: "quick_create_failed" }),
        item({ id: "inbox-b", issue_id: "issue-b" }),
      ];

      render(<InboxPage />);
      replace.mockClear();
      pressArchiveKey();

      expect(replace).toHaveBeenCalledWith("/acme/inbox?issue=issue-b");
    });

    it("does not fire while typing in an editable control", () => {
      renderWithOpenItem();

      typeArchiveKeyInto(document.createElement("input"));
      typeArchiveKeyInto(document.createElement("textarea"));
      const richText = document.createElement("div");
      richText.setAttribute("contenteditable", "true");
      typeArchiveKeyInto(richText);

      expect(archiveMutate).not.toHaveBeenCalled();
    });

    it("stands down while a dialog is open", () => {
      renderWithOpenItem();
      modalState.modal = "create-issue";
      pressArchiveKey();

      expect(archiveMutate).not.toHaveBeenCalled();
    });

    // Keypresses in portaled popups still reach the page listener, where `e`
    // is typeahead, not archive.
    it.each([
      ["menu", "menuitem"],
      ["dialog", "button"],
      ["listbox", "option"],
    ])("stands down while a portaled %s owns the keyboard", (layerRole, itemRole) => {
      renderWithOpenItem();

      const layer = document.createElement("div");
      layer.setAttribute("role", layerRole);
      const focused = document.createElement("div");
      focused.setAttribute("role", itemRole);
      layer.appendChild(focused);
      document.body.appendChild(layer);
      pressArchiveKey(focused);
      layer.remove();

      expect(archiveMutate).not.toHaveBeenCalled();
    });

    it("stands down while a modal layer holds the page inert", () => {
      // Base UI marks everything outside a modal popup `data-base-ui-inert`,
      // even when focus never left the page.
      const { container } = renderWithOpenItem();
      container.firstElementChild?.setAttribute("data-base-ui-inert", "");
      pressArchiveKey();

      expect(archiveMutate).not.toHaveBeenCalled();
    });

    it("ignores an auto-repeated key", () => {
      renderWithOpenItem();
      fireEvent.keyDown(document, { key: "e", repeat: true });

      expect(archiveMutate).not.toHaveBeenCalled();
    });

    it("ignores a press a nearer handler already consumed", () => {
      renderWithOpenItem();

      const consumer = document.createElement("button");
      consumer.addEventListener("keydown", (event) => event.preventDefault());
      document.body.appendChild(consumer);
      pressArchiveKey(consumer);
      consumer.remove();

      expect(archiveMutate).not.toHaveBeenCalled();
    });

    it("leaves an issue's notification to the issue's own bar", () => {
      reset();
      layout.width = DESKTOP;
      searchParams = new URLSearchParams("issue=issue-a");
      listData.active = [item({ id: "inbox-a", issue_id: "issue-a" })];
      render(<InboxPage />);
      pressArchiveKey();

      expect(archiveMutate).not.toHaveBeenCalled();
    });

    it("does nothing when no notification is open", () => {
      renderWithActiveItem();
      pressArchiveKey();

      expect(archiveMutate).not.toHaveBeenCalled();
    });
  });

  // Toasts live in the shared handlers, so every archive surface reports alike.
  describe("archive feedback", () => {
    type MutateOptions = {
      onSuccess?: () => void;
      onError?: (err: unknown) => void;
    };

    async function settle(
      spy: typeof archiveMutate,
      outcome: "success" | "error",
      err: unknown = new Error("boom"),
    ) {
      const options = spy.mock.calls.at(-1)?.[1] as MutateOptions | undefined;
      await act(async () => {
        if (outcome === "success") options?.onSuccess?.();
        else options?.onError?.(err);
      });
    }

    it("confirms an archive from the row action, with an undo", async () => {
      renderWithActiveItem();
      act(() => rowActions?.onAction("inbox-a"));
      await settle(archiveMutate, "success");
      expect(toast.success).toHaveBeenCalledWith(
        "Inbox",
        expect.objectContaining({ duration: 10_000, action: { label: "Inbox", onClick: expect.any(Function) } }),
      );

      expect(toast.success).toHaveBeenCalledTimes(1);
    });

    it("confirms an archive driven by the shortcut", async () => {
      renderWithOpenItem();
      fireEvent.keyDown(document, { key: "e" });
      await settle(archiveMutate, "success");

      expect(toast.success).toHaveBeenCalledTimes(1);
    });

    it("confirms a restore", async () => {
      reset();
      searchParams = new URLSearchParams("view=archived");
      listData.archived = [
        item({ id: "archived-1", issue_id: "issue-9", archived: true }),
      ];

      render(<InboxPage />);
      act(() => rowActions?.onAction("archived-1"));
      await settle(unarchiveMutate, "success");

      expect(toast.success).toHaveBeenCalledTimes(1);
    });

    it("reports a failed archive as a failure only", async () => {
      renderWithActiveItem();
      act(() => rowActions?.onAction("inbox-a"));
      await settle(archiveMutate, "error");

      expect(toast.error).toHaveBeenCalledTimes(1);
      expect(toast.success).not.toHaveBeenCalled();
    });
  });

  it("does not swallow a deep link to an issue that is not in the archive", () => {
    // ?view=archived&issue=X with an empty archive: the drain effect and the
    // unresolved-selection fallback both want to navigate. The fallback must
    // win, or the deep link silently lands on an empty inbox instead of X.
    reset();
    searchParams = new URLSearchParams("view=archived&issue=issue-404");
    listData.archived = [];

    render(<InboxPage />);

    expect(replace).toHaveBeenCalledWith("/acme/issues/issue-404");
    expect(replace).not.toHaveBeenCalledWith("/acme/inbox");
  });
});
