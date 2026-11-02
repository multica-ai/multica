// @vitest-environment jsdom

// Agents-page folder explorer — custom + squad folders with persistent
// in-place expansion, flat search/filter/archived views, move permission
// gates, and squad-change reflection.

import React, { useMemo, useState, useSyncExternalStore } from "react";
import { beforeEach, describe, expect, it, vi, type Mock } from "vitest";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { SupportedLocale } from "@multica/core/i18n";
import type { Agent } from "@multica/core/types";
import type { AgentFolderTree } from "@multica/core/api/schemas";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";

const apiMock = vi.hoisted(() => ({
  createAgentFolder: vi.fn(),
  updateAgentFolder: vi.fn(),
  deleteAgentFolder: vi.fn(),
  setAgentPlacement: vi.fn(),
  setAgentSquadPlacement: vi.fn(),
  archiveAgent: vi.fn(),
  restoreAgent: vi.fn(),
}));

const mocks = vi.hoisted(() => ({
  searchListeners: new Set<() => void>(),
  agents: [] as Agent[],
  folderTree: null as AgentFolderTree | null | undefined,
  folderTreeError: false,
  folderTreeFailures: 0,
  refetchTree: vi.fn(),
  currentUserId: "user-1",
  members: [] as { user_id: string; role: string }[],
  virtualizerCount: 0,
  virtualizerKeys: [] as (string | number)[],
  rowSizes: [] as number[],
  viewState: {
    scope: "all",
    search: "",
    setSearch: vi.fn(),
    sortField: "name",
    sortDirection: "asc" as string,
    hiddenColumns: [] as string[],
    filters: {
      availability: [] as string[],
      runtimes: [] as string[],
      owners: [] as string[],
      models: [] as string[],
      access: [] as string[],
    },
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
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey ?? [];
    if (key[0] === "agents") {
      return { data: mocks.agents, isLoading: false, error: null, refetch: vi.fn() };
    }
    if (key.includes("agent-folders")) {
      return {
        data: mocks.folderTree,
        isLoading: false,
        error: mocks.folderTreeError ? new Error("tree unavailable") : null,
        failureCount: mocks.folderTreeFailures,
        refetch: mocks.refetchTree,
      };
    }
    if (key[0] === "members") {
      return { data: mocks.members, isLoading: false, error: null, refetch: vi.fn() };
    }
    return { data: [], isLoading: false, isPending: false, error: null, refetch: vi.fn() };
  },
  useQueryClient: () => ({
    invalidateQueries: vi.fn(),
    setQueryData: vi.fn(),
  }),
}));

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({
    count,
    estimateSize,
    getItemKey,
  }: {
    count: number;
    estimateSize: (index: number) => number;
    getItemKey: (index: number) => string | number;
  }) => {
    mocks.rowSizes = Array.from({ length: count }, (_, index) => estimateSize(index));
    mocks.virtualizerCount = count;
    mocks.virtualizerKeys = Array.from({ length: count }, (_, index) => getItemKey(index));
    return {
      getVirtualItems: () =>
        Array.from({ length: count }, (_, index) => ({
          index,
          key: getItemKey(index),
          start: index * 64,
          end: (index + 1) * 64,
          size: 64,
        })),
      getTotalSize: () => count * 64,
    };
  },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: mocks.currentUserId } }),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  // Keep the real ApiError / errorCode so the dialogs' coded-error mapping
  // runs against the production helper; only the api client is stubbed.
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    agents: () => "/acme/agents",
    newAgent: () => "/acme/agents/new",
    newAgentManual: () => "/acme/agents/new/manual",
    agentDetail: (id: string) => `/acme/agents/${id}`,
    squadDetail: (id: string) => `/acme/squads/${id}`,
  }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
  memberListOptions: () => ({ queryKey: ["members"] }),
  workspaceKeys: { agents: (wsId: string) => ["agents", wsId] },
}));
vi.mock("@multica/core/agents/folder-queries", () => ({
  agentFolderTreeOptions: () => ({ queryKey: ["workspaces", "ws-1", "agent-folders"] }),
  invalidateAgentFolderQueries: vi.fn(),
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: () => ({ queryKey: ["runtimes"] }),
  runtimeDisplayLabel: () => "runtime",
}));
vi.mock("@multica/core/agents", () => ({
  isAgentRuntimeBound: () => true,
  agentRunCounts30dOptions: () => ({ queryKey: ["agent-run-counts"] }),
  useWorkspaceActivityMap: () => ({ byAgent: new Map(), loading: false }),
  useWorkspacePresenceMap: () => ({ byAgent: new Map(), loading: false }),
  VISIBILITY_TOOLTIP: { private: "Private", workspace: "Workspace" },
  effectiveAccessScope: () => "workspace",
  ALL_ACCESS_SCOPES: ["workspace", "specific-people", "owner-only"],
}));
vi.mock("@multica/core/agents/stores", () => ({
  useAgentsViewStore: (selector: (state: unknown) => unknown) =>
    useSyncExternalStore(
      (listener) => {
        mocks.searchListeners.add(listener);
        return () => { mocks.searchListeners.delete(listener); };
      },
      () => selector(mocks.viewState),
    ),
  AGENT_DEFAULT_HIDDEN_COLUMNS: [],
  AGENT_SCOPES: ["mine", "all", "archived"],
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (url: string | null) => url,
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => null }));
vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: ({ name }: { name: string }) => (
    <span data-testid="folder-avatar">{name.slice(0, 2).toUpperCase()}</span>
  ),
}));
vi.mock("@multica/ui/components/ui/tooltip", async (importOriginal) => ({
  ...await importOriginal<typeof import("@multica/ui/components/ui/tooltip")>(),
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <div role="tooltip">{children}</div>
  ),
}));
vi.mock("@multica/ui/components/ui/skeleton", () => ({
  Skeleton: (props: Record<string, unknown>) => (
    <div data-testid="skeleton" {...props} />
  ),
}));
vi.mock("../presence", () => ({ availabilityConfig: {} }));

// Filter state is preset per test through mocks.viewState.
vi.mock("./agent-list-toolbar", () => ({
  countActiveFilterDimensions: (filters: Record<string, string[]>) =>
    Object.values(filters).filter((values) => values.length > 0).length,
  AgentListToolbar: ({
    scope,
    onScopeChange,
    search,
    onSearchChange,
    folderActions,
  }: {
    scope: string;
    onScopeChange: (scope: string) => void;
    search: string;
    onSearchChange: (value: string) => void;
    folderActions?: React.ReactNode;
  }) => (
    <div>
      <input
        aria-label="Search agents"
        value={search}
        onChange={(event) => onSearchChange(event.target.value)}
      />
      {["mine", "all", "archived"].map((value) => (
        <button
          key={value}
          type="button"
          aria-pressed={scope === value}
          onClick={() => onScopeChange(value)}
        >
          {value === "mine" ? "Mine" : value === "all" ? "All" : "Archived"}
        </button>
      ))}
      {folderActions}
    </div>
  ),
}));

