"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { workspaceSubscriptionSummaryOptions } from "@multica/core/billing";
import {
  autopilotQuotaAdminAsk,
  resolveBillingRecovery,
} from "@multica/core/billing/recovery";
import { useFeatureEnabled } from "@multica/core/config";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import type { InboxItem } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { AutopilotAddonDialog } from "../../billing/autopilot-addon-dialog";
import { useT } from "../../i18n";

function formatResetAt(value: string | undefined): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}

/**
 * The workspace ran out of autopilot runs. The recovery action follows
 * Cloud's billing actions for this reader: upgrade from Free, buy more runs on
 * a paid plan, or, for a member without billing access, who to ask.
 */
export function AutopilotQuotaNotice({
  item,
  onOpenRecovery,
}: {
  item: InboxItem;
  onOpenRecovery: () => void;
}) {
  const { t } = useT("inbox");
  const wsId = item.workspace_id;
  const billingEnabled = useFeatureEnabled(
    BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
    false,
  );
  const summaryQuery = useQuery({
    ...workspaceSubscriptionSummaryOptions(wsId),
    enabled: billingEnabled && wsId.length > 0,
  });
  const [addonOpen, setAddonOpen] = useState(false);
  const details = item.details ?? {};
  const resetAt = formatResetAt(details.reset_at);

  let body = item.body;
  if (details.limit && resetAt) {
    body = t(($) => $.detail.autopilot_quota_exceeded_body, {
      limit: details.limit,
      resetAt,
    });
  }

  const summary = summaryQuery.data;
  const recovery = resolveBillingRecovery({
    actions: summary?.availableActions,
    billingEnabled,
    loading: billingEnabled && !summary && summaryQuery.isFetching,
    autopilotAddon: true,
  });
  const adminAsk = autopilotQuotaAdminAsk(summary);

  let action: { label: string; onClick: () => void } | null = null;
  let note: string | null = null;
  switch (recovery) {
    case "checkout":
      action = { label: t(($) => $.detail.autopilot_quota_upgrade_action), onClick: onOpenRecovery };
      break;
    case "autopilot_addon":
      action = { label: t(($) => $.detail.autopilot_quota_addon_action), onClick: () => setAddonOpen(true) };
      break;
    case "portal":
      action = { label: t(($) => $.detail.autopilot_quota_portal_action), onClick: onOpenRecovery };
      break;
    case "contact_admin":
      note =
        adminAsk === "addon"
          ? t(($) => $.detail.autopilot_quota_contact_addon)
          : adminAsk === "addon_or_upgrade"
            ? t(($) => $.detail.autopilot_quota_contact_addon_or_upgrade)
            : t(($) => $.detail.autopilot_quota_contact_upgrade);
      break;
    default:
      action = { label: t(($) => $.detail.autopilot_quota_upgrade), onClick: onOpenRecovery };
  }

  return (
    <>
      {body && (
        <div className="mt-4 whitespace-pre-wrap text-body leading-relaxed text-foreground">
          {body}
        </div>
      )}
      {note ? (
        <p className="mt-4 text-body text-muted-foreground">{note}</p>
      ) : null}
      {action ? (
        <Button className="mt-4" size="sm" onClick={action.onClick}>
          {action.label}
        </Button>
      ) : null}
      {addonOpen ? (
        <AutopilotAddonDialog wsId={wsId} open={addonOpen} onOpenChange={setAddonOpen} />
      ) : null}
    </>
  );
}
