"use client";

// TECH-3582: the Workspace Copy settings tab — injected into SettingsPage the
// same way cerebro-connections injects the Connections tab. Present only when
// the cerebro_workspace_copy flag is on.
import { CopyPlus } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { WorkspaceCopyConsole } from "./workspace-copy-console";

// Mirrors @multica/views ExtraSettingsTab without importing the views package.
interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

export function useCerebroWorkspaceCopySettingsTabs(): ExtraSettingsTab[] {
  const enabled = useFeatureFlag("cerebro_workspace_copy");
  if (!enabled) return [];
  return [
    {
      value: "workspace-copy",
      label: "Workspace copy",
      icon: CopyPlus,
      content: <WorkspaceCopyConsole />,
    },
  ];
}
