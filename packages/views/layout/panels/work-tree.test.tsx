import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DragEndEvent } from "@dnd-kit/core";
import type { IssueViewPreference } from "@multica/core/api/schemas";
import { issueViewPreferenceOptions } from "@multica/core/issue-views/preferences";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses/queries";
import { issueTableFacetsOptions } from "@multica/core/issues/queries";
import {
  NO_PROJECT_FACET_KEY,
  NO_PROJECT_ORDER_ID,
  NO_PROJECT_SIDEBAR_SOURCE,
  noProjectCountRequest,
} from "@multica/core/issues/sidebar-list";
import { SIDEBAR_PROJECTS_SCOPE } from "@multica/core/issues/sidebar-project-prefs";
import {
  DEFAULT_SIDEBAR_ISSUE_SETTINGS,
  DEFAULT_SIDEBAR_PROJECT_SETTINGS,
  useSidebarTreeStore,
  type SidebarProjectSettings,
} from "@multica/core/issues/stores/sidebar-tree-store";
import type { SidebarFolder } from "@multica/core/projects/folder-tree";
import type { Project } from "@multica/core/types";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import { renderWithI18n } from "../../test/i18n";
import { registerProjectFoldersExtension, type ProjectFolders } from "../../extensions";
import { ProjectsSection } from "./work-tree";

const apiMock = vi.hoisted(() => ({ putIssueViewPreference: vi.fn() }));
vi.mock("@multica/core/api", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@multica/core/api")>()),
  api: apiMock,
}));

const openCreateIssue = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/issues/stores/create-mode-store", () => ({
  openCreateIssueWithPreference: openCreateIssue,
}));

const dnd = vi.hoisted(() => ({ onDragEnd: undefined as ((event: DragEndEvent) => void) | undefined }));

