// Runtime tool inventory and canonical policy administration.

"use client";

import { useMemo, useState } from "react";
import { RefreshCw } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import type { AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { AccessDiagnostics } from "@multica/cerebro-ui";
import { useFeatureFlag } from "@multica/cerebro-feature-flags";
import { ToolPolicyTabs } from "@multica/cerebro-tool-policy/views";
import { toolPolicyKeys } from "@multica/cerebro-tool-policy/core";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { SandboxProfileCard } from "./sandbox-profile-card";

interface RuntimeToolsCardProps {
  runtime: AgentRuntime;
  workspaceId: string;
  canEdit: boolean;
}

const runtimeToolsKey = (runtimeId: string) =>
  ["cerebro", "runtime-tools", runtimeId] as const;

const runtimeAccessDiagnosticsKey = (runtimeId: string) =>
  ["cerebro", "runtime-access-diagnostics", runtimeId] as const;

export function RuntimeToolsCard({
  runtime,
  workspaceId,
  canEdit,
}: RuntimeToolsCardProps) {
  const qc = useQueryClient();
  const wsId = useWorkspaceId() || workspaceId;
  const accessDiagnosticsEnabled = useFeatureFlag("cerebro_access_diagnostics");
  const [scanning, setScanning] = useState(false);
  const toolsQuery = useQuery({
    queryKey: runtimeToolsKey(runtime.id),
    queryFn: () => api.listRuntimeTools(runtime.id),
  });
  const diagnosticsQuery = useQuery({
    queryKey: runtimeAccessDiagnosticsKey(runtime.id),
    queryFn: () => api.getRuntimeAccessDiagnostics(runtime.id),
    enabled: accessDiagnosticsEnabled,
  });
  const lastScannedAt = useMemo(() => {
    let newest: string | null = null;
    for (const tool of toolsQuery.data ?? []) {
      const timestamp = tool.last_scanned_at;
      if (timestamp && (!newest || timestamp > newest)) newest = timestamp;
    }
    return newest;
  }, [toolsQuery.data]);

  async function refreshInventory() {
    await Promise.all([
      qc.invalidateQueries({ queryKey: runtimeToolsKey(runtime.id) }),
      qc.invalidateQueries({ queryKey: runtimeAccessDiagnosticsKey(runtime.id) }),
      qc.invalidateQueries({ queryKey: toolPolicyKeys.all(wsId) }),
    ]);
  }

  async function handleScanNow() {
    setScanning(true);
    try {
      await api.cerebroRequest<void>(
        `/api/runtimes/${runtime.id}/tools/scan-now`,
        { method: "POST" },
      );
      toast.success("Scan started", {
        description: "Asked the daemon to scan now — refreshing inventory…",
      });
      await refreshInventory();
      setTimeout(() => void refreshInventory(), 2500);
      setTimeout(() => void refreshInventory(), 6000);
    } catch (error) {
      const message = error instanceof Error ? error.message : "";
      toast.error("Couldn't start scan", {
        description: /502|offline/i.test(message)
          ? "The runtime's daemon is offline."
          : message || "Try again in a moment.",
      });
    } finally {
      setScanning(false);
    }
  }

  return (
    <div className="space-y-4">
      <SandboxProfileCard runtime={runtime} wsId={wsId} canEdit={canEdit} />
      {accessDiagnosticsEnabled ? (
        <section className="rounded-md border bg-card" aria-labelledby="runtime-capability-discovery">
          <div className="border-b p-4">
            <h3 id="runtime-capability-discovery" className="text-sm font-medium">
              Capability discovery
            </h3>
            <p className="mt-0.5 text-xs text-muted-foreground">
              Provider probe and MCP tools/list are separate evidence. Neither changes Settings → Permissions.
            </p>
          </div>
          <div className="p-3">
            {diagnosticsQuery.isPending ? (
              <div className="rounded-md border border-dashed p-3 text-xs text-muted-foreground" role="status">
                Loading capability diagnostics…
              </div>
            ) : diagnosticsQuery.isError ? (
              <AccessDiagnostics diagnostics={[{
                code: "runtime_diagnostics_error",
                state: "error",
                title: "Capability diagnostics unavailable",
                message: diagnosticsQuery.error instanceof Error ? diagnosticsQuery.error.message : "The diagnostics request failed.",
                affected_capability: `runtime:${runtime.id}`,
                source_policy: "Runtime diagnostics API",
                recovery_action: "Retry the page, then check the Runtime and server logs if the request still fails.",
              }]} />
            ) : (
              <AccessDiagnostics
                diagnostics={diagnosticsQuery.data?.diagnostics ?? []}
                emptyMessage="No capability diagnostics were returned for this Runtime."
              />
            )}
          </div>
        </section>
      ) : null}
      <div className="rounded-md border bg-card">
        <div className="flex flex-wrap items-start justify-between gap-3 border-b p-4">
          <div>
            <h3 className="text-sm font-medium">Tools on runtime</h3>
            <p className="mt-0.5 text-xs text-muted-foreground">
              All tools on <code className="font-mono text-[11px]">{runtime.name}</code>{" "}
              — set Allow, Ask, or Deny through the canonical tool policy. The
              Effective column shows the result of the complete policy chain.
            </p>
          </div>
          <div className="flex items-center gap-2">
            <LastScannedLabel scannedAt={lastScannedAt} />
            <Button
              size="sm"
              variant="outline"
              onClick={handleScanNow}
              disabled={scanning}
              className="gap-1.5"
              title="Ask the daemon to scan this runtime's tools right now"
            >
              <RefreshCw className={cn("h-3.5 w-3.5", scanning && "animate-spin")} />
              {scanning ? "Scanning…" : "Scan now"}
            </Button>
          </div>
        </div>
        <div className="space-y-4 p-3">
          <ToolPolicyTabs wsId={wsId} view="runtime" subjectId={runtime.id} />
        </div>
      </div>
    </div>
  );
}

function LastScannedLabel({ scannedAt }: { scannedAt: string | null }) {
  return (
    <span className="text-[11px] text-muted-foreground">
      {scannedAt ? `Last scanned ${formatRelativeTime(scannedAt)}` : "Never scanned"}
    </span>
  );
}

function formatRelativeTime(iso: string): string {
  const timestamp = new Date(iso).getTime();
  if (Number.isNaN(timestamp)) return "recently";
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) return "just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}
