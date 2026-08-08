import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import type { InboxRow } from "@multica/core/inbox/rows";
import { InboxPage } from "./inbox-page";

vi.mock("react-resizable-panels", () => ({
  useDefaultLayout: () => ({ defaultLayout: undefined, onLayoutChanged: vi.fn() }),
}));

// The page reads one hook for both lists. Tests stock them independently, and
// `grouped` is what selects the v1 or v2 behaviour the page is exercising.
const listData: { active: InboxRow[]; archived: InboxRow[]; grouped: boolean } = {
  active: [],
  archived: [],
  grouped: false,
};

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: undefined, isLoading: false, isError: false }),
  queryOptions: (o: unknown) => o,
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

vi.mock("@multica/core/inbox/rows", () => ({
  useInboxRows: () => ({
    rows: listData.active,
    archivedRows: listData.archived,
    loading: false,
    archivedLoading: false,
    archivedError: false,
    grouped: listData.grouped,
  }),
  // The real implementation, duplicated here rather than imported: mocking a
  // module replaces all of it, and the page's jump-target behaviour is exactly
  // what several of these tests assert.
  inboxRowHighlightCommentId: (row: InboxRow | null | undefined) => {
    if (!row) return undefined;
    if (row.group) {
      return row.group.targetKind === "comment" && row.group.targetId
        ? row.group.targetId
        : undefined;
    }
    return row.details?.comment_id ?? undefined;
  },
}));

// Stable spies: the auto-mark-read effect keys on the mutate identity, so a
// fresh `vi.fn()` per render would make the effect's deps churn.
const markReadMutate = vi.fn();
const markUnreadMutate = vi.fn();
const archiveMutate = vi.fn();
const unarchiveMutate = vi.fn();
const batchMutate = vi.fn();

vi.mock("@multica/core/inbox/row-mutations", () => ({
  useInboxRowActions: () => ({
    markRead: { mutate: markReadMutate },
    markUnread: { mutate: markUnreadMutate },
    archive: { mutate: archiveMutate },
    unarchive: { mutate: unarchiveMutate },
    batch: { mutate: batchMutate },
  }),
}));

let lastHighlight: string | undefined;
vi.mock("../../issues/components", () => ({
  IssueDetail: ({ highlightCommentId }: { highlightCommentId?: string }) => {
    lastHighlight = highlightCommentId;
    return null;
  },
  StatusIcon: () => null,
}));

const replace = vi.fn();
let searchParams = new URLSearchParams();

vi.mock("../../navigation", () => ({
  useNavigation: () => ({ searchParams, replace }),
}));

// Drive the layout from a viewport width so a test can name the device it
// cares about instead of a boolean, and the two breakpoints stay in one place.
// Phone-width by default — one column keeps most assertions simple. Tests
// about actioning a row WHILE it is open need the two-panel desktop layout,
// since a single column swaps the list out for the detail on selection.
const PHONE = 390;
const FOLD_INNER = 851;
const TABLET = 1024;
const DESKTOP = 1440;
const layout = { width: PHONE };

// The auto-read gate reads document.visibilityState directly, so tests drive it
// through the real property rather than a module mock.
let visibility: DocumentVisibilityState = "visible";
Object.defineProperty(document, "visibilityState", {
  configurable: true,
  get: () => visibility,
});
vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => layout.width < 768,
  useIsCompact: () => layout.width < 1024,
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
    items: InboxRow[];
    view: string;
    onSelect: (item: InboxRow) => void;
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

function item(overrides: Partial<InboxRow> = {}): InboxRow {
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
  listData.grouped = false;
  searchParams = new URLSearchParams();
  replace.mockClear();
  markReadMutate.mockClear();
  markUnreadMutate.mockClear();
  archiveMutate.mockClear();
  unarchiveMutate.mockClear();
  batchMutate.mockClear();
  rowActions = null;
  lastHighlight = undefined;
  layout.width = PHONE;
  visibility = "visible";
}

