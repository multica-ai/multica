import { useState, useEffect, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Loader2,
  CheckCircle2,
  XCircle,
  ArrowUpCircle,
  Check,
  Clock,
  Hourglass,
  Info,
  Lock,
  RotateCw,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { ApiError } from "@multica/core/api";
import {
  knownRuntimeUpdateStatus,
  runtimeActiveUpdateOptions,
  runtimeUpdateKeys,
  runtimeUpdateOptions,
  useCancelRuntimeUpdate,
  useInitiateRuntimeUpdate,
} from "@multica/core/runtimes";
import type { RuntimeUpdate, RuntimeUpdateStatus } from "@multica/core/types";
import { useLocale, useT } from "../../i18n";

const GITHUB_RELEASES_URL =
  "https://api.github.com/repos/multica-ai/multica/releases/latest";
const CACHE_TTL_MS = 10 * 60 * 1000; // 10 minutes
// How long a finished update stays on screen before the section returns to
// the plain version row (by then the restarted daemon reports its version).
const COMPLETED_DISMISS_MS = 5000;

// Sent as a `failed` result by servers that predate queued updates. Never shown
// as-is: it is an internal reason, and it is not a failure the user caused.
const LEGACY_BUSY_ERROR_PREFIX = "runtime update deferred because agent work";

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

/** The running daemon already reports the version this update was for. */
function hasReached(target: string, current: string | null): boolean {
  if (!current) return false;
  if (!parseReleaseVersion(target) || !parseReleaseVersion(current)) return false;
  return !isNewer(target, current);
}

type ChipStatus = Exclude<RuntimeUpdateStatus, "cancelled">;

const chipConfig: Record<
  ChipStatus,
  { icon: typeof Loader2; className: string; spin?: boolean }
> = {
  pending: { icon: Loader2, className: "bg-muted text-muted-foreground", spin: true },
  scheduled: {
    icon: Hourglass,
    className: "bg-brand/9 text-brand ring-1 ring-inset ring-brand/22",
  },
  running: { icon: Loader2, className: "bg-info/9 text-info", spin: true },
  completed: { icon: CheckCircle2, className: "bg-success/10 text-success" },
  failed: { icon: XCircle, className: "bg-destructive/9 text-destructive" },
  timeout: { icon: XCircle, className: "bg-warning/12 text-warning" },
  expired: { icon: Clock, className: "bg-muted text-muted-foreground" },
};

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
  const locale = useLocale();
  const qc = useQueryClient();
  const isManaged = launchedBy === "desktop";
  const commandRuntimeId = isManaged ? null : runtimeId;
  const [latestVersion, setLatestVersion] = useState<string | null>(null);
  const [initiateFailed, setInitiateFailed] = useState(false);

  // The request this section follows: the one this viewer just started, or
  // the runtime's in-flight one found on mount (so a queued update survives a
  // reload and shows for every member who can manage the runtime). Once a
  // request is finished with — cancelled, or completed and acknowledged — it
  // is dismissed and the plain version row returns.
  const [startedId, setStartedId] = useState<string | null>(null);
  const [dismissedId, setDismissedId] = useState<string | null>(null);
  const activeQuery = useQuery(runtimeActiveUpdateOptions(commandRuntimeId));
  const candidateId = startedId ?? activeQuery.data?.id ?? null;
  const trackedId =
    candidateId && candidateId !== dismissedId ? candidateId : null;
  const seed =
    activeQuery.data && activeQuery.data.id === trackedId
      ? activeQuery.data
      : undefined;
  const updateQuery = useQuery({
    ...runtimeUpdateOptions(commandRuntimeId, trackedId),
    initialData: seed,
  });
  const update: RuntimeUpdate | null = trackedId
    ? (updateQuery.data ?? null)
    : null;
  const status = knownRuntimeUpdateStatus(update?.status);

  const initiate = useInitiateRuntimeUpdate(commandRuntimeId);
  const cancel = useCancelRuntimeUpdate(commandRuntimeId);

  useEffect(() => {
    fetchLatestVersion().then(setLatestVersion);
  }, []);

  // Leave the tracked request once there is nothing more to say about it.
  useEffect(() => {
    if (!update) return;
    if (status === "cancelled" || status === null) {
      setDismissedId(update.id);
      return;
    }
    if (
      status !== "failed" &&
      status !== "timeout" &&
      status !== "expired" &&
      hasReached(update.target_version, currentVersion)
    ) {
      setDismissedId(update.id);
      return;
    }
    if (status === "completed") {
      const timer = setTimeout(
        () => setDismissedId(update.id),
        COMPLETED_DISMISS_MS,
      );
      return () => clearTimeout(timer);
    }
    return undefined;
  }, [currentVersion, status, update]);

  const now = useNowWhile(status === "scheduled", 30_000);

  const handleUpdate = () => {
    if (!latestVersion || !commandRuntimeId) return;
    setInitiateFailed(false);
    initiate.mutateAsync(latestVersion).then(
      (created) => {
        setStartedId(created.id || null);
        setDismissedId(null);
      },
      (error: unknown) => {
        // Someone else already queued one: follow theirs instead.
        if (error instanceof ApiError && error.status === 409) {
          setStartedId(null);
          setDismissedId(null);
          void qc.invalidateQueries({
            queryKey: runtimeUpdateKeys.active(commandRuntimeId),
          });
          return;
        }
        setInitiateFailed(true);
      },
    );
  };

  const handleCancel = () => {
    if (!update) return;
    const id = update.id;
    cancel.mutateAsync(id).then(
      () => setDismissedId(id),
      // 409: installation already started. The mutation refetches, so the
      // section moves on to what actually happened.
      () => undefined,
    );
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

  const legacyBusy =
    status === "failed" &&
    !!update?.error?.startsWith(LEGACY_BUSY_ERROR_PREFIX);
  const inFlight =
    status === "pending" ||
    status === "scheduled" ||
    status === "running" ||
    status === "completed";
  const showsRequest = status !== null && status !== "cancelled";
  const canStart =
    !!hasUpdate &&
    !!commandRuntimeId &&
    isOnline &&
    !inFlight &&
    status !== "failed" &&
    status !== "timeout" &&
    !initiateFailed;
  const chip: ChipStatus | null =
    showsRequest && !legacyBusy ? (status as ChipStatus) : null;
  const targetVersion = update?.target_version || latestVersion;

  return (
    <div className="space-y-2.5">
      <div className="flex min-h-6 flex-wrap items-center gap-2">
        <span className="text-caption text-muted-foreground">
          {t(($) => $.update.cli_version_label)}
        </span>
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
            {isLocalBuild && !showsRequest && (
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
              !showsRequest && (
                <span className="inline-flex items-center gap-1 text-caption text-success">
                  <Check className="h-3 w-3" />
                  {t(($) => $.update.latest)}
                </span>
              )}

            {(showsRequest || hasUpdate) && targetVersion && (
              <>
                <span className="text-caption text-muted-foreground">→</span>
                <span className="text-caption font-mono text-info">
                  {targetVersion}
                </span>
              </>
            )}

            {hasUpdate && !showsRequest && (
              <span className="text-caption text-muted-foreground">
                {t(($) => $.update.available)}
              </span>
            )}

            {chip && <StatusChip status={chip} version={targetVersion ?? ""} />}

            {hasUpdate && !runtimeId && (
              <span
                className="inline-flex items-center gap-1 text-caption text-muted-foreground"
                title={t(($) => $.update.read_only_title)}
              >
                <Lock className="h-3 w-3" />
                {t(($) => $.update.read_only)}
              </span>
            )}

            {(status === "pending" || status === "scheduled") && (
              <Button
                variant="ghost"
                size="xs"
                className="ml-auto"
                onClick={handleCancel}
                disabled={cancel.isPending}
                aria-busy={cancel.isPending || undefined}
              >
                {t(($) => $.update.cancel)}
              </Button>
            )}

            {canStart && (
              <Button
                variant="outline"
                size="xs"
                className="ml-auto"
                onClick={handleUpdate}
                disabled={initiate.isPending}
                aria-busy={initiate.isPending || undefined}
              >
                <ArrowUpCircle />
                {t(($) => $.update.action)}
              </Button>
            )}
          </>
        )}
      </div>

      {update &&
        (status === "scheduled" ||
          status === "running" ||
          status === "completed") && (
          <UpdateTrack
            update={update}
            status={status}
            locale={locale}
            now={now}
          />
        )}

      {status === "expired" && (
        <NoteBox>{t(($) => $.update.expired_note)}</NoteBox>
      )}

      {legacyBusy && (
        <NoteBox action={<RetryButton onClick={handleUpdate} />}>
          {t(($) => $.update.busy_note)}
        </NoteBox>
      )}

      {(status === "failed" || status === "timeout") && !legacyBusy && (
        <ErrorBox onRetry={handleUpdate}>
          {update?.error || t(($) => $.update.unknown_error)}
        </ErrorBox>
      )}

      {initiateFailed && !showsRequest && (
        <ErrorBox onRetry={handleUpdate}>
          {t(($) => $.update.initiate_failed)}
        </ErrorBox>
      )}
    </div>
  );
}

