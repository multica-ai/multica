import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { ProjectsPage } from "./projects-page";

const mocks = vi.hoisted(() => ({
  projects: [] as Project[],
  members: [] as Array<{ user_id: string; name: string; role: string }>,
  agents: [] as Array<{ id: string; name: string; archived_at: string | null; owner_id?: string }>,
  pins: [] as Array<{ item_type: string; item_id: string }>,
  updateProject: vi.fn(),
  deleteProject: vi.fn(),
  createPin: vi.fn(),
  deletePin: vi.fn(),
  openModal: vi.fn(),
  copyText: vi.fn(),
  copyTextAndHtml: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
  savePrefs: vi.fn(),
  projectViewState: {
    layout: "table",
    groupBy: "none",
    search: "",
    sortField: "name",
    sortDirection: "asc",
    hiddenColumns: [] as string[],
    filters: { statuses: [] as string[], priorities: [] as string[], leads: [] as string[] },
    setLayout: vi.fn(),
    setGroupBy: vi.fn(),
    setSearch: vi.fn(),
    toggleSort: vi.fn(),
    setSortField: vi.fn(),
    setSortDirection: vi.fn(),
    toggleColumn: vi.fn(),
    toggleFilter: vi.fn(),
    clearFilters: vi.fn(),
  },
}));

vi.mock("@multica/ui/lib/clipboard", () => ({
  copyText: mocks.copyText,
  copyTextAndHtml: mocks.copyTextAndHtml,
}));

vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}));

vi.mock("@tanstack/react-query", () => ({
  queryOptions: (options: unknown) => options,
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey?.[0];
    // One project's member list, which a lead picker reads for the access
    // manager of a private project. Nothing here is private.
    if (key === "projects" && options.queryKey?.[2] === "detail") {
      return { data: undefined, isLoading: false };
    }
    if (key === "projects") {
      return { data: mocks.projects, isLoading: false };
    }
    if (key === "members") {
      return { data: mocks.members, isLoading: false };
    }
    if (key === "agents") {
      return { data: mocks.agents, isLoading: false };
    }
    if (key === "pins") {
      return { data: mocks.pins, isLoading: false };
    }
    return { data: [], isLoading: false };
  },
  useQueryClient: () => ({ ensureQueryData: async () => ({ autopilots: [] }) }),
}));

vi.mock("@multica/core/projects", () => ({
  projectListOptions: () => ({ queryKey: ["projects"] }),
  isProjectArchived: (project: { archived_at?: string | null }) => !!project.archived_at,
  useUpdateProject: () => ({ mutate: mocks.updateProject }),
  useDeleteProject: () => ({ mutateAsync: mocks.deleteProject }),
  useArchiveProject: () => ({ mutateAsync: vi.fn() }),
  useRestoreProject: () => ({ mutateAsync: vi.fn() }),
  useProjectViewStore: (selector: (state: unknown) => unknown) =>
    selector(mocks.projectViewState),
  boardGroupBy: (groupBy: string) => (groupBy === "none" ? "status" : groupBy),
  projectLastActivityAt: (p: Project) => p.last_activity_at || p.updated_at,
  useProjectsPagePrefsSync: () => mocks.savePrefs,
}));

