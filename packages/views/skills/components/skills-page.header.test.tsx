// @vitest-environment jsdom

import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SkillSummary } from "@multica/core/types";
import {
  EMPTY_SKILL_FILTERS,
  useSkillsViewStore,
} from "@multica/core/skills/stores";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

const mocks = vi.hoisted(() => ({
  skills: [] as SkillSummary[],
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
  useSkillsViewStore.setState({
    sortField: "name",
    sortDirection: "asc",
    hiddenColumns: [],
    filters: EMPTY_SKILL_FILTERS,
    search: "",
  });
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

  it("offers search and hides Reset until the list is narrowed", () => {
    const header = renderPage();
    expect(within(header).queryByRole("textbox")).toBeNull();
    expect(within(header).getByRole("button", { name: "Search skills" })).toBeInTheDocument();
    expect(within(header).queryByRole("button", { name: "Reset" })).toBeNull();
  });

  it("resets the filters", async () => {
    const user = userEvent.setup();
    useSkillsViewStore.getState().toggleFilter("usage", "used");
    const header = renderPage();
    await user.click(within(header).getByRole("button", { name: "Reset" }));
    expect(useSkillsViewStore.getState().filters).toEqual(EMPTY_SKILL_FILTERS);
    expect(screen.getByText("mine-skill")).toBeInTheDocument();
    expect(screen.getByText("team-skill")).toBeInTheDocument();
  });

  it("searches the list and resets the query from the header", async () => {
    const user = userEvent.setup();
    const header = renderPage();
    await user.click(within(header).getByRole("button", { name: "Search skills" }));
    await user.type(within(header).getByRole("textbox", { name: "Search skills" }), "mine");
    expect(screen.getByText("mine-skill")).toBeInTheDocument();
    expect(screen.queryByText("team-skill")).toBeNull();

    await user.click(within(header).getByRole("button", { name: "Reset" }));
    expect(useSkillsViewStore.getState().search).toBe("");
    expect(screen.getByText("team-skill")).toBeInTheDocument();
  });

  it("combines search with scope and offers to clear a search with no matches", async () => {
    const user = userEvent.setup();
    const header = renderPage();
    await user.click(within(header).getByRole("button", { name: "Search skills" }));
    await user.type(within(header).getByRole("textbox", { name: "Search skills" }), "team");
    await user.click(within(header).getByRole("button", { name: /All skills/ }));
    await user.click(await screen.findByRole("menuitemradio", { name: /My skills/ }));
    expect(screen.queryByText("team-skill")).toBeNull();
    expect(screen.queryByText("mine-skill")).toBeNull();
    expect(screen.getByText('No skills match "team".')).toBeInTheDocument();

    await user.click(screen.getByText("Clear search", { selector: "button" }));
    expect(screen.getByText("mine-skill")).toBeInTheDocument();
    expect(screen.queryByText("team-skill")).toBeNull();
  });

  it("combines search with filters and clears both from the empty state", async () => {
    const user = userEvent.setup();
    useSkillsViewStore.getState().toggleFilter("usage", "used");
    useSkillsViewStore.getState().setSearch("mine");
    renderPage();
    expect(screen.queryByText("mine-skill")).toBeNull();
    expect(screen.getByText('No skills match "mine" in this filter.')).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Clear filters and search" }));
    expect(useSkillsViewStore.getState().search).toBe("");
    expect(useSkillsViewStore.getState().filters).toEqual(EMPTY_SKILL_FILTERS);
    expect(screen.getByText("mine-skill")).toBeInTheDocument();
    expect(screen.getByText("team-skill")).toBeInTheDocument();
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

  it("introduces skills when there are none and adds the first one from the intro", async () => {
    const user = userEvent.setup();
    mocks.skills = [];
    renderPage();

    const intro = screen.getByRole("region", { name: "Turn how your team works into skills" });
    await user.click(within(intro).getByRole("button", { name: "New skill" }));
    await user.click(await screen.findByRole("menuitem", { name: "Create manually" }));
    expect(screen.getByTestId("create-dialog")).toHaveTextContent("manual");
  });
});
