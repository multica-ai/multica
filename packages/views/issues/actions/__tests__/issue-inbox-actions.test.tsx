import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { useInboxFilterStore } from "@multica/core/inbox/filter-store";
import { inboxListOptions } from "@multica/core/inbox/queries";
import { useInboxSelectionStore } from "@multica/core/inbox/selection-store";
import type { InboxItem } from "@multica/core/types";
import { renderWithI18n } from "../../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../../navigation";
import { IssueInboxActions } from "../issue-inbox-actions";

const state = vi.hoisted(() => ({
  markRead: vi.fn(),
  markUnread: vi.fn(),
  archive: vi.fn(),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws" }));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/inbox/mutations", () => ({
  useMarkInboxRead: () => ({ mutate: state.markRead }),
  useMarkInboxUnread: () => ({ mutate: state.markUnread }),
  useArchiveInbox: () => ({ mutate: state.archive }),
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

const ROWS = [
  item({ id: "n-1", issue_id: "issue-1", created_at: "2026-11-12T08:00:00Z" }),
  item({ id: "n-2", issue_id: "issue-2", created_at: "2026-11-11T08:00:00Z", details: { comment_id: "c-9" } }),
];

function setup({
  rows = ROWS,
  issueId = "issue-1",
  onMarkDone,
}: { rows?: InboxItem[]; issueId?: string; onMarkDone?: (onSaved: () => void) => void } = {}) {
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
  qc.setQueryData(agentTaskSnapshotOptions("ws").queryKey, []);
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

  it("toggles the read state of the issue's notification", () => {
    setup();
    fireEvent.click(screen.getByRole("button", { name: "Mark as unread" }));
    expect(state.markUnread).toHaveBeenCalledWith("n-1", expect.anything());
    cleanup();

    setup({ rows: [{ ...ROWS[0]!, read: false }, ROWS[1]!] });
    fireEvent.click(screen.getByRole("button", { name: "Mark as read" }));
    expect(state.markRead).toHaveBeenCalledWith("n-1", expect.anything());
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
});
