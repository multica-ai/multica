import type { WorkspaceSubscriptionSummary } from "../types";

export type BillingRecoveryKind =
  | "billing_disabled"
  | "checkout"
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
 */
export function resolveBillingRecovery({
  actions,
  billingEnabled,
  loading,
  portalFailed = false,
}: {
  actions: BillingActions | undefined;
  billingEnabled: boolean;
  loading: boolean;
  portalFailed?: boolean;
}): BillingRecoveryKind {
  if (!billingEnabled) return "billing_disabled";
  if (portalFailed) return "billing_unavailable";
  if (actions?.checkout) return "checkout";
  if (actions?.portal) return "portal";
  if (actions?.purchaseSeats) return "billing";
  if (actions) return "contact_admin";
  if (loading) return "checking";
  return "billing_unavailable";
}
