"use client";

import { useMemo, useState } from "react";
import {
  AlertCircle,
  AlertTriangle,
  Download,
  HardDrive,
  Lock,
  Pencil,
} from "@multica/ui/icons";
import { SkillIcon } from "../lib/skill-icon";
import type {
  Agent,
  AgentRuntime,
  MemberWithUser,
  Skill,
  SkillSummary,
} from "@multica/core/types";
import { useQuery } from "@tanstack/react-query";
import { useAuthStore } from "@multica/core/auth";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  agentListOptions,
  memberListOptions,
  selectSkillAssignments,
  skillListOptions,
} from "@multica/core/workspace/queries";
import { runtimeDisplayLabel, runtimeListOptions } from "@multica/core/runtimes";
import { resolvePublicFileUrl } from "@multica/core/workspace/avatar-url";
import { Button } from "@multica/ui/components/ui/button";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { rowLinkInteractiveProps, useNavigation } from "../../navigation";
import {
  CollectionPageHeader,
  CollectionPageState,
  CollectionResetButton,
  CollectionScopeItems,
} from "../../layout/collection-page";
import {
  CollectionDisplayMenu,
  CollectionList,
  CollectionNameCell,
  type CollectionColumn,
  type CollectionSort,
} from "../../layout/collection-list";
import { canEditSkill } from "../hooks/use-can-edit-skill";
import { originSourceUrl, readOrigin } from "../lib/origin";
import { rowMatchesFilters, type SkillRow } from "./skill-list-filter";
import {
  CreateSkillDialog,
  type CreateSkillMethod,
} from "./create-skill-dialog";
import {
  useSkillsViewStore,
  type SkillColumnKey,
  type SkillSortField,
} from "@multica/core/skills/stores";
import {
  SkillCreateMenu,
  SkillFilterMenu,
  countActiveFilterDimensions,
} from "./skill-list-controls";
import {
  SkillBatchToolbar,
  SkillRowActions,
  type SkillActionsContext,
} from "./skill-list-actions";
import { useT, useTimeAgo } from "../../i18n";

// Sort/filter/column types and defaults live in the core view store
// (@multica/core/skills/stores/view-store) so the persisted state and the
// UI share one definition. Re-exported here for the toolbar's convenience.
export type SortField = SkillSortField;
export { rowMatchesFilters, type SkillRow } from "./skill-list-filter";

type SkillScope = "all" | "mine";

// Cells

function NameCell({ row }: { row: SkillRow }) {
  const { t } = useT("skills");
  const { skill, canEdit } = row;
  return (
    <CollectionNameCell
      icon={<SkillIcon className="size-4 shrink-0 text-muted-foreground" />}
      title={skill.name}
      badges={
        !canEdit && (
          <Tooltip>
            <TooltipTrigger
              render={<Lock className="h-3 w-3 shrink-0 text-faint-foreground" />}
            />
            <TooltipContent>{t(($) => $.table.lock_tooltip)}</TooltipContent>
          </Tooltip>
        )
      }
      description={skill.description || undefined}
    />
  );
}

function UsedByCell({ agents }: { agents: Agent[] }) {
  const { t } = useT("skills");
  if (agents.length === 0) return <span>{t(($) => $.table.unused)}</span>;
  const soleAgent = agents.length === 1 ? agents[0] : undefined;
  if (soleAgent) {
    const agent = soleAgent;
    return (
      <>
        <ActorAvatar
          name={agent.name}
          initials={agent.name.slice(0, 2).toUpperCase()}
          avatarUrl={resolvePublicFileUrl(agent.avatar_url)}
          isAgent
          size="sm"
        />
        <span className="min-w-0 truncate">{agent.name}</span>
      </>
    );
  }
  const visible = agents.slice(0, 3);
  const extra = agents.length - visible.length;
  return (
    <div className="flex items-center -space-x-1.5">
        {visible.map((a) => (
          <Tooltip key={a.id}>
            <TooltipTrigger
              render={
                <span className="inline-flex rounded-full ring-2 ring-background">
                  <ActorAvatar
                    name={a.name}
                    initials={a.name.slice(0, 2).toUpperCase()}
                    avatarUrl={resolvePublicFileUrl(a.avatar_url)}
                    isAgent
                    size="sm"
                  />
                </span>
              }
            />
            <TooltipContent>{a.name}</TooltipContent>
          </Tooltip>
        ))}
        {extra > 0 && (
          <span className="inline-flex size-5 items-center justify-center rounded-full bg-muted text-micro font-medium text-muted-foreground ring-2 ring-background">
            +{extra}
          </span>
        )}
    </div>
  );
}

