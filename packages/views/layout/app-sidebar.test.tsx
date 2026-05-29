import { act, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@multica/core/api";
import { AppSidebar } from "./app-sidebar";

const { activeIssueContext, detail, deletePin, pins, pathname, openModal } = vi.hoisted(() => ({
  activeIssueContext: {
    current: null as null | {
      issueId: string;
      identifier: string;
      projectId: string | null;
    },
  },
  detail: { current: { isPending: false, isError: false, data: null as unknown, error: null as unknown } },
  deletePin: vi.fn(),
  openModal: vi.fn(),
  pathname: { current: "/acme/issues" },
  pins: {
    current: [
      {
        id: "pin-1",
        workspace_id: "ws-1",
        user_id: "user-1",
        item_type: "issue" as const,
        item_id: "issue-1",
        position: 0,
        created_at: "2026-05-06T00:00:00Z",
      },
    ],
  },
}));

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PointerSensor: vi.fn(),
  closestCenter: vi.fn(),
  useSensor: vi.fn(),
  useSensors: vi.fn(),
}));
vi.mock("@dnd-kit/sortable", () => ({
  SortableContext: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  useSortable: () => ({ attributes: {}, listeners: {}, setNodeRef: vi.fn() }),
  verticalListSortingStrategy: vi.fn(),
}));
vi.mock("@dnd-kit/utilities", () => ({ CSS: { Transform: { toString: () => undefined } } }));
vi.mock("@multica/ui/components/ui/sidebar", () => ({
  Sidebar: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarFooter: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarGroup: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarGroupContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarGroupLabel: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarHeader: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarMenuButton: ({
    children,
    onClick,
    render,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
    render?: React.ReactNode;
  }) => render ? (
    <a href={(render as React.ReactElement<{ href: string }>).props.href} onClick={onClick}>
      {children}
    </a>
  ) : (
    <button type="button" onClick={onClick}>{children}</button>
  ),
  SidebarMenuItem: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SidebarRail: () => null,
}));
vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuContent: () => null,
  DropdownMenuGroup: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuLabel: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuSeparator: () => null,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
}));
vi.mock("@multica/ui/components/ui/collapsible", () => ({
  Collapsible: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  CollapsibleContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  CollapsibleTrigger: () => <button type="button" />,
}));
vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => <button type="button">{children}</button>,
}));
vi.mock("./help-launcher", () => ({ HelpLauncher: () => null }));
vi.mock("../auth", () => ({ useLogout: () => vi.fn() }));
vi.mock("../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));
vi.mock("../navigation", () => ({
  AppLink: ({ children, href }: { children: React.ReactNode; href: string }) => <a href={href}>{children}</a>,
  useNavigation: () => ({ pathname: pathname.current, push: vi.fn() }),
}));
vi.mock("../projects/components/project-icon", () => ({ ProjectIcon: () => <span /> }));
vi.mock("../workspace/workspace-avatar", () => ({ WorkspaceAvatar: () => <span /> }));
vi.mock("@multica/ui/components/common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) => selector({ user: { id: "user-1" } }),
}));
vi.mock("@multica/core/paths", () => ({
  paths: {
    workspace: (slug: string) => ({
      root: () => `/${slug}`,
      issues: () => `/${slug}/issues`,
      issueDetail: (id: string) => `/${slug}/issues/${id}`,
      projectDetail: (id: string) => `/${slug}/projects/${id}`,
    }),
  },
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
  useWorkspacePaths: () => ({
    root: () => "/acme",
    inbox: () => "/acme/inbox",
    myIssues: () => "/acme/my-issues",
    issues: () => "/acme/issues",
    projects: () => "/acme/projects",
    autopilots: () => "/acme/autopilots",
    agents: () => "/acme/agents",
    squads: () => "/acme/squads",
    usage: () => "/acme/usage",
    agentDashboard: () => "/acme/agent-dashboard",
    runtimes: () => "/acme/runtimes",
    skills: () => "/acme/skills",
    wiki: () => "/acme/wiki",
    settings: () => "/acme/settings",
    issueDetail: (id: string) => `/acme/issues/${id}`,
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
}));
vi.mock("@multica/core/api", async (importOriginal) => ({ ...(await importOriginal<typeof import("@multica/core/api")>()), api: {} }));
vi.mock("@multica/core/inbox/queries", () => ({ deduplicateInboxItems: (items: unknown[]) => items, inboxKeys: { list: () => ["inbox"] } }));
vi.mock("@multica/core/issues/queries", () => ({ issueDetailOptions: () => ({ queryKey: ["issue"] }) }));
vi.mock("@multica/core/issues/stores/create-mode-store", () => ({
  useCreateModeStore: { getState: () => ({ lastMode: "manual" }) },
  openCreateIssueWithPreference: (data?: Record<string, unknown> | null) =>
    openModal("create-issue", data ?? null),
}));
vi.mock("@multica/core/issues/stores/active-issue-context-store", () => ({
  useActiveIssueContextStore: (selector: (state: typeof activeIssueContext) => unknown) =>
    selector(activeIssueContext),
}));
vi.mock("@multica/core/issues/stores/draft-store", () => ({ useIssueDraftStore: () => false }));
vi.mock("@multica/core/modals", () => ({ useModalStore: { getState: () => ({ modal: null, open: openModal }) } }));
vi.mock("@multica/core/pins/mutations", () => ({ useDeletePin: () => ({ mutate: deletePin }), useReorderPins: () => ({ mutate: vi.fn() }) }));
vi.mock("@multica/core/pins/queries", () => ({ pinListOptions: () => ({ queryKey: ["pins"] }) }));
vi.mock("@multica/core/projects/queries", () => ({ projectDetailOptions: () => ({ queryKey: ["project"] }) }));
vi.mock("@multica/core/runtimes/hooks", () => ({ useMyRuntimesNeedUpdate: () => false }));
vi.mock("@multica/core/workspace/queries", () => ({
  myInvitationListOptions: () => ({ queryKey: ["invitations"] }),
  workspaceKeys: { myInvitations: () => ["invitations"] },
  workspaceListOptions: () => ({ queryKey: ["workspaces"] }),
}));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useMutation: () => ({ isPending: false, mutate: vi.fn() }),
  useQuery: ({ queryKey }: { queryKey: readonly unknown[] }) => {
    if (queryKey[0] === "pins") return { data: pins.current };
    if (queryKey[0] === "issue") return detail.current;
    return { data: [] };
  },
  useQueryClient: () => ({ fetchQuery: vi.fn(), invalidateQueries: vi.fn() }),
}));

