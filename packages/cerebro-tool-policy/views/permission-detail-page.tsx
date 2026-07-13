"use client";

// FIR-3091 punkt 8 — per-permission detail page. For one tool it lists every
// subject that has the permission set and which layer grants it (the "Who &
// why" tab, fase 1b), every Set/Clear on its policy rows (the "Changes" tab,
// fase 2), and every time a decision for it was applied at an enforcement
// point (the "Usage" tab, fase 3) — the reverse of the per-subject tool-policy
// table. The tab shell mirrors the credential detail page.
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

import {
  getPermissionChanges,
  getPermissionHolders,
  getPermissionUsage,
} from "../api";
import type { PermissionChange, PermissionUsageRow } from "../api";
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

// transitionLabel renders one change-log row's old -> new movement. "" on the
// old side means the layer held no explicit row before the write (Inherit); a
// clear always ends at "Cleared" (back to Inherit).
function transitionLabel(change: PermissionChange): string {
  const from = change.old_setting ? settingLabel(change.old_setting) : "Not set";
  if (change.action === "clear") {
    return `${from} → Cleared`;
  }
  return `${from} → ${settingLabel(change.new_setting)}`;
}

// changeTimeLabel formats a change's RFC3339 timestamp for the row. An
// unparseable or missing value downgrades to the raw string (or a dash)
// instead of rendering "Invalid Date".
function changeTimeLabel(createdAt: string): string {
  if (!createdAt) return "—";
  const d = new Date(createdAt);
  if (Number.isNaN(d.getTime())) return createdAt;
  return d.toLocaleString();
}

const ENFORCEMENT_POINT_LABEL: Record<string, string> = {
  mention_gate: "Agent trigger",
  repo_checkout: "Repo checkout",
  agent_browser_sandbox: "Agent browser",
  gateway_tool: "Gateway tool call",
  gateway_connection_tool: "Connection tool call",
  local_cli: "Local runtime tool call",
  http_action: "Workspace action",
};

// enforcementPointLabel names the gate that applied a usage-log row's
// decision. An unknown future gate downgrades to its raw identifier.
function enforcementPointLabel(point: string): string {
  return ENFORCEMENT_POINT_LABEL[point] ?? point;
}

// usageSubjectLabel names a usage row's subject through the same directory the
// other tabs use. subject_type maps to the directory's layer vocabulary
// ("member" rows are member ids, i.e. the "user" layer); a system subject or
// an unknown type downgrades to a plain word.
function usageSubjectLabel(
  row: PermissionUsageRow,
  labelFor: (layer: string, id: string) => string,
): string {
  switch (row.subject_type) {
    case "member":
      return labelFor("user", row.subject_id);
    case "agent":
      return labelFor("agent", row.subject_id);
    default:
      return "System";
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

  const changesQuery = useQuery({
    queryKey: ["cerebro", "tool-policy", "changes", wsId, toolKey],
    queryFn: () => getPermissionChanges(wsId, toolKey),
    enabled: enabled && !!wsId && !!toolKey,
  });

  const usageQuery = useQuery({
    queryKey: ["cerebro", "tool-policy", "usage", wsId, toolKey],
    queryFn: () => getPermissionUsage(wsId, toolKey),
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
          <TabsTrigger value="changes">Changes</TabsTrigger>
          <TabsTrigger value="usage">Usage</TabsTrigger>
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

        <TabsContent value="changes" className="pt-4">
          {changesQuery.isLoading || directory.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : (changesQuery.data?.changes ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No changes recorded. Recording started when this log shipped, so
              earlier changes are not shown.
            </p>
          ) : (
            <ul className="space-y-2 text-sm">
              {(changesQuery.data?.changes ?? []).map((c, i) => (
                <li
                  key={`${c.created_at}:${c.layer}:${c.subject_id}:${i}`}
                  className="flex items-center justify-between gap-3 rounded border p-2"
                >
                  <div className="min-w-0">
                    <span className="font-medium">
                      {directory.labelFor(c.layer, c.subject_id)}
                    </span>
                    <span className="ml-2 text-xs text-muted-foreground">
                      {layerLabel(c.layer)} layer · {transitionLabel(c)}
                    </span>
                    <div className="text-xs text-muted-foreground">
                      by{" "}
                      {c.actor_type === "member"
                        ? directory.labelFor("user", c.actor_id)
                        : "System"}
                    </div>
                  </div>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {changeTimeLabel(c.created_at)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </TabsContent>

        <TabsContent value="usage" className="pt-4">
          {usageQuery.isLoading || directory.isLoading ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : (usageQuery.data?.usage ?? []).length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No usage recorded. Recording started when this log shipped, so
              earlier usage is not shown.
            </p>
          ) : (
            <ul className="space-y-2 text-sm">
              {(usageQuery.data?.usage ?? []).map((u, i) => (
                <li
                  key={`${u.created_at}:${u.enforcement_point}:${u.subject_id}:${i}`}
                  className="flex items-center justify-between gap-3 rounded border p-2"
                >
                  <div className="min-w-0">
                    <span className="font-medium">
                      {usageSubjectLabel(u, directory.labelFor)}
                    </span>
                    <span className="ml-2 text-xs text-muted-foreground">
                      {enforcementPointLabel(u.enforcement_point)} ·{" "}
                      {settingLabel(u.decision)}
                    </span>
                    {u.resource ? (
                      <div className="truncate text-xs text-muted-foreground">
                        {u.resource}
                      </div>
                    ) : null}
                  </div>
                  <span className="shrink-0 text-xs text-muted-foreground">
                    {changeTimeLabel(u.created_at)}
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
