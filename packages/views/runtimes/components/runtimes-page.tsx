"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import {
  Cloud,
  Monitor,
  Plus,
  Search,
  Server,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { runtimeListOptions, runtimeKeys } from "@multica/core/runtimes/queries";
import { useUpdatableRuntimeIds } from "@multica/core/runtimes/hooks";
import { useWSEvent } from "@multica/core/realtime";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import {
  ResizableHandle,
  ResizablePanel,
  ResizablePanelGroup,
} from "@multica/ui/components/ui/resizable";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useIsMobile } from "@multica/ui/hooks/use-mobile";
import { cn } from "@multica/ui/lib/utils";
import { PageHeader } from "../../layout/page-header";
import { ConnectRemoteDialog } from "./connect-remote-dialog";
import { ProviderLogo } from "./provider-logo";
import { RuntimeList, buildWorkloadIndex } from "./runtime-list";
import {
  buildRuntimeMachines,
  filterRuntimeMachines,
  runtimeMachineCounts,
  type RuntimeMachine,
  type RuntimeMachineFilter,
} from "./runtime-machines";
import { HealthDot, HealthIcon, useHealthLabel } from "./shared";
import { useT } from "../../i18n";

const MACHINE_FILTERS: RuntimeMachineFilter[] = ["all", "online", "issues"];

interface RuntimesPageProps {
  /** Desktop-only daemon id used to mark the row for this Mac. */
  localDaemonId?: string | null;
  /** Desktop-only friendly device name for the local daemon. */
  localMachineName?: string | null;
  /** Desktop-only controls shown when the local machine is selected. */
  localMachineActions?: React.ReactNode;
  /**
   * Desktop-only signal: the bundled daemon is still booting / hasn't
   * registered with the server yet. Forwarded so the empty state can show
   * a "starting" indicator instead of the static "register a runtime" hint
   * during the boot window. Web omits this.
   */
  bootstrapping?: boolean;
}

