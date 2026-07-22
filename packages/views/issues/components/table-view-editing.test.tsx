/**
 * @vitest-environment jsdom
 *
 * MUL-5108 regression coverage: an open cell editor popup must survive the
 * data refreshes that constantly hit an active workspace (realtime refetches
 * rebuilding childProgressMap / issue arrays, window pages arriving, the
 * end-of-load hierarchy assembly). Before the fix, refreshed lookups rebuilt
 * the column defs' render closures — flexRender treats those as component
 * TYPES, so React remounted every cell and the just-opened picker closed.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import type { Issue } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueSurfaceSelectionProvider } from "../surface/selection-context";
import type { IssueSurfaceSelection } from "../surface/selection-context";
import type { IssueCreateDefaults } from "../surface/types";
import type { ChildProgress } from "./list-row";
import { TableView, useReleaseEditingCellOnUnmount } from "./table-view";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// jsdom has no layout, so the real row virtualizer sees a 0-height viewport
// and renders nothing. Render every row inline instead (mirrors the
// react-virtuoso mock in issue-surface.test.tsx).
vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: (options: {
    count: number;
    getItemKey?: (index: number) => unknown;
  }) => ({
    getVirtualItems: () =>
      Array.from({ length: options.count }, (_, index) => ({
        index,
        key: options.getItemKey?.(index) ?? index,
        start: index * 41,
        end: (index + 1) * 41,
        size: 41,
        lane: 0,
      })),
    getTotalSize: () => options.count * 41,
    measureElement: () => {},
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Someone" }),
  buildActorNameResolver: () => () => "Someone",
}));

const mockAuthUser = { id: "user-1", email: "t@t.co", name: "Tester" };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (state: unknown) => unknown) => {
      const state = { user: mockAuthUser, isAuthenticated: true };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: mockAuthUser, isAuthenticated: true }) },
  ),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => (
    <a {...props}>{children}</a>
  ),
  useNavigation: () => ({ push: vi.fn(), pathname: "/" }),
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

class ObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

function makeIssue(id: string, title: string, status: Issue["status"]): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title,
    description: null,
    status,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "member-1",
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

const selection: IssueSurfaceSelection = {
  selectedIds: new Set<string>(),
  toggle: () => {},
  select: () => {},
  deselect: () => {},
  clear: () => {},
};

function Harness({
  issues,
  childProgressMap,
  surfaceKey,
  onCreateIssue = () => {},
}: {
  issues: Issue[];
  childProgressMap: Map<string, ChildProgress>;
  surfaceKey: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
}) {
  return (
    <ViewStoreProvider store={getIssueSurfaceViewStore(surfaceKey)}>
      <IssueSurfaceSelectionProvider selection={selection}>
        <TableView
          issues={issues}
          childProgressMap={childProgressMap}
          fetchNextPage={() => Promise.resolve()}
          hasNextPage={false}
          isFetchingNextPage={false}
          windowError={false}
          total={issues.length}
          search=""
          onSearchChange={() => {}}
          onCreateIssue={onCreateIssue}
          exportIssues={() => Promise.resolve(issues)}
          resolveExportLookups={() =>
            Promise.resolve({
              projectMap: new Map(),
              childProgressMap: new Map(),
            })
          }
        />
      </IssueSurfaceSelectionProvider>
    </ViewStoreProvider>
  );
}

describe("TableView cell editors under data refresh", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.stubGlobal("IntersectionObserver", ObserverStub);
    vi.stubGlobal("ResizeObserver", ObserverStub);
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    setApiInstance({
      listProperties: async () => ({ properties: [] }),
      listMembers: async () => [],
      listAgents: async () => [],
      listSquads: async () => [],
      getAssigneeFrequency: async () => [],
    } as unknown as ApiClient);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  // Explicit timeout: this mounts the full TableView with every picker + a
  // QueryClient, so it is heavier than a unit test. `delay: null` drives
  // userEvent off fake-synchronous timing instead of the default real-timer
  // gaps between events, which were what let the whole gesture blow past the
  // 5s default under concurrent CI worker load (MUL-5108 review R1#1).
  it("keeps the status picker open and the row order frozen across a refresh, then catches up on close", async () => {
    const user = userEvent.setup({ delay: null, pointerEventsCheck: 0 });
    const issueA = makeIssue("a", "Alpha task", "todo");
    const issueB = makeIssue("b", "Beta task", "in_progress");
    const progress1 = new Map<string, ChildProgress>();
    const surfaceKey = `test-surface-${Math.floor(Math.random() * 1e9)}`;

    const view = renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness
          issues={[issueA, issueB]}
          childProgressMap={progress1}
          surfaceKey={surfaceKey}
        />
      </QueryClientProvider>,
    );

    const identifiers = () =>
      screen.getAllByText(/^MUL-/).map((node) => node.textContent);
    expect(identifiers()).toEqual(["MUL-a", "MUL-b"]);

    // Open the status picker on row A: its cell trigger shows "Todo".
    const rowA = screen.getByText("MUL-a").closest("tr")!;
    await user.click(within(rowA).getByRole("button", { name: /Todo/ }));
    // Base UI portals the popup; "Backlog" only exists while it is open.
    expect(screen.getByRole("button", { name: /Backlog/ })).toBeTruthy();

    // A realtime refresh lands: new array identities, rows reordered, new
    // childProgressMap. The popup must stay open and the structure must hold
    // (frozen order) so the anchor row cannot move away mid-interaction.
    const refreshedA = { ...issueA, title: "Alpha task (updated)" };
    view.rerender(
      <QueryClientProvider client={queryClient}>
        <Harness
          issues={[issueB, refreshedA]}
          childProgressMap={new Map<string, ChildProgress>()}
          surfaceKey={surfaceKey}
        />
      </QueryClientProvider>,
    );

    expect(screen.getByRole("button", { name: /Backlog/ })).toBeTruthy();
    expect(identifiers()).toEqual(["MUL-a", "MUL-b"]);
    // …while the VALUES inside the frozen rows keep tracking the live data.
    expect(screen.getByText("Alpha task (updated)")).toBeTruthy();

    // Selecting a value closes the editor; the deferred live order applies.
    await user.click(screen.getByRole("button", { name: /Backlog/ }));
    expect(screen.queryByRole("button", { name: /Backlog/ })).toBeNull();
    expect(identifiers()).toEqual(["MUL-b", "MUL-a"]);
  }, 20_000);

  it("opens creation with the row as parent and inherits its project", async () => {
    const user = userEvent.setup({ delay: null, pointerEventsCheck: 0 });
    const onCreateIssue = vi.fn();
    const issue = {
      ...makeIssue("a", "Alpha task", "todo"),
      project_id: "project-1",
    };

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness
          issues={[issue]}
          childProgressMap={new Map()}
          surfaceKey={`test-create-sub-issue-${Math.floor(Math.random() * 1e9)}`}
          onCreateIssue={onCreateIssue}
        />
      </QueryClientProvider>,
    );

    const row = screen.getByText("MUL-a").closest("tr")!;
    await user.click(
      within(row).getByRole("button", { name: "Create sub-issue" }),
    );

    expect(onCreateIssue).toHaveBeenCalledWith({
      parent_issue_id: "a",
      parent_issue_identifier: "MUL-a",
      project_id: "project-1",
    });
  });
});

// Row virtualization unmounts a cell when its row scrolls out of the window
// (data-table.tsx). Base UI does not fire onOpenChange(false) on unmount, so
// the hoisted editing key — and the frozen structure it holds — needs an
// explicit release when the owning cell leaves the DOM (MUL-5108 review R1#3).
// A cell unmounting is exactly what a virtual-window change does; probing the
// hook directly keeps the assertion deterministic (jsdom has no layout for a
// real virtualizer to react to).
describe("useReleaseEditingCellOnUnmount", () => {
  function Probe({
    cellKey,
    editingCellKey,
    setEditingCellKey,
  }: {
    cellKey: string | null;
    editingCellKey: string | null;
    setEditingCellKey: (key: string | null) => void;
  }) {
    useReleaseEditingCellOnUnmount(cellKey, editingCellKey, setEditingCellKey);
    return null;
  }

  afterEach(cleanup);

  it("clears the key when the cell that owns the open editor unmounts", () => {
    const setEditingCellKey = vi.fn();
    const { unmount } = render(
      <Probe
        cellKey="issue-a:status"
        editingCellKey="issue-a:status"
        setEditingCellKey={setEditingCellKey}
      />,
    );

    unmount();

    expect(setEditingCellKey).toHaveBeenCalledWith(null);
  });

  it("leaves the key untouched when a different cell unmounts", () => {
    const setEditingCellKey = vi.fn();
    const { unmount } = render(
      <Probe
        cellKey="issue-b:status"
        editingCellKey="issue-a:status"
        setEditingCellKey={setEditingCellKey}
      />,
    );

    unmount();

    expect(setEditingCellKey).not.toHaveBeenCalled();
  });

  it("does not fire on mount while the cell is not yet the active editor", () => {
    const setEditingCellKey = vi.fn();
    // Mount not-owning, then the editor opens on THIS cell (rerender, no
    // remount), then it unmounts — the responder reads the latest key.
    const { rerender, unmount } = render(
      <Probe
        cellKey="issue-a:status"
        editingCellKey={null}
        setEditingCellKey={setEditingCellKey}
      />,
    );
    expect(setEditingCellKey).not.toHaveBeenCalled();

    rerender(
      <Probe
        cellKey="issue-a:status"
        editingCellKey="issue-a:status"
        setEditingCellKey={setEditingCellKey}
      />,
    );
    expect(setEditingCellKey).not.toHaveBeenCalled();

    unmount();
    expect(setEditingCellKey).toHaveBeenCalledTimes(1);
    expect(setEditingCellKey).toHaveBeenCalledWith(null);
  });
});
