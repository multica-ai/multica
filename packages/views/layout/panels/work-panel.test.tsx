import { useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { EMPTY_ISSUE_VIEW_PREFERENCE } from "@multica/core/api/schemas";
import { useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { issueViewPreferenceOptions } from "@multica/core/issue-views/preferences";
import { issueViewEverywhereOptions } from "@multica/core/issue-views/queries";
import { issueDetailOptions, issueKeys } from "@multica/core/issues/queries";
import { SIDEBAR_PROJECTS_SCOPE } from "@multica/core/issues/sidebar-project-prefs";
import { useSidebarKeptStore } from "@multica/core/issues/stores/sidebar-kept-store";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { spaceForPath } from "@multica/core/paths";
import { pinListOptions } from "@multica/core/pins/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Issue, IssueTableRowsResponse, PinnedItem, Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { KeptRowsRelease, WorkPanel } from "./work-panel";

const apiMock = vi.hoisted(() => ({ listIssueTableRows: vi.fn(), getIssue: vi.fn() }));
const actions = vi.hoisted(() => ({
  sidebar: {
    items: [
      { id: "builtin:my_issues", kind: "builtin", key: "my_issues" },
      { id: "builtin:all_issues", kind: "builtin", key: "all_issues" },
    ],
    reorder: vi.fn(),
    unstar: vi.fn(),
  },
  canManage: () => true,
}));

vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
vi.mock("../../issue-views/use-view-actions", () => ({ useIssueViewActions: () => actions }));
vi.mock("../../issue-views/delete-view-dialog", () => ({ DeleteViewDialog: () => null }));
vi.mock("../../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));
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
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("acme"),
    useCurrentWorkspace: () => ({ id: "ws-1" }),
  };
});
vi.mock("@multica/core/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/hooks")>()),
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/auth", async (importOriginal) => {
  const state = { user: { id: "u1" } };
  return {
    ...(await importOriginal<typeof import("@multica/core/auth")>()),
    useAuthStore: Object.assign((selector: (s: typeof state) => unknown) => selector(state), {
      getState: () => state,
    }),
  };
});
vi.mock("@multica/core/issue-statuses/hooks", async () => {
  const { buildIssueStatusCatalog } = await import("@multica/core/issue-statuses/queries");
  const catalog = buildIssueStatusCatalog([
    {
      id: "s1",
      workspace_id: "ws-1",
      key: "todo",
      name: "Todo",
      description: "",
      category: "unstarted",
      color: "",
      is_system: true,
      position: 1,
      archived_at: null,
      created_at: "",
      updated_at: "",
    },
  ]);
  return { useIssueStatuses: () => catalog };
});

const ISSUE = {
  id: "0a3f0a8e-6a52-4c1e-9f5e-1b2c3d4e5f60",
  identifier: "MUL-1",
  title: "Fix the sidebar",
  status: "todo",
} as Issue;
const OTHER = {
  id: "7b9e2c41-0d3a-4f6b-8e7c-2a1b0c9d8e7f",
  identifier: "MUL-2",
  title: "Polish the header",
  status: "todo",
} as Issue;
const PIN: PinnedItem = {
  id: "pin-1",
  workspace_id: "ws-1",
  user_id: "u1",
  item_type: "issue",
  item_id: ISSUE.id,
  position: 0,
  created_at: "",
};

const PROJECT = {
  id: "5d4c3b2a-1f0e-4d9c-8b7a-6f5e4d3c2b1a",
  title: "Launch",
  status: "in_progress",
  lead_type: null,
  lead_id: null,
  issue_count: 0,
  done_count: 0,
  created_at: "",
  updated_at: "",
} as unknown as Project;