import { AgentsPage } from "./agents-page";

const BASE_AGENT: Agent = {
  id: "agent-base",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Base Agent",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "private",
  invocation_targets: [],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "claude",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-06-01T00:00:00Z",
  updated_at: "2026-06-01T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function makeAgent(id: string, name: string, over: Partial<Agent> = {}): Agent {
  return { ...BASE_AGENT, id, name, ...over };
}

const LOOSE = makeAgent("a-loose", "loose-agent");
const PLACED = makeAgent("a-placed", "placed-agent");
const LEADER = makeAgent("a-leader", "squad-leader");
const MEMBER = makeAgent("a-member", "squad-member", { owner_id: "user-2" });
const BOTH = makeAgent("a-both", "dual-agent");
const OTHER = makeAgent("a-other", "other-agent", { owner_id: "user-2" });
const ARCHIVED = makeAgent("a-archived", "archived-agent", {
  archived_at: "2026-08-01T00:00:00Z",
});

const folderTree: AgentFolderTree = {
  folders: [
    {
      id: "f-design",
      workspaceId: "ws-1",
      parentId: null,
      name: "Design",
      createdAt: "",
      updatedAt: "",
    },
    {
      id: "f-research",
      workspaceId: "ws-1",
      parentId: "f-design",
      name: "Research",
      createdAt: "",
      updatedAt: "",
    },
    {
      id: "f-eng",
      workspaceId: "ws-1",
      parentId: null,
      name: "Engineering",
      createdAt: "",
      updatedAt: "",
    },
  ],
  placements: [
    { agentId: "a-placed", folderId: "f-design" },
    { agentId: "a-both", folderId: "f-research" },
    { agentId: "a-archived", folderId: "f-design" },
  ],
  squads: [
    {
      squadId: "sq-game",
      name: "Game design",
      avatarUrl: null,
      leaderId: "a-leader",
      creatorId: "user-1",
      parentId: null,
      agentIds: ["a-leader", "a-member", "a-both"],
    },
    {
      squadId: "sq-ops",
      name: "Ops",
      avatarUrl: null,
      leaderId: "a-hidden",
      creatorId: "user-2",
      parentId: null,
      agentIds: [],
    },
    {
      // dual-agent belongs to two squads at once (M20 regression fixture).
      squadId: "sq-growth",
      name: "Growth",
      avatarUrl: null,
      leaderId: "a-leader",
      creatorId: "user-1",
      parentId: null,
      agentIds: ["a-both"],
    },
  ],
};

type NavController = {
  path: string;
  push: Mock<(path: string) => void>;
  replace: Mock<(path: string) => void>;
  back: Mock<() => void>;
};

function makeController(path: string): NavController {
  return {
    path,
    push: vi.fn<(path: string) => void>(),
    replace: vi.fn<(path: string) => void>(),
    back: vi.fn<() => void>(),
  };
}

function NavigationHarness({
  controller,
  initialPath,
  children,
}: {
  controller: NavController;
  initialPath: string;
  children: React.ReactNode;
}) {
  const [history, setHistory] = useState({ entries: [initialPath], index: 0 });
  const [, setVersion] = useState(0);
  const currentPath = history.entries[history.index]!;
  controller.path = currentPath;
  useMemo(() => {
    controller.push.mockImplementation((path: string) => {
      setHistory((previous) => ({
        entries: [...previous.entries.slice(0, previous.index + 1), path],
        index: previous.index + 1,
      }));
    });
    controller.replace.mockImplementation((path: string) => {
      setHistory((previous) => ({
        ...previous,
        entries: previous.entries.map((entry, index) =>
          index === previous.index ? path : entry,
        ),
      }));
    });
    controller.back.mockImplementation(() => {
      setHistory((previous) => ({
        ...previous,
        index: Math.max(0, previous.index - 1),
      }));
    });
    // The real store persists scope; the mock needs a render bump to make a
    // scope click visible.
    mocks.viewState.setScope.mockImplementation((scope: string) => {
      mocks.viewState.scope = scope;
      setVersion((version) => version + 1);
    });
  }, [controller]);
  const url = new URL(currentPath, "https://example.test");
  const adapter: NavigationAdapter = {
    push: controller.push,
    replace: controller.replace,
    back: controller.back,
    pathname: url.pathname,
    searchParams: new URLSearchParams(url.search),
    hash: url.hash,
    getShareableUrl: (path) => path,
    openInNewTab: vi.fn(),
  };
  return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
}

function renderPage(initialPath = "/acme/agents", locale?: SupportedLocale) {
  const controller = makeController(initialPath);
  const ui = (
    <NavigationHarness controller={controller} initialPath={initialPath}>
      <AgentsPage />
    </NavigationHarness>
  );
  const result = renderWithI18n(ui, { locale });
  return { ...result, controller };
}

const EXPANSION_STORAGE_KEY = "multica.folder_explorer.expanded.agents.ws-1";

async function expandFolder(name: string) {
  await userEvent.click(await screen.findByRole("button", { name: `Expand ${name}` }));
}

beforeEach(() => {
  vi.clearAllMocks();
  window.localStorage.clear();
  mocks.agents = [LOOSE, PLACED, LEADER, MEMBER, BOTH, OTHER, ARCHIVED];
  mocks.folderTree = folderTree;
  mocks.folderTreeError = false;
  mocks.folderTreeFailures = 0;
  mocks.refetchTree = vi.fn();
  mocks.currentUserId = "user-1";
  mocks.members = [{ user_id: "user-1", role: "member" }];
  mocks.virtualizerCount = 0;
  mocks.virtualizerKeys = [];
  mocks.viewState.scope = "all";
  mocks.viewState.search = "";
  mocks.viewState.setSearch.mockImplementation((value: string) => {
    mocks.viewState.search = value;
    mocks.searchListeners.forEach((listener) => listener());
  });
  mocks.viewState.hiddenColumns = [];
  for (const key of Object.keys(mocks.viewState.filters) as (keyof typeof mocks.viewState.filters)[]) {
    mocks.viewState.filters[key] = [];
  }
  apiMock.createAgentFolder.mockResolvedValue({ id: "f-new" });
  apiMock.updateAgentFolder.mockResolvedValue({ id: "f-design" });
  apiMock.deleteAgentFolder.mockResolvedValue({ deleted: true });
  apiMock.setAgentPlacement.mockResolvedValue({ updated: true });
  apiMock.setAgentSquadPlacement.mockResolvedValue({ updated: true });
});

