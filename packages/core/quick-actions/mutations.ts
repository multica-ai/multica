import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { quickActionKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import { issueKeys } from "../issues/queries";
import type {
  Comment,
  CreateQuickActionRequest,
  UpdateQuickActionRequest,
} from "../types";

/**
 * Catalog mutations. None are optimistic: they happen in the settings form
 * where a round-trip is acceptable, and the server canonicalizes values
 * (position assignment, prompt trimming, derived visibility) anyway — an
 * optimistic row would show the wrong visibility badge until it settled.
 */
export function useCreateQuickAction() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateQuickActionRequest) => api.createQuickAction(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: quickActionKeys.all(wsId) });
    },
  });
}

export function useUpdateQuickAction() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateQuickActionRequest) =>
      api.updateQuickAction(id, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: quickActionKeys.all(wsId) });
    },
  });
}

export function useDeleteQuickAction() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteQuickAction(id),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: quickActionKeys.all(wsId) });
    },
  });
}

/**
 * Run one quick action against one issue.
 *
 * Returns the created comment plus its `trigger_outcomes`, so the caller can
 * tell `queued` (a run started) from `coalesced` (merged into the target's
 * existing pending task) from `blocked`. That distinction is the whole point:
 * the DB allows only one pending task per (issue, agent), so a second click
 * genuinely does NOT start a second run and the UI must say so rather than
 * pretending it did.
 *
 * Invalidates the issue's comments and task runs so the new card and the
 * execution-log entry appear without waiting for a websocket round-trip.
 */
export function useRunQuickAction(issueId: string) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation<Comment, Error, { quickActionId: string }>({
    mutationFn: ({ quickActionId }) => api.runQuickAction(issueId, quickActionId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: issueKeys.timeline(issueId) });
      qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
      // Usage counters and last_used_at moved server-side on a successful run.
      qc.invalidateQueries({ queryKey: quickActionKeys.all(wsId) });
    },
  });
}
