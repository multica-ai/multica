import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import type { ListProjectsResponse, Project, ProjectStatus } from "@multica/core/types";
import { projectDetailOptions, projectKeys, useProjectViewStore } from "@multica/core/projects";
import { setCurrentWorkspace } from "@multica/core/platform";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";
import { ProjectsPage } from "./projects-page";

const context = vi.hoisted(() => ({ wsId: "ws-1" }));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => context.wsId }));
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "user-1" } };
  return { useAuthStore: Object.assign((selector: (s: typeof state) => unknown) => selector(state), {
    getState: () => state,
  }) };
});
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ projectDetail: (id: string) => `/acme/projects/${id}` }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: () => "Lead" }),
}));

const statuses: ProjectStatus[] = ["planned", "in_progress", "paused", "completed", "cancelled"];
const fixtures: Project[] = statuses.map((status, index) => ({
  id: status, workspace_id: "ws-1", title: `Project ${status}`, status,
  description: "Preserved history", icon: null, priority: index === 0 ? "high" : "low",
  lead_type: null, lead_id: null, start_date: null, due_date: null,
  created_at: "2026-06-01T00:00:00Z", updated_at: "2026-06-01T00:00:00Z",
  issue_count: 3, done_count: 1, resource_count: 0,
}));
let qc: QueryClient;
let server: Project[];
let update: ReturnType<typeof vi.fn>;
const adapter = {
  push: vi.fn(), replace: vi.fn(), back: vi.fn(), pathname: "/acme/projects",
  searchParams: new URLSearchParams(), hash: "", getShareableUrl: (p: string) => p,
};

function page() {
  return <QueryClientProvider client={qc}>
    <NavigationProvider value={adapter}><ProjectsPage /></NavigationProvider>
  </QueryClientProvider>;
}

async function scope(name: string) {
  await userEvent.click(within(screen.getByRole("heading", { level: 1 })).getByRole("button"));
  const item = await screen.findByRole("menuitemradio", { name: new RegExp("^" + name.replace(/ \d+$/, "")) });
  expect(item).toHaveTextContent(name.match(/\d+$/)![0]!);
  await userEvent.click(item);
}

async function chooseStatus(id: string, current: string, next: string) {
  const row = screen.getByText(`Project ${id}`).closest('[role="row"]')!;
  await userEvent.click(within(row as HTMLElement).getByRole("button", { name: current }));
  await userEvent.click(await screen.findByRole("menuitem", { name: next }));
}

beforeEach(async () => {
  localStorage.clear();
  context.wsId = "ws-1";
  server = fixtures.map((p) => ({ ...p }));
  update = vi.fn(async (id: string, data: Partial<Project>) => {
    server = server.map((p) => p.id === id ? { ...p, ...data } : p);
    return server.find((p) => p.id === id)!;
  });
  setApiInstance({
    listProjects: vi.fn(async () => ({ projects: server, total: server.length })),
    getProject: vi.fn(async (id: string) => server.find((p) => p.id === id)),
    updateProject: update,
    listMembers: vi.fn(async () => [{ user_id: "user-1", role: "admin", name: "Fixture Admin" }]),
    listPins: vi.fn(async () => []),
    listAgents: vi.fn(async () => []),
  } as unknown as ApiClient);
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  qc.setQueryData(projectKeys.list("ws-1"), { projects: server, total: server.length });
  qc.setQueryData(["workspaces", "ws-1", "members"], [{ user_id: "user-1", role: "admin", name: "Fixture Admin" }]);
  qc.setQueryData(["workspaces", "ws-1", "agents"], []);
  setCurrentWorkspace("acme", "ws-1");
  await Promise.resolve();
  useProjectViewStore.getState().clearFilters();
  useProjectViewStore.getState().setLifecycleView("active");
  useProjectViewStore.getState().setViewMode("compact");
});

afterEach(() => { cleanup(); qc.clear(); setCurrentWorkspace(null, null); vi.restoreAllMocks(); });

