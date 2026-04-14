import { Loader2, CheckCircle2, XCircle, ArrowUpCircle, Check } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { useUpdateRuntime } from "@multica/core/runtimes/mutations";
import { latestCliVersionOptions, runtimeKeys } from "@multica/core/runtimes/queries";
import type { RuntimeUpdate, RuntimeUpdateStatus } from "@multica/core/types";

function stripV(v: string): string {
  return v.replace(/^v/, "");
}

function isNewer(latest: string, current: string): boolean {
  const l = stripV(latest).split(".").map(Number);
  const c = stripV(current).split(".").map(Number);
  for (let i = 0; i < Math.max(l.length, c.length); i++) {
    const lv = l[i] ?? 0;
    const cv = c[i] ?? 0;
    if (lv > cv) return true;
    if (lv < cv) return false;
  }
  return false;
}

const statusConfig: Record<
  RuntimeUpdateStatus,
  { label: string; icon: typeof Loader2; color: string }
> = {
  pending: {
    label: "Waiting for daemon...",
    icon: Loader2,
    color: "text-muted-foreground",
  },
  running: {
    label: "Updating...",
    icon: Loader2,
    color: "text-info",
  },
  completed: {
    label: "Update complete. Daemon is restarting...",
    icon: CheckCircle2,
    color: "text-success",
  },
  failed: { label: "Update failed", icon: XCircle, color: "text-destructive" },
  timeout: { label: "Timeout", icon: XCircle, color: "text-warning" },
};

interface UpdateSectionProps {
  runtimeId: string;
  currentVersion: string | null;
  isOnline: boolean;
}

export function UpdateSection({
  runtimeId,
  currentVersion,
  isOnline,
}: UpdateSectionProps) {
  const qc = useQueryClient();
  const { data: latestVersion } = useQuery(latestCliVersionOptions());
  const cachedUpdate = qc.getQueryData<RuntimeUpdate>(runtimeKeys.updateResult(runtimeId));
  const updateMutation = useUpdateRuntime(runtimeId);

  const status = updateMutation.isPending ? "pending" : (cachedUpdate?.status ?? null);
  const error = cachedUpdate?.error ?? "";
  const output = cachedUpdate?.output ?? "";
  const updating = updateMutation.isPending;

  const handleUpdate = () => {
    if (!latestVersion) return;
    updateMutation.mutate(latestVersion);
  };

  const hasUpdate =
    currentVersion &&
    latestVersion &&
    isNewer(latestVersion, currentVersion);

  const config = status ? statusConfig[status] : null;
  const Icon = config?.icon;
  const isActive = status === "pending" || status === "running";

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 flex-wrap">
        <span className="text-xs text-muted-foreground">CLI Version:</span>
        <span className="text-xs font-mono">
          {currentVersion ?? "unknown"}
        </span>

        {!hasUpdate && currentVersion && latestVersion && !status && (
          <span className="inline-flex items-center gap-1 text-xs text-success">
            <Check className="h-3 w-3" />
            Latest
          </span>
        )}

        {hasUpdate && !status && (
          <>
            <span className="text-xs text-muted-foreground">→</span>
            <span className="text-xs font-mono text-info">
              {latestVersion}
            </span>
            <span className="text-xs text-muted-foreground">available</span>
          </>
        )}

        {hasUpdate && isOnline && !status && (
          <Button
            variant="outline"
            size="xs"
            onClick={handleUpdate}
            disabled={updating}
          >
            <ArrowUpCircle className="h-3 w-3" />
            Update
          </Button>
        )}

        {config && Icon && (
          <span
            className={`inline-flex items-center gap-1 text-xs ${config.color}`}
          >
            <Icon className={`h-3 w-3 ${isActive ? "animate-spin" : ""}`} />
            {config.label}
          </span>
        )}
      </div>

      {status === "completed" && output && (
        <div className="rounded-lg border bg-success/5 px-3 py-2">
          <p className="text-xs text-success">{output}</p>
        </div>
      )}

      {(status === "failed" || status === "timeout") && error && (
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2">
          <p className="text-xs text-destructive">{error}</p>
          {status === "failed" && (
            <Button
              variant="ghost"
              size="xs"
              className="mt-1"
              onClick={handleUpdate}
            >
              Retry
            </Button>
          )}
        </div>
      )}
    </div>
  );
}
