import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Squad } from "@multica/core/types";
import type { SquadsScope } from "@multica/core/squads/stores";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { SquadsPage } from "./squads-page";

const mocks = vi.hoisted(() => ({
  squads: [] as Squad[],
  openModal: vi.fn(),
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

function renderPage() {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/squads",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (path) => path,
  };
  renderWithI18n(
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

  it("offers to create the first squad from the empty state", async () => {
    const user = userEvent.setup();
    mocks.squads = [];
    renderPage();

    expect(screen.getByText("No squads yet.")).toBeInTheDocument();
    const buttons = screen.getAllByRole("button", { name: "New squad" });
    await user.click(buttons[buttons.length - 1]!);
    expect(mocks.openModal).toHaveBeenCalledWith("create-squad");
  });
});
