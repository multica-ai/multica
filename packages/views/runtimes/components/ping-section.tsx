import { Loader2, CheckCircle2, XCircle, Zap } from "lucide-react";
import { useQueryClient } from "@tanstack/react-query";
import { Button } from "@multica/ui/components/ui/button";
import { usePingRuntime } from "@multica/core/runtimes/mutations";
import { runtimeKeys } from "@multica/core/runtimes/queries";
import type { RuntimePing, RuntimePingStatus } from "@multica/core/types";

const pingStatusConfig: Record<
  RuntimePingStatus,
  { label: string; icon: typeof Loader2; color: string }
> = {
  pending: { label: "Waiting for daemon...", icon: Loader2, color: "text-muted-foreground" },
  running: { label: "Running test...", icon: Loader2, color: "text-info" },
  completed: { label: "Connected", icon: CheckCircle2, color: "text-success" },
  failed: { label: "Failed", icon: XCircle, color: "text-destructive" },
  timeout: { label: "Timeout", icon: XCircle, color: "text-warning" },
};

export function PingSection({ runtimeId }: { runtimeId: string }) {
  const qc = useQueryClient();
  const cachedPing = qc.getQueryData<RuntimePing>(runtimeKeys.pingResult(runtimeId));
  const pingMutation = usePingRuntime(runtimeId);

  const status = pingMutation.isPending ? "pending" : (cachedPing?.status ?? null);
  const output = cachedPing?.output ?? "";
  const error = cachedPing?.error ?? "";
  const durationMs = cachedPing?.duration_ms ?? null;
  const testing = pingMutation.isPending;

  const handleTest = () => {
    pingMutation.mutate();
  };

  const config = status ? pingStatusConfig[status] : null;
  const Icon = config?.icon;
  const isActive = status === "pending" || status === "running";

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2">
        <Button
          variant="outline"
          size="xs"
          onClick={handleTest}
          disabled={testing}
        >
          {testing ? (
            <Loader2 className="h-3 w-3 animate-spin" />
          ) : (
            <Zap className="h-3 w-3" />
          )}
          {testing ? "Testing..." : "Test Connection"}
        </Button>

        {config && Icon && (
          <span className={`inline-flex items-center gap-1 text-xs ${config.color}`}>
            <Icon className={`h-3 w-3 ${isActive ? "animate-spin" : ""}`} />
            {config.label}
            {durationMs != null && (
              <span className="text-muted-foreground">
                ({(durationMs / 1000).toFixed(1)}s)
              </span>
            )}
          </span>
        )}
      </div>

      {status === "completed" && output && (
        <div className="rounded-lg border bg-success/5 px-3 py-2">
          <pre className="text-xs font-mono whitespace-pre-wrap">{output}</pre>
        </div>
      )}

      {(status === "failed" || status === "timeout") && error && (
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2">
          <p className="text-xs text-destructive">{error}</p>
        </div>
      )}
    </div>
  );
}
