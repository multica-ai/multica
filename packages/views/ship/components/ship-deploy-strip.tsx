"use client";

import { useMemo, useState } from "react";
import { ExternalLink, Pencil, Plus, Rocket } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import {
  useDeployEnvironments,
  useRecentDeploys,
} from "@multica/core/ship";
import type { Deploy, DeployEnvironment } from "@multica/core/types";
import { useT } from "../../i18n";
import { ConfigureDeployEnvDialog } from "./configure-deploy-env-dialog";
import { LogDeployDialog } from "./log-deploy-dialog";

interface ShipDeployStripProps {
  projectId: string;
}

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000;

function statusDotClass(status: string | undefined): string {
  switch (status) {
    case "succeeded":
      return "bg-emerald-500";
    case "failed":
      return "bg-destructive";
    case "in_progress":
    case "pending":
      return "bg-amber-500";
    case "rolled_back":
      return "bg-orange-500";
    default:
      return "bg-muted";
  }
}

function shortSha(sha: string | null): string {
  if (!sha) return "";
  return sha.slice(0, 7);
}

function formatRelative(iso: string | null, locale: string): string {
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

interface DeployLaneProps {
  env: DeployEnvironment;
  /** Repo URL from the project resource — used to build the GitHub commit
   *  link. Optional because the lane can render without a SHA. */
  repoUrl?: string;
}

function DeployLane({ env, repoUrl }: DeployLaneProps) {
  const { t, i18n } = useT("ship");
  const { data: deploysData } = useRecentDeploys(env.id, 50);
  const deploys: Deploy[] = useMemo(
    () => deploysData?.deploys ?? [],
    [deploysData],
  );
  const latest = deploys[0];
  const recentCount = deploys.filter((d) => {
    const triggered = new Date(d.triggered_at).getTime();
    return Number.isFinite(triggered) && Date.now() - triggered < SEVEN_DAYS_MS;
  }).length;
  const [configOpen, setConfigOpen] = useState(false);
  const [logOpen, setLogOpen] = useState(false);

  const sha = env.current_sha ?? latest?.sha ?? null;
  const commitUrl = repoUrl && sha ? `${repoUrl.replace(/\.git$/, "")}/commit/${sha}` : null;

  const laneLabel =
    env.kind === "production"
      ? t(($) => $.deploy_strip.lane_production)
      : t(($) => $.deploy_strip.lane_staging);

  return (
    <div className="flex flex-col gap-2 rounded-md border bg-card p-3 text-card-foreground sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 items-center gap-3">
        <span className={cn("size-2 rounded-full", statusDotClass(latest?.status))} aria-hidden />
        <div className="min-w-0">
          <div className="flex items-center gap-2 text-sm font-medium">
            <span>{laneLabel}</span>
            <span className="text-xs text-muted-foreground">·</span>
            <span className="truncate text-xs text-muted-foreground">{env.name}</span>
          </div>
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
            {sha ? (
              commitUrl ? (
                <a
                  href={commitUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="font-mono tabular-nums hover:text-foreground"
                >
                  {t(($) => $.deploy_strip.current_sha)} {shortSha(sha)}
                </a>
              ) : (
                <span className="font-mono tabular-nums">
                  {t(($) => $.deploy_strip.current_sha)} {shortSha(sha)}
                </span>
              )
            ) : (
              <span>{t(($) => $.deploy_strip.never_deployed)}</span>
            )}
            {env.current_deployed_at && (
              <span>
                {t(($) => $.deploy_strip.deployed_at, {
                  when: formatRelative(env.current_deployed_at, i18n.language),
                })}
              </span>
            )}
            <span>
              {t(($) => $.deploy_strip.recent_deploys_count, {
                count: recentCount,
              })}
            </span>
            {env.target_url && (
              <a
                href={env.target_url}
                target="_blank"
                rel="noopener noreferrer"
                className="inline-flex items-center gap-1 hover:text-foreground"
              >
                <ExternalLink className="size-3" />
                {t(($) => $.deploy_strip.open_target)}
              </a>
            )}
          </div>
        </div>
      </div>

      <div className="flex items-center gap-2">
        <Button
          size="sm"
          variant="outline"
          onClick={() => setLogOpen(true)}
        >
          <Rocket className="size-3" />
          {t(($) => $.deploy_strip.log_deploy)}
        </Button>
        <Button
          size="sm"
          variant="ghost"
          onClick={() => setConfigOpen(true)}
          aria-label={t(($) => $.deploy_strip.edit_environment)}
        >
          <Pencil className="size-3" />
        </Button>
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

export function ShipDeployStrip({ projectId }: ShipDeployStripProps) {
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
        <div className="h-16 rounded-md border bg-muted/30" />
        <div className="h-16 rounded-md border bg-muted/30" />
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
        <DeployLane key={env.id} env={env} />
      ))}
    </div>
  );
}
