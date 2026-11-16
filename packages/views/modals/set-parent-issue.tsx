"use client";

import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  childIssuesOptions,
} from "@multica/core/issues/queries";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { IssuePickerModal } from "./issue-picker-modal";
import { useT } from "../i18n";

export function SetParentIssueModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const { t } = useT("modals");
  const issueId = (data?.issueId as string) || "";
  const wsId = useWorkspaceId();
  const { mutateAsync: updateIssue } = useUpdateIssue();

  const { data: children = [] } = useQuery({
    ...childIssuesOptions(wsId, issueId),
    enabled: !!issueId,
  });

  const excludeIds = [issueId, ...children.map((c) => c.id)];

  // The picker closes as soon as an issue is picked, unmounting this modal, so
  // the toasts follow the request's promise rather than per-call callbacks.

  return (
    <IssuePickerModal
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      title={t(($) => $.set_parent.title)}
      description={t(($) => $.set_parent.description)}
      excludeIds={excludeIds}
      onSelect={(selected) => {
        updateIssue({
          id: issueId,
          parent_issue_id: selected.id,
        }).then(
          () =>
            toast.success(
              t(($) => $.set_parent.toast_success, {
                identifier: selected.identifier,
              }),
            ),
          (err) =>
            toast.error(
              err instanceof Error && err.message
                ? err.message
                : t(($) => $.set_parent.toast_failed),
            ),
        );
      }}
    />
  );
}
