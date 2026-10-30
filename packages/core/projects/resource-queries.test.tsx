/** @vitest-environment jsdom */
import { act, renderHook } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  QueryObserver,
} from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { ListProjectResourcesResponse, ProjectResource } from "../types";
import {
  projectResourceKeys,
  projectResourcesOptions,
  useCreateProjectResource,
  useDeleteProjectResource,
  useUpdateProjectResource,
} from "./resource-queries";

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function resource(id: string): ProjectResource {
  return {
    id,
    project_id: "p1",
    workspace_id: "ws1",
    resource_type: "github_repo",
    resource_ref: { owner: "multica-ai", repo: id },
    label: id,
    position: 0,
    created_at: "2026-10-28T00:00:00Z",
    created_by: null,
  };
}
const a = resource("a");
const b = resource("b");
const c = resource("c");
const key = projectResourceKeys.list("ws1", "p1");
const failure = new Error("DELETE failed");

function list(...resources: ProjectResource[]): ListProjectResourcesResponse {
  return { resources, total: resources.length };
}

describe("useDeleteProjectResource", () => {
  let qc: QueryClient;
  let deletion: ReturnType<typeof deferred<void>>;
  let deleteResource: ReturnType<typeof vi.fn>;
  let createResource: ReturnType<typeof vi.fn>;
  let updateResource: ReturnType<typeof vi.fn>;
  let listResources: ReturnType<typeof vi.fn>;

  function hooks() {
    return renderHook(
      () => ({
        remove: useDeleteProjectResource("ws1", "p1"),
        create: useCreateProjectResource("ws1", "p1"),
        update: useUpdateProjectResource("ws1", "p1"),
      }),
      {
        wrapper: ({ children }: { children: ReactNode }) => (
          <QueryClientProvider client={qc}>{children}</QueryClientProvider>
        ),
      },
    );
  }

  beforeEach(() => {
    qc = new QueryClient({
      defaultOptions: {
        queries: { retry: false, staleTime: Infinity },
        mutations: { retry: false },
      },
    });
    deletion = deferred<void>();
    deleteResource = vi.fn().mockReturnValue(deletion.promise);
    createResource = vi.fn();
    updateResource = vi.fn();
    listResources = vi.fn();
    setApiInstance({
      deleteProjectResource: deleteResource,
      createProjectResource: createResource,
      updateProjectResource: updateResource,
      listProjectResources: listResources,
    } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it.each([
    ["create", "success"],
    ["update", "success"],
    ["delete", "success"],
    ["create", "failure"],
    ["update", "failure"],
    ["delete", "failure"],
  ] as const)(
    "preserves a concurrent successful %s when deletion ends in %s and refetch fails",
    async (operation, outcome) => {
      qc.setQueryData(key, list(a, b));
      const refresh = deferred<ListProjectResourcesResponse>();
      listResources.mockReturnValue(refresh.promise);
      const observer = new QueryObserver(
        qc,
        projectResourcesOptions("ws1", "p1"),
      );
      const unsubscribe = observer.subscribe(() => {});
      const { result } = hooks();
      let pending!: Promise<void>;
      await act(async () => {
        pending = result.current.remove.mutateAsync(a.id);
      });
      const settled =
        outcome === "failure" ? expect(pending).rejects.toBe(failure) : pending;
      await vi.waitFor(() =>
        expect(deleteResource).toHaveBeenCalledWith("p1", a.id),
      );
      const success = deferred<ProjectResource>();
      const otherDeletion = deferred<void>();
      createResource.mockReturnValue(success.promise);
      updateResource.mockReturnValue(success.promise);
      deleteResource.mockReturnValue(otherDeletion.promise);
      const updated = { ...b, label: "Updated" };
      await act(async () => {
        if (operation === "create") {
          const mutation = result.current.create.mutateAsync({
            resource_type: c.resource_type,
            resource_ref: c.resource_ref,
          });
          success.resolve(c);
          await mutation;
        } else if (operation === "update") {
          const mutation = result.current.update.mutateAsync({
            resourceId: b.id,
            data: { label: updated.label },
          });
          success.resolve(updated);
          await mutation;
        } else {
          const mutation = result.current.remove.mutateAsync(b.id);
          otherDeletion.resolve();
          await mutation;
        }
      });
      await act(async () => {
        if (outcome === "failure") deletion.reject(failure);
        else deletion.resolve();
        await settled;
      });
      const retained = outcome === "failure" ? [a] : [];
      const expected =
        operation === "create"
          ? list(...retained, b, c)
          : operation === "update"
            ? list(...retained, updated)
            : list(...retained);
      // Assert before the network can repair a clobbered cache.
      expect(observer.getCurrentResult().fetchStatus).toBe("fetching");
      expect(qc.getQueryData(key)).toEqual(expected);
      await act(async () => {
        refresh.reject(new Error("List unavailable"));
      });
      await vi.waitFor(() =>
        expect(observer.getCurrentResult().fetchStatus).toBe("idle"),
      );
      expect(observer.getCurrentResult().status).toBe("error");
      expect(qc.getQueryData(key)).toEqual(expected);
      unsubscribe();
    },
  );

  it("preserves a later successful delete after an earlier concurrent failure", async () => {
    qc.setQueryData(key, list(a, b));
    const second = deferred<void>();
    deleteResource
      .mockReturnValueOnce(deletion.promise)
      .mockReturnValueOnce(second.promise);
    const { result } = hooks();
    let first!: Promise<void>;
    let later!: Promise<void>;
    await act(async () => {
      first = result.current.remove.mutateAsync(a.id);
    });
    const rejected = expect(first).rejects.toBe(failure);
    await act(async () => {
      later = result.current.remove.mutateAsync(b.id);
    });
    await act(async () => {
      deletion.reject(failure);
      await rejected;
    });
    expect(qc.getQueryData(key)).toEqual(list(a, b));
    await act(async () => {
      second.resolve();
      await later;
    });
    expect(qc.getQueryData(key)).toEqual(list(a));
  });

  it("retains a list response received during a failed delete", async () => {
    qc.setQueryData(key, list(a, b));
    const { result } = hooks();
    let mutation!: Promise<void>;
    await act(async () => {
      mutation = result.current.remove.mutateAsync(a.id);
    });
    const rejected = expect(mutation).rejects.toBe(failure);
    const refresh = deferred<ListProjectResourcesResponse>();
    listResources.mockReturnValue(refresh.promise);
    const observer = new QueryObserver(
      qc,
      projectResourcesOptions("ws1", "p1"),
    );
    const unsubscribe = observer.subscribe(() => {});
    const fetching = observer.refetch();
    await act(async () => {
      refresh.resolve(list(a, c));
      await fetching;
    });
    await act(async () => {
      deletion.reject(failure);
      await rejected;
    });
    expect(qc.getQueryData(key)).toEqual(list(a, c));
    unsubscribe();
  });

  it("waits for success and changes only the target workspace/project", async () => {
    qc.setQueryData(key, list(a, b));
    const others = [
      projectResourceKeys.list("ws2", "p1"),
      projectResourceKeys.list("ws1", "p2"),
    ];
    others.forEach((other) => qc.setQueryData(other, list(a, b)));
    const { result } = hooks();
    let mutation!: Promise<void>;
    await act(async () => {
      mutation = result.current.remove.mutateAsync(a.id);
    });
    expect(qc.getQueryData(key)).toEqual(list(a, b));
    await act(async () => {
      deletion.resolve();
      await mutation;
    });
    expect(qc.getQueryData(key)).toEqual(list(b));
    expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
    others.forEach((other) => {
      expect(qc.getQueryData(other)).toEqual(list(a, b));
      expect(qc.getQueryState(other)?.isInvalidated).toBe(false);
    });
  });

  it.each(["success", "failure"])(
    "does not manufacture an absent cache on %s",
    async (outcome) => {
      const { result } = hooks();
      await act(async () => {
        const mutation = result.current.remove.mutateAsync(a.id);
        if (outcome === "success") {
          deletion.resolve();
          await mutation;
        } else {
          const rejected = expect(mutation).rejects.toBe(failure);
          deletion.reject(failure);
          await rejected;
        }
      });
      expect(qc.getQueryState(key)).toBeUndefined();
      expect(listResources).not.toHaveBeenCalled();
    },
  );

  it("does not decrement total for an already absent resource or duplicate success", async () => {
    qc.setQueryData(key, list(b));
    const { result } = hooks();
    deletion.resolve();
    await act(async () => {
      await result.current.remove.mutateAsync(a.id);
      await result.current.remove.mutateAsync(b.id);
      await result.current.remove.mutateAsync(b.id);
    });
    expect(qc.getQueryData(key)).toEqual(list());
  });

  it.each(["loaded", "unfetched"])(
    "replaces an in-flight %s list on delete success",
    async (cache) => {
      if (cache === "loaded") qc.setQueryData(key, list(a, b));
      const { result } = hooks();
      let mutation!: Promise<void>;
      await act(async () => {
        mutation = result.current.remove.mutateAsync(a.id);
      });
      await vi.waitFor(() => expect(deleteResource).toHaveBeenCalledTimes(1));
      const stale = deferred<ListProjectResourcesResponse>();
      const fresh = deferred<ListProjectResourcesResponse>();
      listResources
        .mockReturnValue(fresh.promise)
        .mockReturnValueOnce(stale.promise);
      const observer = new QueryObserver(
        qc,
        projectResourcesOptions("ws1", "p1"),
      );
      const unsubscribe = observer.subscribe(() => {});
      const firstFetch = observer.refetch();
      await vi.waitFor(() => expect(listResources).toHaveBeenCalledTimes(1));
      await act(async () => {
        deletion.resolve();
        await mutation;
      });
      expect(listResources).toHaveBeenCalledTimes(2);
      await act(async () => {
        fresh.resolve(list(b));
        stale.resolve(list(a, b));
        await firstFetch;
      });
      await vi.waitFor(() =>
        expect(observer.getCurrentResult().fetchStatus).toBe("idle"),
      );
      expect(qc.getQueryData(key)).toEqual(list(b));
      unsubscribe();
    },
  );
});
