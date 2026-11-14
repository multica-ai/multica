import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import type { InboxSections } from "@multica/core/inbox/sections";
import type { AgentTask, InboxItem, Issue } from "@multica/core/types";
import { useInboxFilterStore } from "@multica/core/inbox/filter-store";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { InboxPage } from "./inbox-page";

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));


// The page runs two queries — the active list and the archived one. They are
// told apart by the queryKey their options carry, so each test can stock the
// two lists independently.
const listData: {
  active: InboxItem[];
  archived: InboxItem[];
  lookup?: InboxItem[];
  tasks: AgentTask[];
  tasksPending: boolean;
  /** Issue details already in the cache, by id. One that is absent is loading. */
  issues: Record<string, Partial<Issue>>;
} = {
  active: [],
  archived: [],
  tasks: [],
  tasksPending: false,
  issues: {},
};

const queryCalls: Array<{ queryKey: readonly unknown[]; enabled?: boolean }> = [];
const lookupState = { isLoading: false, isError: false, refetch: vi.fn() };
const queryClient = vi.hoisted(() => ({ setQueryData: vi.fn() }));
vi.mock("@tanstack/react-query", () => ({
  queryOptions: (options: unknown) => options,
  useQueryClient: () => queryClient,
  useQuery: (options: { queryKey: readonly unknown[]; enabled?: boolean }) => {
    queryCalls.push(options);
    return ({
    data: options.queryKey.includes("archived")
      ? { items: listData.lookup ?? listData.archived, hasMore: false, nextCursor: null }
      : options.queryKey.includes("agent-task-snapshot") ? listData.tasks : listData.active,
    isLoading: false,
    isError: false,
    refetch: vi.fn(),
    ...(options.queryKey.includes("lookup") ? lookupState : {}),
    ...(options.queryKey.includes("agent-task-snapshot") ? { isPending: listData.tasksPending } : {}),
  }); },
  // "Working" rows are tested against their issue under a filter.
  useQueries: ({
    queries,
    combine,
  }: {
    queries: Array<{ queryKey: readonly unknown[] }>;
    combine: (results: Array<{ data: unknown; isPending: boolean }>) => unknown;
  }) =>
    combine(
      queries.map((query) => {
        const issue = listData.issues[String(query.queryKey.at(-1))];
        return { data: issue, isPending: !issue };
      }),
    ),
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

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    inbox: () => "/acme/inbox",
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
  deduplicateInboxItems: (items: InboxItem[]) => items.filter((i) => !i.archived),
  deduplicateArchivedInboxItems: (items: InboxItem[]) => items.filter((i) => i.archived),
  useInboxUnreadCount: () => 2,
}));

// Stable spies: the auto-mark-read effect keys on the mutate identity, so a
// fresh `vi.fn()` per render would make the effect's deps churn.
const markReadMutate = vi.fn();
const markUnreadMutate = vi.fn();
const archiveMutate = vi.fn();
const unarchiveMutate = vi.fn();
const snoozeMutate = vi.fn();
const unsnoozeMutate = vi.fn();
const archiveUpdatesMutate = vi.fn();
const retrySourceContextMutateAsync = vi.fn();
const showIssueLimitUpgradePrompt = vi.hoisted(() => vi.fn());
const showAutopilotQuotaRecoveryPrompt = vi.hoisted(() => vi.fn());

vi.mock("../../modals/use-issue-limit-upgrade-prompt", () => ({
  useIssueLimitUpgradePrompt: (reason?: string) =>
    reason === "autopilot_quota"
      ? showAutopilotQuotaRecoveryPrompt
      : showIssueLimitUpgradePrompt,
}));

vi.mock("@multica/core/inbox/mutations", () => {
  return {
    useMarkInboxRead: () => ({ mutate: markReadMutate }),
    useMarkInboxUnread: () => ({ mutate: markUnreadMutate }),
    useArchiveInbox: () => ({ mutate: archiveMutate }),
    useUnarchiveInbox: () => ({ mutate: unarchiveMutate }),
    useSnoozeInbox: () => ({ mutate: snoozeMutate }),
    useUnsnoozeInbox: () => ({ mutate: unsnoozeMutate }),
    useArchiveInboxUpdates: () => ({ mutate: archiveUpdatesMutate }),
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
    // The header slot the inbox fills with its own actions.
    return <>{props.hostActions as React.ReactNode}</>;
  },
  issueHighlightMementoKey: (issueId: string) => `highlight:${issueId}`,
}));