// Re-render every 30s so derived health (recently_lost → offline transitions)
// catches up even when no underlying query data has changed.
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
  bootstrapping,
}: RuntimesPageProps = {}) {
  const isLoading = useAuthStore((s) => s.isLoading);
  const wsId = useWorkspaceId();
  const qc = useQueryClient();
  const [machineFilter, setMachineFilter] =
    useState<RuntimeMachineFilter>("all");
  const [machineSearch, setMachineSearch] = useState("");
  const [selectedMachineId, setSelectedMachineId] = useState<string | null>(
    null,
  );
  const [showConnectDialog, setShowConnectDialog] = useState(false);
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_runtimes_layout",
  });
  const isMobile = useIsMobile();

  const { data: runtimes = [], isLoading: fetching } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  const handleDaemonEvent = useCallback(() => {
    qc.invalidateQueries({ queryKey: runtimeKeys.all(wsId) });
  }, [qc, wsId]);
  useWSEvent("daemon:register", handleDaemonEvent);

  const updatableIds = useUpdatableRuntimeIds(wsId);
  const now = useNowTick();

  const workloadIndex = useMemo(
    () => buildWorkloadIndex(agents, snapshot),
    [agents, snapshot],
  );

  const machines = useMemo(
    () =>
      buildRuntimeMachines(runtimes, {
        now,
        localDaemonId,
        localMachineName,
        workloadByRuntimeId: workloadIndex,
      }),
    [runtimes, now, localDaemonId, localMachineName, workloadIndex],
  );

  const machineCounts = useMemo(() => runtimeMachineCounts(machines), [machines]);

  const filteredMachines = useMemo(
    () => filterRuntimeMachines(machines, machineSearch, machineFilter),
    [machines, machineSearch, machineFilter],
  );

  useEffect(() => {
    if (filteredMachines.length === 0) {
      if (selectedMachineId !== null) setSelectedMachineId(null);
      return;
    }
    if (!selectedMachineId || !filteredMachines.some((m) => m.id === selectedMachineId)) {
      setSelectedMachineId(filteredMachines[0]?.id ?? null);
    }
  }, [filteredMachines, selectedMachineId]);

  const selectedMachine =
    machines.find((machine) => machine.id === selectedMachineId) ??
    filteredMachines[0] ??
    null;

  if (isLoading || fetching) return <RuntimesPageSkeleton />;

  const totalCount = runtimes.length;
  const showEmpty = totalCount === 0 && !bootstrapping;

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeaderBar
        totalCount={totalCount}
        onConnectRemote={() => setShowConnectDialog(true)}
      />

      {showEmpty ? (
        <div className="flex flex-1 items-center justify-center p-6">
          <EmptyState onConnectRemote={() => setShowConnectDialog(true)} />
        </div>
      ) : isMobile ? (
        <div className="flex min-h-0 flex-1 flex-col border-t bg-background">
          <MachineSidebar
            machines={filteredMachines}
            totalMachines={machines.length}
            counts={machineCounts}
            selectedMachineId={selectedMachine?.id ?? null}
            search={machineSearch}
            setSearch={setMachineSearch}
            filter={machineFilter}
            setFilter={setMachineFilter}
            onSelect={setSelectedMachineId}
          />
          <MachineDetail
            machine={selectedMachine}
            updatableIds={updatableIds}
            now={now}
            bootstrapping={bootstrapping}
            actions={
              selectedMachine?.isCurrent ? localMachineActions : undefined
            }
          />
        </div>
      ) : (
        <div className="min-h-0 flex-1 border-t bg-background">
          <ResizablePanelGroup
            orientation="horizontal"
            className="min-h-0 flex-1"
            defaultLayout={defaultLayout}
            onLayoutChanged={onLayoutChanged}
          >
            <ResizablePanel
              id="machines"
              defaultSize={300}
              minSize={240}
              maxSize={420}
              groupResizeBehavior="preserve-pixel-size"
            >
              <MachineSidebar
                machines={filteredMachines}
                totalMachines={machines.length}
                counts={machineCounts}
                selectedMachineId={selectedMachine?.id ?? null}
                search={machineSearch}
                setSearch={setMachineSearch}
                filter={machineFilter}
                setFilter={setMachineFilter}
                onSelect={setSelectedMachineId}
                className="h-full border-b-0 border-r"
              />
            </ResizablePanel>
            <ResizableHandle />
            <ResizablePanel id="detail" minSize="45%">
              <MachineDetail
                machine={selectedMachine}
                updatableIds={updatableIds}
                now={now}
                bootstrapping={bootstrapping}
                actions={
                  selectedMachine?.isCurrent ? localMachineActions : undefined
                }
              />
            </ResizablePanel>
          </ResizablePanelGroup>
        </div>
      )}

      {showConnectDialog && (
        <ConnectRemoteDialog onClose={() => setShowConnectDialog(false)} />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Header bar — minimal: only icon + title + count, matching Skills.
// Page-level actions (Search, scope, filter) live in the card below.
// ---------------------------------------------------------------------------

function PageHeaderBar({
  totalCount,
  onConnectRemote,
}: {
  totalCount: number;
  onConnectRemote: () => void;
}) {
  const { t } = useT("runtimes");
  return (
    <PageHeader className="justify-between px-5">
      <div className="flex items-center gap-2">
        <Server className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
        {totalCount > 0 && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
            {totalCount}
          </span>
        )}
        <p className="ml-2 hidden text-xs text-muted-foreground md:block">
          {t(($) => $.page.tagline)}{" "}
          <a
            href="https://multica.ai/docs/daemon-runtimes"
            target="_blank"
            rel="noopener noreferrer"
            className="underline decoration-muted-foreground/30 underline-offset-4 transition-colors hover:text-foreground"
          >
            {t(($) => $.page.learn_more)}
          </a>
        </p>
      </div>
      <Button type="button" size="sm" onClick={onConnectRemote}>
        <Plus className="h-3 w-3" />
        {t(($) => $.page.connect_remote)}
      </Button>
    </PageHeader>
  );
}

function MachineSidebar({
  machines,
  totalMachines,
  counts,
  selectedMachineId,
  search,
  setSearch,
  filter,
  setFilter,
  onSelect,
  className,
}: {
  machines: RuntimeMachine[];
  totalMachines: number;
  counts: { all: number; online: number; issues: number };
  selectedMachineId: string | null;
  search: string;
  setSearch: (value: string) => void;
  filter: RuntimeMachineFilter;
  setFilter: (value: RuntimeMachineFilter) => void;
  onSelect: (id: string) => void;
  className?: string;
}) {
  const { t } = useT("runtimes");
  const sections = [
    {
      key: "local" as const,
      label: t(($) => $.machine.section_local),
      machines: machines.filter((machine) => machine.section === "local"),
    },
    {
      key: "remote" as const,
      label: t(($) => $.machine.section_remote),
      machines: machines.filter((machine) => machine.section === "remote"),
    },
    {
      key: "cloud" as const,
      label: t(($) => $.machine.section_cloud),
      machines: machines.filter((machine) => machine.section === "cloud"),
    },
  ].filter((section) => section.machines.length > 0);

  return (
    <aside
      className={cn(
        "flex min-h-0 shrink-0 flex-col border-b bg-muted/20",
        className,
      )}
    >
      <div className="shrink-0 border-b bg-background p-3">
        <div className="relative">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder={t(($) => $.machine.search_placeholder)}
            className="h-9 pl-8 text-sm"
          />
        </div>
        <div className="mt-2 flex items-center gap-1.5 overflow-x-auto">
          {MACHINE_FILTERS.map((key) => (
            <MachineFilterChip
              key={key}
              active={filter === key}
              onClick={() => setFilter(key)}
              label={t(($) => $.machine.filters[key])}
              count={counts[key]}
              tone={key}
            />
          ))}
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-auto py-2">
        {sections.length > 0 ? (
          sections.map((section) => (
            <div key={section.key} className="mb-3 last:mb-0">
              <div className="mb-1 flex items-center gap-2 px-4 text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
                <span>{section.label}</span>
                <span className="h-px flex-1 bg-border" />
              </div>
              <div>
                {section.machines.map((machine) => (
                  <MachineRow
                    key={machine.id}
                    machine={machine}
                    active={machine.id === selectedMachineId}
                    onClick={() => onSelect(machine.id)}
                  />
                ))}
              </div>
            </div>
          ))
        ) : (
          <div className="flex h-full flex-col items-center justify-center px-6 text-center">
            <Search className="h-8 w-8 text-muted-foreground/40" />
            <p className="mt-3 text-sm font-medium">
              {t(($) => $.machine.no_matches_title)}
            </p>
            <p className="mt-1 text-xs text-muted-foreground">
              {totalMachines > 0
                ? t(($) => $.machine.no_matches_hint)
                : t(($) => $.page.bootstrapping.hint)}
            </p>
          </div>
        )}
      </div>
    </aside>
  );
}

function MachineFilterChip({
  active,
  onClick,
  label,
  count,
  tone,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  count: number;
  tone: RuntimeMachineFilter;
}) {
  const dotClass =
    tone === "online"
      ? "bg-success"
      : tone === "issues"
        ? "bg-warning"
        : "bg-muted-foreground/40";
  return (
    <Button
      variant="outline"
      size="sm"
      onClick={onClick}
      className={cn(
        "h-7 gap-1.5 px-2 text-xs",
        active
          ? "bg-accent text-accent-foreground hover:bg-accent/80"
          : "bg-background text-muted-foreground",
      )}
    >
      {tone !== "all" && <span className={cn("h-1.5 w-1.5 rounded-full", dotClass)} />}
      <span>{label}</span>
      <span className="font-mono tabular-nums text-muted-foreground/70">
        {count}
      </span>
    </Button>
  );
}

function MachineRow({
  machine,
  active,
  onClick,
}: {
  machine: RuntimeMachine;
  active: boolean;
  onClick: () => void;
}) {
  const { t } = useT("runtimes");
  const Icon = machine.section === "cloud" ? Cloud : Monitor;
  const busyCount = machine.runningCount + machine.queuedCount;
  const runtimeCount = t(($) => $.machine.runtime_count, {
    count: machine.runtimes.length,
  });
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "group flex w-full min-w-0 items-start gap-3 px-4 py-2.5 text-left transition-colors",
        active ? "bg-accent" : "hover:bg-accent/50",
      )}
    >
      <span className="relative mt-0.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-md border bg-background">
        <Icon className="h-3.5 w-3.5 text-muted-foreground" />
        <HealthDot
          health={machine.health}
          className="absolute -bottom-0.5 -right-0.5 ring-2 ring-background"
        />
      </span>
      <span className="min-w-0 flex-1">
        <span className="flex min-w-0 items-center gap-1.5">
          <span className="truncate text-sm font-medium">{machine.title}</span>
          {machine.isCurrent && (
            <span className="shrink-0 rounded bg-foreground px-1.5 py-0.5 text-[10px] font-medium text-background">
              {t(($) => $.machine.this_machine)}
            </span>
          )}
        </span>
        {machine.subtitle && (
          <span className="mt-0.5 block truncate font-mono text-xs text-muted-foreground">
            {machine.subtitle}
          </span>
        )}
        <span className="mt-2 flex min-w-0 items-center gap-1.5">
          <ProviderIconStack providers={machine.providerNames} />
          {busyCount > 0 ? (
            <span className="ml-auto shrink-0 text-xs font-medium text-primary">
              {t(($) => $.machine.busy_count, { count: busyCount })}
            </span>
          ) : (
            <span className="ml-auto shrink-0 text-xs text-muted-foreground">
              {runtimeCount}
            </span>
          )}
        </span>
      </span>
    </button>
  );
}

