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
  agentRunCounts30dOptions,
  summarizeActivityWindow,
  useWorkspaceActivityMap,
  useWorkspacePresenceMap,
} from "@multica/core/agents";
import { useAgentsViewStore } from "@multica/core/agents/stores";
import { api } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { canAssignAgentToIssue } from "@multica/core/permissions";
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
import { DataTable } from "@multica/ui/components/ui/data-table";
import { useNavigation } from "../../navigation";
import { PageHeader } from "../../layout/page-header";
import { availabilityConfig, availabilityOrder } from "../presence";
import { CreateAgentDialog } from "./create-agent-dialog";
import { type AgentRow, createAgentColumns } from "./agent-columns";
import { useT } from "../../i18n";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";

// Filter axes:
//
//   View         = active vs archived dataset. Archived is low-frequency,
//                  accessed through a ghost link in the toolbar.
//   Scope        = ownership lens (All vs Mine). Layer-1 segment.
//   Availability = "Can the agent take work right now?" — 3-state chip
//                  group (online / unstable / offline) sourced from
//                  AgentAvailability. The only chip filter we keep —
//                  the previous Workload axis was dropped because its
//                  "queued / failed / cancelled" buckets became
//                  meaningless once Failed left the workload model.
type View = "active" | "archived";
type Scope = "all" | "mine";
type AvailabilityFilter = "all" | AgentAvailability;

type SortKey = "recent" | "name" | "runs" | "created";
const SORT_KEYS: SortKey[] = ["recent", "name", "runs", "created"];
const SORT_LABEL_KEY: Record<SortKey, "label_recent" | "label_name" | "label_runs" | "label_created"> = {
  recent: "label_recent",
  name: "label_name",
  runs: "label_runs",
  created: "label_created",
};

