"use client";

import { useCallback } from "react";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  useModalStore,
  type LimitRecoveryReason,
} from "@multica/core/modals";

/** Opens the shared limit-recovery dialog without closing the current surface. */
export function useLimitRecoveryPrompt(
  reason: LimitRecoveryReason,
): () => void {
  const wsId = useWorkspaceId();

  return useCallback(() => {
    useModalStore.getState().showLimitRecovery(wsId, reason);
  }, [reason, wsId]);
}
