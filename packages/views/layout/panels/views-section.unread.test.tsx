import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { builtinSidebarSource, viewSidebarSource } from "@multica/core/issues/sidebar-list";
import { issueUnreadOptions } from "@multica/core/issues/unread";
import { issueViewContainerKey, useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { UNREAD_VIEW_ID } from "@multica/core/issue-views/unread-view";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { ViewsSection } from "./views-section";
import type { IssueViewActions } from "../../issue-views/use-view-actions";

const actions = vi.hoisted(() => ({
  sidebar: { items: [] as unknown[], reorder: vi.fn(), unstar: vi.fn() },
  create: vi.fn(),
  remove: vi.fn(),
  canManage: () => true,
}));

vi.mock("../../issue-views/use-view-actions", () => ({ useIssueViewActions: () => actions }));
vi.mock("../../issue-views/delete-view-dialog", () => ({ DeleteViewDialog: () => null }));
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
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

const UNREAD_VIEW = {
  id: "v1",
  name: "Mine unread",
  scope_type: "workspace",
  scope_id: null,
  scope_variant: null,
  query: { unreadFilter: true },
  display: {},
};

let listIssueUnread: ReturnType<typeof vi.fn>;
let listIssueTableRows: ReturnType<typeof vi.fn>;
let listIssueStatuses: ReturnType<typeof vi.fn>;

function renderSection({ activeIssueId = null }: { activeIssueId?: string | null } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const tree = (issueId: string | null) => (
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter}>
        <ViewsSection
          wsId="ws-1"
          pathname={issueId ? `/acme/issues/${issueId}` : "/acme/issues"}
          plainRowsLit
          issueLitInTree={false}
          activeIssueId={issueId}
          litSource={viewSidebarSource(UNREAD_VIEW_ID)}
          actions={actions as unknown as IssueViewActions}
        />
      </NavigationProvider>
    </QueryClientProvider>
  );
  const view = renderWithI18n(tree(activeIssueId));
  return { ...view, qc, openIssue: (issueId: string | null) => view.rerender(tree(issueId)) };
}

beforeEach(() => {
  listIssueUnread = vi.fn();
  listIssueTableRows = vi.fn(async () => ({
    rows: [],
    total: 0,
    next_cursor: null,
    query_fingerprint: "fp",
    group_key: null,
    parent_id: null,
  }));
  listIssueStatuses = vi.fn(async () => ({ statuses: [], categories: [], total: 0 }));
  setApiInstance({
    listIssueUnread,
    listIssueTableRows,
    listIssueStatuses,
    listIssueViews: vi.fn(async () => []),
    getIssueViewPreference: vi.fn(() => new Promise(() => {})),
  } as unknown as ApiClient);
  useActiveIssueViewStore.setState({ active: {} });
  actions.sidebar.unstar.mockReset();
  actions.sidebar.items = [{ id: "view:v1", kind: "view", view: UNREAD_VIEW }];
  useSidebarTreeStore.setState({ viewsCollapsed: false, expanded: [viewSidebarSource("v1")] });
});

describe("ViewsSection saved view with the unread filter", () => {
  it("holds its placeholder while the unread list loads, then lists the view's issues", async () => {
    let finish!: (marks: unknown[]) => void;
    listIssueUnread.mockImplementation(() => new Promise((resolve) => { finish = resolve; }));
    renderSection();

    await waitFor(() => expect(listIssueUnread).toHaveBeenCalled());
    expect(screen.queryByText("No issues")).toBeNull();
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    expect(listIssueTableRows).not.toHaveBeenCalled();

    finish([{ issue_id: "issue-1", outcome: "manual", agent_id: null, created_at: "2026-01-01T00:00:00Z" }]);
    await screen.findByText("No issues");
    expect(listIssueTableRows).toHaveBeenCalledWith(
      expect.objectContaining({
        query: expect.objectContaining({
          filters: expect.objectContaining({ unread_issue_ids: ["issue-1"] }),
        }),
      }),
    );
  });

  it("offers a retry when the unread list fails, then lists the view's issues", async () => {
    listIssueUnread.mockRejectedValueOnce(new Error("offline"));
    renderSection();

    const retry = await screen.findByRole("button", { name: "Retry" });
    expect(screen.getByText("Couldn't load issues")).toBeInTheDocument();
    expect(listIssueTableRows).not.toHaveBeenCalled();

    listIssueUnread.mockResolvedValue([
      { issue_id: "issue-1", outcome: "manual", agent_id: null, created_at: "2026-01-01T00:00:00Z" },
    ]);
    fireEvent.click(retry);

    await screen.findByText("No issues");
    expect(screen.queryByRole("button", { name: "Retry" })).toBeNull();
    await waitFor(() =>
      expect(listIssueTableRows).toHaveBeenCalledWith(
        expect.objectContaining({
          query: expect.objectContaining({
            filters: expect.objectContaining({ unread_issue_ids: ["issue-1"] }),
          }),
        }),
      ),
    );
  });
});

