import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClientProvider } from "@tanstack/react-query";
import { ApiClient, setApiInstance } from "@multica/core/api";
import { createAuthStore, registerAuthStore } from "@multica/core/auth";
import { WorkspaceSlugProvider } from "@multica/core/paths";
import { setCurrentWorkspace } from "@multica/core/platform";
import { bindWorkspaceSlugLookup, createQueryClient } from "@multica/core/query-client";
import { projectKeys, projectListOptions, useProjectViewStore } from "@multica/core/projects";
import { workspaceListOptions } from "@multica/core/workspace/queries";
import type { ListProjectsResponse, Project, ProjectStatus } from "@multica/core/types";
import { TooltipProvider } from "@multica/ui/components/ui/tooltip";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider } from "../../navigation";
import { ProjectsPage } from "./projects-page";
import { ProjectPicker } from "./project-picker";

const timestamp = "2026-11-15T00:00:00Z";
const statuses: ProjectStatus[] = ["planned", "in_progress", "paused", "completed", "cancelled"];
const browseStatuses: ProjectStatus[] = [...statuses, "planned", "completed"];
const fixtures: Project[] = browseStatuses.map((status, index) => ({
  id: `project-${index}`, workspace_id: "ws-acme", title: `Project ${index}`, status,
  description: null, icon: null, priority: "medium", lead_type: null, lead_id: null,
  start_date: null, due_date: null, creator_type: "member", creator_id: "fixture-user",
  visibility: "workspace", archived_at: index >= 5 ? timestamp : null,
  created_at: timestamp, updated_at: timestamp, issue_count: 1, done_count: 0, resource_count: 0,
}));
let client: ReturnType<typeof createQueryClient>;
let unbind: () => void;
let rows: Project[];
let failedDelete: string | null;
let savedPrefs: Record<string, Record<string, unknown>>;
let prefsUpdatedAt: Record<string, string>;
let requests: Array<{ method: string; url: URL }>;

function tree(slug = "acme", picker = false) {
  return <QueryClientProvider client={client}>
    <WorkspaceSlugProvider slug={slug}>
      <NavigationProvider value={{
        pathname: `/${slug}/projects`, searchParams: new URLSearchParams(), hash: "",
        push: vi.fn(), replace: vi.fn(), back: vi.fn(), getShareableUrl: (path) => path,
      }}>
        <TooltipProvider>
          {picker ? <ProjectPicker projectId="project-5" onUpdate={vi.fn()} /> : <ProjectsPage />}
        </TooltipProvider>
      </NavigationProvider>
    </WorkspaceSlugProvider>
  </QueryClientProvider>;
}

async function chooseScope(name: string) {
  await userEvent.click(within(screen.getByRole("heading", { level: 1 })).getByRole("button"));
  await userEvent.click(await screen.findByRole("menuitemradio", { name: new RegExp(`^${name}`) }));
  await waitFor(() => expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent(name));
}

async function select(id: string) {
  const row = screen.getByText(id).closest('[role="row"]') as HTMLElement;
  await userEvent.click(within(row).getByRole("checkbox", { name: "Select row" }));
}

async function refresh() {
  await act(async () => { await client.invalidateQueries({ queryKey: projectKeys.list("ws-acme") }); });
}

