"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { AlertCircle, MoreHorizontal, Pencil, Plus, Server } from "@multica/ui/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { runtimeProfileListOptions } from "@multica/core/runtimes";
import { runtimeKeys, runtimeListOptions } from "@multica/core/runtimes/queries";
import { useWSEvent } from "@multica/core/realtime";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import type { MemberWithUser } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { cn } from "@multica/ui/lib/utils";
import { ActorAvatar } from "../../common/actor-avatar";
import { CollectionPageState } from "../../layout/collection-page";
import {
  OBJECT_PAGE_BODY,
  ObjectIconMark,
  ObjectIdentity,
  ObjectPage,
  ObjectPanelSection,
  ObjectProperty,
  ObjectSection,
  ObjectSectionAction,
  ObjectSectionCard,
} from "../../layout/object-page";
import { AppLink } from "../../navigation";
import { buildWorkloadIndex, RuntimeList } from "./runtime-list";
import {
  buildRuntimeMachines,
  sharedCustomName,
  type RuntimeMachine,
} from "./runtime-machines";
import { RenameMachineDialog } from "./rename-machine-dialog";
import { RuntimeProfilesDialog } from "./runtime-profiles-dialog";
import { pendingRuntimesForProfiles } from "./pending-runtime";
import {
  MachineCliSection,
  machineHasCliInfo,
  machineUpdateRuntime,
} from "./machine-cli-section";
import { isQoderLocator, QoderDetailPage } from "./qoder-detail-page";
import {
  HealthIcon,
  ProviderIconStack,
  machineIcon,
  shortDaemonId,
  useHealthLabel,
} from "./shared";
import { useT, useTimeAgo } from "../../i18n";

const LAYOUT_ID = "multica_runtime_detail_layout";

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
  const qc = useQueryClient();
  const healthLabel = useHealthLabel();
  const currentUserId = useAuthStore((state) => state.user?.id);
  const { data: runtimes = [], isLoading } = useQuery(runtimeListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: tasks = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: runtimeProfiles = [] } = useQuery(
    runtimeProfileListOptions(wsId),
  );
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

  if (
    isQoderLocator(machineLocator, runtimes) ||
    (machine?.runtimes.length &&
      machine.runtimes.every((runtime) => runtime.provider === "qoder_cloud"))
  ) {
    return <QoderDetailPage />;
  }

  const segments = [{ href: paths.runtimes(), label: t(($) => $.page.title) }];

  if (isLoading) {
    return (
      <ObjectPage
        layoutId={LAYOUT_ID}
        segments={segments}
        leaf={<Skeleton className="h-4 w-40" />}
        panel={<PropertiesSkeleton />}
      >
        <MachineBodySkeleton />
      </ObjectPage>
    );
  }

  if (!machine) {
    return (
      <ObjectPage
        layoutId={LAYOUT_ID}
        segments={segments}
        leaf={
          <span className="truncate text-muted-foreground">
            {t(($) => $.machine.not_found_title)}
          </span>
        }
      >
        <CollectionPageState
          icon={AlertCircle}
          tone="destructive"
          title={t(($) => $.machine.not_found_title)}
          description={t(($) => $.machine.not_found_hint)}
          actions={
            <Button
              size="sm"
              variant="outline"
              render={<AppLink href={paths.runtimes()} />}
              nativeButton={false}
            >
              {t(($) => $.detail.all_runtimes)}
            </Button>
          }
        />
      </ObjectPage>
    );
  }

  const Icon = machineIcon(machine);
  const updateRuntime = machineUpdateRuntime(machine, currentUserId, isAdmin);
  const daemonActions = machine.isCurrent ? localMachineActions : null;
  const hasCliInfo = machineHasCliInfo(machine, updateRuntime);
  const summary = [
    machine.subtitle,
    machine.cliVersion ? `CLI ${machine.cliVersion}` : null,
    healthLabel(machine.health),
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <>
      <ObjectPage
        layoutId={LAYOUT_ID}
        segments={segments}
        leaf={
          <span className="flex min-w-0 items-center gap-1.5">
            <Icon aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" />
            <span className="truncate">{machine.title}</span>
          </span>
        }
        actions={
          renameTarget ? (
            <MachineActionsMenu onRename={() => setRenameOpen(true)} />
          ) : undefined
        }
        panel={<MachineProperties machine={machine} members={members} />}
      >
        <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
          <ObjectIdentity
            media={<ObjectIconMark icon={Icon} />}
            title={machine.title}
            trailing={
              machine.isCurrent ? (
                <span className="shrink-0 rounded-xs bg-foreground px-1.5 py-0.5 text-micro font-medium text-background">
                  {t(($) => $.machine.this_machine)}
                </span>
              ) : null
            }
            description={summary}
          />

          {(hasCliInfo || daemonActions) && (
            <ObjectSection
              title={t(($) => $.machine.daemon_title)}
              action={daemonActions}
            >
              {hasCliInfo && (
                <ObjectSectionCard className="px-3 py-2.5 text-caption">
                  <MachineCliSection
                    machine={machine}
                    currentUserId={currentUserId}
                    canManagePublicRuntimes={isAdmin}
                  />
                </ObjectSectionCard>
              )}
            </ObjectSection>
          )}

          <ObjectSection
            title={t(($) => $.machine.metrics.runtimes)}
            count={machineRuntimes.length}
            action={
              canAddRuntime ? (
                <ObjectSectionAction
                  icon={Plus}
                  onClick={() => setCreateProfileOpen(true)}
                >
                  {t(($) => $.profiles.add_custom)}
                </ObjectSectionAction>
              ) : null
            }
          >
            {machineRuntimes.length > 0 ? (
              <div className="-mx-2">
                <RuntimeList
                  runtimes={machineRuntimes}
                  now={now}
                  machineTitle={machine.title}
                  runtimeHref={(childRuntimeId) =>
                    paths.runtimeSettings(machine.id, childRuntimeId)
                  }
                />
              </div>
            ) : (
              <div className="flex min-h-40 flex-col items-center justify-center rounded-lg border border-dashed px-6 text-center">
                <Server
                  aria-hidden="true"
                  className="size-6 text-faint-foreground"
                />
                <p className="mt-3 text-body font-medium">
                  {bootstrapping
                    ? t(($) => $.page.bootstrapping.title)
                    : t(($) => $.machine.no_runtimes_title)}
                </p>
                <p className="mt-1 max-w-sm text-caption text-muted-foreground">
                  {bootstrapping
                    ? t(($) => $.page.bootstrapping.hint)
                    : t(($) => $.machine.no_runtimes_hint)}
                </p>
              </div>
            )}
          </ObjectSection>
        </div>
      </ObjectPage>

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
    </>
  );
}