describe("AgentsPage folder hierarchy", () => {
  it("starts with every folder collapsed above uncategorized root agents", async () => {
    renderPage();
    expect(await screen.findByText("loose-agent")).toBeTruthy();
    expect(screen.getByText("other-agent")).toBeTruthy();
    const squadToggle = screen.getByRole("button", { name: "Expand Game design" });
    expect(squadToggle.getAttribute("aria-expanded")).toBe("false");
    expect(squadToggle.textContent).toContain("Squad");
    expect(squadToggle.textContent).toContain("3 agents");
    expect(within(squadToggle).getAllByTestId("folder-avatar")[0]).toBeTruthy();
    expect(mocks.rowSizes.slice(0, 5)).toEqual([56, 56, 56, 56, 56]);
    expect(mocks.rowSizes.slice(5)).toEqual([64, 64]);
    const directory = squadToggle.closest('[role="row"]') as HTMLElement;
    expect(within(directory).getAllByRole("cell")).toHaveLength(11);
    expect([...squadToggle.querySelectorAll("[title]")].map((node) => node.getAttribute("title")))
      .toEqual(["squad-leader", "squad-member", "dual-agent"]);
    expect(screen.getByRole("button", { name: "Expand Ops" }).textContent).toContain("0 agents");
    for (const name of ["Design", "Engineering", "Game design", "Ops", "Growth"]) {
      expect(screen.getByRole("button", { name: `Expand ${name}` }).getAttribute("aria-expanded"))
        .toBe("false");
    }
    expect(screen.queryByRole("button", { name: "Expand Research" })).toBeNull();
    for (const name of ["placed-agent", "squad-leader", "squad-member", "dual-agent"]) {
      expect(screen.queryByText(name)).toBeNull();
    }
    expect(screen.queryByText("archived-agent")).toBeNull();
    const folderRow = screen
      .getByRole("button", { name: "Expand Design" })
      .closest("[role='row']") as HTMLElement;
    const agentRow = screen
      .getByText("loose-agent")
      .closest("[role='row']") as HTMLElement;
    expect(
      folderRow.compareDocumentPosition(agentRow) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
    const folderCells = within(folderRow).getAllByRole("cell");
    const agentCells = within(agentRow).getAllByRole("cell");
    expect(folderCells).toHaveLength(agentCells.length);
    expect(folderCells[0]!.className).toBe(agentCells[0]!.className);
  });

  it("hides folder aggregation when the status column is hidden", async () => {
    const first = renderPage();
    const toggle = await screen.findByRole("button", { name: "Expand Design" });
    const directory = toggle.closest('[role="row"]') as HTMLElement;
    expect(within(directory).getByText("Offline")).toBeTruthy();
    first.unmount();

    mocks.viewState.hiddenColumns = ["status"];
    renderPage();
    const hiddenToggle = await screen.findByRole("button", { name: "Expand Design" });
    const hiddenDirectory = hiddenToggle.closest('[role="row"]') as HTMLElement;
    expect(within(hiddenDirectory).queryByText("Offline")).toBeNull();
  });

  it("renders four preview avatars and one overflow badge for a seven-member squad", async () => {
    const extra = Array.from({ length: 4 }, (_, index) => makeAgent(`extra-${index}`, `Extra ${index}`));
    mocks.agents = [...mocks.agents, ...extra];
    mocks.folderTree = {
      ...folderTree,
      squads: folderTree.squads.map((squad) => squad.squadId === "sq-game"
        ? { ...squad, agentIds: [...squad.agentIds, ...extra.map((agent) => agent.id)] }
        : squad),
    };
    renderPage();
    const directory = await screen.findByRole("button", { name: "Expand Game design" });
    expect([...directory.querySelectorAll("[title]")].map((node) => node.getAttribute("title")))
      .toEqual(["squad-leader", "squad-member", "dual-agent", "Extra 0"]);
    expect(within(directory).getByText("+3").className).toContain("inline-flex");
    expect(within(directory).queryByText("+4")).toBeNull();
    expect(directory.textContent).toContain("7 agents");
  });

  it("uses composite keys and deduplicates agents visible in multiple squads (M20)", async () => {
    renderPage();
    await expandFolder("Growth");
    expect(await screen.findByText("dual-agent")).toBeTruthy();
    expect(screen.queryByText("squad-leader")).toBeNull();

    await expandFolder("Game design");
    expect(screen.getAllByText("dual-agent")).toHaveLength(2);
    expect(mocks.virtualizerKeys).toEqual(expect.arrayContaining([
      "agent-sq-game-a-both",
      "agent-sq-growth-a-both",
    ]));
    const header = screen.getAllByRole("row")[0]!;
    await userEvent.click(within(header).getAllByRole("button")[0]!);
    expect(await screen.findByText("5 selected")).toBeTruthy();
  });

  it("expands a squad in place without navigation and keeps toolbar folder actions", async () => {
    const { controller } = renderPage();
    await expandFolder("Game design");
    expect(controller.path).toBe("/acme/agents");
    expect(controller.push).not.toHaveBeenCalled();
    expect(controller.replace).not.toHaveBeenCalled();
    expect(await screen.findByText("squad-leader")).toBeTruthy();
    expect(screen.getByText("squad-member")).toBeTruthy();
    expect(screen.getByText("dual-agent")).toBeTruthy();
    expect(screen.getByText("Leader")).toBeTruthy();
    expect(screen.getByText("loose-agent")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Expand all" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse all" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "New folder" }).hasAttribute("disabled")).toBe(false);
  });

  it("scopes expanded squad contents and counts to the current scope", async () => {
    mocks.viewState.scope = "mine";
    renderPage();
    await expandFolder("Game design");
    expect(await screen.findByText("squad-leader")).toBeTruthy();
    expect(screen.getByText("dual-agent")).toBeTruthy();
    expect(screen.queryByText("squad-member")).toBeNull();
    expect(screen.getByText("Leader")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse Game design" }).textContent)
      .toContain("2 agents");
  });

  it("shows a placed squad member in both its squad folder and its custom folder", async () => {
    renderPage();
    await expandFolder("Game design");
    await expandFolder("Design");
    await expandFolder("Research");
    expect(await screen.findAllByText("dual-agent")).toHaveLength(2);
    expect(screen.getByText("squad-leader")).toBeTruthy();
  });

  it("keeps folder state out of agent detail navigation", async () => {
    const { controller } = renderPage();
    await expandFolder("Game design");
    await userEvent.click(await screen.findByText("squad-leader"));
    expect(controller.path).toBe("/acme/agents/a-leader");
  });

  it("expands and collapses nested folders in place while keeping parent and root rows visible", async () => {
    const { controller } = renderPage();
    await expandFolder("Design");
    expect(await screen.findByText("placed-agent")).toBeTruthy();
    expect(screen.queryByText("dual-agent")).toBeNull();

    await expandFolder("Research");
    expect(await screen.findByText("dual-agent")).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse Design" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse Research" })).toBeTruthy();
    expect(screen.getByText("loose-agent")).toBeTruthy();
    expect(controller.path).toBe("/acme/agents");

    await userEvent.click(screen.getByRole("button", { name: "Collapse Research" }));
    expect(screen.queryByText("dual-agent")).toBeNull();
    expect(screen.getByRole("button", { name: "Expand Research" })).toBeTruthy();
    expect(screen.getByText("placed-agent")).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "Collapse Design" }));
    expect(screen.queryByRole("button", { name: "Expand Research" })).toBeNull();
    expect(screen.queryByText("placed-agent")).toBeNull();
    expect(screen.getByRole("button", { name: "Expand Design" })).toBeTruthy();
    expect(screen.getByText("loose-agent")).toBeTruthy();
    expect(controller.push).not.toHaveBeenCalled();
    expect(controller.replace).not.toHaveBeenCalled();
  });

  it("expands and collapses the whole tree from the toolbar", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Expand all" }));
    expect(await screen.findByText("placed-agent")).toBeTruthy();
    expect(screen.getByText("squad-leader")).toBeTruthy();
    expect(screen.getAllByText("dual-agent")).toHaveLength(3);
    expect(screen.getByRole("button", { name: "Collapse Research" })).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "Collapse all" }));
    expect(screen.queryByText("placed-agent")).toBeNull();
    expect(screen.queryByText("squad-leader")).toBeNull();
    expect(screen.queryByText("dual-agent")).toBeNull();
    expect(screen.queryByRole("button", { name: "Expand Research" })).toBeNull();
    expect(screen.getByText("loose-agent")).toBeTruthy();
    expect(window.localStorage.getItem(EXPANSION_STORAGE_KEY)).toBe("[]");
  });

  it("restores expanded folders from localStorage after a remount", async () => {
    const first = renderPage();
    await expandFolder("Design");
    await expandFolder("Research");
    expect(JSON.parse(window.localStorage.getItem(EXPANSION_STORAGE_KEY)!)).toEqual([
      "f-design",
      "f-research",
    ]);
    first.unmount();

    renderPage();
    expect(await screen.findByRole("button", { name: "Collapse Design" })).toBeTruthy();
    expect(screen.getByRole("button", { name: "Collapse Research" })).toBeTruthy();
    expect(screen.getByText("placed-agent")).toBeTruthy();
    expect(screen.getByText("dual-agent")).toBeTruthy();
  });
});

