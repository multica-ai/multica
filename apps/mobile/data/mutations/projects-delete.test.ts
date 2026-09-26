// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import type { Project, ProjectResource } from "@multica/core/types";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "@/data/api";
import { projectKeys, projectListOptions } from "@/data/queries/projects";
import {
  clearProjectDetail,
  patchProjectDetail,
  patchProjectsList,
  removeFromProjectsList,
} from "@/data/realtime/project-ws-updaters";
import { useDeleteProject } from "./projects";

const state = vi.hoisted(() => ({ qc: undefined as unknown as QueryClient }));

vi.mock("@tanstack/react-query", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@tanstack/react-query")>();
  return {
    ...actual,
    useQueryClient: () => state.qc,
    // Keep the real mutation lifecycle, substituting only React hook wiring.
    useMutation: (
      options: ConstructorParameters<typeof actual.MutationObserver>[1],
    ) => {
      const observer = new actual.MutationObserver(state.qc, options);
      return { mutateAsync: (variables: unknown) => observer.mutate(variables) };
    },
  };
});

vi.mock("@/data/api", () => ({
  api: { deleteProject: vi.fn(), listProjects: vi.fn() },
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (
    selector: (s: { currentWorkspaceId: string }) => unknown,
  ) => selector({ currentWorkspaceId: "workspace-1" }),
}));

const wsId = "workspace-1";
const project = { id: "project-1", title: "Project" } as Project;
const other = { id: "project-2", title: "Other project" } as Project;
const resources = [{ id: "resource-1", project_id: project.id }] as ProjectResource[];
const listKey = projectKeys.list(wsId);
const detailKey = projectKeys.detail(wsId, project.id);
const resourcesKey = projectKeys.resources(wsId, project.id);

function seedProject() {
  state.qc.setQueryData(listKey, [project, other]);
  state.qc.setQueryData(detailKey, project);
  state.qc.setQueryData(resourcesKey, resources);
}

function expectProjectRetained() {
  expect(state.qc.getQueryData(listKey)).toEqual([project, other]);
  expect(state.qc.getQueryData(detailKey)).toEqual(project);
  expect(state.qc.getQueryData(resourcesKey)).toEqual(resources);
}

