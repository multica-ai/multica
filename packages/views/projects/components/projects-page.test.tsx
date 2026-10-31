import React from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { ProjectsPage } from "./projects-page";

const mocks = vi.hoisted(() => ({
  projects: [] as Project[],
  members: [] as Array<{ user_id: string; name: string; role: string }>,
  agents: [] as Array<{ id: string; name: string; archived_at: string | null }>,
  pins: [] as Array<{ item_type: string; item_id: string }>,
  updateProject: vi.fn(),
  deleteProject: vi.fn(),
  createPin: vi.fn(),
  deletePin: vi.fn(),
  openModal: vi.fn(),
  projectViewState: {
    viewMode: "compact",
    sortField: "name",
    sortDirection: "asc",
    hiddenColumns: [] as string[],
    filters: { statuses: [] as string[], priorities: [] as string[], leads: [] as string[] },
    setViewMode: vi.fn(),
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
    const key = options.queryKey?.[0];
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
}));

vi.mock("@multica/core/projects", () => ({
  projectListOptions: () => ({ queryKey: ["projects"] }),
  useUpdateProject: () => ({ mutate: mocks.updateProject }),
  useDeleteProject: () => ({ mutate: mocks.deleteProject }),
  useProjectViewStore: (selector: (state: unknown) => unknown) =>
    selector(mocks.projectViewState),
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
  mocks.projectViewState.setViewMode.mockClear();
  mocks.projectViewState.clearFilters.mockClear();
  mocks.projectViewState.viewMode = "compact";
  mocks.projectViewState.sortField = "name";
  mocks.projectViewState.sortDirection = "asc";
  mocks.projectViewState.hiddenColumns = [];
  mocks.projectViewState.filters = { statuses: [], priorities: [], leads: [] };
});

describe("ProjectsPage compact row navigation", () => {
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

  it("has no search, and offers Reset only while a filter narrows the list", () => {
    mocks.projects = [PROJECT, CLOSED];
    renderProjects();

    const header = screen.getByRole("banner");
    expect(within(header).queryByRole("button", { name: /search/i })).not.toBeInTheDocument();
    expect(within(header).queryByRole("textbox")).not.toBeInTheDocument();
    expect(within(header).queryByRole("button", { name: "Reset" })).not.toBeInTheDocument();
  });

  it("names the active filter count on the filter control, and Reset clears the filters", async () => {
    const user = userEvent.setup();
    mocks.projectViewState.filters = { statuses: ["in_progress"], priorities: [], leads: [] };
    renderProjects();

    expect(screen.getByRole("button", { name: "1 filter" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Reset" }));
    expect(mocks.projectViewState.clearFilters).toHaveBeenCalled();
  });

  it("switches between table and cards from the display settings", async () => {
    const user = userEvent.setup();
    renderProjects();

    expect(screen.getByRole("button", { name: "Display settings" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Table", pressed: true })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Cards", pressed: false }));
    expect(mocks.projectViewState.setViewMode).toHaveBeenCalledWith("comfortable");
  });

  it("creates a project from the header", async () => {
    const user = userEvent.setup();
    renderProjects();

    await user.click(screen.getByRole("button", { name: "New project" }));
    expect(mocks.openModal).toHaveBeenCalledWith("create-project");
  });
});
