import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueView } from "../api/schemas";
import type { CreateIssueViewRequest } from "../api/schemas";
import { issueViewKeys } from "./queries";

/** Create is a confirmed flow (dialog + toast) — no optimism, await then
 *  invalidate so every surface's selector refetches. */
export function useCreateIssueView(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateIssueViewRequest) => api.createIssueView(data),
    onSuccess: (created) => {
      // Seed the created view into its scope's list BEFORE invalidating:
      // callers activate it immediately, and an id absent from the (stale)
      // list reads as "view deleted" and bounces the surface back to the
      // default tab. The invalidate still fetches the canonical list.
      if (created) {
        queryClient.setQueryData<IssueView[]>(
          issueViewKeys.list(wsId, {
            scope_type: created.scope_type as "workspace" | "my" | "project",
            scope_id: created.scope_id,
          }),
          (old) =>
            old && !old.some((v) => v.id === created.id)
              ? [...old, created]
              : old,
        );
      }
      void queryClient.invalidateQueries({ queryKey: issueViewKeys.all(wsId) });
    },
  });
}

export interface UpdateIssueViewInput {
  id: string;
  name?: string;
  visibility?: "private" | "workspace";
  scope_variant?: string | null;
  query?: Record<string, unknown>;
  display?: Record<string, unknown>;
  expected_revision: number;
}

/** Optimistic-free by design: edits are a confirmed dialog flow and the 409
 *  conflict path (someone else edited first) needs the server's answer. */
export function useUpdateIssueView(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...data }: UpdateIssueViewInput) =>
      api.updateIssueView(id, data),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: issueViewKeys.all(wsId) });
    },
  });
}

export function useDeleteIssueView(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteIssueView(id),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: issueViewKeys.all(wsId) });
    },
  });
}
