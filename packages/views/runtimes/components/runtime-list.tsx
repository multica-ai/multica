"use client";

import { useMemo, useState } from "react";
import {
  AlertTriangle,
  ExternalLink,
  Globe,
  Users,
  Loader2,
  Pencil,
  Trash2,
} from "@multica/ui/icons";
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
  isRuntimeUsableForUser,
  runtimeProfileListOptions,
  runtimeUsageOptions,
} from "@multica/core/runtimes";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useIntentNavigate } from "../../navigation";
import {
  CollectionEmptyValue,
  CollectionList,
  CollectionNameCell,
  CollectionRowMenu,
  type CollectionColumn,
} from "../../layout/collection-list";
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
import { runtimeRowLabel } from "./runtime-machines";
import {
  customRuntimeRegistrationFailure,
  isDisabledCustomRuntime,
  isPendingCustomRuntime,
  isPendingCustomRuntimeWarning,
  pendingRuntimeCommandName,
} from "./pending-runtime";
import { useT, useTimeAgo } from "../../i18n";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";

// The machine detail's runtimes table. A machine hosts 1-5 runtimes, so no
// sorting, filters, column toggles or batch selection — and batch-deleting
// runtimes (a cascade-confirm heavy operation) is deliberately not offered.

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

// Cells

function RuntimeNameCell({
  runtime,
  machineTitle,
}: {
  runtime: AgentRuntime;
  /**
   * The containing machine's title. Lets a per-runtime alias surface here
   * while a machine-level rename (shared by every runtime on the daemon)
   * collapses to the provider base so it isn't repeated on every row
   * (MUL-5248). Omitted when the row has no machine context (orphan custom
   * runtime profiles), where any alias is shown verbatim.
   */
  machineTitle?: string;
}) {
  return (
    <CollectionNameCell
      icon={<ProviderLogo provider={runtime.provider} className="size-4 shrink-0" />}
      title={runtimeRowLabel(runtime, machineTitle ?? "")}
      badges={
        <>
          <RuntimeKindBadge runtime={runtime} />
          <PendingRuntimeBadge runtime={runtime} />
          <VisibilityBadge runtime={runtime} />
        </>
      }
    />
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
          ? "inline-flex shrink-0 items-center rounded-xs bg-info/10 px-1 text-micro font-medium text-info"
          : "inline-flex shrink-0 items-center rounded-xs bg-muted px-1 text-micro font-medium text-muted-foreground"
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
      <span className="inline-flex shrink-0 items-center rounded-xs bg-muted px-1 text-micro font-medium text-muted-foreground">
        {t(($) => $.list.badge_disabled)}
      </span>
    );
  }
  return (
    <span className="inline-flex shrink-0 items-center rounded-xs bg-warning/10 px-1 text-micro font-medium text-warning">
      {t(($) => $.list.badge_registering)}
    </span>
  );
}

