import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Workspace } from "../types";
import { api } from "../api";
import { defaultStorage } from "../platform/storage";
import { clearWorkspaceStorage } from "../platform/storage-cleanup";
import { workspaceKeys } from "./queries";
import {
  markWorkspaceDeletePending,
  unmarkWorkspaceDeletePending,
} from "./pending-delete";

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
      // Tombstone until settled: refetches that land while the DELETE is
      // pending go through workspaceListOptions' queryFn, which filters
      // against this registry — without it, an invalidation/reconnect
      // refetch would write the not-yet-committed row straight back.
      markWorkspaceDeletePending(workspaceId);
      // Cancel in-flight list fetches so a response that started before the
      // delete can't land after the optimistic update and resurrect the row.
      await qc.cancelQueries({ queryKey: workspaceKeys.list() });
      const previous = qc.getQueryData<Workspace[]>(workspaceKeys.list());
      qc.setQueryData<Workspace[]>(workspaceKeys.list(), (old) =>
        old?.filter((w) => w.id !== workspaceId),
      );
      // Capture the slug BEFORE the optimistic removal erases the row: the
      // realtime `workspace:deleted` handler reverse-looks-up the slug from
      // this same cache to clear `${key}:${slug}` storage, so on the
      // initiating client that lookup misses and cleanup falls to onSuccess.
      return { previous, slug: previous?.find((w) => w.id === workspaceId)?.slug };
    },
    // Success is the only path that clears the deleted workspace's persisted
    // `${key}:${slug}` namespace — a failed DELETE means the workspace still
    // exists and its drafts/view state must survive.
    onSuccess: (_data, _workspaceId, ctx) => {
      if (ctx?.slug) clearWorkspaceStorage(defaultStorage, ctx.slug);
    },
    // Rollback: the server still has the workspace, so put it back in the
    // list (the caller surfaces the error toast). onSettled's invalidate
    // then reconciles against server truth either way.
    onError: (_err, _workspaceId, ctx) => {
      if (ctx?.previous) qc.setQueryData(workspaceKeys.list(), ctx.previous);
    },
    onSettled: (_data, _err, workspaceId) => {
      // Lift the tombstone before invalidating so the reconcile refetch
      // reflects server truth: gone on success, restored on failure.
      unmarkWorkspaceDeletePending(workspaceId);
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}
