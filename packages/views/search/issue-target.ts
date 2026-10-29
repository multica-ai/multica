"use client";

import { useCallback, useMemo } from "react";
import { toast } from "sonner";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { errorCode } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useIssueStatuses } from "@multica/core/issue-statuses/hooks";
import { useBatchUpdateIssues, useUpdateIssue } from "@multica/core/issues/mutations";
import { useModalStore } from "@multica/core/modals";
import { runConfirmIntent } from "../issues/actions/run-confirm-gate";
import { useT } from "../i18n";
import type { IssueTarget } from "./types";

export function targetIssues(target: IssueTarget): Issue[] {
  return target.kind === "single" ? [target.issue] : target.issues;
}

/**
 * Field writes for an issue command, routed the way the matching on-page
 * control routes them: a single issue passes the run-confirm gate that asks
 * before handing work to an agent; a selection follows the batch toolbar's
 * rules.
 *
 * The palette closes, and unmounts this hook, before a write settles, so
 * outcomes are handled on the returned promise: per-call `mutate` callbacks
 * would be dropped with the component and a rejected write would roll back
 * silently.
 */
export function useIssueTargetWriter(target: IssueTarget) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const { entryOf } = useIssueStatuses(wsId);
  const single = target.kind === "single" ? target.issue : null;
  const updateIssue = useUpdateIssue();
  const batchUpdate = useBatchUpdateIssues();
  const openModal = useModalStore((s) => s.open);
  const issues = useMemo(() => targetIssues(target), [target]);

  const applyBatch = useCallback(
    async (updates: Partial<UpdateIssueRequest>) => {
      if (target.kind !== "batch") return;
      const ids = issues.map((issue) => issue.id);
      const count = ids.length;
      try {
        const { updated } = await batchUpdate.mutateAsync({ ids, updates });
        if (updated < count) {
          toast.warning(t(($) => $.batch.update_partial, { updated, count }));
        } else {
          toast.success(t(($) => $.batch.update_success, { count }));
        }
      } catch (err) {
        toast.error(
          err instanceof Error && err.message ? err.message : t(($) => $.batch.update_failed),
        );
      }
    },
    [batchUpdate, issues, t, target.kind],
  );

  const applySingle = useCallback(
    (issue: Issue, updates: Partial<UpdateIssueRequest>) => {
      const intent = runConfirmIntent(issue, updates, { entryOf });
      if (intent) {
        openModal("issue-run-confirm", intent);
        return;
      }
      updateIssue.mutateAsync({ id: issue.id, ...updates }).catch((err: unknown) => {
        toast.error(
          errorCode(err) === "revision_conflict"
            ? t(($) => $.revision.conflict)
            : err instanceof Error && err.message
              ? err.message
              : t(($) => $.detail.update_failed),
        );
      });
    },
    [entryOf, openModal, t, updateIssue],
  );

  const apply = useCallback(
    (updates: Partial<UpdateIssueRequest>) => {
      if (single) {
        applySingle(single, updates);
        return;
      }
      void applyBatch(updates);
    },
    [applyBatch, applySingle, single],
  );

  const assign = useCallback(
    (assigneeType: "member" | "agent" | "squad" | null, assigneeId: string | null) => {
      const updates: Partial<UpdateIssueRequest> = {
        assignee_type: assigneeType,
        assignee_id: assigneeId,
      };
      if (target.kind === "batch" && (assigneeType === "agent" || assigneeType === "squad") && assigneeId) {
        // Same rule as the batch toolbar: assigning agent work across a
        // selection previews the run fan-out, unless everything is parked in
        // Backlog and nothing would start.
        const allBacklog = issues.every((issue) => issue.status === "backlog");
        if (!allBacklog) {
          openModal("issue-run-confirm", {
            issueIds: issues.map((issue) => issue.id),
            mode: "assign",
            assigneeType,
            assigneeId,
          });
          return;
        }
      }
      apply(updates);
    },
    [apply, issues, openModal, target.kind],
  );

  return { issues, apply, assign };
}