// The header actions as bare controls, so a test can drive what the page
// wired into them and read back the state it passed down.
const SNOOZE_UNTIL = new Date("2026-06-18T01:00:00Z");
vi.mock("./inbox-detail-actions", () => ({
  InboxDetailActions: ({
    snoozed,
    snoozeOpen,
    onSnooze,
    onUnsnooze,
    onArchive,
  }: {
    snoozed: boolean;
    snoozeOpen: boolean;
    onSnooze: (until: Date) => void;
    onUnsnooze: () => void;
    onArchive: () => void;
  }) => (
    <div data-testid="detail-actions" data-snoozed={String(snoozed)} data-snooze-open={String(snoozeOpen)}>
      <button type="button" data-testid="detail-snooze" onClick={() => onSnooze(SNOOZE_UNTIL)} />
      <button type="button" data-testid="detail-unsnooze" onClick={onUnsnooze} />
      <button type="button" data-testid="detail-archive" onClick={onArchive} />
    </div>
  ),
}));

const replace = vi.fn();
let searchParams = new URLSearchParams();

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ searchParams, replace }),
  // Real hook: reports the detail-pane swap to the shell's progress bar.
  // Nothing here renders that bar, and the page reads nothing back from it.
  useReportNavigating: () => {},
}));

// Drive the layout from a viewport width so a test can name the device it
// cares about instead of a boolean, and the two breakpoints stay in one place.
// Phone-width by default — one column keeps most assertions simple. Tests
// about actioning a row WHILE it is open need the two-panel desktop layout,
// since a single column swaps the list out for the detail on selection.
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
// The main view's list, flattened in section order. Rows keep the `row` test
// id whichever section they are in, so a test that only cares that a row is
// listed does not have to know where; `data-section` is there for the ones
// that do. "Working" rows are issues, not notifications, and get their own id.
vi.mock("./inbox-sectioned-list", () => ({
  InboxSectionedList: ({
    sections,
    onSelect,
    onSelectIssue,
    onClearUpdates,
    emptyLabel,
    emptyAction,
  }: {
    sections: InboxSections;
    onSelect: (item: InboxItem) => void;
    onSelectIssue: (issueId: string) => void;
    onClearUpdates?: () => void;
    emptyLabel?: string;
    emptyAction?: React.ReactNode;
  }) => {
    const groups = [
      ["needs_you", sections.needsYou],
      ["updates", sections.updates],
      ["snoozed", sections.snoozed],
    ] as const;
    const empty = groups.every(([, rows]) => rows.length === 0) && sections.working.length === 0;
    return (
      <div data-testid="list" data-view="inbox">
        {groups.slice(0, 1).map(([section, rows]) =>
          rows.map((row) => (
            <button key={row.item.id} data-testid="row" data-section={section} onClick={() => onSelect(row.item)}>
              {row.item.id}
            </button>
          )),
        )}
        {sections.working.map((row) => (
          <button key={row.issueId} data-testid="working-row" onClick={() => onSelectIssue(row.issueId)}>
            {row.issueId}
          </button>
        ))}
        {groups.slice(1).map(([section, rows]) =>
          rows.map((row) => (
            <button key={row.item.id} data-testid="row" data-section={section} onClick={() => onSelect(row.item)}>
              {row.item.id}
            </button>
          )),
        )}
        {sections.updates.length > 0 && (
          <button type="button" data-testid="clear-updates" onClick={onClearUpdates} />
        )}
        {empty && emptyLabel && <p>{emptyLabel}</p>}
        {empty && emptyAction}
      </div>
    );
  },
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "member-1" } }),
}));
vi.mock("./inbox-filter-menu", () => ({
  InboxFilterMenu: () => <button type="button">Filter inbox</button>,
}));
vi.mock("./inbox-list-item", () => ({ useTimeAgo: () => vi.fn() }));
vi.mock("./inbox-filter-options", () => ({
  InboxFilterChipsBar: ({ filters }: { filters: { types: string[] } }) => (
    <div data-testid="filter-chips" data-types={filters.types.join(",")} />
  ),
}));

// Capture the row actions the page hands the context menu, so the read/unread
// handlers can be driven without standing up Base UI's menu.
let rowActions: {
  onMarkRead: (id: string) => void;
  onMarkUnread: (id: string) => void;
  onAction: (id: string) => void;
  onSnooze: (id: string, until: Date) => void;
  onUnsnooze: (id: string) => void;
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
  useInboxSelectionStore.setState({ lastResolvedKeyByWorkspace: {} });
  listData.active = [];
  listData.archived = [];
  listData.lookup = undefined;
  listData.tasks = [];
  listData.tasksPending = false;
  listData.issues = {};
  lookupState.isLoading = false;
  lookupState.isError = false;
  lookupState.refetch.mockClear();
  queryCalls.length = 0;
  searchParams = new URLSearchParams();
  replace.mockClear();
  markReadMutate.mockReset();
  markUnreadMutate.mockClear();
  archiveMutate.mockClear();
  unarchiveMutate.mockClear();
  snoozeMutate.mockClear();
  unsnoozeMutate.mockClear();
  archiveUpdatesMutate.mockClear();
  retrySourceContextMutateAsync.mockReset();
  retrySourceContextMutateAsync.mockResolvedValue({});
  showIssueLimitUpgradePrompt.mockClear();
  showAutopilotQuotaRecoveryPrompt.mockClear();
  modalState.modal = null;
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
  rowActions = null;
  issueDetailProps.length = 0;
  layout.width = PHONE;
  useInboxFilterStore.setState({ filtersByWorkspace: {} });
}

