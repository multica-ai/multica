import { useState } from "react";
import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ApiError } from "@multica/core/api";
import { cronPreviewOptions } from "@multica/core/autopilots/queries";
import { useT } from "../../../i18n";
import type { ScheduleConfig } from "./model";
import { toCron } from "./cron-mapping";

/** The server's rejection, split the way the editor is: which input is at fault
 *  (the cron box or the timezone picker), and the parser's own words. */
export interface ScheduleRejection {
  code: "invalid_cron" | "invalid_timezone";
  detail: string;
}

/** Read the server's classification off a preview 400. Anything but an explicit
 *  invalid_timezone tag is the cron's fault. */
export function classifyScheduleRejection(err: ApiError): ScheduleRejection {
  const body = typeof err.body === "object" && err.body !== null ? err.body : {};
  const code = (body as { code?: unknown }).code;
  return {
    code: code === "invalid_timezone" ? "invalid_timezone" : "invalid_cron",
    detail: err.message,
  };
}

/**
 * Ask the server whether it accepts this schedule, and return its rejection
 * (or null when it accepts).
 *
 * The editor's inline error only covers expressions whose preview has already
 * settled: typing an invalid cron and clicking Save immediately commits the
 * draft on blur while the preview request is still in flight, so the submit
 * gate would still be open. Creating the autopilot in that state persists it
 * and then fails to create its trigger, leaving an autopilot that can never
 * run — so submit paths validate once more before writing.
 *
 * Transport failures resolve to null: an unreachable preview endpoint must not
 * block saving a schedule the server would have accepted.
 */
export async function findScheduleRejection(
  queryClient: QueryClient,
  wsId: string,
  config: ScheduleConfig,
): Promise<ScheduleRejection | null> {
  try {
    await queryClient.fetchQuery(cronPreviewOptions(wsId, toCron(config), config.timezone));
    return null;
  } catch (err) {
    if (!(err instanceof ApiError) || err.status !== 400) return null;
    return classifyScheduleRejection(err);
  }
}

/** Announce a rejection the way both submit paths must: the headline names the
 *  control at fault, because the cron and the timezone are fixed in different
 *  ones, and the parser's own words go underneath — untranslated, but the only
 *  text that says what is actually wrong. */
export function toastScheduleRejection(
  t: ReturnType<typeof useT<"autopilots">>["t"],
  rejection: ScheduleRejection,
): void {
  toast.error(
    rejection.code === "invalid_timezone"
      ? t(($) => $.schedule_editor.timezone_invalid)
      : t(($) => $.schedule_editor.cron_invalid),
    { description: rejection.detail },
  );
}

/**
 * The submit gate every form that writes a schedule needs, in one place: a
 * schedule the server has rejected must not be submittable, or the form
 * persists an autopilot whose trigger then 400s and never runs.
 *
 * - `scheduleValid` gates the submit button, fed by the editor's
 *   `onValidityChange` and by `ensureAccepted`.
 * - `clearRejection` belongs in the schedule's `onChange`: a submit-time
 *   rejection is the gate's, not the editor's — the editor's own preview may
 *   never query that expression if the user edits again before the debounce
 *   fires, and then nothing would re-enable Save. Any edit lifts it; the
 *   editor's inline error re-disables Save if the new expression is bad too.
 * - `ensureAccepted` runs the pre-write check (see findScheduleRejection) and
 *   toasts the rejection itself; the caller only unwinds its own submit state.
 *
 * Mount the owning form only while it is open: the gate's state lives with it,
 * and a form kept mounted closed would carry a stale rejection into its next
 * opening.
 */
export function useScheduleSubmitGate(wsId: string): {
  scheduleValid: boolean;
  clearRejection: () => void;
  onValidityChange: (valid: boolean) => void;
  ensureAccepted: (config: ScheduleConfig) => Promise<boolean>;
} {
  const { t } = useT("autopilots");
  const queryClient = useQueryClient();
  const [scheduleValid, setScheduleValid] = useState(true);
  return {
    scheduleValid,
    clearRejection: () => setScheduleValid(true),
    onValidityChange: setScheduleValid,
    ensureAccepted: async (config: ScheduleConfig): Promise<boolean> => {
      const rejection = await findScheduleRejection(queryClient, wsId, config);
      if (rejection === null) return true;
      setScheduleValid(false);
      toastScheduleRejection(t, rejection);
      return false;
    },
  };
}
