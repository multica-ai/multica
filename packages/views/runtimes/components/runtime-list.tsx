"use client";

// CEREBRO-PATCH(runtime-list-cerebro): upstream restructured this file from a
// custom filter-bar list into a DataTable wrapper (2026-W19 sync). Cerebro's
// previous additions (AddRuntimeDialog, owner filter, scope toggle, custom
// list rows) are absorbed by upstream's runtimes-page filter chips and the
// new runtime-columns module, so no per-line patches are needed here. The
// cerebro AddRuntimeDialog remains exported from @multica/cerebro-runtime/views
// and can be re-wired in the future if cerebro adds its own runtime sources.

import { useMemo, useState } from "react";
import { Globe, MoreHorizontal, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type {
  Agent,
  AgentRuntime,
  AgentTask,
  MemberWithUser,
} from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import {
  deriveRuntimeHealth,
  runtimeUsageOptions,
  // CEREBRO-PATCH(runtime-list-daemon-col): FIR-2669 — daemon CLI version reader.
  readRuntimeCliVersion,
} from "@multica/core/runtimes";
import { useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
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
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { ProviderLogo } from "./provider-logo";
import { HealthIcon, useHealthLabel } from "./shared";
import { DeleteRuntimeDialog } from "./delete-runtime-dialog";
import {
  computeCostInWindow,
  formatLastSeen,
  isSelfHealingRuntime,
  pctChange,
} from "../utils";
import { splitRuntimeName } from "./runtime-machines";
// CEREBRO-PATCH(runtime-list-columns): FIR-2669 — configurable columns + Account cell.
// CEREBRO-PATCH(runtime-list-mobile-cards): FIR-2669 — mobile card list + computer-name.
import {
  RuntimeAccountCell,
  RuntimeMachineCell,
  RuntimeMobileList,
  useRuntimesViewStore,
  type RuntimeColumnKey,
} from "@multica/cerebro-runtime/views";
import { useFlagValue } from "@multica/cerebro-feature-flags";
import { useT } from "../../i18n";

// The machine detail's runtimes table on the shared ListGrid. Paradigm
// pieces are taken À LA CARTE here: subgrid template + var-width tracks +
// two-zone responsiveness (the detail pane gets squeezed by the machine
// list, so the container-driven core-set collapse matters more than on the
// full-width pages), but NO virtualization / sorting / filters / column
// toggles / batch selection — a machine hosts 1-5 runtimes, those would all
// be dead weight, and batch-deleting runtimes (a cascade-confirm heavy
// operation) is deliberately not offered.
// CEREBRO-PATCH(list-grid-edge-padding): FIR-2172 — no edge tracks (see agents-page).
// CEREBRO-PATCH(runtime-list-columns): FIR-2669 — Account track added before CLI.
// CEREBRO-PATCH(runtime-list-machine-col): FIR-2669 — Machine (computer) track before Account.
// CEREBRO-PATCH(runtime-list-daemon-col): FIR-2669 — Daemon (cli_version) track after CLI.
const GRID_COLS =
  "grid-cols-[minmax(120px,1fr)_var(--rtc-health)_var(--rtc-kebab)] " +
  "@2xl:grid-cols-[minmax(140px,1fr)_var(--rtc-health)_var(--rtc-owner)_var(--rtc-agents)_var(--rtc-cost)_var(--rtc-machine)_var(--rtc-account)_var(--rtc-cli)_var(--rtc-daemon)_var(--rtc-kebab)]";

const COLUMN_WIDTHS = {
  // Health folds the workload in as a suffix ("Healthy · 2 running") —
  // same merge as the agents list's status cell.
  health: 176,
  owner: 96,
  agents: 92,
  cost: 96,
  cli: 112,
  // CEREBRO-PATCH(runtime-list-columns): FIR-2669 — Account column width.
  account: 180,
  // CEREBRO-PATCH(runtime-list-machine-col): FIR-2669 — Machine column width.
  machine: 160,
  // CEREBRO-PATCH(runtime-list-daemon-col): FIR-2669 — Daemon column width.
  daemon: 112,
} as const;

// Fixed tracks (name min 140) plus gap-x-3 between the wide template's tracks.
// CEREBRO-PATCH(runtime-list-daemon-col): FIR-2669 — 9 gaps after adding Daemon.
const FIXED_TRACKS_WIDTH = 140 + 9 * 12;

// The kebab track is conditional like the owner column: on a healthy
// local machine EVERY row's only action (delete) is hidden by the
// self-healing rule, and an unconditionally reserved 28px action track
// would hang a permanent dead zone off the last column.
// CEREBRO-PATCH(runtime-list-columns): FIR-2669 — track widths honour the
// column picker: a hidden column collapses to a 0px track (same technique as
// the agents list).
function columnTrackVars(
  showOwner: boolean,
  showActions: boolean,
  isVisible: (key: RuntimeColumnKey) => boolean,
): React.CSSProperties {
  const w = (key: RuntimeColumnKey) =>
    isVisible(key) ? COLUMN_WIDTHS[key] : 0;
  const px = (key: RuntimeColumnKey) =>
    isVisible(key) ? `${COLUMN_WIDTHS[key]}px` : "0px";
  const minWidth =
    FIXED_TRACKS_WIDTH +
    COLUMN_WIDTHS.health +
    (showOwner ? COLUMN_WIDTHS.owner : 0) +
    w("agents") +
    w("cost") +
    // CEREBRO-PATCH(runtime-list-machine-col): FIR-2669 — Machine in min-width sum.
    w("machine") +
    w("account") +
    w("cli") +
    // CEREBRO-PATCH(runtime-list-daemon-col): FIR-2669 — Daemon in min-width sum.
    w("daemon") +
    (showActions ? 28 : 0);
  return {
    "--rtc-health": `${COLUMN_WIDTHS.health}px`,
    "--rtc-owner": showOwner ? `${COLUMN_WIDTHS.owner}px` : "0px",
    "--rtc-agents": px("agents"),
    "--rtc-cost": px("cost"),
    // CEREBRO-PATCH(runtime-list-machine-col): FIR-2669 — Machine track var.
    "--rtc-machine": px("machine"),
    "--rtc-account": px("account"),
    "--rtc-cli": px("cli"),
    // CEREBRO-PATCH(runtime-list-daemon-col): FIR-2669 — Daemon track var.
    "--rtc-daemon": px("daemon"),
    "--rtc-kebab": showActions ? "1.75rem" : "0px",
    "--rtc-minw": `${minWidth}px`,
  } as React.CSSProperties;
}

interface RuntimeWorkload {
  agentIds: string[];
  runningCount: number;
  queuedCount: number;
}

const EMPTY_WORKLOAD: RuntimeWorkload = {
  agentIds: [],
  runningCount: 0,
  queuedCount: 0,
};

export interface RuntimeRow {
  runtime: AgentRuntime;
  ownerMember: MemberWithUser | null;
  workload: RuntimeWorkload;
  canDelete: boolean;
}

// Per-runtime workload snapshot — agent IDs serving this runtime (drives
// the avatar stack; .length doubles as the agent count) plus task counts
// split by status. Built once per render off the workspace-wide
// agents / agent-task-snapshot caches; filtered locally — no extra requests.
export function buildWorkloadIndex(
  agents: Agent[],
  tasks: AgentTask[],
): Map<string, RuntimeWorkload> {
  const result = new Map<string, RuntimeWorkload>();
  const agentToRuntime = new Map<string, string>();

  for (const a of agents) {
    if (!a.runtime_id || a.archived_at) continue;
    agentToRuntime.set(a.id, a.runtime_id);
    const entry =
      result.get(a.runtime_id) ?? {
        agentIds: [],
        runningCount: 0,
        queuedCount: 0,
      };
    entry.agentIds.push(a.id);
    result.set(a.runtime_id, entry);
  }
  for (const t of tasks) {
    const rid = agentToRuntime.get(t.agent_id);
    if (!rid) continue;
    const entry = result.get(rid);
    if (!entry) continue;
    if (t.status === "running") entry.runningCount += 1;
    else if (t.status === "queued" || t.status === "dispatched")
      entry.queuedCount += 1;
  }
  return result;
}

// ---------------------------------------------------------------------------
// Cells
// ---------------------------------------------------------------------------

function RuntimeNameCell({ runtime }: { runtime: AgentRuntime }) {
  const { base: baseName } = splitRuntimeName(runtime.name);
  return (
    <ListGridCell className="gap-2">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center">
        <ProviderLogo provider={runtime.provider} className="h-5 w-5" />
      </div>
      <div className="flex min-w-0 flex-1 items-center gap-1.5">
        <span className="block min-w-0 shrink truncate text-sm font-medium">
          {baseName}
        </span>
        <VisibilityBadge runtime={runtime} />
      </div>
    </ListGridCell>
  );
}

// Only public is worth a badge — private is the default and rendering a
// `🔒 Private` chip on every row turns the whole column into noise.
function VisibilityBadge({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  if (runtime.visibility !== "public") return null;
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className="inline-flex shrink-0 items-center gap-0.5 rounded bg-info/10 px-1 text-[10px] font-medium text-info">
            <Globe className="h-2.5 w-2.5" />
            {t(($) => $.detail.visibility_label.public)}
          </span>
        }
      />
      <TooltipContent>
        {t(($) => $.detail.visibility_hint.public)}
      </TooltipContent>
    </Tooltip>
  );
}

// Health with the load folded in as a "· N tasks" suffix — verbatim the
// same form as the agents list's status cell, so the two surfaces speak
// one language. The suffix is a unit-bearing count (running + queued);
// offline-ish rows skip it (health already says it all), idle rows skip
// it (idle is the unremarkable default). If "queued but nothing running"
// ever becomes a signal worth surfacing, it belongs to the HEALTH layer
// (a new deriveRuntimeHealth state), not to vocabulary hints here.
function HealthCell({
  runtime,
  workload,
  now,
}: {
  runtime: AgentRuntime;
  workload: RuntimeWorkload;
  now: number;
}) {
  const { t: tAgents } = useT("agents");
  const labelOf = useHealthLabel();
  const health = deriveRuntimeHealth(runtime, now);
  const offline = health === "offline" || health === "about_to_gc";
  const lastSeen = formatLastSeen(runtime.last_seen_at);
  const active = workload.runningCount + workload.queuedCount;

  return (
    <ListGridCell className="gap-1.5">
      <HealthIcon health={health} />
      <span className="block min-w-0 truncate text-xs">
        {labelOf(health)}
        {health !== "online" && runtime.last_seen_at && (
          <span className="text-muted-foreground"> · {lastSeen}</span>
        )}
        {!offline && active > 0 && (
          <span className="text-muted-foreground">
            {" · "}
            {tAgents(($) => $.row.task_count, { count: active })}
          </span>
        )}
      </span>
    </ListGridCell>
  );
}

// Per-row cost — only renders a 7d total + delta vs the prior 7d, so we
// only need 14 days of usage. Previously this fetched a 180-day window to
// share the cache key with the runtime-detail page, but that turned the
// list page into N × 180d in-line aggregations against `task_usage` (one
// per runtime row) and dominated DB load for this view. Detail still
// fetches its own 180d window on navigation; the cold-load difference for
// detail is one extra request, while the steady-state savings on the list
// page are large.
const COST_CELL_DAYS = 14;

export function CostCell({ runtimeId }: { runtimeId: string }) {
  const { t } = useT("runtimes");
  const { data: usage = [] } = useQuery(
    runtimeUsageOptions(runtimeId, COST_CELL_DAYS),
  );
  const cost7d = useMemo(() => computeCostInWindow(usage, 7), [usage]);
  const costPrev7d = useMemo(
    () => computeCostInWindow(usage, 7, 7),
    [usage],
  );
  const delta = pctChange(cost7d, costPrev7d);

  if (usage.length === 0) {
    return (
      <div className="w-full text-right">
        <span className="text-xs text-muted-foreground/50">—</span>
      </div>
    );
  }
  const fmt = cost7d >= 100 ? `$${cost7d.toFixed(0)}` : `$${cost7d.toFixed(2)}`;
  const deltaTone =
    delta == null
      ? "text-muted-foreground"
      : delta > 0
        ? "text-warning"
        : delta < 0
          ? "text-success"
          : "text-muted-foreground";
  const deltaLabel =
    delta == null
      ? null
      : delta === 0
        ? t(($) => $.list.cost_delta_flat)
        : `${delta > 0 ? "↑" : "↓"}${Math.abs(delta)}%`;
  return (
    <div className="flex w-full flex-col items-end leading-tight">
      <span className="text-sm font-medium tabular-nums">{fmt}</span>
      {deltaLabel && (
        <span className={`text-[11px] tabular-nums ${deltaTone}`}>
          {deltaLabel}
        </span>
      )}
    </div>
  );
}

export function CliCell({ runtime }: { runtime: AgentRuntime }) {
  if (runtime.runtime_mode === "cloud") {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  const meta = runtime.metadata as Record<string, unknown> | null;
  // `version` is the agent's own underlying CLI tool version — distinct per
  // provider (e.g. "2.1.5 (Claude Code)", "codex-cli 0.118.0", "0.42.0").
  // The separate `cli_version` is the shared multica daemon CLI, identical
  // for every runtime on one machine; surfacing it here made all agents
  // show the same number (#3838). The daemon CLI version and its update
  // prompt belong to the machine — they live in the machine meta strip and
  // the detail page's UpdateSection, not on a per-agent row.
  const version =
    meta && typeof meta.version === "string" ? meta.version : null;

  if (!version) {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }

  return (
    <div className="flex min-w-0 items-center text-xs">
      <span className="truncate font-mono text-muted-foreground">
        {version}
      </span>
    </div>
  );
}

// CEREBRO-PATCH(runtime-list-daemon-col): FIR-2669 — the multica daemon CLI
// version (metadata.cli_version). Shared per machine, distinct from CliCell's
// per-agent tool version (metadata.version). Cloud runtimes have no daemon.
export function DaemonCell({ runtime }: { runtime: AgentRuntime }) {
  if (runtime.runtime_mode === "cloud") {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  const version = readRuntimeCliVersion(
    runtime.metadata as Record<string, unknown> | undefined,
  );
  if (!version) {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  return (
    <div className="flex min-w-0 items-center text-xs">
      <span className="truncate font-mono text-muted-foreground">
        {version}
      </span>
    </div>
  );
}

// Stacks up to 3 agent avatars, then a "+N" pill if more bind to this
// runtime. Each avatar uses the wrapping ActorAvatar so hover automatically
// surfaces AgentProfileCard.
function AgentStack({ agentIds }: { agentIds: string[] }) {
  if (agentIds.length === 0) {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  const visible = agentIds.slice(0, 3);
  const extra = agentIds.length - visible.length;
  return (
    <div className="flex items-center -space-x-1.5">
      {visible.map((id) => (
        <span
          key={id}
          className="inline-flex rounded-full ring-2 ring-background"
        >
          <ActorAvatar
            actorType="agent"
            actorId={id}
            size={22}
            enableHoverCard
          />
        </span>
      ))}
      {extra > 0 && (
        <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-muted text-xs font-medium text-muted-foreground ring-2 ring-background">
          +{extra}
        </span>
      )}
    </div>
  );
}

export function RuntimeRowMenu({
  runtime,
  wsId,
  canDelete,
}: {
  runtime: AgentRuntime;
  wsId: string;
  canDelete: boolean;
}) {
  const { t } = useT("runtimes");
  const [deleteOpen, setDeleteOpen] = useState(false);
  // Delete is currently the only row action; if the row can't run it, drop
  // the kebab entirely so the column doesn't render an empty popover. The
  // self-healing case (local + online) is the runtime-detail parity fix —
  // see isSelfHealingRuntime for the rationale.
  const selfHealing = isSelfHealingRuntime(runtime);

  if (!canDelete || selfHealing) {
    return <span aria-hidden />;
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant="ghost"
              size="icon-sm"
              aria-label={t(($) => $.list.row_actions_aria)}
              onClick={(e) => e.stopPropagation()}
              onKeyDown={(e) => e.stopPropagation()}
            />
          }
        >
          <MoreHorizontal className="h-4 w-4 text-muted-foreground" />
        </DropdownMenuTrigger>
        <DropdownMenuContent
          align="end"
          className="w-40"
          onClick={(e) => e.stopPropagation()}
        >
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setDeleteOpen(true)}
            title={t(($) => $.list.delete_permission_hint)}
          >
            <Trash2 className="h-3.5 w-3.5" />
            {t(($) => $.list.delete_action)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <DeleteRuntimeDialog
        open={deleteOpen}
        onOpenChange={setDeleteOpen}
        runtime={runtime}
        wsId={wsId}
        onDeleted={() => {
          setDeleteOpen(false);
          toast.success(t(($) => $.detail.toast_deleted));
        }}
      />
    </>
  );
}

// ---------------------------------------------------------------------------
// List
// ---------------------------------------------------------------------------

export function RuntimeList({
  runtimes,
  updatableIds,
  now,
}: {
  runtimes: AgentRuntime[];
  // Kept on the API surface for callers — the CLI column re-derives
  // update state per row via metadata.cli_version + the GitHub-release
  // query, so this prop is now unused. Left to avoid scope creep on the
  // page-level wrapper that still computes the set.
  updatableIds?: Set<string>;
  now: number;
}) {
  void updatableIds;

  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const user = useAuthStore((s) => s.user);

  // CEREBRO-PATCH(runtime-list-columns): FIR-2669 — the picker hides columns;
  // Account only exists when the flag is on. Flag off = original layout.
  const columnsEnabled = useFlagValue("cerebro_interface_columns");
  const hiddenColumns = useRuntimesViewStore((s) => s.hiddenColumns);
  // CEREBRO-PATCH(runtime-list-daemon-col): FIR-2669 — Daemon is a cerebro column, gated like Account/Machine.
  const isVisible = (key: RuntimeColumnKey) =>
    key === "account" || key === "machine" || key === "daemon"
      ? columnsEnabled && !hiddenColumns.includes(key)
      : columnsEnabled
        ? !hiddenColumns.includes(key)
        : true;

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  const currentMember = user
    ? members.find((m) => m.user_id === user.id)
    : null;
  const isAdmin = currentMember
    ? currentMember.role === "owner" || currentMember.role === "admin"
    : false;

  const workloadIndex = useMemo(
    () => buildWorkloadIndex(agents, snapshot),
    [agents, snapshot],
  );

  const memberById = useMemo(() => {
    const map = new Map<string, MemberWithUser>();
    for (const m of members) map.set(m.user_id, m);
    return map;
  }, [members]);

  // Owner column only earns its space when the page actually has multiple
  // distinct owners — otherwise it would just be a column of identical
  // avatars.
  const showOwner = useMemo(() => {
    const owners = new Set<string>();
    for (const r of runtimes) {
      if (r.owner_id) owners.add(r.owner_id);
    }
    return owners.size > 1;
  }, [runtimes]);

  const rows = useMemo<RuntimeRow[]>(() => {
    return runtimes.map((runtime) => ({
      runtime,
      ownerMember: runtime.owner_id
        ? memberById.get(runtime.owner_id) ?? null
        : null,
      workload: workloadIndex.get(runtime.id) ?? EMPTY_WORKLOAD,
      canDelete: isAdmin || (!!user && runtime.owner_id === user.id),
    }));
  }, [runtimes, memberById, workloadIndex, isAdmin, user]);

  // Mirrors RuntimeRowMenu's render guard: the kebab track only earns its
  // width when at least one row will actually show the menu.
  const showActions = rows.some(
    (row) => row.canDelete && !isSelfHealingRuntime(row.runtime),
  );

  return (
    // CEREBRO-PATCH(runtime-list-mobile-scroll): FIR-2669 — scroll vertically like the agents list (was overflow-y-hidden, which trapped the list in a fixed box on mobile).
    <div className="min-h-0 flex-1 overflow-auto @container">
      {/* CEREBRO-PATCH(runtime-list-mobile-cards): FIR-2669 — mobile stacked cards surface enabled columns; desktop table hides below @2xl. */}
      {columnsEnabled && (
        <RuntimeMobileList
          rows={rows.map((r) => ({
            runtime: r.runtime,
            agentCount: r.workload.agentIds.length,
          }))}
          now={now}
          className="@2xl:hidden"
        />
      )}
      <ListGrid
        // CEREBRO-PATCH(runtime-list-mobile-cards): FIR-2669 — desktop-only when the mobile card layout is active.
        className={`${GRID_COLS} @2xl:min-w-[var(--rtc-minw)] ${columnsEnabled ? "hidden @2xl:grid" : ""}`}
        // CEREBRO-PATCH(runtime-list-columns): FIR-2669 — pass column visibility.
        style={columnTrackVars(showOwner, showActions, isVisible)}
      >
        <ListGridHeader>
          <ListGridHeaderCell>
            {t(($) => $.list.col_runtime)}
          </ListGridHeaderCell>
          <ListGridHeaderCell>{t(($) => $.list.col_health)}</ListGridHeaderCell>
          {showOwner ? (
            <ListGridHeaderCell className="hidden @2xl:flex">
              {t(($) => $.list.col_owner)}
            </ListGridHeaderCell>
          ) : (
            <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
          )}
          {/* CEREBRO-PATCH(runtime-list-columns): FIR-2669 — picker-driven headers + Account. */}
          {isVisible("agents") ? (
            <ListGridHeaderCell className="hidden @2xl:flex">
              {t(($) => $.list.col_agents)}
            </ListGridHeaderCell>
          ) : (
            <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
          )}
          {isVisible("cost") ? (
            <ListGridHeaderCell className="hidden @2xl:flex" align="right">
              {t(($) => $.list.col_cost)}
            </ListGridHeaderCell>
          ) : (
            <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
          )}
          {/* CEREBRO-PATCH(runtime-list-machine-col): FIR-2669 — Machine (computer) header. */}
          {isVisible("machine") ? (
            <ListGridHeaderCell className="hidden @2xl:flex">
              {t(($) => $.list.col_machine)}
            </ListGridHeaderCell>
          ) : (
            <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
          )}
          {isVisible("account") ? (
            <ListGridHeaderCell className="hidden @2xl:flex">
              {t(($) => $.list.col_account)}
            </ListGridHeaderCell>
          ) : (
            <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
          )}
          {isVisible("cli") ? (
            <ListGridHeaderCell className="hidden @2xl:flex">
              {t(($) => $.list.col_cli)}
            </ListGridHeaderCell>
          ) : (
            <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
          )}
          {/* CEREBRO-PATCH(runtime-list-daemon-col): FIR-2669 — Daemon (cli_version) header. */}
          {isVisible("daemon") ? (
            <ListGridHeaderCell className="hidden @2xl:flex">
              {t(($) => $.list.col_daemon)}
            </ListGridHeaderCell>
          ) : (
            <ListGridHeaderCell className="hidden px-0 @2xl:flex" />
          )}
          <ListGridHeaderCell className="px-0" />
        </ListGridHeader>
        {rows.map((row) => (
          <ListGridRow
            key={row.runtime.id}
            render={<AppLink href={wsPaths.runtimeDetail(row.runtime.id)} />}
          >
            <RuntimeNameCell runtime={row.runtime} />
            <HealthCell
              runtime={row.runtime}
              workload={row.workload}
              now={now}
            />
            {showOwner ? (
              <ListGridCell className="hidden gap-1.5 @2xl:flex">
                {row.ownerMember ? (
                  <>
                    <ActorAvatar
                      actorType="member"
                      actorId={row.ownerMember.user_id}
                      size={18}
                    />
                    <span className="min-w-0 truncate text-xs text-muted-foreground">
                      {row.ownerMember.name}
                    </span>
                  </>
                ) : (
                  <span className="text-xs text-muted-foreground/50">—</span>
                )}
              </ListGridCell>
            ) : (
              <ListGridCell className="hidden px-0 @2xl:flex" />
            )}
            {/* CEREBRO-PATCH(runtime-list-columns): FIR-2669 — picker-driven cells + Account. */}
            {isVisible("agents") ? (
              <ListGridCell className="hidden @2xl:flex">
                <AgentStack agentIds={row.workload.agentIds} />
              </ListGridCell>
            ) : (
              <ListGridCell className="hidden px-0 @2xl:flex" />
            )}
            {isVisible("cost") ? (
              <ListGridCell className="hidden @2xl:flex">
                <CostCell runtimeId={row.runtime.id} />
              </ListGridCell>
            ) : (
              <ListGridCell className="hidden px-0 @2xl:flex" />
            )}
            {/* CEREBRO-PATCH(runtime-list-machine-col): FIR-2669 — Machine (computer) cell. */}
            {isVisible("machine") ? (
              <ListGridCell className="hidden @2xl:flex">
                <RuntimeMachineCell runtime={row.runtime} />
              </ListGridCell>
            ) : (
              <ListGridCell className="hidden px-0 @2xl:flex" />
            )}
            {isVisible("account") ? (
              <ListGridCell className="hidden @2xl:flex">
                <RuntimeAccountCell runtime={row.runtime} />
              </ListGridCell>
            ) : (
              <ListGridCell className="hidden px-0 @2xl:flex" />
            )}
            {isVisible("cli") ? (
              <ListGridCell className="hidden @2xl:flex">
                <CliCell runtime={row.runtime} />
              </ListGridCell>
            ) : (
              <ListGridCell className="hidden px-0 @2xl:flex" />
            )}
            {/* CEREBRO-PATCH(runtime-list-daemon-col): FIR-2669 — Daemon (cli_version) cell. */}
            {isVisible("daemon") ? (
              <ListGridCell className="hidden @2xl:flex">
                <DaemonCell runtime={row.runtime} />
              </ListGridCell>
            ) : (
              <ListGridCell className="hidden px-0 @2xl:flex" />
            )}
            <ListGridCell className="justify-end px-0">
              <span
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                }}
                className="flex items-center"
              >
                <RuntimeRowMenu
                  runtime={row.runtime}
                  wsId={wsId}
                  canDelete={row.canDelete}
                />
              </span>
            </ListGridCell>
          </ListGridRow>
        ))}
      </ListGrid>
    </div>
  );
}
