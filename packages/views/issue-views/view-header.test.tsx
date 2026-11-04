import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { myIssuesViewStore } from "@multica/core/issues/stores/my-issues-view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import { getIssueSurfaceViewStore } from "@multica/core/issues/stores/surface-view-store";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import type { IssueSurfaceController } from "../issues/surface/use-issue-surface-controller";
import { IssueViewHeader } from "./view-header";

const copyLink = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});
vi.mock("@multica/core/issue-views/use-active-view", () => ({
  useActiveIssueView: () => ({ activeView: null, setActive: vi.fn(), missing: false }),
}));
vi.mock("./use-view-actions", () => ({
  useIssueViewActions: () => ({
    sidebar: { isStarred: () => false, star: vi.fn(), unstar: vi.fn() },
    canManage: () => true,
    copyLink,
    remove: vi.fn(),
    create: vi.fn(),
  }),
}));
vi.mock("../issues/components/filter-chips-bar", () => ({
  useFilterChips: () => ({ chips: [] }),
}));
vi.mock("../issues/components/issues-header", () => ({
  IssueDisplayControls: () => null,
  IssueFilterMenu: () => null,
}));

beforeEach(() => {
  copyLink.mockClear();
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
    const adapter: NavigationAdapter = {
      pathname,
      searchParams: new URLSearchParams(search),
      hash: "",
      push: vi.fn(),
      replace: vi.fn(),
      back: vi.fn(),
      getShareableUrl: (path) => path,
    };
    const controller = {
      hasActiveFilters: false,
      isRefreshing: false,
      surfaceIssues: [],
      workingAgents: [],
      allowGantt: false,
      facetCountsExact: false,
      openCreateIssue: vi.fn(),
    } as unknown as IssueSurfaceController;

    renderWithI18n(
      <NavigationProvider value={adapter}>
        <ViewStoreProvider store={getIssueSurfaceViewStore("header-copy-link")}>
          <IssueViewHeader controller={controller} base={{ kind }} />
        </ViewStoreProvider>
      </NavigationProvider>,
    );

    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: title }));
    await user.click(await screen.findByRole("menuitem", { name: "Copy link" }));
    expect(copyLink).toHaveBeenCalledWith(expected);
  });
});
