"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { errorCode } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDuplicatesOptions } from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { IssuePickerModal } from "./issue-picker-modal";
import { useT } from "../i18n";

/**
 * Marks an issue as a duplicate of the picked one (MUL-7349). A duplicate is a
 * cancelled issue that remembers its original, so the write also cancels it.
 */
export function MarkDuplicateIssueModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const issueId = (data?.issueId as string) || "";
  const wsId = useWorkspaceId();
  const updateIssue = useUpdateIssue();

  // An issue's own duplicates can never be its original.
  const { data: relations } = useQuery({
    ...issueDuplicatesOptions(wsId, issueId),
    enabled: !!issueId,
  });
  const excludeIds = [issueId, ...(relations?.duplicates.map((d) => d.id) ?? [])];

  return (
    <IssuePickerModal
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      title={t(($) => $.mark_duplicate.title)}
      description={t(($) => $.mark_duplicate.description)}
      excludeIds={excludeIds}
      onSelect={(selected) => {
        updateIssue.mutate(
          {
            id: issueId,
            status: "cancelled",
            duplicate_of_issue_id: selected.id,
          },
          {
            onSuccess: () =>
              toast.success(
                t(($) => $.mark_duplicate.toast_success, {
                  identifier: selected.identifier,
                }),
              ),
            onError: (err) => {
              switch (errorCode(err)) {
                case "duplicate_target_is_duplicate":
                  toast.error(
                    t(($) => $.mark_duplicate.error_target_is_duplicate, {
                      identifier: selected.identifier,
                    }),
                  );
                  return;
                case "issue_has_duplicates":
                  toast.error(t(($) => $.mark_duplicate.error_has_duplicates));
                  return;
                default:
                  toast.error(
                    err instanceof Error && err.message
                      ? err.message
                      : t(($) => $.mark_duplicate.toast_failed),
                  );
              }
            },
          },
        );
      }}
    />
  );
}
