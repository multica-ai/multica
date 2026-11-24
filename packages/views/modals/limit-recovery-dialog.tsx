"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Gauge, Loader2 } from "@multica/ui/icons";
import {
  useCreateWorkspaceSubscriptionPortal,
  workspaceSubscriptionSummaryOptions,
} from "@multica/core/billing";
import {
  autopilotQuotaAdminAsk,
  canUpgradeToBusinessFromAddon,
  resolveBillingRecovery,
  type AutopilotQuotaAdminAsk,
} from "@multica/core/billing/recovery";
import { useFeatureEnabled } from "@multica/core/config";
import { BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG } from "@multica/core/feature-flags";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  useModalStore,
  type LimitRecoveryReason,
} from "@multica/core/modals";
import { useWorkspacePaths } from "@multica/core/paths";
import type { WorkspaceSubscriptionSummary } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { AutopilotAddonDialog } from "../billing/autopilot-addon-dialog";
import { useT } from "../i18n";
import { useNavigation } from "../navigation";
import { openExternal } from "../platform";

type ModalsT = ReturnType<typeof useT<"modals">>["t"];
type BillingActions = WorkspaceSubscriptionSummary["availableActions"];

function createPortalIdempotencyKey(wsId: string): string {
  const suffix =
    globalThis.crypto?.randomUUID?.() ??
    `${Date.now()}-${Math.random().toString(36).slice(2)}`;
  return `limit-recovery-portal-${wsId}-${suffix}`.slice(0, 255);
}

/**
 * Centered recovery surface for Cloud-enforced limits, opened from quota
 * notices in Inbox. The modal reason changes copy, never authorization.
 * Cloud's complete action set is the only input that decides which billing
 * action appears; no local role, plan, subscription, or quota inference does.
 */
