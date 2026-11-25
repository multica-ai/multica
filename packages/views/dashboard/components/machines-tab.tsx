"use client";

import { useId, useMemo, useState, type CSSProperties } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronDown, ChevronRight, EyeOff, Trash2 } from "@multica/ui/icons";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";
import { useAuthStore } from "@multica/core/auth";
import { runtimeListOptions } from "@multica/core/runtimes/queries";
import { memberListOptions } from "@multica/core/workspace/queries";
import {
  dashboardUsageByRuntimeOptions,
  type DashboardHistoryLimitParam,
} from "@multica/core/dashboard";
import { formatTokens } from "../../runtimes/utils";
import { runtimeRowLabel } from "../../runtimes/components/runtime-machines";
import { machineIcon } from "../../runtimes/components/shared";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import { useT } from "../../i18n";
import {
  aggregateMachineUsage,
  type MachineRuntimeUsageRow,
  type MachineUsageRow,
} from "../machine-usage";

const MACHINE_GRID_STYLE = {
  minWidth: "fit-content",
  gridTemplateColumns: "minmax(12rem, 1.8fr) minmax(6rem, 1fr) 5rem 5rem 7rem",
} satisfies CSSProperties;

const GRID = "grid items-center gap-3";

// Qoder registers a runtime like a machine, but the Runtimes page lists it as
// one cloud service rather than as a machine; name it the same way here.
const QODER_CLOUD_PROVIDER = "qoder_cloud";

/**
 * The dashboard's "By machine" tab: usage split by the machine each run
 * executed on, grouped and named as on the Runtimes page. The server decides
 * what may be named (runtimes whose usage the viewer may not read arrive as
 * one folded bucket); nothing here is a permission check. It reads the same
 * range, project and squad filters as the other tabs and is only fetched once
 * the tab is open.
 */
export function MachinesTab({
  wsId,
  days,
  projectId,
  viewTZ,
  historyLimit,
  squadId,
}: {
  wsId: string;
  days: number;
  projectId: string | null;
  viewTZ: string;
  historyLimit: DashboardHistoryLimitParam;
  squadId: string | null;
}) {
  const { t } = useT("usage");
  const { t: rt } = useT("runtimes");
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);
  const usageQuery = useQuery(
    dashboardUsageByRuntimeOptions(wsId, days, projectId, viewTZ, historyLimit, squadId),
  );
  const runtimesQuery = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const [expanded, setExpanded] = useState<string | null>(null);

  const rows = useMemo(
    () =>
      usageQuery.data && runtimesQuery.data
        ? aggregateMachineUsage(usageQuery.data, runtimesQuery.data)
        : [],
    [usageQuery.data, runtimesQuery.data],
  );
  const maxTokens = useMemo(() => rows.reduce((m, r) => Math.max(m, r.tokens), 0), [rows]);
  const machineCount = rows.filter((r) => r.kind === "machine").length;
  const memberNames = useMemo(
    () => new Map(members.map((m) => [m.user_id, m.name])),
    [members],
  );

  if (usageQuery.isPending || runtimesQuery.isPending) {
    return (
      <div className="space-y-3">
        <Skeleton className="h-10 rounded-lg" />
        <Skeleton className="h-48 rounded-lg" />
      </div>
    );
  }
  // Without the runtime list every machine would read as deleted, so a failed
  // read of either side is an error rather than a guess.
  if (usageQuery.isError || runtimesQuery.isError) {
    return (
      <p role="alert" className="rounded-lg border bg-card px-4 py-8 text-center text-caption text-muted-foreground">
        {t(($) => $.machines.load_error)}
      </p>
    );
  }

  const describeMachine = (row: Extract<MachineUsageRow, { kind: "machine" }>) => {
    // Same title and "device · owner" line as the Runtimes page.
    const { machine } = row;
    const qoderOnly =
      machine.runtimes.length > 0 &&
      machine.runtimes.every((r) => r.provider === QODER_CLOUD_PROVIDER);
    const title = qoderOnly ? rt(($) => $.qoder.cloud_title) : machine.title;
    const ownerId = machine.runtimes[0]?.owner_id ?? null;
    const owner =
      ownerId && ownerId === currentUserId
        ? rt(($) => $.page.rows.owner_you)
        : ownerId
          ? (memberNames.get(ownerId) ?? null)
          : null;
    const description = [machine.deviceSummary, owner].filter(Boolean).join(" · ") || null;
    return { title, description };
  };

  return (
    <div className="rounded-lg border bg-card">
      <div className="flex flex-wrap items-start justify-between gap-3 border-b px-4 pt-4 pb-3">
        <div className="min-w-0 space-y-1">
          <h4 className="text-body font-semibold">{t(($) => $.machines.title)}</h4>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.machines.description)}
          </p>
        </div>
        <span className="shrink-0 text-caption text-muted-foreground">
          {t(($) => $.machines.caption, { count: machineCount })}
        </span>
      </div>
      {rows.length === 0 ? (
        <p className="px-4 py-8 text-center text-caption text-muted-foreground">
          {t(($) => $.machines.no_data)}
        </p>
      ) : (
        <div
          role="region"
          aria-label={t(($) => $.machines.title)}
          tabIndex={0}
          className="overflow-x-auto overscroll-x-contain"
        >
          <div
            className={`${GRID} border-b px-4 py-2 text-caption font-medium text-muted-foreground`}
            style={MACHINE_GRID_STYLE}
          >
            <span>{t(($) => $.machines.header_machine)}</span>
            <span />
            <span className="text-right">{t(($) => $.machines.header_tokens)}</span>
            <span className="text-right">{t(($) => $.machines.header_cost)}</span>
            <span className="text-right">{t(($) => $.machines.header_tasks)}</span>
          </div>
          <ul aria-label={t(($) => $.machines.title)} className="divide-y">
            {rows.map((row) =>
              row.kind === "machine" ? (
                <MachineRow
                  key={row.id}
                  row={row}
                  {...describeMachine(row)}
                  maxTokens={maxTokens}
                  expanded={expanded === row.id}
                  onToggle={() => setExpanded((cur) => (cur === row.id ? null : row.id))}
                />
              ) : (
                <BucketRow key={row.id} row={row} maxTokens={maxTokens} />
              ),
            )}
          </ul>
        </div>
      )}
    </div>
  );
}

