"use client";

import { useCallback } from "react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  useModalStore,
  type IssueLimitRecoveryReason,
} from "@multica/core/modals";

/** Opens the shared limit-recovery dialog without closing the current surface. */
export function useIssueLimitUpgradePrompt(
  reason: IssueLimitRecoveryReason = "issue_limit",
): () => void {
  const wsId = useWorkspaceId();

  return useCallback(() => {
    useModalStore.getState().showIssueLimitRecovery(wsId, reason);
  }, [reason, wsId]);
}
