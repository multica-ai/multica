import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { toast } from "sonner";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { type InboxBarAction, noteInboxAutoArchive, noteInboxBarAction } from "@multica/core/inbox/auto-archive";
import { useInboxFilterStore } from "@multica/core/inbox/filter-store";
import { inboxListOptions, snoozedInboxOptions } from "@multica/core/inbox/queries";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { childIssuesOptions, issueDetailOptions, issueSubscribersOptions } from "@multica/core/issues/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { Agent, AgentTask, InboxItem, Issue, SnoozedInboxIssue } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../../navigation";
import { takeInboxUndo } from "../../../inbox/components/inbox-undo-toast";
import { IssueInboxBar } from "../issue-inbox-actions";

const state = vi.hoisted(() => ({
  archiveInbox: vi.fn(),
  unarchiveInbox: vi.fn(() => Promise.resolve()),
  unsubscribeArchive: vi.fn(),
  markRead: vi.fn(),
  markUnread: vi.fn(),
  snooze: vi.fn(() => Promise.resolve()),
  unsnooze: vi.fn(() => Promise.resolve()),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), dismiss: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/permissions", () => ({ useIsGuest: () => false }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: "member-1" } }),
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
// Archive runs through the real mutation, so the tests see React Query drop
// per-call callbacks when the next issue's page replaces the header.
vi.mock("@multica/core/inbox/mutations", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/inbox/mutations")>()),
  useUnsubscribeArchiveInbox: () => ({ mutate: state.unsubscribeArchive }),
  undoUnsubscribeArchiveInbox: vi.fn(),
  useMarkInboxRead: () => ({ mutate: state.markRead }),
  useMarkInboxUnread: () => ({ mutate: state.markUnread }),
  useSnoozeInbox: () => ({ mutateAsync: state.snooze }),
  useUnsnoozeInbox: () => ({ mutateAsync: state.unsnooze }),
  cachedInboxSnoozedUntil: () => null,
}));

function item(overrides: Partial<InboxItem> & { id: string }): InboxItem {
  return {
    workspace_id: "ws",
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
    read: true,
    archived: false,
    created_at: "2026-11-10T08:00:00Z",
    details: null,
    ...overrides,
  };
}

const SNOOZED_AT = "2026-11-12T09:00:00Z";

const ROWS = [
  item({ id: "n-1", issue_id: "issue-1", created_at: "2026-11-12T08:00:00Z" }),
  item({ id: "n-2", issue_id: "issue-2", created_at: "2026-11-11T08:00:00Z", details: { comment_id: "c-9" } }),
];

function setup({
  rows = ROWS,
  issueId = "issue-1",
  snoozed = [],
  theirs = false,
  issueActions,
}: {
  rows?: InboxItem[];
  issueId?: string;
  snoozed?: SnoozedInboxIssue[];
  /** The viewer follows the issue, and someone else owns it. */
  theirs?: boolean;
  issueActions?: ReactNode;
} = {}) {
  const nav: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/inbox",
    searchParams: new URLSearchParams({ issue: issueId }),
    hash: "",
    getShareableUrl: (path) => path,
  };
  setApiInstance({
    archiveInbox: state.archiveInbox,
    unarchiveInbox: state.unarchiveInbox,
    listInbox: async () => rows,
    listSnoozedInbox: async () => snoozed,
    getInboxUnreadSummary: async () => [],
    getAgentTaskSnapshot: async () => [],
    // The child list refetches on mount; unsubscribing acts directly once it is known.
    listChildIssues: async () => ({ issues: [] }),
    // The bar's reason line shows the actor's avatar.
    getBaseUrl: () => "",
  } as unknown as ApiClient);
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } },
  });
  qc.setQueryData(inboxListOptions("ws").queryKey, rows);
  qc.setQueryData(snoozedInboxOptions("ws").queryKey, snoozed);
  qc.setQueryData(agentTaskSnapshotOptions("ws").queryKey, []);
  if (theirs) {
    qc.setQueryData(issueDetailOptions("ws", issueId).queryKey, {
      id: issueId,
      assignee_type: "member",
      assignee_id: "member-other",
    } as Issue);
    qc.setQueryData(agentListOptions("ws").queryKey, []);
    qc.setQueryData(issueSubscribersOptions(issueId).queryKey, [
      { issue_id: issueId, user_type: "member", user_id: "member-1", reason: "commenter", created_at: SNOOZED_AT },
    ]);
    qc.setQueryData(childIssuesOptions("ws", issueId).queryKey, []);
  }
  const page = (id: string | null) => (
    <QueryClientProvider client={qc}>
      <NavigationProvider value={nav}>
        {id ? <IssueInboxBar issueId={id} issueActions={issueActions} /> : <span>Next issue</span>}
      </NavigationProvider>
    </QueryClientProvider>
  );
  const { rerender } = renderWithI18n(page(issueId));
  /** The route reaching another issue, which the same header then shows. */
  const arrive = (id: string) => rerender(page(id));
  /** The next page replacing this one, taking the header with it. */
  const leave = () => rerender(page(null));
  return Object.assign(nav, { arrive, leave, qc });
}