function MachineActionsMenu({ onRename }: { onRename: () => void }) {
  const { t } = useT("runtimes");
  const { t: tCommon } = useT("common");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <IconButton
            label={tCommon(($) => $.more_actions)}
            tooltipSide="bottom"
            className="text-muted-foreground"
          >
            <MoreHorizontal />
          </IconButton>
        }
      />
      <DropdownMenuContent align="end" className="w-auto">
        <DropdownMenuItem onClick={onRename}>
          <Pencil className="size-3.5" />
          {t(($) => $.machine.rename)}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function MachineProperties({
  machine,
  members,
}: {
  machine: RuntimeMachine;
  members: MemberWithUser[];
}) {
  const { t } = useT("runtimes");
  const { t: tLayout } = useT("layout");
  const healthLabel = useHealthLabel();
  const timeAgo = useTimeAgo();
  const ownerId = machine.runtimes[0]?.owner_id ?? null;
  const owner = ownerId
    ? members.find((member) => member.user_id === ownerId) ?? null
    : null;
  const busy = machine.runningCount + machine.queuedCount > 0;
  const daemonShort = shortDaemonId(machine.daemonId);

  return (
    <ObjectPanelSection title={tLayout(($) => $.object_page.properties)}>
      <ObjectProperty label={t(($) => $.page.columns.status)}>
        <HealthIcon health={machine.health} />
        <span className="truncate">
          {healthLabel(machine.health)}
          {machine.lastSeenAt && machine.health !== "online"
            ? ` · ${timeAgo(machine.lastSeenAt)}`
            : null}
        </span>
      </ObjectProperty>
      <ObjectProperty label={t(($) => $.detail.fact_owner)}>
        {owner ? (
          <>
            <ActorAvatar
              actorType="member"
              actorId={owner.user_id}
              size="xs"
              enableHoverCard
            />
            <span className="truncate">{owner.name}</span>
          </>
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </ObjectProperty>
      <ObjectProperty label={t(($) => $.detail.fact_device)}>
        <span className={machine.deviceSummary ? "truncate" : "text-muted-foreground"}>
          {machine.deviceSummary ?? "—"}
        </span>
      </ObjectProperty>
      <ObjectProperty label={t(($) => $.page.columns.harness)}>
        {machine.providerNames.length > 0 ? (
          <ProviderIconStack providers={machine.providerNames} />
        ) : (
          <span className="text-muted-foreground">—</span>
        )}
      </ObjectProperty>
      <ObjectProperty label={t(($) => $.page.columns.workload)}>
        <span className={busy ? "truncate tabular-nums" : "text-muted-foreground"}>
          {busy
            ? t(($) => $.machine.metrics.workload_hint, {
                running: machine.runningCount,
                queued: machine.queuedCount,
              })
            : t(($) => $.machine.metrics.workload_idle)}
        </span>
      </ObjectProperty>
      {daemonShort && (
        <ObjectProperty label={t(($) => $.detail.fact_daemon_id)}>
          <span className="truncate font-mono">{daemonShort}</span>
        </ObjectProperty>
      )}
    </ObjectPanelSection>
  );
}

function PropertiesSkeleton() {
  return (
    <div className="space-y-3 px-2">
      <Skeleton className="h-3 w-20" />
      {Array.from({ length: 5 }).map((_, index) => (
        <Skeleton key={index} className="h-4 w-full" />
      ))}
    </div>
  );
}

function MachineBodySkeleton() {
  return (
    <div className={cn(OBJECT_PAGE_BODY, "space-y-8")}>
      <div className="flex items-center gap-3">
        <Skeleton className="size-12 rounded-xl" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-5 w-56" />
          <Skeleton className="h-4 w-40" />
        </div>
      </div>
      <div className="space-y-2">
        <Skeleton className="h-4 w-24" />
        <div className="overflow-hidden rounded-lg border">
          {Array.from({ length: 3 }).map((_, index) => (
            <Skeleton
              key={index}
              className="h-14 w-full rounded-none border-b last:border-b-0"
            />
          ))}
        </div>
      </div>
    </div>
  );
}
