"use client";

// FIR-3091 punkt 8 (fase 1b) — per-permission detail page. For one tool it
// lists every subject that has the permission set and which layer grants it
// (the "Who & why" tab), the reverse of the per-subject tool-policy table. The
// tab shell mirrors the credential detail page; later phases add a change log
// (fase 2) and a usage log (fase 3) as the two disabled tabs.
//
// Gated by cerebro_permission_detail (default OFF). Nothing links here while the
// flag is off, so the page is dormant and the change is reversible.

import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";

import {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "@multica/ui/components/ui/tabs";
import { Button } from "@multica/ui/components/ui/button";

import { useWorkspaceId } from "@multica/core/hooks";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";

import { getPermissionHolders } from "../api";
import { useHolderDirectory } from "./use-holder-directory";

const LAYER_LABEL: Record<string, string> = {
  workspace: "Workspace",
  runtime: "Runtime",
  agent: "Agent",
  group: "Group",
  user: "Member",
  system: "System",
};

// layerLabel names the policy layer for the row's secondary text. Enum drift
// (an unknown future layer) downgrades to the raw string rather than crashing.
function layerLabel(layer: string): string {
  return LAYER_LABEL[layer] ?? layer;
}

// settingLabel turns a stored setting into a human word. An unknown value
// downgrades to itself (or a dash) instead of throwing.
function settingLabel(setting: string): string {
  switch (setting) {
    case "allow":
      return "Allow";
    case "ask":
      return "Ask";
    case "deny":
      return "Deny";
    default:
      return setting || "—";
  }
}

export function PermissionDetailPage({
  toolKey,
  onBack,
}: {
  toolKey: string;
  onBack: () => void;
}) {
  const wsId = useWorkspaceId();
  const enabled = useFeatureFlag("cerebro_permission_detail");

  const holdersQuery = useQuery({
    queryKey: ["cerebro", "tool-policy", "holders", wsId, toolKey],
    queryFn: () => getPermissionHolders(wsId, toolKey),
    enabled: enabled && !!wsId && !!toolKey,
  });

  const directory = useHolderDirectory(wsId, enabled);

  // Safety fallback: the route always registers, so guard the flag here. Nothing
  // links to the page while off, so this is only reached by a hand-typed URL.
  if (!enabled) {
    return (
      <div className="mx-auto max-w-3xl p-6">
        <Button variant="ghost" size="sm" onClick={onBack} className="mb-4">
          <ArrowLeft className="size-4" /> Back
        </Button>
        <p className="text-sm text-muted-foreground">
          This page is not available.
        </p>
      </div>
    );
  }

  const data = holdersQuery.data;
  const holders = data?.holders ?? [];
  // A tool whose policy is stored but not enforced at runtime yet is shown
  // greyed out with an explicit note, so no one mistakes it for a live control.
  const isEnforced = data?.enforced === true;

  return (
    <div className="mx-auto max-w-3xl p-6">
      <Button variant="ghost" size="sm" onClick={onBack} className="mb-4">
        <ArrowLeft className="size-4" /> Back
      </Button>

      <div className="mb-4">
        <h1 className="text-lg font-semibold">Permission</h1>
        <p className="font-mono text-sm text-muted-foreground">{toolKey}</p>
      </div>

      <Tabs defaultValue="holders">
        <TabsList>
          <TabsTrigger value="holders">Who &amp; why</TabsTrigger>
          <TabsTrigger value="changes" disabled>
            Changes
          </TabsTrigger>
          <TabsTrigger value="usage" disabled>
            Usage
          </TabsTrigger>
        </TabsList>

        <TabsContent value="holders" className="pt-4">
          {!isEnforced ? (
            <div className="mb-3 rounded border border-dashed p-3 text-sm text-muted-foreground">
              Shown, but not enforced yet.
            </div>
          ) : null}

          {holdersQuery.isLoading || directory.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : holders.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No one has this permission set.
            </p>
          ) : (
            <ul
              className={`space-y-2 text-sm ${!isEnforced ? "opacity-60" : ""}`}
            >
              {holders.map((h, i) => (
                <li
                  key={`${h.layer}:${h.subject_id}:${h.resource_pattern}:${i}`}
                  className="flex items-center justify-between gap-3 rounded border p-2"
                >
                  <div className="min-w-0">
                    <span className="font-medium">
                      {directory.labelFor(h.layer, h.subject_id)}
                    </span>
                    <span className="ml-2 text-xs text-muted-foreground">
                      {layerLabel(h.layer)} layer
                    </span>
                  </div>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {settingLabel(h.setting)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
}