function renderPanel(initialPath: string, pins: PinnedItem[] = [], projects: Project[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(projectListOptions("ws-1").queryKey, { projects, total: projects.length });
  qc.setQueryData(pinListOptions("ws-1", "u1").queryKey, pins);
  qc.setQueryData(issueViewEverywhereOptions("ws-1").queryKey, []);
  qc.setQueryData(issueViewPreferenceOptions("ws-1", SIDEBAR_PROJECTS_SCOPE).queryKey, EMPTY_ISSUE_VIEW_PREFERENCE);
  qc.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, []);

  function Harness() {
    const [pathname, setPathname] = useState(initialPath);
    // A collapsed sidebar mounts a second panel as its hover peek.
    const [peek, setPeek] = useState(false);
    // A compact sheet unmounts the panel whenever it closes.
    const [sheetOpen, setSheetOpen] = useState(true);
    const adapter: NavigationAdapter = {
      push: setPathname,
      replace: setPathname,
      back: vi.fn(),
      pathname,
      searchParams: new URLSearchParams(),
      hash: "",
      getShareableUrl: (path) => path,
    };
    return (
      <NavigationProvider value={adapter}>
        <KeptRowsRelease />
        {sheetOpen && spaceForPath(pathname) === "work" && <WorkPanel />}
        <button type="button" onClick={() => setPathname("/acme/inbox")}>
          Visit inbox
        </button>
        <button type="button" onClick={() => setPathname(`/acme/issues/${ISSUE.id}`)}>
          Return to issue
        </button>
        <button type="button" onClick={() => setSheetOpen((open) => !open)}>
          Toggle sheet
        </button>
        <button type="button" onClick={() => setPeek(true)}>
          Open peek
        </button>
        {peek && (
          <div data-testid="peek">
            <WorkPanel />
          </div>
        )}
      </NavigationProvider>
    );
  }

  renderWithI18n(
    <QueryClientProvider client={qc}>
      <Harness />
    </QueryClientProvider>,
  );
  return qc;
}

const issueRow = () => screen.getByRole("link", { name: ISSUE.title });
const allIssues = () => screen.getByRole("link", { name: "All issues" });

beforeEach(() => {
  useSidebarTreeStore.setState({
    expanded: ["builtin:my_issues"],
    viewsCollapsed: false,
    pinnedCollapsed: false,
    projectsCollapsed: false,
  });
  useActiveIssueViewStore.setState({ active: {} });
  useSidebarKeptStore.setState({ held: {} });
  apiMock.listIssueTableRows.mockResolvedValue({
    rows: [ISSUE, OTHER].map((issue) => ({ issue })),
    total: 2,
    next_cursor: null,
  } as unknown as IssueTableRowsResponse);
  // The issue page never finishes loading, so the panel cannot lean on it.
  apiMock.getIssue.mockReturnValue(new Promise<Issue>(() => {}));
});

