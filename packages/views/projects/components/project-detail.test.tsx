import React from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, screen, waitFor, within } from "@testing-library/react";
import { onlineManager, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { projectKeys } from "@multica/core/projects/queries";
import { useModalStore } from "@multica/core/modals";
import userEvent from "@testing-library/user-event";
import type { Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { useProjectTabStore } from "@multica/core/projects/tab-store";
import { getIssueSurfaceViewStore, useIssuesScopeStore } from "@multica/core/issues/stores";
import { ProjectDetail } from "./project-detail";

const mocks = vi.hoisted(() => ({
  role: "admin",
  copyText: vi.fn(),
  copyTextAndHtml: vi.fn(),
  deleteProject: vi.fn(),
  getProject: vi.fn(),
  getProjectMembers: vi.fn(),
  getProjectSubscription: vi.fn(),
  setProjectSubscription: vi.fn(),
  getShareableUrl: vi.fn((path: string) => `https://app.example${path}`),
  push: vi.fn(),
  replace: vi.fn(),
  recordVisit: vi.fn(),
  toastSuccess: vi.fn(),
  toastError: vi.fn(),
}));

vi.mock("@multica/ui/lib/clipboard", () => ({
  copyText: mocks.copyText,
  copyTextAndHtml: mocks.copyTextAndHtml,
}));

vi.mock("../../workflows/project-workflow-sheet", () => ({
  ProjectWorkflowMenuItem: () => null,
  ProjectWorkflowSheet: () => null,
}));
vi.mock("../../workflows/project-workflow-row", () => ({ ProjectWorkflowControl: () => null }));

vi.mock("@multica/core/pins", () => ({
  pinListOptions: () => ({ queryKey: ["pins"], queryFn: async () => [] }),
  useCreatePin: () => ({ mutate: vi.fn() }),
  useDeletePin: () => ({ mutate: vi.fn() }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: async () => [{ user_id: "user-1", name: "User One", role: mocks.role }],
  }),
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: async () => [] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "workspace-1",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/chat", () => ({
  useRecentContextStore: (
    selector: (state: { recordVisit: typeof mocks.recordVisit }) => unknown,
  ) => selector({ recordVisit: mocks.recordVisit }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    projects: () => "/test-workspace/projects",
  }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "User One" }),
}));

vi.mock("sonner", () => ({
  toast: { success: mocks.toastSuccess, error: mocks.toastError },
}));

vi.mock("react-resizable-panels", () => ({
  useDefaultLayout: () => ({
    defaultLayout: undefined,
    onLayoutChanged: vi.fn(),
  }),
  usePanelRef: () => ({
    current: {
      isCollapsed: () => false,
      expand: vi.fn(),
      collapse: vi.fn(),
    },
  }),
}));

vi.mock("@multica/ui/hooks/use-mobile", () => ({
  useIsMobile: () => false,
}));

vi.mock("@multica/ui/components/common/emoji-picker", () => ({
  EmojiPicker: () => null,
}));

