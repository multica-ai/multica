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
import { WebFetchPolicySettingsTab } from "@multica/cerebro-web-fetch-policy/views";
import { ToolPolicyTabs } from "./tool-policy-table";
import { CapabilityIsolationSections } from "./capability-isolation-sections";

// Mirrors @multica/views ExtraSettingsTab structurally. Defined locally so this
// entrypoint stays free of a views dependency (and the topo-sort coupling it
// brings), exactly like cerebro-cost-optimization/settings-tabs.
export interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
  // FIR-1404: wide data-table tabs (Permissions) opt out of the narrow
  // max-w-3xl settings column so the per-tool catalog can use the full pane.
  wide?: boolean;
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
  // FIR-3091 slice 5: when on, the web_fetch host list is rendered here instead
  // of its own "Web fetch" settings tab (which suppresses itself in the same
  // flag state). Off keeps the standalone tab, so the relocation is reversible.
  const webFetchInPermissions = useFeatureFlag("cerebro_web_fetch_permissions");
  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-base font-semibold">Permissions</h2>
        <p className="text-sm text-muted-foreground">
          The workspace-wide default for every tool. Each runtime, group, agent,
          and member below can only tighten what is set here — never loosen it.
        </p>
      </div>
      <ToolPolicyTabs wsId={wsId} view="workspace" subjectId={wsId} />
      {/* FIR-3091 slice 3: the OS-sandbox network profiles, per-runtime
          overrides, and per-agent blocked MCP servers — a parallel isolation
          layer shown alongside the tool-policy catalog, rehomed from the retired
          "Agent capabilities" settings tab. */}
      <CapabilityIsolationSections />
      {/* FIR-3091 slice 5: the web_fetch host allow/deny list, rehomed from the
          standalone "Web fetch" settings tab so all permission surfaces live on
          one screen. Gated so the move is reversible. */}
      {webFetchInPermissions ? (
        <div className="border-t pt-6">
          <WebFetchPolicySettingsTab />
        </div>
      ) : null}
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
      // FIR-1404: the permission catalog is a wide multi-column table — give it
      // the full settings pane instead of the narrow max-w-3xl column.
      wide: true,
    },
  ];
}
