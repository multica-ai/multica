import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DragEndEvent } from "@dnd-kit/core";
import type { IssueViewPreference } from "@multica/core/api/schemas";
import { issueViewPreferenceOptions } from "@multica/core/issue-views/preferences";
import { buildIssueStatusCatalog } from "@multica/core/issue-statuses/queries";
import { SIDEBAR_PROJECTS_SCOPE } from "@multica/core/issues/sidebar-project-prefs";
import {
  DEFAULT_SIDEBAR_ISSUE_SETTINGS,
  DEFAULT_SIDEBAR_PROJECT_SETTINGS,
  useSidebarTreeStore,
} from "@multica/core/issues/stores/sidebar-tree-store";
import type { Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
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
vi.mock("../../navigation", () => ({
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

const savedPrefs = (prefs: Record<string, unknown>): IssueViewPreference => ({
  scope_type: "sidebar_projects",
  scope_id: "u1",
  updated_at: "2026-10-31T00:00:00Z",
  prefs: { hidden: [], order: [], ...prefs },
});

function renderSection(projects = PROJECTS) {
  const qc = new QueryClient({ defaultOptions: { queries: { enabled: false } } });
  qc.setQueryData(
    issueViewPreferenceOptions("ws-1", SIDEBAR_PROJECTS_SCOPE).queryKey,
    savedPrefs({
      projects: { ...DEFAULT_SIDEBAR_PROJECT_SETTINGS, ordering: "name" },
      issues: DEFAULT_SIDEBAR_ISSUE_SETTINGS,
      order: ["p9"],
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
        locatedProjectId={null}
      />
    </QueryClientProvider>,
  );
}

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

  it("expands a project in place, keeping its count", () => {
    renderSection();
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
    expect(prefs.order).toEqual(["p3", "p1", "p2", "p9"]);
    expect(prefs.projects.ordering).toBe("manual");
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

  it("collapses from the section label", () => {
    renderSection();
    fireEvent.click(screen.getByRole("button", { name: "Projects" }));
    expect(useSidebarTreeStore.getState().projectsCollapsed).toBe(true);
  });
});
