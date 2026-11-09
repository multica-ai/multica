import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { IssueView } from "@multica/core/api/schemas";
import { myIssuesViewStore } from "@multica/core/issues/stores/my-issues-view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import { issueViewSeedDefinition } from "@multica/core/issue-views/definition";
import { useIssueViewRenameStore } from "@multica/core/issue-views/rename-store";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import type { IssueSurfaceController } from "../issues/surface/use-issue-surface-controller";
import { IssueViewHeader } from "./view-header";

const copyLink = vi.hoisted(() => vi.fn());
vi.mock("../links/components/links-bar", () => ({ LinksBar: () => null }));
const rename = vi.hoisted(() => vi.fn());
const active = vi.hoisted(() => ({ view: null as IssueView | null }));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/issue-views/use-active-view", () => ({
  useActiveIssueView: () => ({ activeView: active.view, setActive: vi.fn(), missing: false }),
}));
vi.mock("./use-view-actions", () => ({
  useIssueViewActions: () => ({
    sidebar: { isStarred: () => false, star: vi.fn(), unstar: vi.fn() },
    canManage: () => true,
    copyLink,
    rename,
    save: vi.fn(),
    remove: vi.fn(),
    create: vi.fn(),
  }),
}));
vi.mock("../issues/components/filter-chips-bar", () => ({
  useFilterChips: () => ({ chips: [] }),
  ChipSpan: () => null,
}));
vi.mock("../issues/components/issues-header", () => ({
  IssueDisplayControls: () => null,
  IssueFilterMenu: ({ trigger, open }: { trigger: React.ReactNode; open?: boolean }) => (
    <>
      {trigger}
      {open && <div role="menu" aria-label="Filter menu" />}
    </>
  ),
}));

const adapterFor = (pathname: string, search = ""): NavigationAdapter => ({
  pathname,
  searchParams: new URLSearchParams(search),
  hash: "",
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  getShareableUrl: (path) => path,
});

const controller = {
  hasActiveFilters: false,
  isRefreshing: false,
  surfaceIssues: [],
  workingAgents: [],
  allowGantt: false,
  facetCountsExact: false,
  openCreateIssue: vi.fn(),
} as unknown as IssueSurfaceController;

function savedView(definition: Partial<Pick<IssueView, "query" | "display">> = {}): IssueView {
  return {
    id: "view-1",
    workspace_id: "ws-1",
    name: "Untitled view",
    scope_type: "workspace",
    scope_id: null,
    scope_variant: null,
    visibility: "private",
    owner_id: "user-1",
    definition_version: 1,
    revision: 1,
    ...issueViewSeedDefinition({ query: {}, display: {} }),
    ...definition,
  } as unknown as IssueView;
}

function renderHeader(kind: "workspace" | "my", store: string, adapter = adapterFor("/acme/issues")) {
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <ViewStoreProvider store={getIssueSurfaceViewStore(store)}>
        <IssueViewHeader controller={controller} base={{ kind }} />
      </ViewStoreProvider>
    </NavigationProvider>,
  );
}

beforeEach(() => {
  copyLink.mockClear();
  rename.mockClear();
  active.view = null;
  useIssueViewRenameStore.setState({ target: null });
  myIssuesViewStore.setState({ scope: "assigned" });
});

describe("IssueViewHeader copy link", () => {
  it.each([
    {
      kind: "workspace",
      title: "All issues",
      pathname: "/acme/issues",
      search: "",
      expected: "/acme/issues",
    },
    {
      kind: "my",
      title: "My Issues",
      pathname: "/acme/my-issues",
      search: "",
      expected: "/acme/my-issues",
    },
    {
      kind: "my",
      title: "My agents' work",
      pathname: "/acme/my-issues",
      search: "scope=agents",
      expected: "/acme/my-issues?scope=agents",
    },
  ] as const)("copies the URL for $title", async ({ kind, title, pathname, search, expected }) => {
    renderHeader(kind, "header-copy-link", adapterFor(pathname, search));

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: title }));
    await user.click(await screen.findByRole("menuitem", { name: "Copy link" }));
    expect(copyLink).toHaveBeenCalledWith(expected);
  });
});

describe("IssueViewHeader setup of a new view", () => {
  it("opens the filter menu once the new view is named with Enter", async () => {
    active.view = savedView();
    useIssueViewRenameStore.setState({ target: { viewId: "view-1", surface: "header" } });
    renderHeader("workspace", "header-setup-enter");

    expect(screen.getByTestId("view-setup-bar")).toHaveTextContent(
      "No conditions yet, so this view shows every issue",
    );
    expect(screen.queryByRole("menu", { name: "Filter menu" })).toBeNull();

    const user = userEvent.setup();
    await user.clear(screen.getByRole("textbox", { name: "Rename" }));
    await user.type(screen.getByRole("textbox", { name: "Rename" }), "Open bugs{Enter}");

    expect(rename).toHaveBeenCalledWith(active.view, "Open bugs");
    expect(useIssueViewRenameStore.getState().target).toBeNull();
    expect(screen.getByRole("menu", { name: "Filter menu" })).toBeInTheDocument();
  });

  it("leaves the menu closed when naming ends another way", async () => {
    active.view = savedView();
    useIssueViewRenameStore.setState({ target: { viewId: "view-1", surface: "header" } });
    renderHeader("workspace", "header-setup-escape");

    const user = userEvent.setup();
    await user.type(screen.getByRole("textbox", { name: "Rename" }), "{Escape}");

    expect(rename).not.toHaveBeenCalled();
    expect(screen.queryByRole("menu", { name: "Filter menu" })).toBeNull();
    expect(screen.getByRole("button", { name: "Add filter" })).toBeInTheDocument();
  });

  it("shows no setup bar once the view saves a condition", () => {
    active.view = savedView({ query: { labelFilters: ["bug"] } });
    renderHeader("workspace", "header-setup-defined");

    expect(screen.queryByTestId("view-setup-bar")).toBeNull();
  });
});
