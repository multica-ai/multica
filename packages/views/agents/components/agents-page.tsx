"use client";

import { useState, useEffect, useMemo } from "react";
import { useDefaultLayout } from "react-resizable-panels";
import { Bot, Plus, Archive, ChevronDown, Check } from "lucide-react";
import type { CreateAgentRequest, UpdateAgentRequest } from "@multica/core/types";
import {
  ResizablePanelGroup,
  ResizablePanel,
  ResizableHandle,
} from "@multica/ui/components/ui/resizable";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import { toast } from "sonner";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
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
import { AgentListItem } from "./agent-list-item";
import { AgentDetail } from "./agent-detail";
import { ActorAvatar } from "../../common/actor-avatar";
import type { MemberWithUser } from "@multica/core/types";

type AgentScope = "mine" | "all";

export function AgentsPage() {
  const { t } = useT("agents");
  const wsId = useWorkspaceId();
  const [scope, setScope] = useState<AgentScope>("mine");
  const [ownerFilter, setOwnerFilter] = useState<string | null>(null);
  const { data: agents = [], isLoading } = useQuery(
    agentListOptions(wsId, scope === "mine" ? "me" : undefined),
  );
  const [selectedId, setSelectedId] = useState<string>("");
  const [showArchived, setShowArchived] = useState(false);
  const [showCreate, setShowCreate] = useState(false);
  const { data: runtimes = [], isLoading: runtimesLoading } = useQuery(runtimeListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({
    id: "multica_agents_layout",
  });

  const uniqueOwners = useMemo(() => {
    if (scope !== "all") return [] as MemberWithUser[];
    const ids = Array.from(
      new Set(agents.map((a) => a.owner_id).filter(Boolean) as string[]),
    );
    return ids
      .map((id) => members.find((m) => m.user_id === id))
      .filter(Boolean) as MemberWithUser[];
  }, [scope, agents, members]);

  const ownerCounts = useMemo(() => {
    const m = new Map<string, number>();
    for (const a of agents) {
      if (a.owner_id) m.set(a.owner_id, (m.get(a.owner_id) ?? 0) + 1);
    }
    return m;
  }, [agents]);

  const listAfterOwnerFilter = useMemo(() => {
    if (scope === "all" && ownerFilter) {
      return agents.filter((a) => a.owner_id === ownerFilter);
    }
    return agents;
  }, [agents, scope, ownerFilter]);

  const filteredAgents = useMemo(
    () =>
      showArchived
        ? listAfterOwnerFilter.filter((a) => !!a.archived_at)
        : listAfterOwnerFilter.filter((a) => !a.archived_at),
    [listAfterOwnerFilter, showArchived],
  );

  const archivedCount = useMemo(
    () => listAfterOwnerFilter.filter((a) => !!a.archived_at).length,
    [listAfterOwnerFilter],
  );

  const getOwnerMember = (ownerId: string | null) => {
    if (!ownerId) return null;
    return members.find((m) => m.user_id === ownerId) ?? null;
  };

  const selectedOwner = ownerFilter ? getOwnerMember(ownerFilter) : null;

  const invalidateAgentQueries = () => {
    qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
  };

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

  const handleCreate = async (data: CreateAgentRequest) => {
    const agent = await api.createAgent(data);
    invalidateAgentQueries();
    setSelectedId(agent.id);
  };

  const handleUpdate = async (id: string, data: Record<string, unknown>) => {
    try {
      await api.updateAgent(id, data as UpdateAgentRequest);
      invalidateAgentQueries();
      toast.success("Agent updated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to update agent");
      throw e;
    }
  };

  const handleArchive = async (id: string) => {
    try {
      await api.archiveAgent(id);
      invalidateAgentQueries();
      toast.success("Agent archived");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to archive agent");
    }
  };

  const handleRestore = async (id: string) => {
    try {
      await api.restoreAgent(id);
      invalidateAgentQueries();
      toast.success("Agent restored");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to restore agent");
    }
  };

  const handleDuplicate = async (id: string) => {
    try {
      const newAgent = await api.copyAgent(id);
      invalidateAgentQueries();
      setSelectedId(newAgent.id);
      setShowArchived(false);
      toast.success("Agent duplicated");
    } catch (e) {
      toast.error(e instanceof Error ? e.message : "Failed to duplicate agent");
    }
  };

  const selected = agents.find((a) => a.id === selectedId) ?? null;

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
    <ResizablePanelGroup
      orientation="horizontal"
      className="flex-1 min-h-0"
      defaultLayout={defaultLayout}
      onLayoutChanged={onLayoutChanged}
    >
      <ResizablePanel id="list" defaultSize={280} minSize={240} maxSize={400} groupResizeBehavior="preserve-pixel-size">
        {/* Left column — agent list */}
        <div className="overflow-y-auto h-full border-r">
          <PageHeader className="justify-between">
            <h1 className="text-sm font-semibold">Agents</h1>
            <div className="flex items-center gap-1">
              {archivedCount > 0 && (
                <Button
                  variant={showArchived ? "secondary" : "ghost"}
                  size="icon-sm"
                  onClick={() => setShowArchived(!showArchived)}
                  title={showArchived ? "Show active agents" : "Show archived agents"}
                >
                  <Archive className="text-muted-foreground" />
                </Button>
              )}
              <Button
                variant="ghost"
                size="icon-sm"
                onClick={() => setShowCreate(true)}
              >
                <Plus className="text-muted-foreground" />
              </Button>
            </div>
          </PageHeader>

          <div className="flex items-center justify-between border-b px-4 py-2">
            <div className="flex items-center gap-0.5 rounded-md bg-muted p-0.5">
              <button
                type="button"
                onClick={() => {
                  setScope("mine");
                  setOwnerFilter(null);
                }}
                className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
                  scope === "mine"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                Mine
              </button>
              <button
                type="button"
                onClick={() => {
                  setScope("all");
                  setOwnerFilter(null);
                }}
                className={`rounded px-2.5 py-1 text-xs font-medium transition-colors ${
                  scope === "all"
                    ? "bg-background text-foreground shadow-sm"
                    : "text-muted-foreground hover:text-foreground"
                }`}
              >
                All
              </button>
            </div>

            {scope === "all" && uniqueOwners.length > 1 && (
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <button
                      type="button"
                      className="flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                    />
                  }
                >
                  {selectedOwner ? (
                    <>
                      <ActorAvatar actorType="member" actorId={selectedOwner.user_id} size={16} />
                      <span className="max-w-20 truncate">{selectedOwner.name}</span>
                    </>
                  ) : (
                    <span>Owner</span>
                  )}
                  <ChevronDown className="h-3 w-3 opacity-50" />
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end" className="w-48">
                  <DropdownMenuItem
                    onClick={() => setOwnerFilter(null)}
                    className="flex items-center justify-between"
                  >
                    <span className="text-xs">All owners</span>
                    {!ownerFilter && <Check className="h-3.5 w-3.5 text-foreground" />}
                  </DropdownMenuItem>
                  <DropdownMenuSeparator />
                  {uniqueOwners.map((m) => (
                    <DropdownMenuItem
                      key={m.user_id}
                      onClick={() => setOwnerFilter(ownerFilter === m.user_id ? null : m.user_id)}
                      className="flex items-center justify-between"
                    >
                      <div className="flex min-w-0 items-center gap-2">
                        <ActorAvatar actorType="member" actorId={m.user_id} size={18} />
                        <span className="truncate text-xs">{m.name}</span>
                        <span className="text-xs text-muted-foreground">
                          {ownerCounts.get(m.user_id) ?? 0}
                        </span>
                      </div>
                      {ownerFilter === m.user_id && (
                        <Check className="h-3.5 w-3.5 shrink-0 text-foreground" />
                      )}
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
            )}
          </div>

          {filteredAgents.length === 0 ? (
            <div className="flex flex-col items-center justify-center px-4 py-12">
              <Bot className="h-8 w-8 text-muted-foreground/40" />
              <p className="mt-3 text-sm text-muted-foreground">
                {showArchived
                  ? "No archived agents"
                  : scope === "mine"
                    ? archivedCount > 0
                      ? "No active agents"
                      : "No agents yet"
                    : ownerFilter
                      ? "No agents for this owner"
                      : archivedCount > 0
                        ? "No active agents"
                        : "No agents yet"}
              </p>
              {!showArchived && (
                <Button
                  onClick={() => setShowCreate(true)}
                  size="xs"
                  className="mt-3"
                >
                  <Plus className="h-3 w-3" />
                  Create Agent
                </Button>
              )}
            </div>
          ) : (
            <div className="divide-y">
              {filteredAgents.map((agent) => (
                <AgentListItem
                  key={agent.id}
                  agent={agent}
                  isSelected={agent.id === selectedId}
                  onClick={() => setSelectedId(agent.id)}
                  ownerMember={scope === "all" ? getOwnerMember(agent.owner_id) : undefined}

                />
              ))}
            </div>
          )}
        </div>
      </ResizablePanel>

      <ResizableHandle />

      <ResizablePanel id="detail" minSize="50%">
        {/* Right column — agent detail */}
        {selected ? (
          <AgentDetail
            key={selected.id}
            agent={selected}
            runtimes={runtimes}
            members={members}
            currentUserId={currentUser?.id ?? null}
            onUpdate={handleUpdate}
            onArchive={handleArchive}
            onRestore={handleRestore}
            onDuplicate={() => handleDuplicate(selected.id)}
          />
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
