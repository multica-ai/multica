/**
 * @vitest-environment jsdom
 *
 * Board grouped by project. The server's group descriptors carry a project id
 * and nothing else, so both the column header text and the column's leading
 * glyph are resolved on the client — and `projectId` sits next to `assigneeId`
 * on BoardColumnGroup, where an unguarded fallthrough renders a project column
 * as the "no assignee" column.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import type { Issue, Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { IssueContextMenuProvider } from "../actions/issue-actions-context-menu";
import type { IssueGroupBranches } from "../surface/use-issue-group-branches";
import { BoardView } from "./board-view";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/properties", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/properties")>()),
  propertyListOptions: () => ({
    queryKey: ["properties"],
    queryFn: async () => [],
  }),
  useSetIssueProperty: () => ({ mutate: () => {} }),
  useUnsetIssueProperty: () => ({ mutate: () => {} }),
}));

vi.mock("@multica/core/workspace/hooks", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/workspace/hooks")>()),
  useActorName: () => ({ getActorName: () => "Someone" }),
}));

vi.mock("@multica/core/auth", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/auth")>()),
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "viewer-1" } }),
}));

vi.mock("@multica/core/agents", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/agents")>()),
  isAgentRuntimeBound: () => true,
  useAgentPresenceDetail: () => ({ availability: "offline", workload: null }),
}));

vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual =
    await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme" }),
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});

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

const ACME_ID = "11111111-1111-4111-8111-111111111111";
const GHOST_ID = "22222222-2222-4222-8222-222222222222";

function makeProject(id: string, title: string, icon: string | null): Project {
  return {
    id,
    workspace_id: "ws-1",
    title,
    description: null,
    icon,
    status: "in_progress",
    priority: "none",
    lead_type: null,
    lead_id: null,
    start_date: null,
    due_date: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
  };
}

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

const ISSUES = [
  makeIssue("acme", ACME_ID),
  makeIssue("ghost", GHOST_ID),
  makeIssue("loose", null),
];

const NO_PROJECT_DESCRIPTOR = {
  key: "project:none",
  value: { kind: "project" as const, project_id: null },
  count: 1,
};
const PROJECT_DESCRIPTORS = [
  {
    key: `project:${ACME_ID}`,
    value: { kind: "project" as const, project_id: ACME_ID },
    count: 4,
  },
  {
    key: `project:${GHOST_ID}`,
    value: { kind: "project" as const, project_id: GHOST_ID },
    count: 1,
  },
];

/** What `useIssueGroupBranches` hands the board for `{ kind: "project" }`. */
function makeGroupBranches(
  descriptors: IssueGroupBranches["descriptors"],
  issues: Issue[],
): IssueGroupBranches {
  return {
    enabled: true,
    descriptors,
    issues,
    pagination: {},
    total: issues.length,
    isLoading: false,
    isRefreshing: false,
    isError: false,
    hasMoreGroups: false,
    isLoadingMoreGroups: false,
    loadMoreGroups: () => {},
    retryGroups: () => {},
  };
}

class ObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

describe("Board grouped by project", () => {
  let queryClient: QueryClient;

  beforeEach(() => {
    vi.stubGlobal("IntersectionObserver", ObserverStub);
    vi.stubGlobal("ResizeObserver", ObserverStub);
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
  });

  afterEach(() => {
    cleanup();
    vi.unstubAllGlobals();
  });

  function render({
    descriptors = [NO_PROJECT_DESCRIPTOR, ...PROJECT_DESCRIPTORS],
    issues = ISSUES,
  }: {
    descriptors?: IssueGroupBranches["descriptors"];
    issues?: Issue[];
  } = {}) {
    const store = getIssueSurfaceViewStore(
      `board-project-${Math.floor(Math.random() * 1e9)}`,
    );
    store.getState().setGrouping("project");
    // Cards carry their own project chip by default, which would put the same
    // title in two places and make "the column header says X" unprovable.
    if (store.getState().cardProperties.project) {
      store.getState().toggleCardProperty("project");
    }
    renderWithI18n(
      <QueryClientProvider client={queryClient}>
        <ViewStoreProvider store={store}>
          <IssueContextMenuProvider>
          <BoardView
            issues={issues}
            visibleStatuses={["todo"]}
            hiddenStatuses={[]}
            onMoveIssue={() => {}}
            projectMap={
              new Map([[ACME_ID, makeProject(ACME_ID, "Acme Corp", "🚀")]])
            }
            groupBranches={makeGroupBranches(descriptors, issues)}
          />
          </IssueContextMenuProvider>
        </ViewStoreProvider>
      </QueryClientProvider>,
    );
  }

  it("titles each column with its project, never with its id", () => {
    render();

    expect(screen.getByText("Acme Corp")).toBeTruthy();
    expect(screen.getByText("No project")).toBeTruthy();
    expect(screen.queryByText(ACME_ID)).toBeNull();
    expect(screen.queryByText(GHOST_ID)).toBeNull();
  });

  it("keeps a No project column when the server returns no such group", () => {
    // Clearing a card's project by dragging has to stay possible in a workspace
    // where every card currently has one, so this column is not derived from
    // the result set like the others.
    render({
      descriptors: PROJECT_DESCRIPTORS,
      issues: ISSUES.filter((issue) => issue.project_id !== null),
    });

    const header = screen.getByText("No project").parentElement!;
    expect(header.textContent).toContain("0");
  });

  it("keeps exactly one No project column when the server does return it", () => {
    render();

    expect(screen.getAllByText("No project")).toHaveLength(1);
  });

  it("reads an unresolvable project the way the Table does", () => {
    // The board and the table must not describe the same project differently.
    render();

    expect(screen.getByText("Unavailable value")).toBeTruthy();
  });

  it("shows the server's group count, not the loaded card count", () => {
    render();

    // Acme holds four issues; only one is on this page.
    const header = screen.getByText("Acme Corp").parentElement!;
    expect(header.textContent).toContain("4");
  });

  it("heads a project column with that project's own icon", () => {
    render();

    // BoardColumnGroup carries projectId beside assigneeId, so a project column
    // that is not matched explicitly falls through to the ASSIGNEE heading —
    // which renders the same title text next to a no-assignee actor glyph. The
    // icon is what tells the two apart.
    const header = screen.getByText("Acme Corp").parentElement!;
    expect(header.textContent).toContain("\u{1F680}");
  });

  it("puts each card in its own project column", () => {
    render();

    const acmeColumn = screen.getByText("Acme Corp").closest("div.flex-col")!;
    expect(acmeColumn.textContent).toContain("MUL-acme");
    expect(acmeColumn.textContent).not.toContain("MUL-loose");
  });
});
