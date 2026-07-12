"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ChevronRight,
  Cloud,
  Monitor,
  Plus,
  Server,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { runtimeProfileListOptions } from "@multica/core/runtimes";
import { runtimeListOptions, runtimeKeys } from "@multica/core/runtimes/queries";
import { useWSEvent } from "@multica/core/realtime";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
  CollectionPageState,
} from "../../layout/collection-page";
import { PageHeader } from "../../layout/page-header";
import { AppLink } from "../../navigation";
import { ConnectRemoteDialog } from "./connect-remote-dialog";
import { CloudRuntimeDialog } from "./cloud-runtime-dialog";
import { ProviderLogo } from "./provider-logo";
import { buildWorkloadIndex, RuntimeList } from "./runtime-list";
import { pendingRuntimeFromProfile } from "./pending-runtime";
import { buildRuntimeMachines, type RuntimeMachine } from "./runtime-machines";
import { HealthDot, HealthIcon, useHealthLabel } from "./shared";
import { useT, useTimeAgo } from "../../i18n";
import { daemonRuntimesDocsHref } from "./runtime-docs";

export interface RuntimesPageProps {
  /** Desktop-only daemon id used to identify this device. */
  localDaemonId?: string | null;
  /** Desktop-only friendly device name for the local daemon. */
  localMachineName?: string | null;
  /** Desktop-only daemon controls attached to the local machine row. */
  localMachineActions?: React.ReactNode;
  /** Keep the local device visible even before its first runtime registers. */
  hasLocalMachine?: boolean;
  /** The bundled daemon is starting but has not registered yet. */
  bootstrapping?: boolean;
  /** Web SaaS-only Cloud Runtime entrypoint. */
  cloudRuntimeEnabled?: boolean;
}

function useNowTick(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

export function RuntimesPage({
  localDaemonId,
  localMachineName,
  localMachineActions,
  hasLocalMachine,
  bootstrapping,
  cloudRuntimeEnabled = false,
}: RuntimesPageProps = {}) {
  const isAuthLoading = useAuthStore((state) => state.isLoading);
  const currentUserId = useAuthStore((state) => state.user?.id);
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [showConnectDialog, setShowConnectDialog] = useState(false);
  const [showCloudRuntimeDialog, setShowCloudRuntimeDialog] = useState(false);

  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: runtimeProfiles = [], isLoading: profilesLoading } = useQuery(
    runtimeProfileListOptions(wsId),
  );
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  const handleDaemonEvent = useCallback(() => {
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  }, [qc, wsId]);
  useWSEvent("daemon:register", handleDaemonEvent);

  const workloadIndex = useMemo(
    () => buildWorkloadIndex(agents, snapshot),
    [agents, snapshot],
  );
  const now = useNowTick();
  const machines = useMemo(
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
  const orphanProfileRuntimes = useMemo(() => {
    if (machines.some((machine) => machine.mode === "local")) return [];
    return runtimeProfiles.map((profile) => {
      const createdAt = Date.parse(profile.created_at);
      return pendingRuntimeFromProfile({
        profile,
        createdAt: Number.isFinite(createdAt) ? createdAt : 0,
        fallbackMachineName: "Unassigned",
      });
    });
  }, [machines, runtimeProfiles]);

  if (isAuthLoading || runtimesLoading || profilesLoading) {
    return <RuntimesPageSkeleton />;
  }

  const showEmpty =
    machines.length === 0 &&
    orphanProfileRuntimes.length === 0 &&
    !bootstrapping &&
    hasLocalMachine !== true;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeaderBar
        totalCount={machines.length}
        onConnectRemote={() => setShowConnectDialog(true)}
        cloudRuntimeEnabled={cloudRuntimeEnabled}
        onOpenCloudRuntime={() => setShowCloudRuntimeDialog(true)}
      />

      {showEmpty ? (
        <div className="flex flex-1 items-center justify-center p-6">
          <EmptyState onConnectRemote={() => setShowConnectDialog(true)} />
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-y-auto">
          <div className="mx-auto flex w-full max-w-[1440px] flex-col p-4 sm:p-6">
            {(machines.length > 0 || bootstrapping) && (
              <MachineList
                machines={machines}
                bootstrapping={bootstrapping}
                localMachineActions={localMachineActions}
              />
            )}
            {orphanProfileRuntimes.length > 0 && (
              <OrphanRuntimeProfiles
                runtimes={orphanProfileRuntimes}
                now={now}
                hasMachines={machines.length > 0}
              />
            )}
          </div>
        </div>
      )}

      {showConnectDialog && (
        <ConnectRemoteDialog onClose={() => setShowConnectDialog(false)} />
      )}
      {cloudRuntimeEnabled && showCloudRuntimeDialog && (
        <CloudRuntimeDialog onClose={() => setShowCloudRuntimeDialog(false)} />
      )}
    </div>
  );
}

