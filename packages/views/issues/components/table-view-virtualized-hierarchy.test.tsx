/**
 * @vitest-environment jsdom
 *
 * MUL-5477 regression coverage for the PRODUCTION Table path.
 *
 * Production mounts the Table with `virtualizeRows` (table-view.tsx) and
 * `tableHierarchy` defaults to on (view-store.ts), so every parent row carries
 * an auto-activating branch sentinel and the row window is driven by the real
 * @tanstack/react-virtual. No other test covered that combination: they all
 * replace the virtualizer with a stand-in that renders every row, because jsdom
 * reports a zero-height viewport and the real one would render nothing.
 *
 * This file supplies the missing layout instead of removing the virtualizer,
 * because the virtualizer is what closes the circuit MUL-5477 runs around:
 * `serverDisplayRows` feeds the row list, the row list feeds the virtualizer's
 * `getItemKey`, and a new-but-equal row list makes the virtualizer re-key,
 * notify and re-render — which rebuilds the row list again. Any dependency of
 * `serverDisplayRows` that loses referential stability turns that circuit into a
 * sustained commit storm with no fetching and no DOM measurement, which is why a
 * convergence assertion is only meaningful with the real virtualizer mounted.
 *
 * Every mock below therefore returns HOISTED, stable references, mirroring the
 * real hooks (`useActorName` memoises `getActorName`). That is not incidental
 * tidiness: while writing this test, per-render mock closures alone were enough
 * to reproduce the storm, so an unstable mock here would assert the fixture
 * rather than the product.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor } from "@testing-library/react";
import { Profiler } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import type {
  Issue,
  IssueTableQuerySpec,
  IssueTableRowsRequest,
} from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueSurfaceSelectionProvider } from "../surface/selection-context";
import type { IssueSurfaceSelection } from "../surface/selection-context";
import { TableView } from "./table-view";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

const actorNames = vi.hoisted(() => {
  const getActorName = () => "Someone";
  return { getActorName, result: { getActorName } };
});
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => actorNames.result,
  buildActorNameResolver: () => actorNames.getActorName,
}));

const authState = vi.hoisted(() => ({
  value: {
    user: { id: "user-1", email: "t@t.co", name: "Tester" },
    isAuthenticated: true,
  },
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (state: unknown) => unknown) =>
      selector ? selector(authState.value) : authState.value,
    { getState: () => authState.value },
  ),
}));

const navigation = vi.hoisted(() => ({
  value: {
    push: () => {},
    openInNewTab: () => {},
    getShareableUrl: (path: string) => `https://app.example${path}`,
    pathname: "/",
  },
}));
vi.mock("../../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => (
    <a {...props}>{children}</a>
  ),
  useNavigation: () => navigation.value,
}));

vi.mock("@multica/core/paths", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/paths")>(
      "@multica/core/paths",
    );
  const workspacePaths = actual.paths.workspace("test");
  return { ...actual, useWorkspacePaths: () => workspacePaths };
});

/** Matches the DataTable virtualizer's own `virtualRowHeight` default. */
const ROW_HEIGHT = 41;
const VIEWPORT_HEIGHT = 600;

/**
 * The virtualizer reads its viewport from `offsetWidth`/`offsetHeight` on the
 * scroll element, and each row's height through `measureElement`, which the
 * DataTable implements with `getBoundingClientRect().height`. jsdom returns 0
 * for all three, so all three are supplied. The measured height equals
 * `estimateSize`, so a settled table has nothing left to correct — drift there
 * would be a real measurement loop rather than a fixture artifact.
 */
function installLayout() {
  const restore: Array<() => void> = [];
  for (const [property, value] of [
    ["offsetHeight", VIEWPORT_HEIGHT],
    ["offsetWidth", 1200],
  ] as const) {
    const original = Object.getOwnPropertyDescriptor(
      HTMLElement.prototype,
      property,
    );
    Object.defineProperty(HTMLElement.prototype, property, {
      configurable: true,
      get: () => value,
    });
    restore.push(() => {
      if (original) {
        Object.defineProperty(HTMLElement.prototype, property, original);
      } else {
        delete (HTMLElement.prototype as unknown as Record<string, unknown>)[
          property
        ];
      }
    });
  }

  const originalRect = Element.prototype.getBoundingClientRect;
  Element.prototype.getBoundingClientRect = function stubbedRect() {
    return {
      width: 1200,
      height: ROW_HEIGHT,
      top: 0,
      left: 0,
      right: 1200,
      bottom: ROW_HEIGHT,
      x: 0,
      y: 0,
      toJSON: () => ({}),
    } as DOMRect;
  };
  restore.push(() => {
    Element.prototype.getBoundingClientRect = originalRect;
  });

  return () => {
    for (const undo of restore.reverse()) undo();
  };
}

class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

/**
 * A viewport that reports everything handed to it as visible.
 *
 * Hierarchy branch sentinels activate on intersection, so this expands the whole
 * loaded tree at once rather than a scroll at a time — the aggressive end of
 * production, and the shape most likely to run the circuit away.
 */