describe("AgentsPage folder actions", () => {
  it("creates a subfolder from a custom folder row menu", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("New subfolder"));
    await userEvent.type(screen.getByLabelText("Name"), "Research 2");
    await userEvent.click(screen.getByRole("button", { name: "Create" }));
    await waitFor(() =>
      expect(apiMock.createAgentFolder).toHaveBeenCalledWith("ws-1", {
        name: "Research 2",
        parentId: "f-design",
      }),
    );
    expect(await screen.findByRole("button", { name: "Collapse Design" })).toHaveAttribute(
      "aria-expanded",
      "true",
    );
  });

  it("renames, moves and deletes a custom folder from its row menu", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("Rename"));
    const input = screen.getByLabelText("Name");
    await userEvent.clear(input);
    await userEvent.type(input, "Brand");
    await userEvent.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() =>
      expect(apiMock.updateAgentFolder).toHaveBeenCalledWith("ws-1", "f-design", { name: "Brand" }),
    );

    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("Move"));
    // The move dialog offers the root and excludes the folder's own subtree.
    expect(await screen.findByRole("button", { name: "Root" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Design / Research" })).toBeNull();

    await userEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("Delete"));
    expect(
      await screen.findByText(
        "Its agents, subfolders, and squad folders move up one level. No agents or squads are deleted.",
        { exact: false },
      ),
    ).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(apiMock.deleteAgentFolder).toHaveBeenCalledWith("ws-1", "f-design"),
    );
  });

  it("moves a squad folder into a custom folder for its creator", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Game design" }));
    expect(await screen.findByText("Open squad")).toBeTruthy();
    expect(screen.getByText("Move to...")).toBeTruthy();
    await userEvent.click(screen.getByText("Move to..."));
    // The root is a valid squad-folder destination (move back out) — M21.
    const rootOption = await screen.findByRole("button", { name: "Root" });
    expect(rootOption).toBeTruthy();
    await userEvent.click(await screen.findByRole("button", { name: "Design" }));
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(apiMock.setAgentSquadPlacement).toHaveBeenCalledWith("ws-1", "sq-game", "f-design"),
    );
  });

  it("moves a squad folder back to the root", async () => {
    const tree = {
      ...folderTree,
      squads: folderTree.squads.map((squad) =>
        squad.squadId === "sq-game" ? { ...squad, parentId: "f-design" } : squad,
      ),
    };
    mocks.folderTree = tree;
    renderPage();
    await expandFolder("Design");
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Game design" }));
    await userEvent.click(await screen.findByText("Move to..."));
    await userEvent.click(await screen.findByRole("button", { name: "Root" }));
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(apiMock.setAgentSquadPlacement).toHaveBeenCalledWith("ws-1", "sq-game", ""),
    );
  });

  it("moves a custom folder to the chosen destination (M22)", async () => {
    renderPage();
    await expandFolder("Design");
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Research" }));
    await userEvent.click(await screen.findByText("Move"));
    // Both the root and other custom folders are valid destinations; the
    // folder's own subtree is excluded.
    expect(await screen.findByRole("button", { name: "Root" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Design / Research" })).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "Engineering" }));
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(apiMock.updateAgentFolder).toHaveBeenCalledWith("ws-1", "f-research", { parentId: "f-eng" }),
    );
  });

  it("opens the squad detail page from the squad folder menu", async () => {
    const { controller } = renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Game design" }));
    await userEvent.click(await screen.findByText("Open squad"));
    expect(controller.path).toBe("/acme/squads/sq-game");
  });

  it("greys out squad-folder moves for non-creators with the reason", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Ops" }));
    const move = await screen.findByText("Move to...");
    expect(move.closest("[data-disabled]")).toBeTruthy();
    expect(
      screen.getByText("Only the squad creator or a workspace owner/admin can move this squad folder."),
    ).toBeTruthy();
  });

  it("lets a workspace admin move another member's squad folder", async () => {
    mocks.members = [{ user_id: "user-1", role: "admin" }];
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Ops" }));
    const move = await screen.findByText("Move to...");
    expect(move.closest("[data-disabled]")).toBeNull();
    await userEvent.click(move);
    await userEvent.click(await screen.findByRole("button", { name: "Design" }));
    await userEvent.click(screen.getAllByRole("button", { name: "Move" }).pop()!);
    await waitFor(() =>
      expect(apiMock.setAgentSquadPlacement).toHaveBeenCalledWith("ws-1", "sq-ops", "f-design"),
    );
  });
});

