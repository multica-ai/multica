"use client";

// FIR-2284 Bid 5 — the Workspace surface of the five permission flader. Unlike
// the agent/runtime/group/member surfaces (which hang off their own detail
// pages), the workspace root layer is authored under Settings. The platform
// splices this tab into <SettingsPage extraAccountTabs={...}> the same way the
// cost-optimization and feature-flag tabs are injected, so it appears in both
// web and desktop with no duplication.

import {
  Eye,
  Layers3,
  ListChecks,
  ShieldCheck,
  UserRound,
  Wrench,
} from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { WebFetchPolicySettingsTab } from "@multica/cerebro-web-fetch-policy/views";
import { ToolPolicyTabs } from "./tool-policy-table";
import { CapabilityIsolationSections } from "./capability-isolation-sections";
import { PermissionRoles } from "./permission-roles";

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

function PermissionDecisionGuide() {
  const steps = [
    {
      icon: ShieldCheck,
      title: "Roles",
      body: "Reusable permission profiles assigned to agents or members.",
    },
    {
      icon: Layers3,
      title: "Workspace default",
      body: "The broad starting point. Disable remains a non-openable safety floor.",
    },
    {
      icon: UserRound,
      title: "Overrides",
      body: "More specific rules can change general access; protected security floors only tighten.",
    },
    {
      icon: ListChecks,
      title: "Task access",
      body: "A locked per-run list can narrow what the agent may use on one task.",
    },
    {
      icon: Eye,
      title: "Capabilities",
      body: "The read-only live answer the agent receives and the platform enforces.",
    },
  ];
  return (
    <section
      className="overflow-hidden rounded-lg border bg-card"
      aria-labelledby="permission-decision-guide"
    >
      <div className="border-b p-4">
        <h3 id="permission-decision-guide" className="text-sm font-semibold">
          How access is decided
        </h3>
        <p className="mt-1 text-xs text-muted-foreground">
          All permission screens feed one live decision. The declared contract
          decides which specific overrides may open a default and which safety
          floors can only tighten.
        </p>
      </div>
      <ol className="grid gap-px bg-border sm:grid-cols-2 xl:grid-cols-5">
        {steps.map(({ icon: Icon, title, body }, index) => (
          <li key={title} className="min-w-0 bg-card p-4">
            <div className="flex items-center gap-2 text-sm font-medium">
              <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-muted text-xs">
                {index + 1}
              </span>
              <Icon className="size-4 text-muted-foreground" />
              {title}
            </div>
            <p className="mt-2 text-xs leading-relaxed text-muted-foreground">
              {body}
            </p>
          </li>
        ))}
      </ol>
    </section>
  );
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
export function WorkspacePermissionsTab({
  onOpenPermission,
}: {
  onOpenPermission?: (toolKey: string) => void;
}) {
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
          The workspace-wide starting point for every tool. More specific rules
          may change general access, while Disable and protected security floors
          cannot be opened downstream.
        </p>
      </div>
      <PermissionDecisionGuide />
      <PermissionRoles workspaceId={wsId} />
      <ToolPolicyTabs
        wsId={wsId}
        view="workspace"
        subjectId={wsId}
        onOpenPermission={onOpenPermission}
      />
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
export function useCerebroToolPolicySettingsTabs(
  onOpenPermission?: (toolKey: string) => void,
): ExtraSettingsTab[] {
  const enabled = useFeatureFlag("cerebro_tool_policy");
  if (!enabled) return [];
  return [
    {
      value: "permissions",
      label: "Permissions",
      icon: Wrench,
      content: (
        <WorkspacePermissionsTab onOpenPermission={onOpenPermission} />
      ),
      // FIR-1404: the permission catalog is a wide multi-column table — give it
      // the full settings pane instead of the narrow max-w-3xl column.
      wide: true,
    },
  ];
}
