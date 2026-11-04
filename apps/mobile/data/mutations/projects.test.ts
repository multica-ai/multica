// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import type { Project, ProjectResource } from "@multica/core/types";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "@/data/api";
import {
  projectKeys,
  projectListOptions,
  projectResourcesOptions,
} from "@/data/queries/projects";
import { upsertIntoProjectsList } from "@/data/realtime/project-ws-updaters";
import {
  useCreateProject,
  useCreateProjectResource,
  useDeleteProjectResource,
} from "./projects";

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
  api: {
    createProject: vi.fn(),
    listProjects: vi.fn(),
    deleteProjectResource: vi.fn(),
    createProjectResource: vi.fn(),
    listProjectResources: vi.fn(),
  },
}));

vi.mock("@/data/workspace-store", () => ({
  useWorkspaceStore: (
    selector: (s: { currentWorkspaceId: string }) => unknown,
  ) => selector({ currentWorkspaceId: "workspace-1" }),
}));

const wsId = "workspace-1";
const existing = { id: "existing", title: "Existing project" } as Project;
const created = { id: "new", title: "New project" } as Project;

describe("useCreateProject", () => {
  beforeEach(() => {
    state.qc = new QueryClient({
      defaultOptions: {
        queries: { staleTime: 60_000, retry: false },
        mutations: { retry: false },
      },
    });
    vi.mocked(api.createProject).mockReset().mockResolvedValue(created);
    vi.mocked(api.listProjects).mockReset().mockResolvedValue({
      projects: [existing, created],
      total: 2,
    });
  });

  afterEach(() => state.qc.clear());

  it("seeds detail without suppressing the first full list request", async () => {
    await useCreateProject().mutateAsync({ title: created.title });

    expect(api.createProject).toHaveBeenCalledWith({ title: created.title });
    expect(state.qc.getQueryData(projectKeys.detail(wsId, created.id))).toEqual(created);
    expect(state.qc.getQueryState(projectKeys.list(wsId))).toBeUndefined();

    const observer = new QueryObserver(state.qc, projectListOptions(wsId));
    const unsubscribe = observer.subscribe(() => {});
    try {
      await vi.waitFor(() => {
        expect(observer.getCurrentResult().data).toEqual([existing, created]);
      });
      expect(api.listProjects).toHaveBeenCalledTimes(1);
    } finally {
      unsubscribe();
    }
  });

  it("prepends to an already loaded empty list", async () => {
    state.qc.setQueryData(projectKeys.list(wsId), []);
    await useCreateProject().mutateAsync({ title: created.title });
    expect(state.qc.getQueryData(projectKeys.list(wsId))).toEqual([created]);
    expect(api.listProjects).not.toHaveBeenCalled();
  });

  it.each(["before", "after"])("deduplicates a create event %s mutation success", async (order) => {
    state.qc.setQueryData(projectKeys.list(wsId), [existing]);
    state.qc.setQueryData(projectKeys.list("workspace-2"), [existing]);
    if (order === "before") upsertIntoProjectsList(state.qc, wsId, created);

    await useCreateProject().mutateAsync({ title: created.title });

    if (order === "after") upsertIntoProjectsList(state.qc, wsId, created);
    expect(state.qc.getQueryData(projectKeys.list(wsId))).toEqual([created, existing]);
    expect(state.qc.getQueryData(projectKeys.list("workspace-2"))).toEqual([existing]);
    expect(state.qc.getQueryData(projectKeys.detail(wsId, created.id))).toEqual(created);
    expect(api.listProjects).not.toHaveBeenCalled();
  });
});

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe("useDeleteProjectResource", () => {
  const projectId = "p1";
  const key = projectKeys.resources(wsId, projectId);
  const a: ProjectResource = {
    id: "a",
    project_id: projectId,
    workspace_id: wsId,
    resource_type: "github_repo",
    resource_ref: { url: "https://github.com/multica-ai/a" },
    label: null,
    position: 0,
    created_at: "2026-10-28T00:00:00Z",
    created_by: null,
  };
  const b = { ...a, id: "b" };
  const c = { ...a, id: "c" };
  const parent = { id: projectId, resource_count: 2 } as Project;

  beforeEach(() => {
    state.qc = new QueryClient({
      defaultOptions: {
        queries: { staleTime: Infinity, retry: false },
        mutations: { retry: false },
      },
    });
    state.qc.setQueryData(key, [a, b]);
    state.qc.setQueryData(projectKeys.detail(wsId, projectId), parent);
    state.qc.setQueryData(projectKeys.list(wsId), [parent]);
    vi.mocked(api.deleteProjectResource).mockReset();
    vi.mocked(api.createProjectResource).mockReset();
    vi.mocked(api.listProjectResources).mockReset();
  });
  afterEach(() => state.qc.clear());

  it("keeps the resource and counts until success, without changing another workspace", async () => {
    const deletion = deferred<void>();
    vi.mocked(api.deleteProjectResource).mockReturnValue(deletion.promise);
    const otherKey = projectKeys.resources("workspace-2", projectId);
    state.qc.setQueryData(otherKey, [a, b]);
    const mutation = useDeleteProjectResource(projectId).mutateAsync(a.id);
    await vi.waitFor(() => expect(api.deleteProjectResource).toHaveBeenCalled());
    expect(state.qc.getQueryData(key)).toEqual([a, b]);
    expect(state.qc.getQueryData(projectKeys.detail(wsId, projectId))).toEqual(parent);
    deletion.resolve();
    await mutation;
    expect(state.qc.getQueryData(key)).toEqual([b]);
    expect(state.qc.getQueryData(projectKeys.detail(wsId, projectId))).toEqual({
      ...parent, resource_count: 1,
    });
    expect(state.qc.getQueryData(projectKeys.list(wsId))).toEqual([
      { ...parent, resource_count: 1 },
    ]);
    expect(state.qc.getQueryState(key)?.isInvalidated).toBe(true);
    expect(state.qc.getQueryData(otherKey)).toEqual([a, b]);
    expect(state.qc.getQueryState(otherKey)?.isInvalidated).toBe(false);
  });

  it("preserves successful deletes and creates when another delete fails", async () => {
    const deletion = deferred<void>();
    vi.mocked(api.deleteProjectResource)
      .mockReturnValueOnce(deletion.promise)
      .mockResolvedValue(undefined);
    const remove = useDeleteProjectResource(projectId);
    const mutation = remove.mutateAsync(a.id);
    const rejected = expect(mutation).rejects.toThrow("Delete failed");
    await vi.waitFor(() => expect(api.deleteProjectResource).toHaveBeenCalledTimes(1));
    await remove.mutateAsync(b.id);
    vi.mocked(api.createProjectResource).mockResolvedValue(c);
    await useCreateProjectResource(projectId).mutateAsync({
      resource_type: "github_repo", resource_ref: c.resource_ref,
    });
    deletion.reject(new Error("Delete failed"));
    await rejected;
    expect(state.qc.getQueryData(key)).toEqual([a, c]);
    expect(state.qc.getQueryData(projectKeys.detail(wsId, projectId))).toEqual(parent);
    expect(state.qc.getQueryData(projectKeys.list(wsId))).toEqual([parent]);
  });

  it.each(["loaded", "unfetched"])("replaces an in-flight %s list after delete success", async (cache) => {
    if (cache === "unfetched") state.qc.removeQueries({ queryKey: key });
    const deletion = deferred<void>();
    vi.mocked(api.deleteProjectResource).mockReturnValue(deletion.promise);
    const mutation = useDeleteProjectResource(projectId).mutateAsync(a.id);
    await vi.waitFor(() => expect(api.deleteProjectResource).toHaveBeenCalled());
    const stale = deferred<{ resources: ProjectResource[]; total: number }>();
    const fresh = deferred<{ resources: ProjectResource[]; total: number }>();
    vi.mocked(api.listProjectResources)
      .mockReturnValue(fresh.promise)
      .mockReturnValueOnce(stale.promise);
    const observer = new QueryObserver(state.qc, projectResourcesOptions(wsId, projectId));
    const unsubscribe = observer.subscribe(() => {});
    try {
      const fetching = observer.refetch();
      await vi.waitFor(() => expect(api.listProjectResources).toHaveBeenCalledTimes(1));
      deletion.resolve();
      await mutation;
      expect(api.listProjectResources).toHaveBeenCalledTimes(2);
      fresh.resolve({ resources: [b], total: 1 });
      stale.resolve({ resources: [a, b], total: 2 });
      await fetching;
      await vi.waitFor(() => expect(observer.getCurrentResult().fetchStatus).toBe("idle"));
      expect(state.qc.getQueryData(key)).toEqual([b]);
    } finally {
      stale.resolve({ resources: [a, b], total: 2 });
      fresh.resolve({ resources: [b], total: 1 });
      unsubscribe();
    }
  });
});
