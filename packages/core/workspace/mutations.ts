import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Workspace } from "../types";
import { api } from "../api";
import { useAuthStore } from "../auth";
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
      // Creating a workspace now marks the user as onboarded server-side.
      // Refresh the auth store so downstream consumers see the updated
      // `onboarded_at` without waiting for the next page load.
      void useAuthStore.getState().refreshMe();
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
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}
