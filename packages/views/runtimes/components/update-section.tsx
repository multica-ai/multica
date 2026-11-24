import { useState, useEffect } from "react";
import {
  Loader2,
  CheckCircle2,
  XCircle,
  ArrowUpCircle,
  Check,
  Lock,
  Clock,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { api, ApiError } from "@multica/core/api";
import type { RuntimeUpdate, RuntimeUpdateStatus } from "@multica/core/types";
import { useT } from "../../i18n";

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

/**
 * Parses a released CLI version ("v0.4.17" / "0.4.17") into comparable parts,
 * or null when the string is not a release version.
 *
 * A daemon built from source reports a `git describe` string
 * ("v0.4.17-12-gabc1234") or the ldflags default ("dev"), and neither can be
 * ordered against a release tag. This mirrors `IsReleaseVersion` in
 * server/internal/cli/update.go, which is how the daemon's own auto-update
 * loop decides the same question.
 */
function parseReleaseVersion(v: string): number[] | null {
  const parts = v.trim().replace(/^v/, "").split(".");
  if (parts.length !== 3) return null;
  const parsed: number[] = [];
  for (const part of parts) {
    if (!/^\d+$/.test(part)) return null;
    parsed.push(Number(part));
  }
  return parsed;
}

/**
 * True when `latest` is strictly newer than `current`.
 *
 * An unparseable version on either side compares as "no update available".
 * Number("dev") is NaN, and every NaN comparison is false, so the old
 * component-wise scan fell through to the next component and reported an
 * upgrade for a version string it had never actually read — inviting the
 * operator to replace a locally built binary on the strength of a claim we
 * could not make.
 */
function isNewer(latest: string, current: string): boolean {
  const l = parseReleaseVersion(latest);
  const c = parseReleaseVersion(current);
  if (!l || !c) return false;
  for (const [i, lv] of l.entries()) {
    const cv = c[i] ?? 0;
    if (lv > cv) return true;
    if (lv < cv) return false;
  }
  return false;
}

type ShownStatus = "pending" | "scheduled" | "running" | "completed" | "failed" | "timeout";

const statusConfig: Record<ShownStatus, { icon: typeof Loader2; color: string }> = {
  pending: { icon: Loader2, color: "text-muted-foreground" },
  scheduled: { icon: Clock, color: "text-muted-foreground" },
  running: { icon: Loader2, color: "text-info" },
  completed: { icon: CheckCircle2, color: "text-success" },
  failed: { icon: XCircle, color: "text-destructive" },
  timeout: { icon: XCircle, color: "text-warning" },
};

const UNFINISHED: ReadonlySet<RuntimeUpdateStatus> = new Set(["pending", "scheduled", "running"]);

type Note = "expired" | "busy" | "another_update" | "cancel_failed" | "cancel_conflict" | "initiate_failed";

/**
 * A failure that is not the machine's fault — it was busy (a daemon that
 * cannot queue) or already has an update — reads as a neutral note.
 */
function neutralFailure(update: RuntimeUpdate): Note | null {
  if (update.status === "expired") return "expired";
  if (update.status !== "failed") return null;
  const error = update.error ?? "";
  if (error.includes("agent work is starting or still active")) return "busy";
  if (error.includes("already queued or in progress") || error.includes("already in progress")) {
    return "another_update";
  }
  return null;
}

function sameReleaseVersion(a: string, b: string): boolean {
  const pa = parseReleaseVersion(a);
  const pb = parseReleaseVersion(b);
  return !!pa && !!pb && pa.every((part, i) => part === pb[i]);
}

interface UpdateSectionProps {
  /** Null for a read-only viewer who cannot use a runtime as the command channel. */
  runtimeId: string | null;
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
  const { t } = useT("runtimes");
  const isManaged = launchedBy === "desktop";
  const [latestVersion, setLatestVersion] = useState<string | null>(null);
  // The machine's update as the server last reported it.
  const [update, setUpdate] = useState<RuntimeUpdate | null>(null);
  const [note, setNote] = useState<Note | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [cancelling, setCancelling] = useState(false);

  // Fetch latest version on mount.
  useEffect(() => {
    fetchLatestVersion().then(setLatestVersion);
  }, []);

  // A reload, or another runtime of the same daemon, picks up the queue.
  // Switching machines replaces whatever the previous one showed.
  useEffect(() => {
    setUpdate(null);
    setNote(null);
    if (!runtimeId) return;
    let stale = false;
    api
      .getActiveUpdate(runtimeId)
      .then((active) => {
        // Keep an update this page started while the lookup was in flight.
        if (!stale) setUpdate((prev) => active ?? prev);
      })
      .catch(() => {});
    return () => {
      stale = true;
    };
  }, [runtimeId]);

  const updateId = update?.id ?? null;
  const polling = !!update && UNFINISHED.has(update.status);
  const queued = update?.status === "scheduled";
  useEffect(() => {
    if (!runtimeId || !updateId || !polling) return;
    const timer = setInterval(
      async () => {
        try {
          const next = await api.getUpdateResult(runtimeId, updateId);
          setUpdate((prev) => (prev?.id === next.id ? next : prev));
        } catch {
          // ignore poll errors
        }
      },
      queued ? 10_000 : 2_000,
    );
    return () => clearInterval(timer);
  }, [runtimeId, updateId, polling, queued]);

  // The daemon restarts right after installing, often before its report
  // lands; the runtime reporting the target version is proof enough. Only an
  // exact match counts: a CLI request may target an older version.
  useEffect(() => {
    if (update?.status !== "running" || !currentVersion) return;
    if (sameReleaseVersion(update.target_version, currentVersion)) {
      setUpdate({ ...update, status: "completed", output: `Updated to ${update.target_version}` });
    }
  }, [currentVersion, update]);

  // Let the re-fetched runtime show the new version after a moment.
  useEffect(() => {
    if (update?.status !== "completed") return;
    const timer = setTimeout(() => setUpdate(null), 5000);
    return () => clearTimeout(timer);
  }, [update?.status]);

  const handleUpdate = async () => {
    if (!latestVersion || !runtimeId) return;
    setSubmitting(true);
    setNote(null);
    try {
      setUpdate(await api.initiateUpdate(runtimeId, latestVersion));
    } catch (err) {
      // 409: the machine already has an update (e.g. from another tab); show it.
      const active =
        err instanceof ApiError && err.status === 409
          ? await api.getActiveUpdate(runtimeId).catch(() => null)
          : null;
      setUpdate(active);
      if (!active) setNote("initiate_failed");
    } finally {
      setSubmitting(false);
    }
  };

  const handleCancel = async () => {
    if (!runtimeId || !update) return;
    setCancelling(true);
    setNote(null);
    try {
      const cancelled = await api.cancelUpdate(runtimeId, update.id);
      setUpdate(cancelled.status === "cancelled" ? null : cancelled);
    } catch (err) {
      const conflict = err instanceof ApiError && err.status === 409;
      const latest = await api.getUpdateResult(runtimeId, update.id).catch(() => null);
      if (latest) setUpdate(latest.status === "cancelled" ? null : latest);
      // A 409 means it started or ended meanwhile; only a start needs saying.
      if (!conflict) setNote("cancel_failed");
      else if (latest?.status === "running") setNote("cancel_conflict");
    } finally {
      setCancelling(false);
    }
  };

  const hasUpdate =
    currentVersion &&
    latestVersion &&
    isNewer(latestVersion, currentVersion);

  // A source build cannot be ordered against a release tag, so neither
  // "update available" nor "Latest" is a claim we can make. Say that, rather
  // than defaulting to "Latest" and telling the operator their local binary is
  // up to date when we never parsed its version.
  const isLocalBuild =
    !!currentVersion && parseReleaseVersion(currentVersion) === null;

  // Cancelled, expired, and neutral failures leave the card idle with a note.
  const neutral = update ? neutralFailure(update) : null;
  const status: ShownStatus | null =
    update && !neutral && update.status in statusConfig
      ? (update.status as ShownStatus)
      : null;
  const shownNote = note ?? neutral;
  const config = status ? statusConfig[status] : null;
  const Icon = config?.icon;
  const isActive = status === "pending" || status === "running";

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 flex-wrap">
        <span className="text-caption text-muted-foreground">{t(($) => $.update.cli_version_label)}</span>
        <span className="text-caption font-mono">
          {currentVersion ?? t(($) => $.update.version_unknown)}
        </span>

        {isManaged ? (
          <span
            className="inline-flex items-center gap-1 text-caption text-muted-foreground"
            title={t(($) => $.update.managed_by_desktop_title)}
          >
            {t(($) => $.update.managed_by_desktop)}
          </span>
        ) : (
          <>
            {isLocalBuild && !status && (
              <span
                className="inline-flex items-center gap-1 text-caption text-muted-foreground"
                title={t(($) => $.update.local_build_title)}
              >
                {t(($) => $.update.local_build)}
              </span>
            )}

            {!isLocalBuild &&
              !hasUpdate &&
              currentVersion &&
              latestVersion &&
              !status && (
                <span className="inline-flex items-center gap-1 text-caption text-success">
                  <Check className="h-3 w-3" />
                  {t(($) => $.update.latest)}
                </span>
              )}

            {hasUpdate && !status && (
              <>
                <span className="text-caption text-muted-foreground">→</span>
                <span className="text-caption font-mono text-info">
                  {latestVersion}
                </span>
                <span className="text-caption text-muted-foreground">{t(($) => $.update.available)}</span>
              </>
            )}

            {hasUpdate && !runtimeId && (
              <span
                className="inline-flex items-center gap-1 text-caption text-muted-foreground"
                title={t(($) => $.update.read_only_title)}
              >
                <Lock className="h-3 w-3" />
                {t(($) => $.update.read_only)}
              </span>
            )}

            {hasUpdate && runtimeId && isOnline && !status && (
              <Button
                variant="outline"
                size="xs"
                onClick={handleUpdate}
                disabled={submitting}
              >
                <ArrowUpCircle className="h-3 w-3" />
                {t(($) => $.update.action)}
              </Button>
            )}
          </>
        )}

        {config && Icon && status && (
          <span
            className={`inline-flex items-center gap-1 text-caption ${config.color}`}
          >
            <Icon className={`h-3 w-3 ${isActive ? "animate-spin" : ""}`} />
            {t(($) => $.update.status[status])}
            {status === "scheduled" && update?.target_version && (
              <span className="font-mono">→ {update.target_version}</span>
            )}
          </span>
        )}

        {update?.cancellable && UNFINISHED.has(update.status) && runtimeId && (
          <Button variant="ghost" size="xs" onClick={handleCancel} disabled={cancelling}>
            {t(($) => $.update.cancel)}
          </Button>
        )}
      </div>

      {status === "scheduled" && (
        <p className="text-caption text-muted-foreground">
          {update?.active_tasks
            ? `${t(($) => $.update.scheduled_tasks, { running: update.active_tasks })} · `
            : ""}
          {t(($) => $.update.note_no_interrupt)}
        </p>
      )}

      {status === "completed" && update?.output && (
        <div className="rounded-lg border bg-success/5 px-3 py-2">
          <p className="text-caption text-success">{update.output}</p>
        </div>
      )}

      {(status === "failed" || status === "timeout") && (
        <div className="rounded-lg border border-destructive/20 bg-destructive/5 px-3 py-2">
          <p className="text-caption text-destructive">
            {update?.error || t(($) => $.update.unknown_error)}
          </p>
          {status === "failed" && (
            <Button
              variant="ghost"
              size="xs"
              className="mt-1"
              onClick={handleUpdate}
            >
              {t(($) => $.update.retry)}
            </Button>
          )}
        </div>
      )}

      {shownNote && (
        <p
          className={`text-caption ${shownNote === "initiate_failed" ? "text-destructive" : "text-muted-foreground"}`}
        >
          {
            {
              expired: t(($) => $.update.expired_note),
              busy: t(($) => $.update.busy_note),
              another_update: t(($) => $.update.another_update_note),
              cancel_failed: t(($) => $.update.cancel_failed),
              cancel_conflict: t(($) => $.update.cancel_conflict),
              initiate_failed: t(($) => $.update.initiate_failed),
            }[shownNote]
          }
        </p>
      )}
    </div>
  );
}
