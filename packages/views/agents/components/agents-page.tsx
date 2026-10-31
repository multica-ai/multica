"use client";

import { useCallback, useMemo, useState } from "react";
import {
  AlertCircle,
  Bot,
  Lock,
  Plus,
} from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
} from "@multica/core/types";
import {
  type AgentActivity,
  agentRunCounts30dOptions,
  effectiveAccessScope,
  isAgentRuntimeBound,
  useWorkspaceActivityMap,
  useWorkspacePresenceMap,
  VISIBILITY_TOOLTIP,
  type AgentPresenceDetail,
} from "@multica/core/agents";
import {
  type AgentListFilters,
  useAgentsViewStore,
  AGENT_SCOPES,
  type AgentColumnKey,
  type AgentsScope,
  type AgentSortField,
} from "@multica/core/agents/stores";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { runtimeDisplayLabel, runtimeListOptions } from "@multica/core/runtimes";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";
import { useNavigation } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { ProviderLogo } from "../../runtimes/components/provider-logo";
import {
  CollectionCreateButton,
  CollectionPageHeader,
  CollectionPageState,
  CollectionResetButton,
  CollectionScopeItems,
} from "../../layout/collection-page";
import {
  CollectionDisplayMenu,
  CollectionEmptyValue,
  CollectionList,
  CollectionNameCell,
  CollectionTag,
  type CollectionColumn,
  type CollectionSort,
} from "../../layout/collection-list";
import { availabilityConfig } from "../presence";
import { AgentRowActions } from "./agent-row-actions";
import {
  AgentListFilterMenu,
  countActiveFilterDimensions,
} from "./agent-list-toolbar";
import { useLocale, useT } from "../../i18n";

export interface AgentListRow {
  agent: Agent;
  runtime: AgentRuntime | null;
  presence: AgentPresenceDetail | null;
  activity: AgentActivity | null;
  runCount: number;
  /** Days since the last bucket with runs; null = nothing in the window. */
  lastActiveDays: number | null;
  owner: MemberWithUser | null;
  isOwnedByMe: boolean;
  canManage: boolean;
}

// Most recent activity bucket with runs, as "days ago" (0 = today).
// Day-granularity by design — derived from the same 30-day buckets the
// detail page charts, no extra API.
function lastActiveDaysAgo(activity: AgentActivity | null): number | null {
  if (!activity) return null;
  for (let i = activity.buckets.length - 1; i >= 0; i--) {
    const bucket = activity.buckets[i];
    if (bucket && bucket.total > 0) return activity.buckets.length - 1 - i;
  }
  return null;
}

/**
 * Pure row-filter predicate: returns true if the row matches all active
 * filter dimensions. Empty filter arrays are inactive (the row passes). The
 * `access` dimension derives its key via `effectiveAccessScope` so the column
 * and the filter share one derivation. Exported for testing — the page wires
 * it inside its `useMemo`.
 */
export function rowMatchesFilters(
  row: AgentListRow,
  filters: AgentListFilters,
): boolean {
  if (
    filters.availability.length > 0 &&
    (!row.presence || !filters.availability.includes(row.presence.availability))
  ) {
    return false;
  }
  if (
    filters.runtimes.length > 0 &&
    !filters.runtimes.includes(row.agent.runtime_id)
  ) {
    return false;
  }
  if (
    filters.owners.length > 0 &&
    (!row.agent.owner_id || !filters.owners.includes(row.agent.owner_id))
  ) {
    return false;
  }
  if (
    filters.models.length > 0 &&
    !filters.models.includes(row.agent.model)
  ) {
    return false;
  }
  if (
    filters.access.length > 0 &&
    !filters.access.includes(
      effectiveAccessScope(
        row.agent.permission_mode,
        row.agent.invocation_targets,
      ),
    )
  ) {
    return false;
  }
  return true;
}

/**
 * Bulk-access dialog confirm-button enablement is centralized in
 * `@multica/core/agents` as `isAccessChangeReady` (MUL-3963). The dialog
 * consumes it; the picker also gates its internal Save button on the same
 * predicate (its own Save button is hidden via `hideFooter` in the bulk flow).
 */
import { isAccessChangeReady } from "@multica/core/agents";
import { AgentBatchToolbar } from "./agent-batch-toolbar";
export { isAccessChangeReady };

export interface AgentsPageProps {
  /** Desktop-only daemon wiring, currently unused by the list (kept for
   *  platform-layer compatibility; the runtime filter lists runtimes by
   *  name rather than grouped machines). */
  localDaemonId?: string | null;
  localMachineName?: string | null;
  hasLocalMachine?: boolean;
}

