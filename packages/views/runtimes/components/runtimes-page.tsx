"use client";

import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Cloud,
  Download,
  Laptop,
  Loader2,
  Monitor,
  Plus,
  Search,
  Server,
  SquareTerminal,
} from "lucide-react";
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
import { Input } from "@multica/ui/components/ui/input";
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
  ListGrid,
  ListGridCell,
  ListGridHeader,
  ListGridHeaderCell,
  ListGridRow,
} from "@multica/ui/components/ui/list-grid";
import {
  MikaRuntimeChoice,
  type MikaRuntimeSelection,
} from "./mika-runtime-choice";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { CollectionPageHeader } from "../../layout/collection-page";
import { PAGE_GUTTER, PAGE_RAIL, PageHeader } from "../../layout/page-header";
import { cn } from "@multica/ui/lib/utils";
import {
  AppLink,
  rowLinkInteractiveProps,
  useNavigation,
  useRowLink,
} from "../../navigation";
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

type StatusFilter = "all" | "online" | "offline";
const STATUS_FILTERS: StatusFilter[] = ["all", "online", "offline"];

const COMING_SOON_SERVICES = [
  { provider: "cursor", name: "Cursor Cloud Agents", vendor: "Cursor" },
  { provider: "claude", name: "Claude Managed Agents", vendor: "Anthropic" },
];

// Track order: edge, name, harness, agents, workload, status, actions, edge.
// Below @2xl only name, status and actions keep a track; the other cells are
// `hidden @2xl:flex` and drop out of subgrid placement. MUST stay a literal
// string so Tailwind sees the arbitrary template.
const GRID_COLS =
  "grid-cols-[0.5rem_minmax(160px,1fr)_112px_5.5rem_0.5rem] " +
  "@2xl:grid-cols-[0.5rem_minmax(120px,1fr)_128px_64px_112px_136px_5.5rem_0.5rem]";

const OPTIONAL_CELL = "hidden @2xl:flex";

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

