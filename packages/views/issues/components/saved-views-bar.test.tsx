/**
 * @vitest-environment jsdom
 */
import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import {
  getIssueSurfaceViewStore,
  pruneIssueSurfaceViewStates,
} from "@multica/core/issues/stores/surface-view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import type { IssueView } from "@multica/core/issue-views";
import type { IssueViewDefinition } from "@multica/core/issues/stores/view-store";
import { SavedViewsBar } from "./saved-views-bar";

const { navigation, viewList } = vi.hoisted(() => ({
  navigation: {
    pathname: "/acme/issues",
    replace: vi.fn(),
    searchParams: new URLSearchParams("view=view-1"),
  },
  viewList: {
    current: {
      views: [] as IssueView[],
      default_view_id: null as string | null,
    },
    isFetching: false,
  },
}));

vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));
vi.mock("../../i18n", () => ({ useT: () => ({ t: () => "translated" }) }));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    ...navigation,
    getShareableUrl: (path: string) => path,
  }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/issue-views/queries", () => ({
  issueViewListOptions: () => ({ queryKey: ["views"] }),
}));
vi.mock("@multica/core/pins/queries", () => ({
  pinListOptions: () => ({ queryKey: ["pins"] }),
}));

vi.mock("@multica/core/issue-views/mutations", () => {
  const mutation = () => ({ isPending: false, mutate: vi.fn() });
  return {
    defaultIssueViewRequest: (
      scope: Record<string, unknown>,
      viewId: string | null,
    ) => ({ ...scope, view_id: viewId }),
    useCreateIssueView: mutation,
    useDeleteIssueView: mutation,
    useDuplicateIssueView: mutation,
    useSetDefaultIssueView: mutation,
    useUpdateIssueView: mutation,
  };
});
vi.mock("@multica/core/pins/mutations", () => {
  const mutation = () => ({ isPending: false, mutate: vi.fn() });
  return { useCreatePin: mutation, useDeletePin: mutation };
});
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) =>
    queryKey[0] === "views"
      ? {
          data: viewList.current,
          error: null,
          isPending: false,
          isFetching: viewList.isFetching,
        }
      : { data: [], error: null, isPending: false, isFetching: false },
}));

function definition(
  overrides: Partial<IssueViewDefinition> = {},
): IssueViewDefinition {
  return {
    version: 1,
    viewMode: "board",
    grouping: "status",
    statusFilters: [],
    priorityFilters: ["urgent"],
    assigneeFilters: [],
    includeNoAssignee: false,
    creatorFilters: [],
    projectFilters: [],
    includeNoProject: false,
    labelFilters: [],
    propertyFilters: {},
    dateFilter: null,
    agentRunningFilter: false,
    sortBy: "position",
    sortDirection: "asc",
    cardProperties: {
      priority: true,
      description: true,
      assignee: true,
      startDate: true,
      dueDate: true,
      project: true,
      childProgress: true,
      labels: true,
    },
    cardPropertyIds: [],
    showSubIssues: true,
    listCollapsedStatuses: [],
    ganttZoom: "week",
    ganttShowCompleted: false,
    swimlaneGrouping: "assignee",
    swimlaneOrders: { parent: [], project: [], assignee: [] },
    collapsedSwimlanes: { parent: [], project: [], assignee: [] },
    workspaceActorKind: "agents",
    ...overrides,
  };
}

function savedView(
  overrides: Partial<IssueView> = {},
): IssueView {
  return {
    id: "view-1",
    workspace_id: "ws-1",
    creator_id: "user-1",
    name: "Launch focus",
    icon: null,
    color: null,
    scope_type: "workspace",
    scope_id: null,
    visibility: "private",
    definition: definition(),
    position: 1,
    can_edit: true,
    created_at: "2026-07-15T00:00:00Z",
    updated_at: "2026-07-15T00:00:00Z",
    ...overrides,
  };
}

beforeAll(() => {
  if (typeof globalThis.localStorage?.clear === "function") return;
  const values = new Map<string, string>();
  const storage: Storage = {
    get length() {
      return values.size;
    },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => Array.from(values.keys())[index] ?? null,
    removeItem: (key) => {
      values.delete(key);
    },
    setItem: (key, value) => {
      values.set(key, value);
    },
  };
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: storage,
  });
  Object.defineProperty(window, "localStorage", {
    configurable: true,
    value: storage,
  });
});

beforeEach(() => {
  localStorage.clear();
  pruneIssueSurfaceViewStates([]);
  navigation.replace.mockReset();
  navigation.searchParams = new URLSearchParams("view=view-1");
  viewList.current = { views: [savedView()], default_view_id: null };
  viewList.isFetching = false;
});

