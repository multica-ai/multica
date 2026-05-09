"use client";

import { useMemo, useState } from "react";
import { ExternalLink, Pencil, Plus, Rocket } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Popover,
  PopoverContent,
  PopoverDescription,
  PopoverHeader,
  PopoverTitle,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import {
  useDeployEnvironments,
  useRecentDeploys,
} from "@multica/core/ship";
import type { Deploy, DeployEnvironment } from "@multica/core/types";
import { useT } from "../../i18n";
import { ConfigureDeployEnvDialog } from "./configure-deploy-env-dialog";
import { LogDeployDialog } from "./log-deploy-dialog";

interface ShipDeploySwimlanesProps {
  projectId: string;
}

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;
/** How many recent deploy pills we render per lane. Beyond this, the lane
 *  scrolls horizontally — fits ~6-8 pills on a typical desktop window. */
const PILLS_PER_LANE = 12;

type StatusKind =
  | "succeeded"
  | "failed"
  | "in_progress"
  | "pending"
  | "rolled_back"
  | "unknown";

function statusKind(status: string | undefined): StatusKind {
  switch (status) {
    case "succeeded":
      return "succeeded";
    case "failed":
      return "failed";
    case "in_progress":
      return "in_progress";
    case "pending":
      return "pending";
    case "rolled_back":
      return "rolled_back";
    default:
      return "unknown";
  }
}

/** Per-status pill chrome. Tailwind classes are listed inline (not via a
 *  utility map keyed by string) so the JIT compiler picks them up at build
 *  time — concatenating template strings would defeat purge.
 *
 *  The `in_progress` and `pending` pills get a subtle pulse animation via
 *  Tailwind's built-in `animate-pulse`; we deliberately stick to CSS-only
 *  animation to keep Framer Motion off the dep tree (per Phase 2 brief). */
function pillClasses(kind: StatusKind): string {
  switch (kind) {
    case "succeeded":
      return "border-emerald-500/40 bg-emerald-500/15 text-emerald-700 dark:text-emerald-300";
    case "failed":
      return "border-destructive/40 bg-destructive/15 text-destructive";
    case "in_progress":
      return "border-amber-500/40 bg-amber-500/15 text-amber-700 dark:text-amber-300 animate-pulse";
    case "pending":
      return "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300 animate-pulse";
    case "rolled_back":
      return "border-orange-500/40 bg-orange-500/15 text-orange-700 dark:text-orange-300";
    case "unknown":
    default:
      return "border-muted-foreground/20 bg-muted/30 text-muted-foreground";
  }
}

/** Glyph rendered inside the pill so the status reads even on the
 *  smallest size. Returned as a static string so we don't bloat the
 *  dep tree with another lucide icon import per status. */
function pillGlyph(kind: StatusKind): string {
  switch (kind) {
    case "succeeded":
      return "✓";
    case "failed":
      return "×";
    case "in_progress":
      return "⏳";
    case "pending":
      return "•";
    case "rolled_back":
      return "↺";
    case "unknown":
    default:
      return "?";
  }
}

function shortSha(sha: string | null | undefined): string {
  if (!sha) return "";
  return sha.slice(0, 7);
}

function formatRelative(iso: string | null | undefined, locale: string): string {
  if (!iso) return "";
  const then = new Date(iso).getTime();
  if (!Number.isFinite(then)) return "";
  const diff = Math.max(1, Math.round((Date.now() - then) / 1000));
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" });
  if (diff < 60) return rtf.format(-diff, "second");
  if (diff < 3600) return rtf.format(-Math.round(diff / 60), "minute");
  if (diff < 86400) return rtf.format(-Math.round(diff / 3600), "hour");
  if (diff < 86400 * 30) return rtf.format(-Math.round(diff / 86400), "day");
  return rtf.format(-Math.round(diff / 86400 / 30), "month");
}

interface DeployPillProps {
  deploy: Deploy;
  /** Repo URL is used to build the GitHub commit link in the popover. */
  repoUrl?: string;
}

