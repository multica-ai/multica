"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ChevronRight,
  Cloud,
  Loader2,
  Monitor,
  Plus,
  Server,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberNeedsMikaSetup, useBootstrapMika } from "@multica/core/onboarding";
import { MIKA_PLACEHOLDER_EMOJI } from "../../onboarding/components/mika-intro";
import { useRequiredWorkspaceSlug, useWorkspacePaths } from "@multica/core/paths";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { runtimeProfileListOptions } from "@multica/core/runtimes";
import { runtimeListOptions, runtimeKeys } from "@multica/core/runtimes/queries";
import { useWSEvent } from "@multica/core/realtime";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { AgentRuntime } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  MikaRuntimeChoice,
  type MikaRuntimeSelection,
} from "./mika-runtime-choice";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
  CollectionPageState,
} from "../../layout/collection-page";
import { PageHeader } from "../../layout/page-header";
import { AppLink, useNavigation } from "../../navigation";
import {
  getMikaOnboarding,
  pickContentLang,
} from "../../onboarding/templates";
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
  const { data: agents = [], isLoading: agentsLoading } = useQuery(
    agentListOptions(wsId),
  );
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  // The Mika entrypoint is per member, not per workspace: the agent alone does
  // not say whether *this* member's conversation was ever opened and kicked
  // off. See memberNeedsMikaSetup.
  const { data: chatSessions = [], isLoading: chatSessionsLoading } = useQuery(
    chatSessionsOptions(wsId),
  );

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
            {!agentsLoading &&
              !chatSessionsLoading &&
              memberNeedsMikaSetup(agents, chatSessions) &&
              runtimes.length > 0 && (
              <MikaSetupCard
                workspaceId={wsId}
                runtimes={runtimes}
                runtimesLoading={runtimesLoading}
                currentUserId={currentUserId ?? null}
              />
            )}
            {(machines.length > 0 || bootstrapping) && (
              <MachineList
                machines={machines}
                bootstrapping={bootstrapping}
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

/**
 * Entry point for creating Mika once a runtime exists.
 *
 * The action opens a picker rather than provisioning straight away. It used to
 * take `runtimes.find(online) ?? runtimes[0]` and create Mika on it silently —
 * but one machine commonly exposes every agent CLI it has installed (nine, on
 * the box this was reported from), so "the first online one" is arbitrary and
 * could well be a CLI the member never intended to run their Chief of Staff
 * on. Onboarding already makes this an explicit choice; this is the same
 * decision reached from a different entry point, so it asks the same way and
 * reuses the same two controls.
 */
function MikaSetupCard({
  workspaceId,
  runtimes,
  runtimesLoading,
  currentUserId,
}: {
  workspaceId: string;
  runtimes: AgentRuntime[];
  runtimesLoading?: boolean;
  currentUserId: string | null;
}) {
  const { t, i18n } = useT("runtimes");
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const wsSlug = useRequiredWorkspaceSlug();
  const bootstrapMika = useBootstrapMika(workspaceId);

  const [open, setOpen] = useState(false);
  // Seeded with the old heuristic so the dialog opens on a sensible default;
  // the point is that it is now visible and changeable, not that it is unset.
  const defaultRuntimeId =
    runtimes.find((runtime) => runtime.status === "online")?.id ??
    runtimes[0]?.id ??
    "";
  const [choice, setChoice] = useState<MikaRuntimeSelection | null>(null);

  const value: MikaRuntimeSelection = choice ?? {
    runtimeId: defaultRuntimeId,
    model: "",
  };
  const runtimeId = value.runtimeId;

  const handleStart = async () => {
    if (!runtimeId || bootstrapMika.isPending) return;
    const lang = pickContentLang(i18n.language);
    try {
      const result = await bootstrapMika.mutateAsync({
        workspaceSlug: wsSlug,
        runtimeId,
        model: value.model || undefined,
        ...getMikaOnboarding(lang),
      });
      setOpen(false);
      navigation.push(paths.chatSession(result.chatSession.id));
    } catch (error) {
      toast.error(
        error instanceof Error ? error.message : t(($) => $.mika_setup.failed),
      );
    }
  };

  return (
    <>
      <div className="mb-6 flex flex-col gap-4 rounded-xl border bg-card p-5 sm:flex-row sm:items-center">
        <span
          role="img"
          aria-label={t(($) => $.mika_setup.title)}
          className="flex size-10 shrink-0 select-none items-center justify-center rounded-full bg-muted text-title-lg leading-none"
        >
          {MIKA_PLACEHOLDER_EMOJI}
        </span>
        <div className="min-w-0 flex-1">
          <h2 className="text-body font-semibold">
            {t(($) => $.mika_setup.title)}
          </h2>
          <p className="mt-1 text-body leading-relaxed text-muted-foreground">
            {t(($) => $.mika_setup.description)}
          </p>
        </div>
        <Button className="shrink-0" onClick={() => setOpen(true)}>
          {t(($) => $.mika_setup.action)}
        </Button>
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-[480px]">
          <DialogHeader>
            <DialogTitle>{t(($) => $.mika_setup.dialog_title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.mika_setup.dialog_description)}
            </DialogDescription>
          </DialogHeader>

          <MikaRuntimeChoice
            runtimes={runtimes}
            runtimesLoading={runtimesLoading}
            currentUserId={currentUserId}
            value={value}
            onChange={setChoice}
            disabled={bootstrapMika.isPending}
          />

          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => setOpen(false)}
              disabled={bootstrapMika.isPending}
            >
              {t(($) => $.mika_setup.cancel)}
            </Button>
            <Button
              onClick={handleStart}
              disabled={!runtimeId || bootstrapMika.isPending}
            >
              {bootstrapMika.isPending && (
                <Loader2 aria-hidden className="size-4 animate-spin" />
              )}
              {t(($) => $.mika_setup.action)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
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
        <h2 className="text-body font-semibold">
          {t(($) => $.profiles.unassigned_title)}
        </h2>
        <p className="mt-1 text-caption text-muted-foreground">
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
}: {
  machines: RuntimeMachine[];
  bootstrapping?: boolean;
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
          <MachineRow key={machine.id} machine={machine} />
        ))}
      </div>
    </div>
  );
}

