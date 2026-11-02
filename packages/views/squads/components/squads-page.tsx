"use client";

import { useMemo, useState } from "react";
import {
  ExternalLink,
  Loader2,
  Plus,
  Trash2,
  Users,
} from "@multica/ui/icons";
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
  type SquadColumnKey,
  type SquadListFilters,
  type SquadsScope,
  type SquadSortField,
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
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { ActorAvatar as ActorAvatarBase } from "@multica/ui/components/common/actor-avatar";
import { ActorAvatar } from "../../common/actor-avatar";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { useIntentNavigate } from "../../navigation";
import {
  CollectionCreateButton,
  CollectionFilterButton,
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
  CollectionRowMenu,
  type CollectionColumn,
  type CollectionSort,
} from "../../layout/collection-list";
import { useLocale, useT } from "../../i18n";

// Cells

function SquadAvatar({ squad }: { squad: Squad }) {
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
        size="sm"
        className="shrink-0"
      />
    );
  }
  return (
    <ActorAvatarBase
      name={squad.name}
      initials={initials}
      isSquad
      size="sm"
      className="shrink-0"
    />
  );
}

function LeaderCell({
  leaderId,
  leader,
}: {
  leaderId: string;
  leader: Agent | undefined;
}) {
  return (
    <>
      <ActorAvatar actorType="agent" actorId={leaderId} size="sm" />
      <span className="min-w-0 truncate">{leader?.name ?? leaderId.slice(0, 8)}</span>
    </>
  );
}

// Polymorphic member avatar stack (agent + human members), driven by the
// list payload's member_preview / member_count. NOT AgentAvatarStack, which
// is agent-only.
function MembersCell({ squad }: { squad: Squad }) {
  const preview = squad.member_preview ?? [];
  const count = squad.member_count ?? preview.length;
  if (count === 0) return <CollectionEmptyValue />;
  const visible = preview.slice(0, 3);
  const overflow = count - visible.length;
  return (
    <div className="flex items-center -space-x-1.5">
        {visible.map((m) => (
          <span
            key={`${m.member_type}-${m.member_id}`}
            className="inline-flex rounded-full ring-2 ring-background"
          >
            <ActorAvatar
              actorType={m.member_type}
              actorId={m.member_id}
              size="sm"
              enableHoverCard={m.member_type === "agent"}
            />
          </span>
        ))}
        {overflow > 0 && (
          <span className="inline-flex size-5 items-center justify-center rounded-full bg-muted text-micro font-medium text-muted-foreground ring-2 ring-background">
            +{overflow}
          </span>
        )}
    </div>
  );
}

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

function SquadRowActions({ squad }: { squad: Squad }) {
  const { t } = useT("squads");
  const { t: tCommon } = useT("common");
  const p = useWorkspacePaths();
  const intentNavigate = useIntentNavigate();
  const [archiveOpen, setArchiveOpen] = useState(false);
  return (
    <>
      <CollectionRowMenu label={t(($) => $.page.row_menu)} contentClassName="w-40">
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
      </CollectionRowMenu>
      <ArchiveSquadDialog
        squad={squad}
        open={archiveOpen}
        onOpenChange={setArchiveOpen}
      />
    </>
  );
}

// Toolbar

interface ActorOption {
  id: string;
  name: string;
  count: number;
}

function countActiveFilters(filters: SquadListFilters): number {
  return (
    (filters.leaders.length > 0 ? 1 : 0) +
    (filters.creators.length > 0 ? 1 : 0)
  );
}

function SquadFilterMenu({
  filters,
  onToggleFilter,
  leaderOptions,
  creatorOptions,
}: {
  filters: SquadListFilters;
  onToggleFilter: (key: keyof SquadListFilters, value: string) => void;
  leaderOptions: ActorOption[];
  creatorOptions: ActorOption[];
}) {
  const { t } = useT("squads");
  const countBadge = (n: number) => (
    <span className="ml-auto pl-3 text-caption text-muted-foreground">{n}</span>
  );

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={<CollectionFilterButton count={countActiveFilters(filters)} />}
      />
      <DropdownMenuContent align="start" className="w-auto">
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
  );
}

// Page

