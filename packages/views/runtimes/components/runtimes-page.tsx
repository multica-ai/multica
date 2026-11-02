"use client";

import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  Check,
  Cloud,
  Download,
  Laptop,
  Loader2,
  Server,
  SquareTerminal,
} from "@multica/ui/icons";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  memberNeedsMikaSetup,
  useBootstrapMika,
} from "@multica/core/onboarding";
import { MIKA_PLACEHOLDER_EMOJI } from "../../onboarding/components/mika-intro";
import {
  useRequiredWorkspaceSlug,
  useWorkspacePaths,
} from "@multica/core/paths";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { chatSessionsOptions } from "@multica/core/chat/queries";
import { runtimeProfileListOptions } from "@multica/core/runtimes";
import {
  runtimeListOptions,
  runtimeKeys,
} from "@multica/core/runtimes/queries";
import { useWSEvent } from "@multica/core/realtime";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import type { AgentRuntime, MemberWithUser } from "@multica/core/types";
import type { QoderConnection } from "@multica/core/runtimes/qoder";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  MikaRuntimeChoice,
  type MikaRuntimeSelection,
} from "./mika-runtime-choice";
import {
  CollectionCreateButton,
  CollectionPageHeader,
  CollectionScopeItems,
} from "../../layout/collection-page";
import {
  CollectionEmptyValue,
  CollectionList,
  CollectionNameCell,
  CollectionTag,
  type CollectionColumn,
  type CollectionGroup,
} from "../../layout/collection-list";
import { cn } from "@multica/ui/lib/utils";
import { useNavigation } from "../../navigation";
import { getMikaOnboarding, pickContentLang } from "../../onboarding/templates";
import { qoderOptions } from "@multica/core/runtimes/qoder";
import { QODER_RUNTIME_LOCATOR } from "./qoder-detail-page";
import { RemoteRuntimeDialog } from "./remote-runtime-dialog";
import { ConnectRemoteDialog } from "./connect-remote-dialog";
import { CloudRuntimeDialog } from "./cloud-runtime-dialog";
import { ProviderLogo } from "./provider-logo";
import { RuntimeList, buildWorkloadIndex } from "./runtime-list";
import { pendingRuntimeFromProfile } from "./pending-runtime";
import { buildRuntimeMachines, type RuntimeMachine } from "./runtime-machines";
import {
  HealthIcon,
  ProviderIconStack,
  machineIcon,
  useHealthLabel,
} from "./shared";
import { useT, useTimeAgo } from "../../i18n";

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

type StatusFilter = "all" | "online" | "offline";
const STATUS_FILTERS: StatusFilter[] = ["all", "online", "offline"];

const COMING_SOON_SERVICES = [
  { provider: "cursor", name: "Cursor Cloud Agents", vendor: "Cursor" },
  { provider: "claude", name: "Claude Managed Agents", vendor: "Anthropic" },
];

type RuntimeRow =
  | { kind: "machine"; id: string; machine: RuntimeMachine; agentCount: number }
  | { kind: "qoder"; id: string; connection: QoderConnection; agentCount: number }
  | { kind: "coming-soon"; id: string; provider: string; name: string; vendor: string }
  | {
      kind: "action";
      id: string;
      icon: typeof Server;
      title: string;
      description: string;
      action: ReactNode;
    };

type QoderStatus = "stopped" | "online" | "error" | "offline" | "starting";

function useNowTick(intervalMs = 30_000): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(id);
  }, [intervalMs]);
  return now;
}

function qoderIsOnline(connection: QoderConnection): boolean {
  return (
    connection.configured &&
    connection.enabled &&
    connection.status === "online"
  );
}

function qoderStatus(connection: QoderConnection): QoderStatus {
  if (!connection.enabled) return "stopped";
  if (connection.status === "online") return "online";
  if (connection.status === "error") return "error";
  if (connection.status === "offline") return "offline";
  return "starting";
}

const QODER_DOT: Record<QoderStatus, string> = {
  online: "bg-success",
  error: "bg-destructive",
  starting: "bg-warning",
  offline: "bg-muted-foreground/40",
  stopped: "bg-muted-foreground/40",
};

