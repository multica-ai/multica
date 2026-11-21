import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { useModalStore } from "@multica/core/modals";
import type { Project, ProjectProgressDetail } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { ProjectProgressSection } from "./project-progress-section";
import { getProjectProgress } from "./project-issue-metrics";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_type: string, id: string) => (id === "agent-1" ? "Lambda" : "Chen") }),
}));
vi.mock("../../common/actor-avatar", () => ({ ActorAvatar: () => <span data-testid="avatar" /> }));

const getProjectProgressMock = vi.fn();

const baseProject: Project = {
  id: "project-1",
  workspace_id: "workspace-1",
  title: "Mobile 2.0",
  description: null,
  icon: null,
  status: "in_progress",
  priority: "none",
  lead_type: null,
  lead_id: null,
  start_date: null,
  due_date: "2026-12-18",
  created_at: "2026-10-12T00:00:00Z",
  updated_at: "2026-10-12T00:00:00Z",
  issue_count: 12,
  done_count: 6,
  resource_count: 0,
  progress: { unit: "issues", scope: 11, completed: 5, started: 3, unstarted: 3, cancelled: 1, percent: 45 },
};

const detail: ProjectProgressDetail = {
  progress: baseProject.progress!,
  by_assignee: [
    { assignee_type: "agent", assignee_id: "agent-1", scope: 5, completed: 3, started: 2 },
    { assignee_type: null, assignee_id: null, scope: 1, completed: 0, started: 0 },
  ],
  history: [
    { date: "2026-10-12", scope: 6, started: 1, completed: 0 },
    { date: "2026-11-09", scope: 11, started: 3, completed: 5 },
  ],
  forecast: { completion_date: "2026-12-14", weekly_velocity: 1.25 },
};

function renderSection(project: Project) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(
    <QueryClientProvider client={queryClient}>
      <ProjectProgressSection project={project} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  getProjectProgressMock.mockReset();
  setApiInstance({ getProjectProgress: getProjectProgressMock } as unknown as ApiClient);
});

describe("ProjectProgressSection", () => {
  it("shows completion against scope, keeps cancelled issues out, and forecasts against the due date", async () => {
    getProjectProgressMock.mockResolvedValue(detail);
    renderSection(baseProject);

    expect(screen.getByText("45%")).toBeInTheDocument();
    expect(screen.getByText("5 of 11 issues done")).toBeInTheDocument();
    expect(screen.getByText("1 cancelled issue isn't counted")).toBeInTheDocument();

    expect(await screen.findByText(/done by Dec 14, 4 days before the due date/)).toBeInTheDocument();
    expect(screen.getByText("Lambda")).toBeInTheDocument();
    expect(screen.getByText("Agent")).toBeInTheDocument();
    expect(screen.getByText("Unassigned")).toBeInTheDocument();
    expect(screen.getByText("3/5")).toBeInTheDocument();
    expect(getProjectProgressMock).toHaveBeenCalledWith("project-1");
  });

  it("keeps the section on an empty project with ways to add issues", async () => {
    getProjectProgressMock.mockResolvedValue({ ...detail, progress: { ...detail.progress, scope: 0, completed: 0, started: 0, unstarted: 0, cancelled: 0, percent: 0 }, by_assignee: [], history: [] });
    const open = vi.spyOn(useModalStore.getState(), "open");
    renderSection({ ...baseProject, issue_count: 0, done_count: 0, progress: { ...detail.progress, scope: 0, completed: 0, started: 0, unstarted: 0, cancelled: 0, percent: 0 } });

    expect(screen.getByText("0%")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "New Issue" }));
    expect(open).toHaveBeenCalledWith("create-issue", { project_id: "project-1" });
    await userEvent.click(screen.getByRole("button", { name: "Add existing issue" }));
    expect(open).toHaveBeenCalledWith("project-add-issue", { projectId: "project-1" });
  });
});

describe("getProjectProgress", () => {
  it("falls back to issue_count / done_count from servers without progress", () => {
    expect(getProjectProgress({ issue_count: 4, done_count: 1 })).toEqual({
      unit: "issues", scope: 4, completed: 1, started: 0, unstarted: 3, cancelled: 0, percent: 25,
    });
    expect(getProjectProgress({ issue_count: 0, done_count: 0 }).percent).toBe(0);
  });
});
