"use client";

import { useMemo, useState, type ReactNode } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  AppWindow,
  ArrowUpRight,
  Bot,
  ChevronRight,
  Copy,
  ExternalLink,
  Globe,
  Laptop,
  MoreHorizontal,
  RotateCw,
  ScrollText,
  Server,
  Square,
  WifiOff,
} from "@multica/ui/icons";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useControlIssueEnvironment, type IssueEnvironmentControl } from "@multica/core/issues/environments";
import { useCreateComment } from "@multica/core/issues/mutations";
import { issueEnvironmentsOptions, issueKeys, issueTasksOptions } from "@multica/core/issues/queries";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import type {
  Agent,
  DeclaredEnvironment,
  IssueAssigneeType,
  IssueEnvironment,
  IssueEnvironmentActionRecord,
  IssueEnvironmentConfig,
  IssueEnvironmentState,
} from "@multica/core/types";
import { useActorName } from "@multica/core/workspace/hooks";
import { agentListOptions } from "@multica/core/workspace/queries";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Spinner } from "@multica/ui/components/ui/spinner";
import { copyText } from "@multica/ui/lib/clipboard";
import { cn } from "@multica/ui/lib/utils";
import { escapeMarkdownLabel } from "../../editor/utils/escape-markdown-label";
import { useT, useTimeAgo } from "../../i18n";
import { canFocusLocalEnvironments, focusLocalEnvironment } from "../../platform/environment-focus";
import { useLocalDaemonStatus } from "../../platform/use-local-daemon-status";
import { buildRuntimeMachines, type RuntimeMachine } from "../../runtimes/components/runtime-machines";
import { EnvironmentLogDialog } from "./environment-log-dialog";
import {
  displayHost,
  environmentTitle,
  exitCode,
  isLive,
  isLoopbackUrl,
  isPinnedCrash,
  pendingAction,
  preparingRun,
  recentFailure,
  runOrdinals,
} from "./environment-model";
import { EnvironmentStartMenu, type DeclaredState } from "./environment-start-menu";
import { IssueSidebarSectionHeader } from "./issue-sidebar-section";

type IssuesT = ReturnType<typeof useT<"issues">>["t"];

type RowState = IssueEnvironmentState | "stopping";

const STATE_TONE: Record<RowState, string> = {
  starting: "text-warning",
  running: "text-success",
  unreachable: "text-warning",
  exited: "text-destructive",
  stopped: "text-muted-foreground",
  stopping: "text-muted-foreground",
};

// Comments this tab asked an agent to start the environment with, so the
// "preparing" row shows before the run's summary is fetched.
const requestedStartComments = new Map<string, Set<string>>();

type MachineGroup<T> = { key: string; machine: RuntimeMachine | null; offline: boolean; items: T[] };

/** How a row can be acted on, given the machine and the caller. */
interface RowAccess {
  machineTitle: string | null;
  offline: boolean;
  localhostHint: string | null;
  /** Why this machine cannot switch to an app's window; null when it can. */
  appHint: string | null;
  originLabel: string | null;
}

/**
 * "Environments" in the issue sidebar: the servers this issue's runs left
 * running — declared in the repository's environment config or found
 * listening in a run's directory — grouped by the machine they run on,
 * because a localhost URL only opens there. A desktop app has no URL; Desktop
 * brings its window forward, but only through the daemon that found it.
 * Members who may use a machine
 * stop, restart and start its environments here; the machine carries the
 * command out and the row shows it pending until it answers.
 */
