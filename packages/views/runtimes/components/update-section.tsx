import { useState, useEffect, useCallback, useRef } from "react";
import {
  Loader2,
  CheckCircle2,
  XCircle,
  ArrowUpCircle,
  Check,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { ApiError, api } from "@multica/core/api";
import type { RuntimeUpdateStatus } from "@multica/core/types";
import {
  UPDATE_RECOVERY_WINDOW_MS,
  resolveUpdateScopeId,
  useDaemonUpdateStore,
} from "@multica/core/runtimes";

const GITHUB_RELEASES_URL =
  "https://api.github.com/repos/multica-ai/multica/releases/latest";
const CACHE_TTL_MS = 10 * 60 * 1000; // 10 minutes

let cachedLatestVersion: string | null = null;
let cachedAt = 0;

async function fetchLatestVersion(): Promise<string | null> {
  if (cachedLatestVersion && Date.now() - cachedAt < CACHE_TTL_MS) {
    return cachedLatestVersion;
  }
  try {
    const resp = await fetch(GITHUB_RELEASES_URL, {
      headers: { Accept: "application/vnd.github+json" },
    });
    if (!resp.ok) return null;
    const data = await resp.json();
    cachedLatestVersion = data.tag_name ?? null;
    cachedAt = Date.now();
    return cachedLatestVersion;
  } catch {
    return null;
  }
}

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
  interrupted: { label: "Update interrupted", icon: XCircle, color: "text-warning" },
};

interface UpdateSectionProps {
  runtimeId: string;
  daemonId: string | null;
  currentVersion: string | null;
  isOnline: boolean;
}

export function UpdateSection({
  runtimeId,
  daemonId,
  currentVersion,
  isOnline,
}: UpdateSectionProps) {
  const scopeId = resolveUpdateScopeId(daemonId, runtimeId);
  const entry = useDaemonUpdateStore((s) => s.entries[scopeId]);
  const setEntry = useDaemonUpdateStore((s) => s.setEntry);
  const updateEntry = useDaemonUpdateStore((s) => s.updateEntry);
  const clearEntry = useDaemonUpdateStore((s) => s.clearEntry);
  const cleanupExpired = useDaemonUpdateStore((s) => s.cleanupExpired);

  const [latestVersion, setLatestVersion] = useState<string | null>(null);
  const [status, setStatus] = useState<RuntimeUpdateStatus | null>(entry?.status ?? null);
  const [error, setError] = useState(entry?.error ?? "");
  const [output, setOutput] = useState(entry?.output ?? "");
  const [updating, setUpdating] = useState(false);
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
    setError(entry?.error ?? "");
    setOutput(entry?.output ?? "");
    setUpdating(entry?.status === "pending" || entry?.status === "running");
  }, [entry]);

  // Fetch latest version on mount.
  useEffect(() => {
    fetchLatestVersion().then(setLatestVersion);
  }, []);

  const finishWithEntry = useCallback(
    (
      nextStatus: Extract<
        RuntimeUpdateStatus,
        "completed" | "failed" | "timeout" | "interrupted"
      >,
      patch: { output?: string; error?: string } = {},
    ) => {
      updateEntry(scopeId, {
        status: nextStatus,
        finishedAt: Date.now(),
        output: patch.output,
        error: patch.error,
      });
      setUpdating(false);
      cleanup();
    },
    [cleanup, scopeId, updateEntry],
  );

  const handleDismiss = useCallback(() => {
    cleanup();
    setNotice("");
    clearEntry(scopeId);
  }, [cleanup, clearEntry, scopeId]);

  const startPolling = useCallback(
    (requestRuntimeId: string, requestId: string) => {
      if (activeRequestIdRef.current === requestId) {
        return;
      }

      cleanup();
      activeRequestIdRef.current = requestId;
      pollFailuresRef.current = 0;
      setUpdating(true);

      const pollOnce = async () => {
        try {
          const result = await api.getUpdateResult(requestRuntimeId, requestId);
          pollFailuresRef.current = 0;
          updateEntry(scopeId, {
            status: result.status as RuntimeUpdateStatus,
          });

          if (result.status === "completed") {
            finishWithEntry("completed", {
              output: result.output ?? "",
            });
            return;
          }

          if (result.status === "failed" || result.status === "timeout") {
            finishWithEntry(result.status as "failed" | "timeout", {
              error: result.error ?? "Unknown error",
            });
          }
        } catch (e) {
          if (e instanceof ApiError && e.status === 404) {
            clearEntry(scopeId);
            setNotice("Previous update status expired");
            setUpdating(false);
            cleanup();
            return;
          }

          pollFailuresRef.current += 1;
          if (pollFailuresRef.current >= 5) {
            finishWithEntry("interrupted", {
              error: "Update polling was interrupted. Please check again.",
            });
          }
        }
      };

      void pollOnce();
      pollRef.current = setInterval(() => {
        void pollOnce();
      }, 2000);
    },
    [cleanup, clearEntry, finishWithEntry, scopeId, updateEntry],
  );

  useEffect(() => {
    if (!entry) return;
    if (!entry.requestId) return;

    const shouldClearCompleted =
      entry.status === "completed" &&
      currentVersion != null &&
      !isNewer(entry.targetVersion, currentVersion);

    if (shouldClearCompleted) {
      clearEntry(scopeId);
      return;
    }

    if (
      entry.status === "completed" ||
      entry.status === "failed" ||
      entry.status === "timeout"
    ) {
      return;
    }

    const recoverable =
      (entry.status === "pending" ||
        entry.status === "running" ||
        entry.status === "interrupted") &&
      Date.now() - entry.startedAt <= UPDATE_RECOVERY_WINDOW_MS;

    if (recoverable) {
      startPolling(entry.runtimeId, entry.requestId);
      return;
    }

    if (
      (entry.status === "pending" ||
        entry.status === "running" ||
        entry.status === "interrupted") &&
      Date.now() - entry.startedAt > UPDATE_RECOVERY_WINDOW_MS
    ) {
      finishWithEntry("timeout", {
        error: "Update status expired before it could be restored.",
      });
    }
  }, [clearEntry, currentVersion, entry, finishWithEntry, scopeId, startPolling]);

  const handleUpdate = async () => {
    if (!latestVersion) return;
    cleanup();
    setNotice("");
    setUpdating(true);
    setStatus("pending");
    setError("");
    setOutput("");

    try {
      const update = await api.initiateUpdate(runtimeId, latestVersion);
      setEntry({
        daemonId: scopeId,
        runtimeId,
        requestId: update.id,
        targetVersion: latestVersion,
        status: "pending",
        startedAt: Date.now(),
      });
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        setStatus(null);
        setUpdating(false);
        setNotice("An update is already in progress, please wait");
        return;
      }

      setStatus("failed");
      setError("Failed to initiate update");
      setUpdating(false);
    }
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

      {notice && <p className="text-xs text-muted-foreground">{notice}</p>}

      {status === "completed" && output && (
        <div className="rounded-lg border bg-success/5 px-3 py-2">
          <p className="text-xs text-success">{output}</p>
        </div>
      )}

      {(status === "failed" || status === "timeout" || status === "interrupted") && error && (
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2">
          <p className="text-xs text-destructive">{error}</p>
          <div className="mt-1 flex items-center gap-2">
            <Button
              variant="ghost"
              size="xs"
              onClick={handleUpdate}
            >
              Retry
            </Button>
            <Button
              variant="ghost"
              size="xs"
              onClick={handleDismiss}
            >
              Dismiss
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