function StatusChip({ status, version }: { status: ChipStatus; version: string }) {
  const { t } = useT("runtimes");
  const config = chipConfig[status];
  const Icon = config.icon;
  const label =
    status === "completed"
      ? t(($) => $.update.status.completed, { version })
      : t(($) => $.update.status[status]);
  return (
    <span
      className={cn(
        "inline-flex h-5.5 items-center gap-1.5 rounded-md px-2 text-caption font-medium whitespace-nowrap",
        config.className,
      )}
    >
      <Icon className={cn("size-3", config.spin && "animate-spin")} />
      {label}
    </span>
  );
}

type StepKind = "done" | "active" | "spinning" | "todo";

function UpdateTrack({
  update,
  status,
  locale,
  now,
}: {
  update: RuntimeUpdate;
  status: "scheduled" | "running" | "completed";
  locale: string;
  now: number;
}) {
  const { t } = useT("runtimes");
  const version = update.target_version;
  const queuedDetail = formatClock(update.created_at, locale);

  const elapsed = formatElapsed(
    update.scheduled_at || update.created_at,
    now,
    locale,
  );
  const waitDetail =
    typeof update.active_tasks === "number" ? (
      <span className="inline-flex items-start gap-1.5">
        <span
          aria-hidden="true"
          className="mt-[4.5px] size-1.5 shrink-0 rounded-full bg-success ring-3 ring-success/18"
        />
        {t(($) => $.update.steps.wait_detail_with_tasks, {
          running: update.active_tasks,
          elapsed,
        })}
      </span>
    ) : (
      t(($) => $.update.steps.wait_detail, { elapsed })
    );

  const steps: { kind: StepKind; title: string; detail?: ReactNode }[] =
    status === "scheduled"
      ? [
          { kind: "done", title: t(($) => $.update.steps.queued), detail: queuedDetail },
          { kind: "active", title: t(($) => $.update.steps.wait), detail: waitDetail },
          { kind: "todo", title: t(($) => $.update.steps.install, { version }) },
          { kind: "todo", title: t(($) => $.update.steps.restart) },
        ]
      : status === "running"
        ? [
            { kind: "done", title: t(($) => $.update.steps.queued), detail: queuedDetail },
            {
              kind: "done",
              title: t(($) => $.update.steps.tasks_finished),
              detail: formatClock(update.run_started_at, locale),
            },
            { kind: "spinning", title: t(($) => $.update.steps.install, { version }) },
            { kind: "todo", title: t(($) => $.update.steps.restart) },
          ]
        : [
            { kind: "done", title: t(($) => $.update.steps.queued), detail: queuedDetail },
            {
              kind: "done",
              title: t(($) => $.update.steps.tasks_finished),
              detail: formatClock(update.run_started_at, locale),
            },
            {
              kind: "done",
              title: t(($) => $.update.steps.install, { version }),
              detail: formatClock(update.updated_at, locale),
            },
            { kind: "spinning", title: t(($) => $.update.steps.restart) },
          ];

  return (
    <div className="flex flex-col gap-2.5 rounded-md bg-muted/60 px-3.5 pt-3 pb-2.5 dark:bg-muted/45">
      <ol className="flex">
        {steps.map((step, index) => {
          const last = index === steps.length - 1;
          return (
            <li
              key={index}
              className={cn(
                "flex min-w-0 flex-col gap-1.5",
                last ? "shrink-0" : "flex-1",
              )}
              aria-current={step.kind === "active" || step.kind === "spinning" ? "step" : undefined}
            >
              <div className="flex items-center gap-1.5">
                <StepIndicator kind={step.kind} />
                {!last && (
                  <span
                    aria-hidden="true"
                    className={cn(
                      "mr-1.5 h-[1.5px] flex-1 rounded-full",
                      step.kind === "done" ? "bg-brand" : "bg-input",
                    )}
                  />
                )}
              </div>
              <span
                className={cn(
                  "pr-3 text-caption break-words",
                  step.kind === "todo"
                    ? "text-muted-foreground"
                    : "font-medium text-foreground",
                )}
              >
                {step.title}
              </span>
              {step.detail ? (
                <span className="-mt-1 pr-3 text-micro text-muted-foreground tabular-nums">
                  {step.detail}
                </span>
              ) : null}
            </li>
          );
        })}
      </ol>
      {status === "scheduled" && (
        <p className="flex items-center gap-1.5 border-t pt-2 text-caption text-muted-foreground">
          <Info className="size-3.5 shrink-0 text-faint-foreground" />
          {t(($) => $.update.note_no_interrupt)}
        </p>
      )}
    </div>
  );
}

