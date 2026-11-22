"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertCircle, CheckCircle2, Loader2 } from "@multica/ui/icons";
import {
  invalidateAfterAutopilotAddon,
  workspaceAutopilotAddonCheckoutOptions,
} from "@multica/core/billing";
import { Alert, AlertDescription, AlertTitle } from "@multica/ui/components/ui/alert";
import { useLocale, useT } from "../i18n";

const POLL_INTERVAL_MS = 2_000;
const POLL_TIMEOUT_MS = 60_000;
// Stripe Checkout Session IDs: `cs_` plus letters, digits and underscores.
const SESSION_ID_RE = /^cs_[A-Za-z0-9_]+$/;

/**
 * Billing banner after the payer returns from an add-on Stripe Checkout.
 * Reading the session is what confirms it: Cloud records a paid session
 * before answering, and Multica then makes every API instance read the raised
 * limit, so the next autopilot run is admitted. While Stripe is still
 * finishing, the session is read again for a short while.
 */
export function AutopilotAddonReturnNotice({
  wsId,
  sessionId: rawSessionId,
}: {
  wsId: string;
  sessionId: string | null;
}) {
  const sessionId = rawSessionId && SESSION_ID_RE.test(rawSessionId) ? rawSessionId : null;
  const { t } = useT("billing");
  const locale = useLocale();
  const queryClient = useQueryClient();
  const [startedAt] = useState(() => Date.now());
  const sessionQuery = useQuery({
    ...workspaceAutopilotAddonCheckoutOptions(wsId, sessionId ?? ""),
    refetchInterval: (query) =>
      query.state.data?.status === "open" && Date.now() - startedAt < POLL_TIMEOUT_MS
        ? POLL_INTERVAL_MS
        : false,
  });
  const status = sessionQuery.data?.status;
  const invalidatedRef = useRef(false);

  useEffect(() => {
    if (status !== "paid" || invalidatedRef.current) return;
    invalidatedRef.current = true;
    invalidateAfterAutopilotAddon(queryClient, wsId);
  }, [queryClient, status, wsId]);

  if (status === "paid" && sessionQuery.data) {
    return (
      <Alert>
        <CheckCircle2 />
        <AlertTitle>{t(($) => $.autopilot_addon.success_title)}</AlertTitle>
        <AlertDescription>
          {t(($) => $.autopilot_addon.success_description, {
            runs: new Intl.NumberFormat(locale).format(sessionQuery.data.runs),
          })}
        </AlertDescription>
      </Alert>
    );
  }
  if (status === "expired" || status === "unpaid") {
    return (
      <Alert>
        <AlertCircle />
        <AlertTitle>{t(($) => $.autopilot_addon.not_paid_title)}</AlertTitle>
        <AlertDescription>{t(($) => $.autopilot_addon.not_paid_description)}</AlertDescription>
      </Alert>
    );
  }
  // No session to read (a sign-in detour drops it), a read that failed, or a
  // payment Stripe has not finished within the wait: the webhook still adds
  // the runs, so point at the usage instead of claiming an outcome.
  const waiting = sessionId !== null && !sessionQuery.isError &&
    (sessionQuery.isPending || (status === "open" && Date.now() - startedAt < POLL_TIMEOUT_MS));
  return (
    <Alert>
      {waiting ? <Loader2 className="animate-spin motion-reduce:animate-none" /> : <AlertCircle />}
      <AlertTitle>{t(($) => $.autopilot_addon.confirming_title)}</AlertTitle>
      <AlertDescription>{t(($) => $.autopilot_addon.confirming_description)}</AlertDescription>
    </Alert>
  );
}
