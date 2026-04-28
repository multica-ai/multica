"use client";

import { useMemo, useRef, useState } from "react";
import {
  ArrowUpCircle,
  MoreHorizontal,
  Trash2,
  Cloud,
  Monitor,
} from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime, AgentTask, Agent, MemberWithUser } from "@multica/core/types";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { agentListOptions, memberListOptions } from "@multica/core/workspace/queries";
import {
  deriveRuntimeHealth,
  runtimeUsageOptions,
  latestCliVersionOptions,
} from "@multica/core/runtimes";
import { useDeleteRuntime } from "@multica/core/runtimes/mutations";
import { agentTaskSnapshotOptions } from "@multica/core/agents";
import { paths, useWorkspaceSlug } from "@multica/core/paths";
import { AppLink } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { Button } from "@multica/ui/components/ui/button";
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
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { useScrollFade } from "@multica/ui/hooks/use-scroll-fade";
import { HealthIcon, healthLabel } from "./shared";
import {
  formatLastSeen,
  computeCostInWindow,
  pctChange,
  isVersionNewer,
} from "../utils";

// ---------------------------------------------------------------------------
// Per-runtime workload snapshot — agent IDs serving this runtime (drives the
// avatar stack; .length doubles as the agent count) plus task counts split
// by status. Built once per render off the workspace-wide
// agents / agent-task-snapshot caches; filtered locally — zero extra requests.
// ---------------------------------------------------------------------------

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