beforeEach(async () => {
  localStorage.clear();
  rows = fixtures.map((project) => ({ ...project }));
  failedDelete = null;
  savedPrefs = {};
  prefsUpdatedAt = {};
  requests = [];
  vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
    const url = new URL(String(input));
    const method = options?.method ?? "GET";
    requests.push({ method, url });
    const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), {
      status, headers: { "Content-Type": "application/json" },
    });
    if (url.pathname === "/api/workspaces") return json(["acme", "beta"].map((slug) => ({
      id: `ws-${slug}`, slug, name: slug, role: "owner", issue_prefix: "FIX", created_at: timestamp, updated_at: timestamp,
    })));
    if (url.pathname.endsWith("/members")) return json([{
      id: "fixture-member", workspace_id: "ws-acme", user_id: "fixture-user", role: "owner", name: "Fixture",
      email: "fixture@example.invalid", created_at: timestamp,
    }]);
    if (url.pathname === "/api/issue-view-preferences") {
      const slug = new Headers(options?.headers).get("X-Workspace-Slug") ?? "acme";
      if (method === "PUT") {
        savedPrefs[slug] = JSON.parse(String(options?.body)).prefs;
        prefsUpdatedAt[slug] = timestamp;
      }
      return json({ scope_type: "projects_page", scope_id: null, updated_at: prefsUpdatedAt[slug] ?? "", prefs: savedPrefs[slug] ?? {} });
    }
    if (url.pathname === "/api/projects") return json({ projects: rows, total: rows.length });
    if (url.pathname === "/api/autopilots") return json({ autopilots: [], total: 0 });
    if (["/api/agents", "/api/squads", "/api/pins"].includes(url.pathname)) return json([]);
    const id = url.pathname.split("/")[3];
    if (method === "POST" && url.pathname.endsWith("/archive")) {
      rows = rows.map((project) => project.id === id ? { ...project, archived_at: timestamp } : project);
      return json({ ...rows.find((project) => project.id === id), paused_autopilot_ids: [] });
    }
    if (method === "POST" && url.pathname.endsWith("/restore")) {
      rows = rows.map((project) => project.id === id ? { ...project, archived_at: null } : project);
      return json(rows.find((project) => project.id === id));
    }
    if (method === "DELETE" && url.pathname.startsWith("/api/projects/")) {
      if (id === failedDelete) return json({ error: "Retry fixture" }, 500);
      rows = rows.filter((project) => project.id !== id);
      return new Response(null, { status: 204 });
    }
    throw new Error(`Unexpected fixture HTTP request ${method} ${url.pathname}`);
  }));
  const api = new ApiClient("https://fixture.invalid");
  setApiInstance(api);
  const auth = createAuthStore({ api, storage: { getItem: () => null, setItem: () => {}, removeItem: () => {} } });
  registerAuthStore(auth);
  auth.getState().setUser({ id: "fixture-user", name: "Fixture", email: "fixture@example.invalid", avatar_url: null, onboarded_at: null, onboarding_questionnaire: {}, language: null, profile_description: "", timezone: null, created_at: timestamp, updated_at: timestamp });
  client = createQueryClient();
  unbind = bindWorkspaceSlugLookup(client);
  await client.fetchQuery(workspaceListOptions());
  setCurrentWorkspace("acme", "ws-acme");
  await Promise.resolve();
});

afterEach(() => {
  cleanup();
  client.clear();
  unbind();
  setCurrentWorkspace(null, null);
  vi.unstubAllGlobals();
});