// Only public is worth a badge — private is the default and rendering a
// `🔒 Private` chip on every row turns the whole column into noise.
function VisibilityBadge({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const scope = runtime.visibility === "public" ? "public" : "members";
  if (scope === "members" && !runtime.shared_with?.length) return null;
  const Icon = scope === "public" ? Globe : Users;
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className="inline-flex shrink-0 items-center gap-0.5 rounded-xs bg-info/10 px-1 text-micro font-medium text-info">
            <Icon className="h-2.5 w-2.5" />
            {t(($) => $.detail.visibility_label[scope])}
          </span>
        }
      />
      <TooltipContent>
        {t(($) => $.detail.visibility_hint_readonly[scope])}
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
    return <span className="truncate">{t(($) => $.list.pending_health_disabled)}</span>;
  }
  const registrationFailure = customRuntimeRegistrationFailure(runtime);
  if (registrationFailure) {
    return (
      <>
        <AlertTriangle className="size-3.5 shrink-0 text-destructive" />
        <span className="min-w-0 truncate text-destructive" title={registrationFailure}>
          {t(($) => $.list.pending_health_error)}
        </span>
      </>
    );
  }
  if (isPendingCustomRuntime(runtime)) {
    const warning = isPendingCustomRuntimeWarning(runtime, now);
    return (
      <>
        {warning ? (
          <AlertTriangle className="size-3.5 shrink-0 text-warning" />
        ) : (
          <Loader2 className="size-3.5 shrink-0 animate-spin text-info" />
        )}
        <span className="min-w-0 truncate text-foreground">
          {warning
            ? t(($) => $.list.pending_health_warning)
            : t(($) => $.list.pending_health)}
        </span>
      </>
    );
  }

  const health = deriveRuntimeHealth(runtime, now);
  const offline = health === "offline" || health === "long_offline";
  const lastSeen = runtime.last_seen_at ? timeAgo(runtime.last_seen_at) : null;
  const active = workload.runningCount + workload.queuedCount;

  return (
    <>
      <HealthIcon health={health} />
      <span className="min-w-0 truncate">
        <span className="text-foreground">{labelOf(health)}</span>
        {health !== "online" && lastSeen && <> · {lastSeen}</>}
        {!offline && active > 0 && (
          <>
            {" · "}
            {tAgents(($) => $.row.task_count, { count: active })}
          </>
        )}
      </span>
    </>
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

export function canReadRuntimeUsage(
  runtime: AgentRuntime,
  currentUserId: string | null,
): boolean {
  return isRuntimeUsableForUser(runtime, currentUserId);
}

export function CostCell({
  runtimeId,
  enabled,
}: {
  runtimeId: string;
  enabled: boolean;
}) {
  const { t, i18n } = useT("runtimes");
  const tz = useViewingTimezone();
  const locales = i18n.resolvedLanguage ?? i18n.language;
  const { data: usage = [] } = useQuery({
    ...runtimeUsageOptions(runtimeId, COST_CELL_DAYS, tz),
    enabled,
  });
  const cost7d = useMemo(() => computeCostInWindow(usage, 7, tz), [usage, tz]);
  const costPrev7d = useMemo(
    () => computeCostInWindow(usage, 7, tz, 7),
    [usage, tz],
  );
  const delta = pctChange(cost7d, costPrev7d);

  if (usage.length === 0) return <CollectionEmptyValue />;
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
    <span className="flex items-baseline gap-1.5">
      <CurrencyNumberFlow
        value={cost7d}
        locales={locales}
        aria-label={fmt}
        className="text-caption font-medium text-foreground"
      />
      {deltaLabel && (
        <span className={`text-micro tabular-nums ${deltaTone}`}>
          {deltaLabel}
        </span>
      )}
    </span>
  );
}

export function CliCell({ runtime }: { runtime: AgentRuntime }) {
  const { t } = useT("runtimes");
  const failure = customRuntimeRegistrationFailure(runtime);
  if (failure) {
    const command = pendingRuntimeCommandName(runtime);
    return (
      <TruncatedTooltip content={command ? `${command}: ${failure}` : failure}>
        <span className="truncate text-destructive">{failure}</span>
      </TruncatedTooltip>
    );
  }
  if (isPendingCustomRuntime(runtime)) {
    const command = pendingRuntimeCommandName(runtime);
    if (!command) {
      return <span className="truncate">{t(($) => $.list.pending_cli_unknown)}</span>;
    }
    return (
      <TruncatedTooltip content={command}>
        <span className="truncate font-mono">{command}</span>
      </TruncatedTooltip>
    );
  }

  if (runtime.runtime_mode === "cloud") return <CollectionEmptyValue />;
  const meta = runtime.metadata as Record<string, unknown> | null;
  // `version` is the agent's own underlying CLI tool version — distinct per
  // provider (e.g. "2.1.5 (Claude Code)", "codex-cli 0.118.0", "0.42.0").
  // The separate `cli_version` is the shared multica daemon CLI, identical
  // for every runtime on one machine; surfacing it here made all agents
  // show the same number (#3838). The daemon CLI version and its update
  // prompt belong to the machine — they live in the machine header, not on a
  // per-agent row.
  const version =
    meta && typeof meta.version === "string" ? meta.version : null;

  if (!version) return <CollectionEmptyValue />;

  return <span className="truncate font-mono">{version}</span>;
}

