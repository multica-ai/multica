import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import type { Squad } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { SquadsPage } from "./squads-page";

// Squad creation is workspace owner/admin only (CreateSquad backend gate).
// These tests pin the entry-point gating: the "New Squad" trigger must be
// hidden for a plain workspace member and shown for admins/owners.

const mocks = vi.hoisted(() => ({
  squads: [] as Squad[],
  members: [] as Array<{ user_id: string; name: string; role: string }>,
  agents: [] as Array<{ id: string; name: string }>,
  openModal: vi.fn(),
  viewState: {
    scope: "all",
    setScope: vi.fn(),
    sortField: "name",
    sortDirection: "asc",
    hiddenColumns: [] as string[],
    toggleSort: vi.fn(),
    setSortField: vi.fn(),
    setSortDirection: vi.fn(),
    toggleColumn: vi.fn(),
    filters: { leaders: [], creators: [] },
    toggleFilter: vi.fn(),
    clearFilters: vi.fn(),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey?.[0];
    if (key === "squads") return { data: mocks.squads, isLoading: false };
    if (key === "members") return { data: mocks.members, isLoading: false };
    if (key === "agents") return { data: mocks.agents, isLoading: false };
    return { data: [], isLoading: false };
  },
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
  useMutation: () => ({ mutate: vi.fn(), isPending: false }),
}));

vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1" }),
  useWorkspacePaths: () => ({
    squadDetail: (id: string) => `/test-workspace/squads/${id}`,
  }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  squadListOptions: () => ({ queryKey: ["squads"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  workspaceKeys: { squads: () => ["squads"] },
}));

vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (u: string) => u,
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/api", () => ({ api: { deleteSquad: vi.fn() } }));

vi.mock("@multica/core/modals", () => ({
  useModalStore: { getState: () => ({ open: mocks.openModal }) },
}));

vi.mock("@multica/core/squads/stores", () => ({
  SQUAD_SCOPES: ["mine", "all"],
  SQUAD_DEFAULT_HIDDEN_COLUMNS: ["created"],
  useSquadsViewStore: (selector: (state: unknown) => unknown) =>
    selector(mocks.viewState),
}));

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/test-workspace/squads",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderSquads() {
  renderWithI18n(
    <NavigationProvider value={makeAdapter()}>
      <SquadsPage />
    </NavigationProvider>,
  );
}

beforeEach(() => {
  mocks.squads = [];
  mocks.agents = [];
  mocks.openModal.mockClear();
});

describe("SquadsPage create-squad gating", () => {
  it("hides the New Squad trigger for a plain workspace member", () => {
    mocks.members = [{ user_id: "user-1", name: "User One", role: "member" }];
    renderSquads();

    // The empty state still renders so members can see there are no squads.
    expect(screen.getByText(/no squads yet/i)).toBeInTheDocument();
    // But no create entry point (neither the header nor empty-state button).
    expect(
      screen.queryAllByRole("button", { name: /new squad/i }),
    ).toHaveLength(0);
  });

  it("shows the New Squad trigger for a workspace admin", () => {
    mocks.members = [{ user_id: "user-1", name: "User One", role: "admin" }];
    renderSquads();

    expect(
      screen.getAllByRole("button", { name: /new squad/i }).length,
    ).toBeGreaterThan(0);
  });
});
