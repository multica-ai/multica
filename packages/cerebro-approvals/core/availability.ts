"use client";

import { useFlagValue } from "@multica/cerebro-feature-flags";

// Ask enforcement and its human decision path are one product contract. The
// inbox may be enabled on its own to inspect history, but it can never be
// hidden while the approval gate is creating requests.
export function approvalExperienceEnabled(inboxEnabled: boolean, gateEnabled: boolean): boolean {
  return inboxEnabled || gateEnabled;
}

export function useApprovalExperienceEnabled(): boolean {
  const inboxEnabled = useFlagValue("cerebro_approvals");
  const gateEnabled = useFlagValue("cerebro_approval_gate");
  return approvalExperienceEnabled(inboxEnabled, gateEnabled);
}