describe("SavedViewsBar", () => {
  it("exposes a compact custom-view control and restores the draft for built-in views", async () => {
    const surfaceKey = "workspace:saved-view-control-test";
    const store = getIssueSurfaceViewStore(surfaceKey);
    store.getState().setViewMode("list");
    store.getState().togglePriorityFilter("high");
    const onContextChange = vi.fn();

    render(
      <ViewStoreProvider store={store}>
        <SavedViewsBar
          scope={{ type: "workspace", actorKind: "all" }}
          surfaceKey={surfaceKey}
          onContextChange={onContextChange}
        >
          {({
            savedViewsControl,
            isSavedViewActive,
            selectBuiltInView,
          }) => (
            <div>
              {savedViewsControl}
              <span data-testid="saved-view-active">
                {String(isSavedViewActive)}
              </span>
              <button type="button" onClick={selectBuiltInView}>
                All built-in
              </button>
            </div>
          )}
        </SavedViewsBar>
      </ViewStoreProvider>,
    );

    await waitFor(() => expect(store.getState().viewMode).toBe("board"));
    expect(screen.getByText("Launch focus")).toBeInTheDocument();
    expect(screen.getByTestId("saved-view-active")).toHaveTextContent("true");

    fireEvent.click(screen.getByText("Launch focus"));
    await waitFor(() =>
      expect(screen.getAllByText("Launch focus")).toHaveLength(2),
    );
    fireEvent.keyDown(document, { key: "Escape" });

    navigation.replace.mockImplementation((path: string) => {
      navigation.searchParams = new URL(
        path,
        "https://multica.test",
      ).searchParams;
    });
    fireEvent.click(screen.getByRole("button", { name: "All built-in" }));

    expect(navigation.replace).toHaveBeenCalledWith("/acme/issues");
    expect(store.getState().viewMode).toBe("list");
    expect(store.getState().priorityFilters).toEqual(["high"]);
    expect(onContextChange).toHaveBeenLastCalledWith({
      workspaceActorKind: "all",
    });
  });

  it("opens the per-surface default when no explicit view is in the URL", async () => {
    navigation.searchParams = new URLSearchParams();
    viewList.current = { views: [savedView()], default_view_id: "view-1" };
    const surfaceKey = "workspace:saved-view-default-test";
    const store = getIssueSurfaceViewStore(surfaceKey);

    render(
      <ViewStoreProvider store={store}>
        <SavedViewsBar
          scope={{ type: "workspace", actorKind: "all" }}
          surfaceKey={surfaceKey}
        />
      </ViewStoreProvider>,
    );

    await waitFor(() =>
      expect(navigation.replace).toHaveBeenCalledWith(
        "/acme/issues?view=view-1",
      ),
    );
  });

  it("restores both the local draft and surface context on unmount", async () => {
    const surfaceKey = "workspace:saved-view-test";
    const store = getIssueSurfaceViewStore(surfaceKey);
    store.getState().setViewMode("list");
    store.getState().togglePriorityFilter("high");
    const onContextChange = vi.fn();

    const renderBar = (actorKind: "all" | "agents") => (
      <ViewStoreProvider store={store}>
        <SavedViewsBar
          scope={{ type: "workspace", actorKind }}
          surfaceKey={surfaceKey}
          onContextChange={onContextChange}
        />
      </ViewStoreProvider>
    );
    const result = render(renderBar("all"));

    await waitFor(() => expect(store.getState().viewMode).toBe("board"));
    expect(store.getState().priorityFilters).toEqual(["urgent"]);
    expect(onContextChange).toHaveBeenCalledWith({ workspaceActorKind: "agents" });

    result.rerender(renderBar("agents"));
    result.unmount();

    expect(store.getState().viewMode).toBe("list");
    expect(store.getState().priorityFilters).toEqual(["high"]);
    expect(onContextChange).toHaveBeenLastCalledWith({ workspaceActorKind: "all" });
  });

  it("does not clobber dirty edits when the active row gets a new revision", async () => {
    const surfaceKey = "workspace:saved-view-revision-test";
    const store = getIssueSurfaceViewStore(surfaceKey);
    const renderBar = () => (
      <ViewStoreProvider store={store}>
        <SavedViewsBar
          scope={{ type: "workspace", actorKind: "agents" }}
          surfaceKey={surfaceKey}
        />
      </ViewStoreProvider>
    );
    const result = render(renderBar());
    await waitFor(() => expect(store.getState().priorityFilters).toEqual(["urgent"]));

    store.getState().togglePriorityFilter("medium");
    viewList.current = {
      views: [savedView({ name: "Renamed", updated_at: "2026-07-15T01:00:00Z" })],
      default_view_id: null,
    };
    result.rerender(renderBar());

    await waitFor(() =>
      expect(store.getState().priorityFilters).toEqual(["urgent", "medium"]),
    );
  });
});