function ProviderIconStack({ providers }: { providers: string[] }) {
  const visible = providers.slice(0, 4);
  const extra = providers.length - visible.length;
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

function MachineDetail({
  machine,
  updatableIds,
  now,
  bootstrapping,
  actions,
}: {
  machine: RuntimeMachine | null;
  updatableIds: Set<string>;
  now: number;
  bootstrapping?: boolean;
  actions?: React.ReactNode;
}) {
  const { t } = useT("runtimes");
  const healthLabel = useHealthLabel();

  if (!machine) {
    return (
      <main className="flex min-h-0 flex-1 flex-col items-center justify-center px-6 text-center">
        {bootstrapping ? (
          <>
            <Server className="h-8 w-8 animate-pulse text-muted-foreground/40" />
            <p className="mt-3 text-sm text-muted-foreground">
              {t(($) => $.page.bootstrapping.title)}
            </p>
            <p className="mt-1 max-w-xs text-xs text-muted-foreground/70">
              {t(($) => $.page.bootstrapping.hint)}
            </p>
          </>
        ) : (
          <>
            <Monitor className="h-8 w-8 text-muted-foreground/40" />
            <p className="mt-3 text-sm text-muted-foreground">
              {t(($) => $.machine.select_machine)}
            </p>
          </>
        )}
      </main>
    );
  }

  const onlineRuntimeCount = machine.onlineCount;
  const issueCount = machine.issueCount;
  const workloadLabel =
    machine.runningCount > 0 || machine.queuedCount > 0
      ? t(($) => $.machine.metrics.workload_hint, {
          running: machine.runningCount,
          queued: machine.queuedCount,
        })
      : t(($) => $.machine.metrics.workload_idle);
  const runtimeTotal = machine.runtimes.length;
  const metaItems = [machine.subtitle].filter(Boolean);

  return (
    <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
      <div className="shrink-0 border-b bg-background px-5 py-5">
        <div className="flex min-w-0 flex-col gap-4 lg:flex-row lg:items-start lg:justify-between">
          <div className="min-w-0">
            <div className="flex min-w-0 flex-wrap items-center gap-2">
              <h2 className="truncate text-xl font-semibold tracking-tight">
                {machine.title}
              </h2>
              <span className="inline-flex items-center gap-1 rounded-md border bg-background px-2 py-1 text-xs text-muted-foreground">
                <HealthIcon health={machine.health} />
                {healthLabel(machine.health)}
              </span>
              {machine.isCurrent && (
                <span className="rounded-md bg-foreground px-2 py-1 text-xs font-medium text-background">
                  {t(($) => $.machine.local_badge)}
                </span>
              )}
              <span className="rounded-md border bg-muted px-2 py-1 text-xs text-muted-foreground">
                {machine.section === "cloud"
                  ? t(($) => $.machine.section_cloud)
                  : machine.section === "local"
                    ? t(($) => $.machine.section_local)
                    : t(($) => $.machine.section_remote)}
              </span>
            </div>
            {metaItems.length > 0 && (
              <p className="mt-2 max-w-4xl truncate text-xs text-muted-foreground">
                {metaItems.join(" · ")}
              </p>
            )}
          </div>
          {actions && <div className="shrink-0">{actions}</div>}
        </div>

        <div className="mt-5 grid overflow-hidden rounded-lg border bg-muted/20 sm:grid-cols-2 lg:grid-cols-4">
          <MachineMetric
            label={t(($) => $.machine.metrics.runtimes)}
            value={String(runtimeTotal)}
            hint={t(($) => $.machine.metrics.runtimes_hint, {
              count: onlineRuntimeCount,
            })}
          />
          <MachineMetric
            label={t(($) => $.machine.metrics.health)}
            value={healthLabel(machine.health)}
            hint={
              issueCount > 0
                ? t(($) => $.machine.metrics.health_issues, { count: issueCount })
                : t(($) => $.machine.metrics.health_clear)
            }
          />
          <MachineMetric
            label={t(($) => $.machine.metrics.workload)}
            value={
              machine.runningCount > 0 || machine.queuedCount > 0
                ? String(machine.runningCount + machine.queuedCount)
                : t(($) => $.machine.metrics.workload_value_idle)
            }
            hint={workloadLabel}
          />
          <MachineMetric
            label={t(($) => $.machine.metrics.cli)}
            value={machine.cliVersion ?? "—"}
            hint={
              machine.mode === "cloud"
                ? t(($) => $.machine.metrics.cloud_worker)
                : t(($) => $.machine.metrics.local_daemon)
            }
            mono={!!machine.cliVersion}
          />
        </div>
      </div>

      <RuntimeList
        runtimes={machine.runtimes}
        updatableIds={updatableIds}
        now={now}
      />
    </main>
  );
}

function MachineMetric({
  label,
  value,
  hint,
  mono,
}: {
  label: string;
  value: string;
  hint: string;
  mono?: boolean;
}) {
  return (
    <div className="min-w-0 border-b px-4 py-3 last:border-b-0 sm:odd:border-r lg:border-b-0 lg:border-r lg:last:border-r-0">
      <div className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </div>
      <div
        className={cn(
          "mt-1 truncate text-base font-semibold tabular-nums",
          mono && "font-mono text-sm",
        )}
      >
        {value}
      </div>
      <div className="mt-1 truncate text-xs text-muted-foreground">{hint}</div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Empty state — shown when zero runtimes have ever registered in this
// workspace.
// ---------------------------------------------------------------------------

function EmptyState({ onConnectRemote }: { onConnectRemote: () => void }) {
  const { t } = useT("runtimes");
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <Server className="h-6 w-6 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-base font-semibold">{t(($) => $.page.empty.title)}</h2>
      <p className="mt-1 max-w-md text-sm text-muted-foreground">
        {t(($) => $.page.empty.hint)}
      </p>
      <Button
        type="button"
        size="sm"
        onClick={onConnectRemote}
        className="mt-5"
      >
        <Plus className="h-3 w-3" />
        {t(($) => $.page.connect_remote)}
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Loading skeleton — laid out like the split runtime page so the layout
// does not jump on first paint.
// ---------------------------------------------------------------------------

function RuntimesPageSkeleton() {
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeader className="justify-between px-5">
        <Skeleton className="h-4 w-24" />
      </PageHeader>
      <div className="flex min-h-0 flex-1 border-t">
        <div className="hidden w-[300px] shrink-0 border-r p-3 md:block">
          <Skeleton className="h-9 w-full rounded-md" />
          <div className="mt-3 flex gap-2">
            <Skeleton className="h-7 w-16 rounded-md" />
            <Skeleton className="h-7 w-20 rounded-md" />
            <Skeleton className="h-7 w-20 rounded-md" />
          </div>
          <div className="mt-5 space-y-2">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-20 w-full rounded-lg" />
            ))}
          </div>
        </div>
        <div className="flex min-w-0 flex-1 flex-col">
          <div className="border-b p-5">
            <Skeleton className="h-6 w-64 rounded-md" />
            <Skeleton className="mt-3 h-4 w-full max-w-2xl rounded-md" />
            <div className="mt-5 grid gap-px overflow-hidden rounded-lg border sm:grid-cols-2 lg:grid-cols-4">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-24 rounded-none" />
              ))}
            </div>
          </div>
          <div className="h-12 border-b px-4 py-2">
            <Skeleton className="h-8 w-40 rounded-full" />
          </div>
          <div className="space-y-2 p-4">
            {Array.from({ length: 6 }).map((_, i) => (
              <Skeleton key={i} className="h-14 w-full rounded-md" />
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

export default RuntimesPage;
export type { RuntimesPageProps };
