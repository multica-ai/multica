import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { useSidebarKeptStore } from "@multica/core/issues/stores/sidebar-kept-store";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { viewSidebarSource } from "@multica/core/issues/sidebar-list";
import { issueUnreadOptions } from "@multica/core/issues/unread";
import { issueViewContainerKey, useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { UNREAD_VIEW_ID } from "@multica/core/issue-views/unread-view";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { ViewsSection } from "./views-section";

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
vi.mock("@multica/core/issue-statuses/hooks", async () => {
  const { buildIssueStatusCatalog } = await import("@multica/core/issue-statuses/queries");
  return { useIssueStatuses: () => buildIssueStatusCatalog([]) };
});

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
  setApiInstance({
    listIssueUnread,
    listIssueTableRows,
    listIssueViews: vi.fn(async () => []),
    getIssueViewPreference: vi.fn(() => new Promise(() => {})),
  } as unknown as ApiClient);
  useActiveIssueViewStore.setState({ active: {} });
  useSidebarKeptStore.setState({ held: {} });
  actions.sidebar.items = [{ id: "view:v1", kind: "view", view: UNREAD_VIEW }];
  useSidebarTreeStore.setState({ viewsCollapsed: false, expanded: [viewSidebarSource("v1")] });
});

describe("ViewsSection saved view with the unread filter", () => {
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

  it("is hidden while nothing is unread", async () => {
    listIssueUnread.mockResolvedValue([]);
    renderSection();
    await waitFor(() => expect(listIssueUnread).toHaveBeenCalled());
    await screen.findByRole("link", { name: "All issues" });
    expect(screen.queryByRole("link", { name: /^Unread/ })).toBeNull();
  });

  it("appears right after All issues with the unread count", async () => {
    listIssueUnread.mockResolvedValue([mark("a"), mark("b")]);
    renderSection();
    const link = await screen.findByRole("link", { name: /^Unread/ });
    expect(link).toHaveAttribute("href", "/acme/issues?view=unread");
    expect(link.closest("li")).toHaveTextContent("Unread2");
    const names = screen.getAllByRole("link").map((l) => l.textContent);
    expect(names.slice(0, 3)).toEqual(["All issues", "Unread", "Mine unread"]);
  });

  it("stays while its page is open, even once everything is read", async () => {
    listIssueUnread.mockResolvedValue([]);
    useActiveIssueViewStore
      .getState()
      .setActive(issueViewContainerKey("ws-1", { scope_type: "workspace" }), UNREAD_VIEW_ID);
    renderSection();
    const link = await screen.findByRole("link", { name: /^Unread/ });
    expect(link).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: /^All issues/ })).not.toHaveAttribute("aria-current");
  });

  it("stays while the last unread issue is open and read, until another page opens", async () => {
    listIssueUnread.mockResolvedValue([mark("a")]);
    const { qc, openIssue } = renderSection({ activeIssueId: "a" });
    await screen.findByRole("link", { name: /^Unread/ });

    qc.setQueryData(issueUnreadOptions("ws-1").queryKey, []);
    await waitFor(() => expect(screen.getByRole("link", { name: /^Unread/ }).closest("li")).toHaveTextContent(/^Unread$/));

    openIssue(null);
    await waitFor(() => expect(screen.queryByRole("link", { name: /^Unread/ })).toBeNull());
  });

  it("does not show for an open issue that was already read", async () => {
    listIssueUnread.mockResolvedValue([]);
    renderSection({ activeIssueId: "a" });
    await waitFor(() => expect(listIssueUnread).toHaveBeenCalled());
    await screen.findByRole("link", { name: "All issues" });
    expect(screen.queryByRole("link", { name: /^Unread/ })).toBeNull();
  });
});
