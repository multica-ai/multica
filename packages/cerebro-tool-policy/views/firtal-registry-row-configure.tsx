"use client";

// Renders the "Konfigurer" affordance on a `firtal_registry` row across every
// agent Tools surface (AgentToolsCard, SimpleToolPolicyTable, ToolPolicyTable).
// Drop in once per row; the wrapper is a no-op for any other tool key and when
// the `cerebro_firtal_registry_allowlist_ui` feature flag is off. Centralising
// the affordance here is the fix for TECH-2922 round 2 — the first build only
// patched AgentToolsCard, so workspaces using either of the newer tables saw
// nothing when they flipped the flag on.

import { useState } from "react";
import { Settings2 } from "lucide-react";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { Button } from "@multica/ui/components/ui/button";
import { FirtalRegistryConfigDialog } from "./firtal-registry-config-dialog";

export const FIRTAL_REGISTRY_TOOL_KEY = "firtal_registry";

export function FirtalRegistryRowConfigure({
  toolKey,
  agentId,
  variant = "ghost",
  size = "sm",
  label = "Konfigurer",
}: {
  toolKey: string;
  agentId: string;
  variant?: "ghost" | "outline" | "secondary";
  size?: "sm" | "default";
  label?: string;
}) {
  const enabled = useFeatureFlag("cerebro_firtal_registry_allowlist_ui");
  const [open, setOpen] = useState(false);

  if (toolKey !== FIRTAL_REGISTRY_TOOL_KEY) return null;
  if (!enabled) return null;

  return (
    <>
      <Button
        type="button"
        size={size}
        variant={variant}
        className="gap-1.5"
        onClick={() => setOpen(true)}
        data-testid="firtal-registry-configure"
      >
        <Settings2 className="h-3.5 w-3.5" />
        {label}
      </Button>
      <FirtalRegistryConfigDialog
        agentId={agentId}
        open={open}
        onOpenChange={setOpen}
      />
    </>
  );
}
