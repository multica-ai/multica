"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  AlertCircle,
  ArrowLeft,
  ArrowUpDown,
  Bot,
  Plus,
  Search,
} from "lucide-react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { getCoreRowModel, useReactTable } from "@tanstack/react-table";
import type { Agent, AgentRuntime, CreateAgentRequest } from "@multica/core/types";
import {
  type AgentAvailability,
  type LastTaskState,
  agentRunCounts30dOptions,
  summarizeActivityWindow,
  useWorkspaceActivityMap,
  useWorkspacePresenceMap,
} from "@multica/core/agents";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { runtimeListOptions } from "@multica/core/runtimes";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { DataTable } from "@multica/ui/components/ui/data-table";
import { useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import {
  availabilityConfig,
  availabilityOrder,
  lastTaskOrder,
  taskStateConfig,
} from "../presence";
import { CreateAgentDialog } from "./create-agent-dialog";
import { type AgentRow, createAgentColumns } from "./agent-columns";

// Filter axes layered top → bottom by frequency:
//
//   View         = which dataset are we looking at (Active vs Archived).
//                  Archived is low-frequency, so it is NOT a top-level
//                  segment — it is a ghost link in the toolbar.
//   Scope        = ownership lens (All vs Mine). Layer-1 segment.
//   Availability = "Can it take work?" — 3-state chip group.
//   Last task    = "What was the last thing it did?" — 5-state chip group.
//
// Availability and Last task are independent axes (Option B). Filter is
// the intersection: "online + last failed" is a meaningful combination
// (find broken-but-alive agents). Counts on each chip reflect "if I
// selected this chip on this axis (with the other axis's current
// selection), this many agents would match".
type View = "active" | "archived";
type Scope = "all" | "mine";
type AvailabilityFilter = "all" | AgentAvailability;
type LastTaskFilter = "all" | LastTaskState;

const AVAILABILITY_DESCRIPTION: Record<AgentAvailability, string> = {
  online: "Runtime online — agent ready to take work",
  unstable:
    "Runtime just dropped (< 5 min) — queued work is paused, system is auto-retrying",
  offline: "Runtime unreachable",
};

const LAST_TASK_DESCRIPTION: Record<LastTaskState, string> = {
  running: "At least one task running or queued right now",
  completed: "Most recent task completed successfully",
  failed: "Most recent task failed — needs attention",
  cancelled: "Most recent task was cancelled",
  idle: "No task history yet",
};

type SortKey = "recent" | "name" | "runs" | "created";
const SORT_KEYS: SortKey[] = ["recent", "name", "runs", "created"];
const SORT_LABEL: Record<SortKey, string> = {
  recent: "Recent activity",
  name: "Name",
  runs: "Most runs",
  created: "Recently created",
};

export function AgentsPage() {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const qc = useQueryClient();
  const currentUser = useAuthStore((s) => s.user);

  const {
    data: agents = [],
    isLoading,
    error: listError,
    refetch: refetchList,
  } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: runCountsRaw = [] } = useQuery(agentRunCounts30dOptions(wsId));

  // Single source of truth for derived agent state. The hook owns the
  // 30s tick + the runtime/null/task orchestration; the page only reads
  // the resulting Maps. Replaces the 24-line useMemo presenceMap +
  // 12-line activityMap that lived here previously.
  const { byAgent: presenceMap } = useWorkspacePresenceMap(wsId);
  const { byAgent: activityMap } = useWorkspaceActivityMap(wsId);

  const [view, setView] = useState<View>("active");
  // Default to "mine" — matches runtimes page convention and the visual
  // ordering (Mine first). All is one click away when users want the
  // workspace-wide view.
  const [scope, setScope] = useState<Scope>("mine");
  const [availabilityFilter, setAvailabilityFilter] =
    useState<AvailabilityFilter>("all");
  const [lastTaskFilter, setLastTaskFilter] = useState<LastTaskFilter>("all");
  const [sort, setSort] = useState<SortKey>("recent");
  const [search, setSearch] = useState("");
  const [showCreate, setShowCreate] = useState(false);
  // When set, the Create dialog opens pre-populated with this agent's
  // config — driven by the row-level "Duplicate" action. We keep this
  // separate from `showCreate` so a stray null-template doesn't open the
  // dialog: the dialog opens iff `showCreate || duplicateTemplate`.
  const [duplicateTemplate, setDuplicateTemplate] = useState<Agent | null>(
    null,
  );

  const runtimesById = useMemo(() => {
    const m = new Map<string, AgentRuntime>();
    for (const r of runtimes) m.set(r.id, r);
    return m;
  }, [runtimes]);

  const runCountsById = useMemo(() => {
    const m = new Map<string, number>();
    for (const r of runCountsRaw) m.set(r.agent_id, r.run_count);
    return m;
  }, [runCountsRaw]);

  // Workspace role of the current user, used to gate row-level "manage"
  // operations (archive / cancel-tasks). Mirrors the back-end's
  // canManageAgent rule: workspace owner/admin OR the agent's owner.
  const myRole = useMemo(() => {
    if (!currentUser) return null;
    return members.find((m) => m.user_id === currentUser.id)?.role ?? null;
  }, [members, currentUser]);
  const isWorkspaceAdmin = myRole === "owner" || myRole === "admin";

  // Layer 1a — view (active / archived).
  const inView = useMemo(
    () =>
      agents.filter((a) =>
        view === "archived" ? !!a.archived_at : !a.archived_at,
      ),
    [agents, view],
  );

  // Layer 1b — ownership scope. Counts shown on the segment are
  // computed against the inView set so the numbers always reflect
  // "what would I see if I clicked this".
  const scopeCounts = useMemo(() => {
    let mine = 0;
    if (currentUser) {
      for (const a of inView) {
        if (a.owner_id === currentUser.id) mine += 1;
      }
    }
    return { all: inView.length, mine };
  }, [inView, currentUser]);

  const inScope = useMemo(() => {
    if (scope === "all" || !currentUser) return inView;
    return inView.filter((a) => a.owner_id === currentUser.id);
  }, [inView, scope, currentUser]);

  // Layer 2 — chip counts on each axis. Counts cross-filter against the
  // OTHER axis so the displayed number is "if I clicked this chip with
  // the other axis as-is, this many agents would match". Stable mental
  // model: numbers don't dance unless the user actually changes scope.
  const availabilityCounts = useMemo(() => {
    const counts: Record<AgentAvailability, number> = {
      online: 0,
      unstable: 0,
      offline: 0,
    };
    let total = 0;
    for (const a of inScope) {
      const detail = presenceMap.get(a.id);
      if (!detail) continue;
      if (lastTaskFilter !== "all" && detail.lastTask !== lastTaskFilter) {
        continue;
      }
      counts[detail.availability] += 1;
      total += 1;
    }
    return { counts, total };
  }, [inScope, presenceMap, lastTaskFilter]);

  const lastTaskCounts = useMemo(() => {
    const counts: Record<LastTaskState, number> = {
      running: 0,
      completed: 0,
      failed: 0,
      cancelled: 0,
      idle: 0,
    };
    let total = 0;
    for (const a of inScope) {
      const detail = presenceMap.get(a.id);
      if (!detail) continue;
      if (
        availabilityFilter !== "all" &&
        detail.availability !== availabilityFilter
      ) {
        continue;
      }
      counts[detail.lastTask] += 1;
      total += 1;
    }
    return { counts, total };
  }, [inScope, presenceMap, availabilityFilter]);

  // Final cut — apply both axes + search.
  const filteredAgents = useMemo(() => {
    const q = search.trim().toLowerCase();
    return inScope.filter((a) => {
      // Filter chips only apply to the Active view; Archived hides the
      // chip rows entirely (presence is undefined for archived agents).
      if (view === "active") {
        const detail = presenceMap.get(a.id);
        if (
          availabilityFilter !== "all" &&
          detail?.availability !== availabilityFilter
        ) {
          return false;
        }
        if (
          lastTaskFilter !== "all" &&
          detail?.lastTask !== lastTaskFilter
        ) {
          return false;
        }
      }
      if (q) {
        if (
          !a.name.toLowerCase().includes(q) &&
          !(a.description ?? "").toLowerCase().includes(q)
        )
          return false;
      }
      return true;
    });
  }, [inScope, availabilityFilter, lastTaskFilter, view, search, presenceMap]);

  const sortedAgents = useMemo(() => {
    const xs = [...filteredAgents];
    switch (sort) {
      case "name":
        xs.sort((a, b) => a.name.localeCompare(b.name));
        break;
      case "runs":
        xs.sort(
          (a, b) =>
            (runCountsById.get(b.id) ?? 0) - (runCountsById.get(a.id) ?? 0),
        );
        break;
      case "created":
        xs.sort((a, b) => +new Date(b.created_at) - +new Date(a.created_at));
        break;
      case "recent":
      default:
        // "Recent activity" prioritises 7d total completions (the same
        // window the row's sparkline shows), then 30d run count, then
        // created_at. We don't have a precise last-touched timestamp on
        // Agent today; this approximates it closely without a new column.
        xs.sort((a, b) => {
          const aSum = summarizeActivityWindow(
            activityMap.get(a.id),
            7,
          ).totalRuns;
          const bSum = summarizeActivityWindow(
            activityMap.get(b.id),
            7,
          ).totalRuns;
          if (aSum !== bSum) return bSum - aSum;
          const aRuns = runCountsById.get(a.id) ?? 0;
          const bRuns = runCountsById.get(b.id) ?? 0;
          if (aRuns !== bRuns) return bRuns - aRuns;
          return +new Date(b.created_at) - +new Date(a.created_at);
        });
        break;
    }
    return xs;
  }, [filteredAgents, sort, runCountsById, activityMap]);

  const archivedCount = useMemo(
    () => agents.filter((a) => !!a.archived_at).length,
    [agents],
  );

  const totalActiveCount = useMemo(
    () => agents.filter((a) => !a.archived_at).length,
    [agents],
  );

  // Auto-bounce out of Archived if the population empties (e.g. user
  // restored the last archived agent from another surface).
  useEffect(() => {
    if (view === "archived" && archivedCount === 0) setView("active");
  }, [view, archivedCount]);

  const handleCreate = async (data: CreateAgentRequest) => {
    const agent = await api.createAgent(data);
    // When duplicating, carry the source agent's skill assignments over.
    // Skills aren't part of CreateAgentRequest (they're managed via
    // setAgentSkills) so the create endpoint can't take them inline; we
    // do a follow-up call. Failure here doesn't abort the duplicate —
    // the agent already exists and the user can re-attach skills from
    // the detail page.
    if (duplicateTemplate?.skills.length) {
      try {
        await api.setAgentSkills(agent.id, {
          skill_ids: duplicateTemplate.skills.map((s) => s.id),
        });
      } catch {
        // Surfaced softly; the agent itself is fine.
      }
    }
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    setShowCreate(false);
    setDuplicateTemplate(null);
    navigation.push(paths.agentDetail(agent.id));
  };

  const handleDuplicate = useCallback((agent: Agent) => {
    setDuplicateTemplate(agent);
    setShowCreate(true);
  }, []);

  // Assemble per-row data once per render — agent + runtime + presence +
  // activity + role flags. The columns reach into `row.original` and never
  // pull their own queries, which keeps each cell a pure function.
  const agentRows = useMemo<AgentRow[]>(() => {
    return sortedAgents.map((agent) => {
      const isOwner =
        !!currentUser?.id && agent.owner_id === currentUser.id;
      const canManage = isWorkspaceAdmin || isOwner;
      const ownerIdToShow =
        scope === "all" &&
        agent.owner_id &&
        agent.owner_id !== currentUser?.id
          ? agent.owner_id
          : null;
      return {
        agent,
        runtime: runtimesById.get(agent.runtime_id) ?? null,
        presence: presenceMap.get(agent.id) ?? null,
        activity: activityMap.get(agent.id) ?? null,
        runCount: runCountsById.get(agent.id) ?? 0,
        ownerIdToShow,
        canManage,
      };
    });
  }, [
    sortedAgents,
    currentUser,
    isWorkspaceAdmin,
    scope,
    runtimesById,
    presenceMap,
    activityMap,
    runCountsById,
  ]);

  const columns = useMemo(
    () => createAgentColumns({ onDuplicate: handleDuplicate }),
    [handleDuplicate],
  );

  const table = useReactTable({
    data: agentRows,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  // ---- Loading ----
  if (isLoading) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <PageHeaderBar totalCount={0} onCreate={() => setShowCreate(true)} />
        <div className="flex flex-1 min-h-0 flex-col gap-4 p-6">
          <div className="flex flex-1 min-h-0 flex-col overflow-hidden rounded-lg border">
            <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
              <Skeleton className="h-7 w-32 rounded-md" />
              <Skeleton className="h-7 w-32 rounded-md" />
            </div>
            <div className="flex h-11 shrink-0 items-center gap-2 border-b px-4">
              <Skeleton className="h-6 w-16 rounded-full" />
              <Skeleton className="h-6 w-24 rounded-full" />
              <Skeleton className="h-6 w-20 rounded-full" />
            </div>
            <div className="space-y-2 p-4">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-14 w-full rounded-md" />
              ))}
            </div>
          </div>
        </div>
      </div>
    );
  }

  // ---- List request error ----
  if (listError) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        <PageHeaderBar totalCount={0} onCreate={() => setShowCreate(true)} />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
          <AlertCircle className="h-8 w-8 text-destructive" />
          <div>
            <p className="text-sm font-medium">Couldn&rsquo;t load agents</p>
            <p className="mt-1 text-xs text-muted-foreground">
              {listError instanceof Error
                ? listError.message
                : "Something went wrong fetching the agent list."}
            </p>
          </div>
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => refetchList()}
          >
            Try again
          </Button>
        </div>
      </div>
    );
  }

  const showEmpty = totalActiveCount === 0 && archivedCount === 0;

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeaderBar
        totalCount={totalActiveCount}
        onCreate={() => setShowCreate(true)}
      />

      <div className="flex flex-1 min-h-0 flex-col gap-4 p-6">
        {showEmpty ? (
          <div className="flex flex-1 items-center justify-center">
            <EmptyState onCreate={() => setShowCreate(true)} />
          </div>
        ) : (
          <div className="flex flex-1 min-h-0 flex-col overflow-hidden rounded-lg border bg-background">
            {view === "active" ? (
              <>
                <ActiveToolbarRow
                  scope={scope}
                  setScope={setScope}
                  scopeCounts={scopeCounts}
                  sort={sort}
                  setSort={setSort}
                  search={search}
                  setSearch={setSearch}
                />
                <PresenceFilterRows
                  availabilityFilter={availabilityFilter}
                  setAvailabilityFilter={setAvailabilityFilter}
                  availabilityCounts={availabilityCounts.counts}
                  availabilityTotal={availabilityCounts.total}
                  lastTaskFilter={lastTaskFilter}
                  setLastTaskFilter={setLastTaskFilter}
                  lastTaskCounts={lastTaskCounts.counts}
                  lastTaskTotal={lastTaskCounts.total}
                  visibleCount={sortedAgents.length}
                  totalCount={inScope.length}
                  archivedCount={archivedCount}
                  onShowArchived={() => setView("archived")}
                />
              </>
            ) : (
              <ArchivedToolbarRow
                onBack={() => setView("active")}
                archivedCount={archivedCount}
                sort={sort}
                setSort={setSort}
              />
            )}

            {sortedAgents.length === 0 ? (
              <NoMatches
                view={view}
                search={search}
                hasFilter={
                  availabilityFilter !== "all" || lastTaskFilter !== "all"
                }
                scope={scope}
              />
            ) : (
              <DataTable
                table={table}
                onRowClick={(row) =>
                  navigation.push(paths.agentDetail(row.original.agent.id))
                }
              />
            )}
          </div>
        )}
      </div>

      {showCreate && (
        <CreateAgentDialog
          runtimes={runtimes}
          runtimesLoading={runtimesLoading}
          members={members}
          currentUserId={currentUser?.id ?? null}
          template={duplicateTemplate}
          onClose={() => {
            setShowCreate(false);
            setDuplicateTemplate(null);
          }}
          onCreate={handleCreate}
        />
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Page header — icon + title + count + create CTA. Unchanged.
// ---------------------------------------------------------------------------

function PageHeaderBar({
  totalCount,
  onCreate,
}: {
  totalCount: number;
  onCreate: () => void;
}) {
  return (
    <PageHeader className="justify-between px-5">
      <div className="flex items-center gap-2">
        <Bot className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">Agents</h1>
        {totalCount > 0 && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
            {totalCount}
          </span>
        )}
        {/* Tagline next to the title — mirrors Runtimes / Skills. Single
            sentence + docs link, hidden below md so it never collides with
            the title on narrow screens. The presence chip row below carries
            the state-legend job, so the tagline only needs to anchor what
            an agent IS, not what each colour means. */}
        <p className="ml-2 hidden text-xs text-muted-foreground md:block">
          AI teammates that pick up issues, comment, and update status.{" "}
          <a
            href="https://multica.ai/docs/agents"
            target="_blank"
            rel="noopener noreferrer"
            className="underline decoration-muted-foreground/30 underline-offset-4 transition-colors hover:text-foreground"
          >
            Learn more →
          </a>
        </p>
      </div>
      <Button type="button" size="sm" onClick={onCreate}>
        <Plus className="h-3 w-3" />
        New agent
      </Button>
    </PageHeader>
  );
}

// ---------------------------------------------------------------------------
// Active view — Layer 1: scope segment + sort + search + archived link + live
// ---------------------------------------------------------------------------

function ActiveToolbarRow({
  scope,
  setScope,
  scopeCounts,
  sort,
  setSort,
  search,
  setSearch,
}: {
  scope: Scope;
  setScope: (v: Scope) => void;
  scopeCounts: { all: number; mine: number };
  sort: SortKey;
  setSort: (v: SortKey) => void;
  search: string;
  setSearch: (v: string) => void;
}) {
  // Layout follows Skills: [Search] [Mine|All]              [Sort ▼]
  // Search and the scope segment cluster on the left (Skills puts its
  // filter buttons immediately after the search the same way). Sort
  // gets pushed to the far right via ml-auto.
  return (
    <div className="flex h-12 shrink-0 items-center gap-2 border-b px-4">
      <div className="relative">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search agents…"
          className="h-8 w-64 pl-8 text-sm"
        />
      </div>
      <ScopeSegment scope={scope} setScope={setScope} counts={scopeCounts} />
      <div className="ml-auto">
        <SortDropdown sort={sort} setSort={setSort} />
      </div>
    </div>
  );
}

function ScopeSegment({
  scope,
  setScope,
  counts,
}: {
  scope: Scope;
  setScope: (v: Scope) => void;
  counts: { all: number; mine: number };
}) {
  // Mine first — that's the more frequent scope (your own agents) and
  // also the default selection, so it lives in the leading slot.
  return (
    <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
      <ScopeButton
        active={scope === "mine"}
        label="Mine"
        count={counts.mine}
        onClick={() => setScope("mine")}
      />
      <ScopeButton
        active={scope === "all"}
        label="All"
        count={counts.all}
        onClick={() => setScope("all")}
      />
    </div>
  );
}

function ScopeButton({
  active,
  label,
  count,
  onClick,
}: {
  active: boolean;
  label: string;
  count: number;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`inline-flex items-center gap-1.5 rounded px-2.5 py-1 text-xs font-medium transition-colors ${
        active
          ? "bg-background text-foreground shadow-sm"
          : "text-muted-foreground hover:text-foreground"
      }`}
    >
      <span>{label}</span>
      <span
        className={`font-mono tabular-nums ${
          active ? "text-muted-foreground/80" : "text-muted-foreground/50"
        }`}
      >
        {count}
      </span>
    </button>
  );
}