function UsageCells({ row, maxTokens }: { row: MachineUsageRow; maxTokens: number }) {
  const pct = maxTokens > 0 ? Math.min(100, (row.tokens / maxTokens) * 100) : 0;
  return (
    <>
      <span aria-hidden="true" className="relative h-2 overflow-hidden rounded-full bg-muted">
        <span
          className="block h-full rounded-full bg-chart-1 transition-[width] duration-300 ease-out"
          style={{ width: `${pct}%` }}
        />
      </span>
      <span className="text-right text-caption font-medium tabular-nums text-foreground">
        {formatTokens(row.tokens)}
      </span>
      <span className="text-right text-caption tabular-nums text-muted-foreground">
        ${row.cost.toFixed(2)}
      </span>
      <span className="text-right text-caption tabular-nums text-muted-foreground">
        {row.taskCount}
      </span>
    </>
  );
}

function MachineRow({
  row,
  title,
  description,
  maxTokens,
  expanded,
  onToggle,
}: {
  row: Extract<MachineUsageRow, { kind: "machine" }>;
  title: string;
  description: string | null;
  maxTokens: number;
  expanded: boolean;
  onToggle: () => void;
}) {
  const { t } = useT("usage");
  const panelId = useId();
  const Chevron = expanded ? ChevronDown : ChevronRight;
  const Icon = machineIcon(row.machine);

  return (
    <li>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={expanded}
        aria-controls={expanded ? panelId : undefined}
        aria-label={
          expanded
            ? t(($) => $.machines.hide_runtimes, { name: title })
            : t(($) => $.machines.show_runtimes, { name: title })
        }
        className={`${GRID} w-full px-4 py-2 text-left hover:bg-accent/50 focus-visible:bg-accent/50 focus-visible:outline-none`}
        style={MACHINE_GRID_STYLE}
      >
        <span className="flex min-w-0 items-center gap-2">
          <Chevron aria-hidden="true" className="size-3.5 shrink-0 text-muted-foreground" />
          <Icon aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
          <span className="min-w-0">
            <TruncatedTooltip content={title}>
              <span className="block min-w-0 truncate text-body font-medium">{title}</span>
            </TruncatedTooltip>
            {description ? (
              <span className="block min-w-0 truncate text-caption text-muted-foreground">
                {description}
              </span>
            ) : null}
          </span>
        </span>
        <UsageCells row={row} maxTokens={maxTokens} />
      </button>
      {expanded ? (
        <div id={panelId} className="border-t bg-muted/30 px-4 py-2">
          <ul aria-label={t(($) => $.machines.runtimes_title, { name: title })} className="divide-y">
            {row.runtimes.map((runtimeRow) => (
              <RuntimeRow
                key={runtimeRow.runtime.id}
                row={runtimeRow}
                machineTitle={row.machine.title}
              />
            ))}
          </ul>
        </div>
      ) : null}
    </li>
  );
}

function RuntimeRow({ row, machineTitle }: { row: MachineRuntimeUsageRow; machineTitle: string }) {
  const label = runtimeRowLabel(row.runtime, machineTitle);
  return (
    <li className={`${GRID} py-1.5 pl-6`} style={MACHINE_GRID_STYLE}>
      <span className="flex min-w-0 items-center gap-2">
        <ProviderLogo provider={row.runtime.provider} className="size-3.5 shrink-0" />
        <TruncatedTooltip content={label}>
          <span className="block min-w-0 truncate text-caption">{label}</span>
        </TruncatedTooltip>
      </span>
      <span />
      <span className="text-right text-caption tabular-nums">{formatTokens(row.tokens)}</span>
      <span className="text-right text-caption tabular-nums text-muted-foreground">
        ${row.cost.toFixed(2)}
      </span>
      <span className="text-right text-caption tabular-nums text-muted-foreground">
        {row.taskCount}
      </span>
    </li>
  );
}

function BucketRow({ row, maxTokens }: { row: MachineUsageRow; maxTokens: number }) {
  const { t } = useT("usage");
  const restricted = row.kind === "restricted";
  const Icon = restricted ? EyeOff : Trash2;
  const label = restricted ? t(($) => $.machines.other_machines) : t(($) => $.machines.deleted_machines);
  const hint = restricted ? t(($) => $.machines.other_machines_hint) : null;
  return (
    <li className={`${GRID} px-4 py-2`} style={MACHINE_GRID_STYLE}>
      <span className="flex min-w-0 items-center gap-2">
        {/* Aligns with the chevron of the expandable rows. */}
        <span aria-hidden="true" className="size-3.5 shrink-0" />
        <Icon aria-hidden="true" className="size-4 shrink-0 text-muted-foreground" />
        <span className="min-w-0">
          <span className="block min-w-0 truncate text-body font-medium italic text-muted-foreground">
            {label}
          </span>
          {hint ? (
            <span className="block min-w-0 truncate text-caption text-muted-foreground">{hint}</span>
          ) : null}
        </span>
      </span>
      <UsageCells row={row} maxTokens={maxTokens} />
    </li>
  );
}
