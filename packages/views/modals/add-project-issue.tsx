"use client";

import { toast } from "sonner";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { IssuePickerModal } from "./issue-picker-modal";
import { useT } from "../i18n";

export function AddProjectIssueModal({
  open = true,
  onClose,
  onOpenChangeComplete,
  data,
}: {
  open?: boolean;
  onClose: () => void;
  onOpenChangeComplete?: (open: boolean) => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const projectId = (data?.projectId as string) || "";
  const { mutateAsync: updateIssue } = useUpdateIssue();

  // The picker closes as soon as an issue is picked, unmounting this modal, so
  // the toasts follow the request's promise rather than per-call callbacks.
  return (
    <IssuePickerModal
      open={open}
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      onOpenChangeComplete={onOpenChangeComplete}
      title={t(($) => $.add_project_issue.title)}
      description={t(($) => $.add_project_issue.description)}
      excludeIds={[]}
      isSelectable={(issue) => issue.project_id !== projectId}
      onSelect={(selected) => {
        updateIssue({ id: selected.id, project_id: projectId }).then(
          () =>
            toast.success(
              t(($) => $.add_project_issue.toast_success, {
                identifier: selected.identifier,
              }),
            ),
          (err) =>
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : t(($) => $.add_project_issue.toast_failed),
            ),
        );
      }}
    />
  );
}