describe("AgentsPage move agents", () => {
  it("moves a single agent from its row menu into a custom folder", async () => {
    renderPage();
    const row = (await screen.findByText("loose-agent")).closest("[role='row']") as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "Row actions" }));
    await userEvent.click(await screen.findByText("Move to..."));
    await userEvent.click(await screen.findByRole("button", { name: "Design" }));
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(apiMock.setAgentPlacement).toHaveBeenCalledWith("ws-1", "a-loose", "f-design"),
    );
  });

  it("uses \"also place\" wording inside squad folders and keeps squad membership", async () => {
    renderPage();
    await expandFolder("Game design");
    const row = (await screen.findByText("squad-leader")).closest("[role='row']") as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "Row actions" }));
    expect(await screen.findByText("Also place in folder...")).toBeTruthy();
    expect(screen.queryByText("Move to...")).toBeNull();
    await userEvent.click(screen.getByText("Also place in folder..."));
    await userEvent.click(await screen.findByRole("button", { name: "Design" }));
    expect(
      screen.getByText("Squad members stay in their squad folders; this only sets their custom folder placement."),
    ).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Place" }));
    await waitFor(() =>
      expect(apiMock.setAgentPlacement).toHaveBeenCalledWith("ws-1", "a-leader", "f-design"),
    );
  });

  it("explains that clearing a squad member's placement hides it from the root", async () => {
    renderPage();
    await expandFolder("Design");
    await expandFolder("Research");
    const row = (await screen.findByText("dual-agent")).closest("[role='row']") as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "Row actions" }));
    await userEvent.click(await screen.findByText("Move to..."));
    await userEvent.click(await screen.findByRole("button", { name: "Root" }));
    expect(
      await screen.findByText("Squad members stay in their squad folders. Without a placement they appear only there, not at the root."),
    ).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(apiMock.setAgentPlacement).toHaveBeenCalledWith("ws-1", "a-both", ""),
    );
  });

  it("hides the row move action for agents the caller cannot manage", async () => {
    renderPage();
    const row = (await screen.findByText("other-agent")).closest("[role='row']") as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "Row actions" }));
    // Wait for the menu to actually render before asserting absence (M04).
    await screen.findByText("Open in new tab");
    expect(screen.queryByText("Move to...")).toBeNull();
  });

  it("batch-moves selected agents and reports partial failures", async () => {
    renderPage();
    const looseRow = (await screen.findByText("loose-agent")).closest("[role='row']") as HTMLElement;
    await userEvent.click(within(looseRow).getAllByRole("button")[0]!);
    const otherRow = screen.getByText("other-agent").closest("[role='row']") as HTMLElement;
    await userEvent.click(within(otherRow).getAllByRole("button")[0]!);

    // other-agent is owned by user-2: the batch move is disabled with a reason.
    const moveButton = screen.getByRole("button", { name: "Move to folder" });
    expect(moveButton.hasAttribute("disabled")).toBe(true);
    expect(
      screen.getByText("You don't have permission to move every selected agent."),
    ).toBeTruthy();

    // Deselect the unmanageable agent, then move the rest.
    await userEvent.click(within(otherRow).getAllByRole("button")[0]!);
    await userEvent.click(screen.getByRole("button", { name: "Move to folder" }));
    await userEvent.click(await screen.findByRole("button", { name: "Design" }));
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(apiMock.setAgentPlacement).toHaveBeenCalledWith("ws-1", "a-loose", "f-design"),
    );
  });

  it("reports a partial batch failure", async () => {
    const { toast } = await import("sonner");
    apiMock.setAgentPlacement.mockReset();
    apiMock.setAgentPlacement
      .mockResolvedValueOnce({ updated: true })
      .mockRejectedValueOnce(new Error("boom"));
    mocks.currentUserId = "user-1";
    mocks.members = [{ user_id: "user-1", role: "admin" }];
    renderPage();
    for (const name of ["loose-agent", "other-agent"]) {
      const row = (await screen.findByText(name)).closest("[role='row']") as HTMLElement;
      await userEvent.click(within(row).getAllByRole("button")[0]!);
    }
    await userEvent.click(screen.getByRole("button", { name: "Move to folder" }));
    await userEvent.click(await screen.findByRole("button", { name: "Design" }));
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("Moved 1 of 2; the rest failed."),
    );
  });
});

describe("AgentsPage move/place wording", () => {
  it("defaults the batch action to move even inside an expanded squad", async () => {
    const { toast } = await import("sonner");
    renderPage();
    await expandFolder("Game design");
    const leaderRow = (await screen.findByText("squad-leader")).closest("[role='row']") as HTMLElement;
    await userEvent.click(within(leaderRow).getAllByRole("button")[0]!);
    const bothRow = screen.getByText("dual-agent").closest("[role='row']") as HTMLElement;
    await userEvent.click(within(bothRow).getAllByRole("button")[0]!);

    const button = screen.getByRole("button", { name: "Move to folder" });
    expect(button).toBeTruthy();
    await userEvent.click(button);
    expect(await screen.findByRole("button", { name: "Move" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Place" })).toBeNull();
    await userEvent.click(await screen.findByRole("button", { name: "Design" }));
    expect(
      screen.getByText("Squad members stay in their squad folders; this only sets their custom folder placement."),
    ).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() => {
      expect(apiMock.setAgentPlacement).toHaveBeenCalledWith("ws-1", "a-leader", "f-design");
      expect(apiMock.setAgentPlacement).toHaveBeenCalledWith("ws-1", "a-both", "f-design");
    });
    await waitFor(() =>
      expect(toast.success).toHaveBeenCalledWith("2 agents moved."),
    );
  });

  it("keeps the move wording in flat results reached from a squad folder", async () => {
    renderPage();
    await expandFolder("Game design");
    const search = await screen.findByRole("textbox", { name: "Search agents" });
    await userEvent.type(search, "squad");
    expect(screen.queryByRole("button", { name: "Collapse Game design" })).toBeNull();
    const row = screen.getByText("squad-leader").closest("[role='row']") as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "Row actions" }));
    expect(await screen.findByText("Move to...")).toBeTruthy();
    expect(screen.queryByText("Also place in folder...")).toBeNull();
  });
});