describe("PinRow", () => {
  beforeEach(() => {
    deletePin.mockReset();
    openModal.mockReset();
    activeIssueContext.current = null;
    pathname.current = "/acme/issues";
    detail.current = { isPending: false, isError: false, data: null, error: null };
  });

  it("unpins missing details", async () => {
    detail.current = { isPending: false, isError: true, data: null, error: new ApiError("missing", 404, "Not Found") };
    render(<AppSidebar />);
    await waitFor(() => expect(deletePin).toHaveBeenCalledTimes(1));
  });

  it("ignores non-404 errors", async () => {
    detail.current = { isPending: false, isError: true, data: null, error: new ApiError("error", 500, "Server Error") };
    render(<AppSidebar />);
    await waitFor(() => expect(deletePin).not.toHaveBeenCalled());
  });

  it("renders loaded details", async () => {
    detail.current = { isPending: false, isError: false, data: { identifier: "MUL-123", title: "Keep this pin", status: "todo" }, error: null };
    render(<AppSidebar />);
    expect(await screen.findByText("MUL-123 Keep this pin")).toBeInTheDocument();
  });

  it("uses the workspace slug and issue identifier for pinned issue links", async () => {
    detail.current = { isPending: false, isError: false, data: { identifier: "MUL-123", title: "Keep this pin", status: "todo" }, error: null };
    render(<AppSidebar />);
    const link = await screen.findByRole("link", { name: /MUL-123 Keep this pin/ });
    expect(link).toHaveAttribute("href", "/acme/issues/MUL-123");
  });

  it("opens manual create issue with project prefill on the global shortcut", () => {
    pathname.current = "/acme/projects/project-1";
    render(<AppSidebar />);

    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "c" }));
    });

    expect(openModal).toHaveBeenCalledWith("create-issue", {
      project_id: "project-1",
    });
  });

  it("opens manual create issue with the current issue project on the global shortcut", () => {
    pathname.current = "/acme/issues/MUL-1";
    detail.current = {
      isPending: false,
      isError: false,
      data: {
        id: "issue-1",
        identifier: "MUL-1",
        title: "Project issue",
        status: "todo",
        project_id: "project-1",
      },
      error: null,
    };
    render(<AppSidebar />);

    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "c" }));
    });

    expect(openModal).toHaveBeenCalledWith("create-issue", {
      project_id: "project-1",
    });
  });

  it("opens manual create issue with the active embedded issue project on the global shortcut", () => {
    pathname.current = "/acme/inbox";
    activeIssueContext.current = {
      issueId: "issue-1",
      identifier: "MUL-1",
      projectId: "project-1",
    };
    render(<AppSidebar />);

    act(() => {
      document.dispatchEvent(new KeyboardEvent("keydown", { key: "c" }));
    });

    expect(openModal).toHaveBeenCalledWith("create-issue", {
      project_id: "project-1",
    });
  });
});
