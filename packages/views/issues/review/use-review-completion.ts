"use client";

import { useContext, useRef } from "react";
import { toast } from "sonner";
import { api, errorCode } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { getCurrentWsId } from "@multica/core/platform";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths, issueRouteRef } from "@multica/core/paths";
import type { Issue, UpdateIssueRequest } from "@multica/core/types";
import { resolveStatusCategory } from "../actions/run-confirm-gate";
import { useNavigation } from "../../navigation/context";
import { useT } from "../../i18n";
import { nextReviewIssue, readReviewQueue, ReviewQueueChanged, refreshReviewQuery } from "./queue";
import { REVIEW_PARAM, ReviewDetailContext, ReviewSessionContext, reviewHref } from "./session";

type Write = (input: { id: string } & Partial<UpdateIssueRequest>) => Promise<Issue>;

/** An explicit full-detail action only. Realtime/cache changes never enter here.
 * The ordinary run-confirm gate remains upstream of this hook. */
export function useReviewCompletion() {
  const session = useContext(ReviewSessionContext);
  const detailId = useContext(ReviewDetailContext);
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const workspaceId = useWorkspaceId();
  const userId = useAuthStore((s) => s.user?.id);
  const { t } = useT("issues");
  const latest = useRef({ navigation, workspaceId, userId });
  latest.current = { navigation, workspaceId, userId };

  return (
    issue: Issue,
    updates: Partial<UpdateIssueRequest>,
    category: ReturnType<typeof resolveStatusCategory>,
    write: Write,
  ): boolean => {
    const origin = session?.origin;
    if (
      !session || !origin || detailId !== issue.id || issue.status !== "in_review" ||
      !updates.status || category !== "done" ||
      Object.keys(updates).some((key) => !["status", "stop_previous_assignee_runs", "handoff_note", "suppress_run"].includes(key)) ||
      origin.workspaceId !== workspaceId || origin.userId !== userId ||
      navigation.searchParams.get(REVIEW_PARAM) !== origin.token
    ) return false;
    if (typeof issue.revision !== "number" || !Number.isSafeInteger(issue.revision) || issue.revision < 1) {
      toast.error(t(($) => $.review_queue.changed));
      return true;
    }
    if (session.busy) return true;
    session.busy = true;
    const epoch = session.epoch;
    const owns = () => session.origin === origin && session.epoch === epoch &&
      getCurrentWsId() === origin.workspaceId && useAuthStore.getState().user?.id === origin.userId;
    // Promise callbacks live with the operation/session, not a mutation observer
    // on the detail that navigation is about to unmount.
    void (async () => {
      let committed = false;
      try {
        const before = await readReviewQueue(await refreshReviewQuery(origin.query), (request) => api.listIssueTableRows(request), (request) => api.listIssueTableGroups(request));
        if (!owns()) return;
        if (!before.some((row) => row.id === issue.id)) throw new Error(t(($) => $.review_queue.changed));
        const saved = await write({ id: issue.id, ...updates, expected_revision: issue.revision });
        committed = true;
        if (!owns()) return;
        if (saved.id !== issue.id || saved.status !== updates.status || typeof saved.revision !== "number" || !Number.isSafeInteger(saved.revision) || saved.revision < 1) {
          throw new Error(t(($) => $.review_queue.receipt_failed));
        }
        const receipt = ++session.receipt;
        let lease = session.epoch;
        let undoPending = false;
        const validReceipt = () => session.origin === origin && session.receipt === receipt &&
          session.epoch === lease && getCurrentWsId() === origin.workspaceId &&
          useAuthStore.getState().user?.id === origin.userId;
        if (session.toastId !== undefined) toast.dismiss(session.toastId);
        session.toastId = toast.success(t(($) => $.review_queue.completed), {
          position: "top-center", duration: 10000,
          action: {
            label: t(($) => $.review_queue.undo),
            onClick: () => {
              if (!validReceipt() || undoPending) return;
              undoPending = true;
              // Consume before the network call: late queue reads and old toasts
              // cannot navigate after Undo or after another completion.
              session.receipt++;
              void write({
                id: saved.id, status: issue.status,
                expected_revision: saved.revision, suppress_run: true,
              }).then((restored) => {
                if (restored.id !== saved.id || restored.status !== issue.status) {
                  throw new Error(t(($) => $.review_queue.undo_failed));
                }
                if (session.origin !== origin || session.epoch !== lease ||
                    getCurrentWsId() !== origin.workspaceId || useAuthStore.getState().user?.id !== origin.userId) return;
                const target = reviewHref(paths.issueDetail(issueRouteRef(restored)), origin.token);
                session.preserveNavigation = target;
                latest.current.navigation.replace(target);
              }).catch((error: unknown) => {
                if (session.origin !== origin || session.epoch !== lease ||
                    getCurrentWsId() !== origin.workspaceId || useAuthStore.getState().user?.id !== origin.userId) return;
                toast.error(errorCode(error) === "revision_conflict"
                  ? t(($) => $.revision.conflict)
                  : t(($) => $.review_queue.undo_failed));
              });
            },
          },
        });
        let fresh: Issue[];
        try {
          fresh = await readReviewQueue(await refreshReviewQuery(origin.query), (request) => api.listIssueTableRows(request), (request) => api.listIssueTableGroups(request));
        } catch {
          if (validReceipt()) toast.error(t(($) => $.review_queue.next_failed));
          return;
        }
        if (!validReceipt()) return;
        const next = nextReviewIssue(before, issue.id, fresh);
        const target = next
          ? reviewHref(paths.issueDetail(issueRouteRef(next)), origin.token)
          : origin.listPath;
        session.preserveNavigation = target;
        latest.current.navigation.replace(target);
        lease = session.epoch;
      } catch (error: unknown) {
        if (owns()) toast.error(
          committed ? t(($) => $.review_queue.next_failed) :
          errorCode(error) === "revision_conflict" ? t(($) => $.revision.conflict) :
          error instanceof ReviewQueueChanged ? t(($) => $.review_queue.changed) :
          error instanceof Error ? error.message : t(($) => $.detail.update_failed),
        );
      } finally {
        session.busy = false;
      }
    })();
    return true;
  };
}
