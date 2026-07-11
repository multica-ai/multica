"use client";

// TECH-3522: web_fetch URL policy settings tab — injected into SettingsPage the
// same way cerebro-connections injects the Connections tab.

import { Globe } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { WebFetchPolicySettingsTab } from "./web-fetch-policy-settings-tab";

// Mirrors @multica/views ExtraSettingsTab without importing the views package.
interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

// useCerebroWebFetchPolicySettingsTabs returns the standalone Web fetch settings
// tab when the cerebro_web_fetch_policy flag is on, and nothing when it is off.
// FIR-3091 slice 5: when cerebro_web_fetch_permissions is on, the host list is
// rendered inside the unified Permissions screen instead, so the standalone tab
// is suppressed here to avoid showing the same editor in two places.
export function useCerebroWebFetchPolicySettingsTabs(): ExtraSettingsTab[] {
  const enabled = useFeatureFlag("cerebro_web_fetch_policy");
  const movedToPermissions = useFeatureFlag("cerebro_web_fetch_permissions");
  if (!enabled || movedToPermissions) return [];
  return [
    {
      value: "web-fetch",
      label: "Web fetch",
      icon: Globe,
      content: <WebFetchPolicySettingsTab />,
    },
  ];
}
