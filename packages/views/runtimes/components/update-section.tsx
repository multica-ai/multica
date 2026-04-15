import { useState, useEffect, useCallback, useRef } from "react";
import {
  Loader2,
  CheckCircle2,
  XCircle,
  ArrowUpCircle,
  Check,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import type { RuntimeUpdateStatus } from "@multica/core/types";

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
};

interface UpdateSectionProps {
  runtimeId: string;
  currentVersion: string | null;
  isOnline: boolean;
  /**
   * Non-null when the daemon process was spawned by a managed launcher
   * (e.g. "desktop" for the Electron app). In that case the CLI binary
   * is shipped and upgraded by the launcher itself, so in-app self-update
   * is disabled — upgrading would be clobbered on the next launch anyway.
   */
  launchedBy?: string | null;
}

export function UpdateSection({
  runtimeId,
  currentVersion,
  isOnline,
  launchedBy,
}: UpdateSectionProps) {
  const isManaged = launchedBy === "desktop";
  const [latestVersion, setLatestVersion] = useState<string | null>(null);
  const [status, setStatus] = useState<RuntimeUpdateStatus | null>(null);
  const [error, setError] = useState("");
  const [output, setOutput] = useState("");
  const [updating, setUpdating] = useState(false);
  const pollRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const cleanup = useCallback(() => {
    if (pollRef.current) {
      clearInterval(pollRef.current);
      pollRef.current = null;
    }
  }, []);

  useEffect(() => cleanup, [cleanup]);

  // Fetch latest version on mount.
  useEffect(() => {
    fetchLatestVersion().then(setLatestVersion);
  }, []);

  const handleUpdate = async () => {
    if (!latestVersion) return;
    cleanup();
    setUpdating(true);
    setStatus("pending");
    setError("");
    setOutput("");

    try {
      const update = await api.initiateUpdate(runtimeId, latestVersion);

      pollRef.current = setInterval(async () => {
        try {
          const result = await api.getUpdateResult(runtimeId, update.id);
          setStatus(result.status as RuntimeUpdateStatus);

          if (result.status === "completed") {
            setOutput(result.output ?? "");
            setUpdating(false);
            cleanup();
            // Auto-clear status after a few seconds so the UI
            // refreshes to show the new version from the re-fetched runtime data.
            setTimeout(() => setStatus(null), 5000);
          } else if (
            result.status === "failed" ||
            result.status === "timeout"
          ) {
            setError(result.error ?? "Unknown error");
            setUpdating(false);
            cleanup();
          }
        } catch {
          // ignore poll errors
        }
      }, 2000);
    } catch {
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

        {isManaged ? (
          <span
            className="inline-flex items-center gap-1 text-xs text-muted-foreground"
            title="The CLI binary is managed by Multica Desktop — update Desktop to upgrade the CLI."
          >
            Managed by Desktop
          </span>
        ) : (
          <>
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
          </>
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