describe("InboxPage", () => {
  // Inbox rows need more room than the nav's labels, so the list is sized by
  // the list width rather than the space panel's.
  it("sizes the desktop list with the list column width", () => {
    reset();
    layout.width = DESKTOP;

    const { container } = render(<InboxPage />);

    expect(container.querySelector("[data-slot='list-column']")).toHaveClass(
      "w-[min(var(--list-column-width),40vw)]",
    );
  });

  it("leaves the count to the section it counts, not the title", () => {
    reset();
    const { container } = render(<InboxPage />);

    expect(container.querySelector("h1")?.parentElement?.querySelector("number-flow-react")).toBeNull();
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

  it("leaves the filters that describe a notification to the archive", () => {
    // Read state, type, sender and age are what the sections already sort by.
    // A choice made in the archive is kept for it, and does nothing here.
    reset();
    listData.active = [
      item({ id: "unread-mention", issue_id: "issue-1", type: "mentioned", read: false, actor_type: "member", actor_id: "alice" }),
      item({ id: "read-comment", issue_id: "issue-2", type: "new_comment", read: true, actor_type: "agent", actor_id: "bob" }),
    ];
    const filters = useInboxFilterStore.getState();
    filters.toggleUnreadOnly("workspace-1");
    filters.toggleActorFilter("workspace-1", "member:alice");
    filters.toggleFilterValue("workspace-1", "type", "mention");
    filters.setReceived("workspace-1", "today");

    render(<InboxPage />);

    expect(screen.getAllByTestId("row").map((row) => row.textContent)).toEqual(["unread-mention", "read-comment"]);
    expect(screen.getByTestId("filter-chips").dataset.types).toBe("");
    expect(useInboxFilterStore.getState().filtersByWorkspace["workspace-1"]).toMatchObject({
      unreadOnly: true,
      types: ["mention"],
    });
  });

  it("keeps a notification filter in force in the archive", () => {
    reset();
    searchParams = new URLSearchParams("view=archived");
    listData.archived = [
      item({ id: "mention", issue_id: "issue-1", type: "mentioned", archived: true, issue_label_ids: [] }),
    ];
    useInboxFilterStore.getState().toggleFilterValue("workspace-1", "type", "mention");

    render(<InboxPage />);

    expect(screen.getByTestId("filter-chips").dataset.types).toBe("mention");
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
    // The test's `useT` names every control "Inbox"; the title's view switch
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

  describe.each([PHONE, DESKTOP])("archive deep links at width %s", (width) => {
    function setupLookup() {
      reset();
      layout.width = width;
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

  it("switches between the inbox and the archive from the title menu", async () => {
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("issue=issue-1");
    listData.active = [item({ id: "active-1", issue_id: "issue-1" })];
    const { container } = render(<InboxPage />);

    const titleMenu = container.querySelector("h1 [aria-haspopup='menu']") as HTMLElement;
    fireEvent.click(titleMenu);
    // The test's `useT` labels every option "Inbox", so they are located by order.
    const [inboxOption, archivedOption] = await screen.findAllByRole("menuitemradio");
    expect(inboxOption).toHaveAttribute("aria-checked", "true");
    fireEvent.click(archivedOption!);

    expect(replace).toHaveBeenLastCalledWith("/acme/inbox?view=archived");
    expect(screen.getByTestId("list").dataset.view).toBe("archived");
  });

  it("has no batch menu: nothing leaves Needs you in bulk", () => {
    reset();
    listData.active = [item({ id: "active-1" })];
    const { container } = render(<InboxPage />);

    // The title's view switch is the only menu the header opens.
    expect(container.querySelector(':not(h1) > [aria-haspopup="menu"]')).toBeNull();
  });

  it("keeps the archive open when it is empty", () => {
    reset();
    searchParams = new URLSearchParams("view=archived");
    listData.archived = [];
    render(<InboxPage />);
    expect(replace).not.toHaveBeenCalled();
    expect(screen.getByTestId("list").dataset.view).toBe("archived");
  });

  it("replays the comment highlight when the already-open row is clicked again", () => {
    // Re-clicking the open notification is an explicit "take me back to that
    // comment". It doesn't remount the detail (same issue key), so the page
    // signals the replay through the bumped token; a selection change keeps
    // the token still — its remount replays the landing by itself.
    reset();
    layout.width = DESKTOP;
    listData.active = [item({ details: { comment_id: "comment-1" } })];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));
    expect(issueDetailProps.at(-1)?.highlightRequestToken).toBe(0);

    fireEvent.click(screen.getByTestId("row"));
    expect(issueDetailProps.at(-1)?.highlightRequestToken).toBe(1);
  });

  it("keeps its own detail pane from reading the inbox, so a row marked unread stays unread", () => {
    // The detail reads an issue's notifications when it is opened anywhere
    // else (MUL-8588). Here the page reads the row it selected itself, and a
    // row the reader marks unread while it is open must not be read again.
    reset();
    layout.width = DESKTOP;
    listData.active = [item()];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));
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
    expect(showIssueLimitUpgradePrompt).not.toHaveBeenCalled();
  });

  it("shows issue-limit recovery when a source-context retry is rejected", async () => {
    reset();
    retrySourceContextMutateAsync.mockRejectedValue(
      new ApiError(
        "workspace has reached its issue limit",
        402,
        "Payment Required",
        { code: "issue_limit_reached" },
      ),
    );
    listData.active = [
      item({
        id: "source-context-limit",
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
    expect(showIssueLimitUpgradePrompt).toHaveBeenCalledTimes(1);
    expect(showAutopilotQuotaRecoveryPrompt).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
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

  it("keeps an unread-filtered archived notification open after it is marked read", () => {
    // Opening a row marks it read. With the unread filter on, that would drop
    // the row before its detail renders and clear the selection, so the click
    // looked like it only made the notification disappear.
    reset();
    layout.width = DESKTOP;
    searchParams = new URLSearchParams("view=archived");
    listData.archived = [
      item({ id: "inbox-a", issue_id: "issue-a", read: false, archived: true }),
    ];
    useInboxFilterStore.getState().toggleUnreadOnly("workspace-1");
    markReadMutate.mockImplementation((id: string) => {
      listData.archived = listData.archived.map((current) =>
        current.id === id ? { ...current, read: true } : current,
      );
    });

    const { rerender } = render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));
    rerender(<InboxPage />);

    expect(markReadMutate).toHaveBeenCalledWith("inbox-a", expect.anything());
    expect(screen.getByTestId("row")).toHaveTextContent("inbox-a");
    expect(replace).not.toHaveBeenCalledWith("/acme/inbox?view=archived");
  });

  it("keeps an explicitly unread row unread while it stays open", () => {
    // Without the guard the auto-read effect fires on the very next commit and
    // silently undoes the user's "mark as unread" — the action looks like a
    // no-op.
    reset();
    layout.width = DESKTOP;
    listData.active = [
      item({ id: "inbox-a", issue_id: "issue-a", read: false }),
    ];

    const { rerender } = render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));
    markReadMutate.mockClear();

    act(() => rowActions?.onMarkUnread("inbox-a"));
    rerender(<InboxPage />);

    expect(markUnreadMutate).toHaveBeenCalledWith("inbox-a", expect.anything());
    expect(markReadMutate).not.toHaveBeenCalled();
  });

  it("marks a parked row read again once it is re-opened", () => {
    // The guard is scoped to the row while it stays selected. Coming back to it
    // later is a fresh open and must behave like any other.
    reset();
    layout.width = DESKTOP;
    listData.active = [
      item({ id: "inbox-a", issue_id: "issue-a", read: false }),
      item({ id: "inbox-b", issue_id: "issue-b", read: false }),
    ];

    render(<InboxPage />);
    const [rowA, rowB] = screen.getAllByTestId("row");
    fireEvent.click(rowA!);
    act(() => rowActions?.onMarkUnread("inbox-a"));

    fireEvent.click(rowB!);
    markReadMutate.mockClear();
    fireEvent.click(rowA!);

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

  it.each([TABLET, FOLD_INNER_LANDSCAPE])("keeps both panes at %spx", (width) => {
    // 768px is the first width that keeps the two-pane layout. The nav
    // auto-collapses there instead (see the sidebar), so the list has to stay
    // on screen next to an open item — on a foldable's inner screen held
    // sideways as much as on a tablet.
    reset();
    layout.width = width;
    listData.active = [item({ id: "inbox-a", issue_id: "issue-a" })];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));

    expect(screen.queryByTestId("list")).not.toBeNull();
  });

  function renderWithActiveItem() {
    reset();
    layout.width = DESKTOP;
    listData.active = [item({ id: "inbox-a", issue_id: "issue-a" })];
    return render(<InboxPage />);
  }

  function renderWithOpenItem() {
    const view = renderWithActiveItem();
    fireEvent.click(screen.getByTestId("row"));
    return view;
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
      searchParams = new URLSearchParams("view=archived");
      listData.archived = [
        item({ id: "archived-1", issue_id: "issue-9", archived: true }),
      ];

      render(<InboxPage />);
      fireEvent.click(screen.getByTestId("row"));
      pressArchiveKey();

      expect(unarchiveMutate).toHaveBeenCalledWith("archived-1", expect.anything());
      expect(archiveMutate).not.toHaveBeenCalled();
    });

    it("leaves the selection on the next notification", () => {
      reset();
      layout.width = DESKTOP;
      listData.active = [
        item({ id: "inbox-a", issue_id: "issue-a" }),
        item({ id: "inbox-b", issue_id: "issue-b" }),
      ];

      render(<InboxPage />);
      fireEvent.click(screen.getAllByTestId("row")[0]!);
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

    function settle(
      spy: typeof archiveMutate,
      outcome: "success" | "error",
      err: unknown = new Error("boom"),
    ) {
      const options = spy.mock.calls.at(-1)?.[1] as MutateOptions | undefined;
      act(() => {
        if (outcome === "success") options?.onSuccess?.();
        else options?.onError?.(err);
      });
    }

    it("confirms an archive from the row action", () => {
      renderWithActiveItem();
      act(() => rowActions?.onAction("inbox-a"));
      settle(archiveMutate, "success");

      expect(toast.success).toHaveBeenCalledTimes(1);
    });

    it("confirms an archive driven by the shortcut", () => {
      renderWithOpenItem();
      fireEvent.keyDown(document, { key: "e" });
      settle(archiveMutate, "success");

      expect(toast.success).toHaveBeenCalledTimes(1);
    });

    it("confirms a restore", () => {
      reset();
      layout.width = DESKTOP;
      searchParams = new URLSearchParams("view=archived");
      listData.archived = [
        item({ id: "archived-1", issue_id: "issue-9", archived: true }),
      ];

      render(<InboxPage />);
      act(() => rowActions?.onAction("archived-1"));
      settle(unarchiveMutate, "success");

      expect(toast.success).toHaveBeenCalledTimes(1);
    });

    it("reports a failed archive as a failure only", () => {
      renderWithActiveItem();
      act(() => rowActions?.onAction("inbox-a"));
      settle(archiveMutate, "error");

      expect(toast.error).toHaveBeenCalledTimes(1);
      expect(toast.success).not.toHaveBeenCalled();
    });
  });

  // A run the viewer started themselves: the only kind "Working" lists.
  function ownRun(issueId: string, startedBy = "member-1"): AgentTask {
    return {
      id: `task-${issueId}`,
      agent_id: "agent-1",
      issue_id: issueId,
      status: "running",
      created_at: "2026-06-15T08:00:00Z",
      started_at: "2026-06-15T08:01:00Z",
      attribution: { source: "direct_human", precise: true, originator: { id: startedBy } },
    } as AgentTask;
  }

  const listed = () =>
    screen.getAllByTestId("row").map((row) => `${row.dataset.section}:${row.textContent}`);

  describe("sections", () => {
    it("files each issue under the section its notifications put it in", () => {
      reset();
      layout.width = DESKTOP;
      listData.active = [
        item({ id: "update", issue_id: "issue-u", section: "updates", read: false }),
        item({ id: "needs", issue_id: "issue-n", section: "needs_you" }),
        item({
          id: "snoozed",
          issue_id: "issue-s",
          section: "needs_you",
          snoozed_until: new Date(Date.now() + 3_600_000).toISOString(),
        }),
      ];

      render(<InboxPage />);

      expect(listed()).toEqual(["needs_you:needs", "updates:update", "snoozed:snoozed"]);
    });

    it("lists the issues the viewer handed to an agent, which have no notification", () => {
      reset();
      layout.width = DESKTOP;
      listData.tasks = [ownRun("issue-w"), ownRun("issue-other", "member-2")];

      render(<InboxPage />);

      expect(screen.getAllByTestId("working-row").map((row) => row.textContent)).toEqual(["issue-w"]);
    });

    it("narrows Working by the issue itself, once the issue has loaded", () => {
      reset();
      layout.width = DESKTOP;
      listData.active = [item({ id: "inbox-a", issue_id: "issue-a", issue_priority: "high" })];
      listData.tasks = [ownRun("issue-high"), ownRun("issue-low"), ownRun("issue-loading")];
      listData.issues = {
        "issue-high": { id: "issue-high", status: "in_progress", priority: "high", labels: [] },
        "issue-low": { id: "issue-low", status: "in_progress", priority: "low", labels: [] },
      };
      useInboxFilterStore.getState().togglePriorityFilter("workspace-1", "high");

      render(<InboxPage />);

      expect(listed()).toEqual(["needs_you:inbox-a"]);
      expect(screen.getAllByTestId("working-row").map((row) => row.textContent)).toEqual(["issue-high"]);
    });

    it("keeps narrowing Working once no notification is left", () => {
      reset();
      layout.width = DESKTOP;
      listData.tasks = [ownRun("issue-a"), ownRun("issue-b")];
      listData.issues = {
        "issue-a": { id: "issue-a", status: "in_progress", project_id: "project-a", labels: [] },
        "issue-b": { id: "issue-b", status: "in_progress", project_id: "project-b", labels: [] },
      };
      useInboxFilterStore.getState().toggleFilterValue("workspace-1", "project", "project-a");

      render(<InboxPage />);

      expect(screen.getAllByTestId("working-row").map((row) => row.textContent)).toEqual(["issue-a"]);
    });

    it("does not call a link to a Working issue broken while its issue loads under a filter", () => {
      reset();
      layout.width = DESKTOP;
      searchParams = new URLSearchParams("issue=issue-w");
      listData.tasks = [ownRun("issue-w")];
      useInboxFilterStore.getState().togglePriorityFilter("workspace-1", "high");

      const { rerender } = render(<InboxPage />);
      expect(replace).not.toHaveBeenCalled();

      listData.issues = { "issue-w": { id: "issue-w", status: "in_progress", priority: "high", labels: [] } };
      rerender(<InboxPage />);
      expect(issueDetailProps.at(-1)).toMatchObject({ issueId: "issue-w" });
      expect(replace).not.toHaveBeenCalled();
    });
  });

  describe("updates", () => {
    const update = (id: string, overrides: Partial<InboxItem> = {}) =>
      item({ id, issue_id: `issue-${id}`, section: "updates", read: false, ...overrides });

    it("lists the unread ones and sends the read ones to the archive", () => {
      reset();
      layout.width = DESKTOP;
      listData.active = [
        update("new"),
        update("seen", { read: true }),
        item({ id: "needs", issue_id: "issue-n", section: "needs_you", read: true }),
      ];

      const { rerender } = render(<InboxPage />);

      expect(listed()).toEqual(["needs_you:needs", "updates:new"]);
      expect(archiveUpdatesMutate).toHaveBeenCalledTimes(1);
      expect(archiveUpdatesMutate).toHaveBeenCalledWith({ groups: ["issue-seen"], readOnly: true });

      // The same set is asked for once, however often the page renders.
      rerender(<InboxPage />);
      expect(archiveUpdatesMutate).toHaveBeenCalledTimes(1);
    });

    it("keeps an update listed while it is open, and lets it go when the user moves on", () => {
      reset();
      layout.width = DESKTOP;
      listData.active = [update("a"), update("b")];
      markReadMutate.mockImplementation((id: string) => {
        listData.active = listData.active.map((current) =>
          current.id === id ? { ...current, read: true } : current,
        );
      });

      const { rerender } = render(<InboxPage />);
      fireEvent.click(screen.getByText("a"));
      rerender(<InboxPage />);

      expect(listed()).toEqual(["updates:a", "updates:b"]);
      expect(archiveUpdatesMutate).not.toHaveBeenCalled();

      fireEvent.click(screen.getByText("b"));
      rerender(<InboxPage />);

      expect(listed()).toEqual(["updates:b"]);
      expect(archiveUpdatesMutate).toHaveBeenCalledWith({ groups: ["issue-a"], readOnly: true });
    });

    it("leaves the archive alone while the archive is what is open", () => {
      reset();
      searchParams = new URLSearchParams("view=archived");
      listData.active = [update("seen", { read: true })];

      render(<InboxPage />);

      expect(archiveUpdatesMutate).not.toHaveBeenCalled();
    });

    it("clears the listed updates, read or not, and closes the one that was open", () => {
      reset();
      layout.width = DESKTOP;
      listData.active = [
        update("a"),
        update("b"),
        item({ id: "needs", issue_id: "issue-n", section: "needs_you" }),
      ];
      render(<InboxPage />);
      fireEvent.click(screen.getByText("a"));
      replace.mockClear();

      fireEvent.click(screen.getByTestId("clear-updates"));

      expect(archiveUpdatesMutate).toHaveBeenLastCalledWith(
        { groups: ["issue-a", "issue-b"], readOnly: false },
        expect.anything(),
      );
      expect(replace).toHaveBeenCalledWith("/acme/inbox");
      expect(archiveMutate).not.toHaveBeenCalled();
    });

    it("clears only what a filter shows", () => {
      reset();
      layout.width = DESKTOP;
      listData.active = [
        update("high", { issue_priority: "high" }),
        update("low", { issue_priority: "low" }),
      ];
      useInboxFilterStore.getState().togglePriorityFilter("workspace-1", "high");
      render(<InboxPage />);

      fireEvent.click(screen.getByTestId("clear-updates"));

      expect(archiveUpdatesMutate).toHaveBeenLastCalledWith(
        { groups: ["issue-high"], readOnly: false },
        expect.anything(),
      );
    });

    it("reports a failed clear", () => {
      reset();
      listData.active = [update("a")];
      render(<InboxPage />);
      fireEvent.click(screen.getByTestId("clear-updates"));

      const options = archiveUpdatesMutate.mock.calls.at(-1)?.[1] as { onError: (err: unknown) => void };
      act(() => options.onError(new Error("boom")));

      expect(toast.error).toHaveBeenCalledWith("boom");
    });
  });

  describe("an issue open without a notification", () => {
    it("opens a Working row in the detail pane, with nothing to snooze or archive", () => {
      reset();
      layout.width = DESKTOP;
      listData.tasks = [ownRun("issue-w")];

      render(<InboxPage />);
      fireEvent.click(screen.getByTestId("working-row"));

      expect(replace).toHaveBeenCalledWith("/acme/inbox?issue=issue-w");
      expect(issueDetailProps.at(-1)).toMatchObject({ issueId: "issue-w", readsInbox: false });
      expect(issueDetailProps.at(-1)?.onDone).toBeUndefined();
      expect(screen.queryByTestId("detail-actions")).toBeNull();
      expect(markReadMutate).not.toHaveBeenCalled();
      expect(replace).not.toHaveBeenCalledWith("/acme/issues/issue-w");
    });

    it("opens a link to a Working issue instead of leaving for its page", () => {
      reset();
      layout.width = DESKTOP;
      searchParams = new URLSearchParams("issue=issue-w");
      listData.tasks = [ownRun("issue-w")];

      render(<InboxPage />);

      expect(issueDetailProps.at(-1)).toMatchObject({ issueId: "issue-w" });
      expect(replace).not.toHaveBeenCalled();
    });

    it("waits for the runs to load before calling such a link broken", () => {
      reset();
      layout.width = DESKTOP;
      searchParams = new URLSearchParams("issue=issue-w");
      listData.tasksPending = true;

      const { rerender } = render(<InboxPage />);
      expect(replace).not.toHaveBeenCalled();

      listData.tasksPending = false;
      rerender(<InboxPage />);
      expect(replace).toHaveBeenCalledWith("/acme/issues/issue-w");
    });

    it("keeps the issue open when replying hands it to an agent and its notification goes", () => {
      reset();
      layout.width = DESKTOP;
      listData.active = [item({ id: "inbox-a", issue_id: "issue-a" })];

      const { rerender } = render(<InboxPage />);
      fireEvent.click(screen.getByTestId("row"));
      expect(screen.getByTestId("detail-actions")).toBeInTheDocument();
      replace.mockClear();
      issueDetailProps.length = 0;

      // The server archives the viewer's notifications as the run starts.
      listData.active = [];
      listData.tasks = [ownRun("issue-a")];
      rerender(<InboxPage />);

      expect(issueDetailProps.at(-1)).toMatchObject({ issueId: "issue-a" });
      expect(screen.queryByTestId("detail-actions")).toBeNull();
      expect(replace).not.toHaveBeenCalled();
    });

    it("keeps it open even when no run shows up for it", () => {
      reset();
      layout.width = DESKTOP;
      listData.active = [item({ id: "inbox-a", issue_id: "issue-a" })];

      const { rerender } = render(<InboxPage />);
      fireEvent.click(screen.getByTestId("row"));
      replace.mockClear();
      issueDetailProps.length = 0;
      listData.active = [];
      rerender(<InboxPage />);

      expect(issueDetailProps.at(-1)).toMatchObject({ issueId: "issue-a" });
      expect(replace).not.toHaveBeenCalled();
    });

    it("lets go once the selection moves on", () => {
      reset();
      layout.width = DESKTOP;
      listData.active = [
        item({ id: "inbox-a", issue_id: "issue-a" }),
        item({ id: "inbox-b", issue_id: "issue-b" }),
      ];

      const { rerender } = render(<InboxPage />);
      fireEvent.click(screen.getAllByTestId("row")[0]!);
      listData.active = [item({ id: "inbox-b", issue_id: "issue-b" })];
      rerender(<InboxPage />);
      fireEvent.click(screen.getByTestId("row"));
      expect(issueDetailProps.at(-1)).toMatchObject({ issueId: "issue-b" });
      replace.mockClear();

      // Back on the first issue by URL: it is a plain unknown link again.
      searchParams = new URLSearchParams("issue=issue-a");
      rerender(<InboxPage />);

      expect(replace).toHaveBeenCalledWith("/acme/issues/issue-a");
    });
  });

  describe("snooze", () => {
    type MutateOptions = { onSuccess?: () => void; onError?: (err: unknown) => void };
    const until = new Date("2026-06-18T01:00:00Z");

    function renderTwoWaiting() {
      reset();
      layout.width = DESKTOP;
      listData.active = [
        item({ id: "inbox-a", issue_id: "issue-a", section: "needs_you" }),
        item({ id: "inbox-b", issue_id: "issue-b", section: "needs_you" }),
        item({ id: "inbox-u", issue_id: "issue-u", section: "updates", read: false }),
      ];
      const view = render(<InboxPage />);
      fireEvent.click(screen.getAllByTestId("row")[0]!);
      replace.mockClear();
      return view;
    }

    it("snoozes from the row menu and moves on to the next issue that waits", () => {
      renderTwoWaiting();
      act(() => rowActions?.onSnooze("inbox-a", until));

      expect(snoozeMutate).toHaveBeenCalledWith(
        { id: "inbox-a", until: "2026-06-18T01:00:00.000Z" },
        expect.anything(),
      );
      expect(replace).toHaveBeenCalledWith("/acme/inbox?issue=issue-b");
    });

    it("stays in the section it finished rather than dropping into the updates below", () => {
      renderTwoWaiting();
      fireEvent.click(screen.getAllByTestId("row")[1]!);
      replace.mockClear();
      act(() => rowActions?.onAction("inbox-b"));

      expect(replace).toHaveBeenCalledWith("/acme/inbox?issue=issue-a");
      expect(replace).not.toHaveBeenCalledWith("/acme/inbox?issue=issue-u");
    });

    it("says when the issue comes back, since its row has left the screen", () => {
      renderTwoWaiting();
      act(() => rowActions?.onSnooze("inbox-a", until));
      act(() => (snoozeMutate.mock.calls.at(-1)?.[1] as MutateOptions).onSuccess?.());

      expect(toast.success).toHaveBeenCalledTimes(1);
    });

    it("reports a failed snooze as a failure only", () => {
      renderTwoWaiting();
      act(() => rowActions?.onSnooze("inbox-a", until));
      act(() => (snoozeMutate.mock.calls.at(-1)?.[1] as MutateOptions).onError?.(new Error("boom")));

      expect(toast.error).toHaveBeenCalledWith("boom");
      expect(toast.success).not.toHaveBeenCalled();
    });

    it("snoozes and archives the open notification from the detail header", () => {
      renderTwoWaiting();

      fireEvent.click(screen.getByTestId("detail-snooze"));
      expect(snoozeMutate).toHaveBeenCalledWith(
        { id: "inbox-a", until: SNOOZE_UNTIL.toISOString() },
        expect.anything(),
      );

      fireEvent.click(screen.getByTestId("detail-archive"));
      expect(archiveMutate).toHaveBeenCalledWith("inbox-b", expect.anything());
    });

    it("offers the way back on an issue that is snoozed, and takes it", () => {
      reset();
      layout.width = DESKTOP;
      listData.active = [
        item({
          id: "inbox-s",
          issue_id: "issue-s",
          section: "needs_you",
          snoozed_until: new Date(Date.now() + 3_600_000).toISOString(),
        }),
      ];

      render(<InboxPage />);
      fireEvent.click(screen.getByTestId("row"));
      expect(screen.getByTestId("detail-actions")).toHaveAttribute("data-snoozed", "true");

      fireEvent.click(screen.getByTestId("detail-unsnooze"));
      expect(unsnoozeMutate).toHaveBeenCalledWith("inbox-s", expect.anything());
      // Unsnoozing brings the issue back; it does not move the selection off it.
      expect(replace).not.toHaveBeenCalledWith("/acme/inbox");
    });

    it("opens the snooze menu for the open notification on its shortcut", () => {
      renderTwoWaiting();
      expect(screen.getByTestId("detail-actions")).toHaveAttribute("data-snooze-open", "false");

      fireEvent.keyDown(document, { key: "h" });

      expect(screen.getByTestId("detail-actions")).toHaveAttribute("data-snooze-open", "true");
      expect(snoozeMutate).not.toHaveBeenCalled();
    });

    it("closes the menu with the selection it was opened for", () => {
      renderTwoWaiting();
      fireEvent.keyDown(document, { key: "h" });

      fireEvent.click(screen.getAllByTestId("row")[1]!);

      expect(screen.getByTestId("detail-actions")).toHaveAttribute("data-snooze-open", "false");
    });

    it("has nothing to snooze in the archive or on a Working issue", () => {
      reset();
      layout.width = DESKTOP;
      searchParams = new URLSearchParams("view=archived");
      listData.archived = [item({ id: "archived-1", issue_id: "issue-9", archived: true })];
      const archive = render(<InboxPage />);
      fireEvent.click(screen.getByTestId("row"));
      fireEvent.keyDown(document, { key: "h" });
      expect(screen.queryByTestId("detail-actions")).toBeNull();
      archive.unmount();

      reset();
      layout.width = DESKTOP;
      listData.tasks = [ownRun("issue-w")];
      render(<InboxPage />);
      fireEvent.click(screen.getByTestId("working-row"));
      fireEvent.keyDown(document, { key: "h" });
      expect(screen.queryByTestId("detail-actions")).toBeNull();
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