function SortDropdown({
  sort,
  setSort,
}: {
  sort: SortKey;
  setSort: (v: SortKey) => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant="ghost"
            size="sm"
            className="h-8 gap-1.5 text-xs text-muted-foreground hover:text-foreground"
          />
        }
      >
        <ArrowUpDown className="h-3 w-3" />
        {SORT_LABEL[sort]}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-auto">
        {SORT_KEYS.map((k) => (
          <DropdownMenuItem
            key={k}
            onClick={() => setSort(k)}
            className="text-xs"
          >
            {SORT_LABEL[k]}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// ---------------------------------------------------------------------------
// Active view — Layer 2: two independent filter axes (availability + last
// task) + visible/total count. The right edge hosts the low-frequency
// "Show archived" link, kept out of Layer 1 so the primary toolbar stays
// uncluttered.
//
// Two rows because cramming both axes into a single row makes the chip
// labels feel ambiguous ("Online" and "Failed" side by side reads as a
// single stack of facets, but they're on different axes). Two rows with
// a leading label make the axis split obvious.
// ---------------------------------------------------------------------------

function PresenceFilterRows({
  availabilityFilter,
  setAvailabilityFilter,
  availabilityCounts,
  availabilityTotal,
  lastTaskFilter,
  setLastTaskFilter,
  lastTaskCounts,
  lastTaskTotal,
  visibleCount,
  totalCount,
  archivedCount,
  onShowArchived,
}: {
  availabilityFilter: AvailabilityFilter;
  setAvailabilityFilter: (v: AvailabilityFilter) => void;
  availabilityCounts: Record<AgentAvailability, number>;
  availabilityTotal: number;
  lastTaskFilter: LastTaskFilter;
  setLastTaskFilter: (v: LastTaskFilter) => void;
  lastTaskCounts: Record<LastTaskState, number>;
  lastTaskTotal: number;
  visibleCount: number;
  totalCount: number;
  archivedCount: number;
  onShowArchived: () => void;
}) {
  return (
    <div className="flex shrink-0 flex-col gap-1.5 border-b px-4 py-2.5">
      {/* Row 1: Availability — 3 chips. */}
      <div className="flex items-center gap-2">
        <span className="w-16 shrink-0 text-xs text-muted-foreground">
          Status
        </span>
        <PresenceChip
          active={availabilityFilter === "all"}
          onClick={() => setAvailabilityFilter("all")}
          label="All"
          count={availabilityTotal}
          description="No availability filter"
        />
        {availabilityOrder.map((a) => {
          const cfg = availabilityConfig[a];
          return (
            <PresenceChip
              key={a}
              active={availabilityFilter === a}
              onClick={() => setAvailabilityFilter(a)}
              label={cfg.label}
              count={availabilityCounts[a]}
              dotClass={cfg.dotClass}
              description={AVAILABILITY_DESCRIPTION[a]}
            />
          );
        })}
        <div className="ml-auto flex items-center gap-3">
          {archivedCount > 0 && (
            <button
              type="button"
              onClick={onShowArchived}
              className="text-xs text-muted-foreground transition-colors hover:text-foreground"
            >
              Show archived ({archivedCount}) →
            </button>
          )}
          <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
            {visibleCount} of {totalCount}
          </span>
        </div>
      </div>
      {/* Row 2: Last task — 5 chips. */}
      <div className="flex items-center gap-2">
        <span className="w-16 shrink-0 text-xs text-muted-foreground">
          Last run
        </span>
        <PresenceChip
          active={lastTaskFilter === "all"}
          onClick={() => setLastTaskFilter("all")}
          label="All"
          count={lastTaskTotal}
          description="No last-run filter"
        />
        {lastTaskOrder.map((t) => {
          const cfg = taskStateConfig[t];
          return (
            <PresenceChip
              key={t}
              active={lastTaskFilter === t}
              onClick={() => setLastTaskFilter(t)}
              label={cfg.label}
              count={lastTaskCounts[t]}
              description={LAST_TASK_DESCRIPTION[t]}
            />
          );
        })}
      </div>
    </div>
  );
}

// Same Button + Tooltip pattern Skills uses for its scope filters. Selected
// state mirrors Skills' `bg-accent text-accent-foreground hover:bg-accent/80`,
// so any future global tweak to that token cascades here for free.
function PresenceChip({
  active,
  onClick,
  label,
  count,
  dotClass,
  description,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  count: number;
  dotClass?: string;
  description: string;
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <Button
            variant="outline"
            size="sm"
            onClick={onClick}
            className={
              active
                ? "bg-accent text-accent-foreground hover:bg-accent/80"
                : "text-muted-foreground"
            }
          >
            {dotClass && (
              <span className={`h-1.5 w-1.5 rounded-full ${dotClass}`} />
            )}
            <span>{label}</span>
            <span className="font-mono tabular-nums text-muted-foreground/70">
              {count}
            </span>
          </Button>
        }
      />
      <TooltipContent side="top">{description}</TooltipContent>
    </Tooltip>
  );
}

// ---------------------------------------------------------------------------
// Archived view — single toolbar row (back link + title + count + sort).
// No presence chip row: presence is undefined for archived agents.
// ---------------------------------------------------------------------------

function ArchivedToolbarRow({
  onBack,
  archivedCount,
  sort,
  setSort,
}: {
  onBack: () => void;
  archivedCount: number;
  sort: SortKey;
  setSort: (v: SortKey) => void;
}) {
  return (
    <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <button
        type="button"
        onClick={onBack}
        className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
      >
        <ArrowLeft className="h-3 w-3" />
        Active agents
      </button>
      <span className="text-muted-foreground/40">/</span>
      <span className="text-xs font-medium">Archived agents</span>
      <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
        {archivedCount}
      </span>
      <div className="ml-auto">
        <SortDropdown sort={sort} setSort={setSort} />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Empty / no-matches states
// ---------------------------------------------------------------------------

function EmptyState({ onCreate }: { onCreate: () => void }) {
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <Bot className="h-6 w-6 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-base font-semibold">No agents yet</h2>
      <p className="mt-1 max-w-md text-sm text-muted-foreground">
        Create an agent and assign it issues, like any teammate. Local agents
        run on your machine; cloud agents run on Multica&rsquo;s runtime.
      </p>
      <Button type="button" onClick={onCreate} size="sm" className="mt-5">
        <Plus className="h-3 w-3" />
        New agent
      </Button>
    </div>
  );
}

function NoMatches({
  view,
  search,
  hasFilter: filterActive,
  scope,
}: {
  view: View;
  search: string;
  hasFilter: boolean;
  scope: Scope;
}) {
  const hasSearch = search.length > 0;
  const hasFilter = filterActive || scope === "mine";

  let body: string;
  if (view === "archived") {
    body = hasSearch
      ? `No archived agents match "${search}".`
      : "No archived agents yet.";
  } else if (hasSearch) {
    body = `No agents match "${search}"${hasFilter ? " in this filter" : ""}.`;
  } else {
    body = "No agents match this filter.";
  }

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 px-4 py-16 text-center text-muted-foreground">
      <Search className="h-8 w-8 text-muted-foreground/40" />
      <p className="text-sm">No matches</p>
      <p className="max-w-xs text-xs">{body}</p>
    </div>
  );
}
