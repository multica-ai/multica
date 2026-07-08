// CEREBRO-PATCH(sprint-header-breadcrumb): FIR-2817 test for the BreadcrumbHeader swap.
import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import { NavigationProvider } from "../../navigation";
import type { NavigationAdapter } from "../../navigation";

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectDetailOptions: (_wsId: string, projectId: string) => ({
    queryKey: ["project", projectId],
    queryFn: () => Promise.resolve({ id: projectId, title: "Tech Sprints" }),
  }),
}));

vi.mock("@multica/cerebro-sprints/core/queries", () => ({
  sprintOptions: (_wsId: string, sprintId: string) => ({
    queryKey: ["sprint", sprintId],
    queryFn: () =>
      Promise.resolve({
        id: sprintId,
        project_id: "p1",
        name: "Tech Sprint 3 2026",
        status: "planned",
        start_date: "2026-07-06",
        end_date: "2026-07-19",
      }),
  }),
}));

// The board itself (filters, drag-and-drop, columns) is exercised by its own
// tests — stub it here so this test stays scoped to the header.
vi.mock("./project-detail", () => ({
  ProjectIssuesSurface: () => <div data-testid="issues-surface" />,
}));

function makeAdapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    getShareableUrl: (p: string) => p,
  };
}

async function renderSprintBoard() {
  const { SprintBoard } = await import("./cerebro-sprint-board");
  const queryClient = new QueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <NavigationProvider value={makeAdapter()}>
        <SprintBoard sprintId="s1" />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

describe("SprintBoard header", () => {
  it("renders the project as a breadcrumb link back to the project page (matches the project page's BreadcrumbHeader)", async () => {
    await renderSprintBoard();

    const projectLink = await screen.findByRole("link", { name: "Tech Sprints" });
    expect(projectLink).toHaveAttribute("href", "/acme/projects/p1");
  });

  it("renders the sprint name, status, and date range in the header leaf", async () => {
    await renderSprintBoard();

    expect(await screen.findByText("Tech Sprint 3 2026")).toBeInTheDocument();
    expect(screen.getByText("Planned")).toBeInTheDocument();
    expect(screen.getByText(/2026-07-06.*2026-07-19/)).toBeInTheDocument();
  });

  it("still mounts the (stubbed) project issues surface below the header", async () => {
    await renderSprintBoard();

    expect(await screen.findByTestId("issues-surface")).toBeInTheDocument();
  });
});
