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

// useCerebroWebFetchPolicySettingsTabs returns the Web fetch settings tab when
// the cerebro_web_fetch_policy flag is on, and nothing when it is off.
export function useCerebroWebFetchPolicySettingsTabs(): ExtraSettingsTab[] {
  const enabled = useFeatureFlag("cerebro_web_fetch_policy");
  if (!enabled) return [];
  return [
    {
      value: "web-fetch",
      label: "Web fetch",
      icon: Globe,
      content: <WebFetchPolicySettingsTab />,
    },
  ];
}