describe("useDeleteProject", () => {
  beforeEach(() => {
    state.qc = new QueryClient({
      defaultOptions: {
        queries: { staleTime: 60_000, retry: false },
        mutations: { retry: false },
      },
    });
    vi.mocked(api.deleteProject).mockReset().mockResolvedValue(undefined);
    vi.mocked(api.listProjects).mockReset().mockResolvedValue({ projects: [other], total: 1 });
  });

  afterEach(() => state.qc.clear());

  it.each([
    ["permission denial", Object.assign(new Error("Forbidden"), { status: 403 })],
    ["network failure", new TypeError("Network request failed")],
  ])("retains list, detail and resources after %s", async (_label, error) => {
    seedProject();
    vi.mocked(api.deleteProject).mockRejectedValue(error);

    await expect(useDeleteProject(project.id).mutateAsync()).rejects.toBe(error);

    expectProjectRetained();
    expect(api.deleteProject).toHaveBeenCalledWith(project.id);
    expect(api.listProjects).not.toHaveBeenCalled();
  });

  it("waits for success before removing only the target project's caches", async () => {
    seedProject();
    const otherDetailKey = projectKeys.detail(wsId, other.id);
    const otherResourcesKey = projectKeys.resources(wsId, other.id);
    state.qc.setQueryData(otherDetailKey, other);
    state.qc.setQueryData(otherResourcesKey, []);
    state.qc.setQueryData(projectKeys.list("workspace-2"), [project]);
    state.qc.setQueryData(projectKeys.detail("workspace-2", project.id), project);
    state.qc.setQueryData(projectKeys.resources("workspace-2", project.id), resources);
    const deletion = Promise.withResolvers<void>();
    vi.mocked(api.deleteProject).mockReturnValue(deletion.promise);

    const mutation = useDeleteProject(project.id).mutateAsync();
    await vi.waitFor(() => expect(api.deleteProject).toHaveBeenCalledTimes(1));
    expectProjectRetained();

    deletion.resolve();
    await mutation;

    expect(state.qc.getQueryData(listKey)).toEqual([other]);
    expect(state.qc.getQueryState(detailKey)).toBeUndefined();
    expect(state.qc.getQueryState(resourcesKey)).toBeUndefined();
    expect(state.qc.getQueryData(otherDetailKey)).toEqual(other);
    expect(state.qc.getQueryData(otherResourcesKey)).toEqual([]);
    expect(state.qc.getQueryData(projectKeys.list("workspace-2"))).toEqual([project]);
    expect(state.qc.getQueryData(projectKeys.detail("workspace-2", project.id))).toEqual(project);
    expect(state.qc.getQueryData(projectKeys.resources("workspace-2", project.id))).toEqual(resources);
  });

  it("preserves realtime updates received while a failed delete was pending", async () => {
    seedProject();
    const deletion = Promise.withResolvers<void>();
    const error = new Error("Forbidden");
    vi.mocked(api.deleteProject).mockReturnValue(deletion.promise);
    const mutation = useDeleteProject(project.id).mutateAsync();
    const rejected = expect(mutation).rejects.toBe(error);
    await vi.waitFor(() => expect(api.deleteProject).toHaveBeenCalledTimes(1));

    const updated = { ...project, title: "Updated on another client" };
    patchProjectsList(state.qc, wsId, updated);
    patchProjectDetail(state.qc, wsId, updated);
    deletion.reject(error);
    await rejected;

    expect(state.qc.getQueryData(listKey)).toEqual([updated, other]);
    expect(state.qc.getQueryData(detailKey)).toEqual(updated);
    expect(state.qc.getQueryData(resourcesKey)).toEqual(resources);
  });

  it("does not resurrect a project deleted remotely while the local delete fails", async () => {
    seedProject();
    const deletion = Promise.withResolvers<void>();
    const error = new Error("Not found");
    vi.mocked(api.deleteProject).mockReturnValue(deletion.promise);
    const mutation = useDeleteProject(project.id).mutateAsync();
    const rejected = expect(mutation).rejects.toBe(error);
    await vi.waitFor(() => expect(api.deleteProject).toHaveBeenCalledTimes(1));

    removeFromProjectsList(state.qc, wsId, project.id);
    clearProjectDetail(state.qc, wsId, project.id);
    deletion.reject(error);
    await rejected;

    expect(state.qc.getQueryData(listKey)).toEqual([other]);
    expect(state.qc.getQueryState(detailKey)).toBeUndefined();
    expect(state.qc.getQueryState(resourcesKey)).toBeUndefined();
  });

  it.each(["loaded", "unfetched"])("replaces a stale in-flight %s list request after success", async (cache) => {
    if (cache === "loaded") seedProject();
    const deletion = Promise.withResolvers<void>();
    vi.mocked(api.deleteProject).mockReturnValue(deletion.promise);
    const mutation = useDeleteProject(project.id).mutateAsync();
    await vi.waitFor(() => expect(api.deleteProject).toHaveBeenCalledTimes(1));

    // A list request can start during DELETE and capture a pre-delete snapshot.
    const stale = Promise.withResolvers<Awaited<ReturnType<typeof api.listProjects>>>();
    const fresh = Promise.withResolvers<Awaited<ReturnType<typeof api.listProjects>>>();
    let staleSignal: AbortSignal | undefined;
    vi.mocked(api.listProjects).mockReturnValue(fresh.promise).mockImplementationOnce((options) => {
      staleSignal = options?.signal;
      return stale.promise;
    });
    const observer = new QueryObserver(state.qc, projectListOptions(wsId));
    const unsubscribe = observer.subscribe(() => {});
    const firstFetch = observer.refetch();
    try {
      await vi.waitFor(() => expect(api.listProjects).toHaveBeenCalledTimes(1));
      deletion.resolve();
      // The mutation must finish even while its background refresh is pending.
      await mutation;
      expect(api.listProjects).toHaveBeenCalledTimes(2);
      fresh.resolve({ projects: [other], total: 1 });
      stale.resolve({ projects: [project, other], total: 2 });
      await firstFetch;

      await vi.waitFor(() => {
        expect(observer.getCurrentResult()).toMatchObject({
          status: "success", fetchStatus: "idle", data: [other],
        });
      });
      expect(staleSignal?.aborted).toBe(true);
      expect(api.listProjects).toHaveBeenCalledTimes(2);
    } finally {
      unsubscribe();
    }
  });

  it("leaves an unobserved, unfetched list absent after success", async () => {
    await useDeleteProject(project.id).mutateAsync();

    expect(state.qc.getQueryState(listKey)).toBeUndefined();
    expect(api.listProjects).not.toHaveBeenCalled();
  });
});