vi.mock("@multica/core/pins", () => ({
  pinListOptions: () => ({ queryKey: ["pins"] }),
  useCreatePin: () => ({ mutate: mocks.createPin }),
  useDeletePin: () => ({ mutate: mocks.deletePin }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    projectDetail: (id: string) => `/test-workspace/projects/${id}`,
    memberDetail: (id: string) => `/test-workspace/members/${id}`,
    agentDetail: (id: string) => `/test-workspace/agents/${id}`,
  }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: unknown) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"] }),
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: () => "Test Lead",
    getActorInitials: () => "TL",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: mocks.openModal }),
  },
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => {
  const RadioGroupContext = React.createContext<((value: string) => void) | undefined>(
    undefined,
  );
  return {
    DropdownMenu: ({ children }: { children: React.ReactNode }) => (
      <>{children}</>
    ),
    DropdownMenuTrigger: ({
      render,
      children,
    }: {
      render: React.ReactNode;
      children?: React.ReactNode;
    }) =>
      children && React.isValidElement(render) ? (
        React.cloneElement(render, undefined, children)
      ) : (
        <>{render}</>
      ),
    DropdownMenuContent: ({ children }: { children: React.ReactNode }) => (
      <div>{children}</div>
    ),
    DropdownMenuGroup: ({ children }: { children: React.ReactNode }) => (
      <div>{children}</div>
    ),
    DropdownMenuLabel: ({ children }: { children: React.ReactNode }) => (
      <div>{children}</div>
    ),
    DropdownMenuItem: ({
      children,
      onClick,
    }: {
      children: React.ReactNode;
      onClick?: () => void;
    }) => (
      <button type="button" onClick={onClick}>
        {children}
      </button>
    ),
    DropdownMenuCheckboxItem: ({
      children,
      onCheckedChange,
    }: {
      children: React.ReactNode;
      onCheckedChange?: () => void;
    }) => (
      <button type="button" onClick={onCheckedChange}>
        {children}
      </button>
    ),
    DropdownMenuRadioGroup: ({
      children,
      onValueChange,
    }: {
      children: React.ReactNode;
      onValueChange?: (value: string) => void;
    }) => (
      <RadioGroupContext.Provider value={onValueChange}>
        <div>{children}</div>
      </RadioGroupContext.Provider>
    ),
    DropdownMenuRadioItem: function RadioItem({
      children,
      onClick,
      value,
    }: {
      children: React.ReactNode;
      onClick?: () => void;
      value: string;
    }) {
      const onValueChange = React.useContext(RadioGroupContext);
      return (
        <button
          type="button"
          onClick={() => {
            onClick?.();
            onValueChange?.(value);
          }}
        >
          {children}
        </button>
      );
    },
    DropdownMenuSeparator: () => <hr />,
    DropdownMenuSub: ({ children }: { children: React.ReactNode }) => (
      <>{children}</>
    ),
    DropdownMenuSubContent: ({ children }: { children: React.ReactNode }) => (
      <div>{children}</div>
    ),
    DropdownMenuSubTrigger: ({ children }: { children: React.ReactNode }) => (
      <button type="button">{children}</button>
    ),
  };
});

vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  PopoverContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <div role="tooltip">{children}</div>
  ),
}));

const PROJECT: Project = {
  id: "project-1",
  workspace_id: "workspace-1",
  title: "Launch Plan",
  description: null,
  icon: null,
  status: "in_progress",
  priority: "high",
  lead_type: null,
  lead_id: null,
  start_date: null,
  due_date: null,
  created_at: "2026-06-01T00:00:00Z",
  updated_at: "2026-06-01T00:00:00Z",
  issue_count: 3,
  done_count: 1,
  resource_count: 0,
};

function makeAdapter(
  overrides: Partial<NavigationAdapter> = {},
): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/test-workspace/projects",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderProjects(adapter = makeAdapter()) {
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <ProjectsPage />
    </NavigationProvider>,
  );
  return adapter;
}

function projectRow() {
  const row = screen.getByText(PROJECT.title).closest('[role="row"]');
  if (!row) throw new Error("project row not found");
  return row as HTMLElement;
}