// Stacks up to 3 agent avatars, then a "+N" pill if more bind to this
// runtime. Each avatar uses the wrapping ActorAvatar so hover automatically
// surfaces AgentProfileCard.
function AgentStack({ agentIds }: { agentIds: string[] }) {
  if (agentIds.length === 0) return <CollectionEmptyValue />;
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
            size="sm"
            enableHoverCard
          />
        </span>
      ))}
      {extra > 0 && (
        <span className="inline-flex size-5 items-center justify-center rounded-full bg-muted text-micro font-medium text-muted-foreground ring-2 ring-background">
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
  detailHref,
}: {
  runtime: AgentRuntime;
  profile: RuntimeProfile | null;
  wsId: string;
  canDelete: boolean;
  /**
   * Detail destination for the row, omitted when the row has none — pending
   * custom runtimes are not navigable, mirroring the list's own row link.
   */
  detailHref?: string;
}) {
  const { t } = useT("runtimes");
  const { t: tCommon } = useT("common");
  const intentNavigate = useIntentNavigate();
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const isCustomRuntime = !!runtime.profile_id;
  // Delete is the row's only management action; if the row can't run it, drop
  // the kebab entirely so the column doesn't render a near-empty popover. We
  // used to also hide it for self-healing runtimes (live local daemon
  // re-registers within seconds), but MUL-3352 surfaced that owners read
  // a missing kebab as "I lost my permission" rather than "the daemon
  // would undo this". The dialog now carries the self-heal warning and
  // the user gets to decide.

  if (!canDelete) return null;

  return (
    <>
      <CollectionRowMenu label={t(($) => $.list.row_actions_aria)} contentClassName="w-40">
        {detailHref && (
          <>
            <DropdownMenuItem
              onClick={() => intentNavigate(detailHref, "foreground-tab")}
            >
              <ExternalLink aria-hidden="true" className="h-3.5 w-3.5" />
              {tCommon(($) => $.navigation.open_in_new_tab)}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
          </>
        )}
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
      </CollectionRowMenu>
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

// List

export function RuntimeList({
  runtimes,
  now,
  runtimeHref,
  machineTitle,
}: {
  runtimes: AgentRuntime[];
  now: number;
  /** Machine-detail pages keep runtime settings nested under the machine. */
  runtimeHref?: (runtimeId: string) => string;
  /**
   * The containing machine's title, when this list renders the runtimes of a
   * single machine. Used so a machine-level alias doesn't repeat on every row
   * while a per-runtime alias still shows (MUL-5248).
   */
  machineTitle?: string;
}) {
  const { t } = useT("runtimes");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
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

  const columns: CollectionColumn<RuntimeRow>[] = [
    {
      key: "name",
      header: t(($) => $.list.col_runtime),
      cell: (row) => <RuntimeNameCell runtime={row.runtime} machineTitle={machineTitle} />,
    },
    {
      // Health folds the workload in as a suffix ("Healthy · 2 tasks") —
      // same merge as the agents list's status cell.
      key: "health",
      header: t(($) => $.list.col_health),
      width: 176,
      core: true,
      cell: (row) => <HealthCell runtime={row.runtime} workload={row.workload} now={now} />,
    },
    // Owner only earns its space when the list has more than one owner.
    ...(showOwner
      ? [
          {
            key: "owner",
            header: t(($) => $.list.col_owner),
            width: 120,
            cell: (row: RuntimeRow) =>
              row.ownerMember ? (
                <>
                  <ActorAvatar actorType="member" actorId={row.ownerMember.user_id} size="sm" />
                  <span className="min-w-0 truncate">{row.ownerMember.name}</span>
                </>
              ) : (
                <CollectionEmptyValue />
              ),
          },
        ]
      : []),
    {
      key: "agents",
      header: t(($) => $.list.col_agents),
      width: 92,
      cell: (row) => <AgentStack agentIds={row.workload.agentIds} />,
    },
    {
      key: "cost",
      header: t(($) => $.list.col_cost),
      width: 96,
      align: "end",
      cell: (row) =>
        isPendingCustomRuntime(row.runtime) ? (
          <CollectionEmptyValue />
        ) : (
          <CostCell
            runtimeId={row.runtime.id}
            enabled={canReadRuntimeUsage(row.runtime, user?.id ?? null)}
          />
        ),
    },
    {
      key: "cli",
      header: t(($) => $.list.col_cli),
      width: 112,
      cell: (row) => <CliCell runtime={row.runtime} />,
    },
  ];

  const detailHrefOf = (row: RuntimeRow) =>
    isPendingCustomRuntime(row.runtime)
      ? undefined
      : (runtimeHref?.(row.runtime.id) ?? wsPaths.runtimeDetail(row.runtime.id));

  return (
    <CollectionList
      embedded
      label={t(($) => $.list.col_runtime)}
      columns={columns}
      rows={rows}
      getRowId={(row) => row.runtime.id}
      getRowHref={detailHrefOf}
      // The ⋯ slot only earns its width when some row can show the menu.
      rowMenu={
        rows.some((row) => row.canDelete)
          ? (row) => (
              <RuntimeRowMenu
                runtime={row.runtime}
                profile={row.profile}
                wsId={wsId}
                canDelete={row.canDelete}
                detailHref={detailHrefOf(row)}
              />
            )
          : undefined
      }
    />
  );
}