describe("WorkPanel issue highlight", () => {
  it("lights a clicked tree issue at once, never All issues first", async () => {
    renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));

    expect(issueRow()).toHaveAttribute("aria-current", "page");
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it.each(["MUL-1", "mul-1"])("finds an issue opened by its identifier (%s) in the tree", async (segment) => {
    renderPanel(`/acme/issues/${segment}`);

    expect(await screen.findByRole("link", { name: ISSUE.title })).toHaveAttribute("aria-current", "page");
    expect(allIssues()).not.toHaveAttribute("aria-current");
  });

  it("keeps a pinned issue lit on the tree row it was clicked from", async () => {
    renderPanel("/acme/my-issues", [PIN]);
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));

    expect(issueRow()).toHaveAttribute("aria-current", "page");
  });

  it("leaves All issues lit for an issue no expanded row lists", async () => {
    renderPanel("/acme/issues/9c8d7e6f-5a4b-4c3d-8e2f-1a0b9c8d7e6f");

    await screen.findByRole("link", { name: ISSUE.title });
    expect(allIssues()).toHaveAttribute("aria-current", "page");
  });

  it("keeps an issue its list drops lit in place until another issue opens", async () => {
    const qc = renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));
    qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, { ...ISSUE, title: "Fixed the sidebar", status: "done" });
    apiMock.listIssueTableRows.mockResolvedValue({ rows: [{ issue: OTHER }], total: 1, next_cursor: null });
    await act(() => qc.invalidateQueries({ queryKey: issueKeys.sidebarAll("ws-1") }));

    expect(await screen.findByRole("link", { name: "Fixed the sidebar" })).toHaveAttribute("aria-current", "page");
    expect(allIssues()).not.toHaveAttribute("aria-current");

    fireEvent.click(screen.getByRole("link", { name: OTHER.title }));
    expect(screen.queryByRole("link", { name: "Fixed the sidebar" })).toBeNull();
  });

  it("drops a kept issue once it is deleted", async () => {
    const qc = renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));
    qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, ISSUE);
    apiMock.listIssueTableRows.mockResolvedValue({ rows: [{ issue: OTHER }], total: 1, next_cursor: null });
    await act(async () => {
      qc.removeQueries({ queryKey: issueKeys.detail("ws-1", ISSUE.id) });
      await qc.invalidateQueries({ queryKey: issueKeys.sidebarAll("ws-1") });
    });

    await waitFor(() => expect(screen.queryByRole("link", { name: ISSUE.title })).toBeNull());
  });

  it("keeps the dropped issue lit in a peek opened afterwards", async () => {
    const qc = renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));
    qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, { ...ISSUE, title: "Fixed the sidebar", status: "done" });
    apiMock.listIssueTableRows.mockResolvedValue({ rows: [{ issue: OTHER }], total: 1, next_cursor: null });
    await act(() => qc.invalidateQueries({ queryKey: issueKeys.sidebarAll("ws-1") }));
    await screen.findByRole("link", { name: "Fixed the sidebar" });
    fireEvent.click(screen.getByRole("button", { name: "Open peek" }));

    const peek = within(screen.getByTestId("peek"));
    expect(peek.getByRole("link", { name: "Fixed the sidebar" })).toHaveAttribute("aria-current", "page");
    expect(peek.getByRole("link", { name: "All issues" })).not.toHaveAttribute("aria-current");
  });

  it("keeps the dropped issue while the panel remounts on the same page", async () => {
    const qc = renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));
    qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, { ...ISSUE, title: "Fixed the sidebar", status: "done" });
    apiMock.listIssueTableRows.mockResolvedValue({ rows: [{ issue: OTHER }], total: 1, next_cursor: null });
    await act(() => qc.invalidateQueries({ queryKey: issueKeys.sidebarAll("ws-1") }));
    await screen.findByRole("link", { name: "Fixed the sidebar" });

    fireEvent.click(screen.getByRole("button", { name: "Toggle sheet" }));
    fireEvent.click(screen.getByRole("button", { name: "Toggle sheet" }));

    expect(await screen.findByRole("link", { name: "Fixed the sidebar" })).toHaveAttribute("aria-current", "page");
  });

  it("ends the keep after leaving Work and coming back to the issue", async () => {
    const qc = renderPanel("/acme/my-issues");
    fireEvent.click(await screen.findByRole("link", { name: ISSUE.title }));
    qc.setQueryData(issueDetailOptions("ws-1", ISSUE.id).queryKey, { ...ISSUE, title: "Fixed the sidebar", status: "done" });
    apiMock.listIssueTableRows.mockResolvedValue({ rows: [{ issue: OTHER }], total: 1, next_cursor: null });
    await act(() => qc.invalidateQueries({ queryKey: issueKeys.sidebarAll("ws-1") }));
    await screen.findByRole("link", { name: "Fixed the sidebar" });

    fireEvent.click(screen.getByRole("button", { name: "Visit inbox" }));
    fireEvent.click(screen.getByRole("button", { name: "Return to issue" }));

    expect(await screen.findByRole("link", { name: OTHER.title })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Fixed the sidebar" })).toBeNull();
    expect(allIssues()).toHaveAttribute("aria-current", "page");
  });

  it("keeps the open project in the tree after it leaves the projects filter", async () => {
    const qc = renderPanel(`/acme/projects/${PROJECT.id}`, [], [PROJECT]);
    expect(screen.getByRole("link", { name: PROJECT.title })).toHaveAttribute("aria-current", "page");

    act(() => {
      qc.setQueryData(projectListOptions("ws-1").queryKey, {
        projects: [{ ...PROJECT, title: "Launched", status: "completed" }],
        total: 1,
      });
    });
    expect(await screen.findByRole("link", { name: "Launched" })).toHaveAttribute("aria-current", "page");

    fireEvent.click(allIssues());
    expect(screen.queryByRole("link", { name: "Launched" })).toBeNull();
  });
});