function DeployPill({ deploy, repoUrl }: DeployPillProps) {
  const { t, i18n } = useT("ship");
  const kind = statusKind(deploy.status);
  const sha = shortSha(deploy.sha);
  const commitUrl =
    repoUrl && deploy.sha
      ? `${repoUrl.replace(/\.git$/, "")}/commit/${deploy.sha}`
      : null;

  // Map deploy.status (string-typed for drift) to a translation key. We
  // explicitly include a `default` branch so a future server-side enum
  // value renders as "Unknown" rather than a missing-key warning.
  const statusLabelKey =
    kind === "succeeded"
      ? "status_succeeded"
      : kind === "failed"
        ? "status_failed"
        : kind === "in_progress"
          ? "status_in_progress"
          : kind === "pending"
            ? "status_pending"
            : kind === "rolled_back"
              ? "status_rolled_back"
              : "status_unknown";

  return (
    <Popover>
      <PopoverTrigger
        render={
          <button
            type="button"
            className={cn(
              "inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-[11px] font-mono tabular-nums transition-colors hover:brightness-110",
              pillClasses(kind),
            )}
            aria-label={`${sha} ${deploy.status}`}
          >
            <span aria-hidden>{pillGlyph(kind)}</span>
            <span>{sha || "—"}</span>
          </button>
        }
      />
      <PopoverContent align="start" sideOffset={6}>
        <PopoverHeader>
          <PopoverTitle>
            {commitUrl ? (
              <a
                href={commitUrl}
                target="_blank"
                rel="noopener noreferrer"
                className="font-mono hover:underline"
              >
                {sha || "—"}
              </a>
            ) : (
              <span className="font-mono">{sha || "—"}</span>
            )}
          </PopoverTitle>
          <PopoverDescription>
            {t(($) => $.swimlane[statusLabelKey])}
          </PopoverDescription>
        </PopoverHeader>
        <dl className="mt-1 space-y-1 text-xs">
          <div className="flex items-center justify-between gap-4">
            <dt className="text-muted-foreground">
              {t(($) => $.swimlane.popover_status)}
            </dt>
            <dd>{t(($) => $.swimlane[statusLabelKey])}</dd>
          </div>
          {deploy.triggered_by && (
            <div className="flex items-center justify-between gap-4">
              <dt className="text-muted-foreground">
                {t(($) => $.swimlane.popover_triggered_by)}
              </dt>
              {/* Server returns a UUID for member-triggered deploys. We
                  could resolve to a name via the members cache, but Phase 2
                  keeps the popover dependency-free; show the abbreviated
                  id so the user has *something* to grep for. */}
              <dd className="font-mono text-[10px] text-muted-foreground">
                {deploy.triggered_by.slice(0, 8)}
              </dd>
            </div>
          )}
          <div className="flex items-center justify-between gap-4">
            <dt className="text-muted-foreground">
              {t(($) => $.swimlane.popover_triggered_at)}
            </dt>
            <dd>{formatRelative(deploy.triggered_at, i18n.language)}</dd>
          </div>
        </dl>
        {deploy.log_url ? (
          // Render as an anchor so the click semantics open in a new tab
          // without a button-inside-anchor nesting violation. The styling
          // mimics Button outline+sm; we keep it inline rather than
          // adding an asChild prop to the design-system Button.
          <a
            href={deploy.log_url}
            target="_blank"
            rel="noopener noreferrer"
            className="mt-1 inline-flex w-full items-center gap-1 rounded-lg border bg-background px-2.5 py-1 text-[0.8rem] hover:bg-accent"
          >
            <ExternalLink className="size-3" />
            {t(($) => $.swimlane.popover_open_log)}
          </a>
        ) : (
          <div className="mt-1 text-[10px] text-muted-foreground">
            {t(($) => $.swimlane.popover_no_log)}
          </div>
        )}
        {deploy.error_message && (
          <div className="mt-1 rounded bg-destructive/10 p-1.5 text-[11px] text-destructive">
            {deploy.error_message}
          </div>
        )}
      </PopoverContent>
    </Popover>
  );
}