function StepIndicator({ kind }: { kind: StepKind }) {
  switch (kind) {
    case "done":
      return (
        <span className="flex size-4 shrink-0 items-center justify-center rounded-full bg-brand text-brand-foreground">
          <Check className="size-2.5 stroke-3" />
        </span>
      );
    case "active":
      return (
        <span className="relative flex size-4 shrink-0 items-center justify-center rounded-full ring-[1.5px] ring-brand ring-inset before:absolute before:-inset-1 before:rounded-full before:bg-brand/15">
          <span className="relative size-1.5 rounded-full bg-brand" />
        </span>
      );
    case "spinning":
      return <Loader2 className="size-4 shrink-0 animate-spin text-brand" />;
    case "todo":
      return (
        <span className="size-4 shrink-0 rounded-full ring-[1.5px] ring-input ring-inset" />
      );
  }
}

function NoteBox({
  children,
  action,
}: {
  children: ReactNode;
  action?: ReactNode;
}) {
  return (
    <div className="flex items-center gap-2 rounded-md bg-muted/60 py-2 pr-2.5 pl-3 dark:bg-muted/45">
      <Info className="size-3.5 shrink-0 text-muted-foreground" />
      <p className="flex-1 text-caption text-foreground">{children}</p>
      {action}
    </div>
  );
}

