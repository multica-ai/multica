"use client";

import { useMemo, type ReactNode } from "react";
import { agentCapabilitiesSettingsTab } from "@multica/cerebro-agent-capabilities";
import { cerebroCostOptimizationTabs } from "@multica/cerebro-cost-optimization/views";
import { cerebroFeatureFlagTabs } from "@multica/cerebro-feature-flags/settings-tabs";
import { useCerebroDisplayCurrencyTabs } from "@multica/cerebro-display-currency/settings-tabs";
import { SettingsPage, type ExtraSettingsTab } from "@multica/views/settings";
import { useMembersTabCerebroExtras } from "@multica/cerebro-members/views";
import {
  useCerebroToolPolicySettingsTabs,
  useCerebroAgentTriggerSettingsTabs,
} from "@multica/cerebro-tool-policy/views";

// Assembled here, inside the client boundary, so the lucide icon components
// carried in each tab's `icon` field are never serialized from a Server
// Component (which throws "Functions cannot be passed directly to Client
// Components"). Module-level constant keeps the array reference stable so it
// doesn't bust SettingsPage's useMemo on every render.
const extraAccountTabs: ExtraSettingsTab[] = [
  agentCapabilitiesSettingsTab,
  ...cerebroCostOptimizationTabs,
  ...cerebroFeatureFlagTabs,
];

export function SettingsPageClient({
  documentationContent,
}: {
  documentationContent?: ReactNode;
}) {
  const membersTabCerebroExtras = useMembersTabCerebroExtras();
  // FIR-2284 Bid 5: the workspace Permissions tab, present only when the
  // cerebro_tool_policy flag is on. Merged here (not in the module-level const)
  // because the flag is read via a hook.
  const toolPolicyTabs = useCerebroToolPolicySettingsTabs();
  // FIR-2409: the friendly "Agent-start" tab, present only when the
  // cerebro_agent_trigger_permissions flag is on.
  const agentTriggerTabs = useCerebroAgentTriggerSettingsTabs();
  // FIR-40: the workspace Currency tab, present only when the
  // cerebro_display_currency flag is on (read via a hook).
  const displayCurrencyTabs = useCerebroDisplayCurrencyTabs();
  const accountTabs = useMemo(
    () => [...extraAccountTabs, ...toolPolicyTabs, ...agentTriggerTabs, ...displayCurrencyTabs],
    [toolPolicyTabs, agentTriggerTabs, displayCurrencyTabs],
  );

  return (
    <SettingsPage
      extraAccountTabs={accountTabs}
      membersTabCerebroExtras={membersTabCerebroExtras}
      documentationContent={documentationContent}
    />
  );
}
