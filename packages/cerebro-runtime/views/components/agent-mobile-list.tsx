"use client";

import React from "react";
import {
  useAgentsViewStore,
  type AgentColumnKey,
} from "@multica/core/agents/stores";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "@multica/views/navigation";
import { useT } from "@multica/views/i18n";
import { ActorAvatar } from "@multica/views/common/actor-avatar";
import { availabilityConfig } from "@multica/views/agents/presence";
import type { AgentListRow } from "@multica/views/agents/components/agents-page";
import { useFlagValue } from "@multica/cerebro-feature-flags";
import { RuntimeAccountCell } from "./runtime-account-cell";
import { runtimeComputerName } from "../runtime-computer-name";

// FIR-2669: mobile (< @2xl) card layout for the Agents list.
//
// The shared ListGrid collapses to a static core set below @2xl and column
// toggles don't apply — so every column enabled in the picker is invisible on
// a phone. This renders each agent as a stacked card: avatar + name + status on
// top, then one "label: value" line per ENABLED column. The desktop
// (virtualized) table is untouched and stays authoritative at @2xl and up.
//
// Self-contained: it reads the agents view store + flag itself, mirroring the
// desktop `isColVisible`, so the parent only hands it the rows.

// A single "label: value" line inside a card.
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

function AgentMobileCard({
  row,
  isColVisible,
}: {
  row: AgentListRow;
  isColVisible: (key: AgentColumnKey) => boolean;
}) {
  const { t } = useT("agents");
  const paths = useWorkspacePaths();
  const { agent, presence, owner } = row;
  const isArchived = !!agent.archived_at;

  const computer = row.runtime ? runtimeComputerName(row.runtime) : null;
  const days = row.lastActiveDays;
  const lastActive =
    days === null
      ? isArchived
        ? "—"
        : t(($) => $.last_active.none)
      : days === 0
        ? t(($) => $.last_active.today)
        : t(($) => $.last_active.days_ago, { count: days });

  const statusVisual = presence
    ? availabilityConfig[presence.availability]
    : null;

  return (
    <AppLink
      href={paths.agentDetail(agent.id)}
      className="flex flex-col gap-2 px-4 py-3 transition-colors hover:bg-muted/40"
    >
      <div className="flex items-center gap-3">
        <ActorAvatar
          actorType="agent"
          actorId={agent.id}
          size={32}
          className={`shrink-0 rounded-md ${isArchived ? "opacity-50 grayscale" : ""}`}
        />
        <span className="min-w-0 flex-1 truncate text-sm font-medium">
          {agent.name}
        </span>
        {isColVisible("status") &&
          (isArchived ? (
            <span className="shrink-0 text-xs text-muted-foreground/60">
              {t(($) => $.row.archived)}
            </span>
          ) : statusVisual ? (
            <span
              className={`inline-flex shrink-0 items-center gap-1 text-xs ${statusVisual.textClass}`}
            >
              <span
                className={`size-1.5 rounded-full ${statusVisual.dotClass}`}
              />
              {t(($) => $.availability[presence!.availability])}
            </span>
          ) : null)}
      </div>

      {isColVisible("owner") && (
        <CardField label={t(($) => $.columns.owner)}>
          <span className="truncate text-muted-foreground">
            {owner?.name ?? (agent.owner_id ? agent.owner_id.slice(0, 8) : "—")}
          </span>
        </CardField>
      )}
      {isColVisible("runtime") && (
        <CardField label={t(($) => $.columns.runtime)}>
          {computer ? (
            <span className="truncate font-mono text-muted-foreground">
              {computer}
            </span>
          ) : (
            <span className="text-muted-foreground/40">—</span>
          )}
        </CardField>
      )}
      {isColVisible("lastActive") && (
        <CardField label={t(($) => $.columns.last_active)}>
          <span className="tabular-nums text-muted-foreground">
            {lastActive}
          </span>
        </CardField>
      )}
      {isColVisible("runs") && (
        <CardField label={t(($) => $.columns.runs)}>
          <span className="tabular-nums text-muted-foreground">
            {row.runCount.toLocaleString()}
          </span>
        </CardField>
      )}
      {isColVisible("model") && (
        <CardField label={t(($) => $.columns.model)}>
          <span className="truncate text-muted-foreground">
            {agent.model || "—"}
          </span>
        </CardField>
      )}
      {isColVisible("account") && (
        <CardField label={t(($) => $.columns.account)}>
          {row.runtime ? (
            <RuntimeAccountCell runtime={row.runtime} />
          ) : (
            <span className="text-muted-foreground/40">—</span>
          )}
        </CardField>
      )}
      {isColVisible("thinking") && (
        <CardField label={t(($) => $.columns.thinking)}>
          <span className="truncate text-muted-foreground">
            {agent.thinking_level || "—"}
          </span>
        </CardField>
      )}
      {isColVisible("created") && (
        <CardField label={t(($) => $.columns.created)}>
          <span className="tabular-nums text-muted-foreground">
            {new Date(agent.created_at).toLocaleDateString()}
          </span>
        </CardField>
      )}
    </AppLink>
  );
}

export function AgentMobileList({
  rows,
  className,
}: {
  rows: AgentListRow[];
  className?: string;
}) {
  const extrasEnabled = useFlagValue("cerebro_interface_columns");
  const hiddenColumns = useAgentsViewStore((s) => s.hiddenColumns);
  // Mirrors the desktop isColVisible: account/thinking are flag-gated, the rest
  // are visible unless the user hid them.
  const isColVisible = (key: AgentColumnKey) =>
    key === "account" || key === "thinking"
      ? extrasEnabled && !hiddenColumns.includes(key)
      : !hiddenColumns.includes(key);

  return (
    <div className={`flex flex-col divide-y ${className ?? ""}`}>
      {rows.map((row) => (
        <AgentMobileCard
          key={row.agent.id}
          row={row}
          isColVisible={isColVisible}
        />
      ))}
    </div>
  );
}