describe("archive and lifecycle with the registered API, cache and providers", () => {
  it("keeps four scope counts separate while the canonical HTTP list and selected archived picker value stay complete", async () => {
    const view = renderWithI18n(tree());
    await screen.findByText("Project 0");
    expect(screen.queryByText("Project 3")).toBeNull();
    expect(screen.queryByText("Project 5")).toBeNull();
    await userEvent.click(within(screen.getByRole("heading", { level: 1 })).getByRole("button"));
    for (const [label, count] of [["All projects", 5], ["Active projects", 3], ["Closed projects", 2], ["Archived projects", 2]] as const) {
      expect(await screen.findByRole("menuitemradio", { name: new RegExp(`^${label}`) })).toHaveTextContent(String(count));
    }
    await userEvent.click(screen.getByRole("menuitemradio", { name: /^Archived projects/ }));
    await screen.findByText("Project 5");
    expect(screen.getByText("Project 6")).toBeVisible();
    expect(screen.queryByText("Project 3")).toBeNull();
    expect(requests.find((request) => request.url.pathname === "/api/projects")?.url.searchParams.get("include_archived")).toBe("true");
    expect(client.getQueryData<ListProjectsResponse>(projectKeys.list("ws-acme"))?.projects).toHaveLength(7);
    view.rerender(tree("acme", true));
    await userEvent.click(await screen.findByRole("button", { name: /Project 5/ }));
    expect(await screen.findByText("Project 0")).toBeVisible();
    expect(screen.getAllByText("Project 5").length).toBeGreaterThan(0);
    expect(screen.queryByText("Project 6")).toBeNull();
  });

  it("persists Archived, clears status filters on scope changes and rehydrates workspace-specific preferences", async () => {
    const view = renderWithI18n(tree());
    await screen.findByText("Project 0");
    await chooseScope("Archived projects");
    await waitFor(() => expect(savedPrefs.acme?.lifecycleView).toBe("archived"));
    await act(async () => useProjectViewStore.persist.rehydrate());
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Archived projects");
    await act(async () => useProjectViewStore.getState().toggleFilter("statuses", "completed"));
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("All projects");
    expect(screen.getByText("Project 3")).toBeVisible();
    expect(screen.queryByText("Project 6")).toBeNull();
    await chooseScope("Archived projects");
    expect(useProjectViewStore.getState().filters.statuses).toEqual([]);
    await act(async () => setCurrentWorkspace("beta", "ws-beta"));
    view.rerender(tree("beta"));
    await screen.findByText("Project 0");
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Active projects");
    await act(async () => setCurrentWorkspace("acme", "ws-acme"));
    view.rerender(tree());
    await screen.findByText("Project 5");
    expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Archived projects");
  });

  it("restores an archived closed project into Closed and keeps its status out of Active", async () => {
    renderWithI18n(tree());
    await screen.findByText("Project 0");
    await chooseScope("Archived projects");
    const row = screen.getByText("Project 6").closest('[role="row"]') as HTMLElement;
    await userEvent.click(within(row).getByRole("button", { name: "Restore" }));
    await waitFor(() => expect(screen.queryByText("Project 6")).toBeNull());
    await chooseScope("Active projects");
    expect(screen.queryByText("Project 6")).toBeNull();
    await chooseScope("Closed projects");
    expect(screen.getByText("Project 6")).toBeVisible();
    expect(rows.find((project) => project.id === "project-6")?.status).toBe("completed");
  });

  it("closes a pending batch archive when the visible selection context changes", async () => {
    renderWithI18n(tree());
    await screen.findByText("Project 0");
    await select("Project 0");
    await userEvent.click(within(screen.getByRole("toolbar")).getByRole("button", { name: "Archive" }));
    await screen.findByRole("dialog");
    await act(async () => useProjectViewStore.getState().toggleFilter("priorities", "high"));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(requests.filter((request) => request.method === "POST")).toHaveLength(0);
  });

  it("archives through the delete dialog and prunes the visible selection", async () => {
    renderWithI18n(tree());
    await screen.findByText("Project 0");
    await select("Project 0");
    await userEvent.click(within(screen.getByRole("toolbar")).getByRole("button", { name: "Delete" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Archive instead" }));
    await waitFor(() => expect(screen.queryByText("Project 0")).toBeNull());
    await waitFor(() => expect(screen.queryByText("1 selected")).toBeNull());
    expect(requests.filter((request) => request.method === "DELETE")).toHaveLength(0);
    await chooseScope("Archived projects");
    expect(screen.getByText("Project 0")).toBeVisible();
  });

  it("keeps a partial deletion retry open and closes it when an archived row is restored externally", async () => {
    renderWithI18n(tree());
    await screen.findByText("Project 0");
    await select("Project 0");
    await select("Project 1");
    failedDelete = "project-1";
    await userEvent.click(within(screen.getByRole("toolbar")).getByRole("button", { name: "Delete" }));
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(screen.queryByText("Project 0")).toBeNull());
    await waitFor(() => expect(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" })).toBeEnabled());
    failedDelete = null;
    await userEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Delete" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(requests.filter((request) => request.method === "DELETE").map((request) => request.url.pathname)).toEqual([
      "/api/projects/project-0", "/api/projects/project-1", "/api/projects/project-1",
    ]);
    await chooseScope("Archived projects");
    await select("Project 5");
    await userEvent.click(within(screen.getByRole("toolbar")).getByRole("button", { name: "Delete" }));
    rows = rows.map((project) => project.id === "project-5" ? { ...project, archived_at: null } : project);
    await refresh();
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    await waitFor(() => expect(screen.queryByText("1 selected")).toBeNull());
    expect((await client.fetchQuery(projectListOptions("ws-acme"))).projects).toHaveLength(5);
  });
});


it("restores the selected archived batch without changing either lifecycle status", async () => {
  renderWithI18n(tree());
  await screen.findByText("Project 0");
  await chooseScope("Archived projects");
  await select("Project 5");
  await select("Project 6");
  await userEvent.click(within(screen.getByRole("toolbar")).getByRole("button", { name: "Restore" }));
  await waitFor(() => expect(screen.queryByText("Project 5")).toBeNull());
  await waitFor(() => expect(screen.queryByText("Project 6")).toBeNull());
  await waitFor(() => expect(screen.queryByText("2 selected")).toBeNull());
  await chooseScope("Active projects");
  expect(screen.getByText("Project 5")).toBeVisible();
  expect(screen.queryByText("Project 6")).toBeNull();
  await chooseScope("Closed projects");
  expect(screen.getByText("Project 6")).toBeVisible();
  expect(rows.find((project) => project.id === "project-6")?.status).toBe("completed");
});

it("applies saved board and lifecycle settings without losing archived lookup rows", async () => {
  savedPrefs.acme = { layout: "board", groupBy: "priority", lifecycleView: "closed" };
  prefsUpdatedAt.acme = timestamp;
  renderWithI18n(tree());
  await screen.findByRole("link", { name: /Project 3/ });
  expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("Closed projects");
  expect(screen.getByRole("link", { name: /Project 4/ })).toBeVisible();
  expect(screen.queryByRole("link", { name: /Project 6/ })).toBeNull();
  expect(client.getQueryData<ListProjectsResponse>(projectKeys.list("ws-acme"))?.projects).toHaveLength(7);
});

it("opens a saved explicit status filter from an older server document in All", async () => {
  savedPrefs.acme = { layout: "table", filters: { statuses: ["completed"], priorities: [], leads: [] } };
  prefsUpdatedAt.acme = timestamp;
  renderWithI18n(tree());
  await screen.findByText("Project 3");
  expect(screen.getByRole("heading", { level: 1 })).toHaveTextContent("All projects");
  expect(screen.queryByText("Project 0")).toBeNull();
  expect(screen.queryByText("Project 6")).toBeNull();
});

it("discards an archive request whose autopilot lookup finishes after the selection context changed", async () => {
  const fetchMock = vi.mocked(fetch);
  const handle = fetchMock.getMockImplementation()!;
  let resolve!: (response: Response) => void;
  const pending = new Promise<Response>((done) => { resolve = done; });
  fetchMock.mockImplementation((input, options) => new URL(String(input)).pathname === "/api/autopilots"
    ? pending : handle(input, options));
  renderWithI18n(tree());
  await screen.findByText("Project 0");
  await select("Project 0");
  await userEvent.click(within(screen.getByRole("toolbar")).getByRole("button", { name: "Archive" }));
  await waitFor(() => expect(fetchMock.mock.calls.some(([input]) => String(input).includes("/api/autopilots"))).toBe(true));
  await act(async () => useProjectViewStore.getState().toggleFilter("priorities", "high"));
  await act(async () => { resolve(new Response(JSON.stringify({ autopilots: [] }), { headers: { "Content-Type": "application/json" } })); });
  expect(screen.queryByRole("dialog")).toBeNull();
  expect(requests.filter((request) => request.method === "POST")).toHaveLength(0);
});
