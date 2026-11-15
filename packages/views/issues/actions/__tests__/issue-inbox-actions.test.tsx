import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { useInboxFilterStore } from "@multica/core/inbox/filter-store";
import { inboxListOptions, snoozedInboxOptions } from "@multica/core/inbox/queries";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import { childIssuesOptions, issueDetailOptions, issueSubscribersOptions } from "@multica/core/issues/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { InboxItem, Issue, SnoozedInboxIssue } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../../navigation";
import { IssueInboxActions } from "../issue-inbox-actions";

const state = vi.hoisted(() => ({
  archive: vi.fn(),
  unsubscribeArchive: vi.fn(),
  snooze: vi.fn(() => Promise.resolve()),
  unsnooze: vi.fn(() => Promise.resolve()),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/inbox/mutations", () => ({
  useArchiveInbox: () => ({ mutate: state.archive }),
  useUnsubscribeArchiveInbox: () => ({ mutate: state.unsubscribeArchive }),
  undoUnsubscribeArchiveInbox: vi.fn(),
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
  onMarkDone,
  snoozed = [],
  theirs = false,
}: {
  rows?: InboxItem[];
  issueId?: string;
  onMarkDone?: (onSaved: () => void) => void;
  snoozed?: SnoozedInboxIssue[];
  /** The viewer follows the issue, and someone else owns it. */
  theirs?: boolean;
} = {}) {
  const nav: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: `/acme/issues/${issueId}`,
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
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
    // The child list refetches on mount; the button acts directly once it is known.
    setApiInstance({ listChildIssues: async () => ({ issues: [] }) } as unknown as ApiClient);
  }
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={nav}>
        <IssueInboxActions issueId={issueId} onMarkDone={onMarkDone} />
      </NavigationProvider>
    </QueryClientProvider>,
  );
  return nav;
}

beforeEach(() => {
  vi.clearAllMocks();
  useInboxSelectionStore.setState({ sidebarIssueByWorkspace: { ws: "issue-1" } });
  useInboxFilterStore.getState().clearFilters("ws");
});
afterEach(cleanup);

describe("IssueInboxActions", () => {
  it("stays out of an issue that was not opened from the Inbox", () => {
    useInboxSelectionStore.setState({ sidebarIssueByWorkspace: { ws: "issue-2" } });
    setup();
    expect(screen.queryByRole("button", { name: "Archive" })).toBeNull();
  });

  it("stays out once the issue has left the Inbox", () => {
    setup({ rows: [ROWS[1]!] });
    expect(screen.queryByRole("button", { name: "Archive" })).toBeNull();
  });

  it("offers snooze in place of a read toggle", () => {
    setup();
    expect(screen.queryByRole("button", { name: "Mark as unread" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Mark as read" })).toBeNull();
    expect(screen.getByRole("button", { name: "Snooze" })).toBeInTheDocument();
  });

  it("snoozes and moves on to the next notification", async () => {
    const nav = setup();
    fireEvent.keyDown(document, { key: "h", code: "KeyH" });
    fireEvent.click(await screen.findByRole("menuitem", { name: /^1 hour/ }));
    expect(state.snooze).toHaveBeenCalledWith({ id: "n-1", until: expect.any(Date) });
    expect(nav.push).toHaveBeenCalledWith("/acme/issues/issue-2#comment-c-9");
  });

  it("shows when a snoozed issue comes back, and brings it back with the shortcut", () => {
    useInboxSelectionStore.setState({ sidebarIssueByWorkspace: {} });
    const until = new Date(Date.now() + 60 * 60 * 1000).toISOString();
    setup({
      rows: [ROWS[1]!],
      snoozed: [{ issue_id: "issue-1", item_id: "n-1", snoozed_until: until, snoozed_at: SNOOZED_AT, updates: 0 }],
    });
    expect(screen.getByTitle(/^Back /)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Archive" })).toBeNull();

    fireEvent.keyDown(document, { key: "h", code: "KeyH" });
    expect(state.unsnooze).toHaveBeenCalledWith({ id: "n-1" });
  });

  it("archives and moves on to the next notification", () => {
    const nav = setup();
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    expect(state.archive).toHaveBeenCalledWith("n-1", expect.anything());
    expect(nav.push).toHaveBeenCalledWith("/acme/issues/issue-2#comment-c-9");
    expect(useInboxSelectionStore.getState().sidebarIssueByWorkspace.ws).toBe("issue-2");
  });

  it("stays on the issue when it was the last notification", () => {
    const nav = setup({ rows: [ROWS[0]!] });
    fireEvent.click(screen.getByRole("button", { name: "Archive" }));
    expect(state.archive).toHaveBeenCalledWith("n-1", expect.anything());
    expect(nav.push).not.toHaveBeenCalled();
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
    expect(nav.push).toHaveBeenCalledWith("/acme/issues/issue-3");
  });

  it("marks the issue done, then archives and moves on once that is saved", () => {
    let saved: (() => void) | undefined;
    const onMarkDone = vi.fn((onSaved: () => void) => { saved = onSaved; });
    const nav = setup({ onMarkDone });

    fireEvent.click(screen.getByRole("button", { name: "Mark as done" }));
    expect(onMarkDone).toHaveBeenCalledTimes(1);
    expect(state.archive).not.toHaveBeenCalled();
    expect(nav.push).not.toHaveBeenCalled();

    saved!();
    expect(state.archive).toHaveBeenCalledWith("n-1", expect.anything());
    expect(nav.push).toHaveBeenCalledWith("/acme/issues/issue-2#comment-c-9");
  });

  it("offers no done button when the issue cannot be marked done", () => {
    setup();
    expect(screen.queryByRole("button", { name: "Mark as done" })).toBeNull();
  });

  it("archives with the Inbox shortcut", () => {
    setup();
    fireEvent.keyDown(document, { key: "e", code: "KeyE" });
    expect(state.archive).toHaveBeenCalledWith("n-1", expect.anything());
  });

  // Snooze, archive and unsubscribe share the header; each key does only its own.
  async function setupTheirs() {
    const nav = setup({ theirs: true });
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Unsubscribe & archive" })).not.toHaveAttribute("aria-haspopup"),
    );
    return nav;
  }

  it("unsubscribes and moves on with Shift+E on someone else's issue", async () => {
    const nav = await setupTheirs();
    fireEvent.keyDown(document, { key: "E", code: "KeyE", shiftKey: true });
    expect(state.unsubscribeArchive).toHaveBeenCalledWith({ id: "n-1", issueId: "issue-1", scope: "issue" });
    expect(state.archive).not.toHaveBeenCalled();
    expect(nav.push).toHaveBeenCalledWith("/acme/issues/issue-2#comment-c-9");
  });

  it("still only archives with E there", async () => {
    await setupTheirs();
    fireEvent.keyDown(document, { key: "e", code: "KeyE" });
    expect(state.archive).toHaveBeenCalledWith("n-1", expect.anything());
    expect(state.unsubscribeArchive).not.toHaveBeenCalled();
  });

  it("still only snoozes with H there", async () => {
    await setupTheirs();
    fireEvent.keyDown(document, { key: "h", code: "KeyH" });
    expect(await screen.findByRole("menuitem", { name: /^1 hour/ })).toBeInTheDocument();
    expect(state.unsubscribeArchive).not.toHaveBeenCalled();
    expect(state.archive).not.toHaveBeenCalled();
  });

  it("offers no unsubscribe on an issue the viewer does not follow as a bystander", () => {
    setup();
    expect(screen.queryByRole("button", { name: "Unsubscribe & archive" })).toBeNull();
  });
});