/** An archive request the test settles by hand, after the header is gone. */
function pendingArchive() {
  let settle!: { resolve: (item: InboxItem) => void; reject: (err: Error) => void };
  state.archiveInbox.mockImplementation(
    () => new Promise<InboxItem>((resolve, reject) => (settle = { resolve, reject })),
  );
  return {
    resolve: () => act(async () => settle.resolve(item({ id: "n-1", archived: true }))),
    reject: (err: Error) => act(async () => settle.reject(err)),
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  state.archiveInbox.mockImplementation(async (id: string) => item({ id, archived: true }));
  useInboxSelectionStore.setState({
    heldRows: {},
    leftRowKeyByWorkspace: {},
    lastClearedByWorkspace: {},
  });
  useInboxFilterStore.getState().clearFilters("ws");
});
afterEach(() => {
  cleanup();
  // Mocked toasts never close, so the last one's undo would answer Z in the next test.
  takeInboxUndo();
});

describe("IssueInboxBar", () => {
  it("keeps the last row on a later side-effect archive after snooze fails", async () => {
    state.snooze.mockRejectedValueOnce(new Error("snooze failed"));
    const nav = setup({ rows: [ROWS[0]!] });
    fireEvent.keyDown(document, { key: "h", code: "KeyH" });
    fireEvent.click(await screen.findByRole("menuitem", { name: /^1 hour/ }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("snooze failed"));
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox");
    expect(useInboxSelectionStore.getState().lastClearedByWorkspace.ws).toBeUndefined();

    // Still reading the issue (now still in Inbox), a later side-effect archive keeps it.
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, []));
    expect(await screen.findByText("Archived")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Undo archive" })).toBeInTheDocument();
  });

  it("keeps the last row on a later side-effect archive after undoing snooze", async () => {
    const nav = setup({ rows: [ROWS[0]!] });
    fireEvent.keyDown(document, { key: "h", code: "KeyH" });
    fireEvent.click(await screen.findByRole("menuitem", { name: /^1 hour/ }));
    await waitFor(() => expect(toast.success).toHaveBeenCalledOnce());
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, []));

    const options = vi.mocked(toast.success).mock.calls[0]![1] as unknown as { action: { onClick: () => void } };
    await act(async () => options.action.onClick());
    expect(state.unsnooze).toHaveBeenCalledWith({ id: "n-1", read: true });
    expect(useInboxSelectionStore.getState().lastClearedByWorkspace.ws).toBeUndefined();
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[0]!]));
    expect(await screen.findByRole("button", { name: "Archive" })).toBeInTheDocument();

    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, []));
    expect(await screen.findByText("Archived")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Undo archive" })).toBeInTheDocument();
  });

  it("stays on the open issue until the next notification's issue arrives", async () => {
    const nav = setup();
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    await waitFor(() => expect(state.archiveInbox).toHaveBeenLastCalledWith("n-1"));

    nav.arrive("issue-2");
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    await waitFor(() => expect(state.archiveInbox).toHaveBeenLastCalledWith("n-2"));
  });

  it("stays out once the issue has left the Inbox", () => {
    setup({ rows: [ROWS[1]!] });
    expect(screen.queryByRole("button", { name: "Archive" })).toBeNull();
  });

  it("offers the issue's actions before the notification's, without restating why it is here", () => {
    setup({ issueActions: <button type="button">Set status</button> });
    expect(screen.queryByText("Commented")).toBeNull();
    const issueAction = screen.getByRole("button", { name: "Set status" });
    const snooze = screen.getByRole("button", { name: "Snooze" });
    expect(issueAction.compareDocumentPosition(snooze) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it("leads with Archive as its one filled action, each named action with its shortcut beside it", () => {
    setup();
    const bar = document.querySelector<HTMLElement>("[data-inbox-bar]")!;
    expect(bar.querySelectorAll('[data-slot="button"][data-variant="default"]')).toHaveLength(1);
    const archive = screen.getByRole("button", { name: "Archive" });
    expect(archive).toHaveAttribute("data-variant", "default");
    expect(within(archive).getByText("E")).toBeInTheDocument();
    expect(within(screen.getByRole("button", { name: "Snooze" })).getByText("H")).toBeInTheDocument();
    // Archive ends the named actions; the rarer ones wait behind More, last.
    const buttons = within(bar).getAllByRole("button");
    expect(buttons.at(-2)).toBe(archive);
    expect(buttons.at(-1)).toHaveAccessibleName("More notification actions");
  });

  it("moves on with Next as the settled group's one filled action", async () => {
    const nav = setup();
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));
    const next = await screen.findByRole("button", { name: "Next" });
    expect(next).toHaveAttribute("data-variant", "default");
    const undo = screen.getByRole("button", { name: "Undo archive" });
    expect(undo).toHaveAttribute("data-variant", "ghost");
    expect(within(next).getByText("E")).toBeInTheDocument();
    expect(within(undo).getByText("Z")).toBeInTheDocument();
  });

  it("keeps the issue's actions once the notification has left the Inbox", async () => {
    const nav = setup({ issueActions: <button type="button">Set status</button> });
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));
    expect(await screen.findByText("Archived")).toBeInTheDocument();
    const issueAction = screen.getByRole("button", { name: "Set status" });
    const undo = screen.getByRole("button", { name: "Undo archive" });
    expect(issueAction.compareDocumentPosition(undo) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  describe("after an action from the bar archives the notification", () => {
    /** The bar's action, then the server's archive of it as the user's own. */
    const actFromBar = (nav: ReturnType<typeof setup>, action: InboxBarAction) => {
      noteInboxBarAction("issue-1", action);
      noteInboxAutoArchive({ issue_id: "issue-1", reason: "actor_acted" });
      act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));
    };

    it("names the status it set, keeps the issue's actions, and undoes the archive", async () => {
      const nav = setup({ issueActions: <button type="button">Set status</button> });
      actFromBar(nav, { type: "status", status: "in_progress" });
      expect(await screen.findByText("Changed to In Progress · Returns to Inbox on new activity")).toBeInTheDocument();
      expect(screen.getByRole("button", { name: "Set status" })).toBeInTheDocument();
      expect(nav.push).not.toHaveBeenCalled();

      await act(async () => fireEvent.click(screen.getByRole("button", { name: "Undo archive" })));
      expect(state.unarchiveInbox).toHaveBeenCalledWith("n-1");
    });

    it("names the new assignee", async () => {
      const nav = setup();
      nav.qc.setQueryData(agentListOptions("ws").queryKey, [{ id: "agent-nova", name: "Nova" } as Agent]);
      actFromBar(nav, { type: "assignee", assignee: { type: "agent", id: "agent-nova" } });
      expect(await screen.findByText("Reassigned to Nova · Returns to Inbox on new activity")).toBeInTheDocument();
    });

    it("names the quick action it ran", async () => {
      const nav = setup();
      actFromBar(nav, { type: "quick_action", name: "Code Review" });
      expect(await screen.findByText("Ran Code Review · Returns to Inbox on new activity")).toBeInTheDocument();
    });

    it("names the status for Done too, over the status alone", async () => {
      const nav = setup({ rows: [{ ...ROWS[0]!, issue_status: "done" }, ROWS[1]!] });
      actFromBar(nav, { type: "status", status: "done" });
      expect(await screen.findByText("Changed to Done · Returns to Inbox on new activity")).toBeInTheDocument();
    });

    it("claims nothing when the archive came from elsewhere", async () => {
      const nav = setup();
      noteInboxBarAction("issue-1", { type: "status", status: "in_progress" });
      act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));
      expect(await screen.findByText("Archived")).toBeInTheDocument();
    });
  });

  it("keeps the mark unread and read toggle behind More, beside snooze", async () => {
    state.markRead.mockClear();
    state.markUnread.mockClear();
    useInboxSelectionStore.setState({ manualUnreadIdByWorkspace: {} });
    setup();
    expect(screen.getByRole("button", { name: "Snooze" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Mark as unread" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "More notification actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /^Mark as unread/ }));
    expect(state.markUnread).toHaveBeenCalledWith("n-1", expect.any(Object));
    expect(useInboxSelectionStore.getState().manualUnreadIdByWorkspace.ws).toBe("n-1");
    cleanup();

    setup({ rows: [{ ...ROWS[0]!, read: false }, ROWS[1]!] });
    fireEvent.click(screen.getByRole("button", { name: "More notification actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: /^Mark as read/ }));
    expect(state.markRead).toHaveBeenCalledWith("n-1", expect.any(Object));
    expect(useInboxSelectionStore.getState().manualUnreadIdByWorkspace.ws).toBeUndefined();
  });

  it("snoozes and moves on to the next notification", async () => {
    const nav = setup();
    fireEvent.keyDown(document, { key: "h", code: "KeyH" });
    fireEvent.click(await screen.findByRole("menuitem", { name: /^1 hour/ }));
    expect(state.snooze).toHaveBeenCalledWith({ id: "n-1", until: expect.any(Date) });
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=issue-2");
  });

  it("shows when a snoozed issue comes back, and brings it back with the shortcut", () => {
    const until = new Date(Date.now() + 60 * 60 * 1000).toISOString();
    setup({
      rows: [ROWS[1]!],
      snoozed: [{ issue_id: "issue-1", item_id: "n-1", snoozed_until: until, snoozed_at: SNOOZED_AT, updates: 0 }],
    });
    expect(screen.getByRole("status")).toHaveTextContent(/^Back /);
    expect(screen.queryByRole("button", { name: "Archive" })).toBeNull();

    fireEvent.keyDown(document, { key: "h", code: "KeyH" });
    expect(state.unsnooze).toHaveBeenCalledWith({ id: "n-1" });
  });

  it("keeps when a snoozed issue comes back in the tooltip, for a group too narrow to show it", async () => {
    const until = new Date(Date.now() + 60 * 60 * 1000).toISOString();
    setup({
      rows: [ROWS[1]!],
      snoozed: [{ issue_id: "issue-1", item_id: "n-1", snoozed_until: until, snoozed_at: SNOOZED_AT, updates: 0 }],
    });
    const label = within(screen.getByRole("status")).getByText(/^Back /);
    await act(async () => {
      fireEvent.pointerEnter(label.parentElement!, { pointerType: "mouse" });
      fireEvent.mouseEnter(label.parentElement!);
      fireEvent.mouseMove(label.parentElement!);
    });
    await waitFor(() =>
      expect(document.querySelector("[data-slot=tooltip-content]")).toHaveTextContent(label.textContent!),
    );
  });

  it("stays in place once the notification leaves the Inbox some other way, with undo and the next one", async () => {
    const nav = setup();
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));

    expect(await screen.findByText("Archived")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Archive" })).toBeNull();
    expect(nav.push).not.toHaveBeenCalled();

    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Undo archive" })));
    expect(state.unarchiveInbox).toHaveBeenCalledWith("n-1");
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("says when an issue another window snoozed comes back, and brings it back with the shortcut", async () => {
    const nav = setup();
    const until = new Date(Date.now() + 60 * 60 * 1000).toISOString();
    act(() => {
      nav.qc.setQueryData(snoozedInboxOptions("ws").queryKey, [
        { issue_id: "issue-1", item_id: "n-1", snoozed_until: until, snoozed_at: SNOOZED_AT, updates: 0 },
      ]);
      nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]);
    });

    expect(await screen.findByRole("status")).toHaveTextContent(/^Back /);
    fireEvent.keyDown(document, { key: "h", code: "KeyH" });
    expect(state.unsnooze).toHaveBeenCalledWith(expect.objectContaining({ id: "n-1" }));
    expect(state.unarchiveInbox).not.toHaveBeenCalled();
  });

  it("undoes the settled archive with Z", async () => {
    const nav = setup();
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));
    expect(await screen.findByText("Archived")).toBeInTheDocument();

    await act(async () => fireEvent.keyDown(document, { key: "z", code: "KeyZ" }));
    expect(state.unarchiveInbox).toHaveBeenCalledWith("n-1");
  });

  it("names the status that archived it, and opens the next one", async () => {
    const nav = setup({ rows: [{ ...ROWS[0]!, issue_status: "done" }, ROWS[1]!] });
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));

    expect(await screen.findByText("Done · Returns to Inbox on new activity")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=issue-2");
  });

  it("moves on with E rather than archiving again", async () => {
    const nav = setup();
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));

    await screen.findByText("Archived");
    fireEvent.keyDown(document, { key: "e", code: "KeyE" });
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=issue-2");
    expect(state.archiveInbox).not.toHaveBeenCalled();
  });

  it("returns the row snoozing left once the issue is brought back early", () => {
    useInboxSelectionStore.setState({ leftRowKeyByWorkspace: { ws: "issue-1" } });
    const until = new Date(Date.now() + 60 * 60 * 1000).toISOString();
    setup({
      rows: [ROWS[1]!],
      snoozed: [{ issue_id: "issue-1", item_id: "n-1", snoozed_until: until, snoozed_at: SNOOZED_AT, updates: 0 }],
    });
    fireEvent.keyDown(document, { key: "h", code: "KeyH" });
    expect(state.unsnooze).toHaveBeenCalledWith({ id: "n-1" });
    expect(useInboxSelectionStore.getState().leftRowKeyByWorkspace.ws).toBeUndefined();
  });

  it("archives and moves on to the next notification", async () => {
    const nav = setup();
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=issue-2");
    await waitFor(() => expect(state.archiveInbox).toHaveBeenCalledWith("n-1"));
  });

  it("still confirms the archive, with an undo, after the next issue replaces this one", async () => {
    const archive = pendingArchive();
    const nav = setup();
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    await waitFor(() => expect(state.archiveInbox).toHaveBeenCalledWith("n-1"));
    nav.leave();
    await archive.resolve();

    expect(toast.success).toHaveBeenCalledWith(
      "Notification archived",
      expect.objectContaining({ duration: 10_000, action: { label: "Undo", onClick: expect.any(Function) } }),
    );
    expect(toast.error).not.toHaveBeenCalled();
    const undo = vi.mocked(toast.success).mock.calls.at(-1)?.[1] as unknown as { action: { onClick: () => void } };
    await act(async () => undo.action.onClick());
    expect(state.unarchiveInbox).toHaveBeenCalledWith("n-1");
  });

  it("still reports a failed archive after the next issue replaces this one", async () => {
    const archive = pendingArchive();
    const nav = setup();
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    await waitFor(() => expect(state.archiveInbox).toHaveBeenCalled());
    nav.leave();
    await archive.reject(new Error("Archive rejected"));

    expect(toast.error).toHaveBeenCalledWith("Archive rejected");
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("moves to Inbox Zero and records undo context when archiving the last notification", async () => {
    const nav = setup({ rows: [{ ...ROWS[0]!, issue_identifier: "MUL-9790" }] });
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox");
    expect(useInboxSelectionStore.getState().lastClearedByWorkspace.ws).toMatchObject({
      kind: "archived",
      itemId: "n-1",
      key: "issue-1",
      label: "MUL-9790",
    });
    await waitFor(() => expect(state.archiveInbox).toHaveBeenCalledWith("n-1"));
  });

  it("says only that the last row was archived when it auto-archives in place, and moves to Inbox Zero on E", async () => {
    const until = new Date(Date.now() + 2 * 60 * 60 * 1000).toISOString();
    const nav = setup({
      rows: [{ ...ROWS[0]!, issue_identifier: "MUL-9790" }],
      snoozed: [{ issue_id: "issue-9", item_id: "n-9", snoozed_until: until, snoozed_at: SNOOZED_AT, updates: 0 }],
    });
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, []));

    const bar = await screen.findByRole("status");
    expect(bar).toHaveTextContent("Archived");
    expect(bar).not.toHaveTextContent("Inbox cleared");
    expect(bar).not.toHaveTextContent("snoozed");
    expect(within(bar).getAllByRole("button").map((button) => button.getAttribute("aria-label"))).toEqual(["Undo archive"]);

    fireEvent.keyDown(document, { key: "e", code: "KeyE" });
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox");
    expect(useInboxSelectionStore.getState().lastClearedByWorkspace.ws).toMatchObject({
      kind: "archived",
      itemId: "n-1",
      label: "MUL-9790",
    });
  });

  it("moves on to the next notification the Work panel lists under its filters", () => {
    useInboxFilterStore.getState().toggleUnreadOnly("ws");
    const nav = setup({
      rows: [
        ROWS[0]!,
        ROWS[1]!,
        item({ id: "n-3", issue_id: "issue-3", read: false, created_at: "2026-11-10T08:00:00Z" }),
      ],
    });
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=issue-3");
  });

  it("leaves the issue's status to its own control", () => {
    setup();
    expect(screen.queryByRole("button", { name: "Mark as done" })).toBeNull();
  });

  it("archives with the Inbox shortcut", async () => {
    setup();
    fireEvent.keyDown(document, { key: "e", code: "KeyE" });
    await waitFor(() => expect(state.archiveInbox).toHaveBeenCalledWith("n-1"));
  });

  // Snooze, archive and unsubscribe share the bar; each key does only its own.
  async function setupTheirs() {
    const nav = setup({ theirs: true });
    // Unsubscribing acts directly (no scope to ask) once the More menu lists it as one item.
    fireEvent.click(screen.getByRole("button", { name: "More notification actions" }));
    await screen.findByRole("menuitem", { name: /^Unsubscribe & archive/ });
    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
    return nav;
  }

  it("unsubscribes and moves on with Shift+E on someone else's issue", async () => {
    const nav = await setupTheirs();
    fireEvent.keyDown(document, { key: "E", code: "KeyE", shiftKey: true });
    expect(state.unsubscribeArchive).toHaveBeenCalledWith({ id: "n-1", issueId: "issue-1", scope: "issue" });
    await act(async () => {});
    expect(state.archiveInbox).not.toHaveBeenCalled();
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=issue-2");
  });

  it("still only archives with E there", async () => {
    await setupTheirs();
    fireEvent.keyDown(document, { key: "e", code: "KeyE" });
    await waitFor(() => expect(state.archiveInbox).toHaveBeenCalledWith("n-1"));
    expect(state.unsubscribeArchive).not.toHaveBeenCalled();
  });

  it("still only snoozes with H there", async () => {
    await setupTheirs();
    fireEvent.keyDown(document, { key: "h", code: "KeyH" });
    expect(await screen.findByRole("menuitem", { name: /^1 hour/ })).toBeInTheDocument();
    expect(state.unsubscribeArchive).not.toHaveBeenCalled();
    expect(state.archiveInbox).not.toHaveBeenCalled();
  });

  it("offers no unsubscribe on an issue the viewer does not follow as a bystander", async () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "More notification actions" }));
    await screen.findByRole("menuitem", { name: /^Mark as unread/ });
    expect(screen.queryByRole("menuitem", { name: /Unsubscribe/ })).toBeNull();
  });

  it("names what its undo takes back, and says why in its tooltip rather than between the buttons", async () => {
    const nav = setup();
    noteInboxBarAction("issue-1", { type: "status", status: "in_review" });
    noteInboxAutoArchive({ issue_id: "issue-1", reason: "actor_acted" });
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));

    const undo = await screen.findByRole("button", { name: "Undo archive" });
    const bar = document.querySelector<HTMLElement>("[data-inbox-bar]")!;
    const visible = [...bar.querySelectorAll("span")].filter((el) => !el.className.includes("sr-only"));
    expect(visible.some((el) => el.textContent === "Archived")).toBe(false);

    await act(async () => {
      fireEvent.pointerEnter(undo, { pointerType: "mouse" });
      fireEvent.mouseEnter(undo);
      fireEvent.mouseMove(undo);
    });
    await waitFor(() =>
      expect(document.querySelector("[data-slot=tooltip-content]")).toHaveTextContent(
        "Undo archiveChanged to In Review · Returns to Inbox on new activity",
      ),
    );
  });

  it("ends a settled group with More, which only unsubscribes, leaving the notification as it is", async () => {
    const nav = setup({ theirs: true });
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));
    await screen.findByRole("button", { name: "Undo archive" });

    const bar = document.querySelector<HTMLElement>("[data-inbox-bar]")!;
    const more = within(bar).getAllByRole("button").at(-1)!;
    expect(more).toHaveAccessibleName("More notification actions");
    fireEvent.click(more);
    expect(await screen.findByRole("menuitem", { name: "Unsubscribe from this issue" })).toBeInTheDocument();
    expect(screen.queryByRole("menuitem", { name: /Mark as/ })).toBeNull();
    expect(screen.queryByRole("menuitem", { name: /archive/i })).toBeNull();
  });

  it("leaves More out of a settled group with nothing behind it", async () => {
    const nav = setup();
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));
    await screen.findByRole("button", { name: "Undo archive" });
    expect(screen.queryByRole("button", { name: "More notification actions" })).toBeNull();
  });

  it("opens the way to bring a snoozed issue back or change its time from when it comes back", async () => {
    const until = new Date(Date.now() + 60 * 60 * 1000).toISOString();
    setup({
      rows: [ROWS[1]!],
      snoozed: [{ issue_id: "issue-1", item_id: "n-1", snoozed_until: until, snoozed_at: SNOOZED_AT, updates: 0 }],
    });
    expect(screen.queryByRole("button", { name: "Change time" })).toBeNull();
    fireEvent.click(screen.getByRole("button", { name: /^Back / }));
    expect(await screen.findByText("Change time")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("menuitem", { name: /^Bring back now/ }));
    expect(state.unsnooze).toHaveBeenCalledWith({ id: "n-1" });
  });
});

