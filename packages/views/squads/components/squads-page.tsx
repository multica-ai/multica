"use client";

import { useMemo, useState } from "react";
import {
  ArrowDown,
  ArrowUp,
  ChevronDown,
  ExternalLink,
  Filter,
  Loader2,
  MoreHorizontal,
  Plus,
  Trash2,
  Users,
  X,
} from "lucide-react";
import { useQuery, useQueryClient, useMutation } from "@tanstack/react-query";
import { toast } from "sonner";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  squadListOptions,
  workspaceKeys,
} from "@multica/core/workspace/queries";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import { useModalStore } from "@multica/core/modals";
import {
  useSquadsViewStore,
  SQUAD_SCOPES,
  type SquadListFilters,
  type SquadsScope,
  type SquadSortField,
  type SquadSortDirection,
} from "@multica/core/squads/stores";
import type { Agent, MemberWithUser, Squad } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { ActorAvatar } from "../../common/actor-avatar";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { useIntentNavigate } from "../../navigation";
import {
  CollectionPageHeader,
  CollectionPageHeaderAction,
  CollectionPageState,
} from "../../layout/collection-page";
import { useLocale, useT } from "../../i18n";
import { PAGE_TOOLBAR } from "../../layout/page-header";
import { SquadEnhancedRows } from "./squad-enhanced-rows";

// Cells

export function SquadAvatar({ squad }: { squad: Squad }) {
  const initials = squad.name
    .split(" ")
    .map((w) => w[0])
    .join("")
    .toUpperCase()
    .slice(0, 2);
  if (squad.avatar_url) {
    return (
      <ActorAvatarBase
        name={squad.name}
        initials={initials}
        avatarUrl={resolvePublicFileUrl(squad.avatar_url)}
        size="lg"
        className="shrink-0"
      />
    );
  }
  return (
    <ActorAvatarBase
      name={squad.name}
      initials={initials}
      isSquad
      size="lg"
      className="shrink-0"
    />
  );
}

// ---------------------------------------------------------------------------
// Archive (= delete) dialog — reuses the existing archive_dialog copy.
// Workspace owner/admin only (backend gate). No restore endpoint exists, so
// once archived a squad is gone from the UI.

