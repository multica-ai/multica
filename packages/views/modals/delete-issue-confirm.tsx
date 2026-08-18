"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useWorkspaceId } from "@multica/core/hooks";
import { useDeleteIssue } from "@multica/core/issues/mutations";
import { childIssueProgressOptions } from "@multica/core/issues/queries";
import { useBackOrReplace } from "../navigation";
import { useT } from "../i18n";

export function DeleteIssueConfirmModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const wsId = useWorkspaceId();
  const issueId = (data?.issueId as string) || "";
  const identifier = (data?.identifier as string | null | undefined) || null;
  // Set only by callers that are rendering the issue we are about to delete
  // (the detail page). List surfaces leave it undefined and simply stay put.
  const fallbackPath = (data?.onDeletedFallbackPath as string | undefined) || undefined;
  const [deleting, setDeleting] = useState(false);
  const deleteIssue = useDeleteIssue();
  const backOrReplace = useBackOrReplace();

  // Deleting a parent does NOT delete its sub-issues — the server clears their
  // parent link and they become standalone issues. Warn about that, but only
  // from what the cache already knows: `enabled: false` reads the workspace
  // child-progress map without firing a request, so the warning never lands
  // late and shifts this dialog's buttons under the user's cursor. Every
  // surface that can open this dialog (issue list, board, detail) already
  // renders sub-issue progress, so the map is warm there; anywhere else the
  // dialog simply degrades to the generic copy rather than claiming zero.
  const { data: childProgress } = useQuery({
    ...childIssueProgressOptions(wsId),
    enabled: false,
  });
  const childCount = (issueId && childProgress?.get(issueId)?.total) || 0;

  const handleDelete = async () => {
    if (!issueId || deleting) return;
    setDeleting(true);
    try {
      await deleteIssue.mutateAsync(issueId);
      toast.success(t(($) => $.delete_issue.toast_deleted));
      onClose();
      // Back to whichever list the user opened this issue from; `fallbackPath`
      // only kicks in when there is no in-app history to step back into.
      if (fallbackPath) backOrReplace(fallbackPath);
    } catch (err) {
      toast.error(
        err instanceof Error && err.message
          ? err.message
          : t(($) => $.delete_issue.toast_delete_failed),
      );
      setDeleting(false);
    }
  };

  return (
    <AlertDialog open onOpenChange={(v) => { if (!v && !deleting) onClose(); }}>
      <AlertDialogContent>
        <AlertDialogHeader>
          {/* The identifier belongs in the title, not repeated in the body: this
              dialog is opened from row context menus as often as from the
              issue's own page, so it has to name its subject — but naming it in
              the heading costs no extra line of prose. */}
          <AlertDialogTitle>
            {identifier
              ? t(($) => $.delete_issue.title_named, { identifier })
              : t(($) => $.delete_issue.title)}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.delete_issue.description)}
            {childCount > 0 && (
              <span className="mt-2 block">
                {t(($) => $.delete_issue.sub_issues_detached, { count: childCount })}
              </span>
            )}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={deleting}>{t(($) => $.delete_issue.cancel)}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={handleDelete}
            disabled={deleting}
          >
            {deleting ? t(($) => $.delete_issue.deleting) : t(($) => $.delete_issue.confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
