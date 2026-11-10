"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { AppWindow, ArrowUpRight, ChevronRight, Copy, ExternalLink, Globe, Laptop, Server } from "@multica/ui/icons";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueEnvironmentsOptions } from "@multica/core/issues/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type { IssueEnvironment, IssueEnvironmentState } from "@multica/core/types";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { useT, useTimeAgo } from "../../i18n";
import { canFocusLocalEnvironments, focusLocalEnvironment } from "../../platform/environment-focus";
import { useLocalDaemonStatus } from "../../platform/use-local-daemon-status";
import { buildRuntimeMachines, type RuntimeMachine } from "../../runtimes/components/runtime-machines";
import { IssueSidebarSectionHeader } from "./issue-sidebar-section";

type IssuesT = ReturnType<typeof useT<"issues">>["t"];

const STATE_TONE: Record<IssueEnvironmentState, string> = {
  starting: "text-warning",
  running: "text-success",
  unreachable: "text-warning",
  exited: "text-destructive",
  stopped: "text-muted-foreground",
};

export const isLive = (env: IssueEnvironment) => env.ended_at === null;
const isApp = (env: IssueEnvironment) => env.kind === "app";

type MachineGroup = { key: string; machine: RuntimeMachine | null; envs: IssueEnvironment[] };

/**
 * "Environments" in the issue sidebar: the servers this issue's runs left
 * running — declared in the repository's `.multica/environments.toml` or
 * found listening in a run's directory — grouped by the machine they run on,
 * because a localhost URL only opens there. A desktop app has no URL; Desktop
 * brings its window forward, but only through the daemon that found it.
 * Renders nothing until a run starts one.
 */