export function RuntimesPage({
  localDaemonId,
  localMachineName,
  hasLocalMachine,
  bootstrapping,
  cloudRuntimeEnabled = false,
}: RuntimesPageProps = {}) {
  const { t } = useT("runtimes");
  const isAuthLoading = useAuthStore((state) => state.isLoading);
  const currentUserId = useAuthStore((state) => state.user?.id);
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const paths = useWorkspacePaths();
  const { data: qoderConnection } = useQuery(qoderOptions(wsId));
  const navigation = useNavigation();
  const [qoderDialogOpen, setQoderDialogOpen] = useState(false);
  const [showConnectDialog, setShowConnectDialog] = useState(false);
  const [showCloudRuntimeDialog, setShowCloudRuntimeDialog] = useState(false);
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");

  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: runtimeProfiles = [], isLoading: profilesLoading } = useQuery(
    runtimeProfileListOptions(wsId),
  );
  const { data: agents = [], isLoading: agentsLoading } = useQuery(
    agentListOptions(wsId),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
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
  const agentCountByRuntime = useMemo(() => {
    const counts = new Map<string, number>();
    for (const agent of agents) {
      if (!agent.runtime_id) continue;
      counts.set(agent.runtime_id, (counts.get(agent.runtime_id) ?? 0) + 1);
    }
    return counts;
  }, [agents]);
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

  // The header, toolbar and column headers render through loading; only the
  // rows wait, so nothing above them moves when the list lands.
  const listLoading = isAuthLoading || runtimesLoading || profilesLoading;

  const isDesktop = hasLocalMachine === true;
  const qoder = qoderConnection ?? null;
  const qoderHref = paths.runtimeDetail(QODER_RUNTIME_LOCATOR);
  // Qoder's runtime registers like a machine, but it is listed once, as a
  // cloud service — never as a machine, even when this server cannot run
  // Qoder and the cloud row is hidden.
  const machineList = machines.filter(
    (machine) =>
      !(
        machine.runtimes.length > 0 &&
        machine.runtimes.every((runtime) => runtime.provider === "qoder_cloud")
      ),
  );
  const qoderAgentCount = agents.filter((agent) =>
    runtimes.some(
      (runtime) =>
        runtime.id === agent.runtime_id && runtime.provider === "qoder_cloud",
    ),
  ).length;
  const localMachine = machineList.find((machine) => machine.isCurrent);

  const filtering = statusFilter !== "all";
  const matchesStatus = (online: boolean) =>
    statusFilter === "all" || (statusFilter === "online") === online;

  const visibleMachines = machineList.filter((machine) =>
    matchesStatus(machine.health === "online"),
  );
  const qoderConfigured = qoder?.configured === true;
  const showQoderRow =
    qoder !== null &&
    (statusFilter === "all" ||
      (qoderConfigured && matchesStatus(qoderIsOnline(qoder))));
  const visibleComingSoon = statusFilter === "all" ? COMING_SOON_SERVICES : [];
  const showThisComputerRow =
    !filtering && !isDesktop && machineList.length === 0;
  const showAddMachineRow = !filtering && machineList.length === 0;
  const nothingMatches =
    filtering &&
    visibleMachines.length === 0 &&
    !showQoderRow &&
    visibleComingSoon.length === 0;

  const configuredCloudCount = qoderConfigured ? 1 : 0;
  const totalCount = machineList.length + configuredCloudCount;
  const onlineCount =
    machineList.filter((machine) => machine.health === "online").length +
    (qoder && qoderIsOnline(qoder) ? 1 : 0);
  const statusCounts: Record<StatusFilter, number> = {
    all: totalCount,
    online: onlineCount,
    offline: totalCount - onlineCount,
  };

  const scopeLabels: Record<StatusFilter, string> = {
    all: t(($) => $.page.filters.all),
    online: t(($) => $.page.filters.online),
    offline: t(($) => $.page.filters.offline),
  };

  const openQoder = () =>
    qoderConfigured ? navigation.push(qoderHref) : setQoderDialogOpen(true);

  const columns = useMemo<CollectionColumn<RuntimeRow>[]>(
    () => [
      {
        key: "name",
        header: t(($) => $.page.columns.name),
        cell: (row) => (
          <RuntimeNameCell
            row={row}
            currentUserId={currentUserId ?? null}
            members={members}
            onConnectQoder={() => setQoderDialogOpen(true)}
          />
        ),
      },
      {
        key: "harness",
        header: t(($) => $.page.columns.harness),
        width: 112,
        cell: (row) =>
          row.kind === "machine" ? (
            <ProviderIconStack providers={row.machine.providerNames} />
          ) : row.kind === "qoder" ? (
            <CollectionEmptyValue />
          ) : null,
      },
      {
        key: "agents",
        header: t(($) => $.page.columns.agents),
        width: 56,
        align: "end",
        className: "tabular-nums",
        cell: (row) =>
          row.kind === "machine" || (row.kind === "qoder" && row.connection.configured)
            ? row.agentCount
            : row.kind === "qoder"
              ? <CollectionEmptyValue />
              : null,
      },
      {
        key: "workload",
        header: t(($) => $.page.columns.workload),
        width: 112,
        cell: (row) => <RuntimeWorkloadCell row={row} />,
      },
      {
        key: "status",
        header: t(($) => $.page.columns.status),
        width: 136,
        core: true,
        cell: (row) => <RuntimeStatusCell row={row} bootstrapping={bootstrapping} />,
      },
    ],
    [t, currentUserId, members, bootstrapping],
  );

  const machineRows: RuntimeRow[] = visibleMachines.map((machine) => ({
    kind: "machine",
    id: machine.id,
    machine,
    agentCount: machine.runtimes.reduce(
      (sum, runtime) => sum + (agentCountByRuntime.get(runtime.id) ?? 0),
      0,
    ),
  }));
  if (showThisComputerRow) {
    machineRows.push({
      kind: "action",
      id: "action:this-computer",
      icon: Laptop,
      title: t(($) => $.page.rows.this_computer_title),
      description: t(($) => $.page.rows.this_computer_hint),
      action: (
        <a
          href="/download"
          target="_blank"
          rel="noreferrer"
          className={buttonVariants({ variant: "outline", size: "xs" })}
        >
          <Download aria-hidden="true" className="size-3.5" />
          {t(($) => $.page.rows.download_desktop)}
        </a>
      ),
    });
  }
  if (showAddMachineRow) {
    machineRows.push({
      kind: "action",
      id: "action:add-machine",
      icon: SquareTerminal,
      title: t(($) => $.page.rows.add_machine_title),
      description: t(($) => $.page.rows.add_machine_hint),
      action: (
        <Button variant="outline" size="xs" onClick={() => setShowConnectDialog(true)}>
          {t(($) => $.page.rows.view_commands)}
        </Button>
      ),
    });
  }
  const cloudRows: RuntimeRow[] = [];
  if (showQoderRow && qoder) {
    cloudRows.push({
      kind: "qoder",
      id: "qoder",
      connection: qoder,
      agentCount: qoderAgentCount,
    });
  }
  for (const service of visibleComingSoon) {
    cloudRows.push({ kind: "coming-soon", id: `soon:${service.provider}`, ...service });
  }
  const groups: CollectionGroup<RuntimeRow>[] = nothingMatches
    ? []
    : [
        {
          key: "machines",
          label: t(($) => $.page.groups.machines.title),
          count: machineList.length,
          rows: machineRows,
        },
        {
          key: "cloud",
          label: t(($) => $.page.groups.cloud.title),
          count: configuredCloudCount,
          rows: cloudRows,
        },
      ];
  const rowHref = (row: RuntimeRow) =>
    row.kind === "machine"
      ? paths.runtimeDetail(row.machine.id)
      : row.kind === "qoder" && row.connection.configured
        ? qoderHref
        : null;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader
        icon={Server}
        title={scopeLabels[statusFilter]}
        menu={
          <CollectionScopeItems
            value={statusFilter}
            onChange={setStatusFilter}
            options={STATUS_FILTERS.map((filter) => ({
              value: filter,
              label: scopeLabels[filter],
              count: statusCounts[filter],
            }))}
          />
        }
        actions={
          <AddRuntimeMenu
            isDesktop={isDesktop}
            localMachineName={localMachineName ?? localMachine?.title ?? null}
            qoder={qoder}
            cloudRuntimeEnabled={cloudRuntimeEnabled}
            onAddMachine={() => setShowConnectDialog(true)}
            onQoder={openQoder}
            onCloudRuntime={() => setShowCloudRuntimeDialog(true)}
          />
        }
      />

      <div className="min-h-0 flex-1 overflow-y-auto">
        <CollectionList
          embedded
          pageShortcuts
          label={scopeLabels[statusFilter]}
          columns={columns}
          groups={groups}
          loading={listLoading}
          getRowId={(row) => row.id}
          getRowHref={rowHref}
          getRowTitle={(row) =>
            row.kind === "machine"
              ? row.machine.title
              : row.kind === "qoder"
                ? t(($) => $.qoder.cloud_title)
                : undefined
          }
          empty={t(($) => $.page.rows.no_matches)}
        />
        <div className="flex flex-col gap-6 px-6 pt-6 pb-16 empty:hidden">
          {!agentsLoading &&
            !chatSessionsLoading &&
            memberNeedsMikaSetup(agents, chatSessions) &&
            runtimes.some((runtime) => runtime.provider !== "qoder_cloud") && (
              <MikaSetupCard
                workspaceId={wsId}
                runtimes={runtimes.filter(
                  (runtime) => runtime.provider !== "qoder_cloud",
                )}
                runtimesLoading={runtimesLoading}
                currentUserId={currentUserId ?? null}
              />
            )}
          {orphanProfileRuntimes.length > 0 && (
            <OrphanRuntimeProfiles
              runtimes={orphanProfileRuntimes}
              now={now}
            />
          )}
        </div>
      </div>

      {qoderDialogOpen && (
        <RemoteRuntimeDialog onClose={() => setQoderDialogOpen(false)} />
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

function AddRuntimeMenu({
  isDesktop,
  localMachineName,
  qoder,
  cloudRuntimeEnabled,
  onAddMachine,
  onQoder,
  onCloudRuntime,
}: {
  isDesktop: boolean;
  localMachineName: string | null;
  qoder: QoderConnection | null;
  cloudRuntimeEnabled: boolean;
  onAddMachine: () => void;
  onQoder: () => void;
  onCloudRuntime: () => void;
}) {
  const { t } = useT("runtimes");
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<CollectionCreateButton label={t(($) => $.page.add)} />}
      />
      <DropdownMenuContent align="end" className="w-80">
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t(($) => $.page.add_menu.machines)}</DropdownMenuLabel>
          {isDesktop ? (
            <AddMenuItem
              icon={<Laptop className="size-4 text-muted-foreground" />}
              title={t(($) => $.page.add_menu.this_computer)}
              description={
                localMachineName
                  ? t(($) => $.page.add_menu.this_computer_connected, {
                      name: localMachineName,
                    })
                  : undefined
              }
              trailing={<Check className="size-4 text-muted-foreground" />}
              disabled
            />
          ) : (
            <AddMenuItem
              icon={<Laptop className="size-4 text-muted-foreground" />}
              title={t(($) => $.page.add_menu.this_computer)}
              description={t(($) => $.page.add_menu.this_computer_download)}
              render={<a href="/download" target="_blank" rel="noreferrer" />}
            />
          )}
          <AddMenuItem
            icon={<SquareTerminal className="size-4 text-muted-foreground" />}
            title={t(($) => $.page.add_menu.other_machine)}
            description={t(($) => $.page.add_menu.other_machine_hint)}
            onClick={onAddMachine}
          />
        </DropdownMenuGroup>
        <DropdownMenuSeparator />
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t(($) => $.page.add_menu.cloud_services)}</DropdownMenuLabel>
          {qoder && (
            <AddMenuItem
              icon={<ProviderLogo provider="qoder_cloud" className="size-4" />}
              title={t(($) => $.qoder.cloud_title)}
              description={
                qoder.configured
                  ? t(($) => $.page.add_menu.qoder_manage_hint)
                  : t(($) => $.page.rows.provided_by, { vendor: "Qoder" })
              }
              trailing={
                qoder.configured ? (
                  <Check className="size-4 text-muted-foreground" />
                ) : undefined
              }
              onClick={onQoder}
            />
          )}
          {cloudRuntimeEnabled && (
            <AddMenuItem
              icon={<Cloud className="size-4 text-muted-foreground" />}
              title={t(($) => $.cloud_runtime.action)}
              description={t(($) => $.page.add_menu.cloud_runtime_hint)}
              onClick={onCloudRuntime}
            />
          )}
          {COMING_SOON_SERVICES.map((service) => (
            <AddMenuItem
              key={service.provider}
              icon={<ProviderLogo provider={service.provider} className="size-4" />}
              title={service.name}
              description={t(($) => $.page.rows.provided_by, {
                vendor: service.vendor,
              })}
              trailing={
                <Badge variant="secondary" className="shrink-0">
                  {t(($) => $.page.groups.cloud.coming_soon)}
                </Badge>
              }
              disabled
            />
          ))}
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function AddMenuItem({
  icon,
  title,
  description,
  trailing,
  ...props
}: {
  icon: ReactNode;
  title: string;
  description?: string;
  trailing?: ReactNode;
} & Omit<React.ComponentProps<typeof DropdownMenuItem>, "children" | "title">) {
  return (
    <DropdownMenuItem className="gap-2.5 px-2 py-1.5" {...props}>
      <span className="flex size-7 shrink-0 items-center justify-center rounded-md border bg-background">
        {icon}
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate text-body font-medium">{title}</span>
        {description && (
          <span className="truncate text-caption text-muted-foreground">
            {description}
          </span>
        )}
      </span>
      {trailing}
    </DropdownMenuItem>
  );
}

function RuntimeNameCell({
  row,
  currentUserId,
  members,
  onConnectQoder,
}: {
  row: RuntimeRow;
  currentUserId: string | null;
  members: MemberWithUser[];
  onConnectQoder: () => void;
}) {
  const { t } = useT("runtimes");
  switch (row.kind) {
    case "machine": {
      const { machine } = row;
      const Icon = machineIcon(machine);
      const ownerId = machine.runtimes[0]?.owner_id ?? null;
      const owner =
        ownerId && ownerId === currentUserId
          ? t(($) => $.page.rows.owner_you)
          : (members.find((member) => member.user_id === ownerId)?.name ?? null);
      const description =
        [machine.deviceSummary, owner].filter(Boolean).join(" · ") ||
        (machine.section === "cloud"
          ? t(($) => $.machine.metrics.cloud_worker)
          : t(($) => $.machine.metrics.local_daemon));
      return (
        <CollectionNameCell
          icon={<Icon aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />}
          title={machine.title}
          badges={
            machine.isCurrent ? (
              <CollectionTag>{t(($) => $.machine.this_machine)}</CollectionTag>
            ) : undefined
          }
          description={description}
        />
      );
    }
    case "qoder": {
      const { connection } = row;
      const byQoder = t(($) => $.page.rows.provided_by, { vendor: "Qoder" });
      const error =
        connection.configured &&
        qoderStatus(connection) === "error" &&
        connection.lastError;
      return (
        <CollectionNameCell
          icon={<ProviderLogo provider="qoder_cloud" className="size-4 shrink-0" />}
          title={t(($) => $.qoder.cloud_title)}
          description={error || byQoder}
          trailing={
            connection.configured ? undefined : (
              <Button variant="outline" size="xs" onClick={onConnectQoder}>
                {t(($) => $.page.rows.connect)}
              </Button>
            )
          }
        />
      );
    }
    case "coming-soon":
      return (
        <CollectionNameCell
          icon={
            <span className="shrink-0 opacity-60">
              <ProviderLogo provider={row.provider} className="size-4" />
            </span>
          }
          title={row.name}
          description={t(($) => $.page.rows.provided_by, { vendor: row.vendor })}
          muted
        />
      );
    case "action": {
      const Icon = row.icon;
      return (
        <CollectionNameCell
          icon={
            <span className="flex size-5 shrink-0 items-center justify-center rounded-sm border border-dashed text-muted-foreground">
              <Icon aria-hidden="true" className="size-3" />
            </span>
          }
          title={row.title}
          description={row.description}
          trailing={<span className="shrink-0">{row.action}</span>}
        />
      );
    }
  }
}

function WorkloadText({
  running,
  queued,
}: {
  running: number;
  queued: number;
}) {
  const { t } = useT("runtimes");
  if (running + queued === 0) return <>{t(($) => $.machine.metrics.workload_idle)}</>;
  return (
    <span className="truncate tabular-nums text-foreground">
      {t(($) => $.machine.metrics.workload_hint, { running, queued })}
    </span>
  );
}

function RuntimeWorkloadCell({ row }: { row: RuntimeRow }) {
  if (row.kind === "machine") {
    return row.machine.health === "online" ? (
      <WorkloadText running={row.machine.runningCount} queued={row.machine.queuedCount} />
    ) : (
      <CollectionEmptyValue />
    );
  }
  if (row.kind === "qoder") {
    return row.connection.configured && qoderStatus(row.connection) === "online" ? (
      <WorkloadText running={row.connection.activeRuns} queued={row.connection.queuedRuns} />
    ) : (
      <CollectionEmptyValue />
    );
  }
  return null;
}

function RuntimeStatusCell({
  row,
  bootstrapping,
}: {
  row: RuntimeRow;
  bootstrapping?: boolean;
}) {
  const { t } = useT("runtimes");
  const healthLabel = useHealthLabel();
  const timeAgo = useTimeAgo();
  switch (row.kind) {
    case "machine": {
      const { machine } = row;
      if (machine.isCurrent && bootstrapping && machine.runtimes.length === 0) {
        return (
          <>
            <Loader2 aria-hidden="true" className="size-3 shrink-0 animate-spin" />
            <span className="truncate">{t(($) => $.page.bootstrapping.title)}</span>
          </>
        );
      }
      const online = machine.health === "online";
      return (
        <>
          <HealthIcon health={machine.health} />
          <span className={cn("truncate", online && "text-foreground")}>
            {healthLabel(machine.health)}
            {!online && machine.lastSeenAt ? ` · ${timeAgo(machine.lastSeenAt)}` : ""}
          </span>
        </>
      );
    }
    case "qoder": {
      if (!row.connection.configured) {
        return (
          <Badge variant="secondary" className="bg-warning/10 text-warning">
            {t(($) => $.page.rows.not_connected)}
          </Badge>
        );
      }
      const status = qoderStatus(row.connection);
      return (
        <>
          <span aria-hidden="true" className={cn("size-1.5 shrink-0 rounded-full", QODER_DOT[status])} />
          <span
            className={cn(
              "truncate",
              status === "online" && "text-foreground",
              status === "error" && "text-destructive",
            )}
          >
            {t(($) => $.qoder[status])}
          </span>
        </>
      );
    }
    case "coming-soon":
      return <Badge variant="secondary">{t(($) => $.page.groups.cloud.coming_soon)}</Badge>;
    case "action":
      return null;
  }
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
      <div className="flex flex-col gap-4 rounded-xl border bg-card p-5 sm:flex-row sm:items-center">
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
}: {
  runtimes: ReturnType<typeof pendingRuntimeFromProfile>[];
  now: number;
}) {
  const { t } = useT("runtimes");
  return (
    <section>
      <div className="mb-3">
        <h2 className="text-body font-semibold">
          {t(($) => $.profiles.unassigned_title)}
        </h2>
        <p className="mt-1 text-caption text-muted-foreground">
          {t(($) => $.profiles.unassigned_description)}
        </p>
      </div>
      <RuntimeList runtimes={runtimes} now={now} />
    </section>
  );
}

export default RuntimesPage;