const TODO_STATUS = {
  id: "s-todo",
  workspace_id: "ws-1",
  key: "todo",
  name: "Todo",
  description: "",
  category: "unstarted",
  color: "",
  is_system: true,
  position: 0,
  archived_at: null,
  created_at: "",
  updated_at: "",
};

const mark = (issueId: string) => ({
  issue_id: issueId,
  outcome: "completed",
  agent_id: null,
  created_at: "2026-01-01T00:00:00Z",
});

describe("ViewsSection built-in Unread view", () => {
  beforeEach(() => {
    actions.sidebar.items = [
      { id: "builtin:all_issues", kind: "builtin", key: "all_issues" },
      { id: "view:v1", kind: "view", view: UNREAD_VIEW },
    ];
    useSidebarTreeStore.setState({ viewsCollapsed: false, expanded: [] });
  });

  it("is not in the sidebar until starred, even with unread issues", async () => {
    listIssueUnread.mockResolvedValue([mark("a"), mark("b")]);
    renderSection();
    await waitFor(() => expect(listIssueUnread).toHaveBeenCalled());
    await screen.findByRole("link", { name: "All issues" });
    expect(screen.queryByRole("link", { name: /^Unread/ })).toBeNull();
  });

  describe("once starred", () => {
    beforeEach(() => {
      actions.sidebar.items = [...actions.sidebar.items, { id: "builtin:unread", kind: "builtin", key: "unread" }];
    });

    it("sits where it was starred, with the unread count", async () => {
      listIssueUnread.mockResolvedValue([mark("a"), mark("b")]);
      renderSection();
      const link = await screen.findByRole("link", { name: /^Unread/ });
      expect(link).toHaveAttribute("href", "/acme/issues?view=unread");
      await waitFor(() => expect(link.closest("li")).toHaveTextContent(/^Unread2/));
      const names = screen.getAllByRole("link").map((l) => l.textContent);
      expect(names.slice(0, 3)).toEqual(["All issues", "Mine unread", "Unread"]);
    });

    it("stays when the count reaches 0", async () => {
      listIssueUnread.mockResolvedValue([mark("a")]);
      const { qc } = renderSection({ activeIssueId: "a" });
      const link = await screen.findByRole("link", { name: /^Unread/ });
      await waitFor(() => expect(link.closest("li")).toHaveTextContent(/^Unread1/));

      qc.setQueryData(issueUnreadOptions("ws-1").queryKey, []);
      await waitFor(() => expect(link.closest("li")).toHaveTextContent(/^Unread$/));
      expect(screen.getByRole("link", { name: /^Unread/ })).toBe(link);
    });

    it("lights while its page is open", async () => {
      listIssueUnread.mockResolvedValue([]);
      useActiveIssueViewStore
        .getState()
        .setActive(issueViewContainerKey("ws-1", { scope_type: "workspace" }), UNREAD_VIEW_ID);
      renderSection();
      const link = await screen.findByRole("link", { name: /^Unread/ });
      expect(link).toHaveAttribute("aria-current", "page");
      expect(screen.getByRole("link", { name: /^All issues/ })).not.toHaveAttribute("aria-current");
    });

    it("is removed from the sidebar from its row menu", async () => {
      listIssueUnread.mockResolvedValue([]);
      renderSection();
      const link = await screen.findByRole("link", { name: /^Unread/ });

      fireEvent.contextMenu(link);
      fireEvent.click(await screen.findByRole("menuitem", { name: "Remove from sidebar" }));
      expect(actions.sidebar.unstar).toHaveBeenCalledWith("builtin:unread");
    });
  });
});

describe("ViewsSection built-in All issues view", () => {
  beforeEach(() => {
    listIssueUnread.mockResolvedValue([]);
    actions.sidebar.items = [{ id: "builtin:all_issues", kind: "builtin", key: "all_issues" }];
    useSidebarTreeStore.setState({ viewsCollapsed: false, expanded: [builtinSidebarSource("all_issues")] });
  });

  it("offers a retry when the status catalog fails, then lists its issues", async () => {
    listIssueStatuses.mockRejectedValueOnce(new Error("offline"));
    renderSection();

    const retry = await screen.findByRole("button", { name: "Retry" });
    expect(screen.getByText("Couldn't load issues")).toBeInTheDocument();
    expect(listIssueTableRows).not.toHaveBeenCalled();

    listIssueStatuses.mockResolvedValue({ statuses: [TODO_STATUS], categories: [], total: 1 });
    fireEvent.click(retry);

    await screen.findByText("No issues");
    expect(listIssueTableRows).toHaveBeenCalledWith(
      expect.objectContaining({ query: expect.objectContaining({ scope: { kind: "workspace" } }) }),
    );
  });
});