describe("AgentsPage unreadable write responses", () => {
  it("refreshes the tree and shows an indeterminate result when a folder write reads back null", async () => {
    const { invalidateAgentFolderQueries } = await import(
      "@multica/core/agents/folder-queries"
    );
    apiMock.createAgentFolder.mockResolvedValue(null);
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "New folder" }));
    await userEvent.type(screen.getByLabelText("Name"), "Ghost");
    await userEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(
      await screen.findByText(
        "The result couldn't be confirmed. The list was refreshed — check it before retrying.",
      ),
    ).toBeTruthy();
    expect(invalidateAgentFolderQueries).toHaveBeenCalled();
    expect(screen.queryByText("Couldn't save the folder. Try again.")).toBeNull();
  });

  it("counts null placement responses as unconfirmed, not failed", async () => {
    const { toast } = await import("sonner");
    const { invalidateAgentFolderQueries } = await import(
      "@multica/core/agents/folder-queries"
    );
    apiMock.setAgentPlacement.mockReset();
    apiMock.setAgentPlacement
      .mockResolvedValueOnce({ updated: true })
      .mockResolvedValueOnce(null);
    mocks.members = [{ user_id: "user-1", role: "admin" }];
    renderPage();
    for (const name of ["loose-agent", "other-agent"]) {
      const row = (await screen.findByText(name)).closest("[role='row']") as HTMLElement;
      await userEvent.click(within(row).getAllByRole("button")[0]!);
    }
    await userEvent.click(screen.getByRole("button", { name: "Move to folder" }));
    await userEvent.click(await screen.findByRole("button", { name: "Design" }));
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        "Moved 1 of 2; 1 couldn't be confirmed. The list was refreshed — check it before retrying.",
      ),
    );
    expect(toast.success).not.toHaveBeenCalled();
    expect(invalidateAgentFolderQueries).toHaveBeenCalled();
  });
});

describe("AgentsPage flat view", () => {
  it("uses a flat list with paths for the archived scope", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Archived" }));
    expect(await screen.findByText("archived-agent")).toBeTruthy();
    expect(screen.getByText("Design")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Expand Design" })).toBeNull();
    expect(screen.queryByText("loose-agent")).toBeNull();
  });

  it("searches across folders with path hints and restores the expanded tree when cleared", async () => {
    const { controller } = renderPage();
    await expandFolder("Design");
    const search = await screen.findByRole("textbox", { name: "Search agents" });
    await userEvent.type(search, "agent");
    expect(await screen.findByText("dual-agent")).toBeTruthy();
    expect(screen.getAllByText("Design / Research").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Game design").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Growth").length).toBeGreaterThan(0);
    expect(screen.getByText("+2")).toBeTruthy();
    expect(controller.path).toBe("/acme/agents");
    expect(screen.queryByRole("button", { name: "Collapse Design" })).toBeNull();

    await userEvent.clear(search);
    expect(await screen.findByText("placed-agent")).toBeTruthy();
    expect(screen.queryByText("dual-agent")).toBeNull();
    expect(screen.getByRole("button", { name: "Collapse Design" })).toBeTruthy();
    expect(controller.path).toBe("/acme/agents");
  });

  it("enters flat view immediately when a toolbar filter is active", async () => {
    mocks.viewState.filters.models = ["claude"];
    renderPage();
    expect(await screen.findByText("placed-agent")).toBeTruthy();
    expect(screen.getByText("squad-leader")).toBeTruthy();
    expect(screen.getByText("loose-agent")).toBeTruthy();
    expect(screen.getByText("Design")).toBeTruthy();
    expect(screen.queryByRole("button", { name: "Expand Design" })).toBeNull();
  });

  it("keeps every agent visible with a retry when the tree cannot load", async () => {
    mocks.folderTree = null;
    mocks.folderTreeError = true;
    renderPage();
    for (const name of ["loose-agent", "placed-agent", "squad-leader", "squad-member", "dual-agent"]) {
      expect(await screen.findByText(name)).toBeTruthy();
    }
    expect(
      screen.getByText("Folders are unavailable. Showing agents in a flat list."),
    ).toBeTruthy();
    await userEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(mocks.refetchTree).toHaveBeenCalled();
  });
});

describe("AgentsPage squad changes", () => {
  it("reflects squad renames and membership changes without a manual refresh", async () => {
    const { controller, rerender } = renderPage();
    expect(await screen.findByRole("button", { name: "Expand Game design" })).toBeTruthy();
    mocks.folderTree = {
      ...folderTree,
      squads: folderTree.squads.map((squad) =>
        squad.squadId === "sq-game"
          ? { ...squad, name: "Game design v2", agentIds: [...squad.agentIds, "a-loose"] }
          : squad,
      ),
    };
    rerender(
      <NavigationHarness controller={controller} initialPath="/acme/agents">
        <AgentsPage />
      </NavigationHarness>,
    );
    expect(await screen.findByRole("button", { name: "Expand Game design v2" })).toBeTruthy();
    // loose-agent joined the squad: it leaves the root list.
    expect(screen.queryByText("loose-agent")).toBeNull();
  });

  it("returns squad members without a placement to the root when the squad is archived", async () => {
    const { controller, rerender } = renderPage();
    expect(await screen.findByRole("button", { name: "Expand Game design" })).toBeTruthy();
    mocks.folderTree = {
      ...folderTree,
      squads: folderTree.squads.filter((squad) => squad.squadId !== "sq-game"),
    };
    rerender(
      <NavigationHarness controller={controller} initialPath="/acme/agents">
        <AgentsPage />
      </NavigationHarness>,
    );
    // a-leader and a-member have no other squad and no placement: root.
    expect(await screen.findByText("squad-leader")).toBeTruthy();
    expect(screen.getByText("squad-member")).toBeTruthy();
    // a-both still has its custom placement: not at the root.
    expect(screen.queryByText("dual-agent")).toBeNull();
  });
});

