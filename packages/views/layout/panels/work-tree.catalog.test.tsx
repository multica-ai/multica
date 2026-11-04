import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { projectSidebarSource } from "@multica/core/issues/sidebar-list";
import { useSidebarKeptStore } from "@multica/core/issues/stores/sidebar-kept-store";
import { useSidebarTreeStore } from "@multica/core/issues/stores/sidebar-tree-store";
import type { Project } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { ProjectsSection } from "./work-tree";

vi.mock("@dnd-kit/core", () => ({
  DndContext: ({ children }: { children: ReactNode }) => <>{children}</>,
  PointerSensor: vi.fn(),
  closestCenter: vi.fn(),
  useSensor: vi.fn(),
  useSensors: vi.fn(),
}));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return { ...actual, useWorkspacePaths: () => actual.paths.workspace("acme") };
});

const adapter: NavigationAdapter = {
  push: vi.fn(),
  replace: vi.fn(),
  back: vi.fn(),
  pathname: "/acme/issues",
  searchParams: new URLSearchParams(),
  hash: "",
  getShareableUrl: (path) => path,
};

const TODO_STATUS = {
  id: "s-todo",
  workspace_id: "ws-1",
  key: "todo",
  name: "Todo",
  description: "",
  category: "unstarted",
  color: "",
  is_system: true,
  position: 0,
  archived_at: null,
  created_at: "",
  updated_at: "",
};

const PROJECT = { id: "p1", title: "Desktop", status: "in_progress", issue_count: 4, done_count: 1 } as Project;

let listIssues: ReturnType<typeof vi.fn>;
let listIssueStatuses: ReturnType<typeof vi.fn>;

function renderSection() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  renderWithI18n(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter}>
        <ProjectsSection
          wsId="ws-1"
          userId="u1"
          pathname="/acme/issues"
          projects={[PROJECT]}
          totalProjects={1}
          plainRowsLit
          activeIssueId={null}
        />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  listIssues = vi.fn(async () => ({ issues: [], total: 0 }));
  listIssueStatuses = vi.fn(async () => ({ statuses: [TODO_STATUS], categories: [], total: 1 }));
  setApiInstance({
    listIssues,
    listIssueStatuses,
    getIssueViewPreference: vi.fn(() => new Promise(() => {})),
  } as unknown as ApiClient);
  useSidebarKeptStore.setState({ held: {} });
  useSidebarTreeStore.setState({ projectsCollapsed: false, expanded: [projectSidebarSource("p1")] });
});

describe("ProjectsSection project row", () => {
  it("offers a retry when the status catalog fails, then lists its issues", async () => {
    listIssueStatuses.mockRejectedValueOnce(new Error("offline"));
    renderSection();

    const retry = await screen.findByRole("button", { name: "Retry" });
    expect(screen.getByText("Couldn't load issues")).toBeInTheDocument();
    expect(listIssues).not.toHaveBeenCalled();

    fireEvent.click(retry);

    await screen.findByText("No issues");
    expect(listIssues).toHaveBeenCalledWith(expect.objectContaining({ project_id: "p1" }));
  });
});