// Page header

function ListError({
  listError,
  onRetry,
}: {
  listError: unknown;
  onRetry: () => void;
}) {
  const { t } = useT("agents");
  return (
    <CollectionPageState
      role="alert"
      tone="destructive"
      icon={AlertCircle}
      title={t(($) => $.page.list_load_failed)}
      description={
        listError instanceof Error
          ? listError.message
          : t(($) => $.page.list_load_failed_default)
      }
      actions={
        <Button type="button" variant="outline" size="sm" onClick={onRetry}>
          {t(($) => $.page.try_again)}
        </Button>
      }
    />
  );
}

function EmptyState({ onCreate }: { onCreate: () => void }) {
  const { t } = useT("agents");
  return (
    <CollectionPageState
      icon={Bot}
      title={t(($) => $.empty.title)}
      description={t(($) => $.empty.description)}
      actions={
        <Button type="button" onClick={onCreate} size="sm">
          <Plus aria-hidden="true" className="size-3" />
          {t(($) => $.page.new_agent)}
        </Button>
      }
    />
  );
}

// Cells

function NameCell({ row }: { row: AgentListRow }) {
  const { t } = useT("agents");
  const { agent, isOwnedByMe } = row;
  const isArchived = !!agent.archived_at;
  const isPrivate = agent.visibility === "private";
  return (
    <CollectionNameCell
      icon={
        <ActorAvatar
          actorType="agent"
          actorId={agent.id}
          size="sm"
          className={`shrink-0 ${isArchived ? "opacity-50 grayscale" : ""}`}
          showStatusDot
        />
      }
      title={agent.name}
      muted={isArchived}
      badges={
        <>
          {isPrivate && !isArchived && (
            <Tooltip>
              <TooltipTrigger
                render={<Lock className="h-3 w-3 shrink-0 text-faint-foreground" />}
              />
              <TooltipContent>{VISIBILITY_TOOLTIP.private}</TooltipContent>
            </Tooltip>
          )}
          {isOwnedByMe && <CollectionTag>{t(($) => $.row.you)}</CollectionTag>}
        </>
      }
      description={agent.description || undefined}
    />
  );
}

// Availability dot + label, with the workload folded in as a suffix
// ("Online · 2 tasks") — a 0-2 integer doesn't earn its own column.
function StatusCell({ row }: { row: AgentListRow }) {
  const { t } = useT("agents");
  const { agent, presence } = row;
  if (agent.archived_at) return <span>{t(($) => $.row.archived)}</span>;
  if (!isAgentRuntimeBound(agent)) {
    return (
      <>
        <AlertCircle className="size-3.5 shrink-0 text-amber-500" />
        <span className="truncate text-amber-600 dark:text-amber-400">
          {t(($) => $.row.needs_runtime)}
        </span>
      </>
    );
  }
  if (!presence) return <CollectionEmptyValue />;
  const visual = availabilityConfig[presence.availability];
  const active = presence.runningCount + presence.queuedCount;
  return (
    <>
      <span className={`size-1.5 shrink-0 rounded-full ${visual.dotClass}`} />
      <span className={`truncate ${visual.textClass}`}>
        {t(($) => $.availability[presence.availability])}
        {active > 0 && (
          <span className="text-muted-foreground">
            {" · "}
            {t(($) => $.row.task_count, { count: active })}
          </span>
        )}
      </span>
    </>
  );
}

// Owner = the agent's owner_id, which is set to the creator at creation and
// never transferred (so owner ≡ creator). It carries management rights, so
// the column is "Owner", not "Created by".
function OwnerCell({ row }: { row: AgentListRow }) {
  const { agent, owner } = row;
  if (!agent.owner_id) return <CollectionEmptyValue />;
  return (
    <>
      <ActorAvatar actorType="member" actorId={agent.owner_id} size="sm" />
      <span className="min-w-0 truncate">
        {owner?.name ?? agent.owner_id.slice(0, 8)}
      </span>
    </>
  );
}

// Effective access scope derived from permission_mode + invocation_targets
// (not the lossy derived `visibility`). Text label, not icon-only, so screen
// readers announce the scope.
export function AccessCell({ row }: { row: AgentListRow }) {
  const { t } = useT("agents");
  const scope = useMemo(
    () =>
      effectiveAccessScope(
        row.agent.permission_mode,
        row.agent.invocation_targets,
      ),
    [row.agent.permission_mode, row.agent.invocation_targets],
  );
  const label = t(($) =>
    scope === "workspace"
      ? $.access.scope_labels.workspace
      : scope === "specific-people"
        ? $.access.scope_labels.specific_people
        : $.access.scope_labels.owner_only,
  );
  return <span className="min-w-0 truncate">{label}</span>;
}

