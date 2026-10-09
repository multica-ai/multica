// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import type { Project } from "@multica/core/types";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "@/data/api";
import { projectKeys, projectListOptions } from "@/data/queries/projects";
import { upsertIntoProjectsList } from "@/data/realtime/project-ws-updaters";
import { useCreateProject } from "./projects";

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
  api: { createProject: vi.fn(), listProjects: vi.fn() },
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
