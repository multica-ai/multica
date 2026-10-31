import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { createStore } from "zustand/vanilla";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { IssueViewSchema, type IssueView } from "@multica/core/api/schemas";
import { issueViewSeedDefinition } from "@multica/core/issue-views/definition";
import { seedIssueSurfaceViewState, clearIssueSurfaceViewState, getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import { viewStoreSlice, type IssueViewState } from "@multica/core/issues/stores/view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { NavigationProvider } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import type { IssueSurfaceController } from "../issues/surface/use-issue-surface-controller";
import { IssueViewHeader } from "./view-header";

const fixture = vi.hoisted(() => ({ active: null as IssueView | null }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: { user: { id: string } }) => unknown) => select({ user: { id: "me" } }),
}));
vi.mock("@multica/core/issue-views/use-active-view", () => ({
  useActiveIssueView: () => ({ activeView: fixture.active, setActive: vi.fn(), missing: false }),
}));
vi.mock("@multica/core/issue-views/sidebar-views", () => ({
  savedViewItemId: (id: string) => `view:${id}`,
  builtinViewItemId: (id: string) => `builtin:${id}`,
  useSidebarViews: () => ({ isStarred: () => false, star: vi.fn(), unstar: vi.fn() }),
}));

const layout = {
  swimlaneProjectAxis: "project-columns",
  swimlaneProjectColumnOrder: ["p2", "p1", "none"],
  collapsedSwimlaneStatusRows: ["qa"],
} as const;
const savedView = (patch: Partial<IssueView> = {}) => IssueViewSchema.parse({
  id: "axis-view", workspace_id: "ws-1", owner_id: "me", name: "Project axes",
  scope_type: "workspace", scope_id: null, visibility: "private", revision: 7,
  query: { agentRunningFilter: true },
  display: { viewMode: "swimlane", swimlaneGrouping: "project", ...layout },
  ...patch,
});

function renderHeader() {
  const store = createStore<IssueViewState>()(viewStoreSlice);
  store.setState({
    viewMode: "swimlane", swimlaneGrouping: "project", agentRunningFilter: true,
    ...layout, swimlaneProjectColumnOrder: [...layout.swimlaneProjectColumnOrder],
    collapsedSwimlaneStatusRows: [...layout.collapsedSwimlaneStatusRows],
  });
  const create = vi.fn(async (request) => savedView(request));
  const update = vi.fn(async (_id, request) => savedView(request));
  setApiInstance(new Proxy({ createIssueView: create, updateIssueView: update }, {
    get: (target, key) => key in target ? target[key as keyof typeof target] : async () => [],
  }) as unknown as ApiClient);
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const controller = {
    hasActiveFilters: true, isRefreshing: false, surfaceIssues: [], workingAgents: [],
    allowGantt: false, facetCountsExact: false, openCreateIssue: vi.fn(),
  } as unknown as IssueSurfaceController;
  renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <NavigationProvider value={{ pathname: "/acme/issues", searchParams: new URLSearchParams(), hash: "", push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: (p) => p }}>
        <ViewStoreProvider store={store}><IssueViewHeader controller={controller} base={{ kind: "workspace" }} /></ViewStoreProvider>
      </NavigationProvider>
    </QueryClientProvider>,
  );
  return { store, create, update };
}

beforeEach(() => { fixture.active = null; });
afterEach(() => { cleanup(); clearIssueSurfaceViewState("view:axis-view"); });

describe("saved project axes in the current view header", () => {
  it("creates a view with all three layout fields and the Agent status filter", async () => {
    const { create } = renderHeader();
    await userEvent.click(screen.getByRole("button", { name: "Save as view" }));
    await waitFor(() => expect(create).toHaveBeenCalledOnce());
    const request = create.mock.calls[0]![0];
    expect(request.display).toMatchObject(layout);
    expect(request.query.agentRunningFilter).toBe(true);
    const response = savedView(request);
    const seed = issueViewSeedDefinition(response);
    seedIssueSurfaceViewState("view:axis-view", { ...seed.query, ...seed.display });
    const loaded = getIssueSurfaceViewStore("view:axis-view");
    expect(loaded.getState()).toMatchObject({ ...layout, agentRunningFilter: true });
    loaded.getState().setSwimlaneProjectAxis("status-columns");
    seedIssueSurfaceViewState("view:axis-view", { ...seed.query, ...seed.display });
    expect(loaded.getState().swimlaneProjectAxis).toBe("status-columns");
  });

  it("updates the saved definition after an axis edit, and resets to its stored layout", async () => {
    fixture.active = savedView({ display: { viewMode: "swimlane", swimlaneGrouping: "project" } });
    const { store, update } = renderHeader();
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(update).toHaveBeenCalledOnce());
    expect(update.mock.calls[0]).toEqual(["axis-view", expect.objectContaining({
      expected_revision: 7, display: expect.objectContaining(layout),
      query: expect.objectContaining({ agentRunningFilter: true }),
    })]);
    await userEvent.click(screen.getByRole("button", { name: "Reset" }));
    expect(store.getState()).toMatchObject({
      swimlaneProjectAxis: "status-columns", swimlaneProjectColumnOrder: [], collapsedSwimlaneStatusRows: [],
      agentRunningFilter: true,
    });
    expect(screen.queryByRole("button", { name: "Save" })).not.toBeInTheDocument();
    act(() => store.getState().setSwimlaneProjectAxis("project-columns"));
    expect(screen.getByRole("button", { name: "Save" })).toBeInTheDocument();
  });
});