interface SwimlaneProps {
  env: DeployEnvironment;
  /** Repo URL from the project resource — used to build GitHub commit links
   *  in pill popovers. */
  repoUrl?: string;
}

/** A single environment row. The row aggregates summary stats on the
 *  right (in-flight count, last deployed, health pill) and renders the
 *  N most recent deploy pills horizontally on the left. The "now"
 *  indicator is a faint vertical line at the right edge of the pill row,
 *  giving a sense of "time flowing left → right". */
function Swimlane({ env, repoUrl }: SwimlaneProps) {
  const { t, i18n } = useT("ship");
  const { data: deploysData } = useRecentDeploys(env.id, 50);
  const deploys: Deploy[] = useMemo(
    () => deploysData?.deploys ?? [],
    [deploysData],
  );

  const visibleDeploys = useMemo(
    () => deploys.slice(0, PILLS_PER_LANE),
    [deploys],
  );

  const inFlight = useMemo(
    () =>
      deploys.filter(
        (d) => d.status === "in_progress" || d.status === "pending",
      ).length,
    [deploys],
  );

  const recentCount = useMemo(
    () =>
      deploys.filter((d) => {
        const triggered = new Date(d.triggered_at).getTime();
        return (
          Number.isFinite(triggered) && Date.now() - triggered < SEVEN_DAYS_MS
        );
      }).length,
    [deploys],
  );

  // Health bucket — derived from the LATEST deploy's status, then folded
  // through the in-flight count so a successful prod deploy followed by
  // an in-progress staging deploy still reads "awaiting".
  const latest = deploys[0];
  const health: "healthy" | "failing" | "awaiting" | "idle" = !latest
    ? "idle"
    : inFlight > 0
      ? "awaiting"
      : latest.status === "failed"
        ? "failing"
        : "healthy";

  const healthClass =
    health === "healthy"
      ? "text-emerald-600 dark:text-emerald-400"
      : health === "failing"
        ? "text-destructive"
        : health === "awaiting"
          ? "text-amber-600 dark:text-amber-400"
          : "text-muted-foreground";

  const [configOpen, setConfigOpen] = useState(false);
  const [logOpen, setLogOpen] = useState(false);

  const laneLabel =
    env.kind === "production"
      ? t(($) => $.swimlane.lane_production)
      : t(($) => $.swimlane.lane_staging);

  return (
    <div className="rounded-md border bg-card p-3 text-card-foreground">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium">{laneLabel}</span>
          <span className="text-xs text-muted-foreground">·</span>
          <span className="truncate text-xs text-muted-foreground">{env.name}</span>
        </div>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            variant="outline"
            onClick={() => setLogOpen(true)}
          >
            <Rocket className="size-3" />
            {t(($) => $.swimlane.log_deploy)}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setConfigOpen(true)}
            aria-label={t(($) => $.swimlane.edit_environment)}
          >
            <Pencil className="size-3" />
          </Button>
        </div>
      </div>

      {/* Stats line — in-flight count, last-deployed, health bucket */}
      <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
        <span className="tabular-nums">
          {t(($) => $.swimlane.in_flight_count, { count: inFlight })}
        </span>
        <span aria-hidden>·</span>
        {latest ? (
          <span>
            {t(($) => $.swimlane.last_deployed, {
              when: formatRelative(latest.triggered_at, i18n.language),
            })}
          </span>
        ) : (
          <span>{t(($) => $.swimlane.never_deployed)}</span>
        )}
        <span aria-hidden>·</span>
        <span className={cn("inline-flex items-center gap-1", healthClass)}>
          <span
            className={cn(
              "size-1.5 rounded-full",
              health === "healthy" && "bg-emerald-500",
              health === "failing" && "bg-destructive",
              health === "awaiting" && "bg-amber-500",
              health === "idle" && "bg-muted-foreground/40",
            )}
            aria-hidden
          />
          {health === "healthy" && t(($) => $.swimlane.health_healthy)}
          {health === "failing" && t(($) => $.swimlane.health_failing)}
          {health === "awaiting" && t(($) => $.swimlane.health_awaiting)}
          {health === "idle" && t(($) => $.swimlane.health_idle)}
        </span>
        {env.target_url && (
          <>
            <span aria-hidden>·</span>
            <a
              href={env.target_url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-1 hover:text-foreground"
            >
              <ExternalLink className="size-3" />
              {t(($) => $.deploy_strip.open_target)}
            </a>
          </>
        )}
        <span aria-hidden>·</span>
        <span className="tabular-nums">
          {t(($) => $.deploy_strip.recent_deploys_count, { count: recentCount })}
        </span>
      </div>

      {/* Pill rail. Wrapped in a relative container so the "now" indicator
          can position itself absolutely against the right edge. The rail
          scrolls horizontally on overflow rather than wrapping — feels
          more like a real timeline. */}
      <div className="relative mt-2">
        {visibleDeploys.length === 0 ? (
          <div className="rounded border border-dashed bg-muted/20 px-3 py-2 text-xs text-muted-foreground">
            {t(($) => $.swimlane.empty_lane)}
          </div>
        ) : (
          <>
            <div className="flex gap-1.5 overflow-x-auto pr-6 pb-1">
              {visibleDeploys.map((deploy) => (
                <DeployPill
                  key={deploy.id}
                  deploy={deploy}
                  repoUrl={repoUrl}
                />
              ))}
            </div>
            {/* "Now" indicator — a faint vertical line on the right edge.
                Decorative, so aria-hidden. We deliberately render this as
                a positioned div instead of a border so it doesn't shift
                layout when pills overflow. */}
            <div
              aria-hidden
              className="pointer-events-none absolute right-1 top-0 bottom-1 w-px bg-foreground/15"
              title={t(($) => $.swimlane.now_indicator)}
            />
          </>
        )}
      </div>

      <ConfigureDeployEnvDialog
        open={configOpen}
        onOpenChange={setConfigOpen}
        projectId={env.project_id}
        existing={env}
      />
      <LogDeployDialog
        open={logOpen}
        onOpenChange={setLogOpen}
        environment={env}
      />
    </div>
  );
}