function buildWorkloadIndex(
  agents: Agent[],
  tasks: AgentTask[],
): Map<string, RuntimeWorkload> {
  const result = new Map<string, RuntimeWorkload>();
  const agentToRuntime = new Map<string, string>();

  for (const a of agents) {
    if (!a.runtime_id) continue;
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
// Grid template. Two variants because the Owner column only earns its keep
// in workspaces with multiple runtime owners — single-owner workspaces just
// see an entire column of identical avatars, which is noise. We pick at
// list-mount time and pass the chosen template down so the header and every
// row stay byte-identically aligned.
//
// Leading-visual columns are dedicated tracks so header labels sit on the
// same x as the row text, with no per-cell padding hacks. Same pattern as
// Agents (avatar) and Skills (source icon).
//
// Width choices (left → right):
//   IconBox:    2rem            — 32px Cloud / Monitor mode badge
//   Runtime:    minmax(0,1fr)   — primary, fills remainder, truncates first
//   HealthIcon: 0.875rem        — 12px wifi-style icon
//   Health:     minmax(0,1fr)   — text label, can shrink + truncate at
//                                 narrow widths so it never overlaps the
//                                 Runtime column. ("Recently lost · 2m
//                                 14s ago" is the worst case; the long
//                                 suffix truncates first.)
//   Owner:      2rem            — single 18px avatar (multi-owner only)
//   Agents:     6rem            — 3 avatars overlapped + "+N" pill
//   Active:     5rem            — "2 +5q" worst case, right-aligned
//   Cost:       7rem            — "$879.00" + "↑18%" stacked, right-aligned
//   CLI:        8rem            — "Desktop · v0.2.17" worst case
//   Kebab:      2.5rem          — icon button
//
// Switched Health from a fixed 9.5rem to minmax(0,1fr) so it competes
// fairly with Runtime under width pressure. The previous fixed-width
// reservation guaranteed the worst-case copy fit, but at intermediate
// viewports the Runtime column was squeezed below its content width and
// overflowed visually.
// ---------------------------------------------------------------------------

const GRID_WITH_OWNER =
  "grid items-center gap-4 " +
  "grid-cols-[2rem_minmax(0,1fr)_0.875rem_minmax(0,1fr)_2rem_2.5rem] " +
  "md:grid-cols-[2rem_minmax(0,1fr)_0.875rem_minmax(0,1fr)_2rem_6rem_5rem_7rem_2.5rem] " +
  "lg:grid-cols-[2rem_minmax(0,1.2fr)_0.875rem_minmax(0,1fr)_2rem_6rem_5rem_7rem_8rem_2.5rem]";

const GRID_NO_OWNER =
  "grid items-center gap-4 " +
  "grid-cols-[2rem_minmax(0,1fr)_0.875rem_minmax(0,1fr)_2.5rem] " +
  "md:grid-cols-[2rem_minmax(0,1fr)_0.875rem_minmax(0,1fr)_6rem_5rem_7rem_2.5rem] " +
  "lg:grid-cols-[2rem_minmax(0,1.2fr)_0.875rem_minmax(0,1fr)_6rem_5rem_7rem_8rem_2.5rem]";

export function RuntimeList({
  runtimes,
  updatableIds,
  now,
}: {
  runtimes: AgentRuntime[];
  // Kept on the API surface for callers (currently a noop here — the CLI
  // column re-derives from `metadata.cli_version` + the GitHub-release query
  // so per-row state is co-located. Removing it would break the page-level
  // wrapper that still computes the set; left in place to avoid scope creep.)
  updatableIds?: Set<string>;
  now: number;
}) {
  // Kept to avoid an unused-prop lint; the data path is now per-row.
  void updatableIds;

  const wsId = useWorkspaceId();
  const slug = useWorkspaceSlug();
  const user = useAuthStore((s) => s.user);
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const currentMember = user
    ? members.find((m) => m.user_id === user.id)
    : null;
  const isAdmin = currentMember
    ? currentMember.role === "owner" || currentMember.role === "admin"
    : false;
  // Snapshot also includes each agent's latest terminal task; the workload
  // index iterates by status and only counts active ones, so terminal rows
  // are silently ignored.
  const { data: snapshot = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const { data: latestCliVersion = null } = useQuery(latestCliVersionOptions());

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
  // distinct owners; otherwise it's a column of identical avatars.
  const showOwner = useMemo(() => {
    const owners = new Set<string>();
    for (const r of runtimes) {
      if (r.owner_id) owners.add(r.owner_id);
    }
    return owners.size > 1;
  }, [runtimes]);

  const gridClass = showOwner ? GRID_WITH_OWNER : GRID_NO_OWNER;

  const scrollRef = useRef<HTMLDivElement | null>(null);
  const fadeStyle = useScrollFade(scrollRef);

  return (
    <div
      ref={scrollRef}
      style={fadeStyle}
      className="flex-1 min-h-0 overflow-y-auto"
    >
      <div
        role="row"
        className={`${gridClass} sticky top-0 z-10 border-b bg-muted/30 px-4 py-2 text-xs font-medium uppercase tracking-wider text-muted-foreground backdrop-blur`}
      >
        {/* Icon-box leading slot — empty in header so "Runtime" aligns with
            the row's name text rather than the mode-badge edge. */}
        <span aria-hidden />
        <span>Runtime</span>
        {/* Health-dot leading slot — empty in header so "Health" aligns
            with the row's status text rather than the dot edge. */}
        <span aria-hidden />
        <span>Health</span>
        {showOwner && <span className="hidden md:block">Owner</span>}
        <span className="hidden md:block">Agents</span>
        <span className="hidden text-right md:block">Active</span>
        <span className="hidden text-right md:block">Cost · 7d</span>
        <span className="hidden lg:block">CLI</span>
        <span aria-label="Actions" />
      </div>
      {runtimes.map((runtime) => (
        <RuntimeRow
          key={runtime.id}
          runtime={runtime}
          now={now}
          gridClass={gridClass}
          showOwner={showOwner}
          workload={workloadIndex.get(runtime.id) ?? EMPTY_WORKLOAD}
          ownerMember={
            runtime.owner_id ? memberById.get(runtime.owner_id) ?? null : null
          }
          latestCliVersion={latestCliVersion}
          href={slug ? paths.workspace(slug).runtimeDetail(runtime.id) : "#"}
          wsId={wsId}
          canDelete={
            isAdmin || (!!user && runtime.owner_id === user.id)
          }
        />
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Backend formats `runtime.name` as `"<base> (<hostname>)"` (e.g.
// "Claude (qingnaiyuandebijibendiannao-8.local)"). Every runtime on the same
// machine repeats the same hostname suffix, so it carries near-zero scan
// value once the eye has seen it on the first row — yet it dominates the
// column width by character count. Split it so the base name stays
// emphasised and the hostname renders muted + truncates first when the cell
// is tight. Falls back to the raw name when the format doesn't match.
// ---------------------------------------------------------------------------

function splitRuntimeName(name: string): {
  base: string;
  hostname: string | null;
} {
  const m = name.match(/^(.+?)\s+\(([^)]+)\)$/);
  if (!m || !m[1] || !m[2]) return { base: name, hostname: null };
  return { base: m[1], hostname: m[2] };
}

function RuntimeRow({
  runtime,
  now,
  gridClass,
  showOwner,
  workload,
  ownerMember,
  latestCliVersion,
  href,
  wsId,
  canDelete,
}: {
  runtime: AgentRuntime;
  now: number;
  gridClass: string;
  showOwner: boolean;
  workload: RuntimeWorkload;
  ownerMember: MemberWithUser | null;
  latestCliVersion: string | null;
  href: string;
  wsId: string;
  canDelete: boolean;
}) {
  const health = deriveRuntimeHealth(runtime, now);
  const lastSeen = formatLastSeen(runtime.last_seen_at);
  const { base: baseName, hostname } = splitRuntimeName(runtime.name);
  const isOffline = health === "offline" || health === "about_to_gc";

  // Per-row cost fetch reuses the SAME cache key as the runtime-detail page
  // (`runtimeUsageOptions(rid, 180)`). On cold load that's N parallel
  // requests, but every one of them populates the same cache the detail
  // page reads — so clicking a row to drill in is instant.
  const { data: usage = [] } = useQuery(runtimeUsageOptions(runtime.id, 180));

  const cost7d = useMemo(() => computeCostInWindow(usage, 7), [usage]);
  const costPrev7d = useMemo(() => computeCostInWindow(usage, 7, 7), [usage]);
  const costDelta = pctChange(cost7d, costPrev7d);

  return (
    <AppLink
      href={href}
      className={`${gridClass} group border-b px-4 py-3 text-sm transition-colors last:border-b-0 hover:bg-accent/40 focus-visible:bg-accent/40 focus-visible:outline-none`}
    >
      {/* MODE BADGE — leading column so the "Runtime" header aligns with the
          name text rather than the badge edge. */}
      <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-md border bg-card">
        {runtime.runtime_mode === "cloud" ? (
          <Cloud className="h-4 w-4 text-muted-foreground" />
        ) : (
          <Monitor className="h-4 w-4 text-muted-foreground" />
        )}
      </div>

      {/* RUNTIME — base name + (hostname). Both can truncate at narrow
          widths so the cell never overflows into the adjacent Health
          column. The base name takes precedence (truncate later) by
          virtue of being the first child; the hostname (more disposable)
          shrinks first. Provider lives in the right-side CLI column. */}
      <div className="flex min-w-0 items-center gap-1.5">
        <span className="min-w-0 truncate text-sm font-medium">{baseName}</span>
        {hostname && (
          <span className="min-w-0 truncate text-xs text-muted-foreground/70">
            ({hostname})
          </span>
        )}
      </div>

      {/* HEALTH icon — wifi-style indicator carries both shape (Wifi / WifiOff)
          and colour (success / warning / muted / destructive) for the four
          health buckets. Leading column for the same alignment reason as
          the icon-box on the left. */}
      <HealthIcon health={health} />

      {/* HEALTH label — last_seen folded in only when it carries information
          (everything except the always-"Just now" online case). */}
      <span className="min-w-0 truncate text-sm">
        {healthLabel(health)}
        {health !== "online" && runtime.last_seen_at && (
          <span className="text-muted-foreground"> · {lastSeen}</span>
        )}
      </span>

      {/* OWNER — small avatar only; name lives in the hover card. Conditional
          on the workspace having more than one distinct owner. */}
      {showOwner && (
        <div className="hidden md:flex">
          {ownerMember ? (
            <ActorAvatar
              actorType="member"
              actorId={ownerMember.user_id}
              size={18}
            />
          ) : (
            <span className="text-xs text-muted-foreground/50">—</span>
          )}
        </div>
      )}

      {/* AGENTS — avatar stack (3 + "+N"). */}
      <div className="hidden md:block">
        <AgentStack agentIds={workload.agentIds} />
      </div>

      {/* ACTIVE — running count is the headline (brand colour) when active;
          queued count piggy-backs as a smaller "+Nq" suffix when non-zero.
          Offline runtimes show "—" instead of "0" because zero on an offline
          row is a tautology, not information. */}
      <div className="hidden text-right md:block">
        <ActiveCell
          running={workload.runningCount}
          queued={workload.queuedCount}
          offline={isOffline}
        />
      </div>

      {/* COST · 7D — main number + trend delta stacked. The 7d window is
          fixed (vs the period selector on the detail page) so the column
          stays comparable across rows. */}
      <div className="hidden text-right md:block">
        <CostCell cost={cost7d} delta={costDelta} hasData={usage.length > 0} />
      </div>

      {/* CLI — source badge + version + ↑ marker if behind. Pure status,
          no clickable upgrade button — the actual upgrade lives on the
          detail page where polling/error state has room to breathe. */}
      <div className="hidden lg:block">
        <CliCell runtime={runtime} latestCliVersion={latestCliVersion} />
      </div>

      {/* ⋯ menu */}
      <div className="flex justify-end">
        <RowMenu runtime={runtime} wsId={wsId} canDelete={canDelete} />
      </div>
    </AppLink>
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
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  if (running === 0 && queued === 0) {
    return <span className="text-sm text-muted-foreground">0</span>;
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
      {queued > 0 && (
        <span className="text-xs text-warning">+{queued}q</span>
      )}
    </div>
  );
}

function CostCell({
  cost,
  delta,
  hasData,
}: {
  cost: number;
  delta: number | null;
  hasData: boolean;
}) {
  if (!hasData) {
    return <span className="text-xs text-muted-foreground/50">—</span>;
  }
  const fmt = cost >= 100 ? `$${cost.toFixed(0)}` : `$${cost.toFixed(2)}`;
  // Tone choice: rising cost is a "watch out" signal (warning), falling is
  // good news (success), no comparable prior is silent.
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
  // Electron app ships and replaces the binary), so the "↑" upgrade marker
  // would be a lie — suppress it regardless of version comparison.
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
        className={`min-w-0 truncate font-mono ${
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
// surfaces AgentProfileCard — same hover affordance Agents/Skills already
// use, no per-row extra wiring.
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
          <ActorAvatar actorType="agent" actorId={id} size={22} enableHoverCard />
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
    // Keep the grid cell occupied so column alignment doesn't shift between
    // rows the user can act on and rows they can't.
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
          // Prevent the row's anchor navigation from firing if a click on a
          // menu item somehow bubbles back through the portal.
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
              Are you sure you want to delete &ldquo;{runtime.name}&rdquo;? This action cannot be undone.
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
