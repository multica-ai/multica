import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { IssueView } from "@multica/core/api/schemas";
import { renderWithI18n } from "../test/i18n";
import { useActiveIssueViewStore } from "@multica/core/issue-views/active-view-store";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { AllViewsPage } from "./all-views-page";

const WORKSPACE_KEY = "ws-1:workspace";

const mocks = vi.hoisted(() => ({
  views: [] as IssueView[],
  starred: new Set<string>(),
  star: vi.fn(),
  unstar: vi.fn(),
  open: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: ({ queryKey }: { queryKey: unknown[] }) => ({
    data: queryKey.includes("views") ? mocks.views : [],
  }),
}));
vi.mock("@multica/core/issue-views/queries", () => ({
  issueViewEverywhereOptions: () => ({ queryKey: ["views"] }),
}));
vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects"] }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    issues: () => "/acme/issues",
    myIssues: () => "/acme/my-issues",
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: { user: { id: string } }) => unknown) => select({ user: { id: "me" } }),
}));
vi.mock("../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("./use-view-actions", () => ({
  useIssueViewActions: () => ({
    sidebar: {
      isStarred: (id: string) => mocks.starred.has(id),
      star: mocks.star,
      unstar: mocks.unstar,
    },
    canManage: () => true,
    open: mocks.open,
    create: vi.fn(),
    duplicate: vi.fn(),
    remove: vi.fn(),
    copyLink: vi.fn(),
  }),
}));

function view(id: string, name: string, patch: Partial<IssueView> = {}): IssueView {
  return {
    id,
    name,
    workspace_id: "ws-1",
    owner_id: "me",
    scope_type: "workspace",
    scope_id: null,
    scope_variant: null,
    visibility: "private",
    definition_version: 1,
    query: {},
    display: {},
    revision: 1,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    ...patch,
  } as IssueView;
}

function renderPage(overrides: Partial<NavigationAdapter> = {}) {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/views",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
    ...overrides,
  };
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <AllViewsPage />
    </NavigationProvider>,
  );
  return adapter;
}

beforeEach(() => {
  mocks.views = [
    view("v1", "Needs review", { query: { statusFilters: ["in_review"], labelFilters: ["l1"] } }),
    view("v2", "Team backlog", { owner_id: "someone", visibility: "workspace" }),
  ];
  mocks.starred = new Set(["builtin:my_issues", "view:v1"]);
  mocks.star.mockReset();
  mocks.unstar.mockReset();
  mocks.open.mockReset();
  useActiveIssueViewStore.setState({ active: {} });
});

describe("AllViewsPage", () => {
  it("lists built-in and saved views with their filter counts", () => {
    renderPage();
    expect(screen.getByRole("button", { name: "Add All issues to sidebar" })).toBeInTheDocument();
    expect(screen.getByText("Needs review")).toBeInTheDocument();
    expect(screen.getByText("Team backlog")).toBeInTheDocument();
    expect(screen.getByText("2")).toBeInTheDocument();
  });

  it("narrows by tab and search", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(screen.getByRole("button", { name: /^Shared/ }));
    expect(screen.queryByText("Needs review")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add All issues to sidebar" })).not.toBeInTheDocument();
    expect(screen.getByText("Team backlog")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^All/ }));
    await user.type(screen.getByRole("textbox", { name: "Search views" }), "review");
    expect(screen.getByText("Needs review")).toBeInTheDocument();
    expect(screen.queryByText("Team backlog")).not.toBeInTheDocument();
  });

  it("stars and unstars without opening the view", async () => {
    const user = userEvent.setup();
    const { push } = renderPage();

    await user.click(screen.getByRole("button", { name: "Remove Needs review from sidebar" }));
    expect(mocks.unstar).toHaveBeenCalledWith("view:v1");
    await user.click(screen.getByRole("button", { name: "Add Team backlog to sidebar" }));
    expect(mocks.star).toHaveBeenCalledWith("view:v2");
    expect(push).not.toHaveBeenCalled();
    expect(useActiveIssueViewStore.getState().active).toEqual({});

    await user.click(screen.getByText("Team backlog"));
    expect(push).toHaveBeenCalledWith("/acme/issues?view=v2");
    expect(useActiveIssueViewStore.getState().active[WORKSPACE_KEY]).toBe("v2");
  });

  it("opens a view in place for a shift-click, which rows never hand to the browser", async () => {
    const user = userEvent.setup();
    const { push } = renderPage();

    await user.keyboard("{Shift>}");
    await user.click(screen.getByText("Team backlog"));
    await user.keyboard("{/Shift}");

    expect(push).toHaveBeenCalledWith("/acme/issues?view=v2");
    expect(useActiveIssueViewStore.getState().active[WORKSPACE_KEY]).toBe("v2");
  });

  it("leaves the open view alone when a view or All issues opens in a new tab", async () => {
    const user = userEvent.setup();
    useActiveIssueViewStore.setState({ active: { [WORKSPACE_KEY]: "v-open" } });
    const openInNewTab = vi.fn();
    renderPage({ openInNewTab });

    const allIssuesRow = screen.getByRole("button", { name: "Add All issues to sidebar" }).closest('[role="row"]')!;
    await user.keyboard("{Meta>}");
    await user.click(screen.getByText("Team backlog"));
    await user.click(allIssuesRow);
    await user.keyboard("{/Meta}");

    expect(openInNewTab).toHaveBeenCalledWith("/acme/issues?view=v2", "Team backlog");
    expect(openInNewTab).toHaveBeenCalledWith("/acme/issues", "All issues");
    expect(useActiveIssueViewStore.getState().active[WORKSPACE_KEY]).toBe("v-open");
  });
});