describe("project lifecycle browse integration", () => {
  it("defaults to the three active states, counts the full lifecycle groups and keeps cancelled discoverable", async () => {
    renderWithI18n(page());
    for (const status of statuses.slice(0, 3)) expect(screen.getByText(`Project ${status}`)).toBeVisible();
    expect(screen.queryByText("Project completed")).toBeNull();
    expect(screen.queryByText("Project cancelled")).toBeNull();
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Active projects");
    await scope("Closed projects 2");
    expect(screen.getByText("Project completed")).toBeVisible();
    expect(screen.queryByText("Project planned")).toBeNull();
    await scope("All projects 5");
    for (const status of statuses) expect(screen.getByText(`Project ${status}`)).toBeVisible();
    expect(qc.getQueryData<ListProjectsResponse>(projectKeys.list("ws-1"))?.projects).toHaveLength(5);
    expect(await qc.fetchQuery(projectDetailOptions("ws-1", "completed"))).toMatchObject({ description: "Preserved history" });
  });

  it("completes and reopens using the real mutation without removing canonical rows", async () => {
    renderWithI18n(page());
    await chooseStatus("planned", "Planned", "Completed");
    await waitFor(() => expect(screen.queryByText("Project planned")).toBeNull());
    await scope("Closed projects 3");
    await chooseStatus("planned", "Completed", "Paused");
    await waitFor(() => expect(screen.queryByText("Project planned")).toBeNull());
    await scope("Active projects 3");
    expect(screen.getByText("Project planned")).toBeVisible();
    expect(update).toHaveBeenCalledTimes(2);
    expect(qc.getQueryData<ListProjectsResponse>(projectKeys.list("ws-1"))?.projects).toHaveLength(5);
  });

  it("prunes selected rows on optimistic completion and does not reselect them on rollback", async () => {
    let reject!: (error: Error) => void;
    update.mockImplementationOnce(() => new Promise((_resolve, rejectPromise) => { reject = rejectPromise; }));
    renderWithI18n(page());
    const row = screen.getByText("Project planned").closest('[role="row"]')!;
    await userEvent.click(within(row as HTMLElement).getByRole("checkbox", { checked: false }));
    expect(screen.getByText("1 selected")).toBeVisible();
    await chooseStatus("planned", "Planned", "Completed");
    await waitFor(() => expect(screen.queryByText("Project planned")).toBeNull());
    expect(screen.queryByText("1 selected")).toBeNull();
    await act(async () => reject(new Error("denied")));
    await waitFor(() => expect(screen.getByText("Project planned")).toBeVisible());
    expect(screen.queryByText("1 selected")).toBeNull();
  });

  it("projects realtime cache changes and clears hidden selections on lifecycle, filters and workspace changes", async () => {
    const view = renderWithI18n(page());
    const select = async () => {
      const row = screen.getByText("Project planned").closest('[role="row"]')!;
      await userEvent.click(within(row as HTMLElement).getByRole("checkbox", { checked: false }));
    };
    await select();
    await act(async () => {
      qc.setQueryData<ListProjectsResponse>(projectKeys.list("ws-1"), (old) => ({
        ...old!, projects: old!.projects.map((p) => p.id === "planned" ? { ...p, status: "completed" } : p),
      }));
    });
    await waitFor(() => expect(screen.queryByText("Project planned")).toBeNull());
    expect(screen.queryByText("1 selected")).toBeNull();
    await scope("All projects 5");
    await select();
    await act(async () => useProjectViewStore.getState().toggleFilter("priorities", "urgent"));
    expect(screen.getByText("No projects match")).toBeVisible();
    await act(async () => useProjectViewStore.getState().clearFilters());
    expect(screen.queryByText("1 selected")).toBeNull();
    await select();
    qc.setQueryData(projectKeys.list("ws-2"), { projects: server, total: 5 });
    context.wsId = "ws-2";
    await act(async () => setCurrentWorkspace("beta", "ws-2"));
    view.rerender(page());
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Active projects");
    expect(screen.queryByText("1 selected")).toBeNull();
  });

  it("closes pending batch deletion when a selected project is hidden and does not revive it", async () => {
    renderWithI18n(page());
    const row = screen.getByText("Project planned").closest('[role="row"]')!;
    await userEvent.click(within(row as HTMLElement).getByRole("checkbox", { checked: false }));
    await userEvent.click(screen.getByRole("button", { name: "Delete" }));
    expect(screen.getByRole("dialog")).toBeVisible();
    await act(async () => {
      qc.setQueryData<ListProjectsResponse>(projectKeys.list("ws-1"), (old) => ({
        ...old!, projects: old!.projects.map((p) => p.id === "planned" ? { ...p, status: "completed" } : p),
      }));
    });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await scope("Closed projects 3");
    expect(screen.getByText("Project planned")).toBeVisible();
    expect(screen.queryByText("1 selected")).toBeNull();
    expect(screen.queryByRole("dialog")).toBeNull();
  });

  it("distinguishes workspace, lifecycle and filtered emptiness and preserves density and sorting", async () => {
    qc.setQueryData(projectKeys.list("ws-1"), { projects: [fixtures[3]], total: 1 });
    renderWithI18n(page());
    expect(screen.getByText("No active projects")).toBeVisible();
    await scope("Closed projects 1");
    await act(async () => useProjectViewStore.getState().toggleFilter("priorities", "high"));
    expect(screen.getByText("No projects match")).toBeVisible();
    await userEvent.click(screen.getByRole("button", { name: "Reset" }));
    await act(async () => useProjectViewStore.getState().setViewMode("comfortable"));
    expect(screen.getByRole("link", { name: /Project completed/ })).toHaveAttribute("href", "/acme/projects/completed");
    await act(async () => qc.setQueryData(projectKeys.list("ws-1"), { projects: [], total: 0 }));
    await waitFor(() => expect(screen.getByText("No projects yet")).toBeVisible());
  });

  it("combines explicit status filters in All and retains other filters and sort order when switching lifecycle", async () => {
    renderWithI18n(page());
    await act(async () => useProjectViewStore.getState().toggleFilter("statuses", "cancelled"));
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("All projects");
    expect(screen.getByText("Project cancelled")).toBeVisible();
    expect(screen.queryByText("Project completed")).toBeNull();
    await act(async () => useProjectViewStore.getState().toggleFilter("priorities", "high"));
    await scope("Active projects 3");
    expect(screen.getByText("Project planned")).toBeVisible();
    expect(screen.queryByText("Project paused")).toBeNull();
    expect(useProjectViewStore.getState().filters).toEqual({ statuses: [], priorities: ["high"], leads: [] });
    await act(async () => {
      useProjectViewStore.getState().clearFilters();
      useProjectViewStore.getState().setSortField("name");
      useProjectViewStore.getState().setSortDirection("desc");
    });
    const rows = screen.getAllByRole("row").slice(1);
    expect(rows.map((row) => within(row).getByText(/^Project /).textContent)).toEqual([
      "Project planned", "Project paused", "Project in_progress",
    ]);
  });
});