beforeEach(() => {
  mocks.projects = [PROJECT];
  mocks.members = [
    { user_id: "user-1", name: "User One", role: "admin" },
  ];
  mocks.agents = [];
  mocks.pins = [];
  mocks.updateProject.mockClear();
  mocks.deleteProject.mockClear();
  mocks.createPin.mockClear();
  mocks.deletePin.mockClear();
  mocks.openModal.mockClear();
  mocks.copyText.mockReset().mockResolvedValue(true);
  mocks.copyTextAndHtml.mockReset().mockResolvedValue(true);
  mocks.toastSuccess.mockReset();
  mocks.toastError.mockReset();
  mocks.savePrefs.mockClear();
  mocks.projectViewState.setLayout.mockClear();
  mocks.projectViewState.setGroupBy.mockClear();
  mocks.projectViewState.setSearch.mockClear();
  mocks.projectViewState.clearFilters.mockClear();
  mocks.projectViewState.layout = "table";
  mocks.projectViewState.groupBy = "none";
  mocks.projectViewState.search = "";
  mocks.projectViewState.sortField = "name";
  mocks.projectViewState.sortDirection = "asc";
  mocks.projectViewState.hiddenColumns = [];
  mocks.projectViewState.filters = { statuses: [], priorities: [], leads: [] };
});

describe("ProjectsPage deletion permissions", () => {
  it.each(["table", "board"])("checks each project in the %s layout", (layout) => {
    mocks.projectViewState.layout = layout;
    mocks.members[0]!.role = "member";
    mocks.projects = [
      { ...PROJECT, creator_type: "member", creator_id: "user-1" },
      { ...PROJECT, id: "other", title: "Other project", creator_type: "member", creator_id: "someone" },
    ];
    renderProjects();
    expect(screen.getAllByRole("button", { name: "Delete" })).toHaveLength(1);
  });

  it("uses current agent ownership for row deletion", () => {
    mocks.members[0]!.role = "member";
    mocks.projects = [{ ...PROJECT, creator_type: "agent", creator_id: "owned-agent" }];
    mocks.agents = [{ id: "owned-agent", name: "Agent", archived_at: null, owner_id: "user-1" }];
    renderProjects();
    expect(screen.getByRole("button", { name: "Delete" })).toBeInTheDocument();
  });

  it("offers batch deletion only when every selected project is deletable", async () => {
    mocks.members[0]!.role = "member";
    mocks.projects = [
      { ...PROJECT, creator_type: "member", creator_id: "user-1" },
      { ...PROJECT, id: "other", title: "Other project" },
    ];
    const user = userEvent.setup();
    renderProjects();
    const checkboxes = screen.getAllByRole("checkbox", { name: "Select row" });
    await user.click(checkboxes[0]!);
    expect(screen.getAllByRole("button", { name: "Delete" })).toHaveLength(2);
    await user.click(checkboxes[1]!);
    expect(screen.getAllByRole("button", { name: "Delete" })).toHaveLength(1);
  });
});

describe("ProjectsPage rename", () => {
  // The row menu is shared by the table's ⋯ button, its right-click menu and
  // the board cards, so one assertion per layout covers every list entry point.
  it.each(["table", "board"])(
    "opens the rename dialog with the current name from the %s row menu",
    async (layout) => {
      mocks.projectViewState.layout = layout;
      const user = userEvent.setup();
      const push = vi.fn();
      renderProjects(makeAdapter({ push }));

      await user.click(screen.getByRole("button", { name: "Rename" }));

      expect(mocks.openModal).toHaveBeenCalledWith("project-rename", {
        projectId: "project-1",
        title: "Launch Plan",
      });
      expect(push).not.toHaveBeenCalled();
    },
  );
});

