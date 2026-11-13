"use client";

import { toast } from "sonner";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { IssuePickerModal } from "./issue-picker-modal";
import { useT } from "../i18n";

export function AddProjectIssueModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const projectId = (data?.projectId as string) || "";
  const updateIssue = useUpdateIssue();

  return (
    <IssuePickerModal
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      title={t(($) => $.add_project_issue.title)}
      description={t(($) => $.add_project_issue.description)}
      excludeIds={[]}
      isSelectable={(issue) => issue.project_id !== projectId}
      onSelect={(selected) => {
        updateIssue.mutate(
          { id: selected.id, project_id: projectId },
          {
            onSuccess: () =>
              toast.success(
                t(($) => $.add_project_issue.toast_success, {
                  identifier: selected.identifier,
                }),
              ),
            onError: (err) =>
              toast.error(
                err instanceof Error && err.message
                  ? err.message
                  : t(($) => $.add_project_issue.toast_failed),
              ),
          },
        );
      }}
    />
  );
}
