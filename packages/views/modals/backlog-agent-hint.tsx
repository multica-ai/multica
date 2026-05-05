"use client";

import { toast } from "sonner";
import { BacklogAgentHintDialog } from "../issues/components/backlog-agent-hint-dialog";
import { useUpdateIssue } from "@multica/core/issues/mutations";
import { useT } from "@multica/i18n/react";

export function BacklogAgentHintModal({
  onClose,
  data,
}: {
  onClose: () => void;
  data: Record<string, unknown> | null;
}) {
  const issueId = (data?.issueId as string) || "";
  const updateIssue = useUpdateIssue();
  const t = useT("issues");

  return (
    <BacklogAgentHintDialog
      open
      onOpenChange={(v) => {
        if (!v) onClose();
      }}
      onDismissPermanently={() => {
        localStorage.setItem("multica:backlog-agent-hint-dismissed", "true");
      }}
      onMoveToTodo={() => {
        if (issueId) {
          updateIssue.mutate(
            { id: issueId, status: "todo" },
            { onError: () => toast.error(t("toast_failed_update_status")) },
          );
        }
        onClose();
      }}
    />
  );
}
