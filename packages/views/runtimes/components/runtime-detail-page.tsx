"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { AlertCircle, Cloud, Monitor, Pencil, Plus, Server } from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { runtimeProfileListOptions } from "@multica/core/runtimes";
import { runtimeKeys, runtimeListOptions } from "@multica/core/runtimes/queries";
import { useUpdatableRuntimeIds } from "@multica/core/runtimes/hooks";
import { useWSEvent } from "@multica/core/realtime";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { AppLink, useNavigation } from "../../navigation";
import { buildWorkloadIndex, RuntimeList } from "./runtime-list";
import {
  buildRuntimeMachines,
  sharedCustomName,
} from "./runtime-machines";
import { RenameMachineDialog } from "./rename-machine-dialog";
import { RuntimeProfilesDialog } from "./runtime-profiles-dialog";
import { pendingRuntimesForProfiles } from "./pending-runtime";
import { HealthIcon, useHealthLabel } from "./shared";
import { useT, useTimeAgo } from "../../i18n";

export interface RuntimeDetailPageProps {
  /** A machine id, or a legacy runtime id that locates its machine. */
  runtimeId: string;
  localDaemonId?: string | null;
  localMachineName?: string | null;
  localMachineActions?: React.ReactNode;
  hasLocalMachine?: boolean;
  bootstrapping?: boolean;
}

