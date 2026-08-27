"use client";

import { useCallback } from "react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useModalStore } from "@multica/core/modals";

/** Opens the shared issue-limit recovery dialog without closing the current draft. */
export function useIssueLimitUpgradePrompt(): () => void {
  const wsId = useWorkspaceId();

  return useCallback(() => {
    useModalStore.getState().showIssueLimitRecovery(wsId);
  }, [wsId]);
}
