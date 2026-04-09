import { useState, useEffect, useCallback, useRef } from "react";
import { Loader2, CheckCircle2, XCircle, Zap } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import type { RuntimePingStatus } from "@multica/core/types";
import { useAppLocale } from "../../i18n";

const pingStatusIcons: Record<RuntimePingStatus, { icon: typeof Loader2; color: string }> = {
  pending: { icon: Loader2, color: "text-muted-foreground" },
  running: { icon: Loader2, color: "text-info" },
  completed: { icon: CheckCircle2, color: "text-success" },
  failed: { icon: XCircle, color: "text-destructive" },
  timeout: { icon: XCircle, color: "text-warning" },
};

export function PingSection({ runtimeId }: { runtimeId: string }) {
  const { t } = useAppLocale();
  const pingStatusLabels: Record<RuntimePingStatus, string> = {
    pending: t.runtimes.waitingForDaemon,
    running: t.runtimes.runningTest,
    completed: t.runtimes.connected,
    failed: t.runtimes.testFailed,
    timeout: t.runtimes.testTimeout,
  };
  const [status, setStatus] = useState<RuntimePingStatus | null>(null);
  const [output, setOutput] = useState("");
  const [error, setError] = useState("");
  const [durationMs, setDurationMs] = useState<number | null>(null);
  const [testing, setTesting] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const cleanup = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  useEffect(() => cleanup, [cleanup]);

  const handleTest = async () => {
    cleanup();
    setTesting(true);
    setStatus("pending");
    setOutput("");
    setError("");
    setDurationMs(null);

    try {
      const ping = await api.pingRuntime(runtimeId);

      pollRef.current = setInterval(async () => {
        try {
          const result = await api.getPingResult(runtimeId, ping.id);
          setStatus(result.status as RuntimePingStatus);

          if (result.status === "completed") {
            setOutput(result.output ?? "");
            setDurationMs(result.duration_ms ?? null);
            setTesting(false);
            cleanup();
          } else if (result.status === "failed" || result.status === "timeout") {
            setError(result.error ?? t.runtimes.unknownError);
            setDurationMs(result.duration_ms ?? null);
            setTesting(false);
            cleanup();
          }
        } catch {
          // ignore poll errors
        }
      }, 2000);
    } catch {
      setStatus("failed");
      setError(t.runtimes.failedToInitiateTest);
      setTesting(false);
    }
  };

  const config = status ? pingStatusIcons[status] : null;
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
          {testing ? t.runtimes.testing : t.runtimes.testConnection}
        </Button>

        {config && Icon && (
          <span className={`inline-flex items-center gap-1 text-xs ${config.color}`}>
            <Icon className={`h-3 w-3 ${isActive ? "animate-spin" : ""}`} />
            {pingStatusLabels[status!]}
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