function OrphanRuntimeProfiles({
  runtimes,
  now,
  hasMachines,
}: {
  runtimes: ReturnType<typeof pendingRuntimeFromProfile>[];
  now: number;
  hasMachines: boolean;
}) {
  const { t } = useT("runtimes");
  return (
    <section className={hasMachines ? "mt-6" : undefined}>
      <div className="mb-3">
        <h2 className="text-sm font-semibold">
          {t(($) => $.profiles.unassigned_title)}
        </h2>
        <p className="mt-1 text-xs text-muted-foreground">
          {t(($) => $.profiles.unassigned_description)}
        </p>
      </div>
      <div className="overflow-hidden rounded-lg border bg-card">
        <RuntimeList runtimes={runtimes} now={now} />
      </div>
    </section>
  );
}

function PageHeaderBar({
  totalCount,
  onConnectRemote,
  cloudRuntimeEnabled,
  onOpenCloudRuntime,
}: {
  totalCount: number;
  onConnectRemote: () => void;
  cloudRuntimeEnabled: boolean;
  onOpenCloudRuntime: () => void;
}) {
  const { t, i18n } = useT("runtimes");
  return (
    <CollectionPageHeader
      icon={Server}
      title={t(($) => $.page.title)}
      count={totalCount}
      description={t(($) => $.page.tagline)}
      learnMore={{
        href: daemonRuntimesDocsHref(i18n.language),
        label: t(($) => $.page.learn_more),
      }}
      actions={
        <>
          {cloudRuntimeEnabled && (
            <CollectionPageHeaderAction
              icon={Cloud}
              label={t(($) => $.cloud_runtime.action)}
              onClick={onOpenCloudRuntime}
            />
          )}
          <CollectionPageHeaderAction
            icon={Plus}
            label={t(($) => $.page.connect_remote)}
            onClick={onConnectRemote}
          />
        </>
      }
    />
  );
}

function MachineList({
  machines,
  bootstrapping,
  localMachineActions,
}: {
  machines: RuntimeMachine[];
  bootstrapping?: boolean;
  localMachineActions?: React.ReactNode;
}) {
  const { t } = useT("runtimes");
  if (machines.length === 0) {
    return (
      <CollectionPageState
        icon={Server}
        title={
          bootstrapping
            ? t(($) => $.page.bootstrapping.title)
            : t(($) => $.page.empty.title)
        }
        description={
          bootstrapping
            ? t(($) => $.page.bootstrapping.hint)
            : t(($) => $.page.empty.hint)
        }
      />
    );
  }

  return (
    <div className="overflow-hidden rounded-lg border bg-card">
      <div className="divide-y">
        {machines.map((machine) => (
          <MachineRow
            key={machine.id}
            machine={machine}
            actions={machine.isCurrent ? localMachineActions : undefined}
          />
        ))}
      </div>
    </div>
  );
}

