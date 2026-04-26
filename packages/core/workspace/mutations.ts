import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Workspace, CreateRepoBindingRequest, UpdateRepoBindingRequest } from "../types";
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
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.list() });
    },
  });
}

export function useCreateRepoBinding(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateRepoBindingRequest) => api.createRepoBinding(workspaceId, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.repoBindings(workspaceId) });
    },
  });
}

export function useUpdateRepoBinding(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ bindingId, data }: { bindingId: string; data: UpdateRepoBindingRequest }) =>
      api.updateRepoBinding(workspaceId, bindingId, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.repoBindings(workspaceId) });
    },
  });
}

export function useDeleteRepoBinding(workspaceId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (bindingId: string) => api.deleteRepoBinding(workspaceId, bindingId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.repoBindings(workspaceId) });
    },
  });
}
