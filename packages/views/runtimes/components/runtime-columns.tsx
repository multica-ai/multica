"use client";

import { useMemo, useState } from "react";
import {
  ArrowUpCircle,
  Cloud,
  Monitor,
  MoreHorizontal,
  Trash2,
} from "lucide-react";
import { toast } from "sonner";
import type { ColumnDef } from "@tanstack/react-table";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime, MemberWithUser } from "@multica/core/types";
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
import { HealthIcon, healthLabel } from "./shared";
import {
  computeCostInWindow,
  formatLastSeen,
  isVersionNewer,
  pctChange,
} from "../utils";

// Per-row data assembled at the page level. The columns reach into
// `row.original` and never pull their own data — except for the per-runtime
// usage query in CostCell, which intentionally keeps its 180-day cache key
// shared with the runtime-detail page (clicking a row pre-warms detail).
export interface RuntimeRow {
  runtime: AgentRuntime;
  ownerMember: MemberWithUser | null;
  workload: { agentIds: string[]; runningCount: number; queuedCount: number };
  canDelete: boolean;
}

const COL_WIDTHS = {
  runtime: 280,
  health: 200,
  owner: 60,
  agents: 120,
  active: 100,
  cost: 120,
  cli: 160,
  actions: 48,
} as const;

interface CreateColumnsArgs {
  showOwner: boolean;
  latestCliVersion: string | null;
  wsId: string;
  now: number;
}

export function createRuntimeColumns({
  showOwner,
  latestCliVersion,
  wsId,
  now,
}: CreateColumnsArgs): ColumnDef<RuntimeRow>[] {
  const cols: ColumnDef<RuntimeRow>[] = [
    {
      id: "runtime",
      header: "Runtime",
      size: COL_WIDTHS.runtime,
      cell: ({ row }) => <RuntimeNameCell runtime={row.original.runtime} />,
    },
    {
      id: "health",
      header: "Health",
      size: COL_WIDTHS.health,
      cell: ({ row }) => (
        <HealthCell runtime={row.original.runtime} now={now} />
      ),
    },
  ];

  if (showOwner) {
    cols.push({
      id: "owner",
      header: "Owner",
      size: COL_WIDTHS.owner,
      cell: ({ row }) =>
        row.original.ownerMember ? (
          <ActorAvatar
            actorType="member"
            actorId={row.original.ownerMember.user_id}
            size={18}
          />
        ) : (
          <span className="text-xs text-muted-foreground/50">—</span>
        ),
    });
  }

  cols.push(
    {
      id: "agents",
      header: "Agents",
      size: COL_WIDTHS.agents,
      cell: ({ row }) => (
        <AgentStack agentIds={row.original.workload.agentIds} />
      ),
    },
    {
      id: "active",
      header: () => <div className="text-right">Active</div>,
      size: COL_WIDTHS.active,
      cell: ({ row }) => {
        const health = deriveRuntimeHealth(row.original.runtime, now);
        const offline = health === "offline" || health === "about_to_gc";
        return (
          <ActiveCell
            running={row.original.workload.runningCount}
            queued={row.original.workload.queuedCount}
            offline={offline}
          />
        );
      },
    },
    {
      id: "cost",
      header: () => <div className="text-right">Cost · 7d</div>,
      size: COL_WIDTHS.cost,
      cell: ({ row }) => <CostCell runtimeId={row.original.runtime.id} />,
    },
    {
      id: "cli",
      header: "CLI",
      size: COL_WIDTHS.cli,
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
    <div className="flex items-center gap-2">
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border bg-card">
        {runtime.runtime_mode === "cloud" ? (
          <Cloud className="h-4 w-4 text-muted-foreground" />
        ) : (
          <Monitor className="h-4 w-4 text-muted-foreground" />
        )}
      </div>
      <div className="flex min-w-0 items-center gap-1.5">
        <span className="truncate text-sm font-medium">{baseName}</span>
        {hostname && (
          <Tooltip>
            <TooltipTrigger
              render={
                <span className="truncate text-xs text-muted-foreground/70">
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
  const health = deriveRuntimeHealth(runtime, now);
  const lastSeen = formatLastSeen(runtime.last_seen_at);
  return (
    <div className="flex items-center gap-1.5">
      <HealthIcon health={health} />
      <span className="truncate text-sm">
        {healthLabel(health)}
        {health !== "online" && runtime.last_seen_at && (
          <span className="text-muted-foreground"> · {lastSeen}</span>
        )}
      </span>
    </div>
  );
}

function ActiveCell({
  running,
  queued,
  offline,
}: {
  running: number;
  queued: number;
  offline: boolean;
}) {
  if (offline) {
    return (
      <div className="text-right">
        <span className="text-xs text-muted-foreground/50">—</span>
      </div>
    );
  }
  if (running === 0 && queued === 0) {
    return (
      <div className="text-right">
        <span className="text-sm text-muted-foreground">0</span>
      </div>
    );
  }
  return (
    <div className="flex items-center justify-end gap-1.5 text-sm tabular-nums">
      {running > 0 ? (
        <>
          <span className="h-1.5 w-1.5 rounded-full bg-brand" />
          <span className="text-brand">{running}</span>
        </>
      ) : (
        <span className="text-muted-foreground">0</span>
      )}
      {queued > 0 && <span className="text-xs text-warning">+{queued}q</span>}
    </div>
  );
}

// Per-row cost — fetches the same 180-day usage window used by the
// runtime-detail page so clicking a row makes detail render instantly from
// cache. Cold load incurs N parallel requests, but each is cheap and they
// all share the same query key.
function CostCell({ runtimeId }: { runtimeId: string }) {
  const { data: usage = [] } = useQuery(runtimeUsageOptions(runtimeId, 180));
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
        ? "flat"
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
          Desktop
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
                aria-label="Update available"
              />
            }
          />
          <TooltipContent>
            Update available: {latestCliVersion}
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
  const deleteMutation = useDeleteRuntime(wsId);
  const [deleteOpen, setDeleteOpen] = useState(false);

  if (!canDelete) {
    return <span aria-hidden />;
  }

  const handleDelete = () => {
    deleteMutation.mutate(runtime.id, {
      onSuccess: () => {
        toast.success("Runtime deleted");
        setDeleteOpen(false);
      },
      onError: (e) => {
        toast.error(
          e instanceof Error ? e.message : "Failed to delete runtime",
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
              aria-label="Row actions"
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
          >
            <Trash2 className="h-3.5 w-3.5" />
            Delete
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
            <AlertDialogTitle>Delete Runtime</AlertDialogTitle>
            <AlertDialogDescription>
              Are you sure you want to delete &ldquo;{runtime.name}&rdquo;?
              This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={handleDelete}
              disabled={deleteMutation.isPending}
            >
              {deleteMutation.isPending ? "Deleting..." : "Delete"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </>
  );
}