class VisibleIntersectionObserver {
  #callback: IntersectionObserverCallback;
  constructor(callback: IntersectionObserverCallback) {
    this.#callback = callback;
  }
  observe(target: Element) {
    setTimeout(() => {
      this.#callback(
        [{ isIntersecting: true, target } as IntersectionObserverEntry],
        this as unknown as IntersectionObserver,
      );
    }, 0);
  }
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

function makeIssue(id: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title: `Task ${id}`,
    description: null,
    status: "todo",
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

/** Three roots, two leaf children each. Bounded, so settling is observable. */
const TREE: Record<string, string[]> = {
  root: ["a", "b", "c"],
  a: ["a1", "a2"],
  b: ["b1", "b2"],
  c: ["c1", "c2"],
};

const serverQuery: IssueTableQuerySpec = {
  scope: { kind: "workspace" },
  filters: {},
  sort: { field: "position", direction: "asc" },
};

const selection: IssueSurfaceSelection = {
  selectedIds: new Set<string>(),
  toggle: () => {},
  select: () => {},
  deselect: () => {},
  clear: () => {},
};

const EMPTY_PROGRESS = new Map();
const noop = () => {};
const exportIssues = () => Promise.resolve([]);
const resolveExportLookups = () =>
  Promise.resolve({ projectMap: new Map(), childProgressMap: new Map() });

let commits = 0;

function Harness({ surfaceKey }: { surfaceKey: string }) {
  return (
    <Profiler
      id="table"
      onRender={() => {
        commits += 1;
      }}
    >
      <ViewStoreProvider store={getIssueSurfaceViewStore(surfaceKey)}>
        <IssueSurfaceSelectionProvider selection={selection}>
          <TableView
            serverQuery={serverQuery}
            childProgressMap={EMPTY_PROGRESS}
            search=""
            onSearchChange={noop}
            onLoadedIssuesChange={noop}
            onCreateIssue={noop}
            exportIssues={exportIssues}
            resolveExportLookups={resolveExportLookups}
          />
        </IssueSurfaceSelectionProvider>
      </ViewStoreProvider>
    </Profiler>
  );
}

describe("Table view on the production virtualized hierarchy path", () => {
  let queryClient: QueryClient;
  let restoreLayout: () => void;
  let rowRequests: string[];

  beforeEach(() => {
    commits = 0;
    rowRequests = [];
    restoreLayout = installLayout();
    vi.stubGlobal("IntersectionObserver", VisibleIntersectionObserver);
    vi.stubGlobal("ResizeObserver", ResizeObserverStub);
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    setApiInstance({
      listProperties: async () => ({ properties: [] }),
      listMembers: async () => [],
      listAgents: async () => [],
      listSquads: async () => [],
      getAssigneeFrequency: async () => [],
      listIssueTableRows: async (request: IssueTableRowsRequest) => {
        const parent = request.parent_id ?? "root";
        rowRequests.push(parent);
        const ids = TREE[parent] ?? [];
        return {
          query_fingerprint: "test",
          group_key: request.group_key ?? null,
          parent_id: request.parent_id ?? null,
          total: ids.length,
          rows: ids.map((id) => ({
            issue: makeIssue(id),
            direct_child_count: (TREE[id] ?? []).length,
          })),
          branch_total: ids.length,
          next_cursor: null,
        };
      },
    } as unknown as ApiClient);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
    restoreLayout();
  });

  it("renders the hierarchy through the real virtualizer and stops committing once it settles", async () => {
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <Harness surfaceKey={`virtualized-${Math.floor(Math.random() * 1e9)}`} />
      </QueryClientProvider>,
    );

    // The virtualizer is real, so rows appear only if it saw a viewport. This is
    // the part every other Table test skips by replacing it.
    await screen.findByText("MUL-a");
    // Depth 1 arrives only by a branch sentinel activating its own query.
    await waitFor(() => expect(screen.queryByText("MUL-a1")).toBeTruthy(), {
      timeout: 5000,
    });
    await waitFor(() => expect(screen.queryByText("MUL-c2")).toBeTruthy(), {
      timeout: 5000,
    });

    // One request per branch: the root plus its three parents. A churning query
    // list detaches and reinstalls observers, which shows up here as repeats.
    await waitFor(() => expect(rowRequests).toHaveLength(4));
    expect([...rowRequests].sort()).toEqual(["a", "b", "c", "root"]);

    // Convergence. Nothing external touches the table past this point, so any
    // further commit is the table driving itself — the shape of the hang. It is
    // sampled twice because a loop presents as steady growth, not as one late
    // commit.
    const settled = commits;
    await new Promise((resolve) => setTimeout(resolve, 300));
    const afterIdle = commits;
    await new Promise((resolve) => setTimeout(resolve, 300));
    expect(afterIdle).toBe(settled);
    expect(commits).toBe(settled);
  }, 30000);
});