vi.mock("@dnd-kit/core", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@dnd-kit/core")>();
  return {
    ...actual,
    DndContext: (props: React.ComponentProps<typeof actual.DndContext>) => {
      dnd.onDragEnd = props.onDragEnd;
      return <actual.DndContext {...props} />;
    },
  };
});
vi.mock("@multica/core/issue-statuses/hooks", () => ({
  useIssueStatuses: () => buildIssueStatusCatalog([]),
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "u1" } };
  return {
    useAuthStore: Object.assign(
      (selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state),
      { getState: () => state },
    ),
  };
});
vi.mock("../../navigation", () => ({
  useWarmRoute: () => undefined,
  useIntentNavigate: () => vi.fn(),
  useNavigation: () => ({ getShareableUrl: (path: string) => `https://app.example${path}` }),
  AppLink: ({
    children,
    href,
    newTabTitle: _newTabTitle,
    ...props
  }: { children: ReactNode; href: string; newTabTitle?: string } & React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("../../issues/components/status-icon", () => ({ StatusIcon: () => <span /> }));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});

const project = (id: string, title: string, icon: string | null = null): Project =>
  ({ id, title, icon, status: "in_progress", issue_count: 4, done_count: 1 }) as Project;
const PROJECTS = [project("p1", "Desktop"), project("p2", "Mobile"), project("p3", "Web")];

beforeEach(() => {
  useSidebarTreeStore.setState({ expanded: [], projectsCollapsed: false });
  apiMock.putIssueViewPreference
    .mockReset()
    .mockImplementation(async ({ prefs }: { prefs: Record<string, unknown> }) => savedPrefs(prefs));
});

afterEach(() => registerProjectFoldersExtension(null));

const GROWTH: SidebarFolder = { id: "f1", name: "Growth" };
const moveProject = vi.fn();

/** Registers folders the way packages/ee provides them. */
function provideFolders(folders: SidebarFolder[], folderOf: Record<string, string> = {}, canFile = true) {
  moveProject.mockReset();
  const value: ProjectFolders = {
    folders,
    folderOf: new Map(Object.entries(folderOf)),
    canFile,
    moveProject,
    labels: { group: "Folder", noFolder: "No folder", empty: "No projects", newProjectIn: (name) => `New project in ${name}` },
  };
  registerProjectFoldersExtension({
    useProjectFolders: () => value,
    AddMenuItems: () => <DropdownMenuItem>New folder</DropdownMenuItem>,
    FolderMenu: () => null,
    ProjectMenuItems: () => null,
    CreateProjectField: () => null,
    Host: () => null,
  });
}

const savedPrefs = (prefs: Record<string, unknown>): IssueViewPreference => ({
  scope_type: "sidebar_projects",
  scope_id: "u1",
  updated_at: "2026-10-31T00:00:00Z",
  prefs: { hidden: [], order: [], ...prefs },
});

function renderSection(
  projects = PROJECTS,
  projectSettings: Partial<SidebarProjectSettings> = {},
  order: string[] = ["p9"],
) {
  const qc = new QueryClient({ defaultOptions: { queries: { enabled: false, retry: false } } });
  qc.setQueryData(issueTableFacetsOptions("ws-1", noProjectCountRequest([])).queryKey, {
    query_fingerprint: "",
    total: 0,
    facets: [{ kind: "project", values: [{ key: "p1", count: 3 }, { key: NO_PROJECT_FACET_KEY, count: 7 }] }],
  });
  qc.setQueryData(
    issueViewPreferenceOptions("ws-1", SIDEBAR_PROJECTS_SCOPE).queryKey,
    savedPrefs({
      projects: { ...DEFAULT_SIDEBAR_PROJECT_SETTINGS, ordering: "name", ...projectSettings },
      issues: DEFAULT_SIDEBAR_ISSUE_SETTINGS,
      order,
    }),
  );
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <ProjectsSection
        wsId="ws-1"
        userId="u1"
        pathname="/acme/issues"
        projects={projects}
        totalProjects={4}
        plainRowsLit
        activeIssueId={null}
        litSource={null}
        locatedSource={null}
      />
    </QueryClientProvider>,
  );
}

const rowNames = () =>
  screen
    .getAllByRole("button", { name: /^(No project|Issues in .+)$/ })
    .map((button) => button.getAttribute("aria-label")?.replace(/^Issues in /, "") ?? button.textContent);

describe("ProjectsSection", () => {
  it("shows the project's chosen icon, keeping the folder when it has none", () => {
    renderSection([project("p1", "Desktop", "🚀"), project("p2", "Mobile")]);
    const iconSlot = (name: string) =>
      screen.getByRole("button", { name: `Issues in ${name}` }).previousElementSibling;
    expect(iconSlot("Desktop")).toHaveTextContent("🚀");
    expect(iconSlot("Desktop")?.querySelector("svg")).toBeNull();
    expect(iconSlot("Mobile")).toHaveTextContent("");
    expect(iconSlot("Mobile")?.querySelector("svg")).not.toBeNull();
  });

  it("shows the folder for every project when minimal icons are on", () => {
    renderSection([project("p1", "Desktop", "🚀")], { minimalIcons: true });
    const iconSlot = screen.getByRole("button", { name: "Issues in Desktop" }).previousElementSibling;
    expect(iconSlot).not.toHaveTextContent("🚀");
    expect(iconSlot?.querySelector("svg")).not.toBeNull();
  });

  it("marks a private project with a lock inside its link", () => {
    renderSection([{ ...project("p1", "Desktop"), visibility: "private" }, { ...project("p2", "Mobile"), visibility: "workspace" }]);
    const lock = screen.getByRole("img", { name: "Visible to project members only" });
    expect(lock.closest("a")).toHaveTextContent("Desktop");
    expect(screen.getAllByRole("img", { name: "Visible to project members only" })).toHaveLength(1);
  });

  it("expands a project in place, keeping its count", () => {
    renderSection(PROJECTS, { showCount: true });
    expect(screen.getAllByText("3")).toHaveLength(3);

    fireEvent.click(screen.getByRole("button", { name: "Issues in Mobile" }));
    expect(useSidebarTreeStore.getState().expanded).toEqual(["project:p2"]);
    expect(screen.getByRole("button", { name: "Issues in Mobile" })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getAllByText("3")).toHaveLength(3);
  });

  it("keeps display settings on the section header only", () => {
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "Issues in Mobile" }));
    expect(screen.getAllByRole("button", { name: "Project display" })).toHaveLength(1);
  });

  it("saves a manual order for every device, keeping hidden projects after the shown ones", async () => {
    renderSection();
    act(() => dnd.onDragEnd?.({ active: { id: "p3" }, over: { id: "p1" } } as DragEndEvent));
    await waitFor(() => expect(apiMock.putIssueViewPreference).toHaveBeenCalledTimes(1));
    const { prefs } = apiMock.putIssueViewPreference.mock.calls[0]![0];
    expect(prefs.order).toEqual([NO_PROJECT_ORDER_ID, "p3", "p1", "p2", "p9"]);
    expect(prefs.projects.ordering).toBe("manual");
  });

  it("files projects under their folders, which only expand", () => {
    provideFolders([GROWTH], { p2: "f1" });
    renderSection([project("p1", "Desktop"), project("p2", "Mobile")], {}, []);
    expect(screen.queryByRole("link", { name: "Mobile" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "Growth" }));
    expect(useSidebarTreeStore.getState().expanded).toEqual(["folder:f1"]);
    const mobile = screen.getByRole("link", { name: "Mobile" });
    expect(mobile.closest("ul")).toHaveAttribute("id", screen.getByRole("button", { name: "Growth" }).getAttribute("aria-controls"));
    expect(rowNames()).toEqual(["No project", "Mobile", "Desktop"]);
  });

  it("keeps an empty folder so projects can be filed in it", () => {
    provideFolders([GROWTH]);
    renderSection(PROJECTS, {}, []);
    fireEvent.click(screen.getByRole("button", { name: "Growth" }));
    expect(screen.getByText("No projects")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New project in Growth" })).toBeInTheDocument();
  });

  it("hides a folder whose projects the settings hide", () => {
    provideFolders([GROWTH], { p2: "f1" });
    renderSection([project("p1", "Desktop")], {}, []);
    expect(screen.queryByRole("button", { name: "Growth" })).toBeNull();
  });

  it("files a project dropped on a folder", () => {
    provideFolders([GROWTH]);
    renderSection(PROJECTS, {}, []);
    act(() => dnd.onDragEnd?.({ active: { id: "p1" }, over: { id: "folder-drop:f1" } } as DragEndEvent));
    expect(moveProject).toHaveBeenCalledWith("p1", "f1");
    expect(apiMock.putIssueViewPreference).not.toHaveBeenCalled();
  });

  it("moves a project dragged out of its folder to the top level, where it was dropped", async () => {
    useSidebarTreeStore.setState({ expanded: ["folder:f1"] });
    provideFolders([GROWTH], { p2: "f1" });
    renderSection(PROJECTS, {}, []);
    act(() => dnd.onDragEnd?.({ active: { id: "p2" }, over: { id: "p3" } } as DragEndEvent));
    expect(moveProject).toHaveBeenCalledWith("p2", null);
    await waitFor(() => expect(apiMock.putIssueViewPreference).toHaveBeenCalledTimes(1));
    const { prefs } = apiMock.putIssueViewPreference.mock.calls[0]![0];
    expect(prefs.order).toEqual([NO_PROJECT_ORDER_ID, "f1", "p1", "p2", "p3"]);
  });

  it("files nothing while filing is unavailable, but still lets projects out", () => {
    useSidebarTreeStore.setState({ expanded: ["folder:f1"] });
    provideFolders([GROWTH], { p2: "f1" }, false);
    renderSection(PROJECTS, {}, []);
    expect(screen.queryByRole("button", { name: "New project in Growth" })).toBeNull();
    act(() => dnd.onDragEnd?.({ active: { id: "p1" }, over: { id: "folder-drop:f1" } } as DragEndEvent));
    act(() => dnd.onDragEnd?.({ active: { id: "p3" }, over: { id: "p2" } } as DragEndEvent));
    expect(moveProject).not.toHaveBeenCalled();
    act(() => dnd.onDragEnd?.({ active: { id: "p2" }, over: { id: "p3" } } as DragEndEvent));
    expect(moveProject).toHaveBeenCalledWith("p2", null);
  });

  it("adds the extension's items to the section's add menu", async () => {
    provideFolders([]);
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "New project" }));
    const menu = await screen.findByRole("menu");
    const items = Array.from(menu.querySelectorAll('[role="menuitem"]'), (el) => el.textContent?.trim());
    expect(items).toEqual(["New project", "New folder"]);
  });

  it("opens a new project straight from the add button without folders", () => {
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "New project" }));
    expect(screen.queryByRole("menu")).toBeNull();
  });

  // dnd-kit stops the release's click at the document without cancelling
  // it. Where React listens below the document (Desktop), it never sees that
  // click, so the row must not wait for it before opening on the next one.
  it("does not open the project released at the end of a drag, and opens it on the next click", async () => {
    renderSection();
    const row = screen.getByRole("link", { name: "Mobile" });
    const pointer = { isPrimary: true, button: 0, clientX: 30, clientY: 100 };
    fireEvent.pointerDown(row, pointer);
    fireEvent.pointerMove(document, { ...pointer, clientY: 110 });
    fireEvent.pointerUp(document, { ...pointer, clientY: 110 });

    expect(fireEvent.click(row)).toBe(false);

    await new Promise((resolve) => setTimeout(resolve, 60));
    expect(fireEvent.click(row)).toBe(true);
  });

  it("creates an issue in a project from its row", () => {
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "New issue in Mobile" }));
    expect(openCreateIssue).toHaveBeenCalledWith({ project_id: "p2" });
  });

  it("opens the project's menu on right-click, led by a new issue in it", async () => {
    renderSection();
    fireEvent.contextMenu(screen.getByText("Mobile"), { clientX: 20, clientY: 40 });

    const menu = await screen.findByRole("menu");
    const items = Array.from(menu.querySelectorAll('[role="menuitem"]'), (el) => el.textContent?.trim());
    expect(items).toEqual([
      "New issue in this project",
      "Rename",
      "Open in new tab",
      "Pin to sidebar",
      "Copy link",
      "Copy link with title",
      "Archive",
    ]);

    fireEvent.click(screen.getByRole("menuitem", { name: "New issue in this project" }));
    expect(openCreateIssue).toHaveBeenCalledWith({ project_id: "p2" });
  });

  it("lists issues outside every project under No project, which only expands", () => {
    renderSection();
    expect(screen.queryByRole("link", { name: "No project" })).toBeNull();

    fireEvent.click(screen.getByRole("button", { name: "No project" }));
    expect(useSidebarTreeStore.getState().expanded).toEqual([NO_PROJECT_SIDEBAR_SOURCE]);
    expect(screen.getByRole("button", { name: "No project" })).toHaveAttribute("aria-expanded", "true");
    expect(document.querySelector(`[data-sidebar-list="${NO_PROJECT_SIDEBAR_SOURCE}"]`)).not.toBeNull();
  });

  it("counts the open issues outside every project", () => {
    renderSection(PROJECTS, { showCount: true });
    expect(screen.getByRole("button", { name: "No project" }).parentElement).toHaveTextContent("7");
  });

  it("shows no counts by default", () => {
    renderSection();
    expect(screen.queryByText("3")).toBeNull();
    expect(screen.getByRole("button", { name: "No project" }).parentElement).not.toHaveTextContent("7");
  });

  it("creates an issue without a project from the No project row", () => {
    renderSection();
    openCreateIssue.mockClear();
    fireEvent.click(screen.getByRole("button", { name: "New issue without a project" }));
    expect(openCreateIssue).toHaveBeenCalledWith({ project_id: null });
  });

  it("lists No project first, and saves where it is dragged", async () => {
    renderSection();
    expect(rowNames()).toEqual(["No project", "Desktop", "Mobile", "Web"]);

    act(() => dnd.onDragEnd?.({ active: { id: NO_PROJECT_ORDER_ID }, over: { id: "p2" } } as DragEndEvent));
    await waitFor(() => expect(apiMock.putIssueViewPreference).toHaveBeenCalledTimes(1));
    const { prefs } = apiMock.putIssueViewPreference.mock.calls[0]![0];
    expect(prefs.order).toEqual(["p1", "p2", NO_PROJECT_ORDER_ID, "p3", "p9"]);
    expect(prefs.projects.ordering).toBe("manual");
  });

  it("keeps No project where it was dragged", () => {
    renderSection(PROJECTS, { ordering: "manual" }, ["p1", NO_PROJECT_ORDER_ID, "p2", "p3"]);
    expect(rowNames()).toEqual(["Desktop", "No project", "Mobile", "Web"]);
  });

  it("leaves out No project when its setting is off", () => {
    renderSection(PROJECTS, { showNoProject: false });
    expect(screen.queryByRole("button", { name: "No project" })).toBeNull();
  });

  it("collapses from the section label", () => {
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "Projects" }));
    expect(useSidebarTreeStore.getState().projectsCollapsed).toBe(true);
  });
});
