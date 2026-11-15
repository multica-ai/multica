import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { builtinSidebarSource, viewSidebarSource } from "@multica/core/issues/sidebar-list";
import { issueViewContainerKey, useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
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
let getWorkspaceWorkingAgents: ReturnType<typeof vi.fn>;

function renderSection({ activeIssueId = null }: { activeIssueId?: string | null } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const tree = (issueId: string | null) => (
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter}>
        <ViewsSection
          wsId="ws-1"
          pathname={issueId ? `/acme/issues/${issueId}` : "/acme/issues"}
          plainRowsLit
          issueInAllIssues={false}
          activeIssueId={issueId}
          litSource={null}
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
  getWorkspaceWorkingAgents = vi.fn(async () => []);
  setApiInstance({
    listIssueUnread,
    listIssueTableRows,
    listIssueStatuses,
    getWorkspaceWorkingAgents,
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

describe("ViewsSection built-in filtered views", () => {
  const items = (...keys: string[]) => keys.map((key) => ({ id: `builtin:${key}`, kind: "builtin", key }));

  beforeEach(() => {
    listIssueUnread.mockResolvedValue([]);
    listIssueStatuses.mockResolvedValue({ statuses: [TODO_STATUS], categories: [], total: 1 });
    actions.sidebar.items = items("all_issues", "my_work", "working", "subscribed");
    useSidebarTreeStore.setState({ viewsCollapsed: false, expanded: [] });
  });

  it("opens All issues under each built-in's filter", async () => {
    renderSection();
    await screen.findByRole("link", { name: "All issues" });
    const href = (name: string) => screen.getByRole("link", { name: new RegExp(`^${name}`) }).getAttribute("href");
    expect(href("My work")).toBe("/acme/issues?view=my_work");
    expect(href("Working")).toBe("/acme/issues?view=working");
    expect(href("Subscribed")).toBe("/acme/issues?view=subscribed");
  });

  it.each([
    ["my_work", "My work", { relations: ["assigned", "my_agents"] }],
    ["subscribed", "Subscribed", { relations: ["subscribed"] }],
  ])("lists %s's issues when expanded", async (key, name, filters) => {
    useSidebarTreeStore.setState({ expanded: [builtinSidebarSource(key as "my_work")] });
    renderSection();
    await screen.findByRole("link", { name });
    await waitFor(() =>
      expect(listIssueTableRows).toHaveBeenCalledWith(
        expect.objectContaining({
          query: expect.objectContaining({
            scope: { kind: "workspace" },
            filters: expect.objectContaining(filters),
          }),
        }),
      ),
    );
  });

  it("counts and lists the issues agents are running on under Working", async () => {
    getWorkspaceWorkingAgents.mockResolvedValue([
      { id: "a1", name: "A", avatar_url: null, running_task_count: 2, issue_ids: ["i2", "i1"] },
      { id: "a2", name: "B", avatar_url: null, running_task_count: 1, issue_ids: ["i1"] },
    ]);
    useSidebarTreeStore.setState({ expanded: [builtinSidebarSource("working")] });
    renderSection();

    const link = await screen.findByRole("link", { name: /^Working/ });
    await waitFor(() => expect(link.closest("div")).toHaveTextContent(/^Working2/));
    await waitFor(() =>
      expect(listIssueTableRows).toHaveBeenCalledWith(
        expect.objectContaining({
          query: expect.objectContaining({
            filters: expect.objectContaining({ working_issue_ids: ["i1", "i2"] }),
          }),
        }),
      ),
    );
  });

  it("lights while its page is open, instead of All issues", async () => {
    useActiveIssueViewStore
      .getState()
      .setActive(issueViewContainerKey("ws-1", { scope_type: "workspace" }), "subscribed");
    renderSection();
    const link = await screen.findByRole("link", { name: "Subscribed" });
    expect(link).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "All issues" })).not.toHaveAttribute("aria-current");
  });

  it("is removed from the sidebar from its row menu", async () => {
    renderSection();
    fireEvent.contextMenu(await screen.findByRole("link", { name: "Subscribed" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Remove from sidebar" }));
    expect(actions.sidebar.unstar).toHaveBeenCalledWith("builtin:subscribed");
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