function ErrorBox({
  children,
  onRetry,
}: {
  children: ReactNode;
  onRetry: () => void;
}) {
  return (
    <div className="flex items-start gap-2.5 rounded-md border border-destructive/20 bg-destructive/5 py-2 pr-2.5 pl-3">
      <p className="mt-0.5 flex-1 text-caption break-words text-destructive">
        {children}
      </p>
      <RetryButton onClick={onRetry} />
    </div>
  );
}

function RetryButton({ onClick }: { onClick: () => void }) {
  const { t } = useT("runtimes");
  return (
    <Button variant="outline" size="xs" onClick={onClick}>
      <RotateCw />
      {t(($) => $.update.retry)}
    </Button>
  );
}

/** Re-renders every `intervalMs` while `active`, for elapsed-time labels. */
function useNowWhile(active: boolean, intervalMs: number): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [active, intervalMs]);
  return now;
}

function formatClock(iso: string | undefined, locale: string): string {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(locale, {
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

// "18 min" / "3 hr", localized by Intl so every locale gets its own units.
function formatElapsed(iso: string, now: number, locale: string): string {
  const start = new Date(iso).getTime();
  const minutes = Number.isNaN(start)
    ? 1
    : Math.max(1, Math.floor((now - start) / 60_000));
  const [value, unit] =
    minutes < 60 ? [minutes, "minute"] : [Math.floor(minutes / 60), "hour"];
  return new Intl.NumberFormat(locale, {
    style: "unit",
    unit,
    unitDisplay: "short",
  }).format(value);
}
