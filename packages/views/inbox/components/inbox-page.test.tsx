import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { InboxPage } from "./inbox-page";

vi.mock("react-resizable-panels", () => ({
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
}));

// The page runs two queries — the active list and the archived one. They are
// told apart by the queryKey their options carry, so each test can stock the
// two lists independently.
const listData: { active: InboxItem[]; archived: InboxItem[] } = {
  active: [],
  archived: [],
};

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey: readonly unknown[] }) => ({
    data: options.queryKey.includes("archived") ? listData.archived : listData.active,
    isLoading: false,
    isError: false,
  }),
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

vi.mock("@multica/core/modals", () => ({
  useModalStore: { getState: () => ({ open: vi.fn() }) },
}));

vi.mock("@multica/core/issues/stores/draft-store", () => ({
  useIssueDraftStore: { getState: () => ({ setDraft: vi.fn() }) },
}));

vi.mock("@multica/core/inbox/queries", () => ({
  inboxListOptions: () => ({ queryKey: ["inbox", "workspace-1", "list"] }),
  archivedInboxListOptions: () => ({ queryKey: ["inbox", "workspace-1", "archived"] }),
  deduplicateInboxItems: (items: InboxItem[]) => items.filter((i) => !i.archived),
  deduplicateArchivedInboxItems: (items: InboxItem[]) => items.filter((i) => i.archived),
  useInboxUnreadCount: () => 2,
}));

// Stable spies: the auto-mark-read effect keys on the mutate identity, so a
// fresh `vi.fn()` per render would make the effect's deps churn.
const markReadMutate = vi.fn();
const markUnreadMutate = vi.fn();

vi.mock("@multica/core/inbox/mutations", () => {
  const mutation = () => ({ mutate: vi.fn() });
  return {
    useMarkInboxRead: () => ({ mutate: markReadMutate }),
    useMarkInboxUnread: () => ({ mutate: markUnreadMutate }),
    useArchiveInbox: mutation,
    useUnarchiveInbox: mutation,
    useMarkAllInboxRead: mutation,
    useArchiveAllInbox: mutation,
    useArchiveAllReadInbox: mutation,
    useArchiveCompletedInbox: mutation,
  };
});

vi.mock("../../issues/components", () => ({
  IssueDetail: () => null,
  StatusIcon: () => null,
}));

const replace = vi.fn();
let searchParams = new URLSearchParams();

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ searchParams, replace }),
}));

// Mobile by default — it renders the list and the detail as one column, which
// keeps most assertions simple. Tests about actioning a row WHILE it is open
// need the two-panel desktop layout, since mobile swaps the list out for the
// detail on selection.
const layout = { isMobile: true };
vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => layout.isMobile,
}));
vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  ResizablePanel: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  ResizableHandle: () => null,
}));
vi.mock("./inbox-list", () => ({
  InboxList: ({
    items,
    view,
    onSelect,
  }: {
    items: InboxItem[];
    view: string;
    onSelect: (item: InboxItem) => void;
  }) => (
    <div data-testid="list" data-view={view}>
      {items.map((i) => (
        <button key={i.id} data-testid="row" onClick={() => onSelect(i)}>
          {i.id}
        </button>
      ))}
    </div>
  ),
}));
vi.mock("./inbox-list-item", () => ({ useTimeAgo: () => vi.fn() }));

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
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "Inbox" }) }));

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
    read: true,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    ...overrides,
  };
}

function reset() {
  listData.active = [];
  listData.archived = [];
  searchParams = new URLSearchParams();
  replace.mockClear();
  markReadMutate.mockClear();
  markUnreadMutate.mockClear();
  rowActions = null;
  layout.isMobile = true;
}

describe("InboxPage", () => {
  it("keeps the title unread count static", () => {
    reset();
    const { container } = render(<InboxPage />);
    const titleCount = container.querySelector("h1")?.parentElement?.querySelector(
      "number-flow-react",
    ) as (HTMLElement & { animated?: boolean }) | null;

    expect(titleCount?.getAttribute("aria-label")).toBe("2");
    expect(titleCount?.animated).toBe(false);
  });

  it("shows the active list by default", () => {
    reset();
    listData.active = [item({ id: "active-1" })];
    listData.archived = [item({ id: "archived-1", archived: true })];

    render(<InboxPage />);

    expect(screen.getByTestId("list").dataset.view).toBe("inbox");
    expect(screen.getByTestId("row").textContent).toBe("active-1");
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

  it("hides the batch-actions menu in the archived view", () => {
    // Every batch action archives from the MAIN inbox; offering them over the
    // archived list would read as "archive all of these" and do the opposite.
    reset();
    listData.archived = [item({ id: "archived-1", archived: true })];
    const { container: mainView } = render(<InboxPage />);
    expect(mainView.querySelector('[aria-haspopup="menu"]')).not.toBeNull();

    searchParams = new URLSearchParams("view=archived");
    const { container: archivedView } = render(<InboxPage />);
    expect(archivedView.querySelector('[aria-haspopup="menu"]')).toBeNull();
  });

  it("falls back to the main inbox when the archive drains", () => {
    // Restoring the last archived item must not strand the user on an empty
    // archive — same fallback chat's archived view has.
    reset();
    searchParams = new URLSearchParams("view=archived");
    listData.archived = [];

    render(<InboxPage />);

    expect(replace).toHaveBeenCalledWith("/acme/inbox");
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

  it("marks the opened notification read", () => {
    reset();
    listData.active = [
      item({ id: "inbox-a", issue_id: "issue-a", read: false }),
    ];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));

    expect(markReadMutate).toHaveBeenCalledWith("inbox-a", expect.anything());
  });

  it("keeps an explicitly unread row unread while it stays open", () => {
    // Without the guard the auto-read effect fires on the very next commit and
    // silently undoes the user's "mark as unread" — the action looks like a
    // no-op.
    reset();
    layout.isMobile = false;
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
    layout.isMobile = false;
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