function SourceCell({
  skill,
  runtime,
}: {
  skill: SkillSummary;
  runtime: AgentRuntime | null;
}) {
  const { t } = useT("skills");
  const origin = readOrigin(skill);

  let icon = <Pencil className="h-3 w-3 shrink-0" />;
  let label: string = t(($) => $.table.source_manual);
  if (origin.type === "runtime_local") {
    icon = <HardDrive className="h-3 w-3 shrink-0" />;
    label = runtime
      ? t(($) => $.table.source_runtime_named, { name: runtimeDisplayLabel(runtime) })
      : origin.provider
        ? t(($) => $.table.source_runtime_provider, {
            provider: origin.provider,
          })
        : t(($) => $.table.source_runtime_unknown);
  } else if (origin.type === "clawhub") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = t(($) => $.table.source_clawhub);
  } else if (origin.type === "skills_sh") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = t(($) => $.table.source_skills_sh);
  } else if (origin.type === "github") {
    icon = <Download className="h-3 w-3 shrink-0" />;
    label = t(($) => $.table.source_github);
  }

  // Imported skills link to their upstream page; the anchor must not bubble
  // its click OR auxclick into the row's whole-row navigation.
  const sourceUrl = originSourceUrl(origin);

  return (
    <>
      {icon}
      {sourceUrl ? (
        <Tooltip>
          <TooltipTrigger
            render={
              <a
                href={sourceUrl}
                target="_blank"
                rel="noopener noreferrer"
                {...rowLinkInteractiveProps}
                className="min-w-0 truncate hover:underline"
              >
                {label}
              </a>
            }
          />
          <TooltipContent side="top">{sourceUrl}</TooltipContent>
        </Tooltip>
      ) : (
        <span className="min-w-0 truncate">{label}</span>
      )}
    </>
  );
}

function CreatorCell({ creator }: { creator: MemberWithUser | null }) {
  if (!creator) return null;
  return (
    <>
      <ActorAvatar
        name={creator.name}
        initials={creator.name.slice(0, 2).toUpperCase()}
        avatarUrl={resolvePublicFileUrl(creator.avatar_url)}
        size="sm"
      />
      <span className="min-w-0 truncate">{creator.name}</span>
    </>
  );
}

// Empty state

function EmptyState() {
  const { t } = useT("skills");
  return (
    <CollectionPageState
      icon={SkillIcon}
      title={t(($) => $.page.empty.title)}
      description={t(($) => $.page.empty.description)}
    />
  );
}

// Page