export function RuntimesPage({
  localDaemonId,
  localMachineName,
  hasLocalMachine,
  bootstrapping,
  cloudRuntimeEnabled = false,
}: RuntimesPageProps = {}) {
  const { t, i18n } = useT("runtimes");
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
  const [query, setQuery] = useState("");
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

  if (isAuthLoading || runtimesLoading || profilesLoading) {
    return <RuntimesPageSkeleton />;
  }

  const isDesktop = hasLocalMachine === true;
  const qoder = qoderConnection?.available === true ? qoderConnection : null;
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

  const needle = query.trim().toLowerCase();
  const filtering = needle.length > 0 || statusFilter !== "all";
  const matchesQuery = (...fields: (string | null | undefined)[]) =>
    !needle || fields.some((field) => field?.toLowerCase().includes(needle));
  const matchesStatus = (online: boolean) =>
    statusFilter === "all" || (statusFilter === "online") === online;

  const visibleMachines = machineList.filter(
    (machine) =>
      matchesStatus(machine.health === "online") &&
      matchesQuery(machine.title, machine.subtitle, ...machine.providerNames),
  );
  const qoderConfigured = qoder?.configured === true;
  const showQoderRow =
    qoder !== null &&
    matchesQuery(t(($) => $.qoder.cloud_title), "qoder") &&
    (statusFilter === "all" ||
      (qoderConfigured && matchesStatus(qoderIsOnline(qoder))));
  const visibleComingSoon =
    statusFilter === "all"
      ? COMING_SOON_SERVICES.filter((service) =>
          matchesQuery(service.name, service.vendor),
        )
      : [];
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

  const openQoder = () =>
    qoderConfigured ? navigation.push(qoderHref) : setQoderDialogOpen(true);

  return (
    <div className="flex min-h-0 flex-1 flex-col">
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
        <div className={cn(PAGE_RAIL, PAGE_GUTTER, "flex flex-col gap-3 py-4 sm:py-6")}>
          <RuntimesToolbar
            query={query}
            onQueryChange={setQuery}
            statusFilter={statusFilter}
            onStatusFilterChange={setStatusFilter}
            counts={statusCounts}
          />

          <div className="@container overflow-hidden rounded-lg border bg-card">
            <ListGrid className={cn(GRID_COLS, "[&>[role=row]:last-child]:border-b-0")}>
              <ListGridHeader className="static border-b bg-card after:hidden">
                <ListGridHeaderCell>{t(($) => $.page.columns.name)}</ListGridHeaderCell>
                <ListGridHeaderCell className={OPTIONAL_CELL}>
                  {t(($) => $.page.columns.harness)}
                </ListGridHeaderCell>
                <ListGridHeaderCell className={OPTIONAL_CELL}>
                  {t(($) => $.page.columns.agents)}
                </ListGridHeaderCell>
                <ListGridHeaderCell className={OPTIONAL_CELL}>
                  {t(($) => $.page.columns.workload)}
                </ListGridHeaderCell>
                <ListGridHeaderCell>{t(($) => $.page.columns.status)}</ListGridHeaderCell>
                <ListGridHeaderCell aria-hidden="true" />
              </ListGridHeader>

              {nothingMatches ? (
                <div
                  role="row"
                  className="col-span-full px-4 py-10 text-center text-caption text-muted-foreground"
                >
                  {t(($) => $.page.rows.no_matches)}
                </div>
              ) : (
                <>
                  <GroupHeader
                    title={t(($) => $.page.groups.machines.title)}
                    count={machineList.length}
                  />
                  {visibleMachines.map((machine) => (
                    <MachineRow
                      key={machine.id}
                      machine={machine}
                      bootstrapping={bootstrapping}
                      currentUserId={currentUserId ?? null}
                      members={members}
                      agentCount={machine.runtimes.reduce(
                        (sum, runtime) =>
                          sum + (agentCountByRuntime.get(runtime.id) ?? 0),
                        0,
                      )}
                    />
                  ))}
                  {showThisComputerRow && (
                    <ActionRow
                      icon={Laptop}
                      title={t(($) => $.page.rows.this_computer_title)}
                      description={t(($) => $.page.rows.this_computer_hint)}
                      action={
                        <a
                          href="/download"
                          target="_blank"
                          rel="noreferrer"
                          className={buttonVariants({ variant: "outline", size: "sm" })}
                        >
                          <Download aria-hidden="true" className="size-3.5" />
                          {t(($) => $.page.rows.download_desktop)}
                        </a>
                      }
                    />
                  )}
                  {showAddMachineRow && (
                    <ActionRow
                      icon={SquareTerminal}
                      title={t(($) => $.page.rows.add_machine_title)}
                      description={t(($) => $.page.rows.add_machine_hint)}
                      action={
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => setShowConnectDialog(true)}
                        >
                          {t(($) => $.page.rows.view_commands)}
                        </Button>
                      }
                    />
                  )}

                  <GroupHeader
                    title={t(($) => $.page.groups.cloud.title)}
                    count={configuredCloudCount}
                  />
                  {showQoderRow && qoder && (
                    <QoderRow
                      connection={qoder}
                      agentCount={qoderAgentCount}
                      detailHref={qoderHref}
                      onConnect={() => setQoderDialogOpen(true)}
                    />
                  )}
                  {visibleComingSoon.map((service) => (
                    <ComingSoonRow key={service.provider} {...service} />
                  ))}
                </>
              )}
            </ListGrid>
          </div>

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
              hasMachines={machines.length > 0}
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
        render={
          <Button size="sm" className="gap-1">
            <Plus aria-hidden="true" className="size-3.5" />
            {t(($) => $.page.add)}
            <ChevronDown aria-hidden="true" className="size-3.5 opacity-70" />
          </Button>
        }
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

function RuntimesToolbar({
  query,
  onQueryChange,
  statusFilter,
  onStatusFilterChange,
  counts,
}: {
  query: string;
  onQueryChange: (query: string) => void;
  statusFilter: StatusFilter;
  onStatusFilterChange: (filter: StatusFilter) => void;
  counts: Record<StatusFilter, number>;
}) {
  const { t } = useT("runtimes");
  const labels: Record<StatusFilter, string> = {
    all: t(($) => $.page.filters.all),
    online: t(($) => $.page.filters.online),
    offline: t(($) => $.page.filters.offline),
  };
  return (
    <div className="flex min-w-0 items-center gap-2">
      <div className="relative min-w-0 flex-1 sm:flex-none">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={query}
          onChange={(e) => onQueryChange(e.target.value)}
          aria-label={t(($) => $.page.search_placeholder)}
          placeholder={t(($) => $.page.search_placeholder)}
          className="h-8 w-full pl-8 text-body sm:w-64"
        />
      </div>
      <div className="flex shrink-0 items-center gap-1">
        {STATUS_FILTERS.map((filter) => (
          <Button
            key={filter}
            variant="outline"
            size="sm"
            aria-pressed={statusFilter === filter}
            className={
              statusFilter === filter
                ? "gap-1.5 bg-accent text-accent-foreground hover:bg-accent/80"
                : "gap-1.5 text-muted-foreground"
            }
            onClick={() => onStatusFilterChange(filter)}
          >
            {labels[filter]}
            <span className="tabular-nums text-caption text-muted-foreground">
              {counts[filter]}
            </span>
          </Button>
        ))}
      </div>
    </div>
  );
}

function GroupHeader({ title, count }: { title: string; count: number }) {
  return (
    <div
      role="row"
      className="col-span-full flex h-9 min-w-0 items-center gap-2 border-b bg-muted/40 px-4 text-caption"
    >
      <span className="font-semibold text-foreground">{title}</span>
      <span className="font-mono tabular-nums text-muted-foreground">{count}</span>
    </div>
  );
}

function ActionRow({
  icon: Icon,
  title,
  description,
  action,
}: {
  icon: typeof Server;
  title: string;
  description: string;
  action: ReactNode;
}) {
  return (
    <div
      role="row"
      className="col-span-full flex min-h-14 items-center gap-3 border-b px-4 py-2.5"
    >
      <span className="flex size-8 shrink-0 items-center justify-center rounded-lg border border-dashed text-muted-foreground">
        <Icon aria-hidden="true" className="size-4" />
      </span>
      <span className="min-w-0 flex-1">
        <span className="block truncate text-body font-medium">{title}</span>
        <span className="block truncate text-caption text-muted-foreground">
          {description}
        </span>
      </span>
      <span className="shrink-0">{action}</span>
    </div>
  );
}

function RowTile({
  children,
  dot,
}: {
  children: ReactNode;
  dot?: ReactNode;
}) {
  return (
    <span className="relative flex size-8 shrink-0 items-center justify-center rounded-lg border bg-background">
      {children}
      {dot}
    </span>
  );
}

function NameCell({
  tile,
  title,
  href,
  badge,
  subtitle,
  muted,
}: {
  tile: ReactNode;
  title: string;
  href?: string;
  badge?: ReactNode;
  subtitle: string;
  muted?: boolean;
}) {
  return (
    <ListGridCell className="gap-3 py-2.5">
      {tile}
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-center gap-2">
          {href ? (
            <AppLink
              href={href}
              className="truncate text-body font-medium focus-visible:outline-none focus-visible:underline"
              {...rowLinkInteractiveProps}
            >
              {title}
            </AppLink>
          ) : (
            <span
              className={cn(
                "truncate text-body font-medium",
                muted && "text-muted-foreground",
              )}
            >
              {title}
            </span>
          )}
          {badge}
        </span>
        <span className="block truncate text-caption text-muted-foreground">
          {subtitle}
        </span>
      </span>
    </ListGridCell>
  );
}

function EmptyCell({ optional = true }: { optional?: boolean }) {
  return (
    <ListGridCell className={cn(optional && OPTIONAL_CELL, "text-caption text-faint-foreground")}>
      —
    </ListGridCell>
  );
}

function RowChevron() {
  return (
    <ListGridCell className="justify-end">
      <ChevronRight
        aria-hidden="true"
        className="size-4 text-faint-foreground transition-transform group-hover/row:translate-x-0.5 group-hover/row:text-muted-foreground"
      />
    </ListGridCell>
  );
}

function WorkloadText({
  running,
  queued,
}: {
  running: number;
  queued: number;
}) {
  const { t } = useT("runtimes");
  if (running + queued === 0) {
    return (
      <span className="text-caption text-muted-foreground">
        {t(($) => $.machine.metrics.workload_idle)}
      </span>
    );
  }
  return (
    <span className="truncate text-caption tabular-nums">
      {t(($) => $.machine.metrics.workload_hint, { running, queued })}
    </span>
  );
}

function MachineRow({
  machine,
  bootstrapping,
  currentUserId,
  members,
  agentCount,
}: {
  machine: RuntimeMachine;
  bootstrapping?: boolean;
  currentUserId: string | null;
  members: MemberWithUser[];
  agentCount: number;
}) {
  const { t } = useT("runtimes");
  const healthLabel = useHealthLabel();
  const timeAgo = useTimeAgo();
  const paths = useWorkspacePaths();
  const rowLink = useRowLink();
  const href = paths.runtimeDetail(machine.id);
  const Icon = machine.isCurrent
    ? Laptop
    : machine.section === "cloud"
      ? Cloud
      : Monitor;
  const ownerId = machine.runtimes[0]?.owner_id ?? null;
  const owner =
    ownerId && ownerId === currentUserId
      ? t(($) => $.page.rows.owner_you)
      : (members.find((member) => member.user_id === ownerId)?.name ?? null);
  const subtitle =
    [machine.deviceSummary, owner].filter(Boolean).join(" · ") ||
    (machine.section === "cloud"
      ? t(($) => $.machine.metrics.cloud_worker)
      : t(($) => $.machine.metrics.local_daemon));
  const starting =
    machine.isCurrent && bootstrapping && machine.runtimes.length === 0;
  const online = machine.health === "online";

  return (
    <ListGridRow
      className="h-auto min-h-14 cursor-pointer border-b"
      {...rowLink(href, machine.title)}
    >
      <NameCell
        tile={
          <RowTile
            dot={
              <HealthDot
                health={machine.health}
                className="absolute -bottom-0.5 -right-0.5 ring-2 ring-card"
              />
            }
          >
            <Icon aria-hidden="true" className="size-4 text-muted-foreground" />
          </RowTile>
        }
        title={machine.title}
        href={href}
        badge={
          machine.isCurrent ? (
            <Badge variant="secondary" className="shrink-0">
              {t(($) => $.machine.this_machine)}
            </Badge>
          ) : undefined
        }
        subtitle={subtitle}
      />
      <ListGridCell className={OPTIONAL_CELL}>
        <ProviderIconStack providers={machine.providerNames} />
      </ListGridCell>
      <ListGridCell className={cn(OPTIONAL_CELL, "text-body tabular-nums")}>
        {agentCount}
      </ListGridCell>
      {online ? (
        <ListGridCell className={OPTIONAL_CELL}>
          <WorkloadText
            running={machine.runningCount}
            queued={machine.queuedCount}
          />
        </ListGridCell>
      ) : (
        <EmptyCell />
      )}
      <ListGridCell className="gap-1.5 text-caption">
        {starting ? (
          <>
            <Loader2 aria-hidden="true" className="size-3 animate-spin text-muted-foreground" />
            <span className="truncate text-muted-foreground">
              {t(($) => $.page.bootstrapping.title)}
            </span>
          </>
        ) : (
          <>
            <HealthIcon health={machine.health} />
            <span className={cn("truncate", !online && "text-muted-foreground")}>
              {healthLabel(machine.health)}
              {!online && machine.lastSeenAt
                ? ` · ${timeAgo(machine.lastSeenAt)}`
                : ""}
            </span>
          </>
        )}
      </ListGridCell>
      <RowChevron />
    </ListGridRow>
  );
}

function QoderRow({
  connection,
  agentCount,
  detailHref,
  onConnect,
}: {
  connection: QoderConnection;
  agentCount: number;
  detailHref: string;
  onConnect: () => void;
}) {
  const { t } = useT("runtimes");
  const rowLink = useRowLink();
  const title = t(($) => $.qoder.cloud_title);
  const logo = <ProviderLogo provider="qoder_cloud" className="size-4" />;
  const byQoder = t(($) => $.page.rows.provided_by, { vendor: "Qoder" });

  if (!connection.configured) {
    return (
      <ListGridRow className="h-auto min-h-14 border-b">
        <NameCell
          tile={
            <RowTile
              dot={
                <span className="absolute -bottom-0.5 -right-0.5 size-2 rounded-full bg-warning ring-2 ring-card" />
              }
            >
              {logo}
            </RowTile>
          }
          title={title}
          subtitle={byQoder}
        />
        <EmptyCell />
        <EmptyCell />
        <EmptyCell />
        <ListGridCell>
          <Badge variant="secondary" className="bg-warning/10 text-warning">
            {t(($) => $.page.rows.not_connected)}
          </Badge>
        </ListGridCell>
        <ListGridCell className="justify-end">
          <Button variant="outline" size="sm" onClick={onConnect}>
            {t(($) => $.page.rows.connect)}
          </Button>
        </ListGridCell>
      </ListGridRow>
    );
  }

  const status = !connection.enabled
    ? "stopped"
    : connection.status === "online"
      ? "online"
      : connection.status === "error"
        ? "error"
        : connection.status === "offline"
          ? "offline"
          : "starting";
  const dotTone =
    status === "online"
      ? "bg-success"
      : status === "error"
        ? "bg-destructive"
        : status === "starting"
          ? "bg-warning"
          : "bg-muted-foreground/40";
  const subtitle =
    status === "error" && connection.lastError ? connection.lastError : byQoder;

  return (
    <ListGridRow
      className="h-auto min-h-14 cursor-pointer border-b"
      {...rowLink(detailHref, title)}
    >
      <NameCell
        tile={
          <RowTile
            dot={
              <span
                className={cn(
                  "absolute -bottom-0.5 -right-0.5 size-2 rounded-full ring-2 ring-card",
                  dotTone,
                )}
              />
            }
          >
            {logo}
          </RowTile>
        }
        title={title}
        href={detailHref}
        subtitle={subtitle}
      />
      <EmptyCell />
      <ListGridCell className={cn(OPTIONAL_CELL, "text-body tabular-nums")}>
        {agentCount}
      </ListGridCell>
      {status === "online" ? (
        <ListGridCell className={OPTIONAL_CELL}>
          <WorkloadText
            running={connection.activeRuns}
            queued={connection.queuedRuns}
          />
        </ListGridCell>
      ) : (
        <EmptyCell />
      )}
      <ListGridCell className="gap-1.5 text-caption">
        <span aria-hidden="true" className={cn("size-1.5 rounded-full", dotTone)} />
        <span
          className={cn(
            "truncate",
            status === "error" && "text-destructive",
            status !== "online" && status !== "error" && "text-muted-foreground",
          )}
        >
          {t(($) => $.qoder[status])}
        </span>
      </ListGridCell>
      <RowChevron />
    </ListGridRow>
  );
}

function ComingSoonRow({
  provider,
  name,
  vendor,
}: {
  provider: string;
  name: string;
  vendor: string;
}) {
  const { t } = useT("runtimes");
  return (
    <ListGridRow className="h-auto min-h-14 border-b hover:bg-transparent">
      <NameCell
        tile={
          <RowTile>
            <span className="opacity-60">
              <ProviderLogo provider={provider} className="size-4" />
            </span>
          </RowTile>
        }
        title={name}
        subtitle={t(($) => $.page.rows.provided_by, { vendor })}
        muted
      />
      <EmptyCell />
      <EmptyCell />
      <EmptyCell />
      <ListGridCell>
        <Badge variant="secondary">{t(($) => $.page.groups.cloud.coming_soon)}</Badge>
      </ListGridCell>
      <ListGridCell aria-hidden="true" />
    </ListGridRow>
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

function ProviderIconStack({ providers }: { providers: string[] }) {
  const visible = providers.slice(0, 4);
  const extra = providers.length - visible.length;
  if (visible.length === 0) return null;
  return (
    <span className="flex min-w-0 items-center -space-x-1">
      {visible.map((provider) => (
        <span
          key={provider}
          className="inline-flex h-5 w-5 items-center justify-center rounded-xs bg-background ring-1 ring-border"
        >
          <ProviderLogo provider={provider} className="h-3.5 w-3.5" />
        </span>
      ))}
      {extra > 0 && (
        <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-xs bg-muted px-1 text-micro font-medium text-muted-foreground ring-1 ring-border">
          +{extra}
        </span>
      )}
    </span>
  );
}

function RuntimesPageSkeleton() {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <PageHeader>
        <Skeleton className="h-4 w-24" />
      </PageHeader>
      <div className={cn(PAGE_RAIL, PAGE_GUTTER, "py-6")}>
        <div className="overflow-hidden rounded-lg border">
          {Array.from({ length: 5 }).map((_, index) => (
            <div
              key={index}
              className="flex h-[76px] items-center gap-3 border-b px-4 last:border-b-0"
            >
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
