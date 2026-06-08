"use client";

// TECH-3108: workspace connections settings tab — injected into SettingsPage
// the same way cerebro-tool-policy injects the Permissions tab.

import { Cable } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { ConnectionsSettingsTab } from "./connections-settings-tab";

// Mirrors @multica/views ExtraSettingsTab without importing the views package.
interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

// useCerebroConnectionsSettingsTabs returns the Connections settings tab when
// the cerebro_connections flag is on, and nothing when it is off.
export function useCerebroConnectionsSettingsTabs(): ExtraSettingsTab[] {
  const enabled = useFeatureFlag("cerebro_connections");
  if (!enabled) return [];
  return [
    {
      value: "connections",
      label: "Forbindelser",
      icon: Cable,
      content: <ConnectionsSettingsTab />,
    },
  ];
}
