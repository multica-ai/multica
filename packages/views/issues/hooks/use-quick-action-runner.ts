"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { dispatchReasonCode } from "@multica/core/api";
import { useRunQuickAction } from "@multica/core/quick-actions";
import type { QuickAction } from "@multica/core/types";
import { outcomeMessage } from "../components/quick-actions-section";
import { useT } from "../../i18n";

/**
 * Runs a quick action from a surface that closes on pick — a menu or the
 * command palette — and reports the outcome as a toast.
 *
 * The surface unmounts before the request settles, so per-call mutate
 * callbacks would never fire; the result is read off `mutateAsync` instead.
 * A permission refusal is a toast here too: the sidebar's explanatory dialog
 * would have nowhere to mount once the menu is gone.
 */
export function useQuickActionRunner(issueId: string) {
  const { t } = useT("issues");
  const { mutateAsync } = useRunQuickAction(issueId);

  return useCallback(
    (action: QuickAction) => {
      const targetName = action.target_name || t(($) => $.detail.quick_action_target_fallback);
      mutateAsync({ quickActionId: action.id }).then(
        (comment) => {
          const { message, kind } = outcomeMessage(comment.trigger_outcomes?.[0], targetName, t);
          if (kind === "success") toast.success(message);
          else if (kind === "error") toast.error(message);
          else toast.info(message);
        },
        (error: unknown) => {
          if (dispatchReasonCode(error) === "invocation_not_allowed") {
            toast.error(t(($) => $.detail.quick_action_blocked_body, { name: action.name }));
            return;
          }
          toast.error(error instanceof Error ? error.message : String(error));
        },
      );
    },
    [mutateAsync, t],
  );
}