function ArchiveSquadDialog({
  squad,
  open,
  onOpenChange,
}: {
  squad: Squad;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { t } = useT("squads");
  const wsId = useCurrentWorkspace()?.id ?? "";
  const qc = useQueryClient();
  const archive = useMutation({
    mutationFn: () => api.deleteSquad(squad.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.squads(wsId) });
      onOpenChange(false);
      toast.success(t(($) => $.archive_dialog.success));
    },
    onError: (err) =>
      toast.error(err instanceof Error ? err.message : String(err)),
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t(($) => $.archive_dialog.title)}</DialogTitle>
          <DialogDescription>
            {t(($) => $.archive_dialog.description, { name: squad.name })}
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={archive.isPending}
            onClick={() => onOpenChange(false)}
          >
            {t(($) => $.archive_dialog.cancel)}
          </Button>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            disabled={archive.isPending}
            onClick={() => archive.mutate()}
          >
            {archive.isPending ? (
              <>
                <Loader2 className="mr-1 size-3.5 animate-spin" />
                {t(($) => $.archive_dialog.archiving)}
              </>
            ) : (
              t(($) => $.archive_dialog.confirm)
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function SquadRowActions({ squad }: { squad: Squad }) {
  const { t } = useT("squads");
  const { t: tCommon } = useT("common");
  const p = useWorkspacePaths();
  const intentNavigate = useIntentNavigate();
  const [archiveOpen, setArchiveOpen] = useState(false);
  return (
    <span
      onClick={(e) => e.stopPropagation()}
      className="flex items-center"
    >
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <button
              type="button"
              aria-label={t(($) => $.page.row_menu)}
              className="flex size-7 items-center justify-center rounded-md text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-accent-foreground group-hover/row:opacity-100 data-popup-open:bg-accent data-popup-open:opacity-100 data-popup-open:text-accent-foreground"
            >
              <MoreHorizontal className="size-4" />
            </button>
          }
        />
        <DropdownMenuContent align="end" className="w-40">
          <DropdownMenuItem
            onClick={() =>
              intentNavigate(
                p.squadDetail(squad.id),
                "foreground-tab",
                squad.name,
              )
            }
          >
            <ExternalLink className="size-3.5" />
            {tCommon(($) => $.navigation.open_in_new_tab)}
          </DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            variant="destructive"
            onClick={() => setArchiveOpen(true)}
          >
            <Trash2 className="size-3.5" />
            {t(($) => $.page.archive_action)}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <ArchiveSquadDialog
        squad={squad}
        open={archiveOpen}
        onOpenChange={setArchiveOpen}
      />
    </span>
  );
}

// Header + toolbar

const SORT_FIELDS: SquadSortField[] = ["name", "members", "created"];

interface ActorOption {
  id: string;
  name: string;
  count: number;
}

function SquadListToolbar({
  scope,
  onScopeChange,
  scopeCounts,
  filters,
  onToggleFilter,
  onClearFilters,
  leaderOptions,
  creatorOptions,
  visibleCount,
  totalCount,
  sortField,
  sortDirection,
  onSortFieldChange,
  onSortDirectionChange,
}: {
  scope: SquadsScope;
  onScopeChange: (scope: SquadsScope) => void;
  scopeCounts: Record<SquadsScope, number>;
  filters: SquadListFilters;
  onToggleFilter: (key: keyof SquadListFilters, value: string) => void;
  onClearFilters: () => void;
  leaderOptions: ActorOption[];
  creatorOptions: ActorOption[];
  visibleCount: number;
  totalCount: number;
  sortField: SquadSortField;
  sortDirection: SquadSortDirection;
  onSortFieldChange: (field: SquadSortField) => void;
  onSortDirectionChange: (direction: SquadSortDirection) => void;
}) {
  const { t } = useT("squads");
  const activeFilterCount =
    (filters.leaders.length > 0 ? 1 : 0) +
    (filters.creators.length > 0 ? 1 : 0);
  const hasActiveFilters = activeFilterCount > 0;
  const countBadge = (n: number) => (
    <span className="ml-auto pl-3 text-caption text-muted-foreground">{n}</span>
  );
  const SCOPE_LABELS: Record<SquadsScope, string> = {
    mine: t(($) => $.scope.mine),
    all: t(($) => $.scope.all),
  };
  const SORT_LABELS: Record<SquadSortField, string> = {
    name: t(($) => $.page.table.name),
    members: t(($) => $.page.table.members),
    created: t(($) => $.page.table.created),
  };
  const sortLabel = SORT_LABELS[sortField];

  return (
    <div className={PAGE_TOOLBAR}>
      <div className="flex min-w-0 items-center gap-2">
        <div className="hidden shrink-0 items-center gap-1 md:flex">
          {SQUAD_SCOPES.map((s) => (
            <Button
              key={s}
              variant="outline"
              size="sm"
              className={
                scope === s
                  ? "gap-1.5 bg-accent text-accent-foreground hover:bg-accent/80"
                  : "gap-1.5 text-muted-foreground"
              }
              onClick={() => onScopeChange(s)}
            >
              {SCOPE_LABELS[s]}
              <span className="tabular-nums text-caption text-muted-foreground">
                {scopeCounts[s]}
              </span>
            </Button>
          ))}
        </div>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="outline"
                size="sm"
                className="shrink-0 gap-1 text-muted-foreground md:hidden"
              >
                <span className="truncate">{SCOPE_LABELS[scope]}</span>
                <ChevronDown className="size-3 text-muted-foreground" />
              </Button>
            }
          />
          <DropdownMenuContent align="start" className="w-auto">
            <DropdownMenuRadioGroup
              value={scope}
              onValueChange={(value) => onScopeChange(value as SquadsScope)}
            >
              {SQUAD_SCOPES.map((s) => (
                <DropdownMenuRadioItem key={s} value={s}>
                  {SCOPE_LABELS[s]}
                  <span className="ml-2 tabular-nums text-caption text-muted-foreground">
                    {scopeCounts[s]}
                  </span>
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        {hasActiveFilters && (
          <span
            title={t(($) => $.toolbar.result_count_title)}
            className="hidden shrink-0 text-caption tabular-nums text-muted-foreground md:inline"
          >
            {visibleCount} / {totalCount}
          </span>
        )}
      </div>

      <div className="flex shrink-0 items-center gap-1">
      {/* Filter */}
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <Button
              variant={hasActiveFilters ? "default" : "outline"}
              size="sm"
              className={
                hasActiveFilters
                  ? "h-8 w-8 gap-1 bg-brand px-0 text-white hover:bg-brand/90 md:w-auto md:px-2.5"
                  : "h-8 w-8 gap-1 px-0 text-muted-foreground md:w-auto md:px-2.5"
              }
            >
              <Filter className="size-3.5" />
              {hasActiveFilters ? (
                <>
                  <span className="hidden md:inline">
                    {t(($) => $.toolbar.filter_active_count, { count: activeFilterCount })}
                  </span>
                  <span className="tabular-nums md:hidden">{activeFilterCount}</span>
                </>
              ) : (
                <span className="hidden md:inline">{t(($) => $.toolbar.filter_label)}</span>
              )}
              {hasActiveFilters && (
                <span
                  role="button"
                  tabIndex={-1}
                  aria-label={t(($) => $.toolbar.clear_filters)}
                  className="-mr-1 ml-0.5 hidden rounded-sm p-0.5 hover:bg-white/20 md:inline-flex"
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    onClearFilters();
                  }}
                  onPointerDown={(e) => e.stopPropagation()}
                >
                  <X className="size-3" />
                </span>
              )}
            </Button>
          }
        />
        <DropdownMenuContent align="end" className="w-auto">
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <span className="flex-1">{t(($) => $.page.table.leader)}</span>
              {filters.leaders.length > 0 && (
                <span className="text-caption font-medium text-primary">{filters.leaders.length}</span>
              )}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
              {leaderOptions.map((o) => (
                <DropdownMenuCheckboxItem
                  key={o.id}
                  checked={filters.leaders.includes(o.id)}
                  onCheckedChange={() => onToggleFilter("leaders", o.id)}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={filters.leaders.includes(o.id)} />
                  <ActorAvatar actorType="agent" actorId={o.id} size="sm" />
                  <span className="min-w-0 truncate">{o.name}</span>
                  {countBadge(o.count)}
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <span className="flex-1">{t(($) => $.page.table.creator)}</span>
              {filters.creators.length > 0 && (
                <span className="text-caption font-medium text-primary">{filters.creators.length}</span>
              )}
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
              {creatorOptions.map((o) => (
                <DropdownMenuCheckboxItem
                  key={o.id}
                  checked={filters.creators.includes(o.id)}
                  onCheckedChange={() => onToggleFilter("creators", o.id)}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={filters.creators.includes(o.id)} />
                  <ActorAvatar actorType="member" actorId={o.id} size="sm" />
                  <span className="min-w-0 truncate">{o.name}</span>
                  {countBadge(o.count)}
                </DropdownMenuCheckboxItem>
              ))}
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        </DropdownMenuContent>
      </DropdownMenu>

      {/* Display settings */}
      <Popover>
        <Tooltip>
          <PopoverTrigger
            render={
              <TooltipTrigger
                render={
                  <Button
                    variant="outline"
                    size="sm"
                    className="h-8 w-8 gap-1 px-0 text-muted-foreground md:w-auto md:px-2.5"
                  >
                    {sortDirection === "asc" ? (
                      <ArrowUp className="size-3.5" />
                    ) : (
                      <ArrowDown className="size-3.5" />
                    )}
                    <span className="hidden md:inline">{sortLabel}</span>
                  </Button>
                }
              />
            }
          />
          <TooltipContent side="bottom">
            {t(($) => $.toolbar.display)}
          </TooltipContent>
        </Tooltip>
        <PopoverContent align="end" className="w-64 p-0">
          <div className="border-b px-3 py-2.5">
            <span className="text-caption font-medium text-muted-foreground">
              {t(($) => $.toolbar.sort_by)}
            </span>
            <div className="mt-2 flex items-center gap-1.5">
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={
                    <Button
                      variant="outline"
                      size="sm"
                      className="flex-1 justify-between text-caption"
                    >
                      {sortLabel}
                      <ChevronDown className="size-3 text-muted-foreground" />
                    </Button>
                  }
                />
                <DropdownMenuContent align="start" className="w-auto">
                  <DropdownMenuRadioGroup
                    value={sortField}
                    onValueChange={(v) =>
                      onSortFieldChange(v as SquadSortField)
                    }
                  >
                    {SORT_FIELDS.map((field) => (
                      <DropdownMenuRadioItem key={field} value={field}>
                        {SORT_LABELS[field]}
                      </DropdownMenuRadioItem>
                    ))}
                  </DropdownMenuRadioGroup>
                </DropdownMenuContent>
              </DropdownMenu>
              <Button
                variant="outline"
                size="icon-sm"
                onClick={() =>
                  onSortDirectionChange(
                    sortDirection === "asc" ? "desc" : "asc",
                  )
                }
                title={
                  sortDirection === "asc"
                    ? t(($) => $.toolbar.direction_asc)
                    : t(($) => $.toolbar.direction_desc)
                }
              >
                {sortDirection === "asc" ? (
                  <ArrowUp className="size-3.5" />
                ) : (
                  <ArrowDown className="size-3.5" />
                )}
              </Button>
            </div>
          </div>
        </PopoverContent>
      </Popover>
      </div>
    </div>
  );
}

// Page

export function SquadsPage() {
  const { t } = useT("squads");
  const locale = useLocale();
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const currentUser = useAuthStore((s) => s.user);

  const { data: squads = [], isLoading } = useQuery({
    ...squadListOptions(wsId),
    enabled: !!wsId,
  });
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));

  const agentsById = useMemo(() => {
    const m = new Map<string, Agent>();
    for (const a of agents) m.set(a.id, a);
    return m;
  }, [agents]);

  const membersById = useMemo(() => {
    const m = new Map<string, MemberWithUser>();
    for (const mem of members) m.set(mem.user_id, mem);
    return m;
  }, [members]);

  const isWorkspaceAdmin = useMemo(() => {
    if (!currentUser) return false;
    const me = members.find((mem: MemberWithUser) => mem.user_id === currentUser.id);
    return me?.role === "owner" || me?.role === "admin";
  }, [members, currentUser]);

  const scope = useSquadsViewStore((s) => s.scope);
  const setScope = useSquadsViewStore((s) => s.setScope);
  const sortField = useSquadsViewStore((s) => s.sortField);
  const sortDirection = useSquadsViewStore((s) => s.sortDirection);
  const handleSortFieldSelect = useSquadsViewStore((s) => s.setSortField);
  const setSortDirection = useSquadsViewStore((s) => s.setSortDirection);
  const filters = useSquadsViewStore((s) => s.filters);
  const toggleFilter = useSquadsViewStore((s) => s.toggleFilter);
  const clearFilters = useSquadsViewStore((s) => s.clearFilters);

  const scopeCounts = useMemo<Record<SquadsScope, number>>(() => {
    let mine = 0;
    if (currentUser) {
      for (const s of squads) if (s.creator_id === currentUser.id) mine++;
    }
    return { mine, all: squads.length };
  }, [squads, currentUser]);

  // Rows within the current scope, unfiltered — filter option lists + the
  // "n / total" denominator derive from this.
  const scopeRows = useMemo<Squad[]>(() => {
    return squads.filter((s) => {
      if (scope === "mine") {
        return !!currentUser && s.creator_id === currentUser.id;
      }
      return true;
    });
  }, [squads, scope, currentUser]);

  const leaderOptions = useMemo(() => {
    const m = new Map<string, { id: string; name: string; count: number }>();
    for (const s of scopeRows) {
      const e = m.get(s.leader_id);
      if (e) e.count += 1;
      else
        m.set(s.leader_id, {
          id: s.leader_id,
          name: agentsById.get(s.leader_id)?.name ?? s.leader_id.slice(0, 8),
          count: 1,
        });
    }
    return [...m.values()];
  }, [scopeRows, agentsById]);

  const creatorOptions = useMemo(() => {
    const m = new Map<string, { id: string; name: string; count: number }>();
    for (const s of scopeRows) {
      const e = m.get(s.creator_id);
      if (e) e.count += 1;
      else
        m.set(s.creator_id, {
          id: s.creator_id,
          name: membersById.get(s.creator_id)?.name ?? s.creator_id.slice(0, 8),
          count: 1,
        });
    }
    return [...m.values()];
  }, [scopeRows, membersById]);

  const rows = useMemo<Squad[]>(() => {
    const inScope = scopeRows.filter((s) => {
      if (filters.leaders.length > 0 && !filters.leaders.includes(s.leader_id)) {
        return false;
      }
      if (
        filters.creators.length > 0 &&
        !filters.creators.includes(s.creator_id)
      ) {
        return false;
      }
      return true;
    });
    const dir = sortDirection === "asc" ? 1 : -1;
    const sorted = [...inScope];
    sorted.sort((a, b) => {
      if (sortField === "members") {
        const av = a.member_count ?? a.member_preview?.length ?? 0;
        const bv = b.member_count ?? b.member_preview?.length ?? 0;
        return (av - bv) * dir || a.name.localeCompare(b.name);
      }
      if (sortField === "created") {
        return (
          (Date.parse(a.created_at) - Date.parse(b.created_at)) * dir
        );
      }
      return a.name.localeCompare(b.name) * dir;
    });
    return sorted;
  }, [scopeRows, filters, sortField, sortDirection]);

  // Reserve the row-actions (kebab) track when the current user can manage at
  // least one visible squad. Workspace admins manage all squads; a regular
  // member manages the squads they created (MUL-4223).
  const canManageAnyRow = useMemo(
    () =>
      isWorkspaceAdmin ||
      (!!currentUser && rows.some((s) => s.creator_id === currentUser.id)),
    [isWorkspaceAdmin, rows, currentUser],
  );

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={Users}
        title={t(($) => $.page.title)}
        count={squads.length}
        actions={
          <CollectionPageHeaderAction
            icon={Plus}
            label={t(($) => $.page.new_button)}
            onClick={() => useModalStore.getState().open("create-squad")}
          />
        }
      />

      {isLoading ? (
        <LoadingSkeleton />
      ) : squads.length === 0 ? (
        <CollectionPageState
          icon={Users}
          title={t(($) => $.page.empty_no_squads)}
          actions={
            <Button
              size="sm"
              onClick={() => useModalStore.getState().open("create-squad")}
            >
              <Plus aria-hidden="true" className="size-3.5" />
              {t(($) => $.page.new_button)}
            </Button>
          }
        />
      ) : (
        <>
          <SquadListToolbar
            scope={scope}
            onScopeChange={setScope}
            scopeCounts={scopeCounts}
            filters={filters}
            onToggleFilter={toggleFilter}
            onClearFilters={clearFilters}
            leaderOptions={leaderOptions}
            creatorOptions={creatorOptions}
            visibleCount={rows.length}
            totalCount={scopeRows.length}
            sortField={sortField}
            sortDirection={sortDirection}
            onSortFieldChange={handleSortFieldSelect}
            onSortDirectionChange={setSortDirection}
          />
          <div className="min-h-0 flex-1 overflow-auto p-1">
            {rows.length === 0 ? (
              <div className="py-16 text-center text-body text-muted-foreground">
                {t(($) => $.page.no_matches)}
              </div>
            ) : (
              <SquadEnhancedRows
                squads={rows}
                agentsById={agentsById}
                membersById={membersById}
                locale={locale}
                isWorkspaceAdmin={isWorkspaceAdmin}
                currentUserId={currentUser?.id}
                canManageAnyRow={canManageAnyRow}
              />
            )}
          </div>
        </>
      )}
    </div>
  );
}

function LoadingSkeleton() {
  return (
    <div className="flex flex-col gap-2 p-1">
      {Array.from({ length: 4 }).map((_, i) => (
        <div
          key={i}
          className="grid items-center gap-4 rounded-lg border border-surface-border bg-surface px-5 py-3.5"
        >
          <div className="grid w-full grid-cols-[56px_minmax(220px,1.2fr)_auto_auto_minmax(140px,auto)] items-center gap-4">
            <Skeleton className="size-8 rounded-full" />
            <div className="flex flex-col gap-1.5">
              <Skeleton className="h-3.5 w-32 max-w-full" />
              <Skeleton className="h-3 w-48 max-w-full" />
            </div>
            <div className="flex items-center gap-2">
              <Skeleton className="size-8 rounded-full" />
              <div className="flex flex-col gap-1">
                <Skeleton className="h-2.5 w-10" />
                <Skeleton className="h-3 w-16" />
              </div>
            </div>
            <div className="flex items-center -space-x-1.5">
              <Skeleton className="size-5 rounded-full ring-2 ring-surface" />
              <Skeleton className="size-5 rounded-full ring-2 ring-surface" />
              <Skeleton className="size-5 rounded-full ring-2 ring-surface" />
            </div>
            <div className="flex items-center justify-end gap-3">
              <div className="flex flex-col items-end gap-1">
                <div className="flex items-center gap-1">
                  <Skeleton className="size-4 rounded-full" />
                  <Skeleton className="h-2.5 w-12" />
                </div>
                <Skeleton className="h-2.5 w-16" />
              </div>
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}