export function IssueEnvironmentsSection({ issueId }: { issueId: string }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const [open, setOpen] = useState(true);
  const [showEnded, setShowEnded] = useState(false);
  const { data } = useQuery(issueEnvironmentsOptions(wsId, issueId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const local = useLocalDaemonStatus();
  const currentUserId = useAuthStore((state) => state.user?.id ?? null);

  const machineByRuntime = useMemo(() => {
    const machines = buildRuntimeMachines(runtimes, {
      now: Date.now(),
      localDaemonId: local.daemonId,
      localMachineName: local.deviceName,
      currentUserId,
    });
    const byRuntime = new Map<string, RuntimeMachine>();
    for (const machine of machines) {
      for (const runtime of machine.runtimes) byRuntime.set(runtime.id, machine);
    }
    return byRuntime;
  }, [runtimes, local.daemonId, local.deviceName, currentUserId]);

  const envs = data?.environments ?? [];
  if (envs.length === 0) return null;

  const live = envs.filter(isLive);
  const ended = envs.filter((env) => !isLive(env));
  const running = live.filter((env) => env.state === "running").length;
  // Only Desktop knows which machine it is on. Anywhere else, every
  // localhost URL may belong to another machine, so every row says whose it is.
  const knowsLocal = !!local.daemonId;
  const localhostHint = (machine: RuntimeMachine | null) => {
    if (knowsLocal && machine?.isCurrent) return null;
    return machine
      ? t(($) => $.environments.remote_hint, { machine: machine.title })
      : t(($) => $.environments.localhost_hint);
  };
  // Only the daemon that detected an app knows its process, so a machine
  // that is merely named like this one does not count.
  const focusesHere = (machine: RuntimeMachine | null) =>
    canFocusLocalEnvironments() && knowsLocal && machine?.daemonId === local.daemonId;
  const appHint = (machine: RuntimeMachine | null) => {
    if (focusesHere(machine)) return null;
    return machine && !(knowsLocal && machine.isCurrent)
      ? t(($) => $.environments.app_remote_hint, { machine: machine.title })
      : t(($) => $.environments.app_desktop_hint);
  };

  const groups = (list: IssueEnvironment[]) => {
    const byMachine = new Map<string, MachineGroup>();
    for (const env of list) {
      const machine = env.runtime_id ? machineByRuntime.get(env.runtime_id) ?? null : null;
      const key = machine?.id ?? "unknown";
      const group = byMachine.get(key) ?? { key, machine, envs: [] };
      group.envs.push(env);
      byMachine.set(key, group);
    }
    return [...byMachine.values()].sort((a, b) => Number(b.machine?.isCurrent ?? false) - Number(a.machine?.isCurrent ?? false));
  };

  const renderGroups = (list: IssueEnvironment[]) =>
    groups(list).map((group) => (
      <div key={group.key}>
        <div className="flex min-h-7 items-center gap-1.5 px-2 text-micro text-muted-foreground">
          <Laptop className="size-3 shrink-0" />
          <span className="min-w-0 truncate">{group.machine?.title ?? t(($) => $.environments.unknown_machine)}</span>
          {group.machine?.isCurrent ? (
            <span className="shrink-0 rounded-xs bg-muted px-1 text-micro font-medium">{t(($) => $.environments.this_machine)}</span>
          ) : null}
        </div>
        {group.envs.map((env) => (
          <EnvironmentRow
            key={env.id}
            env={env}
            localhostHint={localhostHint(group.machine)}
            appHint={appHint(group.machine)}
          />
        ))}
      </div>
    ));

  return (
    <div>
      <IssueSidebarSectionHeader
        title={t(($) => $.environments.section_title)}
        open={open}
        onOpenChange={setOpen}
        count={live.length}
        status={running > 0 ? (
          <span className="inline-flex shrink-0 items-center gap-1 text-caption text-success tabular-nums">
            <span aria-hidden="true" className="size-1.5 rounded-full bg-success" />
            {t(($) => $.environments.running_count, { count: running })}
          </span>
        ) : undefined}
      />
      {open && (
        <div className="px-2">
          {renderGroups(live)}
          {ended.length > 0 && (
            <>
              <button
                type="button"
                aria-expanded={showEnded}
                onClick={() => setShowEnded((v) => !v)}
                className="flex w-full items-center gap-1 rounded-md px-2 py-1 text-left text-micro text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground"
              >
                {t(($) => $.environments.ended_count, { count: ended.length })}
                <ChevronRight aria-hidden="true" className={cn("size-3 text-faint-foreground transition-transform motion-reduce:transition-none", showEnded && "rotate-90")} />
              </button>
              {showEnded && renderGroups(ended)}
            </>
          )}
        </div>
      )}
    </div>
  );
}

function EnvironmentRow({
  env,
  localhostHint,
  appHint,
}: {
  env: IssueEnvironment;
  localhostHint: string | null;
  /** Why this machine cannot switch to an app's window; null when it can. */
  appHint: string | null;
}) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const live = isLive(env);
  const app = isApp(env);
  const openable = !app && live && env.url !== "" && env.state !== "unreachable";
  // Without the daemon's key for it there is nothing to ask the daemon for.
  const focusable = app && live && env.state === "running" && appHint === null && env.local_key !== "";
  const Icon = app ? AppWindow : env.url ? Globe : Server;
  const host = !app && env.url ? displayHost(env.url) : null;
  const title = app || env.source === "config" ? env.label || env.name : host ?? env.name;

  const parts: string[] = [];
  if (env.source === "config") {
    if (host) parts.push(host);
  } else if (!app) {
    parts.push(env.name);
  }
  const meta = environmentMeta(env, t, timeAgo);
  const tooltip = [
    env.source === "detected" ? t(($) => $.environments.detected_hint) : null,
    env.detail || null,
    app ? appHint : isLoopbackUrl(env.url) ? localhostHint : null,
  ].filter(Boolean).join("\n");

  const focus = () => {
    void focusLocalEnvironment(env.local_key).then((result) => {
      if (result.ok) return;
      if (result.reason === "gone") toast.error(t(($) => $.environments.focus_gone, { name: title }));
      else if (result.reason === "unsupported") toast.error(t(($) => $.environments.focus_unsupported));
      else if (result.reason === "unavailable") toast.error(t(($) => $.environments.focus_unavailable));
      else toast.error(t(($) => $.environments.focus_failed, { name: title }));
    });
  };

  const body = (
    <>
      <Icon className={cn("mt-px size-3.5 shrink-0", STATE_TONE[env.state] ?? "text-muted-foreground")} />
      <div className="min-w-0 flex-1">
        <p className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-label font-medium">{title}</span>
          <span
            className={cn(
              "shrink-0 text-caption",
              STATE_TONE[env.state] ?? "text-muted-foreground",
              (openable || focusable) && "group-hover/env:invisible group-has-[:focus-visible]/env:invisible [@media(pointer:coarse)]:invisible",
            )}
          >
            {t(($) => $.environments.state[env.state])}
          </span>
        </p>
        <p className="mt-1 flex min-w-0 items-center gap-1.5 overflow-hidden whitespace-nowrap text-caption text-muted-foreground tabular-nums">
          {parts.length > 0 && <span className="max-w-[70%] shrink-0 truncate">{parts.join(" · ")}</span>}
          {parts.length > 0 && meta && <span aria-hidden="true" className="text-faint-foreground">·</span>}
          {meta && <span className="min-w-0 truncate">{meta}</span>}
        </p>
      </div>
    </>
  );

  return (
    <div data-testid="environment-row" className="group/env relative rounded-md transition-colors hover:bg-accent/50" title={tooltip || undefined}>
      {openable ? (
        <a href={env.url} target="_blank" rel="noreferrer noopener" className="flex items-start gap-2 rounded-md px-2 py-2">
          {body}
        </a>
      ) : focusable ? (
        <button type="button" onClick={focus} className="flex w-full items-start gap-2 rounded-md px-2 py-2 text-left">
          {body}
        </button>
      ) : (
        <div className="flex items-start gap-2 px-2 py-2">{body}</div>
      )}
      {focusable && (
        <div className="absolute top-1 right-1 flex items-center gap-0.5 opacity-0 group-hover/env:opacity-100 group-has-[:focus-visible]/env:opacity-100 [@media(pointer:coarse)]:opacity-100">
          <IconButton variant="ghost" size="icon-xs" label={t(($) => $.environments.focus)} onClick={focus}>
            <ArrowUpRight />
          </IconButton>
        </div>
      )}
      {openable && (
        <div className="absolute top-1 right-1 flex items-center gap-0.5 opacity-0 group-hover/env:opacity-100 group-has-[:focus-visible]/env:opacity-100 [@media(pointer:coarse)]:opacity-100">
          <IconButton
            variant="ghost"
            size="icon-xs"
            label={t(($) => $.environments.open)}
            nativeButton={false}
            render={<a href={env.url} target="_blank" rel="noreferrer noopener" />}
          >
            <ExternalLink />
          </IconButton>
          <IconButton
            variant="ghost"
            size="icon-xs"
            label={t(($) => $.environments.copy_url)}
            onClick={() => {
              void copyText(env.url).then((ok) => {
                if (ok) toast.success(t(($) => $.environments.copied));
                else toast.error(t(($) => $.environments.copy_failed));
              });
            }}
          >
            <Copy />
          </IconButton>
        </div>
      )}
    </div>
  );
}

function environmentMeta(env: IssueEnvironment, t: IssuesT, timeAgo: (iso: string) => string): string | null {
  if (env.ended_at) {
    if (env.state === "exited" && env.detail) return env.detail;
    return t(($) => $.environments.ended_ago, { time: timeAgo(env.ended_at) });
  }
  if (env.state === "unreachable") return t(($) => $.environments.last_seen, { time: timeAgo(env.last_seen_at) });
  if (env.expires_at) {
    const minutes = Math.max(1, Math.round((new Date(env.expires_at).getTime() - Date.now()) / 60_000));
    if (minutes < 60) return t(($) => $.environments.expires_in_minutes, { count: minutes });
    const hours = Math.round(minutes / 60);
    if (hours < 48) return t(($) => $.environments.expires_in_hours, { count: hours });
    return t(($) => $.environments.expires_in_days, { count: Math.round(hours / 24) });
  }
  return t(($) => $.environments.started_ago, { time: timeAgo(env.started_at) });
}

function isLoopbackUrl(url: string): boolean {
  try {
    const host = new URL(url).hostname;
    return host === "localhost" || host.endsWith(".localhost") || host === "[::1]" || host === "0.0.0.0" || host.startsWith("127.");
  } catch {
    return false;
  }
}

function displayHost(url: string): string | null {
  try {
    const parsed = new URL(url);
    const path = parsed.pathname === "/" ? "" : parsed.pathname;
    return `${parsed.host}${path}`;
  } catch {
    return null;
  }
}
