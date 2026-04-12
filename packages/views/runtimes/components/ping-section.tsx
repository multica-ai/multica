import { useState, useEffect, useCallback, useRef } from "react";
import { Loader2, CheckCircle2, XCircle, Zap } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { ApiError, api } from "@multica/core/api";
import type { RuntimePingStatus } from "@multica/core/types";
import {
  PING_RECOVERY_WINDOW_MS,
  useRuntimePingStore,
} from "@multica/core/runtimes";

const pingStatusConfig: Record<
  RuntimePingStatus,
  { label: string; icon: typeof Loader2; color: string }
> = {
  pending: { label: "Waiting for daemon...", icon: Loader2, color: "text-muted-foreground" },
  running: { label: "Running test...", icon: Loader2, color: "text-info" },
  completed: { label: "Connected", icon: CheckCircle2, color: "text-success" },
  failed: { label: "Failed", icon: XCircle, color: "text-destructive" },
  timeout: { label: "Timeout", icon: XCircle, color: "text-warning" },
  interrupted: { label: "Connection check interrupted", icon: XCircle, color: "text-warning" },
};

export function PingSection({ runtimeId }: { runtimeId: string }) {
  const entry = useRuntimePingStore((s) => s.entries[runtimeId]);
  const setEntry = useRuntimePingStore((s) => s.setEntry);
  const updateEntry = useRuntimePingStore((s) => s.updateEntry);
  const clearEntry = useRuntimePingStore((s) => s.clearEntry);
  const cleanupExpired = useRuntimePingStore((s) => s.cleanupExpired);

  const [status, setStatus] = useState<RuntimePingStatus | null>(entry?.status ?? null);
  const [output, setOutput] = useState("");
  const [error, setError] = useState("");
  const [durationMs, setDurationMs] = useState<number | null>(null);
  const [testing, setTesting] = useState(false);
  const [notice, setNotice] = useState("");
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const activeRequestIdRef = useRef<string | null>(null);
  const pollFailuresRef = useRef(0);

  const cleanup = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
    activeRequestIdRef.current = null;
  }, []);

  useEffect(() => cleanup, [cleanup]);

  useEffect(() => {
    cleanupExpired();
  }, [cleanupExpired]);

  useEffect(() => {
    setStatus(entry?.status ?? null);
    setOutput(entry?.output ?? "");
    setError(entry?.error ?? "");
    setDurationMs(entry?.durationMs ?? null);
    setTesting(entry?.status === "pending" || entry?.status === "running");
  }, [entry]);

  const finishWithEntry = useCallback(
    (
      nextStatus: Extract<
        RuntimePingStatus,
        "completed" | "failed" | "timeout" | "interrupted"
      >,
      patch: {
        output?: string;
        error?: string;
        durationMs?: number | null;
      } = {},
    ) => {
      updateEntry(runtimeId, {
        status: nextStatus,
        finishedAt: Date.now(),
        output: patch.output,
        error: patch.error,
        durationMs: patch.durationMs ?? null,
      });
      setTesting(false);
      cleanup();
    },
    [cleanup, runtimeId, updateEntry],
  );

  const startPolling = useCallback(
    (requestId: string) => {
      if (activeRequestIdRef.current === requestId) {
        return;
      }

      cleanup();
      activeRequestIdRef.current = requestId;
      pollFailuresRef.current = 0;
      setTesting(true);

      const pollOnce = async () => {
        try {
          const result = await api.getPingResult(runtimeId, requestId);
          pollFailuresRef.current = 0;
          updateEntry(runtimeId, {
            status: result.status as RuntimePingStatus,
          });

          if (result.status === "completed") {
            finishWithEntry("completed", {
              output: result.output ?? "",
              durationMs: result.duration_ms ?? null,
            });
            return;
          }

          if (result.status === "failed" || result.status === "timeout") {
            finishWithEntry(result.status as "failed" | "timeout", {
              error: result.error ?? "Unknown error",
              durationMs: result.duration_ms ?? null,
            });
          }
        } catch (e) {
          if (e instanceof ApiError && e.status === 404) {
            clearEntry(runtimeId);
            setNotice("Previous test status expired");
            setTesting(false);
            cleanup();
            return;
          }

          pollFailuresRef.current += 1;
          if (pollFailuresRef.current >= 5) {
            finishWithEntry("interrupted", {
              error: "Connection check was interrupted. Please try again.",
            });
          }
        }
      };

      void pollOnce();
      pollRef.current = setInterval(() => {
        void pollOnce();
      }, 2000);
    },
    [cleanup, clearEntry, finishWithEntry, runtimeId, updateEntry],
  );

  useEffect(() => {
    if (!entry) return;
    if (!entry.requestId) return;

    const isRecoverable =
      (entry.status === "pending" || entry.status === "running") &&
      Date.now() - entry.startedAt <= PING_RECOVERY_WINDOW_MS;

    if (isRecoverable) {
      startPolling(entry.requestId);
      return;
    }

    if (
      (entry.status === "pending" || entry.status === "running") &&
      Date.now() - entry.startedAt > PING_RECOVERY_WINDOW_MS
    ) {
      finishWithEntry("timeout", {
        error: "Connection check expired before it could be restored.",
      });
    }
  }, [entry, finishWithEntry, startPolling]);

  const handleTest = async () => {
    cleanup();
    setNotice("");
    setTesting(true);
    setStatus("pending");
    setOutput("");
    setError("");
    setDurationMs(null);

    try {
      const ping = await api.pingRuntime(runtimeId);
      setEntry({
        runtimeId,
        requestId: ping.id,
        status: "pending",
        startedAt: Date.now(),
      });
    } catch {
      setStatus("failed");
      setError("Failed to initiate test");
      setTesting(false);
    }
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

      {notice && <p className="text-xs text-muted-foreground">{notice}</p>}

      {status === "completed" && output && (
        <div className="rounded-lg border bg-success/5 px-3 py-2">
          <pre className="text-xs font-mono whitespace-pre-wrap">{output}</pre>
        </div>
      )}

      {(status === "failed" || status === "timeout" || status === "interrupted") && error && (
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2">
          <p className="text-xs text-destructive">{error}</p>
        </div>
      )}
    </div>
  );
}
