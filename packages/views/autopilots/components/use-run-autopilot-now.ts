"use client";

import { useCallback } from "react";
import { toast } from "sonner";
import { clientErrorMessage, dispatchReasonCode } from "@multica/core/api";
import { useTriggerAutopilot } from "@multica/core/autopilots/mutations";
import { useT } from "../../i18n";
import { runNowBlockedKey, runNowToastKind } from "./run-now-toast";

/**
 * Manual "run now" with its outcome toast. Shared by the autopilot detail page
 * and the command bar so both report the same result for the same run.
 */
export function useRunAutopilotNow() {
  const { t } = useT("autopilots");
  const triggerAutopilot = useTriggerAutopilot();

  const runNow = useCallback(
    async (autopilotId: string) => {
      try {
        const run = await triggerAutopilot.mutateAsync(autopilotId);
        // Manual "run now" returns 200 even when admission blocks the run, so the
        // toast is driven by the run's domain status, not the HTTP 2xx (MUL-4525).
        // Success is a whitelist (issue_created/running) — a skipped run warns, a
        // failed or unknown/future status errors — never a false "triggered".
        const kind = runNowToastKind(run?.status);
        if (kind === "success") {
          toast.success(t(($) => $.detail.toast_triggered));
          return;
        }
        // reason_code is the stable, typed cause the server decided at admission
        // time; an unknown/absent code degrades to a generic "not triggered".
        const message = t(($) => $.detail[runNowBlockedKey(run?.reason_code)]);
        if (kind === "warning") {
          toast.warning(message);
        } else {
          toast.error(message);
        }
      } catch (e: unknown) {
        const reason = dispatchReasonCode(e);
        if (reason) {
          toast.error(t(($) => $.detail[runNowBlockedKey(reason)]));
          return;
        }
        // Only a 4xx message is written for the user; a 5xx one is internal
        // server detail (MUL-6472), so an unclassified dispatch failure shows the
        // localized generic sentence instead of the raw body.
        toast.error(clientErrorMessage(e) || t(($) => $.detail.toast_trigger_failed));
      }
    },
    [triggerAutopilot, t],
  );

  return { runNow, isPending: triggerAutopilot.isPending };
}