describe("AgentsPage folder regressions", () => {
  it("localizes the in-place expansion controls", async () => {
    renderPage("/acme/agents", "ja");
    await userEvent.click(await screen.findByRole("button", { name: "「Design」を展開" }));
    expect(await screen.findByRole("button", { name: "「Design」を折りたたむ" })).toBeTruthy();
  });

  it("shows a skeleton instead of flashing the flat list while the tree loads", () => {
    mocks.folderTree = undefined;
    renderPage();
    expect(screen.queryByText("squad-leader")).toBeNull();
    expect(screen.queryByRole("button", { name: "Expand Game design" })).toBeNull();
    expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
  });

  it("keeps the upstream empty-workspace state when there are no agents and no folders", async () => {
    mocks.agents = [];
    mocks.folderTree = { folders: [], placements: [], squads: [] };
    renderPage();
    expect(await screen.findByText("No agents yet")).toBeTruthy();
  });

  it("limits selection and select-all to visible agent rows", async () => {
    renderPage();
    await expandFolder("Game design");
    const header = screen.getAllByRole("row")[0]!;
    await userEvent.click(within(header).getAllByRole("button")[0]!);
    expect(await screen.findByText("5 selected")).toBeTruthy();

    await userEvent.click(screen.getByRole("button", { name: "Collapse Game design" }));
    expect(await screen.findByText("2 selected")).toBeTruthy();
  });
});

describe("AgentsPage unreadable folder move/delete responses", () => {
  it("refreshes the tree and shows an indeterminate result when a folder move reads back null", async () => {
    const { invalidateAgentFolderQueries } = await import(
      "@multica/core/agents/folder-queries"
    );
    apiMock.updateAgentFolder.mockResolvedValue(null);
    renderPage();
    await expandFolder("Design");
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Research" }));
    await userEvent.click(await screen.findByText("Move"));
    await userEvent.click(await screen.findByRole("button", { name: "Engineering" }));
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(apiMock.updateAgentFolder).toHaveBeenCalledWith("ws-1", "f-research", { parentId: "f-eng" }),
    );
    expect(
      await screen.findByText(
        "The result couldn't be confirmed. The list was refreshed — check it before retrying.",
      ),
    ).toBeTruthy();
    expect(invalidateAgentFolderQueries).toHaveBeenCalled();
    expect(screen.queryByText("Couldn't move the folder. Try again.")).toBeNull();
  });

  it("refreshes the tree and shows an indeterminate result when a folder delete reads back null", async () => {
    const { invalidateAgentFolderQueries } = await import(
      "@multica/core/agents/folder-queries"
    );
    apiMock.deleteAgentFolder.mockResolvedValue(null);
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("Delete"));
    await userEvent.click(await screen.findByRole("button", { name: "Delete" }));
    await waitFor(() =>
      expect(apiMock.deleteAgentFolder).toHaveBeenCalledWith("ws-1", "f-design"),
    );
    expect(
      await screen.findByText(
        "The result couldn't be confirmed. The list was refreshed — check it before retrying.",
      ),
    ).toBeTruthy();
    expect(invalidateAgentFolderQueries).toHaveBeenCalled();
    expect(screen.queryByText("Couldn't delete the folder. Try again.")).toBeNull();
  });
});

