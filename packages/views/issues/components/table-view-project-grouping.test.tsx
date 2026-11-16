/**
 * @vitest-environment jsdom
 *
 * Table grouped by project. The server hands back only a project id per group,
 * so the header text the user reads is resolved on the client — a group row
 * that renders the raw uuid is the failure this file exists to catch.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import type {
  Issue,
  IssueTableGroupsRequest,
  IssueTableQuerySpec,
  IssueTableRowsRequest,
} from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueSurfaceSelectionProvider } from "../surface/selection-context";
import type { IssueSurfaceSelection } from "../surface/selection-context";
import { TableView } from "./table-view";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

// jsdom has no layout, so the real row virtualizer sees a 0-height viewport and
// renders nothing. Render every row inline instead.
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

const authState = { user: { id: "user-1", email: "t@t.co", name: "Tester" }, isAuthenticated: true };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (state: unknown) => unknown) =>
      selector ? selector(authState) : authState,
    { getState: () => authState },
  ),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ children, ...props }: React.ComponentProps<"a">) => (
    <a {...props}>{children}</a>
  ),
  useNavigation: () => ({
    push: () => {},
    openInNewTab: () => {},
    getShareableUrl: (path: string) => `https://app.example${path}`,
    pathname: "/",
  }),
  resolveClickIntent: () => "push",
  useIntentNavigate: () => () => {},
}));

vi.mock("@multica/core/paths", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/paths")>(
      "@multica/core/paths",
    );
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("test") };
});

class ObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

/**
 * Reports everything handed to it as visible. A collapsed group's rows load
 * through an activation sentinel, so an inert observer leaves every group
 * header rendered above an empty branch.
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

const ACME_ID = "11111111-1111-4111-8111-111111111111";
/** A project the projects query does not answer for — deleted, or invisible to
 *  this member. Its rows still exist, so its header still has to say something. */
const GHOST_ID = "22222222-2222-4222-8222-222222222222";

function makeIssue(id: string, projectId: string | null): Issue {
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
    project_id: projectId,
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

const ROWS_BY_GROUP: Record<string, Issue[]> = {
  "project:none": [makeIssue("loose", null)],
  [`project:${ACME_ID}`]: [makeIssue("acme", ACME_ID)],
  [`project:${GHOST_ID}`]: [makeIssue("ghost", GHOST_ID)],
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

describe("Table grouped by project", () => {
  let queryClient: QueryClient;
  let groupRequests: IssueTableGroupsRequest[];
  let surfaceKey: string;

  beforeEach(() => {
    groupRequests = [];
    surfaceKey = `project-grouping-${Math.floor(Math.random() * 1e9)}`;
    vi.stubGlobal("IntersectionObserver", VisibleIntersectionObserver);
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
      listProjects: async () => ({
        projects: [
          {
            id: ACME_ID,
            workspace_id: "ws-1",
            title: "Acme Corp",
            description: null,
            status: "in_progress",
            icon: null,
            color: null,
            created_at: "2026-01-01T00:00:00Z",
            updated_at: "2026-01-01T00:00:00Z",
          },
        ],
        total: 1,
      }),
      listIssueTableGroups: async (request: IssueTableGroupsRequest) => {
        groupRequests.push(request);
        return {
          query_fingerprint: "test",
          total: 3,
          // No-project first, then by title — the server's own group order.
          groups: [
            { key: "project:none", value: { kind: "project", project_id: null }, count: 1 },
            {
              key: `project:${ACME_ID}`,
              value: { kind: "project", project_id: ACME_ID },
              count: 1,
            },
            {
              key: `project:${GHOST_ID}`,
              value: { kind: "project", project_id: GHOST_ID },
              count: 1,
            },
          ],
          next_cursor: null,
        };
      },
      listIssueTableRows: async (request: IssueTableRowsRequest) => {
        const rows = ROWS_BY_GROUP[request.group_key ?? ""] ?? [];
        return {
          query_fingerprint: "test",
          group_key: request.group_key ?? null,
          parent_id: request.parent_id ?? null,
          total: rows.length,
          rows: rows.map((issue) => ({ issue, direct_child_count: 0 })),
          branch_total: rows.length,
          next_cursor: null,
        };
      },
    } as unknown as ApiClient);
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  function render() {
    const store = getIssueSurfaceViewStore(surfaceKey);
    store.getState().setTableGrouping("project");
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ViewStoreProvider store={store}>
          <IssueSurfaceSelectionProvider selection={selection}>
            <TableView
              serverQuery={serverQuery}
              childProgressMap={new Map()}
              search=""
              onSearchChange={() => {}}
              onLoadedIssuesChange={() => {}}
              onCreateIssue={() => {}}
              exportIssues={() => Promise.resolve([])}
              resolveExportLookups={() =>
                Promise.resolve({
                  projectMap: new Map(),
                  childProgressMap: new Map(),
                })
              }
            />
          </IssueSurfaceSelectionProvider>
        </ViewStoreProvider>
      </QueryClientProvider>,
    );
  }

  it("asks the server for project groups", async () => {
    render();
    await waitFor(() => expect(groupRequests).not.toHaveLength(0));
    expect(groupRequests[0]?.group).toEqual({ kind: "project" });
  });

  /** Group headers only exist while the branch catalog is settled, so every
   *  assertion about a header runs inside one retried block rather than after
   *  a separate await — a header read between two catalog states proves
   *  nothing. */
  async function groupHeaders(assert: () => void) {
    await waitFor(() => {
      expect(screen.getByText("Acme Corp")).toBeTruthy();
      assert();
    });
  }

  it("labels each group with the project title, never its id", async () => {
    render();
    await groupHeaders(() => {
      expect(screen.queryByText(ACME_ID)).toBeNull();
    });
  });

  it("names the no-project group instead of leaving it blank", async () => {
    render();
    await groupHeaders(() => {
      expect(screen.getByText("No project")).toBeTruthy();
    });
  });

  it("reads an unresolvable project as unavailable rather than as its id", async () => {
    render();
    await groupHeaders(() => {
      expect(screen.queryByText(GHOST_ID)).toBeNull();
      expect(screen.getByText("Unavailable value")).toBeTruthy();
    });
  });

  it("lands each row under its own project group", async () => {
    render();
    await screen.findByText("MUL-acme");
    await screen.findByText("MUL-loose");
  });
});