describe("IssueInboxBar once an action is undone", () => {
  it("keeps the row again after archiving the last row and undoing", async () => {
    const original = ROWS[0]!;
    const rows = [original];
    state.archiveInbox.mockImplementation(async () => {
      rows.splice(0);
      return { ...original, archived: true };
    });
    state.unarchiveInbox.mockImplementation(async () => { rows.push(original); });
    const nav = setup({ rows });
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    await waitFor(() => expect(toast.success).toHaveBeenCalled());
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox");
    const undo = vi.mocked(toast.success).mock.calls.at(-1)?.[1] as unknown as { action: { onClick: () => void } };
    await act(async () => undo.action.onClick());
    await screen.findByRole("button", { name: "Archive" });
    // The user continues reading the same issue; a later status change archives it.
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, []));
    expect(await screen.findByText("Archived")).toBeInTheDocument();
  });

  it("keeps the group up while handing off to the next notification", async () => {
    const nav = setup();
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=issue-2");
    expect(useInboxSelectionStore.getState().transitionByWorkspace.ws).toMatchObject({
      targetKey: "issue-2",
      direction: "next",
    });

    // Simulate the archive mutation removing the row from the Inbox cache before
    // the next issue's route mounts: the group stays up instead of blinking out.
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));
    expect(document.querySelector("[data-inbox-bar]")).not.toBeNull();
  });

  it("stays on the issue in place when completing an issue auto-archives it", async () => {
    const nav = setup();
    noteInboxBarAction("issue-1", { type: "status", status: "done" });
    noteInboxAutoArchive({ issue_id: "issue-1", reason: "issue_status_terminal" });
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));

    expect(await screen.findByText("Changed to Done · Returns to Inbox on new activity")).toBeInTheDocument();
    expect(nav.push).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
    expect(document.querySelector("[data-inbox-auto-advance-bar]")).toBeNull();
  });

  it("stays on the issue, leaving live agent status to the header, when triggering an agent archives it", async () => {
    const nav = setup();
    act(() => {
      nav.qc.setQueryData(agentListOptions("ws").queryKey, [{ id: "agent-1", name: "Emacs" } as Agent]);
      nav.qc.setQueryData(agentTaskSnapshotOptions("ws").queryKey, [
        { id: "task-1", issue_id: "issue-1", agent_id: "agent-1", status: "running" } as AgentTask,
      ]);
    });
    noteInboxAutoArchive({ issue_id: "issue-1", reason: "actor_acted", trigger: "agent_triggered" });
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));

    const statusBar = await screen.findByRole("status");
    expect(statusBar).toHaveTextContent("Archived");
    expect(statusBar).toHaveTextContent("Agent triggered · Returns to Inbox on new activity");
    expect(statusBar).not.toHaveTextContent("Emacs is working");
    expect(screen.getByRole("button", { name: "Undo archive" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next" })).toBeInTheDocument();
    expect(nav.push).not.toHaveBeenCalled();

    fireEvent.keyDown(document, { key: "e", code: "KeyE" });
    expect(nav.push).toHaveBeenCalledWith("/acme/inbox?issue=issue-2");
  });

  it("shows fallback agent-triggered copy before the agent snapshot updates", async () => {
    const nav = setup();
    noteInboxAutoArchive({ issue_id: "issue-1", reason: "actor_acted", trigger: "agent_triggered" });
    act(() => nav.qc.setQueryData(inboxListOptions("ws").queryKey, [ROWS[1]!]));

    expect(await screen.findByText("Agent triggered · Returns to Inbox on new activity")).toBeInTheDocument();
    expect(nav.push).not.toHaveBeenCalled();
  });
});