describe("ProjectsPage sharing", () => {
  it.each(["table", "board"])(
    "copies the title link immediately after Copy link in the %s row menu",
    async (layout) => {
      mocks.projectViewState.layout = layout;
      const user = userEvent.setup();
      const adapter = renderProjects(makeAdapter({
        getShareableUrl: (path) => `https://app.example${path}`,
      }));
      const copyLink = screen.getByRole("button", { name: "Copy link" });
      const copyTitle = screen.getByRole("button", { name: "Copy link with title" });
      expect(copyLink.nextElementSibling).toBe(copyTitle);

      await user.click(copyTitle);

      expect(mocks.copyTextAndHtml).toHaveBeenCalledWith(
        "[Launch Plan](https://app.example/test-workspace/projects/project-1)",
        '<a href="https://app.example/test-workspace/projects/project-1">Launch Plan</a>',
      );
      expect(mocks.toastSuccess).toHaveBeenCalledWith("Link copied with title");
      expect(adapter.push).not.toHaveBeenCalled();

      await user.click(copyLink);
      expect(mocks.copyText).toHaveBeenCalledWith(
        "https://app.example/test-workspace/projects/project-1",
      );
    },
  );

  it("reports a failed title-link copy without a success toast", async () => {
    mocks.copyTextAndHtml.mockResolvedValueOnce(false);
    const user = userEvent.setup();
    renderProjects();

    await user.click(screen.getByRole("button", { name: "Copy link with title" }));

    expect(mocks.toastError).toHaveBeenCalledWith("Failed to copy link");
    expect(mocks.toastSuccess).not.toHaveBeenCalled();
  });
});

describe("ProjectsPage table row navigation", () => {
  it("renders the project name as text, not a title link", () => {
    renderProjects();

    const row = projectRow();
    expect(within(row).getByText(PROJECT.title).tagName).toBe("SPAN");
    expect(
      within(row).queryByRole("link", { name: PROJECT.title }),
    ).not.toBeInTheDocument();
  });

  it("navigates from the row surface", async () => {
    const user = userEvent.setup();
    const push = vi.fn();
    renderProjects(makeAdapter({ push }));

    await user.click(projectRow());

    expect(push).toHaveBeenCalledWith("/test-workspace/projects/project-1");
    expect(push).toHaveBeenCalledTimes(1);
  });

  it("does not navigate when inline controls are clicked", async () => {
    const user = userEvent.setup();
    const push = vi.fn();
    renderProjects(makeAdapter({ push }));
    const row = projectRow();

    await user.click(within(row).getByRole("checkbox", { name: "Select row" }));
    await user.click(within(row).getByRole("button", { name: "Project actions" }));
    await user.click(within(row).getAllByRole("button", { name: "In Progress" })[0]!);
    await user.click(within(row).getAllByRole("button", { name: "High" })[0]!);
    await user.click(within(row).getByRole("button", { name: "—" }));

    expect(push).not.toHaveBeenCalled();
  });

  it("uses the rowLink modifier and middle-click paths when openInNewTab is available", () => {
    const push = vi.fn();
    const openInNewTab = vi.fn();
    renderProjects(makeAdapter({ push, openInNewTab }));
    const row = projectRow();

    fireEvent.click(row, { metaKey: true });
    fireEvent.click(row, { ctrlKey: true });
    const middleClick = new MouseEvent("auxclick", {
      bubbles: true,
      button: 1,
      cancelable: true,
    });
    row.dispatchEvent(middleClick);

    expect(middleClick.defaultPrevented).toBe(true);
    expect(openInNewTab).toHaveBeenCalledTimes(3);
    expect(openInNewTab).toHaveBeenNthCalledWith(1, "/test-workspace/projects/project-1", "Launch Plan");
    expect(openInNewTab).toHaveBeenNthCalledWith(2, "/test-workspace/projects/project-1", "Launch Plan");
    expect(openInNewTab).toHaveBeenNthCalledWith(3, "/test-workspace/projects/project-1", "Launch Plan");
    expect(push).not.toHaveBeenCalled();
  });

  // Web (no adapter): the row is a <div>, so nothing native catches a
  // modifier or middle click — rowLink opens the browser tab itself instead
  // of navigating in place (MUL-5456).
  it("has a single rowLink path for modifier and middle clicks without openInNewTab", () => {
    const push = vi.fn();
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    renderProjects(makeAdapter({ push }));
    const row = projectRow();

    fireEvent.click(row, { metaKey: true });
    fireEvent.click(row, { ctrlKey: true });
    const middleClick = new MouseEvent("auxclick", {
      bubbles: true,
      button: 1,
      cancelable: true,
    });
    row.dispatchEvent(middleClick);

    expect(middleClick.defaultPrevented).toBe(true);
    expect(open).toHaveBeenCalledTimes(3);
    for (const nth of [1, 2, 3]) {
      expect(open).toHaveBeenNthCalledWith(
        nth,
        "/test-workspace/projects/project-1",
        "_blank",
        "noopener,noreferrer",
      );
    }
    expect(push).not.toHaveBeenCalled();
    open.mockRestore();
  });
});