function RuntimeCell({ row }: { row: AgentListRow }) {
  const { t } = useT("agents");
  if (!isAgentRuntimeBound(row.agent)) {
    return (
      <span className="truncate text-amber-600 dark:text-amber-400">
        {t(($) => $.row.needs_runtime)}
      </span>
    );
  }
  const runtime = row.runtime;
  if (!runtime) return <CollectionEmptyValue />;
  // Provider mark before the label: scanning this column for "which of these
  // run on Codex" is a shape match, not a read.
  return (
    <>
      <ProviderLogo provider={runtime.provider} className="h-3.5 w-3.5 shrink-0" />
      <span className="min-w-0 truncate">{runtimeDisplayLabel(runtime)}</span>
    </>
  );
}

function LastActiveCell({ row }: { row: AgentListRow }) {
  const { t } = useT("agents");
  const days = row.lastActiveDays;
  if (days === null) {
    return (
      <span className="truncate">
        {row.agent.archived_at ? "—" : t(($) => $.last_active.none)}
      </span>
    );
  }
  return (
    <span className="tabular-nums">
      {days === 0
        ? t(($) => $.last_active.today)
        : t(($) => $.last_active.days_ago, { count: days })}
    </span>
  );
}

// Page

export function AgentsPage(_props: AgentsPageProps = {}) {
  const { t } = useT("agents");
  const locale = useLocale();
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const currentUser = useAuthStore((s) => s.user);

  const {
    data: agents = [],
    isLoading,
    error: listError,
    refetch: refetchList,
  } = useQuery(agentListOptions(wsId));
  const { data: runtimes = [] } = useQuery(
    runtimeListOptions(wsId),
  );
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: runCountsRaw = [], isPending: runCountsPending } = useQuery(
    agentRunCounts30dOptions(wsId),
  );
  const { byAgent: presenceMap, loading: presenceLoading } =
    useWorkspacePresenceMap(wsId);
  const { byAgent: activityMap, loading: activityLoading } =
    useWorkspaceActivityMap(wsId);

  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(
    new Set(),
  );
  const rawScope = useAgentsViewStore((s) => s.scope);
  const scope = AGENT_SCOPES.includes(rawScope) ? rawScope : "mine";
  const setScope = useAgentsViewStore((s) => s.setScope);
  const sortField = useAgentsViewStore((s) => s.sortField);
  const sortDirection = useAgentsViewStore((s) => s.sortDirection);
  const hiddenColumns = useAgentsViewStore((s) => s.hiddenColumns);
  const filters = useAgentsViewStore((s) => s.filters);
  const handleSort = useAgentsViewStore((s) => s.toggleSort);
  const handleSortFieldSelect = useAgentsViewStore((s) => s.setSortField);
  const setSortDirection = useAgentsViewStore((s) => s.setSortDirection);
  const toggleColumn = useAgentsViewStore((s) => s.toggleColumn);
  const toggleFilter = useAgentsViewStore((s) => s.toggleFilter);
  const clearFilters = useAgentsViewStore((s) => s.clearFilters);

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

  const membersById = useMemo(() => {
    const m = new Map<string, MemberWithUser>();
    for (const mem of members) m.set(mem.user_id, mem);
    return m;
  }, [members]);

  const isWorkspaceAdmin = useMemo(() => {
    if (!currentUser) return false;
    const me = members.find((m) => m.user_id === currentUser.id);
    return me?.role === "owner" || me?.role === "admin";
  }, [members, currentUser]);

  // Scope counts come from the FULL set (filters never affect them).
  // Archived ignores the ownership lens (see the view store comment).
  const scopeCounts = useMemo<Record<AgentsScope, number>>(() => {
    let mine = 0;
    let all = 0;
    let archived = 0;
    for (const a of agents) {
      if (a.archived_at) {
        archived++;
        continue;
      }
      all++;
      if (currentUser && a.owner_id === currentUser.id) mine++;
    }
    return { mine, all, archived };
  }, [agents, currentUser]);

  // Rows within the current scope, unfiltered, fully assembled — the
  // filter menu's option lists derive from this; cells never pull their
  // own queries.
  const scopeRows = useMemo<AgentListRow[]>(() => {
    const inScope = agents.filter((a) => {
      if (scope === "archived") return !!a.archived_at;
      if (a.archived_at) return false;
      if (scope === "mine") {
        return !!currentUser && a.owner_id === currentUser.id;
      }
      return true;
    });
    return inScope.map((agent) => {
      const isOwner = !!currentUser?.id && agent.owner_id === currentUser.id;
      const activity = activityMap.get(agent.id) ?? null;
      return {
        agent,
        runtime: runtimesById.get(agent.runtime_id) ?? null,
        presence: presenceMap.get(agent.id) ?? null,
        activity,
        runCount: runCountsById.get(agent.id) ?? 0,
        lastActiveDays: lastActiveDaysAgo(activity),
        owner: agent.owner_id ? membersById.get(agent.owner_id) ?? null : null,
        isOwnedByMe: isOwner,
        canManage: isWorkspaceAdmin || isOwner,
      };
    });
  }, [
    agents,
    scope,
    currentUser,
    runtimesById,
    membersById,
    presenceMap,
    activityMap,
    runCountsById,
    isWorkspaceAdmin,
  ]);

  // Visible rows: filters, then sort.
  const rows = useMemo<AgentListRow[]>(() => {
    const filtered = scopeRows.filter((row) => rowMatchesFilters(row, filters));

    const dir = sortDirection === "asc" ? 1 : -1;
    filtered.sort((a, b) => {
      if (sortField === "name") {
        return a.agent.name.localeCompare(b.agent.name) * dir;
      }
      if (sortField === "runs") {
        return (a.runCount - b.runCount) * dir ||
          a.agent.name.localeCompare(b.agent.name);
      }
      if (sortField === "created") {
        return (
          (Date.parse(a.agent.created_at) - Date.parse(b.agent.created_at)) *
          dir
        );
      }
      // lastActive: smaller daysAgo = more recent. "desc" (the default)
      // means most recently active first; never-active rows sort last in
      // both directions. Run count breaks ties.
      const av = a.lastActiveDays ?? Number.POSITIVE_INFINITY;
      const bv = b.lastActiveDays ?? Number.POSITIVE_INFINITY;
      const byDays = sortDirection === "desc" ? av - bv : bv - av;
      return (
        byDays || b.runCount - a.runCount ||
        a.agent.name.localeCompare(b.agent.name)
      );
    });
    return filtered;
  }, [scopeRows, filters, sortField, sortDirection]);

  const noMatchText = useMemo(() => {
    if (scope === "archived") return t(($) => $.no_matches.no_archived);
    if (countActiveFilterDimensions(filters) > 0) {
      return t(($) => $.no_matches.no_filter_match);
    }
    return t(($) => $.no_matches.title);
  }, [filters, scope, t]);

  // Straight to the manual form: a duplicate already has every field decided,
  // so the method chooser would be a step with nothing to choose.
  const duplicateHref = useCallback(
    (agent: Agent) =>
      `${paths.newAgentManual()}?duplicate=${encodeURIComponent(agent.id)}`,
    [paths],
  );

  const selectedRows = rows.filter((row) => selectedIds.has(row.agent.id));

  const columns = useMemo<CollectionColumn<AgentListRow>[]>(
    () => [
      {
        key: "name",
        header: t(($) => $.columns.agent),
        sortKey: "name",
        cell: (row) => <NameCell row={row} />,
      },
      {
        key: "status",
        // Sized for the worst case "Online · 2 tasks".
        header: t(($) => $.columns.status),
        width: 132,
        core: true,
        hideable: true,
        cell: (row) => <StatusCell row={row} />,
      },
      {
        key: "owner",
        header: t(($) => $.columns.owner),
        width: 132,
        hideable: true,
        cell: (row) => <OwnerCell row={row} />,
      },
      {
        key: "access",
        header: t(($) => $.columns.access),
        width: 120,
        hideable: true,
        cell: (row) => <AccessCell row={row} />,
      },
      {
        key: "runtime",
        header: t(($) => $.columns.runtime),
        width: 152,
        hideable: true,
        cell: (row) => <RuntimeCell row={row} />,
      },
      {
        key: "model",
        header: t(($) => $.columns.model),
        width: 140,
        hideable: true,
        cell: (row) => (
          <TruncatedTooltip content={row.agent.model || undefined}>
            <span className="min-w-0 truncate">{row.agent.model || "—"}</span>
          </TruncatedTooltip>
        ),
      },
      {
        key: "lastActive",
        header: t(($) => $.columns.last_active),
        width: 104,
        sortKey: "lastActive",
        hideable: true,
        cell: (row) => <LastActiveCell row={row} />,
      },
      {
        key: "runs",
        header: t(($) => $.columns.runs),
        width: 48,
        align: "end",
        sortKey: "runs",
        hideable: true,
        className: "font-mono tabular-nums",
        cell: (row) => row.runCount.toLocaleString(locale),
      },
      {
        key: "created",
        header: t(($) => $.columns.created),
        width: 104,
        sortKey: "created",
        hideable: true,
        cell: (row) => (
          <span className="tabular-nums">
            {new Date(row.agent.created_at).toLocaleDateString(locale)}
          </span>
        ),
      },
    ],
    [t, locale],
  );

  const sort: CollectionSort = {
    field: sortField,
    direction: sortDirection,
    onToggle: (field) => handleSort(field as AgentSortField),
    onFieldChange: (field) => handleSortFieldSelect(field as AgentSortField),
    onDirectionChange: setSortDirection,
  };

  const showEmpty = !listError && !isLoading && agents.length === 0;
  const hasNarrowing = countActiveFilterDimensions(filters) > 0;
  const scopeTitles: Record<AgentsScope, string> = {
    mine: t(($) => $.page.scope_title.mine),
    all: t(($) => $.page.scope_title.all),
    archived: t(($) => $.page.scope_title.archived),
  };
  const openCreate = () => navigation.push(paths.newAgent());

  // The active sort field / availability filter reads columns that arrive in
  // separate queries from the main agent list (activity → lastActiveDays,
  // run-counts → runCount, presence → availability). Rendering real rows
  // before those land would sort on placeholder values (lastActiveDays
  // null→Infinity, runCount 0) and visibly re-order the list once each query
  // resolves. Gate the first real paint on exactly the auxiliary queries the
  // current sort / filter depends on — nothing for name/created, run-counts
  // for runs, activity + run-counts (its tiebreaker) for the default
  // lastActive, plus presence whenever an availability filter is active. The
  // queries still run in parallel, so this defers the first paint by at most
  // one extra round-trip (shown as skeleton) and never serialises them. An
  // empty workspace (showEmpty) skips the gate so the empty state is never
  // blocked on the auxiliary queries.
  const needsRunCounts = sortField === "lastActive" || sortField === "runs";
  const needsActivity = sortField === "lastActive";
  const needsPresence = filters.availability.length > 0;
  const listReady =
    (!needsActivity || !activityLoading) &&
    (!needsRunCounts || !runCountsPending) &&
    (!needsPresence || !presenceLoading);

  return (
    // relative: positioning anchor for the batch toolbar (page-centered,
    // not viewport-centered).
    <div className="relative flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={Bot}
        title={scopeTitles[scope]}
        menu={
          <CollectionScopeItems
            value={scope}
            onChange={setScope}
            options={AGENT_SCOPES.map((value) => ({
              value,
              label: scopeTitles[value],
              count: scopeCounts[value],
            }))}
          />
        }
        tools={
          <>
            <AgentListFilterMenu
              filters={filters}
              onToggleFilter={toggleFilter}
              allRows={scopeRows}
              members={members}
            />
            {hasNarrowing ? <CollectionResetButton onClick={clearFilters} /> : null}
          </>
        }
        actions={
          <>
            <CollectionDisplayMenu
              columns={columns}
              hiddenColumns={hiddenColumns}
              onToggleColumn={(key) => toggleColumn(key as AgentColumnKey)}
              sort={sort}
            />
            <CollectionCreateButton
              label={t(($) => $.page.new_agent)}
              onClick={openCreate}
            />
          </>
        }
      />

      {listError ? (
        <ListError listError={listError} onRetry={() => refetchList()} />
      ) : showEmpty ? (
        <div className="flex flex-1 items-center justify-center">
          <EmptyState onCreate={openCreate} />
        </div>
      ) : (
        <CollectionList
          label={scopeTitles[scope]}
          columns={columns}
          hiddenColumns={hiddenColumns}
          rows={rows}
          loading={isLoading || !listReady}
          getRowId={(row) => row.agent.id}
          getRowHref={(row) => paths.agentDetail(row.agent.id)}
          getRowTitle={(row) => row.agent.name}
          rowMenu={(row) => (
            <AgentRowActions
              agent={row.agent}
              presence={row.presence}
              canManage={row.canManage}
              duplicateHref={duplicateHref(row.agent)}
            />
          )}
          sort={sort}
          selection={{ selectedIds, onChange: setSelectedIds }}
          empty={noMatchText}
        />
      )}

      <AgentBatchToolbar
        rows={selectedRows}
        members={members}
        currentUserId={currentUser?.id ?? null}
        onClear={() => setSelectedIds(new Set())}
      />

    </div>
  );
}