function useNowTick(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

function decodeRouteParam(value: string): string {
  try {
    return decodeURIComponent(value);
  } catch {
    return value;
  }
}

function findMachine(
  machines: ReturnType<typeof buildRuntimeMachines>,
  locator: string,
) {
  return (
    machines.find(
      (candidate) =>
        candidate.id === locator ||
        candidate.runtimes.some((runtime) => runtime.id === locator),
    ) ??
    (locator === "local:placeholder"
      ? machines.find((candidate) => candidate.isCurrent) ?? null
      : null)
  );
}

/**
 * Machine-level detail route. New links use the daemon-level machine id;
 * legacy links that still carry a runtime id remain valid and are expanded
 * to their containing machine before child runtime settings are exposed.
 */
export function RuntimeDetailPage({
  runtimeId,
  localDaemonId,
  localMachineName,
  localMachineActions,
  hasLocalMachine,
  bootstrapping,
}: RuntimeDetailPageProps) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const qc = useQueryClient();
  const healthLabel = useHealthLabel();
  const timeAgo = useTimeAgo();
  const currentUserId = useAuthStore((state) => state.user?.id);
  const { data: runtimes = [], isLoading } = useQuery(runtimeListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: tasks = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: runtimeProfiles = [] } = useQuery(
    runtimeProfileListOptions(wsId),
  );
  const updatableIds = useUpdatableRuntimeIds(wsId);
  const now = useNowTick();
  const machineLocator = decodeRouteParam(runtimeId);
  const [renameOpen, setRenameOpen] = useState(false);
  const [createProfileOpen, setCreateProfileOpen] = useState(false);
  const workloadIndex = useMemo(
    () => buildWorkloadIndex(agents, tasks),
    [agents, tasks],
  );
  const baseMachines = useMemo(
    () =>
      buildRuntimeMachines(runtimes, {
        now,
        localDaemonId,
        localMachineName,
        currentUserId,
        workloadByRuntimeId: workloadIndex,
        ensureLocalMachine: hasLocalMachine,
      }),
    [
      runtimes,
      now,
      localDaemonId,
      localMachineName,
      currentUserId,
      workloadIndex,
      hasLocalMachine,
    ],
  );
  const baseMachine = findMachine(baseMachines, machineLocator);
  const profileRows = useMemo(
    () =>
      runtimeProfiles.map((profile) => {
        const createdAt = Date.parse(profile.created_at);
        return {
          profile,
          createdAt: Number.isFinite(createdAt) ? createdAt : 0,
        };
      }),
    [runtimeProfiles],
  );
  const machineRuntimes = useMemo(() => {
    if (!baseMachine) return [];
    if (baseMachine.mode !== "local") return baseMachine.runtimes;
    return pendingRuntimesForProfiles({
      pendingProfiles: profileRows,
      runtimes: baseMachine.runtimes,
      localDaemonId: baseMachine.daemonId,
      localMachineName: baseMachine.title,
      fallbackMachineName: baseMachine.title,
    });
  }, [baseMachine, profileRows]);
  const machine = baseMachine;
  const handleDaemonEvent = useCallback(() => {
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  }, [qc, wsId]);
  useWSEvent("daemon:register", handleDaemonEvent);

  const currentMember = currentUserId
    ? members.find((member) => member.user_id === currentUserId)
    : null;
  const isAdmin =
    currentMember?.role === "owner" || currentMember?.role === "admin";
  const canAddRuntime =
    isAdmin && machine?.mode === "local" && !!machine.daemonId;
  const renameTarget = useMemo(() => {
    if (!machine || machine.runtimes.length === 0) return null;
    const editable = isAdmin
      ? machine.runtimes[0]
      : machine.runtimes.find((runtime) => runtime.owner_id === currentUserId);
    if (!editable) return null;
    return {
      runtimeId: editable.id,
      currentName: sharedCustomName(machine.runtimes) ?? "",
    };
  }, [machine, isAdmin, currentUserId]);

  if (isLoading) return <MachineDetailSkeleton />;

  if (!machine) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <div>
            <p className="text-sm font-medium">
              {t(($) => $.machine.not_found_title)}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {t(($) => $.machine.not_found_hint)}
            </p>
          </div>
          <Button size="sm" onClick={() => navigation.push(paths.runtimes())}>
            {t(($) => $.detail.all_runtimes)}
          </Button>
        </div>
      </div>
    );
  }

  const Icon = machine.section === "cloud" ? Cloud : Monitor;
  const busyCount = machine.runningCount + machine.queuedCount;
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <header className="shrink-0 border-b bg-background px-4 pb-5 pt-3 sm:px-6">
        <div className="mx-auto max-w-[1440px]">
          <div className="flex min-w-0 items-center gap-1.5 text-xs text-muted-foreground">
            <AppLink
              href={paths.runtimes()}
              className="rounded-sm transition-colors hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              {t(($) => $.page.title)}
            </AppLink>
            <span aria-hidden="true">/</span>
            <span className="truncate text-foreground">{machine.title}</span>
          </div>

          <div className="mt-4 flex flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
            <div className="flex min-w-0 items-start gap-4">
              <div className="flex h-12 w-12 shrink-0 items-center justify-center rounded-xl border bg-card sm:h-14 sm:w-14">
                <Icon
                  aria-hidden="true"
                  className="h-5 w-5 text-muted-foreground sm:h-6 sm:w-6"
                />
              </div>
              <div className="min-w-0 pt-0.5">
                <div className="flex flex-wrap items-center gap-x-3 gap-y-1.5">
                  <h1 className="min-w-0 text-balance text-xl font-semibold tracking-tight sm:text-2xl">
                    {machine.title}
                  </h1>
                  <span className="inline-flex items-center gap-1.5 text-xs">
                    <HealthIcon health={machine.health} />
                    {healthLabel(machine.health)}
                  </span>
                  {machine.isCurrent && (
                    <span className="rounded bg-foreground px-1.5 py-0.5 text-[10px] font-medium text-background">
                      {t(($) => $.machine.this_machine)}
                    </span>
                  )}
                </div>
                {machine.subtitle && (
                  <p className="mt-1 text-sm text-muted-foreground">
                    {machine.subtitle}
                  </p>
                )}
                <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs text-muted-foreground">
                  <span>
                    {t(($) => $.machine.runtime_count, {
                      count: machineRuntimes.length,
                    })}
                  </span>
                  <span>
                    {busyCount > 0
                      ? t(($) => $.machine.metrics.workload_hint, {
                          running: machine.runningCount,
                          queued: machine.queuedCount,
                        })
                      : t(($) => $.machine.metrics.workload_idle)}
                  </span>
                  {machine.cliVersion && (
                    <span className="font-mono">CLI {machine.cliVersion}</span>
                  )}
                  {machine.lastSeenAt && (
                    <span>{timeAgo(machine.lastSeenAt)}</span>
                  )}
                </div>
              </div>
            </div>

            <div className="flex shrink-0 items-center gap-2 self-end lg:self-start">
              {renameTarget && (
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => setRenameOpen(true)}
                >
                  <Pencil aria-hidden="true" className="h-3.5 w-3.5" />
                  {t(($) => $.machine.rename)}
                </Button>
              )}
              {machine.isCurrent && localMachineActions}
            </div>
          </div>
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto bg-background">
        <div className="mx-auto w-full max-w-[1440px] p-4 sm:p-6">
          <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
            <div className="min-w-0">
              <h2 className="text-sm font-semibold">
                {t(($) => $.machine.metrics.runtimes)}
              </h2>
              <p className="mt-1 text-xs text-muted-foreground">
                {t(($) => $.machine.select_runtime)}
              </p>
            </div>
            {canAddRuntime && (
              <Button
                type="button"
                size="sm"
                onClick={() => setCreateProfileOpen(true)}
              >
                <Plus aria-hidden="true" className="h-3.5 w-3.5" />
                {t(($) => $.profiles.add_custom)}
              </Button>
            )}
          </div>
          {machineRuntimes.length > 0 ? (
            <div className="overflow-hidden rounded-lg border bg-card">
              <RuntimeList
                runtimes={machineRuntimes}
                updatableIds={updatableIds}
                now={now}
                runtimeHref={(childRuntimeId) =>
                  paths.runtimeSettings(machine.id, childRuntimeId)
                }
              />
            </div>
          ) : (
            <div className="flex min-h-48 flex-col items-center justify-center rounded-lg border border-dashed px-6 text-center">
              <Server
                aria-hidden="true"
                className="h-7 w-7 text-muted-foreground/40"
              />
              <p className="mt-3 text-sm font-medium">
                {bootstrapping
                  ? t(($) => $.page.bootstrapping.title)
                  : t(($) => $.machine.no_runtimes_title)}
              </p>
              <p className="mt-1 max-w-sm text-xs text-muted-foreground">
                {bootstrapping
                  ? t(($) => $.page.bootstrapping.hint)
                  : t(($) => $.machine.no_runtimes_hint)}
              </p>
            </div>
          )}
        </div>
      </div>

      {renameTarget && (
        <RenameMachineDialog
          open={renameOpen}
          onOpenChange={setRenameOpen}
          wsId={wsId}
          runtimeId={renameTarget.runtimeId}
          currentName={renameTarget.currentName}
        />
      )}
      {canAddRuntime && createProfileOpen && (
        <RuntimeProfilesDialog
          wsId={wsId}
          intent="create"
          machineName={machine.title}
          onClose={() => setCreateProfileOpen(false)}
        />
      )}
    </div>
  );
}

function MachineDetailSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="border-b px-6 pb-5 pt-3">
        <Skeleton className="h-3 w-36" />
        <div className="mt-4 flex items-start gap-4">
          <Skeleton className="h-14 w-14 rounded-xl" />
          <div className="flex-1">
            <Skeleton className="h-7 w-64" />
            <Skeleton className="mt-2 h-4 w-40" />
            <Skeleton className="mt-3 h-3 w-72" />
          </div>
        </div>
      </div>
      <div className="mx-auto w-full max-w-[1440px] p-6">
        <Skeleton className="h-4 w-24" />
        <Skeleton className="mt-2 h-3 w-72" />
        <div className="mt-4 overflow-hidden rounded-lg border">
          {Array.from({ length: 4 }).map((_, index) => (
            <Skeleton key={index} className="h-14 w-full rounded-none border-b last:border-b-0" />
          ))}
        </div>
      </div>
    </div>
  );
}