function MachineRow({ machine }: { machine: RuntimeMachine }) {
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
        <span className="block truncate text-body font-medium">
          {machine.title}
        </span>
        <span className="mt-1 flex min-w-0 items-center gap-2 text-caption text-muted-foreground">
          <span className="truncate">
            {machine.subtitle ??
              (machine.section === "cloud"
                ? t(($) => $.machine.metrics.cloud_worker)
                : t(($) => $.machine.metrics.local_daemon))}
          </span>
          {machine.isCurrent && (
            <span className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-micro font-medium text-muted-foreground">
              {t(($) => $.machine.this_machine)}
            </span>
          )}
        </span>
      </span>

      <span className="hidden w-36 shrink-0 items-center gap-1.5 text-caption md:flex">
        <HealthIcon health={machine.health} />
        <span>{healthLabel(machine.health)}</span>
      </span>
      <span className="hidden w-40 shrink-0 flex-col gap-1 lg:flex">
        <span className="text-caption text-muted-foreground">
          {t(($) => $.machine.runtime_count, {
            count: machine.runtimes.length,
          })}
        </span>
        <ProviderIconStack providers={machine.providerNames} />
      </span>
      <span className="hidden w-36 shrink-0 text-caption text-muted-foreground xl:block">
        {busyCount > 0
          ? t(($) => $.machine.metrics.workload_hint, {
              running: machine.runningCount,
              queued: machine.queuedCount,
            })
          : t(($) => $.machine.metrics.workload_idle)}
      </span>
      <span className="hidden w-28 shrink-0 text-right text-caption text-muted-foreground lg:block">
        {machine.lastSeenAt ? timeAgo(machine.lastSeenAt) : "—"}
      </span>
      {locator && (
        <ChevronRight
          aria-hidden="true"
          className="h-4 w-4 shrink-0 text-faint-foreground transition-transform group-hover:translate-x-0.5 group-hover:text-muted-foreground"
        />
      )}
    </>
  );

  return (
    <AppLink
      href={paths.runtimeDetail(locator)}
      className="group flex min-w-0 items-center gap-3 px-4 py-3.5 transition-colors hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring"
    >
      {body}
    </AppLink>
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
        <span className="inline-flex h-5 min-w-5 items-center justify-center rounded bg-muted px-1 text-micro font-medium text-muted-foreground ring-1 ring-border">
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
