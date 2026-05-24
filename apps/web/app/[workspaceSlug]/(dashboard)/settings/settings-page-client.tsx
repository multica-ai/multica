"use client";

import type { ReactNode } from "react";
import { agentCapabilitiesSettingsTab } from "@multica/cerebro-agent-capabilities";
import { cerebroFeatureFlagTabs } from "@multica/cerebro-feature-flags/settings-tabs";
import { SettingsPage, type ExtraSettingsTab } from "@multica/views/settings";
import { useMembersTabCerebroExtras } from "@multica/cerebro-members/views";

// Assembled here, inside the client boundary, so the lucide icon components
// carried in each tab's `icon` field are never serialized from a Server
// Component (which throws "Functions cannot be passed directly to Client
// Components"). Module-level constant keeps the array reference stable so it
// doesn't bust SettingsPage's useMemo on every render.
const extraAccountTabs: ExtraSettingsTab[] = [agentCapabilitiesSettingsTab, ...cerebroFeatureFlagTabs];

export function SettingsPageClient({
  documentationContent,
}: {
  documentationContent?: ReactNode;
}) {
  const membersTabCerebroExtras = useMembersTabCerebroExtras();

  return (
    <SettingsPage
      extraAccountTabs={extraAccountTabs}
      membersTabCerebroExtras={membersTabCerebroExtras}
      documentationContent={documentationContent}
    />
  );
}
