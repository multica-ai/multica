"use client";

import { useMemo, useState } from "react";
import {
  ArrowUpCircle,
  MoreHorizontal,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import type { ColumnDef } from "@tanstack/react-table";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime, MemberWithUser } from "@multica/core/types";
import { deriveWorkload } from "@multica/core/agents";
import {
  deriveRuntimeHealth,
  runtimeUsageOptions,
} from "@multica/core/runtimes";
import { useDeleteRuntime } from "@multica/core/runtimes/mutations";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "../../common/actor-avatar";
import { workloadConfig } from "../../agents/presence";
import { ProviderLogo } from "./provider-logo";
import { HealthIcon, useHealthLabel } from "./shared";
import {
  computeCostInWindow,
  formatLastSeen,
  isVersionNewer,
  pctChange,
} from "../utils";
import { useT } from "../../i18n";

// Per-row data assembled at the page level. The columns reach into
// `row.original` and never pull their own data — except for the per-runtime
// usage query in CostCell, which fetches its own narrow 14-day window
// (just enough for the cell's 7d cost + 7d prior-window delta).
export interface RuntimeRow {
  runtime: AgentRuntime;
  ownerMember: MemberWithUser | null;
  workload: { agentIds: string[]; runningCount: number; queuedCount: number };
  canDelete: boolean;
}

// Column widths in px. Runtime, Health, and CLI grow together until the
// user resizes them. Their `size` values still flow into table.getTotalSize()
// to set the table's min-width, giving each grow column a real floor below
// which the container scrolls horizontally instead of shrinking further.
const COL_WIDTHS = {
  runtime: 240,
  health: 200,
  owner: 60,
  agents: 100,
  workload: 140,
  cost: 100,
  cli: 140,
  // 60 = 16 left padding + 28 kebab + 16 right padding. Keeps the
  // kebab's right edge 16px from the card so it lines up with the
  // toolbar's px-4 right inset.
  actions: 60,
} as const;

type RuntimesT = ReturnType<typeof useT<"runtimes">>["t"];

interface CreateColumnsArgs {
  showOwner: boolean;
  latestCliVersion: string | null;
  wsId: string;
  now: number;
  t: RuntimesT;
}

export function createRuntimeColumns({
  showOwner,
  latestCliVersion,
  wsId,
  now,
  t,
}: CreateColumnsArgs): ColumnDef<RuntimeRow>[] {
  const cols: ColumnDef<RuntimeRow>[] = [
    {
      id: "runtime",
      header: () => t(($) => $.list.col_runtime),
      size: COL_WIDTHS.runtime,
      meta: { grow: true },
      cell: ({ row }) => <RuntimeNameCell runtime={row.original.runtime} />,
    },
    {
      id: "health",
      header: () => t(($) => $.list.col_health),
      size: COL_WIDTHS.health,
      meta: { grow: true },
      cell: ({ row }) => (
        <HealthCell runtime={row.original.runtime} now={now} />
      ),
    },
  ];

  if (showOwner) {
    cols.push({
      id: "owner",
      header: () => t(($) => $.list.col_owner),
      size: COL_WIDTHS.owner,
      cell: ({ row }) =>
        row.original.ownerMember ? (
          <span className="inline-flex min-w-0 items-center gap-1.5">
            <ActorAvatar
              actorType="member"
              actorId={row.original.ownerMember.user_id}
              size={18}
            />
            <span className="truncate text-xs text-muted-foreground">
              {row.original.ownerMember.name}
            </span>
          </span>
        ) : (
          <span className="text-xs text-muted-foreground/50">—</span>
        ),
    });
  }

  cols.push(
    {
      id: "agents",
      header: () => t(($) => $.list.col_agents),
      size: COL_WIDTHS.agents,
      cell: ({ row }) => (
        <AgentStack agentIds={row.original.workload.agentIds} />
      ),
    },
    {
      id: "workload",
      header: () => t(($) => $.list.col_workload),
      size: COL_WIDTHS.workload,
      cell: ({ row }) => {
        const health = deriveRuntimeHealth(row.original.runtime, now);
        const offline = health === "offline" || health === "about_to_gc";
        return (
          <WorkloadCell
            running={row.original.workload.runningCount}
            queued={row.original.workload.queuedCount}
            offline={offline}
          />
        );
      },
    },
    {
      id: "cost",
      header: () => <div className="text-right">{t(($) => $.list.col_cost)}</div>,
      size: COL_WIDTHS.cost,
      cell: ({ row }) => <CostCell runtimeId={row.original.runtime.id} />,
    },
    {
      id: "cli",
      header: () => t(($) => $.list.col_cli),
      size: COL_WIDTHS.cli,
      meta: { grow: true },
      cell: ({ row }) => (
        <CliCell
          runtime={row.original.runtime}
          latestCliVersion={latestCliVersion}
        />
      ),
    },
    {
      id: "actions",
      header: () => null,
      size: COL_WIDTHS.actions,
      enableResizing: false,
      cell: ({ row }) => (
        <div
          className="flex justify-end"
          onClick={(e) => e.stopPropagation()}
        >
          <RowMenu
            runtime={row.original.runtime}
            wsId={wsId}
            canDelete={row.original.canDelete}
          />
        </div>
      ),
    },
  );

  return cols;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// Backend formats `runtime.name` as `"<base> (<hostname>)"`. Every runtime on
// the same machine repeats the hostname suffix, so it dominates column width
// while carrying near-zero scan value once seen on the first row. Split it
// so the base name stays emphasised and the hostname renders muted.
export function splitRuntimeName(name: string): {
  base: string;
  hostname: string | null;
} {
  const m = name.match(/^(.+?)\s+\(([^)]+)\)$/);
  if (!m || !m[1] || !m[2]) return { base: name, hostname: null };
  return { base: m[1], hostname: m[2] };
}