vi.mock("@multica/ui/components/ui/resizable", () => ({
  ResizablePanelGroup: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  ResizablePanel: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  ResizableHandle: () => null,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    onClick,
    disabled,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
    disabled?: boolean;
  }) => (
    <button type="button" onClick={onClick} disabled={disabled}>
      {children}
    </button>
  ),
  DropdownMenuSeparator: () => <hr />,
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/sheet", () => ({
  Sheet: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  SheetContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("../../editor", () => ({
  TitleEditor: ({ defaultValue }: { defaultValue: string }) => (
    <div>{defaultValue}</div>
  ),
  ContentEditor: () => null,
  ReadonlyContent: ({ content }: { content: string }) => <div>{content}</div>,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => null,
}));

vi.mock("../../issues/components/priority-icon", () => ({
  PriorityIcon: () => null,
}));

vi.mock("./project-resources-section", () => ({
  ProjectResourcesSection: ({ readOnly }: { readOnly?: boolean }) => (
    <div data-testid="project-resources" data-read-only={String(!!readOnly)} />
  ),
}));

vi.mock("./project-insights", () => ({
  ProjectInsights: ({ onFilter, onShowActivity, readOnly }: {
    onFilter: (filter: unknown) => void;
    onShowActivity: () => void;
    readOnly?: boolean;
  }) => (
    <div data-testid="project-insights" data-read-only={String(!!readOnly)}>
      <button type="button" onClick={() => onFilter({ kind: "label", id: "label-1" })}>Filter by Design</button>
      <button type="button" onClick={onShowActivity}>View all activity</button>
    </div>
  ),
}));

vi.mock("./project-activity", () => ({
  ProjectActivityPage: () => <div data-testid="project-activity-page" />,
}));

vi.mock("./project-start-date-picker", () => ({
  ProjectStartDatePicker: () => null,
}));

vi.mock("./project-due-date-picker", () => ({
  ProjectDueDatePicker: () => null,
}));

vi.mock("../../issues/surface/issue-surface", () => ({
  IssueSurface: ({
    renderHeader,
  }: {
    renderHeader?: (ctx: { controller: unknown }) => React.ReactNode;
  }) => <>{renderHeader?.({ controller: {} })}</>,
}));

vi.mock("../../issue-views/view-header", () => ({
  IssueViewHeader: ({
    baseMenu,
    afterTitle,
    trailing,
  }: {
    baseMenu?: React.ReactNode;
    afterTitle?: React.ReactNode;
    trailing?: React.ReactNode;
  }) => (
    <header data-testid="issue-view-header">
      {baseMenu}
      {afterTitle}
      {trailing}
    </header>
  ),
}));

vi.mock("../../layout/animated-right-sidebar", () => ({
  AnimatedRightSidebar: ({ children }: { children: React.ReactNode }) => (
    <aside>{children}</aside>
  ),
  getAnimatedRightSidebarInitialOpen: () => true,
  rightSidebarPanelMotionProps: {},
  useRightSidebarShortcut: vi.fn(),
  useAnimatedRightSidebarState: () => ({
    open: true,
    visualOpen: true,
    motionEnabled: false,
    beginToggle: vi.fn(),
    handleResize: vi.fn(),
  }),
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

let qc: QueryClient;
let pending: ReturnType<typeof deferred>;

function deferred() {
  let resolve!: () => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<void>((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}

function renderProjectDetail(search = "") {
  qc.setQueryData(["members"], [{ user_id: "user-1", name: "User One", role: mocks.role }]);
  const adapter: NavigationAdapter = {
    push: mocks.push,
    replace: mocks.replace,
    back: vi.fn(),
    pathname: "/test-workspace/projects/project-1",
    searchParams: new URLSearchParams(search),
    hash: "",
    getShareableUrl: mocks.getShareableUrl,
  };

  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter}>
        <ProjectDetail projectId={PROJECT.id} />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  mocks.role = "admin";
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(projectKeys.detail("workspace-1", PROJECT.id), PROJECT);
  qc.setQueryData(["agents"], []);
  qc.setQueryData(["pins"], []);
  pending = deferred();
  mocks.copyText.mockReset().mockResolvedValue(true);
  mocks.copyTextAndHtml.mockReset().mockResolvedValue(true);
  mocks.deleteProject.mockReset().mockReturnValue(pending.promise);
  mocks.getProject.mockReset().mockResolvedValue(PROJECT);
  mocks.getProjectMembers.mockReset().mockResolvedValue({ members: [], manager_id: null });
  mocks.getProjectSubscription.mockReset().mockResolvedValue(false);
  mocks.setProjectSubscription.mockReset().mockImplementation(async (_id: string, subscribed: boolean) => subscribed);
  setApiInstance({
    deleteProject: mocks.deleteProject,
    getProject: mocks.getProject,
    getProjectMembers: mocks.getProjectMembers,
    getProjectSubscription: mocks.getProjectSubscription,
    setProjectSubscription: mocks.setProjectSubscription,
  } as unknown as ApiClient);
  mocks.getShareableUrl.mockClear();
  mocks.push.mockReset();
  mocks.replace.mockReset();
  useProjectTabStore.setState({ lastTab: {} });
  mocks.recordVisit.mockReset();
  mocks.toastSuccess.mockReset();
  mocks.toastError.mockReset();
});

afterEach(() => {
  onlineManager.setOnline(true);
  qc.clear();
  useModalStore.getState().close();
});

describe("ProjectDetail tabs", () => {
  it("opens on Overview, with the title and description in the page and not the sidebar", () => {
    renderProjectDetail();
    expect(screen.getByRole("tab", { name: "Overview" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("tab", { name: /Issues\s*3/ })).toBeInTheDocument();
    expect(screen.queryByTestId("issue-view-header")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Description" })).toBeInTheDocument();
    expect(within(screen.getByRole("complementary")).queryByText("Description")).not.toBeInTheDocument();
  });

  it("opens the panel with the properties, above the insights", () => {
    renderProjectDetail();
    const sidebar = screen.getByRole("complementary");
    const properties = within(sidebar).getByTestId("project-properties");
    expect(within(properties).getByRole("button", { name: "No lead" })).toBeInTheDocument();
    expect(within(sidebar).getByTestId("project-insights")).toBeInTheDocument();
    expect(
      properties.compareDocumentPosition(within(sidebar).getByTestId("project-insights")) &
        Node.DOCUMENT_POSITION_FOLLOWING,
    ).toBeTruthy();
  });

  it("shows the activity feed for ?tab=activity", async () => {
    renderProjectDetail("tab=activity");
    expect(screen.getByRole("tab", { name: "Activity" })).toHaveAttribute("aria-selected", "true");
    expect(screen.getByTestId("project-activity-page")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Description" })).not.toBeInTheDocument();
    expect(within(screen.getByRole("complementary")).getByTestId("project-properties")).toBeInTheDocument();
  });

  it("opens the Activity tab from the sidebar digest", async () => {
    renderProjectDetail();
    await userEvent.click(screen.getByRole("button", { name: "View all activity" }));
    expect(mocks.replace).toHaveBeenCalledWith("/test-workspace/projects/project-1?tab=activity");
  });

  it("opens the Issues tab filtered to a breakdown row", async () => {
    const store = getIssueSurfaceViewStore("project:project-1");
    store.setState({ statusFilters: ["todo"] });
    useIssuesScopeStore.getState().setScope("project:project-1", "members");
    renderProjectDetail();
    await userEvent.click(screen.getByRole("button", { name: "Filter by Design" }));
    expect(store.getState().labelFilters).toEqual(["label-1"]);
    expect(store.getState().statusFilters).toEqual([]);
    expect(useIssuesScopeStore.getState().scopes["project:project-1"]).toBe("all");
    expect(mocks.replace).toHaveBeenCalledWith("/test-workspace/projects/project-1?tab=issues");
    store.getState().clearFilters();
  });

  it("shows the issue views for ?tab=issues and remembers that choice", () => {
    renderProjectDetail("tab=issues");
    expect(screen.getByTestId("issue-view-header")).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: /Issues/ })).toHaveAttribute("aria-selected", "true");
    expect(useProjectTabStore.getState().lastTab["project-1"]).toBe("issues");
  });

  it("switches tabs through the URL", async () => {
    renderProjectDetail();
    await userEvent.click(screen.getByRole("tab", { name: /Issues/ }));
    expect(mocks.replace).toHaveBeenCalledWith("/test-workspace/projects/project-1?tab=issues");
    expect(useProjectTabStore.getState().lastTab["project-1"]).toBe("issues");
  });

  it("reopens on the remembered tab unless the project has no issues", () => {
    useProjectTabStore.setState({ lastTab: { "project-1": "issues" } });
    const { unmount } = renderProjectDetail();
    expect(screen.getByTestId("issue-view-header")).toBeInTheDocument();
    unmount();

    qc.setQueryData(projectKeys.detail("workspace-1", PROJECT.id), { ...PROJECT, issue_count: 0, done_count: 0 });
    renderProjectDetail();
    expect(screen.queryByTestId("issue-view-header")).not.toBeInTheDocument();
  });
});

describe("ProjectDetail rename", () => {
  it("opens the rename dialog with the current name from the header menu", async () => {
    const user = userEvent.setup();
    renderProjectDetail();

    await user.click(screen.getByRole("button", { name: "Rename project" }));

    expect(useModalStore.getState()).toMatchObject({
      modal: "project-rename",
      data: { projectId: "project-1", title: "Launch Plan" },
    });
  });
});

describe("ProjectDetail lead permissions", () => {
  it("does not let ordinary members change the lead of a private project", async () => {
    mocks.role = "member";
    qc.setQueryData(projectKeys.detail("workspace-1", PROJECT.id), { ...PROJECT, visibility: "private" });
    renderProjectDetail();
    expect(screen.getByRole("button", { name: "No lead" })).toBeDisabled();
    await userEvent.click(screen.getByRole("button", { name: "No lead" }));
    expect(screen.queryByPlaceholderText("Assign lead...")).not.toBeInTheDocument();
  });

  it("keeps the lead of a workspace project editable for ordinary members", async () => {
    mocks.role = "member";
    renderProjectDetail();
    expect(screen.getByRole("button", { name: "No lead" })).toBeEnabled();
    await userEvent.click(screen.getByRole("button", { name: "No lead" }));
    expect(screen.getByPlaceholderText("Assign lead...")).toBeInTheDocument();
  });
});

describe("ProjectDetail for a guest", () => {
  beforeEach(() => {
    mocks.role = "guest";
    qc.setQueryData(projectKeys.detail("workspace-1", PROJECT.id), { ...PROJECT, description: "Ship the launch." });
  });

  it("keeps sharing, pinning and following, and drops every change to the project", async () => {
    renderProjectDetail();
    expect(screen.getByRole("button", { name: "Copy link" })).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Subscribe to all issues" })).toBeInTheDocument();
    for (const name of ["Rename project", "Archive project", "Delete project"]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    }
  });

  it("shows the overview as read-only, without the agent context", () => {
    renderProjectDetail();
    expect(screen.getByRole("heading", { name: "Launch Plan" })).toBeInTheDocument();
    expect(screen.getByText("Ship the launch.")).toBeInTheDocument();
    expect(screen.queryByText("Agent context")).not.toBeInTheDocument();
    expect(screen.getByTestId("project-resources")).toHaveAttribute("data-read-only", "true");
  });

  it("locks the properties and passes read-only to the insights", () => {
    renderProjectDetail();
    const properties = screen.getByTestId("project-properties");
    expect(within(properties).getByRole("button", { name: "No lead" }).closest("[inert]")).not.toBeNull();
    expect(within(properties).getByText("Guests can view this project, but cannot change its properties.")).toBeInTheDocument();
    expect(screen.getByTestId("project-insights")).toHaveAttribute("data-read-only", "true");
  });

  it("does not ask for the members of a private project it was shared", async () => {
    qc.setQueryData(projectKeys.detail("workspace-1", PROJECT.id), { ...PROJECT, visibility: "private" });
    renderProjectDetail();
    expect(await screen.findByRole("button", { name: "Subscribe to all issues" })).toBeInTheDocument();
    expect(mocks.getProjectMembers).not.toHaveBeenCalled();
  });

  it("says an archived project is archived without offering Restore", () => {
    qc.setQueryData(projectKeys.detail("workspace-1", PROJECT.id), { ...PROJECT, archived_at: "2026-11-01T00:00:00Z" });
    renderProjectDetail();
    expect(screen.getByRole("status")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Restore" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Restore project" })).not.toBeInTheDocument();
  });

  it("does not offer deletion to a creator who is now a guest", () => {
    qc.setQueryData(projectKeys.detail("workspace-1", PROJECT.id), {
      ...PROJECT,
      creator_type: "member",
      creator_id: "user-1",
    });
    renderProjectDetail();
    expect(screen.queryByRole("button", { name: "Delete project" })).not.toBeInTheDocument();
  });

  it("leaves a member's page editable", () => {
    mocks.role = "member";
    renderProjectDetail();
    expect(screen.getByRole("button", { name: "Rename project" })).toBeInTheDocument();
    expect(screen.getByText("Agent context")).toBeInTheDocument();
    expect(screen.getByTestId("project-resources")).toHaveAttribute("data-read-only", "false");
    expect(within(screen.getByTestId("project-properties")).getByRole("button", { name: "No lead" }).closest("[inert]")).toBeNull();
  });
});

describe("ProjectDetail sharing", () => {
  it("copies the platform shareable URL instead of the renderer URL", async () => {
    const user = userEvent.setup();
    renderProjectDetail();

    await user.click(screen.getByRole("button", { name: "Copy link" }));

    expect(mocks.getShareableUrl).toHaveBeenCalledWith(
      "/test-workspace/projects/project-1",
    );
    expect(mocks.copyText).toHaveBeenCalledWith(
      "https://app.example/test-workspace/projects/project-1",
    );
  });
});

describe("ProjectDetail subscription", () => {
  it("subscribes to every issue in the project and can unsubscribe again", async () => {
    const user = userEvent.setup();
    renderProjectDetail();

    await user.click(await screen.findByRole("button", { name: "Subscribe to all issues" }));
    expect(mocks.setProjectSubscription).toHaveBeenCalledWith("project-1", true);
    expect(mocks.toastSuccess).toHaveBeenCalled();

    await user.click(await screen.findByRole("button", { name: "Unsubscribe from project" }));
    expect(mocks.setProjectSubscription).toHaveBeenLastCalledWith("project-1", false);
    expect(await screen.findByRole("button", { name: "Subscribe to all issues" })).toBeInTheDocument();
  });

  it("keeps the current state and reports a failed update", async () => {
    mocks.setProjectSubscription.mockRejectedValueOnce(new Error("boom"));
    const user = userEvent.setup();
    renderProjectDetail();

    await user.click(await screen.findByRole("button", { name: "Subscribe to all issues" }));
    await waitFor(() => expect(mocks.toastError).toHaveBeenCalled());
    expect(screen.getByRole("button", { name: "Subscribe to all issues" })).toBeInTheDocument();
  });
});

describe("ProjectDetail copy link with title", () => {
  it("copies the project title as a Markdown link and an HTML anchor", async () => {
    const user = userEvent.setup();
    renderProjectDetail();

    await user.click(screen.getByRole("button", { name: "Copy link with title" }));

    expect(mocks.copyTextAndHtml).toHaveBeenCalledWith(
      "[Launch Plan](https://app.example/test-workspace/projects/project-1)",
      '<a href="https://app.example/test-workspace/projects/project-1">Launch Plan</a>',
    );
    expect(mocks.toastSuccess).toHaveBeenCalledWith("Link copied with title");
  });

  it("reports a failed copy without a success toast", async () => {
    mocks.copyTextAndHtml.mockResolvedValueOnce(false);
    const user = userEvent.setup();
    renderProjectDetail();

    await user.click(screen.getByRole("button", { name: "Copy link with title" }));

    expect(mocks.toastError).toHaveBeenCalledWith("Failed to copy link");
    expect(mocks.toastSuccess).not.toHaveBeenCalled();
  });
});

describe("ProjectDetail project deletion", () => {
  it("allows cancelling an offline detail deletion without executing it on reconnect", async () => {
    onlineManager.setOnline(false);
    renderProjectDetail();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Delete project" }));
    await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(mocks.toastError).toHaveBeenCalledWith(
      "You're offline. Reconnect and try deleting again.",
    ));
    expect(mocks.deleteProject).not.toHaveBeenCalled();
    expect(qc.getQueryData(projectKeys.detail("workspace-1", PROJECT.id))).toEqual(PROJECT);
    const dialog = screen.getByRole("alertdialog");
    expect(within(dialog).getByRole("button", { name: "Delete" })).toBeEnabled();
    const cancel = within(dialog).getByRole("button", { name: "Cancel" });
    expect(cancel).toBeEnabled();
    await user.click(cancel);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    await act(async () => {
      onlineManager.setOnline(true);
      await qc.resumePausedMutations();
    });
    expect(mocks.deleteProject).not.toHaveBeenCalled();
    expect(mocks.push).not.toHaveBeenCalled();
  });

  it("requires confirmation and navigates only after deletion succeeds", async () => {
    const user = userEvent.setup();
    renderProjectDetail();

    await user.click(screen.getByRole("button", { name: "Delete project" }));

    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    expect(mocks.deleteProject).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Delete" }));

    await waitFor(() => expect(mocks.deleteProject).toHaveBeenCalledWith(PROJECT.id));
    expect(mocks.push).not.toHaveBeenCalled();
    const dialog = screen.getByRole("alertdialog");
    const confirm = within(dialog).getByRole("button", { name: "Delete" });
    expect(confirm).toBeDisabled();
    expect(confirm).toHaveAttribute("aria-busy", "true");
    expect(confirm.querySelector("svg")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "Cancel" })).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(screen.getByRole("alertdialog")).toBeInTheDocument();
    await user.click(confirm);
    expect(mocks.deleteProject).toHaveBeenCalledOnce();
    await act(async () => pending.resolve());
    await waitFor(() => expect(mocks.toastSuccess).toHaveBeenCalledWith("Project deleted"));
    expect(mocks.push).toHaveBeenCalledWith("/test-workspace/projects");
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(mocks.getProject).not.toHaveBeenCalled();
    expect(screen.queryByText("Project not found")).not.toBeInTheDocument();
  });

  it.each([
    ["403", new Error("Forbidden")],
    ["network error", new TypeError("Failed to fetch")],
  ])("reports %s, retains the detail and allows retry", async (_label, error) => {
    renderProjectDetail();
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: "Delete project" }));
    await user.click(within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(mocks.deleteProject).toHaveBeenCalledOnce());
    await act(async () => pending.reject(error));
    await waitFor(() => expect(mocks.toastError).toHaveBeenCalledWith(error.message));
    const confirm = within(screen.getByRole("alertdialog")).getByRole("button", { name: "Delete" });
    expect(confirm).toBeEnabled();
    expect(qc.getQueryData(projectKeys.detail("workspace-1", PROJECT.id))).toEqual(PROJECT);
    expect(mocks.push).not.toHaveBeenCalled();
    expect(mocks.getProject).not.toHaveBeenCalled();
    mocks.deleteProject.mockResolvedValue(undefined);
    await user.click(confirm);
    await waitFor(() => expect(mocks.push).toHaveBeenCalledWith("/test-workspace/projects"));
    expect(mocks.deleteProject).toHaveBeenCalledTimes(2);
  });

  it("offers deletion to the project creator", () => {
    mocks.role = "member";
    PROJECT.creator_type = "member";
    PROJECT.creator_id = "user-1";
    try {
      renderProjectDetail();
      expect(screen.getByRole("button", { name: "Delete project" })).toBeInTheDocument();
    } finally {
      delete PROJECT.creator_type;
      delete PROJECT.creator_id;
    }
  });

  it("does not offer project deletion to regular members with an unknown creator", () => {
    mocks.role = "member";

    renderProjectDetail();

    expect(
      screen.queryByRole("button", { name: "Delete project" }),
    ).not.toBeInTheDocument();
  });
});