export function SquadsPage() {
  const { t } = useT("squads");
  const locale = useLocale();
  const workspace = useCurrentWorkspace();
  const wsId = workspace?.id ?? "";
  const p = useWorkspacePaths();
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
  const hiddenColumns = useSquadsViewStore((s) => s.hiddenColumns);
  const handleSort = useSquadsViewStore((s) => s.toggleSort);
  const handleSortFieldSelect = useSquadsViewStore((s) => s.setSortField);
  const setSortDirection = useSquadsViewStore((s) => s.setSortDirection);
  const toggleColumn = useSquadsViewStore((s) => s.toggleColumn);
  const filters = useSquadsViewStore((s) => s.filters);
  const toggleFilter = useSquadsViewStore((s) => s.toggleFilter);
  const clearFilters = useSquadsViewStore((s) => s.clearFilters);

  const hasActiveFilters = countActiveFilters(filters) > 0;
  const openCreateSquad = () => useModalStore.getState().open("create-squad");

  const scopeLabels: Record<SquadsScope, string> = {
    mine: t(($) => $.scope.mine),
    all: t(($) => $.scope.all),
  };

  const scopeCounts = useMemo<Record<SquadsScope, number>>(() => {
    let mine = 0;
    if (currentUser) {
      for (const s of squads) if (s.creator_id === currentUser.id) mine++;
    }
    return { mine, all: squads.length };
  }, [squads, currentUser]);

  // Rows within the current scope, unfiltered — the filter option lists
  // derive from this.
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

  const canManage = (squad: Squad) =>
    isWorkspaceAdmin || (!!currentUser && squad.creator_id === currentUser.id);

  const columns = useMemo<CollectionColumn<Squad>[]>(
    () => [
      {
        key: "name",
        header: t(($) => $.page.table.name),
        sortKey: "name",
        cell: (squad) => (
          <CollectionNameCell
            icon={<SquadAvatar squad={squad} />}
            title={squad.name}
            description={squad.description}
          />
        ),
      },
      {
        key: "leader",
        header: t(($) => $.page.table.leader),
        width: 160,
        core: true,
        cell: (squad) => (
          <LeaderCell leaderId={squad.leader_id} leader={agentsById.get(squad.leader_id)} />
        ),
      },
      {
        key: "members",
        header: t(($) => $.page.table.members),
        width: 120,
        sortKey: "members",
        hideable: true,
        cell: (squad) => <MembersCell squad={squad} />,
      },
      {
        key: "creator",
        header: t(($) => $.page.table.creator),
        width: 144,
        hideable: true,
        cell: (squad) => (
          <>
            <ActorAvatar actorType="member" actorId={squad.creator_id} size="sm" />
            <span className="min-w-0 truncate">
              {membersById.get(squad.creator_id)?.name ?? squad.creator_id.slice(0, 8)}
            </span>
          </>
        ),
      },
      {
        key: "created",
        header: t(($) => $.page.table.created),
        width: 104,
        sortKey: "created",
        hideable: true,
        cell: (squad) => (
          <span className="tabular-nums">
            {new Date(squad.created_at).toLocaleDateString(locale)}
          </span>
        ),
      },
    ],
    [t, locale, agentsById, membersById],
  );

  const sort: CollectionSort = {
    field: sortField,
    direction: sortDirection,
    onToggle: (field) => handleSort(field as SquadSortField),
    onFieldChange: (field) => handleSortFieldSelect(field as SquadSortField),
    onDirectionChange: setSortDirection,
  };

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={Users}
        title={scopeLabels[scope]}
        menu={
          <CollectionScopeItems
            value={scope}
            onChange={setScope}
            options={SQUAD_SCOPES.map((s) => ({
              value: s,
              label: scopeLabels[s],
              count: scopeCounts[s],
            }))}
          />
        }
        tools={
          <>
            <SquadFilterMenu
              filters={filters}
              onToggleFilter={toggleFilter}
              leaderOptions={leaderOptions}
              creatorOptions={creatorOptions}
            />
            {hasActiveFilters ? <CollectionResetButton onClick={clearFilters} /> : null}
          </>
        }
        actions={
          <>
            <CollectionDisplayMenu
              columns={columns}
              hiddenColumns={hiddenColumns}
              onToggleColumn={(key) => toggleColumn(key as SquadColumnKey)}
              sort={sort}
            />
            <CollectionCreateButton
              label={t(($) => $.page.new_button)}
              onClick={openCreateSquad}
            />
          </>
        }
      />

      {!isLoading && squads.length === 0 ? (
        <CollectionPageState
          icon={Users}
          title={t(($) => $.page.empty_no_squads)}
          actions={
            <Button size="sm" variant="outline" onClick={openCreateSquad}>
              <Plus aria-hidden="true" className="size-3.5" />
              {t(($) => $.page.new_button)}
            </Button>
          }
        />
      ) : (
        <CollectionList
          label={scopeLabels[scope]}
          columns={columns}
          hiddenColumns={hiddenColumns}
          rows={rows}
          loading={isLoading}
          getRowId={(squad) => squad.id}
          getRowHref={(squad) => p.squadDetail(squad.id)}
          getRowTitle={(squad) => squad.name}
          rowMenu={
            canManageAnyRow
              ? (squad) => (canManage(squad) ? <SquadRowActions squad={squad} /> : null)
              : undefined
          }
          sort={sort}
          empty={t(($) => $.page.no_matches)}
        />
      )}
    </div>
  );
}
