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
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import { useModalStore } from "@multica/core/modals";
import type { ApiClient } from "@multica/core/api/client";
import { issueKeys } from "@multica/core/issues/queries";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import type {
  Issue,
  IssueTableQuerySpec,
  IssueTableRowsResponse,
} from "@multica/core/types";
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

// The assignee cell's avatar only renders for a row that HAS an assignee (the
// run-confirm case below) and pulls in presence, images and the rest of the
// workspace-hooks surface. None of that is under test here.
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
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

const navigationMocks = vi.hoisted(() => ({
  push: vi.fn(),
  openInNewTab: vi.fn(),
  getShareableUrl: vi.fn((path: string) => `https://app.example${path}`),
}));
const navigationState = vi.hoisted(() => ({ hasOpenInNewTab: true }));

vi.mock("../../navigation", async () => {
  // Real resolver — pure, no React context.
  const { resolveClickIntent } = await vi.importActual<
    typeof import("../../navigation/click-intent")
  >("../../navigation/click-intent");
  return {
    AppLink: ({ children, ...props }: React.ComponentProps<"a">) => (
      <a {...props}>{children}</a>
    ),
    resolveClickIntent,
    useNavigation: () => ({
      push: navigationMocks.push,
      openInNewTab: navigationState.hasOpenInNewTab
        ? navigationMocks.openInNewTab
        : undefined,
      getShareableUrl: navigationMocks.getShareableUrl,
      pathname: "/",
    }),
    // Mirrors the real hook's adapter semantics against the mocks above.
    useIntentNavigate:
      () => (href: string, intent: string, newTabTitle?: string) => {
        if (intent === "push") {
          navigationMocks.push(href);
          return;
        }
        if (navigationState.hasOpenInNewTab) {
          if (intent === "foreground-tab") {
            navigationMocks.openInNewTab(href, newTabTitle, {
              activate: true,
            });
          } else {
            navigationMocks.openInNewTab(href, newTabTitle);
          }
          return;
        }
        window.open(
          navigationMocks.getShareableUrl(href),
          "_blank",
          "noopener,noreferrer",
        );
      },
  };
});

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

const serverQuery: IssueTableQuerySpec = {
  scope: { kind: "workspace" },
  filters: {},
  sort: { field: "position", direction: "asc" },
};

let serverIssues: Issue[] = [];

