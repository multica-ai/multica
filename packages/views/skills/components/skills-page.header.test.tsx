// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SkillSummary } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

const mocks = vi.hoisted(() => ({
  skills: [] as SkillSummary[],
  viewState: {
    sortField: "name",
    sortDirection: "asc" as string,
    hiddenColumns: [] as string[],
    filters: {
      usage: [] as string[],
      origins: [] as string[],
      agents: [] as string[],
      creators: [] as string[],
      labels: [] as string[],
    },
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
    data: options.queryKey?.[0] === "skills" ? mocks.skills : [],
    isLoading: false,
    error: null,
    refetch: vi.fn(),
  }),
}));

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count }: { count: number }) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        key: index,
        start: index * 48,
        end: (index + 1) * 48,
        size: 48,
      })),
    getTotalSize: () => count * 48,
  }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
// Partial mock: SkillIcon resolves its icon from the real WORKSPACE_PAGES.
vi.mock("@multica/core/paths", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/paths")>()),
  useWorkspacePaths: () => ({
    skillDetail: (id: string) => `/acme/skills/${id}`,
  }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  skillListOptions: () => ({ queryKey: ["skills"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  selectSkillAssignments: () => new Map(),
}));
vi.mock("@multica/core/labels/queries", () => ({
  labelListOptions: () => ({ queryKey: ["labels"] }),
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
  runtimeDisplayLabel: () => "runtime",
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (u: string | null) => u,
}));
vi.mock("@multica/core/skills/stores", () => ({
  useSkillsViewStore: (selector: (state: unknown) => unknown) =>
    selector(mocks.viewState),
  DEFAULT_HIDDEN_COLUMNS: [],
}));

vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));
vi.mock("../../labels/label-manager", () => ({ LabelManager: () => null }));
vi.mock("./create-skill-dialog", () => ({
  CreateSkillDialog: ({ initialMethod }: { initialMethod?: string }) => (
    <div data-testid="create-dialog">{initialMethod}</div>
  ),
}));
vi.mock("./skill-list-actions", () => ({
  SkillBatchToolbar: () => null,
  SkillRowActions: () => null,
}));

import SkillsPage from "./skills-page";

function skill(id: string, name: string, createdBy: string): SkillSummary {
  return {
    id,
    workspace_id: "ws-1",
    name,
    description: "",
    config: {},
    created_by: createdBy,
    created_at: "2026-07-28T18:11:37Z",
    updated_at: "2026-07-28T18:14:40Z",
  };
}

function renderPage() {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/skills",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
  };
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <SkillsPage />
    </NavigationProvider>,
  );
  return screen.getByRole("banner");
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.skills = [
    skill("skill-1", "mine-skill", "user-1"),
    skill("skill-2", "team-skill", "user-2"),
  ];
});

describe("SkillsPage header", () => {
  it("titles the page with its scope and keeps every control in one row", () => {
    const header = renderPage();
    expect(
      within(header).getByRole("heading", { name: "All skills", level: 1 }),
    ).toBeInTheDocument();
    for (const name of ["Filter", "Display settings", "New skill"]) {
      expect(within(header).getByRole("button", { name })).toBeInTheDocument();
    }
    expect(screen.queryByRole("link", { name: /Learn more/ })).toBeNull();
    expect(screen.queryByText("New skill")).toBeNull();
  });

  it("switches scope from the title menu, which counts each scope", async () => {
    const user = userEvent.setup();
    const header = renderPage();

    await user.click(within(header).getByRole("button", { name: /All skills/ }));
    expect(
      await screen.findByRole("menuitemradio", { name: /All skills/ }),
    ).toHaveTextContent("2");
    const mine = screen.getByRole("menuitemradio", { name: /My skills/ });
    expect(mine).toHaveTextContent("1");

    await user.click(mine);
    expect(
      within(header).getByRole("heading", { name: "My skills", level: 1 }),
    ).toBeInTheDocument();
    expect(screen.getByText("mine-skill")).toBeInTheDocument();
    expect(screen.queryByText("team-skill")).toBeNull();
  });

  it("has no search, and offers Reset only while a filter narrows the list", () => {
    const header = renderPage();
    expect(within(header).queryByRole("textbox")).toBeNull();
    expect(within(header).queryByRole("button", { name: /search/i })).toBeNull();
    expect(within(header).queryByRole("button", { name: "Reset" })).toBeNull();
  });

  it("resets the filters", async () => {
    const user = userEvent.setup();
    mocks.viewState.filters.usage = ["used"];
    try {
      const header = renderPage();
      await user.click(within(header).getByRole("button", { name: "Reset" }));
      expect(mocks.viewState.clearFilters).toHaveBeenCalled();
    } finally {
      mocks.viewState.filters.usage = [];
    }
  });

  it("offers every way to add a skill from the + menu and opens the dialog on it", async () => {
    const user = userEvent.setup();
    const header = renderPage();

    await user.click(within(header).getByRole("button", { name: "New skill" }));
    const menu = await screen.findByRole("menu");
    expect(
      within(menu).getAllByRole("menuitem").map((item) => item.textContent),
    ).toEqual(["Create manually", "Import from local", "Import from URL", "Copy from runtime"]);

    await user.click(within(menu).getByRole("menuitem", { name: "Import from URL" }));
    expect(screen.getByTestId("create-dialog")).toHaveTextContent("url");
  });
});