// ---------------------------------------------------------------------------
// Cell renderers
// ---------------------------------------------------------------------------

function RuntimeNameCell({ runtime }: { runtime: AgentRuntime }) {
  const { base: baseName, hostname } = splitRuntimeName(runtime.name);
  return (
    <div className="flex min-w-0 items-center gap-2">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center">
        <ProviderLogo provider={runtime.provider} className="h-5 w-5" />
      </div>
      <div className="flex min-w-0 flex-1 items-center gap-1.5">
        <span className="block min-w-0 truncate text-sm font-medium">
          {baseName}
        </span>
        {hostname && (
          <Tooltip>
            <TooltipTrigger
              render={
                <span className="block min-w-0 truncate text-xs text-muted-foreground/70">
                  ({hostname})
                </span>
              }
            />
            <TooltipContent>{hostname}</TooltipContent>
          </Tooltip>
        )}
      </div>
    </div>
  );
}

function HealthCell({
  runtime,
  now,
}: {
  runtime: AgentRuntime;
  now: number;
}) {
  const labelOf = useHealthLabel();
  const health = deriveRuntimeHealth(runtime, now);
  const lastSeen = formatLastSeen(runtime.last_seen_at);
  return (
    <div className="flex min-w-0 items-center gap-1.5">
      <HealthIcon health={health} />
      <span className="block min-w-0 truncate text-sm">
        {labelOf(health)}
        {health !== "online" && runtime.last_seen_at && (
          <span className="text-muted-foreground"> · {lastSeen}</span>
        )}
      </span>
    </div>
  );
}

