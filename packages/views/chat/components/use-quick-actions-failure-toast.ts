import { useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  chatKeys,
  chatQuickActionsFailureOptions,
} from "@multica/core/chat/queries";
import type { ChatQuickActionsFailureState } from "@multica/core/types";
import { toast } from "sonner";
import { useT } from "../../i18n";

/**
 * Surfaces a toast when a session's explicit quick-actions refresh fails
 * asynchronously — the HTTP call was accepted but the daemon's regeneration
 * never produced suggestions, so the turn's pills are unchanged (MUL-5149). The
 * failure arrives over chat:quick_actions with failed=true, which
 * applyChatQuickActionsToCache turns into a one-shot client-only signal; this
 * hook consumes it and clears it. The synchronous HTTP-failure path (409 busy,
 * network) already toasts from QuickActions.handleRegenerate, so this covers
 * only the async half — without it, a failed refresh would look identical to a
 * successful one whose suggestions happened not to change.
 *
 * A stable toast id keyed on the failure nonce collapses duplicates when both
 * chat surfaces (floating window + chat tab) are mounted and each runs this
 * hook against the same signal. Mount once per surface that reads the marker.
 */
export function useQuickActionsFailureToast(sessionId: string | null): void {
  const qc = useQueryClient();
  const { t } = useT("chat");
  const { data: failure = null } = useQuery(
    chatQuickActionsFailureOptions(sessionId ?? ""),
  );

  useEffect(() => {
    if (!sessionId || !failure) return;
    toast.error(t(($) => $.message_list.quick_actions_regenerate_failed), {
      id: `qa-refresh-fail:${sessionId}:${failure.at}`,
    });
    // Clear only this exact signal so a newer failure raised in the same tick
    // is not swallowed.
    qc.setQueryData<ChatQuickActionsFailureState | null>(
      chatKeys.quickActionsFailure(sessionId),
      (current) => (current && current.at === failure.at ? null : current),
    );
  }, [qc, t, sessionId, failure]);
}
