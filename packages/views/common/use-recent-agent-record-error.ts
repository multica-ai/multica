"use client";

import { useCallback } from "react";
import type { RecentAgentRecordErrorHandler } from "@multica/core/agents";
import { toast } from "sonner";
import { useT } from "../i18n";

export function useRecentAgentRecordError(): RecentAgentRecordErrorHandler {
  const { t } = useT("common");

  return useCallback(
    (_error, retry) => {
      toast.error(t(($) => $.agents.recent.sync_failed), {
        action: {
          label: t(($) => $.agents.recent.retry),
          onClick: retry,
        },
      });
    },
    [t],
  );
}
