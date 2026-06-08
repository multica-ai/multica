"use client";

// FIR-2284 Bid 5 — the Workspace surface of the five permission flader. Unlike
// the agent/runtime/group/member surfaces (which hang off their own detail
// pages), the workspace root layer is authored under Settings. The platform
// splices this tab into <SettingsPage extraAccountTabs={...}> the same way the
// cost-optimization and feature-flag tabs are injected, so it appears in both
// web and desktop with no duplication.

import { Wrench } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { ToolPolicySurface } from "./tool-policy-surface";

// Mirrors @multica/views ExtraSettingsTab structurally. Defined locally so this
// entrypoint stays free of a views dependency (and the topo-sort coupling it
// brings), exactly like cerebro-cost-optimization/settings-tabs.
export interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

// WorkspacePermissionsTab is the Settings-tab body: the per-tool catalog editing
// the workspace root layer — the default below every runtime, agent, group, and
// user. The workspace id is both the view subject and the layer's subject_id.
//
// Tools are grouped into four tabs:
//   Repos        — per-repository read/checkout/push permissions
//   Runtime      — runtime and daemon tools
//   Multica      — all other platform tools (issues, comments, agents, etc.)
//   Connections  — workspace connection tools (one row per configured connection)
export function WorkspacePermissionsTab() {
  const wsId = useWorkspaceId();
  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-base font-semibold">Permissions</h2>
        <p className="text-sm text-muted-foreground">
          The workspace-wide default for every tool. Each runtime, group, agent,
          and member below can only tighten what is set here — never loosen it.
        </p>
      </div>
      <ToolPolicySurface wsId={wsId} view="workspace" subjectId={wsId} />
    </div>
  );
}

// useCerebroToolPolicySettingsTabs returns the workspace Permissions tab when the
// cerebro_tool_policy flag is on, and nothing when it is off — so the tab only
// appears once the feature is enabled. The platform layer (web + desktop) spreads
// the result into SettingsPage's extraAccountTabs.
export function useCerebroToolPolicySettingsTabs(): ExtraSettingsTab[] {
  const enabled = useFeatureFlag("cerebro_tool_policy");
  if (!enabled) return [];
  return [
    {
      value: "permissions",
      label: "Permissions",
      icon: Wrench,
      content: <WorkspacePermissionsTab />,
    },
  ];
}
