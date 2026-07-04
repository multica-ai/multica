"use client";

import React, { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { deriveRuntimeHealth, runtimeUsageOptions } from "@multica/core/runtimes";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "@multica/views/navigation";
import { useT } from "@multica/views/i18n";
import { ProviderLogo } from "@multica/views/runtimes/components/provider-logo";
import {
  HealthIcon,
  useHealthLabel,
} from "@multica/views/runtimes/components/shared";
import { splitRuntimeName } from "@multica/views/runtimes/components/runtime-machines";
import { computeCostInWindow, formatLastSeen } from "@multica/views/runtimes/utils";
import { useFlagValue } from "@multica/cerebro-feature-flags";
import { RuntimeAccountCell } from "./runtime-account-cell";
import { runtimeComputerName } from "../runtime-computer-name";
import {
  useRuntimesViewStore,
  type RuntimeColumnKey,
} from "../runtime-view-store";

// FIR-2669: mobile (< @2xl) card layout for the Runtimes list.
//
// The shared ListGrid is two-zone: below @2xl it collapses to a static core
// set (name + health) and column toggles DO NOT apply — so every column the
// user enables in the picker is invisible on a phone. This renders each row as
// a stacked card instead: the runtime name + health on top, then one
// "label: value" line per ENABLED column. The desktop table is unchanged and
// stays authoritative at @2xl and up.
//
// Self-contained on purpose: it reads the same view store + flag the desktop
// list reads, so the parent only needs to hand it the rows. The parent renders
// it only when the columns feature flag is on (flag off = pure upstream).

interface RuntimeMobileRow {
  runtime: AgentRuntime;
  /** Agent ids bound to this runtime — length is the "Agents" count. */
  agentCount: number;
}

// A single "label: value" line inside a card. Value is right-aligned and
// allowed to shrink/truncate so long identities never push the label off-row.
function CardField({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center justify-between gap-3 text-xs">
      <span className="shrink-0 text-muted-foreground">{label}</span>
      <span className="flex min-w-0 items-center justify-end text-right">
        {children}
      </span>
    </div>
  );
}

// Compact 7d cost value — same 14d window + 7d aggregation the desktop CostCell
// uses, without the delta chip (a card row wants one number, not two).
function CostValue({ runtimeId }: { runtimeId: string }) {
  const { data: usage = [] } = useQuery(runtimeUsageOptions(runtimeId, 14));
  const cost7d = useMemo(() => computeCostInWindow(usage, 7), [usage]);
  if (usage.length === 0) {
    return <span className="text-muted-foreground/50">—</span>;
  }
  const fmt = cost7d >= 100 ? `$${cost7d.toFixed(0)}` : `$${cost7d.toFixed(2)}`;
  return <span className="tabular-nums">{fmt}</span>;
}

function CliValue({ runtime }: { runtime: AgentRuntime }) {
  if (runtime.runtime_mode === "cloud") {
    return <span className="text-muted-foreground/50">—</span>;
  }
  const meta = runtime.metadata as Record<string, unknown> | null;
  const version =
    meta && typeof meta.version === "string" ? meta.version : null;
  if (!version) return <span className="text-muted-foreground/50">—</span>;
  return (
    <span className="truncate font-mono text-muted-foreground">{version}</span>
  );
}

function RuntimeMobileCard({
  row,
  now,
  isVisible,
}: {
  row: RuntimeMobileRow;
  now: number;
  isVisible: (key: RuntimeColumnKey) => boolean;
}) {
  const { t } = useT("runtimes");
  const wsPaths = useWorkspacePaths();
  const labelOf = useHealthLabel();

  const runtime = row.runtime;
  const { base } = splitRuntimeName(runtime.name);
  const health = deriveRuntimeHealth(runtime, now);
  const computer = runtimeComputerName(runtime);

  return (
    <AppLink
      href={wsPaths.runtimeDetail(runtime.id)}
      className="flex flex-col gap-2 px-4 py-3 transition-colors hover:bg-muted/40"
    >
      <div className="flex items-center gap-2">
        <ProviderLogo provider={runtime.provider} className="h-5 w-5 shrink-0" />
        <span className="min-w-0 flex-1 truncate text-sm font-medium">
          {base}
        </span>
        <span className="inline-flex shrink-0 items-center gap-1 text-xs text-muted-foreground">
          <HealthIcon health={health} />
          {labelOf(health)}
        </span>
      </div>

      {(health !== "online" && runtime.last_seen_at) && (
        <CardField label={t(($) => $.list.col_health)}>
          <span className="text-muted-foreground">
            {formatLastSeen(runtime.last_seen_at)}
          </span>
        </CardField>
      )}
      {isVisible("machine") && (
        <CardField label={t(($) => $.list.col_machine)}>
          {computer ? (
            <span className="truncate font-mono text-muted-foreground">
              {computer}
            </span>
          ) : (
            <span className="text-muted-foreground/50">—</span>
          )}
        </CardField>
      )}
      {isVisible("agents") && (
        <CardField label={t(($) => $.list.col_agents)}>
          <span className="tabular-nums text-muted-foreground">
            {row.agentCount}
          </span>
        </CardField>
      )}
      {isVisible("cost") && (
        <CardField label={t(($) => $.list.col_cost)}>
          <CostValue runtimeId={runtime.id} />
        </CardField>
      )}
      {isVisible("account") && (
        <CardField label={t(($) => $.list.col_account)}>
          <RuntimeAccountCell runtime={runtime} />
        </CardField>
      )}
      {isVisible("cli") && (
        <CardField label={t(($) => $.list.col_cli)}>
          <CliValue runtime={runtime} />
        </CardField>
      )}
    </AppLink>
  );
}

export function RuntimeMobileList({
  rows,
  now,
  className,
}: {
  rows: RuntimeMobileRow[];
  now: number;
  className?: string;
}) {
  const columnsEnabled = useFlagValue("cerebro_interface_columns");
  const hiddenColumns = useRuntimesViewStore((s) => s.hiddenColumns);
  const isVisible = (key: RuntimeColumnKey) =>
    columnsEnabled && !hiddenColumns.includes(key);

  return (
    <div className={`flex flex-col divide-y ${className ?? ""}`}>
      {rows.map((row) => (
        <RuntimeMobileCard
          key={row.runtime.id}
          row={row}
          now={now}
          isVisible={isVisible}
        />
      ))}
    </div>
  );
}

export type { RuntimeMobileRow };