function Harness({
  childProgressMap,
  surfaceKey,
  onCreateIssue = () => {},
}: {
  childProgressMap: Map<string, ChildProgress>;
  surfaceKey: string;
  onCreateIssue?: (defaults: IssueCreateDefaults) => void;
}) {
  return (
    <ViewStoreProvider store={getIssueSurfaceViewStore(surfaceKey)}>
      <IssueSurfaceSelectionProvider selection={selection}>
        <TableView
          serverQuery={serverQuery}
          childProgressMap={childProgressMap}
          search=""
          onSearchChange={() => {}}
          onLoadedIssuesChange={() => {}}
          onCreateIssue={onCreateIssue}
          exportIssues={() => Promise.resolve(serverIssues)}
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
  // The table's inline pickers are single-issue writes like the issue detail's,
  // so they route on the same run-confirm gate: promoting an agent-owned issue
  // out of backlog starts a run and must confirm first (MUL-6463). The gate's
  // own matrix lives in ../actions/run-confirm-gate.test.ts; this only proves
  // the table asks it instead of writing straight through.
  it("confirms a status change that would start an agent run instead of applying it", async () => {
    const user = userEvent.setup({ delay: null, pointerEventsCheck: 0 });
    serverIssues = [
      {
        ...makeIssue("c", "Parked task", "backlog"),
        assignee_type: "agent",
        assignee_id: "agent-1",
      },
    ];
    useModalStore.getState().close();

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness
          childProgressMap={new Map<string, ChildProgress>()}
          surfaceKey={`test-surface-${Math.floor(Math.random() * 1e9)}`}
        />
      </QueryClientProvider>,
    );

    await screen.findByText("MUL-c");
    const row = screen.getByText("MUL-c").closest("tr")!;
    await user.click(within(row).getByRole("button", { name: /Backlog/ }));
    await user.click(screen.getByRole("button", { name: /^Todo$/ }));

    const { modal, data } = useModalStore.getState();
    expect(modal).toBe("issue-run-confirm");
    expect(data).toMatchObject({
      mode: "promote",
      status: "todo",
      assigneeType: "agent",
      assigneeId: "agent-1",
    });
    useModalStore.getState().close();
  });

  let queryClient: QueryClient;

  beforeEach(() => {
    navigationMocks.push.mockReset();
    navigationMocks.openInNewTab.mockReset();
    navigationMocks.getShareableUrl.mockReset();
    navigationMocks.getShareableUrl.mockImplementation(
      (path: string) => `https://app.example${path}`,
    );
    navigationState.hasOpenInNewTab = true;
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
      listIssueStatuses: async () => ({ statuses: [] }),
      listIssueTableRows: async () => ({
        query_fingerprint: "test",
        group_key: null,
        parent_id: null,
        total: serverIssues.length,
        rows: serverIssues.map((issue) => ({
          issue,
          direct_child_count: 0,
        })),
        branch_total: serverIssues.length,
        next_cursor: null,
      }),
    } as unknown as ApiClient);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  // Explicit timeout: this mounts the full TableView with every picker + a
  // QueryClient and drives three realistic userEvent click gestures, each
  // re-rendering the whole table — far heavier than a unit test. `delay: null`
  // strips the default real-timer gaps between events (MUL-5108 review R1#1).
  // Even so, the frontend CI job runs the entire `turbo build typecheck lint
  // test` pipeline on a 2-core runner, so builds/lints/typechecks and 258
  // vitest files all oversubscribe both cores at once; at the worst-case
  // scheduling peak this test's wall clock blew past the earlier 20s cap
  // (MUL-5326). It runs in ~1s in isolation, so the generous 60s ceiling
  // (matching the repo's heaviest FE tests) only absorbs CI CPU starvation —
  // it never masks a real hang.
  it("keeps the status picker open and the row order frozen across a refresh, then catches up on close", async () => {
    const user = userEvent.setup({ delay: null, pointerEventsCheck: 0 });
    const issueA = makeIssue("a", "Alpha task", "todo");
    const issueB = makeIssue("b", "Beta task", "in_progress");
    serverIssues = [issueA, issueB];
    const progress1 = new Map<string, ChildProgress>();
    const surfaceKey = `test-surface-${Math.floor(Math.random() * 1e9)}`;

    const view = renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness
          childProgressMap={progress1}
          surfaceKey={surfaceKey}
        />
      </QueryClientProvider>,
    );

    const identifiers = () =>
      screen.getAllByText(/^MUL-/).map((node) => node.textContent);
    await screen.findByText("MUL-a");
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
    serverIssues = [issueB, refreshedA];
    view.rerender(
      <QueryClientProvider client={queryClient}>
        <Harness
          childProgressMap={new Map<string, ChildProgress>()}
          surfaceKey={surfaceKey}
        />
      </QueryClientProvider>,
    );
    act(() => {
      queryClient.setQueriesData<IssueTableRowsResponse>(
        { queryKey: issueKeys.tableAll("ws-1") },
        (previous) =>
          previous
            ? {
                ...previous,
                total: serverIssues.length,
                branch_total: serverIssues.length,
                rows: serverIssues.map((issue) => ({
                  issue,
                  direct_child_count: 0,
                })),
              }
            : previous,
      );
    });

    await waitFor(() => {
      expect(screen.getByRole("button", { name: /Backlog/ })).toBeTruthy();
      expect(identifiers()).toEqual(["MUL-a", "MUL-b"]);
      // …while the VALUES inside the frozen rows keep tracking live data.
      expect(screen.getByText("Alpha task (updated)")).toBeTruthy();
    });

    // Selecting a value closes the editor; the deferred live order applies.
    await user.click(screen.getByRole("button", { name: /Backlog/ }));
    expect(screen.queryByRole("button", { name: /Backlog/ })).toBeNull();
    expect(identifiers()).toEqual(["MUL-b", "MUL-a"]);
  }, 60_000);

  it("opens creation with the row as parent and inherits its project", async () => {
    const user = userEvent.setup({ delay: null, pointerEventsCheck: 0 });
    const onCreateIssue = vi.fn();
    const issue = {
      ...makeIssue("a", "Alpha task", "todo"),
      project_id: "project-1",
    };
    serverIssues = [issue];

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness
          childProgressMap={new Map()}
          surfaceKey={`test-create-sub-issue-${Math.floor(Math.random() * 1e9)}`}
          onCreateIssue={onCreateIssue}
        />
      </QueryClientProvider>,
    );

    const row = (await screen.findByText("MUL-a")).closest("tr")!;
    await user.click(
      within(row).getByRole("button", { name: "Create sub-issue" }),
    );

    expect(onCreateIssue).toHaveBeenCalledWith({
      parent_issue_id: "a",
      parent_issue_identifier: "MUL-a",
      project_id: "project-1",
    });
  });

  it("navigates in place on plain title and row clicks; modifiers open tabs", async () => {
    const user = userEvent.setup({ delay: null, pointerEventsCheck: 0 });
    serverIssues = [makeIssue("a", "Alpha task", "todo")];

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness
          childProgressMap={new Map()}
          surfaceKey={`test-new-tab-${Math.floor(Math.random() * 1e9)}`}
        />
      </QueryClientProvider>,
    );

    const row = (await screen.findByText("MUL-a")).closest("tr")!;
    const title = within(row).getByRole("button", { name: "Alpha task" });

    await user.click(title);
    expect(navigationMocks.push).toHaveBeenCalledWith("/test/issues/a");
    expect(navigationMocks.openInNewTab).not.toHaveBeenCalled();

    navigationMocks.push.mockClear();
    await user.click(row);
    expect(navigationMocks.push).toHaveBeenCalledWith("/test/issues/a");
    expect(navigationMocks.openInNewTab).not.toHaveBeenCalled();

    navigationMocks.push.mockClear();
    fireEvent.click(title, { metaKey: true });
    expect(navigationMocks.openInNewTab).toHaveBeenCalledWith(
      "/test/issues/a",
      "MUL-a",
    );

    navigationMocks.openInNewTab.mockClear();
    fireEvent.click(row, { metaKey: true, shiftKey: true });
    expect(navigationMocks.openInNewTab).toHaveBeenCalledWith(
      "/test/issues/a",
      "MUL-a",
      { activate: true },
    );
    expect(navigationMocks.push).not.toHaveBeenCalled();
  });

  it("middle click on an interactive cell does not trigger row navigation", async () => {
    serverIssues = [makeIssue("a", "Alpha task", "todo")];

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness
          childProgressMap={new Map()}
          surfaceKey={`test-aux-cells-${Math.floor(Math.random() * 1e9)}`}
        />
      </QueryClientProvider>,
    );

    const row = (await screen.findByText("MUL-a")).closest("tr")!;
    const auxClick = (el: HTMLElement) =>
      el.dispatchEvent(
        new MouseEvent("auxclick", { bubbles: true, button: 1, cancelable: true }),
      );

    // Status cell trigger (interactive) — must NOT bubble into a row open.
    auxClick(within(row).getByRole("button", { name: /Todo/ }));
    expect(navigationMocks.openInNewTab).not.toHaveBeenCalled();
    expect(navigationMocks.push).not.toHaveBeenCalled();

    // Row checkbox — same.
    auxClick(within(row).getByRole("checkbox"));
    expect(navigationMocks.openInNewTab).not.toHaveBeenCalled();

    // Dead space on the row still opens a background tab on middle click.
    auxClick(row);
    expect(navigationMocks.openInNewTab).toHaveBeenCalledWith(
      "/test/issues/a",
      "MUL-a",
    );
  });

  it("without a tab adapter (web), plain click pushes and a modifier click opens a browser tab", async () => {
    const user = userEvent.setup({ delay: null, pointerEventsCheck: 0 });
    const windowOpen = vi.fn();
    vi.stubGlobal("open", windowOpen);
    navigationState.hasOpenInNewTab = false;
    serverIssues = [makeIssue("a", "Alpha task", "todo")];

    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness
          childProgressMap={new Map()}
          surfaceKey={`test-browser-tab-${Math.floor(Math.random() * 1e9)}`}
        />
      </QueryClientProvider>,
    );

    const row = (await screen.findByText("MUL-a")).closest("tr")!;
    const title = within(row).getByRole("button", { name: "Alpha task" });

    await user.click(title);
    expect(navigationMocks.push).toHaveBeenCalledWith("/test/issues/a");
    expect(windowOpen).not.toHaveBeenCalled();

    fireEvent.click(title, { metaKey: true });
    expect(navigationMocks.getShareableUrl).toHaveBeenCalledWith(
      "/test/issues/a",
    );
    expect(windowOpen).toHaveBeenCalledWith(
      "https://app.example/test/issues/a",
      "_blank",
      "noopener,noreferrer",
    );
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
