"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { useCancelIssueRun } from "@multica/core/issues/mutations";
import { useT } from "../../i18n";

/**
 * "Stop and start over": the chosen turns are stopped first, so the message
 * that follows starts a fresh run instead of waiting behind them. Resolves
 * false (and keeps the draft) when a turn could not be stopped.
 */
export function useStopRunsBeforeSend(issueId: string) {
  const { t } = useT("issues");
  const cancel = useCancelIssueRun(issueId);
  const { mutateAsync } = cancel;
  return useCallback(async (taskIds: string[]): Promise<boolean> => {
    if (taskIds.length === 0) return true;
    try {
      await Promise.all(taskIds.map((taskId) => mutateAsync(taskId)));
      return true;
    } catch {
      toast.error(t(($) => $.comment.restart_failed));
      return false;
    }
  }, [mutateAsync, t]);
}
