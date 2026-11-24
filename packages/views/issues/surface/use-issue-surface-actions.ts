"use client";

import { useCallback, useMemo, useRef } from "react";
import {
  notifyManager,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import { toast } from "sonner";
import type { UpdateIssueRequest } from "@multica/core/types";
import {
  useBatchDeleteIssues,
  useBatchUpdateIssues,
  useUpdateIssue,
} from "@multica/core/issues/mutations";
import { errorCode } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueKeys } from "@multica/core/issues/queries";
import { useModalStore } from "@multica/core/modals";
import {
  type IssueSurfaceActions,
  type IssueSurfaceMutationOptions,
} from "./actions-context";
import type { IssueCreateDefaults } from "./types";
import { useT } from "../../i18n";

export type MoveIssueUpdates = Pick<
  UpdateIssueRequest,
  | "status"
  | "assignee_type"
  | "assignee_id"
  | "position"
  | "stop_previous_assignee_runs"
  | "parent_issue_id"
  | "project_id"
> & {
  before_id: string | null;
  after_id: string | null;
};

export interface IssueSurfaceActionController {
  actions: IssueSurfaceActions;
  openCreateIssue: (defaults?: IssueCreateDefaults) => void;
  moveIssue: (
    issueId: string,
    updates: MoveIssueUpdates,
    onSettled?: () => void,
  ) => void;
}

/**
 * Resolves once every active table row page that is fetching now has landed a
 * result or stopped. A realtime echo restarts those fetches, and the fetch it
 * replaces rejects, so each page is followed until its own data changes.
 */
async function tableRefetchLanded(qc: QueryClient, wsId: string) {
  const fetching = qc.getQueryCache().findAll({
    queryKey: [...issueKeys.tableAll(wsId), "rows"],
    type: "active",
    fetchStatus: "fetching",
  });
  await Promise.all(
    fetching.map(async (query) => {
      const landed = query.state.dataUpdateCount;
      while (
        query.state.fetchStatus === "fetching" &&
        query.state.dataUpdateCount === landed
      ) {
        await query.fetch(undefined, { cancelRefetch: false }).catch(() => {});
      }
    }),
  );
  // Observers hear about landed rows through this queue; go after them.
  await new Promise<void>((resolve) => notifyManager.schedule(resolve));
}

export function useIssueSurfaceActions({
  createDefaults,
}: {
  createDefaults: IssueCreateDefaults;
}): IssueSurfaceActionController {
  const { t } = useT("projects");
  const { t: tIssues } = useT("issues");
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const lastMoveRef = useRef(0);
  const updateIssueMutation = useUpdateIssue();
  const batchUpdateMutation = useBatchUpdateIssues();
  const batchDeleteMutation = useBatchDeleteIssues();

  const updateIssue = useCallback(
    (
      issueId: string,
      updates: Partial<UpdateIssueRequest>,
      options?: IssueSurfaceMutationOptions,
    ) => {
      // Keep UI callbacks observer-scoped; error feedback belongs to the request.
      void updateIssueMutation
        .mutateAsync(
          { id: issueId, ...updates },
          {
            onSuccess: (issue) => options?.onSuccess?.(issue),
            onError: (err) => options?.onError?.(err),
            onSettled: () => options?.onSettled?.(),
          },
        )
        .catch((err: unknown) => {
          toast.error(
            errorCode(err) === "revision_conflict"
              ? tIssues(($) => $.revision.conflict)
              : err instanceof Error && err.message
              ? err.message
              : (options?.errorMessage ??
                  t(($) => $.detail.toast_move_issue_failed)),
          );
        });
    },
    [t, tIssues, updateIssueMutation],
  );

  const moveIssue = useCallback(
    (
      issueId: string,
      updates: MoveIssueUpdates,
      onSettled?: () => void,
    ) => {
      const { before_id, after_id, ...optimisticUpdates } = updates;
      const move = ++lastMoveRef.current;
      updateIssueMutation.mutate(
        {
          id: issueId,
          ...optimisticUpdates,
          move_intent: { before_id, after_id },
        },
        {
          onError: (err) => {
            toast.error(
              errorCode(err) === "revision_conflict"
                ? tIssues(($) => $.revision.conflict)
                : err instanceof Error && err.message
                ? err.message
                : t(($) => $.detail.toast_move_issue_failed),
            );
          },
          // A moved card has left its old branch's rows as soon as it is
          // patched, and enters the new branch's only when that one refetches.
          // Callers hold their local placement until this fires, so it waits
          // for those rows. A newer move owns the hold from then on.
          onSettled: async (_issue, err) => {
            if (!err) await tableRefetchLanded(qc, wsId);
            if (move === lastMoveRef.current) onSettled?.();
          },
        },
      );
    },
    [qc, t, tIssues, updateIssueMutation, wsId],
  );

  const openCreateIssue = useCallback(
    (defaults?: IssueCreateDefaults) => {
      useModalStore
        .getState()
        .open("create-issue", { ...createDefaults, ...defaults });
    },
    [createDefaults],
  );

  const actions = useMemo<IssueSurfaceActions>(
    () => ({
      isPending:
        updateIssueMutation.isPending ||
        batchUpdateMutation.isPending ||
        batchDeleteMutation.isPending,
      createIssue: openCreateIssue,
      updateIssue,
      moveIssue: (issueId, updates, options) =>
        updateIssue(issueId, updates, {
          errorMessage: t(($) => $.detail.toast_move_issue_failed),
          ...options,
        }),
      batchUpdate: (issueIds, updates, ops) =>
        batchUpdateMutation.mutateAsync({ ids: issueIds, updates, ...ops }),
      batchDelete: async (issueIds) => {
        await batchDeleteMutation.mutateAsync(issueIds);
      },
    }),
    [
      batchDeleteMutation,
      batchUpdateMutation,
      openCreateIssue,
      t,
      updateIssue,
      updateIssueMutation.isPending,
    ],
  );

  return { actions, openCreateIssue, moveIssue };
}