function MachineRow({
  machine,
  actions,
}: {
  machine: RuntimeMachine;
  actions?: React.ReactNode;
}) {
  const { t } = useT("runtimes");
  const healthLabel = useHealthLabel();
  const timeAgo = useTimeAgo();
  const paths = useWorkspacePaths();
  const Icon = machine.section === "cloud" ? Cloud : Monitor;
  const locator = machine.id;
  const busyCount = machine.runningCount + machine.queuedCount;
  const body = (
    <>
      <span className="relative flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border bg-background">
        <Icon aria-hidden="true" className="h-4 w-4 text-muted-foreground" />
        <HealthDot
          health={machine.health}
          className="absolute -bottom-0.5 -right-0.5 ring-2 ring-background"
        />
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 flex-wrap items-center gap-2">
          <span className="truncate text-sm font-medium">{machine.title}</span>
          {machine.isCurrent && (
            <span className="shrink-0 rounded bg-foreground px-1.5 py-0.5 text-[10px] font-medium text-background">
              {t(($) => $.machine.this_machine)}
            </span>
          )}
        </span>
        <span className="mt-1 block truncate text-xs text-muted-foreground">
          {machine.subtitle ??
            (machine.section === "cloud"
              ? t(($) => $.machine.metrics.cloud_worker)
              : t(($) => $.machine.metrics.local_daemon))}
        </span>
      </span>

      <span className="hidden w-36 shrink-0 items-center gap-1.5 text-xs md:flex">
        <HealthIcon health={machine.health} />
        <span>{healthLabel(machine.health)}</span>
      </span>
      <span className="hidden w-40 shrink-0 flex-col gap-1 lg:flex">
        <span className="text-xs text-muted-foreground">
          {t(($) => $.machine.runtime_count, {
            count: machine.runtimes.length,
          })}
        </span>
        <ProviderIconStack providers={machine.providerNames} />
      </span>
      <span className="hidden w-36 shrink-0 text-xs text-muted-foreground xl:block">
        {busyCount > 0
          ? t(($) => $.machine.metrics.workload_hint, {
              running: machine.runningCount,
              queued: machine.queuedCount,
            })
          : t(($) => $.machine.metrics.workload_idle)}
      </span>
      <span className="hidden w-28 shrink-0 text-right text-xs text-muted-foreground lg:block">
        {machine.lastSeenAt ? timeAgo(machine.lastSeenAt) : "—"}
      </span>
      {locator && (
        <ChevronRight
          aria-hidden="true"
          className="h-4 w-4 shrink-0 text-muted-foreground/50 transition-transform group-hover:translate-x-0.5 group-hover:text-muted-foreground"
        />
      )}
    </>
  );

  return (
    <div className="flex min-w-0 items-center">
      <AppLink
        href={paths.runtimeDetail(locator)}
        className="group flex min-w-0 flex-1 items-center gap-3 px-4 py-3.5 transition-colors hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
      >
        {body}
      </AppLink>
      {actions && <div className="shrink-0 pr-4">{actions}</div>}
    </div>
  );
}

function ProviderIconStack({ providers }: { providers: string[] }) {
  const visible = providers.slice(0, 4);
  const extra = providers.length - visible.length;
  if (visible.length === 0) return null;
  return (
    <span className="flex min-w-0 items-center -space-x-1">
      {visible.map((provider) => (
        <span
          key={provider}
          className="inline-flex h-5 w-5 items-center justify-center rounded bg-background ring-1 ring-border"
        >
          <ProviderLogo provider={provider} className="h-3.5 w-3.5" />
        </span>
      ))}
      {extra > 0 && (
        <span className="inline-flex h-5 min-w-5 items-center justify-center rounded bg-muted px-1 text-[10px] font-medium text-muted-foreground ring-1 ring-border">
          +{extra}
        </span>
      )}
    </span>
  );
}

function EmptyState({ onConnectRemote }: { onConnectRemote: () => void }) {
  const { t } = useT("runtimes");
  return (
    <CollectionPageState
      icon={Server}
      title={t(($) => $.page.empty.title)}
      description={t(($) => $.page.empty.hint)}
      actions={
        <Button type="button" size="sm" onClick={onConnectRemote}>
          <Plus aria-hidden="true" className="size-3" />
          {t(($) => $.page.connect_remote)}
        </Button>
      }
    />
  );
}

function RuntimesPageSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader className="justify-between px-5">
        <Skeleton className="h-4 w-24" />
      </PageHeader>
      <div className="mx-auto w-full max-w-[1440px] p-6">
        <div className="overflow-hidden rounded-lg border">
          {Array.from({ length: 5 }).map((_, index) => (
            <div key={index} className="flex h-[76px] items-center gap-3 border-b px-4 last:border-b-0">
              <Skeleton className="h-10 w-10 rounded-lg" />
              <div className="flex-1">
                <Skeleton className="h-4 w-44" />
                <Skeleton className="mt-2 h-3 w-28" />
              </div>
              <Skeleton className="hidden h-4 w-24 md:block" />
              <Skeleton className="hidden h-4 w-28 lg:block" />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export default RuntimesPage;
