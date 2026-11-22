import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { useModalStore } from "@multica/core/modals";
import type { Project, ProjectProgressDetail } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { ProjectBreakdown, ProjectProgressSection } from "./project-progress-section";
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
  by_label: [
    { label_id: "label-1", scope: 4, completed: 1, started: 1 },
    { label_id: null, scope: 7, completed: 4, started: 2 },
  ],
  history: [
    { date: "2026-10-12", scope: 6, started: 1, completed: 0 },
    { date: "2026-11-09", scope: 11, started: 3, completed: 5 },
  ],
  forecast: { completion_date: "2026-12-14", weekly_velocity: 1.25 },
};

function renderWithQueries(ui: React.ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return renderWithI18n(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
}

function renderSection(project: Project) {
  return renderWithQueries(<ProjectProgressSection project={project} />);
}

beforeEach(() => {
  getProjectProgressMock.mockReset();
  setApiInstance({
    getProjectProgress: getProjectProgressMock,
    listLabels: vi.fn().mockResolvedValue({
      labels: [{ id: "label-1", workspace_id: "workspace-1", name: "Design", color: "#3b82f6" }],
    }),
  } as unknown as ApiClient);
});

describe("ProjectProgressSection", () => {
  it("shows completion against scope, keeps cancelled issues out, and forecasts against the due date", async () => {
    getProjectProgressMock.mockResolvedValue(detail);
    renderSection(baseProject);

    expect(screen.getByText("45%")).toBeInTheDocument();
    expect(screen.getByText("5 of 11 issues done")).toBeInTheDocument();
    expect(screen.getByText("1 cancelled issue isn't counted")).toBeInTheDocument();

    expect(await screen.findByText(/done by Dec 14, 4 days before the due date/)).toBeInTheDocument();
    expect(getProjectProgressMock).toHaveBeenCalledWith("project-1");
  });

  it("keeps the section on an empty project with ways to add issues", async () => {
    getProjectProgressMock.mockResolvedValue({ ...detail, progress: { ...detail.progress, scope: 0, completed: 0, started: 0, unstarted: 0, cancelled: 0, percent: 0 }, by_assignee: [], history: [] });
    const open = vi.spyOn(useModalStore.getState(), "open");
    renderSection({ ...baseProject, issue_count: 0, done_count: 0, progress: { ...detail.progress, scope: 0, completed: 0, started: 0, unstarted: 0, cancelled: 0, percent: 0 } });

    expect(screen.getByText("0%")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "New issue" }));
    expect(open).toHaveBeenCalledWith("create-issue", { project_id: "project-1" });
    await userEvent.click(screen.getByRole("button", { name: "Add existing issue" }));
    expect(open).toHaveBeenCalledWith("project-add-issue", { projectId: "project-1" });
  });

  it("offers no way to add issues to an archived empty project", () => {
    getProjectProgressMock.mockReturnValue(new Promise(() => {}));
    const empty = { ...detail.progress, scope: 0, completed: 0, started: 0, unstarted: 0, cancelled: 0, percent: 0 };
    renderSection({ ...baseProject, issue_count: 0, done_count: 0, progress: empty, archived_at: "2026-11-01T00:00:00Z" });

    expect(screen.getByText("0%")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "New issue" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add existing issue" })).not.toBeInTheDocument();
  });
});

describe("ProjectBreakdown", () => {
  it("breaks progress down by assignee and filters to a row", async () => {
    getProjectProgressMock.mockResolvedValue(detail);
    const onFilter = vi.fn();
    renderWithQueries(<ProjectBreakdown project={baseProject} onFilter={onFilter} />);

    await userEvent.click(await screen.findByRole("button", { name: /Lambda/ }));
    expect(onFilter).toHaveBeenCalledWith({ kind: "assignee", type: "agent", id: "agent-1" });
    expect(screen.getByText("Agent")).toBeInTheDocument();
    expect(screen.getByText("3/5")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /Unassigned/ }));
    expect(onFilter).toHaveBeenLastCalledWith({ kind: "unassigned" });
  });

  it("switches to labels, where only a named label filters", async () => {
    getProjectProgressMock.mockResolvedValue(detail);
    const onFilter = vi.fn();
    renderWithQueries(<ProjectBreakdown project={baseProject} onFilter={onFilter} />);

    await userEvent.click(await screen.findByRole("tab", { name: "Label" }));
    await userEvent.click(await screen.findByRole("button", { name: /Design/ }));
    expect(onFilter).toHaveBeenCalledWith({ kind: "label", id: "label-1" });
    expect(screen.getByText("No label")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /No label/ })).not.toBeInTheDocument();
    expect(screen.getByText("4/7")).toBeInTheDocument();
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