export function AgentsPage() {
  const { t } = useT("agents");
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
  // Scope (Mine/All) is persisted per workspace so it survives list →
  // detail → back navigation. Default is "mine" on first visit.
  const scope = useAgentsViewStore((s) => s.scope);
  const setScope = useAgentsViewStore((s) => s.setScope);
  const [availabilityFilter, setAvailabilityFilter] =
    useState<AvailabilityFilter>("all");
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

  // Layer 1b — visibility. Personal (visibility=private) agents owned by
  // someone else are hidden from regular members; workspace owners/admins
  // still see everything. Mirrors the assign-to-issue gate so the list
  // only ever shows agents the user could actually act on. Backend keeps
  // returning all agents, so admin tools (and the API itself) are
  // unaffected — this is a UI-only filter.
  const visibleInView = useMemo(() => {
    return inView.filter((a) =>
      canAssignAgentToIssue(a, {
        userId: currentUser?.id ?? null,
        role: myRole,
      }).allowed,
    );
  }, [inView, currentUser?.id, myRole]);

  // Layer 1c — ownership scope. Counts shown on the segment are
  // computed against the visibleInView set so the numbers always reflect
  // "what would I see if I clicked this".
  const scopeCounts = useMemo(() => {
    let mine = 0;
    if (currentUser) {
      for (const a of visibleInView) {
        if (a.owner_id === currentUser.id) mine += 1;
      }
    }
    return { all: visibleInView.length, mine };
  }, [visibleInView, currentUser]);

  const inScope = useMemo(() => {
    // Archived view ignores Mine / All — its toolbar has no scope
    // segment, so silently filtering by `scope` would hide other
    // people's archived agents without any UI to explain why.
    if (view === "archived") return visibleInView;
    if (scope === "all" || !currentUser) return visibleInView;
    return visibleInView.filter((a) => a.owner_id === currentUser.id);
  }, [visibleInView, scope, currentUser, view]);

  // Final cut — availability chip + search.
  const filteredAgents = useMemo(() => {
    const q = search.trim().toLowerCase();
    return inScope.filter((a) => {
      // Availability chip filter only applies to the Active view —
      // archived agents have no presence to match against.
      if (view === "active" && availabilityFilter !== "all") {
        const detail = presenceMap.get(a.id);
        if (detail?.availability !== availabilityFilter) return false;
      }
      if (q) {
        if (
          !a.name.toLowerCase().includes(q) &&
          !matchesPinyin(a.name, q) &&
          !(a.description ?? "").toLowerCase().includes(q)
        ) {
          return false;
        }
      }
      return true;
    });
  }, [inScope, view, availabilityFilter, presenceMap, search]);

  // Per-availability counts for the chip badges. Computed against
  // `inScope` (ignoring the availability filter itself) so the numbers
  // reflect "if I clicked this chip, this many agents would match"
  // rather than collapsing to 0 for the unselected chips.
  const availabilityCounts = useMemo(() => {
    const counts: Record<AgentAvailability, number> = {
      online: 0,
      unstable: 0,
      offline: 0,
    };
    for (const a of inScope) {
      const detail = presenceMap.get(a.id);
      if (!detail) continue;
      counts[detail.availability] += 1;
    }
    return counts;
  }, [inScope, presenceMap]);

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

  const handleCreate = async (data: CreateAgentRequest): Promise<Agent> => {
    const agent = await api.createAgent(data);
    // Skill follow-up is now owned by the dialog (it reads the user's
    // form selection, which already includes the duplicate source's
    // skills as a default when applicable). The dialog will call
    // setAgentSkills after we return; we just have to surface the
    // created agent so it can.
    qc.setQueryData<Agent[]>(workspaceKeys.agents(wsId), (current = []) => {
      const exists = current.some((a) => a.id === agent.id);
      return exists
        ? current.map((a) => (a.id === agent.id ? agent : a))
        : [...current, agent];
    });
    setShowCreate(false);
    setDuplicateTemplate(null);
    navigation.push(paths.agentDetail(agent.id));
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    return agent;
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
        isOwnedByMe: isOwner,
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
    () => createAgentColumns({ onDuplicate: handleDuplicate, t }),
    [handleDuplicate, t],
  );

  const table = useReactTable({
    data: agentRows,
    columns,
    getCoreRowModel: getCoreRowModel(),
    enableColumnResizing: true,
    // Pin the kebab column right so it stays accessible during horizontal
    // scroll — matches the pattern in Linear / Notion / GitHub.
    initialState: { columnPinning: { right: ["actions"] } },
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
    return <ListError onCreate={() => setShowCreate(true)} listError={listError} onRetry={refetchList} />;
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
                  visibleCount={sortedAgents.length}
                  totalCount={inScope.length}
                  archivedCount={archivedCount}
                  onShowArchived={() => setView("archived")}
                />
                <AvailabilityFilterRow
                  value={availabilityFilter}
                  onChange={setAvailabilityFilter}
                  counts={availabilityCounts}
                  totalCount={inScope.length}
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
              <NoMatches view={view} search={search} scope={scope} />
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
  const { t } = useT("agents");
  return (
    <PageHeader className="justify-between px-5">
      <div className="flex items-center gap-2">
        <Bot className="h-4 w-4 text-muted-foreground" />
        <h1 className="text-sm font-medium">{t(($) => $.page.title)}</h1>
        {totalCount > 0 && (
          <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
            {totalCount}
          </span>
        )}
        {/* Tagline next to the title — mirrors Runtimes / Skills. */}
        <p className="ml-2 hidden text-xs text-muted-foreground md:block">
          {t(($) => $.page.tagline)}{" "}
          <a
            href="https://multica.ai/docs/agents"
            target="_blank"
            rel="noopener noreferrer"
            className="underline decoration-muted-foreground/30 underline-offset-4 transition-colors hover:text-foreground"
          >
            {t(($) => $.page.learn_more)}
          </a>
        </p>
      </div>
      <Button type="button" size="sm" onClick={onCreate}>
        <Plus className="h-3 w-3" />
        {t(($) => $.page.new_agent)}
      </Button>
    </PageHeader>
  );
}

function ListError({
  onCreate,
  listError,
  onRetry,
}: {
  onCreate: () => void;
  listError: unknown;
  onRetry: () => void;
}) {
  const { t } = useT("agents");
  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <PageHeaderBar totalCount={0} onCreate={onCreate} />
      <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 py-16 text-center">
        <AlertCircle className="h-8 w-8 text-destructive" />
        <div>
          <p className="text-sm font-medium">{t(($) => $.page.list_load_failed)}</p>
          <p className="mt-1 text-xs text-muted-foreground">
            {listError instanceof Error
              ? listError.message
              : t(($) => $.page.list_load_failed_default)}
          </p>
        </div>
        <Button
          type="button"
          variant="outline"
          size="sm"
          onClick={onRetry}
        >
          {t(($) => $.page.try_again)}
        </Button>
      </div>
    </div>
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
  visibleCount,
  totalCount,
  archivedCount,
  onShowArchived,
}: {
  scope: Scope;
  setScope: (v: Scope) => void;
  scopeCounts: { all: number; mine: number };
  sort: SortKey;
  setSort: (v: SortKey) => void;
  search: string;
  setSearch: (v: string) => void;
  visibleCount: number;
  totalCount: number;
  archivedCount: number;
  onShowArchived: () => void;
}) {
  const { t } = useT("agents");
  return (
    <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <div className="relative">
        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
        <Input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t(($) => $.page.search_placeholder)}
          className="h-8 w-64 pl-8 text-sm"
        />
      </div>
      <ScopeSegment scope={scope} setScope={setScope} counts={scopeCounts} />
      <div className="ml-auto flex items-center gap-3">
        {archivedCount > 0 && (
          <button
            type="button"
            onClick={onShowArchived}
            className="text-xs text-muted-foreground transition-colors hover:text-foreground"
          >
            {t(($) => $.page.show_archived, { count: archivedCount })}
          </button>
        )}
        <span className="font-mono text-xs tabular-nums text-muted-foreground/70">
          {t(($) => $.page.of_total, { visible: visibleCount, total: totalCount })}
        </span>
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
  const { t } = useT("agents");
  return (
    <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
      <ScopeButton
        active={scope === "mine"}
        label={t(($) => $.scope.mine)}
        count={counts.mine}
        onClick={() => setScope("mine")}
      />
      <ScopeButton
        active={scope === "all"}
        label={t(($) => $.scope.all)}
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
  const { t } = useT("agents");
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
        {t(($) => $.sort[SORT_LABEL_KEY[sort]])}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-auto">
        {SORT_KEYS.map((k) => (
          <DropdownMenuItem
            key={k}
            onClick={() => setSort(k)}
            className="text-xs"
          >
            {t(($) => $.sort[SORT_LABEL_KEY[k]])}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

// ---------------------------------------------------------------------------
// Availability chip row — All / Online / Unstable / Offline. Only shown
// in the Active view; archived agents have no presence.
// ---------------------------------------------------------------------------

function AvailabilityFilterRow({
  value,
  onChange,
  counts,
  totalCount,
}: {
  value: AvailabilityFilter;
  onChange: (v: AvailabilityFilter) => void;
  counts: Record<AgentAvailability, number>;
  totalCount: number;
}) {
  const { t } = useT("agents");
  return (
    <div className="flex h-11 shrink-0 items-center gap-2 border-b px-4">
      <AvailabilityChip
        active={value === "all"}
        onClick={() => onChange("all")}
        label={t(($) => $.availability.all)}
        count={totalCount}
      />
      {availabilityOrder.map((a) => {
        const cfg = availabilityConfig[a];
        return (
          <AvailabilityChip
            key={a}
            active={value === a}
            onClick={() => onChange(a)}
            label={t(($) => $.availability[a])}
            count={counts[a]}
            dotClass={cfg.dotClass}
          />
        );
      })}
    </div>
  );
}

function AvailabilityChip({
  active,
  onClick,
  label,
  count,
  dotClass,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
  count: number;
  dotClass?: string;
}) {
  return (
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
      {dotClass && <span className={`h-1.5 w-1.5 rounded-full ${dotClass}`} />}
      <span>{label}</span>
      <span className="font-mono tabular-nums text-muted-foreground/70">
        {count}
      </span>
    </Button>
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
  const { t } = useT("agents");
  return (
    <div className="flex h-12 shrink-0 items-center gap-3 border-b px-4">
      <button
        type="button"
        onClick={onBack}
        className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground"
      >
        <ArrowLeft className="h-3 w-3" />
        {t(($) => $.archived.active_link)}
      </button>
      <span className="text-muted-foreground/40">/</span>
      <span className="text-xs font-medium">{t(($) => $.archived.title)}</span>
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
  const { t } = useT("agents");
  return (
    <div className="flex flex-1 flex-col items-center justify-center px-6 py-16 text-center">
      <div className="flex h-12 w-12 items-center justify-center rounded-full bg-muted">
        <Bot className="h-6 w-6 text-muted-foreground" />
      </div>
      <h2 className="mt-4 text-base font-semibold">{t(($) => $.empty.title)}</h2>
      <p className="mt-1 max-w-md text-sm text-muted-foreground">
        {t(($) => $.empty.description)}
      </p>
      <Button type="button" onClick={onCreate} size="sm" className="mt-5">
        <Plus className="h-3 w-3" />
        {t(($) => $.page.new_agent)}
      </Button>
    </div>
  );
}

function NoMatches({
  view,
  search,
  scope,
}: {
  view: View;
  search: string;
  scope: Scope;
}) {
  const { t } = useT("agents");
  const hasSearch = search.length > 0;
  const hasFilter = scope === "mine";

  let body: string;
  if (view === "archived") {
    body = hasSearch
      ? t(($) => $.no_matches.search_archived, { query: search })
      : t(($) => $.no_matches.no_archived);
  } else if (hasSearch) {
    body = hasFilter
      ? t(($) => $.no_matches.search_active_filtered, { query: search })
      : t(($) => $.no_matches.search_active, { query: search });
  } else {
    body = t(($) => $.no_matches.no_filter_match);
  }

  return (
    <div className="flex flex-1 flex-col items-center justify-center gap-2 px-4 py-16 text-center text-muted-foreground">
      <Search className="h-8 w-8 text-muted-foreground/40" />
      <p className="text-sm">{t(($) => $.no_matches.title)}</p>
      <p className="max-w-xs text-xs">{body}</p>
    </div>
  );
}