describe("ProjectsPage header", () => {
  const CLOSED: Project = {
    ...PROJECT,
    id: "project-2",
    title: "Old Roadmap",
    status: "completed",
  };

  it("titles the page with the scope and narrows rows from the scope menu", async () => {
    const user = userEvent.setup();
    mocks.projects = [PROJECT, CLOSED];
    renderProjects();

    expect(screen.getByRole("heading", { level: 1, name: "All projects" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Active projects/ })).toHaveTextContent("1");
    expect(screen.getByText("Old Roadmap")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^Active projects/ }));
    expect(screen.getByRole("heading", { level: 1, name: "Active projects" })).toBeInTheDocument();
    expect(screen.getByText(PROJECT.title)).toBeInTheDocument();
    expect(screen.queryByText("Old Roadmap")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^Closed projects/ }));
    expect(screen.queryByText(PROJECT.title)).not.toBeInTheDocument();
    expect(screen.getByText("Old Roadmap")).toBeInTheDocument();
  });

  it("keeps archived projects out of every scope but Archived", async () => {
    const user = userEvent.setup();
    const ARCHIVED: Project = {
      ...CLOSED,
      id: "project-archived",
      title: "Shelved Launch",
      archived_at: "2026-01-02T00:00:00Z",
      archived_by_type: "member",
      archived_by_id: "user-1",
    };
    mocks.projects = [PROJECT, CLOSED, ARCHIVED];
    renderProjects();

    expect(screen.queryByText("Shelved Launch")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Archived projects/ })).toHaveTextContent("1");

    await user.click(screen.getByRole("button", { name: /^Closed projects/ }));
    expect(screen.queryByText("Shelved Launch")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^Archived projects/ }));
    expect(screen.getByRole("heading", { level: 1, name: "Archived projects" })).toBeInTheDocument();
    expect(screen.getByText("Shelved Launch")).toBeInTheDocument();
    expect(screen.queryByText("Old Roadmap")).not.toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Restore" }).length).toBeGreaterThan(0);
  });

  it("offers search, and Reset only while a filter or search narrows the list", () => {
    mocks.projects = [PROJECT, CLOSED];
    renderProjects();

    const header = screen.getByRole("banner");
    expect(within(header).getByRole("button", { name: "Search projects" })).toBeInTheDocument();
    expect(within(header).queryByRole("button", { name: "Reset" })).not.toBeInTheDocument();
  });

  it("matches the search against project names, and Reset clears search and filters", async () => {
    const user = userEvent.setup();
    mocks.projects = [PROJECT, CLOSED];
    mocks.projectViewState.search = "  ROADMAP ";
    renderProjects();

    expect(screen.queryByText(PROJECT.title)).not.toBeInTheDocument();
    expect(screen.getByText("Old Roadmap")).toBeInTheDocument();
    await user.click(within(screen.getByRole("banner")).getByRole("button", { name: "Reset" }));
    expect(mocks.projectViewState.clearFilters).toHaveBeenCalled();
    expect(mocks.projectViewState.setSearch).toHaveBeenCalledWith("");
  });

  it("names the active filter count on the filter control, and Reset clears the filters", async () => {
    const user = userEvent.setup();
    mocks.projectViewState.filters = { statuses: ["in_progress"], priorities: [], leads: [] };
    renderProjects();

    expect(screen.getByRole("button", { name: "1 filter" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Reset" }));
    expect(mocks.projectViewState.clearFilters).toHaveBeenCalled();
  });

  it("switches between table and board from the display settings", async () => {
    const user = userEvent.setup();
    renderProjects();

    expect(screen.getByRole("button", { name: "Display settings" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Table", pressed: true })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Cards" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Board", pressed: false }));
    expect(mocks.projectViewState.setLayout).toHaveBeenCalledWith("board");
    expect(mocks.savePrefs).toHaveBeenCalledTimes(1);
  });

  it("offers no grouping only in the table, and sets the grouping", async () => {
    const user = userEvent.setup();
    const grouping = () => screen.getByText("Grouping").parentElement as HTMLElement;
    renderProjects();

    expect(within(grouping()).getAllByRole("button", { name: "No grouping" }).length).toBeGreaterThan(0);
    await user.click(within(grouping()).getByRole("button", { name: "Priority" }));
    expect(mocks.projectViewState.setGroupBy).toHaveBeenCalledWith("priority");
    expect(mocks.savePrefs).toHaveBeenCalled();

    cleanup();
    mocks.projectViewState.layout = "board";
    renderProjects();
    expect(within(grouping()).queryByRole("button", { name: "No grouping" })).not.toBeInTheDocument();
  });

  it("creates a project from the header", async () => {
    const user = userEvent.setup();
    renderProjects();

    await user.click(screen.getByRole("button", { name: "New project" }));
    expect(mocks.openModal).toHaveBeenCalledWith("create-project");
  });

  it("introduces projects when there are none and creates the first one", async () => {
    const user = userEvent.setup();
    mocks.projects = [];
    renderProjects();

    const intro = screen.getByRole("region", { name: "Break big work into projects" });
    expect(screen.queryByRole("grid")).not.toBeInTheDocument();
    await user.click(within(intro).getByRole("button", { name: "New project" }));
    expect(mocks.openModal).toHaveBeenCalledWith("create-project");
  });
});

describe("ProjectsPage grouping and board", () => {
  const PLANNED: Project = { ...PROJECT, id: "project-3", title: "Next Bets", status: "planned" };
  const CLOSED: Project = { ...PROJECT, id: "project-2", title: "Old Roadmap", status: "completed" };

  it("splits the table into status sections, skipping empty ones", () => {
    mocks.projects = [PROJECT, PLANNED, CLOSED];
    mocks.projectViewState.groupBy = "status";
    renderProjects();

    expect(screen.getAllByRole("rowheader").map((band) => band.textContent)).toEqual([
      "Planned1",
      "In Progress1",
      "Completed1",
    ]);
  });

  it("shows every status the scope allows as a board column, empty ones included", async () => {
    const user = userEvent.setup();
    mocks.projects = [PROJECT, PLANNED, CLOSED];
    mocks.projectViewState.layout = "board";
    renderProjects();

    expect(screen.queryByRole("grid")).not.toBeInTheDocument();
    const inProgress = screen.getByRole("region", { name: "In Progress" });
    expect(within(inProgress).getByText(PROJECT.title)).toBeInTheDocument();
    expect(within(screen.getByRole("region", { name: "Paused" })).getByText("No projects")).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Completed" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^Active projects/ }));
    expect(screen.queryByRole("region", { name: "Completed" })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Cancelled" })).not.toBeInTheDocument();
  });

  it("puts the board in lead columns with a no-lead column last", () => {
    mocks.projects = [{ ...PROJECT, lead_type: "member", lead_id: "user-1" }, PLANNED];
    mocks.projectViewState.layout = "board";
    mocks.projectViewState.groupBy = "lead";
    renderProjects();

    const columns = screen.getAllByRole("region");
    expect(columns.map((c) => c.getAttribute("aria-label"))).toEqual(["Test Lead", "No lead"]);
    expect(within(columns[1]!).getByText(PLANNED.title)).toBeInTheDocument();
  });

  it("sets status, priority and lead from a board card without opening the project", async () => {
    mocks.projectViewState.layout = "board";
    const user = userEvent.setup();
    const push = vi.fn();
    renderProjects(makeAdapter({ push }));
    // Menus render inline in this file, so each card holds its pickers' options.
    const card = within(screen.getByRole("link", { name: /Launch Plan/ }));
    expect(card.getAllByRole("button", { name: "In Progress" }).length).toBeGreaterThan(0);
    expect(card.getAllByRole("button", { name: "High" })[0]).toHaveAttribute("title", "High");

    await user.click(card.getByRole("button", { name: "Paused" }));
    expect(mocks.updateProject).toHaveBeenLastCalledWith({ id: "project-1", status: "paused" });

    await user.click(card.getByRole("button", { name: "Urgent" }));
    expect(mocks.updateProject).toHaveBeenLastCalledWith({ id: "project-1", priority: "urgent" });

    // A lead nobody has yet has no column to drop on.
    await user.click(card.getByRole("button", { name: "User One" }));
    expect(mocks.updateProject).toHaveBeenLastCalledWith({
      id: "project-1",
      lead_type: "member",
      lead_id: "user-1",
    });

    expect(push).not.toHaveBeenCalled();
  });

  it("links each board card to its project", () => {
    mocks.projectViewState.layout = "board";
    renderProjects();

    expect(screen.getByRole("link", { name: /Launch Plan/ })).toHaveAttribute(
      "href",
      "/test-workspace/projects/project-1",
    );
  });
});

describe("ProjectsPage dates", () => {
  const titles = () =>
    screen
      .getAllByRole("row")
      .slice(1)
      .map((row) => within(row).getByText(/^(Due|No due)/).textContent);

  it("sorts by due date with undated projects last in either direction", () => {
    mocks.projects = [
      { ...PROJECT, id: "a", title: "Due later", due_date: "2030-02-01" },
      { ...PROJECT, id: "b", title: "No due date", due_date: null },
      { ...PROJECT, id: "c", title: "Due sooner", due_date: "2030-01-01" },
    ];
    mocks.projectViewState.sortField = "due_date";

    mocks.projectViewState.sortDirection = "asc";
    renderProjects();
    expect(titles()).toEqual(["Due sooner", "Due later", "No due date"]);

    cleanup();
    mocks.projectViewState.sortDirection = "desc";
    renderProjects();
    expect(titles()).toEqual(["Due later", "Due sooner", "No due date"]);
  });

  it("sorts by recent activity, counting issue activity over the project's own edit", () => {
    mocks.projects = [
      { ...PROJECT, id: "a", title: "Due edited", updated_at: "2026-06-03T00:00:00Z" },
      {
        ...PROJECT,
        id: "b",
        title: "Due busy",
        updated_at: "2026-06-01T00:00:00Z",
        last_activity_at: "2026-06-05T00:00:00Z",
      },
    ];
    mocks.projectViewState.sortField = "last_activity";
    mocks.projectViewState.sortDirection = "desc";
    renderProjects();
    expect(titles()).toEqual(["Due busy", "Due edited"]);
  });

  it("marks an open project's overdue due date, but not a closed one's", () => {
    mocks.projects = [
      { ...PROJECT, id: "late", title: "Late project", due_date: "2020-03-05" },
      { ...PROJECT, id: "done", title: "Done project", status: "completed", due_date: "2020-03-06" },
    ];
    renderProjects();

    expect(screen.getByText("Mar 5, 2020")).toHaveClass("text-destructive");
    expect(screen.getByText("Mar 6, 2020")).not.toHaveClass("text-destructive");
  });
});
