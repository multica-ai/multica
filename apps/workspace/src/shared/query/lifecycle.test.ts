import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import {
  prepareQueryCacheForReconnect,
  prepareQueryCacheForWorkspaceSwitch,
} from "./lifecycle";
import { queryKeys } from "./keys";

describe("query lifecycle", () => {
  it("clears issue and task detail caches during workspace switch", async () => {
    const queryClient = new QueryClient();

    queryClient.setQueryData(queryKeys.session.me(), { id: "user-1" });
    queryClient.setQueryData(queryKeys.workspaces.all(), [{ id: "ws-1" }]);
    queryClient.setQueryData(queryKeys.workspace.members("ws-1"), [{ id: "member-1" }]);
    queryClient.setQueryData(queryKeys.workspace.members("ws-2"), [{ id: "member-2" }]);
    queryClient.setQueryData(queryKeys.issues.detail("issue-1"), { id: "issue-1" });
    queryClient.setQueryData(queryKeys.tasks.activeByIssue("issue-1"), { id: "task-1" });

    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");

    await prepareQueryCacheForWorkspaceSwitch(queryClient, "ws-1");

    expect(queryClient.getQueryData(queryKeys.session.me())).toEqual({ id: "user-1" });
    expect(queryClient.getQueryData(queryKeys.workspaces.all())).toEqual([{ id: "ws-1" }]);
    expect(queryClient.getQueryData(queryKeys.workspace.members("ws-1"))).toBeUndefined();
    expect(queryClient.getQueryData(queryKeys.workspace.members("ws-2"))).toEqual([{ id: "member-2" }]);
    expect(queryClient.getQueryData(queryKeys.issues.detail("issue-1"))).toBeUndefined();
    expect(queryClient.getQueryData(queryKeys.tasks.activeByIssue("issue-1"))).toBeUndefined();
    expect(invalidateSpy).toHaveBeenCalledWith({ queryKey: queryKeys.workspaces.all() });
  });

  it("invalidates session, workspace, issue, and task caches on reconnect", async () => {
    const queryClient = new QueryClient();
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");

    await prepareQueryCacheForReconnect(queryClient, "ws-1");

    expect(invalidateSpy).toHaveBeenNthCalledWith(1, { queryKey: queryKeys.session.all() });
    expect(invalidateSpy).toHaveBeenNthCalledWith(2, { queryKey: queryKeys.workspaces.all() });
    expect(invalidateSpy).toHaveBeenNthCalledWith(3, {
      predicate: expect.any(Function),
    });

    const predicate = invalidateSpy.mock.calls[2]?.[0]?.predicate as ((query: { queryKey: readonly unknown[] }) => boolean) | undefined;
    expect(predicate?.({ queryKey: queryKeys.workspace.members("ws-1") })).toBe(true);
    expect(predicate?.({ queryKey: queryKeys.issues.detail("issue-1") })).toBe(true);
    expect(predicate?.({ queryKey: queryKeys.tasks.byIssue("issue-1") })).toBe(true);
    expect(predicate?.({ queryKey: queryKeys.workspace.members("ws-2") })).toBe(false);
  });
});