describe("InboxPage", () => {
  it("keeps the title unread count static, and counts the rows on screen", () => {
    reset();
    listData.active = [
      item({ id: "a", issue_id: "issue-a", read: false }),
      item({ id: "b", issue_id: "issue-b", read: false }),
      item({ id: "c", issue_id: "issue-c", read: true }),
    ];
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

  // `InboxRow.issue_id` is nullable: a quick-create outcome is a notification,
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

    expect(markReadMutate).toHaveBeenCalledWith(
      expect.objectContaining({ id: "inbox-a" }),
      expect.anything(),
    );
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

    expect(markUnreadMutate).toHaveBeenCalledWith(
      expect.objectContaining({ id: "inbox-a" }),
      expect.anything(),
    );
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

    expect(markReadMutate).toHaveBeenCalledWith(
      expect.objectContaining({ id: "inbox-a" }),
      expect.anything(),
    );
  });

  it("folds to a single column on a folded inner screen", () => {
    // 851px — the reported Pixel Fold inner screen. Above the phone breakpoint
    // but far too narrow for nav + list + detail, so it takes the same single
    // column: opening a row replaces the list rather than sharing the width.
    reset();
    layout.width = FOLD_INNER;
    listData.active = [item({ id: "inbox-a", issue_id: "issue-a" })];

    render(<InboxPage />);
    expect(screen.queryByTestId("list")).not.toBeNull();

    fireEvent.click(screen.getByTestId("row"));

    expect(screen.queryByTestId("list")).toBeNull();
  });

  it("keeps both panes at the compact breakpoint", () => {
    // 1024px is the first width that keeps the two-pane layout. The nav
    // auto-collapses there instead (see the sidebar), so the list has to stay
    // on screen next to an open item.
    reset();
    layout.width = TABLET;
    listData.active = [item({ id: "inbox-a", issue_id: "issue-a" })];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));

    expect(screen.queryByTestId("list")).not.toBeNull();
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

// --- v2 group behaviour ----------------------------------------------------
//
// These three are the behavioural contracts the group model exists to deliver.
// They are asserted through the page, because each one is a rule about what the
// READER experiences, not about what a query returns.

function groupRow(overrides: Partial<InboxRow> = {}): InboxRow {
  return item({
    id: "group-a",
    issue_id: "issue-a",
    read: false,
    group: {
      seq: 5,
      stateVersion: 2,
      targetKind: "comment",
      targetId: "comment-5",
      sourceKind: "issue",
      sourceId: "issue-a",
    },
    ...overrides,
  });
}

describe("InboxPage, grouped", () => {
  it("jumps to the target the server resolved, not one dug out of details", () => {
    reset();
    listData.grouped = true;
    layout.width = DESKTOP;
    // `details.comment_id` disagrees on purpose: the v1 client read that field,
    // and reading it under v2 would send the user to the wrong comment.
    listData.active = [
      groupRow({ details: { comment_id: "stale-comment" } }),
    ];

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));

    expect(lastHighlight).toBe("comment-5");
  });

  it("does not move the reader when new events land on the open row", () => {
    reset();
    listData.grouped = true;
    layout.width = DESKTOP;
    listData.active = [groupRow()];

    const { rerender } = render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));
    expect(lastHighlight).toBe("comment-5");

    // Two newer events arrive for the SAME row while it is open.
    listData.active = [
      groupRow({ group: { seq: 7, stateVersion: 4, targetKind: "comment", targetId: "comment-7", sourceKind: "issue", sourceId: "issue-a" } }),
    ];
    rerender(<InboxPage />);

    // The highlight stays where the reader is.
    expect(lastHighlight).toBe("comment-5");
    // And the new events are offered instead.
    expect(screen.getByRole("button", { name: /Inbox/ })).toBeTruthy();
  });

  it("adopts the new events only when the reader asks", () => {
    reset();
    listData.grouped = true;
    layout.width = DESKTOP;
    listData.active = [groupRow()];

    const { rerender } = render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));

    listData.active = [
      groupRow({ group: { seq: 9, stateVersion: 5, targetKind: "comment", targetId: "comment-9", sourceKind: "issue", sourceId: "issue-a" } }),
    ];
    rerender(<InboxPage />);
    expect(lastHighlight).toBe("comment-5");

    // The offer is the only control that moves them.
    const offer = screen
      .getAllByRole("button")
      .find((b) => b.className.includes("bg-accent/40"));
    expect(offer).toBeTruthy();
    fireEvent.click(offer!);
    rerender(<InboxPage />);

    expect(lastHighlight).toBe("comment-9");
  });

  it("does not mark a row read while the tab is in the background", () => {
    reset();
    listData.grouped = true;
    layout.width = DESKTOP;
    listData.active = [groupRow()];
    visibility = "hidden";

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));

    // "Selected" is not "seen": a tab behind the window has a selected row and
    // would otherwise report it read without a human ever looking at it.
    expect(markReadMutate).not.toHaveBeenCalled();
  });

  it("marks it read as soon as the tab comes forward", () => {
    reset();
    listData.grouped = true;
    layout.width = DESKTOP;
    listData.active = [groupRow()];
    visibility = "hidden";

    render(<InboxPage />);
    fireEvent.click(screen.getByTestId("row"));
    expect(markReadMutate).not.toHaveBeenCalled();

    act(() => {
      visibility = "visible";
      document.dispatchEvent(new Event("visibilitychange"));
    });

    expect(markReadMutate).toHaveBeenCalledWith(
      expect.objectContaining({ id: "group-a" }),
      expect.anything(),
    );
  });

  it("routes writes with the group row, so they reach the group endpoints", () => {
    reset();
    listData.grouped = true;
    layout.width = DESKTOP;
    listData.active = [groupRow()];

    render(<InboxPage />);
    act(() => rowActions?.onAction("group-a"));

    expect(archiveMutate).toHaveBeenCalledWith(
      expect.objectContaining({ id: "group-a", group: expect.objectContaining({ seq: 5 }) }),
      expect.anything(),
    );
  });
});