describe("AgentsPage move-variant result toasts", () => {
  async function selectTwoSquadMembers() {
    renderPage();
    await expandFolder("Game design");
    const leaderRow = (await screen.findByText("squad-leader")).closest("[role='row']") as HTMLElement;
    await userEvent.click(within(leaderRow).getAllByRole("button")[0]!);
    const bothRow = screen.getByText("dual-agent").closest("[role='row']") as HTMLElement;
    await userEvent.click(within(bothRow).getAllByRole("button")[0]!);
    await userEvent.click(screen.getByRole("button", { name: "Move to folder" }));
    await userEvent.click(await screen.findByRole("button", { name: "Design" }));
  }

  it("uses the move wording for an unconfirmed result", async () => {
    const { toast } = await import("sonner");
    apiMock.setAgentPlacement.mockReset();
    apiMock.setAgentPlacement
      .mockResolvedValueOnce({ updated: true })
      .mockResolvedValueOnce(null);
    await selectTwoSquadMembers();
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith(
        "Moved 1 of 2; 1 couldn't be confirmed. The list was refreshed — check it before retrying.",
      ),
    );
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("uses the move wording for a partial failure", async () => {
    const { toast } = await import("sonner");
    apiMock.setAgentPlacement.mockReset();
    apiMock.setAgentPlacement
      .mockResolvedValueOnce({ updated: true })
      .mockRejectedValueOnce(new Error("boom"));
    await selectTwoSquadMembers();
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("Moved 1 of 2; the rest failed."),
    );
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("uses the move wording when every move fails", async () => {
    const { toast } = await import("sonner");
    apiMock.setAgentPlacement.mockReset();
    apiMock.setAgentPlacement.mockRejectedValue(new Error("boom"));
    await selectTwoSquadMembers();
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() =>
      expect(toast.error).toHaveBeenCalledWith("Couldn't move the agents. Try again."),
    );
    expect(toast.success).not.toHaveBeenCalled();
  });
});

describe("AgentsPage localized folder errors", () => {
  it("localizes a coded create error instead of showing the server message", async () => {
    const { ApiError } = await import("@multica/core/api");
    const { invalidateAgentFolderQueries } = await import(
      "@multica/core/agents/folder-queries"
    );
    apiMock.createAgentFolder.mockRejectedValue(
      new ApiError("a folder with this name already exists at the destination", 409, "Conflict", {
        code: "name_conflict",
        error: "a folder with this name already exists at the destination",
      }),
    );
    renderPage("/acme/agents", "zh-Hans");
    await userEvent.click(await screen.findByRole("button", { name: "新建文件夹" }));
    await userEvent.type(screen.getByLabelText("名称"), "Research");
    await userEvent.click(screen.getByRole("button", { name: "创建" }));
    expect(await screen.findByText("目标位置已存在同名文件夹，请换一个名称。")).toBeTruthy();
    expect(screen.queryByText(/already exists at the destination/)).toBeNull();
    // A failed write still refreshes the tree — the copy says so.
    expect(invalidateAgentFolderQueries).toHaveBeenCalled();
  });

  // Every code the dialog's mapping claims to localize must render its own
  // copy — deleting a branch must fail a test.
  it.each([
    { code: "name_conflict", expected: "A folder with this name already exists at the destination." },
    { code: "folder_cycle", expected: "A folder can't be moved into one of its own subfolders." },
    {
      code: "invalid_name",
      expected:
        "The folder name isn't valid: 1–255 bytes, no surrounding whitespace, no control characters or line breaks.",
    },
    { code: "forbidden", expected: "You don't have permission to do this." },
    {
      code: "not_found",
      expected: "The target couldn't be found — it may have been deleted. The list was refreshed.",
    },
  ])("localizes the $code error", async ({ code, expected }) => {
    const { ApiError } = await import("@multica/core/api");
    apiMock.createAgentFolder.mockRejectedValue(
      new ApiError(`server says ${code}`, 409, "Conflict", { code, error: `server says ${code}` }),
    );
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "New folder" }));
    await userEvent.type(screen.getByLabelText("Name"), "Ghost");
    await userEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByText(expected)).toBeTruthy();
    expect(screen.queryByText(`server says ${code}`)).toBeNull();
  });

  it("localizes a coded move error", async () => {
    const { ApiError } = await import("@multica/core/api");
    apiMock.updateAgentFolder.mockRejectedValue(
      new ApiError("folder move would create a cycle", 409, "Conflict", {
        code: "folder_cycle",
        error: "folder move would create a cycle",
      }),
    );
    renderPage();
    await expandFolder("Design");
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Research" }));
    await userEvent.click(await screen.findByText("Move"));
    await userEvent.click(await screen.findByRole("button", { name: "Engineering" }));
    await userEvent.click(screen.getByRole("button", { name: "Move" }));
    expect(
      await screen.findByText("A folder can't be moved into one of its own subfolders."),
    ).toBeTruthy();
    expect(screen.queryByText("folder move would create a cycle")).toBeNull();
  });

  it("localizes a coded delete error", async () => {
    const { ApiError } = await import("@multica/core/api");
    apiMock.deleteAgentFolder.mockRejectedValue(
      new ApiError("object not found in this workspace", 404, "Not Found", {
        code: "not_found",
        error: "object not found in this workspace",
      }),
    );
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "Actions for Design" }));
    await userEvent.click(await screen.findByText("Delete"));
    await userEvent.click(await screen.findByRole("button", { name: "Delete" }));
    expect(
      await screen.findByText(
        "The target couldn't be found — it may have been deleted. The list was refreshed.",
      ),
    ).toBeTruthy();
    expect(screen.queryByText("object not found in this workspace")).toBeNull();
  });

  it("falls back to the generic failure copy for an uncoded error", async () => {
    const { invalidateAgentFolderQueries } = await import(
      "@multica/core/agents/folder-queries"
    );
    apiMock.createAgentFolder.mockRejectedValue(new Error("socket hangup"));
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "New folder" }));
    await userEvent.type(screen.getByLabelText("Name"), "Ghost");
    await userEvent.click(screen.getByRole("button", { name: "Create" }));
    expect(await screen.findByText("Couldn't save the folder. Try again.")).toBeTruthy();
    expect(screen.queryByText("socket hangup")).toBeNull();
    expect(invalidateAgentFolderQueries).toHaveBeenCalled();
  });
});

describe("AgentsPage tree wait fallback", () => {
  it("keeps the hierarchy when a background tree refresh fails once with cached data", async () => {
    // Cached tree + first failed background attempt (retry still pending):
    // the hierarchy must not flip to flat for a transient failure.
    mocks.folderTreeFailures = 1;
    renderPage();
    await expandFolder("Design");
    expect(screen.getByRole("button", { name: "Expand Research" })).toBeTruthy();
    expect(screen.getByText("placed-agent")).toBeTruthy();
    expect(screen.getByText("loose-agent")).toBeTruthy();
    expect(
      screen.queryByText("Folders are unavailable. Showing agents in a flat list."),
    ).toBeNull();
  });

  it("falls back to the flat list on the first failed tree attempt, before the retry settles", () => {
    mocks.folderTree = undefined;
    mocks.folderTreeFailures = 1;
    renderPage();
    for (const name of ["loose-agent", "placed-agent", "squad-leader", "squad-member", "dual-agent"]) {
      expect(screen.getByText(name)).toBeTruthy();
    }
    expect(
      screen.getByText("Folders are unavailable. Showing agents in a flat list."),
    ).toBeTruthy();
    expect(screen.queryByTestId("skeleton")).toBeNull();
    expect(screen.queryByRole("button", { name: "Expand Design" })).toBeNull();
  });

  it("falls back to the flat list after 5s without tree data, then restores the expanded hierarchy", async () => {
    vi.useFakeTimers();
    try {
      mocks.folderTree = undefined;
      window.localStorage.setItem(EXPANSION_STORAGE_KEY, JSON.stringify(["sq-game"]));
      const { controller, rerender } = renderPage();
      expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
      expect(screen.queryByText("squad-leader")).toBeNull();

      // One millisecond before the deadline the grace window must still hold
      // (guards the 5s threshold itself).
      act(() => {
        vi.advanceTimersByTime(4999);
      });
      expect(screen.getAllByTestId("skeleton").length).toBeGreaterThan(0);
      expect(screen.queryByText("squad-leader")).toBeNull();
      expect(
        screen.queryByText("Folders are unavailable. Showing agents in a flat list."),
      ).toBeNull();

      act(() => {
        vi.advanceTimersByTime(1);
      });
      expect(screen.queryByTestId("skeleton")).toBeNull();
      expect(screen.getByText("squad-leader")).toBeTruthy();
      expect(screen.getByText("loose-agent")).toBeTruthy();
      expect(
        screen.getByText("Folders are unavailable. Showing agents in a flat list."),
      ).toBeTruthy();

      mocks.folderTree = folderTree;
      rerender(
        <NavigationHarness controller={controller} initialPath="/acme/agents">
          <AgentsPage />
        </NavigationHarness>,
      );
      expect(screen.queryByText("Folders are unavailable. Showing agents in a flat list.")).toBeNull();
      expect(screen.getByRole("button", { name: "Collapse Game design" })).toBeTruthy();
      expect(screen.getByText("squad-leader")).toBeTruthy();
      expect(screen.getByText("squad-member")).toBeTruthy();
      expect(screen.getByText("loose-agent")).toBeTruthy();
    } finally {
      vi.useRealTimers();
    }
  });
});
