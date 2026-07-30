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
  installations: [] as Array<{
    id: string;
    agent_id: string;
    status: string;
  }>,
  pins: [] as Array<{ item_type: string; item_id: string }>,
  updateProject: vi.fn(),
  beginFeishuBinding: vi.fn(),
  deleteFeishuBinding: vi.fn(),
  deleteProject: vi.fn(),
  createPin: vi.fn(),
  deletePin: vi.fn(),
  openModal: vi.fn(),
  projectViewState: {
    viewMode: "compact",
    sortField: "name",
    sortDirection: "asc",
    hiddenColumns: [] as string[],
    filters: { statuses: [], priorities: [], leads: [] },
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
  queryOptions: <T,>(options: T) => options,
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
    if (key === "lark") {
      return {
        data: {
          installations: mocks.installations,
          configured: true,
          install_supported: true,
        },
        isLoading: false,
      };
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

vi.mock("@multica/core/lark", () => ({
  larkInstallationsOptions: () => ({ queryKey: ["lark"] }),
}));

vi.mock("@multica/core/projects/mutations", () => ({
  useBeginProjectFeishuBinding: () => ({
    mutate: mocks.beginFeishuBinding,
    isPending: false,
  }),
  useDeleteProjectFeishuBinding: () => ({
    mutate: mocks.deleteFeishuBinding,
    isPending: false,
  }),
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
  useCurrentWorkspace: () => null,
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

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => (
    <>{children}</>
  ),
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => (
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
  DropdownMenuRadioGroup: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuRadioItem: ({
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
}));

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
  mocks.installations = [];
  mocks.pins = [];
  mocks.updateProject.mockClear();
  mocks.beginFeishuBinding.mockClear();
  mocks.deleteFeishuBinding.mockClear();
  mocks.deleteProject.mockClear();
  mocks.createPin.mockClear();
  mocks.deletePin.mockClear();
  mocks.openModal.mockClear();
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

  it("binds a Project from the assignee-style Feishu Bot picker without navigating", async () => {
    const user = userEvent.setup();
    const push = vi.fn();
    mocks.installations = [
      { id: "installation-1", agent_id: "agent-1", status: "active" },
    ];
    mocks.agents = [
      { id: "agent-1", name: "Frontend Bot", archived_at: null },
    ];
    renderProjects(makeAdapter({ push }));

    expect(screen.getByText("Feishu sync Bot")).toBeInTheDocument();
    expect(
      within(projectRow()).getByRole("button", { name: "Agent Bot" }),
    ).toHaveTextContent("No Bot");
    const frontendBotOptions = within(projectRow()).getAllByRole("button", {
      name: /Frontend Bot/,
    });
    await user.click(frontendBotOptions[frontendBotOptions.length - 1]!);

    expect(mocks.beginFeishuBinding).toHaveBeenCalledWith(
      { projectId: "project-1", installationId: "installation-1" },
      expect.objectContaining({
        onSuccess: expect.any(Function),
        onError: expect.any(Function),
      }),
    );
    expect(push).not.toHaveBeenCalled();
  });

  it("shows the active Feishu Bot and does not bind it again", async () => {
    const user = userEvent.setup();
    mocks.installations = [
      { id: "installation-1", agent_id: "agent-1", status: "active" },
    ];
    mocks.agents = [
      { id: "agent-1", name: "Leader Bot", archived_at: null },
    ];
    mocks.projects = [
      {
        ...PROJECT,
        feishu_sync: {
          state: "active",
          project_binding_id: "binding-1",
          installation_id: "installation-1",
          bot_name: "Leader Bot",
          agent_id: "agent-1",
          agent_name: "Leader Bot",
          chat_id: "chat-1",
          chat_name: "Project Group",
          bound_issue_count: 0,
          manual_unbound_issue_count: 0,
          total_issue_count: 3,
          pending_notification_count: 0,
          last_synced_at: null,
        },
      },
    ];
    renderProjects();

    expect(
      within(projectRow()).getByRole("button", { name: "Agent Bot" }),
    ).toHaveTextContent("Leader Bot");
    const leaderBotOptions = within(projectRow()).getAllByRole("button", {
      name: /Leader Bot/,
    });
    await user.click(leaderBotOptions[leaderBotOptions.length - 1]!);

    expect(mocks.beginFeishuBinding).not.toHaveBeenCalled();
  });

  it("does not navigate when inline controls are clicked", async () => {
    const user = userEvent.setup();
    const push = vi.fn();
    renderProjects(makeAdapter({ push }));
    const row = projectRow();

    await user.click(within(row).getByRole("button", { pressed: false }));
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
    expect(openInNewTab).toHaveBeenNthCalledWith(1, "/test-workspace/projects/project-1");
    expect(openInNewTab).toHaveBeenNthCalledWith(2, "/test-workspace/projects/project-1");
    expect(openInNewTab).toHaveBeenNthCalledWith(3, "/test-workspace/projects/project-1");
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
