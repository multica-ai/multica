import { useState, type ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { issueViewEverywhereOptions } from "@multica/core/issue-views/queries";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import { pinListOptions } from "@multica/core/pins/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import type { Issue, IssueTableRowsResponse, PinnedItem } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { WorkPanel } from "./work-panel";

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

function renderPanel(initialPath: string, pins: PinnedItem[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData(projectListOptions("ws-1").queryKey, { projects: [], total: 0 });
  qc.setQueryData(pinListOptions("ws-1", "u1").queryKey, pins);
  qc.setQueryData(issueViewEverywhereOptions("ws-1").queryKey, []);
  qc.setQueryData(agentTaskSnapshotOptions("ws-1").queryKey, []);

  function Harness() {
    const [pathname, setPathname] = useState(initialPath);
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
        <WorkPanel />
      </NavigationProvider>
    );
  }

  renderWithI18n(
    <QueryClientProvider client={qc}>
      <Harness />
    </QueryClientProvider>,
  );
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
});
