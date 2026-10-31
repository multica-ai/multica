"use client";

import { useCallback, useMemo, useState } from "react";
import {
  ExternalLink,
  FolderKanban,
  LayoutGrid,
  Pin,
  PinOff,
  Rows3,
  Search,
  Trash2,
} from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  projectListOptions,
  useUpdateProject,
  useDeleteProject,
  useProjectViewStore,
  type ProjectColumnKey,
  type ProjectListFilters,
  type ProjectSortField,
  type ProjectViewMode,
} from "@multica/core/projects";
import {
  pinListOptions,
  useCreatePin,
  useDeletePin,
} from "@multica/core/pins";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useAuthStore } from "@multica/core/auth";
import { useActorName } from "@multica/core/workspace/hooks";
import { memberListOptions } from "@multica/core/workspace/queries";
import { useModalStore } from "@multica/core/modals";
import { AppLink, useIntentNavigate } from "../../navigation";
import { ActorAvatar } from "../../common/actor-avatar";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
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
import { Toggle } from "@multica/ui/components/ui/toggle";
import type {
  MemberWithUser,
  Project,
  ProjectPriority,
  ProjectStatus,
  UpdateProjectRequest,
} from "@multica/core/types";
import {
  CollectionCreateButton,
  CollectionFilterButton,
  CollectionPageHeader,
  CollectionPageState,
  CollectionResetButton,
  CollectionScopeItems,
} from "../../layout/collection-page";
import {
  CollectionBatchBar,
  CollectionDisplayMenu,
  CollectionEmptyValue,
  CollectionList,
  CollectionNameCell,
  CollectionRowMenu,
  type CollectionColumn,
  type CollectionSort,
} from "../../layout/collection-list";
import { ProjectIcon } from "./project-icon";
import { useT } from "../../i18n";
import { useFormatRelativeDate } from "./labels";
import { ProjectStatusBadge, ProjectPriorityBadge } from "./project-badge";
import { ProjectLeadPicker } from "./project-lead-picker";
import { PAGE_GUTTER } from "../../layout/page-header";
import { cn } from "@multica/ui/lib/utils";

// Sort order maps for the enum columns (header sort needs a total order).
const PRIORITY_ORDER: Record<ProjectPriority, number> = {
  urgent: 4,
  high: 3,
  medium: 2,
  low: 1,
  none: 0,
};
const STATUS_ORDER: Record<ProjectStatus, number> = {
  planned: 0,
  in_progress: 1,
  paused: 2,
  completed: 3,
  cancelled: 4,
};

const progressOf = (p: Project) =>
  p.issue_count > 0 ? p.done_count / p.issue_count : -1;

// Composite "type:id" lead value so the string[] filter holds member/agent
// refs alike.
function leadFilterValue(p: Project): string | null {
  return p.lead_type && p.lead_id ? `${p.lead_type}:${p.lead_id}` : null;
}

function ProgressRing({ project }: { project: Project }) {
  if (project.issue_count === 0) return <CollectionEmptyValue />;
  const pct = Math.round((project.done_count / project.issue_count) * 100);
  return (
    <span className="flex items-center gap-1.5">
      <span className="relative h-3.5 w-3.5">
        <svg className="h-3.5 w-3.5 -rotate-90" viewBox="0 0 16 16">
          <circle className="text-muted" strokeWidth="2" stroke="currentColor" fill="none" r="6" cx="8" cy="8" />
          <circle
            className="text-emerald-500"
            strokeWidth="2"
            stroke="currentColor"
            fill="none"
            r="6"
            cx="8"
            cy="8"
            strokeDasharray={`${pct * 0.377} 37.7`}
            strokeLinecap="round"
          />
        </svg>
      </span>
      <span className="tabular-nums">
        {project.done_count}/{project.issue_count}
      </span>
    </span>
  );
}

