"use client";

import { useMemo, type ReactNode } from "react";
import { agentCapabilitiesSettingsTab } from "@multica/cerebro-agent-capabilities";
import { cerebroCostOptimizationTabs } from "@multica/cerebro-cost-optimization/views";
import { cerebroFeatureFlagTabs } from "@multica/cerebro-feature-flags/settings-tabs";
import { cerebroNotesSettingsTabs } from "@multica/cerebro-notes/settings-tabs";
import { useCerebroDisplayCurrencyTabs } from "@multica/cerebro-display-currency/settings-tabs";
import { SettingsPage, type ExtraSettingsTab } from "@multica/views/settings";
import { useMembersTabCerebroExtras } from "@multica/cerebro-members/views";
import {
  useCerebroToolPolicySettingsTabs,
  useCerebroAgentTriggerSettingsTabs,
} from "@multica/cerebro-tool-policy/views";
// CEREBRO-PATCH(cerebro-connections-settings-tab): TECH-3108 workspace connections tab.
import { useCerebroConnectionsSettingsTabs } from "@multica/cerebro-connections/views";
// TECH-3582: workspace copy console tab, present only when cerebro_workspace_copy is on.
import { useCerebroWorkspaceCopySettingsTabs } from "@multica/cerebro-workspace-copy/views";
// TECH-3522: web_fetch URL policy tab.
import { useCerebroWebFetchPolicySettingsTabs } from "@multica/cerebro-web-fetch-policy/views";

// Assembled here, inside the client boundary, so the lucide icon components
// carried in each tab's `icon` field are never serialized from a Server
// Component (which throws "Functions cannot be passed directly to Client
// Components"). Module-level constant keeps the array reference stable so it
// doesn't bust SettingsPage's useMemo on every render.
const extraAccountTabs: ExtraSettingsTab[] = [
  agentCapabilitiesSettingsTab,
  ...cerebroCostOptimizationTabs,
  ...cerebroNotesSettingsTabs,
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
  // TECH-3108: the workspace Connections tab, present only when the
  // cerebro_connections flag is on.
  const connectionsTabs = useCerebroConnectionsSettingsTabs();
  // TECH-3582: the Workspace copy tab, present only when the
  // cerebro_workspace_copy flag is on (read via a hook).
  const workspaceCopyTabs = useCerebroWorkspaceCopySettingsTabs();
  // TECH-3522: the workspace Web fetch tab, present only when the
  // cerebro_web_fetch_policy flag is on.
  const webFetchPolicyTabs = useCerebroWebFetchPolicySettingsTabs();
  const accountTabs = useMemo(
    () => [
      ...extraAccountTabs,
      ...toolPolicyTabs,
      ...agentTriggerTabs,
      ...displayCurrencyTabs,
      ...connectionsTabs,
      ...workspaceCopyTabs,
      ...webFetchPolicyTabs,
    ],
    [
      toolPolicyTabs,
      agentTriggerTabs,
      displayCurrencyTabs,
      connectionsTabs,
      workspaceCopyTabs,
      webFetchPolicyTabs,
    ],
  );

  return (
    <SettingsPage
      extraAccountTabs={accountTabs}
      membersTabCerebroExtras={membersTabCerebroExtras}
      documentationContent={documentationContent}
    />
  );
}