export default function SkillsPage() {
  const { t } = useT("skills");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const timeAgo = useTimeAgo();
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);

  const {
    data: skills = [],
    isLoading,
    error: listError,
    refetch: refetchList,
  } = useQuery(skillListOptions(wsId));
  const { data: agents = [], error: agentsError } = useQuery(
    agentListOptions(wsId),
  );
  const { data: members = [], error: membersError } = useQuery(
    memberListOptions(wsId),
  );
  const { data: runtimes = [], error: runtimesError } = useQuery(
    runtimeListOptions(wsId),
  );

  const [createMethod, setCreateMethod] = useState<CreateSkillMethod | null>(
    null,
  );
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(
    new Set(),
  );
  const [scope, setScope] = useState<SkillScope>("all");

  // Persisted view preferences (per workspace, per user/device). Header sort
  // buttons and the header's display panel mutate the SAME store, so both
  // surfaces always agree. Selection stays session-local on purpose.
  const sortField = useSkillsViewStore((s) => s.sortField);
  const sortDirection = useSkillsViewStore((s) => s.sortDirection);
  const hiddenColumns = useSkillsViewStore((s) => s.hiddenColumns);
  const filters = useSkillsViewStore((s) => s.filters);
  const handleSort = useSkillsViewStore((s) => s.toggleSort);
  const handleSortFieldSelect = useSkillsViewStore((s) => s.setSortField);
  const setSortDirection = useSkillsViewStore((s) => s.setSortDirection);
  const toggleColumn = useSkillsViewStore((s) => s.toggleColumn);
  const toggleFilter = useSkillsViewStore((s) => s.toggleFilter);
  const clearFilters = useSkillsViewStore((s) => s.clearFilters);

  const assignments = useMemo(() => selectSkillAssignments(agents), [agents]);

  const membersById = useMemo(() => {
    const map = new Map<string, MemberWithUser>();
    for (const m of members) map.set(m.user_id, m);
    return map;
  }, [members]);

  const runtimesById = useMemo(() => {
    const map = new Map<string, AgentRuntime>();
    for (const r of runtimes) map.set(r.id, r);
    return map;
  }, [runtimes]);

  const myRole =
    members.find((m: MemberWithUser) => m.user_id === currentUserId)?.role ??
    null;
  const isAdmin = myRole === "owner" || myRole === "admin";

  const actionsCtx: SkillActionsContext = {
    wsId,
    agents,
    currentUserId,
    isAdmin,
  };

  const allRows = useMemo<SkillRow[]>(() => {
    return skills.map((skill) => {
      const origin = readOrigin(skill);
      const runtime =
        origin.type === "runtime_local" && origin.runtime_id
          ? runtimesById.get(origin.runtime_id) ?? null
          : null;
      return {
        skill,
        agents: assignments.get(skill.id) ?? [],
        creator: skill.created_by
          ? membersById.get(skill.created_by) ?? null
          : null,
        runtime,
        originType: origin.type,
        canEdit: canEditSkill(skill, { userId: currentUserId, role: myRole }),
      };
    });
  }, [skills, assignments, membersById, runtimesById, currentUserId, myRole]);

  const mineRows = useMemo(
    () =>
      currentUserId
        ? allRows.filter((row) => row.skill.created_by === currentUserId)
        : [],
    [allRows, currentUserId],
  );
  // The current scope, unfiltered — filter option lists and counts derive
  // from this.
  const scopeRows = scope === "mine" ? mineRows : allRows;

  // Visible rows: filters, then sort.
  const rows = useMemo<SkillRow[]>(() => {
    const filtered = scopeRows.filter((row) => rowMatchesFilters(row, filters));

    const dir = sortDirection === "asc" ? 1 : -1;
    filtered.sort((a, b) => {
      if (sortField === "name") {
        return a.skill.name.localeCompare(b.skill.name) * dir;
      }
      if (sortField === "usedBy") {
        return (
          (a.agents.length - b.agents.length) * dir ||
          a.skill.name.localeCompare(b.skill.name)
        );
      }
      if (sortField === "created") {
        return (
          (Date.parse(a.skill.created_at) - Date.parse(b.skill.created_at)) *
          dir
        );
      }
      return (
        (Date.parse(a.skill.updated_at) - Date.parse(b.skill.updated_at)) * dir
      );
    });
    return filtered;
  }, [scopeRows, filters, sortField, sortDirection]);

  const handleCreated = (skill: Skill) => {
    navigation.push(paths.skillDetail(skill.id));
  };

  const selectedRows = rows.filter((row) => selectedIds.has(row.skill.id));

  const columns = useMemo<CollectionColumn<SkillRow>[]>(
    () => [
      {
        key: "name",
        header: t(($) => $.table.name),
        sortKey: "name",
        cell: (row) => <NameCell row={row} />,
      },
      {
        key: "usedBy",
        header: t(($) => $.table.used_by),
        width: 144,
        sortKey: "usedBy",
        core: true,
        hideable: true,
        cell: (row) => <UsedByCell agents={row.agents} />,
      },
      {
        key: "source",
        header: t(($) => $.table.source),
        width: 152,
        hideable: true,
        cell: (row) => <SourceCell skill={row.skill} runtime={row.runtime} />,
      },
      {
        key: "creator",
        header: t(($) => $.table.created_by),
        width: 144,
        hideable: true,
        cell: (row) => <CreatorCell creator={row.creator} />,
      },
      {
        key: "updated",
        header: t(($) => $.table.updated),
        width: 104,
        sortKey: "updated",
        hideable: true,
        cell: (row) => <span className="tabular-nums">{timeAgo(row.skill.updated_at)}</span>,
      },
      {
        key: "created",
        header: t(($) => $.table.created),
        width: 104,
        sortKey: "created",
        hideable: true,
        cell: (row) => <span className="tabular-nums">{timeAgo(row.skill.created_at)}</span>,
      },
    ],
    [t, timeAgo],
  );

  const sort: CollectionSort = {
    field: sortField,
    direction: sortDirection,
    onToggle: (field) => handleSort(field as SkillSortField),
    onFieldChange: (field) => handleSortFieldSelect(field as SkillSortField),
    onDirectionChange: setSortDirection,
  };

  const narrowed = countActiveFilterDimensions(filters) > 0;
  const scopeLabels: Record<SkillScope, string> = {
    all: t(($) => $.page.scopes.all.label),
    mine: t(($) => $.page.scopes.mine.label),
  };

  const header = (
    <CollectionPageHeader
      icon={SkillIcon}
      title={scopeLabels[scope]}
      menu={
        <CollectionScopeItems
          value={scope}
          onChange={setScope}
          options={[
            { value: "all", label: scopeLabels.all, count: allRows.length },
            { value: "mine", label: scopeLabels.mine, count: mineRows.length },
          ]}
        />
      }
      tools={
        <>
          <SkillFilterMenu
            filters={filters}
            onToggleFilter={toggleFilter}
            rows={scopeRows}
          />
          {narrowed && <CollectionResetButton onClick={clearFilters} />}
        </>
      }
      actions={
        <>
          <CollectionDisplayMenu
            columns={columns}
            hiddenColumns={hiddenColumns}
            onToggleColumn={(key) => toggleColumn(key as SkillColumnKey)}
            sort={sort}
          />
          <SkillCreateMenu onCreate={setCreateMethod} />
        </>
      }
    />
  );

  const createDialog = createMethod && (
    <CreateSkillDialog
      initialMethod={createMethod}
      onClose={() => setCreateMethod(null)}
      onCreated={handleCreated}
    />
  );

  // --- List request error ---
  if (listError) {
    return (
      <div className="flex flex-1 min-h-0 flex-col">
        {header}
        <CollectionPageState
          role="alert"
          tone="destructive"
          icon={AlertCircle}
          title={t(($) => $.page.list_error.title)}
          description={
            listError instanceof Error
              ? listError.message
              : t(($) => $.page.list_error.fallback)
          }
          actions={
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => refetchList()}
            >
              {t(($) => $.page.list_error.retry)}
            </Button>
          }
        />
        {createDialog}
      </div>
    );
  }

  const showEmpty = !isLoading && skills.length === 0;
  const supportingQueryDown =
    !!agentsError || !!membersError || !!runtimesError;

  return (
    // relative: positioning anchor for the batch toolbar (page-centered,
    // not viewport-centered).
    <div className="relative flex flex-1 min-h-0 flex-col">
      {header}

      {supportingQueryDown && (
        <div
          role="status"
          className="flex shrink-0 items-start gap-2 border-b bg-warning/10 px-6 py-2 text-caption text-muted-foreground"
        >
          <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-warning" />
          <span>{t(($) => $.page.supporting_data_warning)}</span>
        </div>
      )}

      {showEmpty ? (
        <div className="flex flex-1 items-center justify-center">
          <EmptyState />
        </div>
      ) : (
        <CollectionList
          label={scopeLabels[scope]}
          columns={columns}
          hiddenColumns={hiddenColumns}
          rows={rows}
          loading={isLoading}
          getRowId={(row) => row.skill.id}
          getRowHref={(row) => paths.skillDetail(row.skill.id)}
          getRowTitle={(row) => row.skill.name}
          rowMenu={(row) => <SkillRowActions row={row} ctx={actionsCtx} />}
          sort={sort}
          selection={{ selectedIds, onChange: setSelectedIds }}
          empty={t(($) => $.page.no_matches.title)}
        />
      )}

      <SkillBatchToolbar
        rows={selectedRows}
        ctx={actionsCtx}
        onClear={() => setSelectedIds(new Set())}
      />

      {createDialog}
    </div>
  );
}