export function ShipDeploySwimlanes({ projectId }: ShipDeploySwimlanesProps) {
  const { t } = useT("ship");
  const { data, isLoading } = useDeployEnvironments(projectId);
  const envs = useMemo(() => data?.environments ?? [], [data]);
  // Render staging on top, production below. Falling back to API order
  // when neither matches (e.g. preview environments in a future phase).
  const sorted = useMemo(() => {
    const staging = envs.filter((e) => e.kind === "staging");
    const production = envs.filter((e) => e.kind === "production");
    const other = envs.filter(
      (e) => e.kind !== "staging" && e.kind !== "production",
    );
    return [...staging, ...production, ...other];
  }, [envs]);

  const [createOpen, setCreateOpen] = useState(false);

  if (isLoading) {
    return (
      <div className="space-y-2">
        <div className="h-20 rounded-md border bg-muted/30" />
        <div className="h-20 rounded-md border bg-muted/30" />
      </div>
    );
  }

  if (sorted.length === 0) {
    return (
      <>
        <div className="flex items-center justify-between gap-3 rounded-md border border-dashed bg-muted/20 p-4">
          <p className="text-sm text-muted-foreground">
            {t(($) => $.deploy_strip.no_environments)}
          </p>
          <Button size="sm" variant="outline" onClick={() => setCreateOpen(true)}>
            <Plus className="size-3" />
            {t(($) => $.deploy_strip.configure_environments)}
          </Button>
        </div>
        <ConfigureDeployEnvDialog
          open={createOpen}
          onOpenChange={setCreateOpen}
          projectId={projectId}
        />
      </>
    );
  }

  return (
    <div className="space-y-2">
      {sorted.map((env) => (
        <Swimlane key={env.id} env={env} />
      ))}
    </div>
  );
}
