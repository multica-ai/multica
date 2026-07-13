"use client";

import { useMemo, useState } from "react";
import {
  AlertTriangle,
  Globe,
  Loader2,
  MoreHorizontal,
  Pencil,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { CurrencyNumberFlow } from "@multica/ui/components/ui/number-flow";
import type {
  Agent,
  AgentRuntime,
  AgentTask,
  MemberWithUser,
  RuntimeProfile,
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
  runtimeProfileListOptions,
  runtimeUsageOptions,
} from "@multica/core/runtimes";
import { useWorkspacePaths } from "@multica/core/paths";
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
import { useRowLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { useViewingTimezone } from "../../common/use-viewing-timezone";
import { ProviderLogo } from "./provider-logo";
import { HealthIcon, useHealthLabel } from "./shared";
import { DeleteRuntimeDialog } from "./delete-runtime-dialog";
import { DeleteRuntimeProfileDialog } from "./delete-runtime-profile-dialog";
import { RuntimeProfilesDialog } from "./runtime-profiles-dialog";
import {
  computeCostInWindow,
  pctChange,
} from "../utils";
import { splitRuntimeName } from "./runtime-machines";
import {
  customRuntimeRegistrationFailure,
  isDisabledCustomRuntime,
  isPendingCustomRuntime,
  isPendingCustomRuntimeWarning,
  pendingRuntimeCommandName,
} from "./pending-runtime";
import { useT, useTimeAgo } from "../../i18n";

// The machine detail's runtimes table on the shared ListGrid. Paradigm
// pieces are taken À LA CARTE here: subgrid template + var-width tracks +
// two-zone responsiveness (the detail pane gets squeezed by the machine
// list, so the container-driven core-set collapse matters more than on the
// full-width pages), but NO virtualization / sorting / filters / column
// toggles / batch selection — a machine hosts 1-5 runtimes, those would all
// be dead weight, and batch-deleting runtimes (a cascade-confirm heavy
// operation) is deliberately not offered.
const GRID_COLS =
  "grid-cols-[0.75rem_minmax(120px,1fr)_var(--rtc-health)_var(--rtc-kebab)_0.75rem] " +
  "@2xl:grid-cols-[0.75rem_minmax(140px,1fr)_var(--rtc-health)_var(--rtc-owner)_var(--rtc-agents)_var(--rtc-cost)_var(--rtc-cli)_var(--rtc-kebab)_0.75rem]";

const COLUMN_WIDTHS = {
  // Health folds the workload in as a suffix ("Healthy · 2 running") —
  // same merge as the agents list's status cell.
  health: 176,
  owner: 96,
  agents: 92,
  cost: 96,
  cli: 112,
} as const;

// Fixed tracks (edges 12+12, name min 140) plus the 8 gap-x-3 gaps
// between the wide template's 9 tracks (zero-width tracks still carry
// gaps).
const FIXED_TRACKS_WIDTH = 164 + 8 * 12;

// The kebab track is conditional like the owner column: on a list where
// no row carries a delete-permission, EVERY row's only action is hidden,
// and an unconditionally reserved 28px action track would hang a
// permanent dead zone off the last column.
function columnTrackVars(
  showOwner: boolean,
  showActions: boolean,
): React.CSSProperties {
  const minWidth =
    FIXED_TRACKS_WIDTH +
    COLUMN_WIDTHS.health +
    (showOwner ? COLUMN_WIDTHS.owner : 0) +
    COLUMN_WIDTHS.agents +
    COLUMN_WIDTHS.cost +
    COLUMN_WIDTHS.cli +
    (showActions ? 28 : 0);
  return {
    "--rtc-health": `${COLUMN_WIDTHS.health}px`,
    "--rtc-owner": showOwner ? `${COLUMN_WIDTHS.owner}px` : "0px",
    "--rtc-agents": `${COLUMN_WIDTHS.agents}px`,
    "--rtc-cost": `${COLUMN_WIDTHS.cost}px`,
    "--rtc-cli": `${COLUMN_WIDTHS.cli}px`,
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
  profile: RuntimeProfile | null;
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
        <RuntimeKindBadge runtime={runtime} />
        <PendingRuntimeBadge runtime={runtime} />
        <VisibilityBadge runtime={runtime} />
      </div>
    </ListGridCell>
  );
}

// Distinguishes a built-in protocol-family runtime from one launched off a
// custom runtime profile. `profile_id` is the discriminator: a non-null /
// non-empty value means the runtime was started from a custom profile.
// Older backends omit the field — treated as built-in.
function RuntimeKindBadge({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const isCustom = !!runtime.profile_id;
  return (
    <span
      className={
        isCustom
          ? "inline-flex shrink-0 items-center rounded bg-info/10 px-1 text-[10px] font-medium text-info"
          : "inline-flex shrink-0 items-center rounded bg-muted px-1 text-[10px] font-medium text-muted-foreground"
      }
    >
      {isCustom
        ? t(($) => $.list.badge_custom)
        : t(($) => $.list.badge_builtin)}
    </span>
  );
}

function PendingRuntimeBadge({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  if (!isPendingCustomRuntime(runtime)) return null;
  if (isDisabledCustomRuntime(runtime)) {
    return (
      <span className="inline-flex shrink-0 items-center rounded bg-muted px-1 text-[10px] font-medium text-muted-foreground">
        {t(($) => $.list.badge_disabled)}
      </span>
    );
  }
  return (
    <span className="inline-flex shrink-0 items-center rounded bg-warning/10 px-1 text-[10px] font-medium text-warning">
      {t(($) => $.list.badge_registering)}
    </span>
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
  const { t } = useT("runtimes");
  const { t: tAgents } = useT("agents");
  const labelOf = useHealthLabel();
  const timeAgo = useTimeAgo();
  if (isDisabledCustomRuntime(runtime)) {
    return (
      <ListGridCell>
        <span className="text-xs text-muted-foreground">
          {t(($) => $.list.pending_health_disabled)}
        </span>
      </ListGridCell>
    );
  }
  const registrationFailure = customRuntimeRegistrationFailure(runtime);
  if (registrationFailure) {
    return (
      <ListGridCell className="gap-1.5">
        <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-destructive" />
        <span
          className="block min-w-0 truncate text-xs text-destructive"
          title={registrationFailure}
        >
          {t(($) => $.list.pending_health_error)}
        </span>
      </ListGridCell>
    );
  }
  if (isPendingCustomRuntime(runtime)) {
    const warning = isPendingCustomRuntimeWarning(runtime, now);
    return (
      <ListGridCell className="gap-1.5">
        {warning ? (
          <AlertTriangle className="h-3.5 w-3.5 shrink-0 text-warning" />
        ) : (
          <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-info" />
        )}
        <span className="block min-w-0 truncate text-xs">
          {warning
            ? t(($) => $.list.pending_health_warning)
            : t(($) => $.list.pending_health)}
        </span>
      </ListGridCell>
    );
  }

  const health = deriveRuntimeHealth(runtime, now);
  const offline = health === "offline" || health === "about_to_gc";
  const lastSeen = runtime.last_seen_at ? timeAgo(runtime.last_seen_at) : null;
  const active = workload.runningCount + workload.queuedCount;

  return (
    <ListGridCell className="gap-1.5">
      <HealthIcon health={health} />
      <span className="block min-w-0 truncate text-xs">
        {labelOf(health)}
        {health !== "online" && lastSeen && (
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
  const { t, i18n } = useT("runtimes");
  const tz = useViewingTimezone();
  const locales = i18n.resolvedLanguage ?? i18n.language;
  const { data: usage = [] } = useQuery(
    runtimeUsageOptions(runtimeId, COST_CELL_DAYS, tz),
  );
  const cost7d = useMemo(() => computeCostInWindow(usage, 7, tz), [usage, tz]);
  const costPrev7d = useMemo(
    () => computeCostInWindow(usage, 7, tz, 7),
    [usage, tz],
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
      <CurrencyNumberFlow
        value={cost7d}
        locales={locales}
        aria-label={fmt}
        className="text-sm font-medium"
      />
      {deltaLabel && (
        <span className={`text-[11px] tabular-nums ${deltaTone}`}>
          {deltaLabel}
        </span>
      )}
    </div>
  );
}

export function CliCell({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const failure = customRuntimeRegistrationFailure(runtime);
  if (failure) {
    const command = pendingRuntimeCommandName(runtime);
    return (
      <div className="flex min-w-0 flex-col text-xs">
        {command && (
          <span
            className="truncate font-mono text-muted-foreground"
            title={command}
          >
            {command}
          </span>
        )}
        <span className="truncate text-destructive" title={failure}>
          {failure}
        </span>
      </div>
    );
  }
  if (isPendingCustomRuntime(runtime)) {
    const command = pendingRuntimeCommandName(runtime);
    if (!command) {
      return (
        <span className="text-xs text-muted-foreground/50">
          {t(($) => $.list.pending_cli_unknown)}
        </span>
      );
    }
    return (
      <div className="flex min-w-0 items-center text-xs">
        <span
          className="truncate font-mono text-muted-foreground"
          title={command}
        >
          {command}
        </span>
      </div>
    );
  }

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
            size="md"
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
  profile,
  wsId,
  canDelete,
}: {
  runtime: AgentRuntime;
  profile: RuntimeProfile | null;
  wsId: string;
  canDelete: boolean;
}) {
  const { t } = useT("runtimes");
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const isCustomRuntime = !!runtime.profile_id;
  // Delete is currently the only row action; if the row can't run it, drop
  // the kebab entirely so the column doesn't render an empty popover. We
  // used to also hide it for self-healing runtimes (live local daemon
  // re-registers within seconds), but MUL-3352 surfaced that owners read
  // a missing kebab as "I lost my permission" rather than "the daemon
  // would undo this". The dialog now carries the self-heal warning and
  // the user gets to decide.

  if (!canDelete) {
    return <span aria-hidden />;
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              aria-label={t(($) => $.list.row_actions_aria)}
              className="flex size-7 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-accent-foreground focus-visible:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring group-hover/row:opacity-100 data-popup-open:bg-accent data-popup-open:opacity-100 data-popup-open:text-accent-foreground"
            >
              <MoreHorizontal className="size-4" />
            </button>
          }
        />
        <DropdownMenuContent align="end" className="w-40">
          {isCustomRuntime && profile && (
            <DropdownMenuItem onClick={() => setEditOpen(true)}>
              <Pencil aria-hidden="true" className="h-3.5 w-3.5" />
              {t(($) => $.list.edit_action)}
            </DropdownMenuItem>
          )}
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setDeleteOpen(true)}
            title={t(($) => $.list.delete_permission_hint)}
          >
            <Trash2 aria-hidden="true" className="h-3.5 w-3.5" />
            {isCustomRuntime
              ? t(($) => $.list.delete_profile_action)
              : t(($) => $.list.delete_action)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      {isCustomRuntime && profile && editOpen && (
        <RuntimeProfilesDialog
          wsId={wsId}
          intent="edit"
          initialProfile={profile}
          onClose={() => setEditOpen(false)}
        />
      )}
      {isCustomRuntime && profile ? (
        <DeleteRuntimeProfileDialog
          open={deleteOpen}
          onOpenChange={setDeleteOpen}
          profile={profile}
          wsId={wsId}
          onDeleted={() => setDeleteOpen(false)}
        />
      ) : (
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
      )}
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
  runtimeHref,
}: {
  runtimes: AgentRuntime[];
  // Kept on the API surface for callers, but unused here: the CLI column
  // shows each agent's own tool version, while the multica daemon CLI
  // update prompt lives at the machine/detail level (UpdateSection), so the
  // table no longer derives per-row update state. Left to avoid scope creep
  // on the page-level wrapper that still computes the set.
  updatableIds?: Set<string>;
  now: number;
  /** Machine-detail pages keep runtime settings nested under the machine. */
  runtimeHref?: (runtimeId: string) => string;
}) {
  void updatableIds;

  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const rowLink = useRowLink();
  const user = useAuthStore((s) => s.user);

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const { data: profiles = [] } = useQuery(runtimeProfileListOptions(wsId));

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

  const profileById = useMemo(() => {
    const map = new Map<string, RuntimeProfile>();
    for (const p of profiles) map.set(p.id, p);
    return map;
  }, [profiles]);

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
    return runtimes.map((runtime) => {
      const profile = runtime.profile_id
        ? profileById.get(runtime.profile_id) ?? null
        : null;
      const isCustomRuntime = !!runtime.profile_id;
      return {
        runtime,
        profile,
        ownerMember: runtime.owner_id
          ? memberById.get(runtime.owner_id) ?? null
          : null,
        workload: workloadIndex.get(runtime.id) ?? EMPTY_WORKLOAD,
        canDelete: isCustomRuntime
          ? isAdmin && !!profile
          : !isPendingCustomRuntime(runtime) &&
            (isAdmin || (!!user && runtime.owner_id === user.id)),
      };
    });
  }, [runtimes, profileById, memberById, workloadIndex, isAdmin, user]);

  // Mirrors RuntimeRowMenu's render guard: the kebab track only earns its
  // width when at least one row will actually show the menu.
  const showActions = rows.some((row) => row.canDelete);

  return (
    <div className="overflow-x-auto overflow-y-hidden @container">
      <ListGrid
        className={`${GRID_COLS} @2xl:min-w-[var(--rtc-minw)]`}
        style={columnTrackVars(showOwner, showActions)}
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
          <ListGridHeaderCell className="hidden @2xl:flex">
            {t(($) => $.list.col_agents)}
          </ListGridHeaderCell>
          <ListGridHeaderCell className="hidden @2xl:flex" align="right">
            {t(($) => $.list.col_cost)}
          </ListGridHeaderCell>
          <ListGridHeaderCell className="hidden @2xl:flex">
            {t(($) => $.list.col_cli)}
          </ListGridHeaderCell>
          <span aria-hidden="true" />
        </ListGridHeader>
        {rows.map((row) => {
          const pending = isPendingCustomRuntime(row.runtime);
          return (
            <ListGridRow
              key={row.runtime.id}
              className={pending ? "cursor-default" : "cursor-pointer"}
              {...(!pending
                ? rowLink(
                    runtimeHref?.(row.runtime.id) ??
                      wsPaths.runtimeDetail(row.runtime.id),
                  )
                : {})}
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
                        size="sm"
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
              <ListGridCell className="hidden @2xl:flex">
                <AgentStack agentIds={row.workload.agentIds} />
              </ListGridCell>
              <ListGridCell className="hidden @2xl:flex">
                {pending ? (
                  <div className="w-full text-right">
                    <span className="text-xs text-muted-foreground/50">—</span>
                  </div>
                ) : (
                  <CostCell runtimeId={row.runtime.id} />
                )}
              </ListGridCell>
              <ListGridCell className="hidden @2xl:flex">
                <CliCell runtime={row.runtime} />
              </ListGridCell>
              <ListGridCell className="justify-end px-0">
                <span
                  onClick={(e) => e.stopPropagation()}
                  className="flex items-center"
                >
                  <RuntimeRowMenu
                    runtime={row.runtime}
                    profile={row.profile}
                    wsId={wsId}
                    canDelete={row.canDelete}
                  />
                </span>
              </ListGridCell>
            </ListGridRow>
          );
        })}
      </ListGrid>
    </div>
  );
}