// Shared by table rows (⋯ and right-click) and cards.
function ProjectRowActions({
  project,
  pinned,
  canDelete,
}: {
  project: Project;
  pinned: boolean;
  canDelete: boolean;
}) {
  const { t } = useT("projects");
  const { t: tCommon } = useT("common");
  const wsPaths = useWorkspacePaths();
  const intentNavigate = useIntentNavigate();
  const createPin = useCreatePin();
  const deletePin = useDeletePin();
  const deleteProject = useDeleteProject();
  const [deleteOpen, setDeleteOpen] = useState(false);

  const togglePin = () => {
    if (pinned) deletePin.mutate({ itemType: "project", itemId: project.id });
    else createPin.mutate({ item_type: "project", item_id: project.id });
  };

  return (
    <>
      <CollectionRowMenu label={t(($) => $.page.row_menu)} contentClassName="w-44">
        <DropdownMenuItem
          onClick={() =>
            intentNavigate(
              wsPaths.projectDetail(project.id),
              "foreground-tab",
              project.title,
            )
          }
        >
          <ExternalLink className="size-3.5" />
          {tCommon(($) => $.navigation.open_in_new_tab)}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={togglePin}>
          {pinned ? (
            <PinOff className="size-3.5" />
          ) : (
            <Pin className="size-3.5" />
          )}
          {pinned ? t(($) => $.page.unpin) : t(($) => $.page.pin)}
        </DropdownMenuItem>
        {canDelete && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              variant="destructive"
              onClick={() => setDeleteOpen(true)}
            >
              <Trash2 className="size-3.5" />
              {t(($) => $.page.delete)}
            </DropdownMenuItem>
          </>
        )}
      </CollectionRowMenu>

      <Dialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t(($) => $.delete_dialog.title)}</DialogTitle>
            <DialogDescription>
              {t(($) => $.delete_dialog.description)}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setDeleteOpen(false)}
            >
              {t(($) => $.delete_dialog.cancel)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              onClick={() => {
                deleteProject.mutate(project.id, {
                  onError: (err) =>
                    toast.error(
                      err instanceof Error ? err.message : String(err),
                    ),
                });
                setDeleteOpen(false);
              }}
            >
              {t(($) => $.delete_dialog.confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

function ProjectInlineCell({
  project,
  children,
}: {
  project: Project;
  children: (handleUpdate: (data: UpdateProjectRequest) => void) => React.ReactNode;
}) {
  const updateProject = useUpdateProject();
  const handleUpdate = useCallback(
    (data: UpdateProjectRequest) => updateProject.mutate({ id: project.id, ...data }),
    [project.id, updateProject],
  );
  return <>{children(handleUpdate)}</>;
}

// Card (comfortable) view — kept from the prior page.

function ProjectCard({
  project,
  pinned,
  canDelete,
}: {
  project: Project;
  pinned: boolean;
  canDelete: boolean;
}) {
  const { t } = useT("projects");
  const wsPaths = useWorkspacePaths();
  const formatRelativeDate = useFormatRelativeDate();
  const updateProject = useUpdateProject();
  const handleUpdate = useCallback(
    (data: UpdateProjectRequest) => updateProject.mutate({ id: project.id, ...data }),
    [project.id, updateProject],
  );
  const progressPercent =
    project.issue_count > 0
      ? Math.round((project.done_count / project.issue_count) * 100)
      : 0;

  return (
    <div className="group/card group/row flex flex-col rounded-md border bg-card transition-colors hover:border-primary/50">
      <div className="p-3 pb-2">
        <div className="flex items-center gap-2">
          <AppLink
            href={wsPaths.projectDetail(project.id)}
            className="flex min-w-0 flex-1 items-center gap-2"
          >
            <ProjectIcon project={project} size="sm" />
            <h3 className="truncate text-body font-medium">{project.title}</h3>
          </AppLink>
          <ProjectRowActions project={project} pinned={pinned} canDelete={canDelete} />
          <ProjectStatusBadge project={project} handleUpdate={handleUpdate} triggerClassName="shrink-0" />
        </div>

        {project.issue_count > 0 ? (
          <div className="flex items-center justify-end gap-1.5 pt-2">
            <div className="relative h-4 w-4">
              <svg className="h-4 w-4 -rotate-90" viewBox="0 0 16 16">
                <circle className="text-muted" strokeWidth="2" stroke="currentColor" fill="none" r="6" cx="8" cy="8" />
                <circle
                  className="text-emerald-500"
                  strokeWidth="2"
                  stroke="currentColor"
                  fill="none"
                  r="6"
                  cx="8"
                  cy="8"
                  strokeDasharray={`${progressPercent * 0.377} 37.7`}
                  strokeLinecap="round"
                />
              </svg>
            </div>
            <span className="text-micro tabular-nums text-muted-foreground">
              {project.done_count}/{project.issue_count}
            </span>
          </div>
        ) : (
          <span className="flex justify-end pt-2 text-micro text-muted-foreground">
            {t(($) => $.detail.no_issues_yet)}
          </span>
        )}
      </div>

      <div className="mt-0 flex items-center justify-between border-t px-3 pb-3 pt-2">
        <ProjectLeadPicker
          project={project}
          handleUpdate={handleUpdate}
          renderTrigger={(leadName) => (
            <button type="button" className="-mx-1.5 flex items-center gap-1.5 rounded-xs px-1.5 py-0.5 transition-colors hover:bg-accent/60">
              {project.lead_type && project.lead_id ? (
                <ActorAvatar actorType={project.lead_type} actorId={project.lead_id} size="sm" enableHoverCard />
              ) : (
                <span className="inline-flex h-5 w-5 rounded-full border border-dashed border-muted-foreground/30" />
              )}
              <span className="max-w-[60px] truncate text-micro text-muted-foreground">
                {leadName ?? t(($) => $.lead.no_lead)}
              </span>
            </button>
          )}
        />
        <div className="flex items-center gap-2">
          <ProjectPriorityBadge project={project} handleUpdate={handleUpdate} align="start" />
          <span className="text-micro text-muted-foreground">
            {formatRelativeDate(project.created_at)}
          </span>
        </div>
      </div>
    </div>
  );
}

// Header tools — scope menu, filter, display (view, sort, columns).

// Scope splits the lifecycle in two: still being worked on, or closed. The
// status filter narrows further within it.
type ProjectScope = "all" | "active" | "closed";
const PROJECT_SCOPES: ProjectScope[] = ["all", "active", "closed"];
const CLOSED_STATUSES: ReadonlySet<ProjectStatus> = new Set(["completed", "cancelled"]);

function inScope(project: Project, scope: ProjectScope): boolean {
  if (scope === "all") return true;
  return CLOSED_STATUSES.has(project.status) === (scope === "closed");
}

const VIEW_MODES: { mode: ProjectViewMode; Icon: typeof Rows3 }[] = [
  { mode: "compact", Icon: Rows3 },
  { mode: "comfortable", Icon: LayoutGrid },
];

const STATUS_VALUES: ProjectStatus[] = [
  "planned",
  "in_progress",
  "paused",
  "completed",
  "cancelled",
];
const PRIORITY_VALUES: ProjectPriority[] = ["urgent", "high", "medium", "low", "none"];

function countActiveFilters(f: ProjectListFilters): number {
  let c = 0;
  if (f.statuses.length) c++;
  if (f.priorities.length) c++;
  if (f.leads.length) c++;
  return c;
}

// Batch toolbar — page-anchored (not viewport). Pin all selected (any
// member) + Delete (workspace admin). Mirrors the other lists.
function ProjectBatchToolbar({
  rows,
  pinnedIds,
  canDelete,
  onClear,
}: {
  rows: Project[];
  pinnedIds: Set<string>;
  canDelete: boolean;
  onClear: () => void;
}) {
  const { t } = useT("projects");
  const createPin = useCreatePin();
  const deleteProject = useDeleteProject();
  const [confirmDelete, setConfirmDelete] = useState(false);

  const anyUnpinned = rows.some((p) => !pinnedIds.has(p.id));

  return (
    <>
      <CollectionBatchBar count={rows.length} onClear={onClear}>
        {anyUnpinned && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              for (const p of rows) {
                if (!pinnedIds.has(p.id)) {
                  createPin.mutate({ item_type: "project", item_id: p.id });
                }
              }
              onClear();
            }}
          >
            <Pin className="mr-1 size-3.5" />
            {t(($) => $.page.pin)}
          </Button>
        )}
        {canDelete && (
          <Button
            variant="ghost"
            size="sm"
            className="text-destructive hover:text-destructive"
            onClick={() => setConfirmDelete(true)}
          >
            <Trash2 className="mr-1 size-3.5" />
            {t(($) => $.page.delete)}
          </Button>
        )}
      </CollectionBatchBar>

      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t(($) => $.delete_dialog.title)}</DialogTitle>
            <DialogDescription>{t(($) => $.delete_dialog.description)}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" size="sm" onClick={() => setConfirmDelete(false)}>
              {t(($) => $.delete_dialog.cancel)}
            </Button>
            <Button
              type="button"
              variant="destructive"
              size="sm"
              onClick={() => {
                for (const p of rows) deleteProject.mutate(p.id);
                setConfirmDelete(false);
                onClear();
              }}
            >
              {t(($) => $.delete_dialog.confirm)}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}

