import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Squad } from "@multica/core/types";
import type { SquadsScope } from "@multica/core/squads/stores";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { SquadsPage } from "./squads-page";

const mocks = vi.hoisted(() => ({
  squads: [] as Squad[],
  openModal: vi.fn(),
  replace: vi.fn(),
  viewState: {
    scope: "all" as SquadsScope,
    sortField: "name",
    sortDirection: "asc",
    hiddenColumns: [] as string[],
    filters: { leaders: [] as string[], creators: [] as string[] },
    setScope: vi.fn(),
    toggleSort: vi.fn(),
    setSortField: vi.fn(),
    setSortDirection: vi.fn(),
    toggleColumn: vi.fn(),
    toggleFilter: vi.fn(),
    clearFilters: vi.fn(),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => ({
    data: options.queryKey?.[0] === "squads" ? mocks.squads : [],
    isLoading: false,
  }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  squadListOptions: () => ({ queryKey: ["squads"] }),
  workspaceKeys: { squads: () => ["squads"] },
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", slug: "acme" }),
  useWorkspacePaths: () => ({
    squadDetail: (id: string) => `/acme/squads/${id}`,
    agentDetail: (id: string) => `/acme/agents/${id}`,
  }),
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (select: (s: { user: { id: string } }) => unknown) => select({ user: { id: "me" } }),
}));
vi.mock("@multica/core/api", () => ({ api: {} }));
vi.mock("@multica/core/modals", () => ({
  useModalStore: { getState: () => ({ open: mocks.openModal }) },
}));
vi.mock("@multica/core/squads/stores", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/squads/stores")>()),
  useSquadsViewStore: (select: (s: typeof mocks.viewState) => unknown) => select(mocks.viewState),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));

function squad(id: string, name: string, creatorId: string): Squad {
  return {
    id,
    workspace_id: "ws-1",
    name,
    description: "",
    instructions: "",
    avatar_url: null,
    leader_id: `agent-${id}`,
    creator_id: creatorId,
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
    archived_at: null,
    archived_by: null,
  };
}

function renderPage(search = "") {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: mocks.replace,
    back: vi.fn(),
    pathname: "/acme/squads",
    searchParams: new URLSearchParams(search),
    hash: "",
    getShareableUrl: (path) => path,
  };
  return renderWithI18n(
    <NavigationProvider value={adapter}>
      <SquadsPage />
    </NavigationProvider>,
  );
}

beforeEach(() => {
  mocks.squads = [squad("s1", "Strike Team", "me"), squad("s2", "Design Crew", "someone")];
  mocks.viewState.scope = "all";
  mocks.viewState.filters = { leaders: [], creators: [] };
  mocks.viewState.setScope.mockReset();
  mocks.viewState.clearFilters.mockReset();
  mocks.openModal.mockReset();
  mocks.replace.mockReset();
});

describe("SquadsPage header", () => {
  it("titles the page with the scope and picks another scope from the title menu", async () => {
    const user = userEvent.setup();
    renderPage();

    expect(screen.getByRole("heading", { level: 1, name: "All squads" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "All squads" }));
    const mine = await screen.findByRole("menuitemradio", { name: /^My squads/ });
    expect(mine).toHaveTextContent("1");
    expect(screen.getByRole("menuitemradio", { name: /^All squads/ })).toHaveTextContent("2");
    await user.click(mine);
    expect(mocks.viewState.setScope).toHaveBeenCalledWith("mine");
  });

  it("lists only the squads in the current scope", () => {
    mocks.viewState.scope = "mine";
    renderPage();

    expect(screen.getByRole("heading", { level: 1, name: "My squads" })).toBeInTheDocument();
    expect(screen.getByText("Strike Team")).toBeInTheDocument();
    expect(screen.queryByText("Design Crew")).not.toBeInTheDocument();
  });

  it("has no search, and offers Reset only while a filter narrows the list", () => {
    renderPage();

    expect(screen.queryByRole("button", { name: /search/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Reset" })).not.toBeInTheDocument();
  });

  it("names the active filter count on the filter control, and Reset clears the filters", async () => {
    const user = userEvent.setup();
    mocks.viewState.filters = { leaders: ["agent-s1"], creators: ["me"] };
    renderPage();

    expect(screen.getByRole("button", { name: "2 filters" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Reset" }));
    expect(mocks.viewState.clearFilters).toHaveBeenCalled();
  });

  it("keeps display settings and create as icon controls on the right", async () => {
    const user = userEvent.setup();
    renderPage();

    expect(screen.getByRole("button", { name: "Display settings" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "New squad" }));
    expect(mocks.openModal).toHaveBeenCalledWith("create-squad");
  });

  it("introduces squads and offers to create the first one when there are none", async () => {
    const user = userEvent.setup();
    mocks.squads = [];
    renderPage();

    const intro = screen.getByRole("region", {
      name: "Put agents in a squad and they work like a team",
    });
    expect(screen.getByRole("link", { name: "Read the docs" })).toHaveAttribute(
      "href",
      "https://multica.ai/docs/squads",
    );
    await user.click(within(intro).getByRole("button", { name: "New squad" }));
    expect(mocks.openModal).toHaveBeenCalledWith("create-squad");
  });

  it("says so when the user created no squad, and offers all squads", async () => {
    const user = userEvent.setup();
    mocks.squads = [squad("s2", "Design Crew", "someone")];
    mocks.viewState.scope = "mine";
    renderPage();

    expect(screen.getByText("You haven't created a squad")).toBeInTheDocument();
    expect(screen.queryByText("No squads match")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "View all squads" }));
    expect(mocks.viewState.setScope).toHaveBeenCalledWith("all");
  });

  it("names an empty filter dimension instead of opening a blank menu", async () => {
    const user = userEvent.setup();
    mocks.squads = [squad("s2", "Design Crew", "someone")];
    mocks.viewState.scope = "mine";
    renderPage();

    await user.click(screen.getByRole("button", { name: "Filter" }));
    await user.click(await screen.findByRole("menuitem", { name: "Leader" }));
    expect(await screen.findByRole("menuitem", { name: "No leaders to filter by" })).toHaveAttribute(
      "aria-disabled",
      "true",
    );
  });

  it("shows the scope a link asks for in that tab only, leaving the saved scope to other tabs", () => {
    // One shared store, as across desktop tabs, which mount one at a time.
    mocks.viewState.scope = "mine";
    mocks.viewState.setScope.mockImplementation((next: SquadsScope) => {
      mocks.viewState.scope = next;
    });

    const tabA = renderPage();
    expect(screen.getByRole("heading", { level: 1, name: "My squads" })).toBeInTheDocument();
    tabA.unmount();

    const tabB = renderPage("scope=all");
    expect(screen.getByRole("heading", { level: 1, name: "All squads" })).toBeInTheDocument();
    expect(screen.getByText("Design Crew")).toBeInTheDocument();
    tabB.unmount();

    renderPage();
    expect(screen.getByRole("heading", { level: 1, name: "My squads" })).toBeInTheDocument();
    expect(mocks.viewState.setScope).not.toHaveBeenCalled();
    expect(mocks.replace).not.toHaveBeenCalled();
  });

  it("saves a scope picked on the page and keeps the tab's URL in step", async () => {
    const user = userEvent.setup();
    renderPage("scope=all&x=1");

    await user.click(screen.getByRole("button", { name: "All squads" }));
    await user.click(await screen.findByRole("menuitemradio", { name: /^My squads/ }));
    expect(mocks.viewState.setScope).toHaveBeenCalledWith("mine");
    expect(mocks.replace).toHaveBeenCalledWith("/acme/squads?scope=mine&x=1");
  });
});