// Mirrors AgentPresenceIndicator's workload chip — same workloadConfig
// vocabulary applied to runtime-level aggregated counts. Offline runtime
// rows still render `—` (the runtime's Health column already says it
// all; redundant Idle here would just be noise). Online idle runtimes
// show "Idle" explicitly to match the agent-side three-state symmetry.
function WorkloadCell({
  running,
  queued,
  offline,
}: {
  running: number;
  queued: number;
  offline: boolean;
}) {
  const { t: tAgents } = useT("agents");
  if (offline) {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  const workload = deriveWorkload({
    runningCount: running,
    queuedCount: queued,
  });
  const wl = workloadConfig[workload];
  const counts =
    workload === "working"
      ? queued > 0
        ? `${running} +${queued}q`
        : `${running}`
      : workload === "queued"
        ? `${queued}`
        : null;
  return (
    <span className="inline-flex items-center gap-1 text-xs">
      {workload !== "idle" && (
        <wl.icon
          className={`h-3 w-3 shrink-0 ${wl.textClass} ${workload === "working" ? "animate-spin" : ""}`}
        />
      )}
      <span className={`shrink-0 ${wl.textClass}`}>{tAgents(($) => $.workload[workload])}</span>
      {counts && (
        <span className="truncate font-mono tabular-nums text-muted-foreground">
          {counts}
        </span>
      )}
    </span>
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

function CostCell({ runtimeId }: { runtimeId: string }) {
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
      <div className="text-right">
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
    <div className="flex flex-col items-end leading-tight">
      <span className="text-sm font-medium tabular-nums">{fmt}</span>
      {deltaLabel && (
        <span className={`text-[11px] tabular-nums ${deltaTone}`}>
          {deltaLabel}
        </span>
      )}
    </div>
  );
}

function CliCell({
  runtime,
  latestCliVersion,
}: {
  runtime: AgentRuntime;
  latestCliVersion: string | null;
}) {
  const { t } = useT("runtimes");
  if (runtime.runtime_mode === "cloud") {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  const meta = runtime.metadata as Record<string, unknown> | null;
  const cliVersion =
    meta && typeof meta.cli_version === "string" ? meta.cli_version : null;
  const launchedBy =
    meta && typeof meta.launched_by === "string" ? meta.launched_by : null;
  const isManaged = launchedBy === "desktop";

  if (!cliVersion) {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }

  // Desktop-managed daemons can never self-update from this page (the
  // Electron app ships and replaces the binary), so the upgrade marker
  // would lie — suppress regardless of version comparison.
  const hasUpdate =
    !isManaged &&
    !!latestCliVersion &&
    isVersionNewer(latestCliVersion, cliVersion);

  return (
    <div className="flex min-w-0 items-center gap-1 text-xs">
      {isManaged && (
        <span className="shrink-0 rounded-sm bg-muted px-1 py-0.5 text-[10px] font-medium text-muted-foreground">
          {t(($) => $.list.cli_managed_badge)}
        </span>
      )}
      <span
        className={`truncate font-mono ${
          hasUpdate ? "text-warning" : "text-muted-foreground"
        }`}
      >
        {cliVersion}
      </span>
      {hasUpdate && latestCliVersion && (
        <Tooltip>
          <TooltipTrigger
            render={
              <ArrowUpCircle
                className="h-3 w-3 shrink-0 text-warning"
                aria-label={t(($) => $.list.cli_update_available_aria)}
              />
            }
          />
          <TooltipContent>
            {t(($) => $.list.cli_update_available_tooltip, { version: latestCliVersion })}
          </TooltipContent>
        </Tooltip>
      )}
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

function RowMenu({
  runtime,
  wsId,
  canDelete,
}: {
  runtime: AgentRuntime;
  wsId: string;
  canDelete: boolean;
}) {
  const { t } = useT("runtimes");
  const deleteMutation = useDeleteRuntime(wsId);
  const [deleteOpen, setDeleteOpen] = useState(false);

  if (!canDelete) {
    return <span aria-hidden />;
  }

  const handleDelete = () => {
    deleteMutation.mutate(runtime.id, {
      onSuccess: () => {
        toast.success(t(($) => $.detail.toast_deleted));
        setDeleteOpen(false);
      },
      onError: (e) => {
        toast.error(
          e instanceof Error ? e.message : t(($) => $.detail.toast_delete_failed),
        );
      },
    });
  };

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
      <AlertDialog
        open={deleteOpen}
        onOpenChange={(v) => {
          if (deleteMutation.isPending) return;
          setDeleteOpen(v);
        }}
      >
        <AlertDialogContent onClick={(e) => e.stopPropagation()}>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.detail.delete_dialog.title)}</AlertDialogTitle>
            <AlertDialogDescription>
              {t(($) => $.detail.delete_dialog.description, { name: runtime.name })}
              <span className="mt-2 block text-xs text-muted-foreground/80">
                {t(($) => $.list.delete_admin_hint)}
              </span>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t(($) => $.detail.delete_dialog.cancel)}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? t(($) => $.detail.delete_dialog.deleting) : t(($) => $.detail.delete_dialog.confirm)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