export function LimitRecoveryDialog() {
  const recoveryWorkspaceId = useModalStore(
    (state) => state.limitRecoveryWorkspaceId,
  );
  const dismiss = useModalStore((state) => state.dismissLimitRecovery);
  const recoveryReason = useModalStore(
    (state) => state.limitRecoveryReason,
  );
  const { t } = useT("modals");
  const wsId = useWorkspaceId();
  const visible = recoveryWorkspaceId === wsId;
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const billingEnabled = useFeatureEnabled(
    BILLING_WORKSPACE_SUBSCRIPTIONS_FLAG,
    false,
  );
  const summaryQuery = useQuery({
    ...workspaceSubscriptionSummaryOptions(wsId),
    enabled: visible && billingEnabled,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
  const createPortal = useCreateWorkspaceSubscriptionPortal(wsId).mutateAsync;
  const portalIntentKeyRef = useRef<string | null>(null);
  const [openingPortal, setOpeningPortal] = useState(false);
  const [portalFailed, setPortalFailed] = useState(false);
  const [addonOpen, setAddonOpen] = useState(false);

  useEffect(() => {
    if (recoveryWorkspaceId !== null && recoveryWorkspaceId !== wsId) {
      dismiss();
    }
  }, [dismiss, recoveryWorkspaceId, wsId]);

  useEffect(() => {
    portalIntentKeyRef.current = null;
    setOpeningPortal(false);
    setPortalFailed(false);
  }, [visible, wsId]);

  const openBilling = () => {
    dismiss();
    navigation.push(`${paths.settings()}?tab=billing`);
  };
  const openPortal = async () => {
    const key =
      portalIntentKeyRef.current ?? createPortalIdempotencyKey(wsId);
    portalIntentKeyRef.current = key;
    setOpeningPortal(true);
    try {
      const response = await createPortal(key);
      if (!response?.url) {
        setPortalFailed(true);
        return;
      }
      portalIntentKeyRef.current = null;
      dismiss();
      openExternal(response.url, { webTarget: "same-tab" });
    } catch {
      setPortalFailed(true);
    } finally {
      setOpeningPortal(false);
    }
  };

  const openAddon = () => {
    dismiss();
    setAddonOpen(true);
  };

  const summary = summaryQuery.data;
  const actions = summary?.availableActions;
  const copy = getRecoveryCopy(t, recoveryReason);
  const recovery = resolveRecoveryPresentation({
    actions,
    billingEnabled,
    loading: billingEnabled && !actions && summaryQuery.isFetching,
    portalFailed,
    autopilotAddon: recoveryReason === "autopilot_quota",
    adminAsk: autopilotQuotaAdminAsk(summary),
    upgradeToBusiness: canUpgradeToBusinessFromAddon(summary),
    copy,
    openBilling,
    openPortal,
    openAddon,
  });

  return (
    <>
    <Dialog
      open={visible}
      modal
      onOpenChange={(open) => {
        if (!open) dismiss();
      }}
    >
      <DialogContent
        className="gap-0 overflow-hidden p-0 sm:max-w-lg"
        showCloseButton={false}
        onKeyDown={(event) => {
          if (event.key !== "Escape") return;
          event.preventDefault();
          event.stopPropagation();
          dismiss();
        }}
      >
        <div className="px-6 pb-7 pt-8 text-center sm:px-10 sm:pb-8 sm:pt-10">
          <div className="mx-auto flex size-14 items-center justify-center rounded-2xl bg-primary/10 text-primary ring-1 ring-primary/15">
            <Gauge className="size-7" aria-hidden="true" />
          </div>
          <DialogHeader className="mt-5 items-center gap-3 text-center">
            <DialogTitle className="max-w-md text-balance text-display-sm font-semibold leading-tight tracking-tight">
              {copy.title}
            </DialogTitle>
            <DialogDescription className="max-w-sm text-pretty text-center text-body leading-6">
              {recovery.description}
            </DialogDescription>
          </DialogHeader>
        </div>

        <DialogFooter className="m-0 px-6 py-4 sm:justify-center sm:px-10">
          <Button
            variant={recovery.action ? "outline" : "default"}
            size="lg"
            className="sm:min-w-28"
            onClick={dismiss}
          >
            {t(($) => $.common.close)}
          </Button>
          {recovery.secondaryAction && (
            <Button
              variant="outline"
              size="lg"
              className="sm:min-w-28"
              onClick={recovery.secondaryAction.onClick}
            >
              {recovery.secondaryAction.label}
            </Button>
          )}
          {recovery.action && (
            <Button
              size="lg"
              className="sm:min-w-40"
              disabled={openingPortal}
              onClick={recovery.action.onClick}
            >
              {openingPortal && <Loader2 className="animate-spin" />}
              {recovery.action.label}
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
    {addonOpen ? (
      <AutopilotAddonDialog wsId={wsId} open={addonOpen} onOpenChange={setAddonOpen} />
    ) : null}
    </>
  );
}

interface RecoveryPresentation {
  description: string;
  action?: {
    label: string;
    onClick: () => void;
  };
  secondaryAction?: {
    label: string;
    onClick: () => void;
  };
}

interface RecoveryCopy {
  title: string;
  billingDisabledDescription: string;
  upgradeDescription: string;
  portalDescription: string;
  contactDescription: string;
  contactAddonDescription: string;
  contactAddonOrUpgradeDescription: string;
  addonDescription: string;
  billingDescription: string;
  billingUnavailableDescription: string;
  checkingDescription: string;
  upgradeAction: string;
  portalAction: string;
  billingAction: string;
  addonAction: string;
  upgradeBusinessAction: string;
}

function getRecoveryCopy(
  t: ModalsT,
  reason: LimitRecoveryReason,
): RecoveryCopy {
  switch (reason) {
    case "autopilot_quota":
      return {
        title: t(($) => $.create_issue.autopilot_quota_limit.title),
        billingDisabledDescription: t(
          ($) =>
            $.create_issue.autopilot_quota_limit.billing_disabled_description,
        ),
        upgradeDescription: t(
          ($) => $.create_issue.autopilot_quota_limit.upgrade_description,
        ),
        portalDescription: t(
          ($) => $.create_issue.autopilot_quota_limit.portal_description,
        ),
        contactDescription: t(
          ($) => $.create_issue.autopilot_quota_limit.contact_description,
        ),
        contactAddonDescription: t(
          ($) => $.create_issue.autopilot_quota_limit.contact_addon_description,
        ),
        contactAddonOrUpgradeDescription: t(
          ($) =>
            $.create_issue.autopilot_quota_limit
              .contact_addon_or_upgrade_description,
        ),
        addonDescription: t(
          ($) => $.create_issue.autopilot_quota_limit.addon_description,
        ),
        billingDescription: t(
          ($) => $.create_issue.autopilot_quota_limit.billing_description,
        ),
        billingUnavailableDescription: t(
          ($) =>
            $.create_issue.autopilot_quota_limit.billing_unavailable_description,
        ),
        checkingDescription: t(
          ($) => $.create_issue.autopilot_quota_limit.checking_description,
        ),
        upgradeAction: t(
          ($) => $.create_issue.autopilot_quota_limit.upgrade_action,
        ),
        portalAction: t(
          ($) => $.create_issue.autopilot_quota_limit.portal_action,
        ),
        billingAction: t(
          ($) => $.create_issue.autopilot_quota_limit.billing_action,
        ),
        addonAction: t(
          ($) => $.create_issue.autopilot_quota_limit.addon_action,
        ),
        upgradeBusinessAction: t(
          ($) => $.create_issue.autopilot_quota_limit.upgrade_business_action,
        ),
      };
  }
}

function resolveRecoveryPresentation({
  actions,
  billingEnabled,
  loading,
  portalFailed,
  autopilotAddon,
  adminAsk,
  upgradeToBusiness,
  copy,
  openBilling,
  openPortal,
  openAddon,
}: {
  actions: BillingActions | undefined;
  billingEnabled: boolean;
  loading: boolean;
  portalFailed: boolean;
  autopilotAddon: boolean;
  adminAsk: AutopilotQuotaAdminAsk;
  upgradeToBusiness: boolean;
  copy: RecoveryCopy;
  openBilling: () => void;
  openPortal: () => Promise<void>;
  openAddon: () => void;
}): RecoveryPresentation {
  const recovery = resolveBillingRecovery({
    actions,
    billingEnabled,
    loading,
    portalFailed,
    autopilotAddon,
  });

  switch (recovery) {
    case "billing_disabled":
      return {
        description: copy.billingDisabledDescription,
      };
    case "checkout":
      return {
        description: copy.upgradeDescription,
        action: {
          label: copy.upgradeAction,
          onClick: openBilling,
        },
      };
    case "autopilot_addon":
      return {
        description: copy.addonDescription,
        action: {
          label: copy.addonAction,
          onClick: openAddon,
        },
        ...(upgradeToBusiness
          ? {
              secondaryAction: {
                label: copy.upgradeBusinessAction,
                onClick: openBilling,
              },
            }
          : {}),
      };
    case "portal":
      return {
        description: copy.portalDescription,
        action: {
          label: copy.portalAction,
          onClick: () => {
            void openPortal();
          },
        },
      };
    case "billing":
      return {
        description: copy.billingDescription,
        action: {
          label: copy.billingAction,
          onClick: openBilling,
        },
      };
    case "contact_admin":
      return {
        description:
          adminAsk === "addon"
            ? copy.contactAddonDescription
            : adminAsk === "addon_or_upgrade"
              ? copy.contactAddonOrUpgradeDescription
              : copy.contactDescription,
      };
    case "checking":
      return {
        description: copy.checkingDescription,
      };
    case "billing_unavailable":
      return {
        description: copy.billingUnavailableDescription,
        action: {
          label: copy.billingAction,
          onClick: openBilling,
        },
      };
  }
}
