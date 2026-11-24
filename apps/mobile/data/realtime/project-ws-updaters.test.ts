// @vitest-environment node
import { QueryClient, QueryObserver } from "@tanstack/react-query";
import type { Project } from "@multica/core/types";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { api } from "@/data/api";
import { projectKeys, projectListOptions } from "@/data/queries/projects";
import { upsertIntoProjectsList } from "./project-ws-updaters";

vi.mock("@/data/api", () => ({ api: { listProjects: vi.fn() } }));

const wsId = "workspace-1";
const existing = { id: "existing", title: "Existing project" } as Project;
const created = { id: "new", title: "New project" } as Project;

describe("upsertIntoProjectsList", () => {
  let qc: QueryClient;

  beforeEach(() => {
    // Match mobile's freshness window: a partial list must not suppress GET.
    qc = new QueryClient({
      defaultOptions: { queries: { staleTime: 60_000, retry: false } },
    });
    vi.mocked(api.listProjects).mockReset();
  });

  afterEach(() => qc.clear());

  it("fetches the full list on first mount after a create event", async () => {
    vi.mocked(api.listProjects).mockResolvedValue({
      projects: [existing, created],
      total: 2,
    });
    upsertIntoProjectsList(qc, wsId, created);
    expect(qc.getQueryState(projectKeys.list(wsId))).toBeUndefined();

    const observer = new QueryObserver(qc, projectListOptions(wsId));
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

  it("does not turn an unfinished first fetch into a partial successful list", async () => {
    const response = Promise.withResolvers<Awaited<ReturnType<typeof api.listProjects>>>();
    vi.mocked(api.listProjects).mockReturnValue(response.promise);
    const observer = new QueryObserver(qc, projectListOptions(wsId));
    const unsubscribe = observer.subscribe(() => {});
    try {
      upsertIntoProjectsList(qc, wsId, created);
      expect(qc.getQueryState(projectKeys.list(wsId))).toMatchObject({
        status: "pending",
        fetchStatus: "fetching",
        data: undefined,
      });
      response.resolve({ projects: [existing, created], total: 2 });
      await vi.waitFor(() => {
        expect(observer.getCurrentResult().data).toEqual([existing, created]);
      });
      expect(api.listProjects).toHaveBeenCalledTimes(1);
    } finally {
      unsubscribe();
    }
  });

  it.each([{ projects: [] }, { projects: [existing] }])("prepends to an already loaded list $projects", ({ projects }) => {
    qc.setQueryData(projectKeys.list(wsId), projects);
    upsertIntoProjectsList(qc, wsId, created);
    expect(qc.getQueryData(projectKeys.list(wsId))).toEqual([created, ...projects]);
    expect(api.listProjects).not.toHaveBeenCalled();
  });

  it("replaces a repeated project in place and leaves other workspaces alone", () => {
    qc.setQueryData(projectKeys.list(wsId), [existing, created]);
    qc.setQueryData(projectKeys.list("workspace-2"), [existing]);
    const updated = { ...created, title: "Updated title" };

    upsertIntoProjectsList(qc, wsId, updated);
    upsertIntoProjectsList(qc, wsId, updated);

    expect(qc.getQueryData(projectKeys.list(wsId))).toEqual([existing, updated]);
    expect(qc.getQueryData(projectKeys.list("workspace-2"))).toEqual([existing]);
  });
});
