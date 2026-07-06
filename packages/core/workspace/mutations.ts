import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Workspace } from "../types";
import { api } from "../api";
import { workspaceKeys } from "./queries";

export function useCreateWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { name: string; slug: string; description?: string }) =>
      api.createWorkspace(data),
    // Seed the workspace list cache BEFORE callers navigate to /{newWs.slug}/issues.
    // The destination [workspaceSlug]/layout queries by slug from this cache;
    // without seeding, it would briefly show "loading" before the background
    // invalidation completes. TanStack Query guarantees this onSuccess runs
    // before mutateAsync's resolver / before any callback-style onSuccess
    // passed to mutate(), so any caller that navigates after the mutation
    // resolves will see the seeded data synchronously. Switching workspaces
    // is pure navigation now — no imperative store writes needed.
    onSuccess: (newWs) => {
      qc.setQueryData(workspaceKeys.list(), (old: Workspace[] = []) => [...old, newWs]);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useLeaveWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (workspaceId: string) => api.leaveWorkspace(workspaceId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useDeleteWorkspace() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (workspaceId: string) => api.deleteWorkspace(workspaceId),
    // Optimistically drop the workspace from the list cache while the
    // DELETE is in flight. The delete flow navigates away BEFORE awaiting
    // the mutation (see workspace-tab.tsx's navigateAwayFromCurrentWorkspace
    // for the CancelledError race that forces that ordering), so during the
    // pending window every list consumer — sidebar switcher, by-slug route
    // resolution, post-auth destination — must already see the workspace as
    // gone, or a concurrent list refetch re-presents it as selectable and
    // it can be re-entered mid-delete.
    onMutate: async (workspaceId) => {
      // Cancel in-flight list fetches so a response that started before the
      // delete can't land after the optimistic update and resurrect the row.
      await qc.cancelQueries({ queryKey: workspaceKeys.list() });
      const previous = qc.getQueryData<Workspace[]>(workspaceKeys.list());
      qc.setQueryData<Workspace[]>(workspaceKeys.list(), (old) =>
        old?.filter((w) => w.id !== workspaceId),
      );
      return { previous };
    },
    // Rollback: the server still has the workspace, so put it back in the
    // list (the caller surfaces the error toast). onSettled's invalidate
    // then reconciles against server truth either way.
    onError: (_err, _workspaceId, ctx) => {
      if (ctx?.previous) qc.setQueryData(workspaceKeys.list(), ctx.previous);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}