// Page

export function ProjectsPage() {
  const { t } = useT("projects");
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const currentUser = useAuthStore((s) => s.user);
  const { getActorName } = useActorName();

  const viewMode = useProjectViewStore((s) => s.viewMode);
  const setViewMode = useProjectViewStore((s) => s.setViewMode);
  const sortField = useProjectViewStore((s) => s.sortField);
  const sortDirection = useProjectViewStore((s) => s.sortDirection);
  const hiddenColumns = useProjectViewStore((s) => s.hiddenColumns);
  const filters = useProjectViewStore((s) => s.filters);
  const toggleSort = useProjectViewStore((s) => s.toggleSort);
  const setSortField = useProjectViewStore((s) => s.setSortField);
  const setSortDirection = useProjectViewStore((s) => s.setSortDirection);
  const toggleColumn = useProjectViewStore((s) => s.toggleColumn);
  const toggleFilter = useProjectViewStore((s) => s.toggleFilter);
  const clearFilters = useProjectViewStore((s) => s.clearFilters);
  const isCompact = viewMode === "compact";
  const formatRelativeDate = useFormatRelativeDate();

  const { data: projects = [], isLoading } = useQuery(projectListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: pins = [] } = useQuery({
    ...pinListOptions(wsId, currentUser?.id ?? ""),
    enabled: !!wsId && !!currentUser?.id,
  });
  const openCreateProject = () => useModalStore.getState().open("create-project");

  const isWorkspaceAdmin = useMemo(() => {
    if (!currentUser) return false;
    const me = members.find((m: MemberWithUser) => m.user_id === currentUser.id);
    return me?.role === "owner" || me?.role === "admin";
  }, [members, currentUser]);

  const pinnedProjectIds = useMemo(() => {
    const s = new Set<string>();
    for (const pin of pins) if (pin.item_type === "project") s.add(pin.item_id);
    return s;
  }, [pins]);

  const [scope, setScope] = useState<ProjectScope>("all");
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(new Set());

  const activeFilterCount = countActiveFilters(filters);
  const hasActiveFilters = activeFilterCount > 0;

  // Filter option counts derive from the full set so toggling one dimension
  // doesn't make the others vanish.
  const leadOptions = useMemo(() => {
    const m = new Map<string, { type: string; id: string; count: number }>();
    for (const p of projects) {
      const v = leadFilterValue(p);
      if (!v || !p.lead_type || !p.lead_id) continue;
      const e = m.get(v);
      if (e) e.count += 1;
      else m.set(v, { type: p.lead_type, id: p.lead_id, count: 1 });
    }
    return m;
  }, [projects]);

  const scopeCounts = useMemo<Record<ProjectScope, number>>(() => {
    const counts = { all: projects.length, active: 0, closed: 0 };
    for (const p of projects) counts[inScope(p, "closed") ? "closed" : "active"] += 1;
    return counts;
  }, [projects]);

  const visible = useMemo(() => {
    const filtered = projects.filter((p) => {
      if (!inScope(p, scope)) return false;
      if (filters.statuses.length && !filters.statuses.includes(p.status)) return false;
      if (filters.priorities.length && !filters.priorities.includes(p.priority)) {
        return false;
      }
      if (filters.leads.length) {
        const v = leadFilterValue(p);
        if (!v || !filters.leads.includes(v)) return false;
      }
      return true;
    });
    const dir = sortDirection === "asc" ? 1 : -1;
    const sorted = [...filtered];
    sorted.sort((a, b) => {
      if (sortField === "name") return a.title.localeCompare(b.title) * dir;
      if (sortField === "priority") {
        return (
          (PRIORITY_ORDER[a.priority] - PRIORITY_ORDER[b.priority]) * dir ||
          a.title.localeCompare(b.title)
        );
      }
      if (sortField === "status") {
        return (
          (STATUS_ORDER[a.status] - STATUS_ORDER[b.status]) * dir ||
          a.title.localeCompare(b.title)
        );
      }
      if (sortField === "progress") {
        return (progressOf(a) - progressOf(b)) * dir || a.title.localeCompare(b.title);
      }
      return (Date.parse(a.created_at) - Date.parse(b.created_at)) * dir;
    });
    return sorted;
  }, [projects, scope, filters, sortField, sortDirection]);

  const selectedProjects = visible.filter((p) => selectedIds.has(p.id));

  const columns = useMemo<CollectionColumn<Project>[]>(
    () => [
      {
        key: "name",
        header: t(($) => $.table.name),
        sortKey: "name",
        cell: (project) => (
          <CollectionNameCell
            icon={<ProjectIcon project={project} size="sm" />}
            title={project.title}
          />
        ),
      },
      {
        // The project's defining lifecycle field: always visible.
        key: "status",
        header: t(($) => $.table.status),
        width: 116,
        sortKey: "status",
        core: true,
        interactive: true,
        cell: (project) => (
          <ProjectInlineCell project={project}>
            {(handleUpdate) => (
              <ProjectStatusBadge project={project} handleUpdate={handleUpdate} align="start" />
            )}
          </ProjectInlineCell>
        ),
      },
      {
        key: "priority",
        header: t(($) => $.table.priority),
        width: 116,
        sortKey: "priority",
        hideable: true,
        interactive: true,
        cell: (project) => (
          <ProjectInlineCell project={project}>
            {(handleUpdate) => (
              <ProjectPriorityBadge project={project} handleUpdate={handleUpdate} align="start" />
            )}
          </ProjectInlineCell>
        ),
      },
      {
        key: "progress",
        header: t(($) => $.table.progress),
        width: 80,
        sortKey: "progress",
        hideable: true,
        cell: (project) => <ProgressRing project={project} />,
      },
      {
        key: "lead",
        header: t(($) => $.table.lead),
        width: 132,
        hideable: true,
        interactive: true,
        cell: (project) => (
          <ProjectInlineCell project={project}>
            {(handleUpdate) => (
              <ProjectLeadPicker
                project={project}
                handleUpdate={handleUpdate}
                align="start"
                renderTrigger={(leadName) => (
                  <button
                    type="button"
                    className="-mx-1 flex min-w-0 items-center gap-1.5 rounded-xs px-1 py-0.5 transition-colors hover:bg-accent/60"
                  >
                    {project.lead_type && project.lead_id ? (
                      <ActorAvatar
                        actorType={project.lead_type}
                        actorId={project.lead_id}
                        size="sm"
                        enableHoverCard
                      />
                    ) : (
                      <span className="inline-flex h-[18px] w-[18px] rounded-full border border-dashed border-muted-foreground/30" />
                    )}
                    <span className="min-w-0 truncate">{leadName ?? "—"}</span>
                  </button>
                )}
              />
            )}
          </ProjectInlineCell>
        ),
      },
      {
        key: "issues",
        header: t(($) => $.table.issues),
        width: 64,
        align: "end",
        hideable: true,
        className: "font-mono tabular-nums",
        cell: (project) => project.issue_count,
      },
      {
        key: "created",
        header: t(($) => $.table.created),
        width: 104,
        sortKey: "created",
        hideable: true,
        cell: (project) => (
          <span className="tabular-nums">{formatRelativeDate(project.created_at)}</span>
        ),
      },
    ],
    [t, formatRelativeDate],
  );

  const sort: CollectionSort = {
    field: sortField,
    direction: sortDirection,
    onToggle: (field) => toggleSort(field as ProjectSortField),
    onFieldChange: (field) => setSortField(field as ProjectSortField),
    onDirectionChange: setSortDirection,
  };

  const showEmpty = !isLoading && projects.length === 0;
  const countBadge = (n: number) => (
    <span className="ml-auto pl-3 text-caption text-muted-foreground">{n}</span>
  );
  const scopeLabels: Record<ProjectScope, string> = {
    all: t(($) => $.page.scope_all),
    active: t(($) => $.page.scope_active),
    closed: t(($) => $.page.scope_closed),
  };

  return (
    // relative: positioning anchor for the page-centered batch toolbar.
    <div className="relative flex flex-1 min-h-0 flex-col">
      <CollectionPageHeader
        icon={FolderKanban}
        title={scopeLabels[scope]}
        menu={
          <CollectionScopeItems
            value={scope}
            onChange={setScope}
            options={PROJECT_SCOPES.map((s) => ({
              value: s,
              label: scopeLabels[s],
              count: scopeCounts[s],
            }))}
          />
        }
        tools={
          <>
            <DropdownMenu>
              <DropdownMenuTrigger render={<CollectionFilterButton count={activeFilterCount} />} />
              <DropdownMenuContent align="start" className="w-auto">
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <span className="flex-1">{t(($) => $.toolbar.section_status)}</span>
                    {filters.statuses.length > 0 && (
                      <span className="text-caption font-medium text-primary">{filters.statuses.length}</span>
                    )}
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent className="w-auto min-w-44">
                    {STATUS_VALUES.map((s) => (
                      <DropdownMenuCheckboxItem
                        key={s}
                        checked={filters.statuses.includes(s)}
                        onCheckedChange={() => toggleFilter("statuses", s)}
                        className={FILTER_ITEM_CLASS}
                      >
                        <HoverCheck checked={filters.statuses.includes(s)} />
                        {t(($) => $.status[s])}
                      </DropdownMenuCheckboxItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <span className="flex-1">{t(($) => $.toolbar.section_priority)}</span>
                    {filters.priorities.length > 0 && (
                      <span className="text-caption font-medium text-primary">{filters.priorities.length}</span>
                    )}
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent className="w-auto min-w-44">
                    {PRIORITY_VALUES.map((pr) => (
                      <DropdownMenuCheckboxItem
                        key={pr}
                        checked={filters.priorities.includes(pr)}
                        onCheckedChange={() => toggleFilter("priorities", pr)}
                        className={FILTER_ITEM_CLASS}
                      >
                        <HoverCheck checked={filters.priorities.includes(pr)} />
                        {t(($) => $.priority[pr])}
                      </DropdownMenuCheckboxItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger>
                    <span className="flex-1">{t(($) => $.toolbar.section_lead)}</span>
                    {filters.leads.length > 0 && (
                      <span className="text-caption font-medium text-primary">{filters.leads.length}</span>
                    )}
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent className="max-h-72 w-auto min-w-48 overflow-y-auto">
                    {[...leadOptions.entries()].map(([value, { type, id, count }]) => (
                      <DropdownMenuCheckboxItem
                        key={value}
                        checked={filters.leads.includes(value)}
                        onCheckedChange={() => toggleFilter("leads", value)}
                        className={FILTER_ITEM_CLASS}
                      >
                        <HoverCheck checked={filters.leads.includes(value)} />
                        <ActorAvatar actorType={type} actorId={id} size="sm" />
                        <span className="min-w-0 truncate">{getActorName(type, id)}</span>
                        {countBadge(count)}
                      </DropdownMenuCheckboxItem>
                    ))}
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              </DropdownMenuContent>
            </DropdownMenu>
            {hasActiveFilters ? <CollectionResetButton onClick={clearFilters} /> : null}
          </>
        }
        actions={
          <>
            {/* Sort applies to both views; columns only to the table. */}
            <CollectionDisplayMenu
              columns={columns}
              hiddenColumns={hiddenColumns}
              onToggleColumn={(key) => toggleColumn(key as ProjectColumnKey)}
              sort={sort}
              showColumns={isCompact}
              triggerIcon={isCompact ? Rows3 : LayoutGrid}
              extra={
                <div
                  role="group"
                  aria-label={t(($) => $.toolbar.view)}
                  className="grid grid-cols-2 gap-0.5 rounded-lg bg-muted p-0.5"
                >
                  {VIEW_MODES.map(({ mode, Icon }) => (
                    <Toggle
                      key={mode}
                      pressed={viewMode === mode}
                      onPressedChange={() => setViewMode(mode)}
                      className="h-auto min-w-0 flex-col gap-1 rounded-md px-0.5 py-1.5 text-caption font-normal text-muted-foreground hover:bg-transparent hover:text-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm aria-pressed:hover:bg-background"
                    >
                      <Icon className="size-4" />
                      <span className="max-w-full truncate">
                        {mode === "compact" ? t(($) => $.page.view_table) : t(($) => $.page.view_cards)}
                      </span>
                    </Toggle>
                  ))}
                </div>
              }
            />
            <CollectionCreateButton
              label={t(($) => $.page.new_project)}
              onClick={openCreateProject}
            />
          </>
        }
      />

      {showEmpty ? (
        <CollectionPageState
          icon={FolderKanban}
          title={t(($) => $.page.empty)}
          actions={
            <Button size="sm" variant="outline" onClick={openCreateProject}>
              {t(($) => $.page.create_first)}
            </Button>
          }
        />
      ) : (
        <>
          {isCompact ? (
            <CollectionList
              label={scopeLabels[scope]}
              columns={columns}
              hiddenColumns={hiddenColumns}
              rows={visible}
              loading={isLoading}
              getRowId={(project) => project.id}
              getRowHref={(project) => wsPaths.projectDetail(project.id)}
              getRowTitle={(project) => project.title}
              rowMenu={(project) => (
                <ProjectRowActions
                  project={project}
                  pinned={pinnedProjectIds.has(project.id)}
                  canDelete={isWorkspaceAdmin}
                />
              )}
              sort={sort}
              selection={{ selectedIds, onChange: setSelectedIds }}
              empty={t(($) => $.page.no_matches)}
            />
          ) : isLoading ? (
            <CardsLoading />
          ) : visible.length === 0 ? (
            <div className="flex flex-1 flex-col items-center justify-center py-24 text-muted-foreground">
              <Search className="mb-3 h-10 w-10 opacity-30" />
              <p className="text-body">{t(($) => $.page.no_matches)}</p>
            </div>
          ) : (
            <div className={cn("min-h-0 flex-1 overflow-y-auto pt-4 pb-16", PAGE_GUTTER)}>
              <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
                {visible.map((project) => (
                  <ProjectCard
                    key={project.id}
                    project={project}
                    pinned={pinnedProjectIds.has(project.id)}
                    canDelete={isWorkspaceAdmin}
                  />
                ))}
              </div>
            </div>
          )}

          <ProjectBatchToolbar
            rows={selectedProjects}
            pinnedIds={pinnedProjectIds}
            canDelete={isWorkspaceAdmin}
            onClear={() => setSelectedIds(new Set())}
          />
        </>
      )}
    </div>
  );
}

function CardsLoading() {
  return (
    <div className={cn("grid grid-cols-1 gap-3 pt-4 sm:grid-cols-2 lg:grid-cols-4", PAGE_GUTTER)}>
      {Array.from({ length: 8 }).map((_, i) => (
        <div key={i} className="flex flex-col gap-2 rounded-md border p-3">
          <div className="flex items-center gap-2">
            <Skeleton className="h-8 w-8 rounded-xs" />
            <Skeleton className="h-4 w-3/4" />
          </div>
          <div className="flex gap-1.5">
            <Skeleton className="h-5 w-16 rounded-xs" />
            <Skeleton className="h-5 w-20 rounded-xs" />
          </div>
        </div>
      ))}
    </div>
  );
}
