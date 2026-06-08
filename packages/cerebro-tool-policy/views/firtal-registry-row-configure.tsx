"use client";

// Renders the "Konfigurer" affordance on a `firtal_registry` row across every
// agent Tools surface (AgentToolsCard, SimpleToolPolicyTable, ToolPolicyTable).
// Drop in once per row; the wrapper is a no-op for any other tool key and when
// the `cerebro_firtal_registry_allowlist_ui` feature flag is off. Centralising
// the affordance here is the fix for TECH-2922 round 2 — the first build only
// patched AgentToolsCard, so workspaces using either of the newer tables saw
// nothing when they flipped the flag on.
//
// The button surfaces a small summary inline — "Alle data sources" / "12 af 47"
// / "Ingen tilladt" — so the row state is visible without opening the sheet.
// The same TanStack Query keys are reused by the sheet, so opening it does not
// refetch.

import { useState } from "react";
import { Settings2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { Button } from "@multica/ui/components/ui/button";
import {
  getFirtalRegistryGrantConfig,
  listFirtalRegistryDataSources,
} from "../api";
import { FirtalRegistryConfigDialog } from "./firtal-registry-config-dialog";

export const FIRTAL_REGISTRY_TOOL_KEY = "firtal_registry";

const agentToolsKey = (agentId: string) =>
  ["cerebro", "agent-tools", agentId] as const;

const configKey = (agentId: string) =>
  [...agentToolsKey(agentId), "firtal_registry-config"] as const;

const dataSourcesKey = (agentId: string) =>
  ["cerebro", "firtal-registry-data-sources", agentId] as const;

function summarise(
  configLoaded: boolean,
  config:
    | { allowed_data_sources_all?: boolean; allowed_data_sources?: string[] }
    | undefined,
  totalLoaded: boolean,
  total: number | undefined,
): string {
  if (!configLoaded) return "Konfigurer";
  if (config?.allowed_data_sources_all) return "Alle data sources";
  const picked = Array.isArray(config?.allowed_data_sources)
    ? config!.allowed_data_sources!.filter(
        (id): id is string => typeof id === "string" && id.trim().length > 0,
      ).length
    : 0;
  if (picked === 0) return "Ingen tilladt";
  if (totalLoaded && typeof total === "number") {
    return `${picked} af ${total} data sources`;
  }
  return `${picked} data sources`;
}

export function FirtalRegistryRowConfigure({
  toolKey,
  agentId,
  variant = "ghost",
  size = "sm",
}: {
  toolKey: string;
  agentId: string;
  variant?: "ghost" | "outline" | "secondary";
  size?: "sm" | "default";
}) {
  const enabled = useFeatureFlag("cerebro_firtal_registry_allowlist_ui");
  const [open, setOpen] = useState(false);

  const isFirtalRegistry = toolKey === FIRTAL_REGISTRY_TOOL_KEY;

  // Pre-fetch the summary so the row label is meaningful without opening the
  // sheet. The same query keys back the sheet, so opening it does not refetch.
  const configQuery = useQuery({
    queryKey: configKey(agentId),
    queryFn: () => getFirtalRegistryGrantConfig(agentId),
    enabled: enabled && isFirtalRegistry,
    staleTime: 30_000,
  });
  const sourcesQuery = useQuery({
    queryKey: dataSourcesKey(agentId),
    queryFn: () => listFirtalRegistryDataSources(agentId),
    enabled: enabled && isFirtalRegistry,
    staleTime: 60_000,
  });

  if (!isFirtalRegistry) return null;
  if (!enabled) return null;

  const label = summarise(
    configQuery.isSuccess,
    configQuery.data,
    sourcesQuery.isSuccess,
    sourcesQuery.data?.length,
  );

  return (
    <>
      <Button
        type="button"
        size={size}
        variant={variant}
        className="gap-1.5"
        onClick={() => setOpen(true)}
        data-testid="firtal-registry-configure"
        title={`Konfigurer data sources for firtal_registry — ${label}`}
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