export function IssueEnvironmentsSection({
  issueId,
  assigneeType = null,
  assigneeId = null,
}: {
  issueId: string;
  assigneeType?: IssueAssigneeType | null;
  assigneeId?: string | null;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [open, setOpen] = useState(true);
  const [showEnded, setShowEnded] = useState(false);
  const { data } = useQuery(issueEnvironmentsOptions(wsId, issueId));
  const { data: runtimes = [] } = useQuery(runtimeListOptions(wsId));
  const { data: tasks = [] } = useQuery(issueTasksOptions(issueId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { getActorName } = useActorName();
  const local = useLocalDaemonStatus();
  const currentUserId = useAuthStore((state) => state.user?.id ?? null);
  const control = useControlIssueEnvironment(issueId);
  const createComment = useCreateComment(issueId);
  const [requested, setRequested] = useState<ReadonlySet<string>>(() => requestedStartComments.get(issueId) ?? new Set());

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
  const runtimeOnline = useMemo(() => new Map(runtimes.map((runtime) => [runtime.id, runtime.status === "online"])), [runtimes]);
  const ordinals = useMemo(() => runOrdinals(tasks), [tasks]);

  const envs = data?.environments ?? [];
  const configs = data?.configs ?? [];
  const pendingStarts = data?.pending_starts ?? [];
  const now = Date.now();

  // An unknown runtime is one this member cannot see; only a known one reads offline.
  const isOffline = (runtimeId: string | null) => !!runtimeId && runtimeOnline.get(runtimeId) === false;
  const machineOf = (runtimeId: string | null) => (runtimeId ? machineByRuntime.get(runtimeId) ?? null : null);
  const machineTitle = (runtimeId: string | null) => machineOf(runtimeId)?.title ?? t(($) => $.environments.unknown_machine);
  const runLabel = (taskId: string) => {
    const ordinal = ordinals.get(taskId);
    if (!ordinal) return null;
    return ordinal === 1 ? t(($) => $.environments.run_first) : t(($) => $.environments.run_nth, { count: ordinal });
  };
  const actorName = (record: IssueEnvironmentActionRecord) =>
    record.actor_id ? getActorName(record.actor_type, record.actor_id) : t(($) => $.environments.someone);

  const originLabel = (env: IssueEnvironment): string | null => {
    if (env.source === "detected") return t(($) => $.environments.detected_label, { name: env.name });
    if (env.origin) {
      const name = actorName(env.origin);
      return env.origin.action === "restart"
        ? t(($) => $.environments.restarted_by, { name })
        : t(($) => $.environments.started_by, { name });
    }
    if (!env.agent_id) return null;
    const agent = getActorName("agent", env.agent_id);
    const run = runLabel(env.task_id);
    return run
      ? t(($) => $.environments.started_by_agent_in_run, { name: agent, run })
      : t(($) => $.environments.started_by, { name: agent });
  };

  // Who "let the agent start it" goes to: the agent the issue is assigned to,
  // else the last agent that ran on it; with neither, the reader picks.
  const activeAgents = agents.filter((agent) => !agent.archived_at);
  const agentById = (id: string | null | undefined) => (id ? activeAgents.find((agent) => agent.id === id) ?? null : null);
  const latestAgentTask = [...tasks].sort((a, b) => b.created_at.localeCompare(a.created_at)).find((task) => agentById(task.agent_id));
  const startAgent = (assigneeType === "agent" ? agentById(assigneeId) : null) ?? agentById(latestAgentTask?.agent_id);

  const preparing = preparingRun(tasks, requested);
  const preparingShown = preparing && !envs.some((env) => env.task_id === preparing.id && isLive(env));
  const [justRequested, setJustRequested] = useState<string | null>(null);
  const preparingAgentName = preparing
    ? getActorName("agent", preparing.agent_id)
    : justRequested;

  const runControl = (action: IssueEnvironmentControl) => {
    control.mutate(action, {
      onError: (error) => {
        toast.error(t(($) => $.environments.action_failed[action.action]), {
          description: error instanceof Error ? error.message : undefined,
        });
      },
    });
  };

  const askAgent = (agent: Agent) => {
    const mention = `[@${escapeMarkdownLabel(agent.name)}](mention://agent/${agent.id})`;
    setJustRequested(agent.name);
    createComment.mutate(
      { content: `${mention} ${t(($) => $.environments.start_menu.agent_instruction)}` },
      {
        onSuccess: (comment) => {
          const ids = new Set(requestedStartComments.get(issueId) ?? []);
          ids.add(comment.id);
          requestedStartComments.set(issueId, ids);
          setRequested(ids);
          void qc.invalidateQueries({ queryKey: issueKeys.tasks(issueId) });
        },
        onError: (error) => {
          toast.error(t(($) => $.environments.start_menu.ask_failed), {
            description: error instanceof Error ? error.message : undefined,
          });
        },
        onSettled: () => setJustRequested(null),
      },
    );
  };

  const live = envs.filter(isLive);
  const pinned = envs.filter((env) => isPinnedCrash(env, envs, now));
  const ended = envs.filter((env) => !isLive(env) && !pinned.includes(env));
  const shownStarts = pendingStarts.filter((start) =>
    !live.some((env) => env.runtime_id === start.runtime_id && env.name === start.name && env.started_at >= start.created_at),
  );
  const running = live.filter((env) => env.state === "running").length;
  const knowsLocal = !!local.daemonId;

  const declaredState = (config: IssueEnvironmentConfig, declared: DeclaredEnvironment): DeclaredState => {
    const sameEnv = (runtimeId: string | null, name: string) => runtimeId === config.runtime_id && name === declared.name;
    if (pendingStarts.some((start) => sameEnv(start.runtime_id, start.name))) return "starting";
    const current = live.find((env) => env.source === "config" && sameEnv(env.runtime_id, env.name));
    if (!current) return "available";
    return current.state === "starting" || pendingAction(current)?.action === "restart" ? "starting" : "running";
  };

  const groupBy = <T,>(items: T[], runtimeOf: (item: T) => string | null): MachineGroup<T>[] => {
    const byMachine = new Map<string, MachineGroup<T>>();
    for (const item of items) {
      const runtimeId = runtimeOf(item);
      const machine = machineOf(runtimeId);
      const key = machine?.id ?? "unknown";
      const group = byMachine.get(key) ?? { key, machine, offline: true, items: [] };
      group.items.push(item);
      group.offline = group.offline && isOffline(runtimeId);
      byMachine.set(key, group);
    }
    return [...byMachine.values()].sort((a, b) =>
      Number(a.offline) - Number(b.offline) ||
      Number(b.machine?.isCurrent ?? false) - Number(a.machine?.isCurrent ?? false));
  };

  const access = (env: IssueEnvironment, group: MachineGroup<unknown>): RowAccess => {
    const hint = knowsLocal && group.machine?.isCurrent
      ? null
      : group.machine
        ? t(($) => $.environments.remote_hint, { machine: group.machine.title })
        : t(($) => $.environments.localhost_hint);
    // Only the daemon that detected an app knows its process, so a machine
    // that is merely named like this one does not count.
    const focusesHere = canFocusLocalEnvironments() && knowsLocal && group.machine?.daemonId === local.daemonId;
    const appHint = focusesHere
      ? null
      : group.machine && !(knowsLocal && group.machine.isCurrent)
        ? t(($) => $.environments.app_remote_hint, { machine: group.machine.title })
        : t(($) => $.environments.app_desktop_hint);
    return {
      machineTitle: group.machine?.title ?? null,
      offline: isOffline(env.runtime_id),
      localhostHint: hint,
      appHint,
      originLabel: originLabel(env),
    };
  };

  type MainItem = { kind: "env"; env: IssueEnvironment } | { kind: "start"; start: IssueEnvironmentActionRecord };
  const mainItems: MainItem[] = [
    ...[...live, ...pinned].map((env) => ({ kind: "env" as const, env })),
    ...shownStarts.map((start) => ({ kind: "start" as const, start })),
  ];

  const renderGroupHeader = (group: MachineGroup<unknown>) => (
    <div className="flex min-h-7 items-center gap-1.5 px-2 text-micro text-muted-foreground">
      <Laptop className="size-3 shrink-0" />
      <span className="min-w-0 truncate">{group.machine?.title ?? t(($) => $.environments.unknown_machine)}</span>
      {group.machine?.isCurrent ? (
        <span className="shrink-0 rounded-xs bg-muted px-1 text-micro font-medium">{t(($) => $.environments.this_machine)}</span>
      ) : null}
      {group.offline ? (
        <span className="inline-flex shrink-0 items-center gap-0.5 rounded-xs bg-warning/10 px-1 text-micro font-medium text-warning">
          <WifiOff className="size-2.5" />
          {t(($) => $.environments.offline)}
        </span>
      ) : null}
    </div>
  );

  const renderEnv = (env: IssueEnvironment, group: MachineGroup<unknown>) => (
    <EnvironmentRow
      key={env.id}
      issueId={issueId}
      env={env}
      access={access(env, group)}
      pinned={pinned.includes(env)}
      actorName={actorName}
      onControl={runControl}
    />
  );

  const renderStart = (start: IssueEnvironmentActionRecord) => {
    const declared = configs
      .find((config) => config.runtime_id === start.runtime_id)
      ?.environments.find((env) => env.repo === start.repo && env.name === start.name);
    return (
      <StatusRow
        key={start.id}
        icon={<Server className="mt-px size-3.5 shrink-0 text-warning" />}
        title={declared?.label || start.name}
        state={<PendingState label={t(($) => $.environments.state.starting)} />}
        meta={`${t(($) => $.environments.started_by, { name: actorName(start) })} · ${t(($) => $.environments.waiting_ready)}`}
      />
    );
  };

  return (
    <div>
      <IssueSidebarSectionHeader
        title={t(($) => $.environments.section_title)}
        open={open}
        onOpenChange={setOpen}
        count={live.length + pinned.length}
        status={running > 0 ? (
          <span className="inline-flex shrink-0 items-center gap-1 text-caption text-success tabular-nums">
            <span aria-hidden="true" className="size-1.5 rounded-full bg-success" />
            {t(($) => $.environments.running_count, { count: running })}
          </span>
        ) : undefined}
        actions={
          <EnvironmentStartMenu
            configs={configs}
            machineFor={(runtimeId) => ({ title: machineTitle(runtimeId), offline: isOffline(runtimeId) })}
            runLabel={runLabel}
            declaredState={declaredState}
            onStart={(config, declared) =>
              runControl({ action: "start", body: { task_id: config.task_id, repo: declared.repo, name: declared.name } })
            }
            agent={startAgent}
            agentChoices={activeAgents}
            preparing={!!preparing || justRequested !== null}
            onAskAgent={askAgent}
          />
        }
      />
      {open && (
        <div className="px-2">
          {(preparingShown || justRequested) && preparingAgentName && (
            <StatusRow
              icon={<Bot className="mt-px size-3.5 shrink-0 text-muted-foreground" />}
              title={t(($) => $.environments.preparing_title, { name: preparingAgentName })}
              state={<PendingState label={t(($) => $.environments.state.preparing)} />}
              meta={t(($) => $.environments.preparing_hint)}
            />
          )}
          {groupBy(mainItems, (item) => (item.kind === "env" ? item.env.runtime_id : item.start.runtime_id)).map((group) => (
            <div key={group.key}>
              {renderGroupHeader(group)}
              {group.items.map((item) => (item.kind === "env" ? renderEnv(item.env, group) : renderStart(item.start)))}
            </div>
          ))}
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
              {showEnded && groupBy(ended, (env) => env.runtime_id).map((group) => (
                <div key={group.key}>
                  {renderGroupHeader(group)}
                  {group.items.map((env) => renderEnv(env, group))}
                </div>
              ))}
            </>
          )}
        </div>
      )}
    </div>
  );
}

function PendingState({ label }: { label: string }) {
  return (
    <span className="inline-flex shrink-0 items-center gap-1 text-caption text-muted-foreground">
      <Spinner className="size-3" />
      {label}
    </span>
  );
}

function StatusRow({ icon, title, state, meta }: { icon: ReactNode; title: string; state: ReactNode; meta: string }) {
  return (
    <div data-testid="environment-pending-row" className="flex items-start gap-2 rounded-md px-2 py-2">
      {icon}
      <div className="min-w-0 flex-1">
        <p className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-label font-medium">{title}</span>
          {state}
        </p>
        <p className="mt-1 truncate text-caption text-muted-foreground">{meta}</p>
      </div>
    </div>
  );
}

function EnvironmentRow({
  issueId,
  env,
  access,
  pinned,
  actorName,
  onControl,
}: {
  issueId: string;
  env: IssueEnvironment;
  access: RowAccess;
  pinned: boolean;
  actorName: (record: IssueEnvironmentActionRecord) => string;
  onControl: (control: IssueEnvironmentControl) => void;
}) {
  const { t } = useT("issues");
  const timeAgo = useTimeAgo();
  const [logsOpen, setLogsOpen] = useState(false);
  const [confirmStop, setConfirmStop] = useState(false);
  const live = isLive(env);
  const pending = pendingAction(env);
  const failure = pending ? null : recentFailure(env, Date.now());
  const declared = env.source === "config";
  const operable = env.controllable && env.can_control && !access.offline && !pending;
  const canStop = live && operable;
  const canRestart = declared && operable;
  const canReadLogs = declared && env.controllable && !access.offline;

  const app = env.kind === "app";
  const rowState: RowState = pending?.action === "stop" ? "stopping" : pending ? "starting" : env.state;
  const openable = !app && live && !pending && env.url !== "" && env.state !== "unreachable";
  // Without the daemon's key for it there is nothing to ask the daemon for.
  const focusable = app && live && env.state === "running" && access.appHint === null && env.local_key !== "";
  const Icon = app ? AppWindow : env.url ? Globe : Server;
  const host = !app && env.url ? displayHost(env.url) : null;
  const title = environmentTitle(env);
  const primary = declared ? host : app ? null : env.name;
  const meta = environmentMeta(env, pending, t, timeAgo, actorName);
  const tooltip = [
    env.source === "detected" ? t(($) => $.environments.detected_hint) : null,
    failure?.error || null,
    env.detail || null,
    app ? access.appHint : isLoopbackUrl(env.url) ? access.localhostHint : null,
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

  const restart = () => onControl({ action: "restart", environmentId: env.id });
  const stop = () => onControl({ action: "stop", environmentId: env.id });

  const showRestartIcon = !live && !pinned && canRestart;
  // A detected app has no logs and is not stopped from here: it may be the
  // reader's own app, and the daemon knows its process, not its listener.
  const hasMenu = !app && (live || declared);
  const hasActions = openable || focusable || hasMenu || showRestartIcon;

  const body = (
    <>
      <Icon className={cn("mt-px size-3.5 shrink-0", STATE_TONE[rowState] ?? "text-muted-foreground")} />
      <div className="min-w-0 flex-1">
        <p className="flex min-w-0 items-center gap-2">
          <span className="min-w-0 flex-1 truncate text-label font-medium">{title}</span>
          <span
            className={cn(
              "shrink-0 text-caption",
              STATE_TONE[rowState] ?? "text-muted-foreground",
              hasActions && "group-hover/env:invisible group-has-[:focus-visible]/env:invisible [@media(pointer:coarse)]:invisible",
            )}
          >
            {pending ? <PendingState label={t(($) => $.environments.state[rowState])} /> : t(($) => $.environments.state[rowState])}
          </span>
        </p>
        <p className="mt-1 flex min-w-0 items-center gap-1.5 overflow-hidden whitespace-nowrap text-caption text-muted-foreground tabular-nums">
          {primary && <span className="max-w-[60%] shrink-0 truncate">{primary}</span>}
          {primary && meta && <span aria-hidden="true" className="text-faint-foreground">·</span>}
          {meta && <span className="min-w-0 truncate">{meta}</span>}
          {failure && (
            <>
              <span aria-hidden="true" className="text-faint-foreground">·</span>
              <span className="shrink-0 text-warning">
                {failure.status === "expired"
                  ? t(($) => $.environments.no_response)
                  : t(($) => $.environments.action_failed[failure.action])}
              </span>
            </>
          )}
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
      {pinned && (
        <div className="px-2 pb-2 pl-7.5">
          {env.excerpt && (
            <pre className="mb-2 overflow-hidden rounded-sm bg-destructive/10 px-2 py-1 font-mono text-micro leading-4 text-destructive">
              {env.excerpt.split("\n").slice(-2).map((line, index) => (
                <span key={index} className="block truncate">{line}</span>
              ))}
            </pre>
          )}
          <div className="flex items-center gap-1.5">
            <Button variant="outline" size="xs" onClick={() => setLogsOpen(true)} disabled={!env.controllable}>
              <ScrollText />
              {t(($) => $.environments.view_logs)}
            </Button>
            <Button variant="outline" size="xs" onClick={restart} disabled={!canRestart}>
              <RotateCw />
              {t(($) => $.environments.restart)}
            </Button>
          </div>
        </div>
      )}
      {hasActions && (
        <div className="absolute top-1 right-1 flex items-center gap-0.5 opacity-0 group-hover/env:opacity-100 group-has-[:focus-visible]/env:opacity-100 has-[[data-popup-open]]:opacity-100 [@media(pointer:coarse)]:opacity-100">
          {openable && (
            <>
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
            </>
          )}
          {focusable && (
            <IconButton variant="ghost" size="icon-xs" label={t(($) => $.environments.focus)} onClick={focus}>
              <ArrowUpRight />
            </IconButton>
          )}
          {showRestartIcon && (
            <IconButton variant="ghost" size="icon-xs" label={t(($) => $.environments.restart)} onClick={restart}>
              <RotateCw />
            </IconButton>
          )}
          {hasMenu && (
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <IconButton variant="ghost" size="icon-xs" label={t(($) => $.environments.more_actions)}>
                    <MoreHorizontal />
                  </IconButton>
                }
              />
              <DropdownMenuContent align="end" className="w-64">
                <DropdownMenuGroup>
                  <DropdownMenuLabel className="truncate">
                    {access.offline && access.machineTitle
                      ? t(($) => $.environments.machine_offline, { machine: access.machineTitle })
                      : access.originLabel ?? title}
                  </DropdownMenuLabel>
                  <DropdownMenuItem disabled={!canReadLogs} onClick={() => setLogsOpen(true)}>
                    <ScrollText />
                    {t(($) => $.environments.view_logs)}
                  </DropdownMenuItem>
                  {!declared && <MenuNote>{t(($) => $.environments.detected_no_logs)}</MenuNote>}
                  {declared && (env.can_control || access.offline || !env.controllable) && (
                    <DropdownMenuItem disabled={!canRestart} onClick={restart}>
                      <RotateCw />
                      {t(($) => $.environments.restart)}
                    </DropdownMenuItem>
                  )}
                </DropdownMenuGroup>
                {live && (env.can_control || access.offline || !env.controllable) && (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem
                      disabled={!canStop}
                      onClick={() => (declared ? stop() : setConfirmStop(true))}
                    >
                      <Square />
                      {declared ? t(($) => $.environments.stop) : t(($) => $.environments.stop_confirm_item)}
                    </DropdownMenuItem>
                  </>
                )}
                {access.offline ? (
                  <MenuNote>{t(($) => $.environments.offline_hint)}</MenuNote>
                ) : !env.controllable ? (
                  <MenuNote>{t(($) => $.environments.outdated_hint)}</MenuNote>
                ) : !env.can_control ? (
                  <>
                    <DropdownMenuSeparator />
                    <MenuNote>{t(($) => $.environments.no_permission_hint)}</MenuNote>
                  </>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>
          )}
        </div>
      )}
      {logsOpen && (
        <EnvironmentLogDialog
          issueId={issueId}
          env={env}
          open={logsOpen}
          onOpenChange={setLogsOpen}
          machineTitle={access.machineTitle}
          originLabel={access.originLabel}
          offline={access.offline}
          canRestart={canRestart}
          onRestart={() => {
            restart();
            setLogsOpen(false);
          }}
        />
      )}
      <AlertDialog open={confirmStop} onOpenChange={setConfirmStop}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.environments.stop_confirm_title, { name: title })}</AlertDialogTitle>
            <AlertDialogDescription>{t(($) => $.environments.stop_confirm_body, { process: env.name })}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.environments.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                setConfirmStop(false);
                stop();
              }}
            >
              {t(($) => $.environments.stop)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function MenuNote({ children }: { children: ReactNode }) {
  return <p className="px-2 py-1.5 text-caption text-muted-foreground">{children}</p>;
}

function environmentMeta(
  env: IssueEnvironment,
  pending: IssueEnvironmentActionRecord | null,
  t: IssuesT,
  timeAgo: (iso: string) => string,
  actorName: (record: IssueEnvironmentActionRecord) => string,
): string | null {
  if (pending?.action === "stop") return t(($) => $.environments.waiting_machine);
  if (pending?.action === "restart") {
    return `${t(($) => $.environments.restarted_by, { name: actorName(pending) })} · ${t(($) => $.environments.waiting_ready)}`;
  }
  if (env.ended_at) {
    const code = exitCode(env);
    if (env.state === "exited" && code !== null && code !== 0) {
      return `${t(($) => $.environments.exited_ago, { time: timeAgo(env.ended_at) })} · ${t(($) => $.environments.exit_code, { code })}`;
    }
    const last = env.last_action;
    if (env.state === "stopped" && last?.action === "stop" && last.status === "succeeded") {
      return `${t(($) => $.environments.stopped_by, { name: actorName(last) })} · ${timeAgo(env.ended_at)}`;
    }
    if (env.state === "stopped" && env.detail === "expired") return t(($) => $.environments.stopped_on_expiry);
    return t(($) => $.environments.ended_ago, { time: timeAgo(env.ended_at) });
  }
  if (env.state === "unreachable") return t(($) => $.environments.last_seen, { time: timeAgo(env.last_seen_at) });
  if (env.state === "starting" && env.origin) {
    const by = env.origin.action === "restart"
      ? t(($) => $.environments.restarted_by, { name: actorName(env.origin) })
      : t(($) => $.environments.started_by, { name: actorName(env.origin) });
    return `${by} · ${t(($) => $.environments.waiting_ready)}`;
  }
  if (env.expires_at) {
    const minutes = Math.max(1, Math.round((new Date(env.expires_at).getTime() - Date.now()) / 60_000));
    if (minutes < 60) return t(($) => $.environments.expires_in_minutes, { count: minutes });
    const hours = Math.round(minutes / 60);
    if (hours < 48) return t(($) => $.environments.expires_in_hours, { count: hours });
    return t(($) => $.environments.expires_in_days, { count: Math.round(hours / 24) });
  }
  return t(($) => $.environments.started_ago, { time: timeAgo(env.started_at) });
}

