import type { WorkspaceSubscriptionSummary } from "../types";

export type BillingRecoveryKind =
  | "billing_disabled"
  | "checkout"
  | "autopilot_addon"
  | "portal"
  | "billing"
  | "contact_admin"
  | "checking"
  | "billing_unavailable";

type BillingActions = WorkspaceSubscriptionSummary["availableActions"];

/**
 * Resolve the one recovery state shared by every issue/quota-limit surface.
 * Cloud's complete availableActions object is authoritative: callers must not
 * infer checkout or portal access from a locally cached member role or plan.
 *
 * `autopilotAddon` is set by autopilot-quota surfaces: buying more runs then
 * comes right after an upgrade from Free and before the Billing Portal, so a
 * paying workspace that ran out is offered more runs, not only the portal.
 */
export function resolveBillingRecovery({
  actions,
  billingEnabled,
  loading,
  portalFailed = false,
  autopilotAddon = false,
}: {
  actions: BillingActions | undefined;
  billingEnabled: boolean;
  loading: boolean;
  portalFailed?: boolean;
  autopilotAddon?: boolean;
}): BillingRecoveryKind {
  if (!billingEnabled) return "billing_disabled";
  if (portalFailed) return "billing_unavailable";
  if (actions?.checkout) return "checkout";
  if (autopilotAddon && actions?.purchaseAutopilotRuns) return "autopilot_addon";
  if (actions?.portal) return "portal";
  if (actions?.purchaseSeats) return "billing";
  if (actions) return "contact_admin";
  if (loading) return "checking";
  return "billing_unavailable";
}

/**
 * What a member without billing access should ask an admin for when the
 * autopilot allowance runs out. Display only: the plan picks the wording,
 * never an action.
 */
export type AutopilotQuotaAdminAsk = "upgrade" | "addon_or_upgrade" | "addon";

export function autopilotQuotaAdminAsk(
  summary: WorkspaceSubscriptionSummary | null | undefined,
): AutopilotQuotaAdminAsk {
  if (!summary?.autopilotAddon) return "upgrade";
  return summary.entitlement.plan === "business" ? "addon" : "addon_or_upgrade";
}

/**
 * Whether the add-on recovery also offers moving up to Business: Cloud allows
 * a plan change and the workspace is on Pro.
 */
export function canUpgradeToBusinessFromAddon(
  summary: WorkspaceSubscriptionSummary | null | undefined,
): boolean {
  return (
    summary?.availableActions.changePlan === true &&
    summary.entitlement.plan === "pro"
  );
}
