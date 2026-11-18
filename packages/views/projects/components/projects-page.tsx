"use client";

import { useCallback, useMemo, useState } from "react";
import {
  Archive,
  ArchiveRestore,
  Columns3,
  Folder,
  FolderKanban,
  FolderMinus,
  Pin,
  Rows3,
  Trash2,
} from "@multica/ui/icons";
import { useQuery } from "@tanstack/react-query";
import {
  boardGroupBy,
  isProjectArchived,
  projectLastActivityAt,
  useProjectsPagePrefsSync,
  projectListOptions,
  useUpdateProject,
  useProjectViewStore,
  type ProjectColumnKey,
  type ProjectGroupBy,
  type ProjectLayout,
  type ProjectListFilters,
  type ProjectSortField,
} from "@multica/core/projects";
import { PROJECT_STATUS_CONFIG } from "@multica/core/projects/config";
import {
  pinListOptions,
  useCreatePin,
} from "@multica/core/pins";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useAuthStore } from "@multica/core/auth";
import { useActorName } from "@multica/core/workspace/hooks";
import {
  agentListOptions,
  memberListOptions,
} from "@multica/core/workspace/queries";
import { canDeleteProject } from "@multica/core/projects/deletion-permissions";
import { useModalStore } from "@multica/core/modals";
import { ActorAvatar } from "../../common/actor-avatar";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
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
  CollectionNoMatches,
  CollectionPageHeader,
  CollectionResetButton,
  CollectionScopeItems,
  CollectionSearch,
} from "../../layout/collection-page";
import {
  CollectionBatchBar,
  CollectionDisplayMenu,
  CollectionEmptyValue,
  CollectionList,
  CollectionNameCell,
  type CollectionColumn,
  type CollectionGroup,
  type CollectionSort,
} from "../../layout/collection-list";
import { ProjectIcon } from "./project-icon";
import { ProjectsIntro } from "./projects-intro";
import { useT } from "../../i18n";
import { DeleteProjectsDialog, ProjectRowActions } from "./project-row-actions";
import { useArchiveProjects, useRestoreProjects } from "./project-archive";
import { getProjectFoldersExtension, useProjectFolders } from "../../extensions";
import { useFormatRelativeDate } from "./labels";
import { ProjectStatusBadge, ProjectPriorityBadge } from "./project-badge";
import { ProjectLeadPicker } from "./project-lead-picker";
import { ProjectBoard, ProjectBoardLoading, type ProjectBoardColumn } from "./project-board";
import { ProjectDate, projectDueState } from "./project-date";
import {
  NO_FOLDER_KEY,
  NO_LEAD_KEY,
  groupProjects,
  projectGroupUpdate,
  type ProjectGroupField,
} from "./project-groups";
import { PriorityIcon } from "../../issues/components/priority-icon";
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

// Header tools — scope menu, filter, display (view, sort, columns).

// Scope splits the lifecycle in two: still being worked on, or closed. The
// status filter narrows further within it. Archived projects sit outside the
// lifecycle scopes, in their own. (MUL-8861)
type ProjectScope = "all" | "active" | "closed" | "archived";
const PROJECT_SCOPES: ProjectScope[] = ["all", "active", "closed", "archived"];
const CLOSED_STATUSES: ReadonlySet<ProjectStatus> = new Set(["completed", "cancelled"]);

function statusInScope(status: ProjectStatus, scope: ProjectScope): boolean {
  if (scope === "all" || scope === "archived") return true;
  return CLOSED_STATUSES.has(status) === (scope === "closed");
}

function inScope(project: Project, scope: ProjectScope): boolean {
  if (isProjectArchived(project)) return scope === "archived";
  if (scope === "archived") return false;
  return statusInScope(project.status, scope);
}

const LAYOUTS: { layout: ProjectLayout; Icon: typeof Rows3 }[] = [
  { layout: "table", Icon: Rows3 },
  { layout: "board", Icon: Columns3 },
];

const GROUP_FIELDS: ProjectGroupField[] = ["status", "priority", "lead"];

// Calendar days compare as strings. Projects without the date sort last
// whichever way the sort runs.
function compareDates(a: string | null, b: string | null, dir: number): number {
  if (a === b) return 0;
  if (!a) return 1;
  if (!b) return -1;
  return (a < b ? -1 : 1) * dir;
}

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
// member), Archive or Restore (any member), Delete (only when every selected
// project is deletable).
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
  const [confirmDelete, setConfirmDelete] = useState(false);
  const { requestArchive, dialog: archiveDialog } = useArchiveProjects();
  const restore = useRestoreProjects();

  const anyUnpinned = rows.some((p) => !pinnedIds.has(p.id));
  const allArchived = rows.length > 0 && rows.every(isProjectArchived);

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
        {allArchived ? (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              for (const p of rows) restore(p);
              onClear();
            }}
          >
            <ArchiveRestore className="mr-1 size-3.5" />
            {t(($) => $.page.restore)}
          </Button>
        ) : (
          <Button variant="ghost" size="sm" onClick={() => requestArchive(rows, { onArchived: onClear })}>
            <Archive className="mr-1 size-3.5" />
            {t(($) => $.page.archive)}
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

      <DeleteProjectsDialog
        rows={rows}
        open={confirmDelete}
        onOpenChange={setConfirmDelete}
        onDeleted={onClear}
        onArchiveInstead={() => requestArchive(rows, { onArchived: onClear, immediate: true })}
      />
      {archiveDialog}
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

  const layout = useProjectViewStore((s) => s.layout);
  const groupBy = useProjectViewStore((s) => s.groupBy);
  const search = useProjectViewStore((s) => s.search);
  const setSearch = useProjectViewStore((s) => s.setSearch);
  const sortField = useProjectViewStore((s) => s.sortField);
  const sortDirection = useProjectViewStore((s) => s.sortDirection);
  const hiddenColumns = useProjectViewStore((s) => s.hiddenColumns);
  const filters = useProjectViewStore((s) => s.filters);
  // Every change the user makes to the display is saved for their other devices.
  const savePrefs = useProjectsPagePrefsSync(wsId);
  const persisted =
    <A extends unknown[]>(action: (...args: A) => void) =>
    (...args: A) => {
      action(...args);
      savePrefs();
    };
  const toggleSort = persisted(useProjectViewStore((s) => s.toggleSort));
  const setSortField = persisted(useProjectViewStore((s) => s.setSortField));
  const setSortDirection = persisted(useProjectViewStore((s) => s.setSortDirection));
  const toggleColumn = persisted(useProjectViewStore((s) => s.toggleColumn));
  const toggleFilter = persisted(useProjectViewStore((s) => s.toggleFilter));
  const clearFilters = persisted(useProjectViewStore((s) => s.clearFilters));
  const setLayout = persisted(useProjectViewStore((s) => s.setLayout));
  const setGroupBy = persisted(useProjectViewStore((s) => s.setGroupBy));
  const isTable = layout === "table";
  const formatRelativeDate = useFormatRelativeDate();
  const updateProject = useUpdateProject();

  const { data: projects = [], isLoading } = useQuery(projectListOptions(wsId));
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: pins = [] } = useQuery({
    ...pinListOptions(wsId, currentUser?.id ?? ""),
    enabled: !!wsId && !!currentUser?.id,
  });
  const openCreateProject = () => useModalStore.getState().open("create-project");

  const role = members.find((m: MemberWithUser) => m.user_id === currentUser?.id)?.role ?? null;
  const mayDelete = (project: Project) =>
    canDeleteProject(project, currentUser?.id ?? null, role, agents);

  const pinnedProjectIds = useMemo(() => {
    const s = new Set<string>();
    for (const pin of pins) if (pin.item_type === "project") s.add(pin.item_id);
    return s;
  }, [pins]);

  const [scope, setScope] = useState<ProjectScope>("all");
  const [selectedIds, setSelectedIds] = useState<ReadonlySet<string>>(new Set());

  const activeFilterCount = countActiveFilters(filters);
  const hasActiveFilters = activeFilterCount > 0;
  const query = search.trim().toLowerCase();
  const hasSearch = query !== "";
  const resetNarrowing = () => {
    clearFilters();
    setSearch("");
  };

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
    const counts = { all: 0, active: 0, closed: 0, archived: 0 };
    for (const p of projects) {
      if (isProjectArchived(p)) {
        counts.archived += 1;
        continue;
      }
      counts.all += 1;
      counts[inScope(p, "closed") ? "closed" : "active"] += 1;
    }
    return counts;
  }, [projects]);
  const restore = useRestoreProjects();

  const visible = useMemo(() => {
    const filtered = projects.filter((p) => {
      if (!inScope(p, scope)) return false;
      if (query && !p.title.toLowerCase().includes(query)) return false;
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
      if (sortField === "start_date" || sortField === "due_date") {
        return compareDates(a[sortField], b[sortField], dir) || a.title.localeCompare(b.title);
      }
      if (sortField === "last_activity") {
        return (
          (Date.parse(projectLastActivityAt(a)) - Date.parse(projectLastActivityAt(b))) * dir ||
          a.title.localeCompare(b.title)
        );
      }
      return (Date.parse(a.created_at) - Date.parse(b.created_at)) * dir;
    });
    return sorted;
  }, [projects, scope, query, filters, sortField, sortDirection]);

  const folders = useProjectFolders(wsId);
  const hasFolders = getProjectFoldersExtension() !== null;
  const groupFields: ProjectGroupField[] = hasFolders ? [...GROUP_FIELDS, "folder"] : GROUP_FIELDS;
  // A folder grouping saved where folders are unavailable shows ungrouped.
  const shownGroupBy: ProjectGroupBy = groupBy === "folder" && !hasFolders ? "none" : groupBy;

  // The board always groups (status unless another field is chosen); the
  // table only when asked to.
  const groupField: ProjectGroupField | null = isTable
    ? shownGroupBy === "none"
      ? null
      : shownGroupBy
    : boardGroupBy(shownGroupBy);

  const leadKeyName = useCallback(
    (key: string) => {
      const [type, id] = key.split(":");
      return type && id ? getActorName(type, id) : key;
    },
    [getActorName],
  );

  // Every group the scope and filters allow, in a stable order, so a board
  // column can be dropped on while empty. Leads are the ones projects have.
  const groupKeys = useMemo<string[]>(() => {
    if (groupField === "status") {
      return STATUS_VALUES.filter(
        (s) =>
          statusInScope(s, scope) &&
          (filters.statuses.length === 0 || filters.statuses.includes(s)),
      );
    }
    if (groupField === "priority") {
      return PRIORITY_VALUES.filter(
        (p) => filters.priorities.length === 0 || filters.priorities.includes(p),
      );
    }
    if (groupField === "lead") {
      const leads = [...(filters.leads.length ? filters.leads : leadOptions.keys())].sort(
        (a, b) => leadKeyName(a).localeCompare(leadKeyName(b)),
      );
      return filters.leads.length ? leads : [...leads, NO_LEAD_KEY];
    }
    if (groupField === "folder") return [...folders.folders.map((folder) => folder.id), NO_FOLDER_KEY];
    return [];
  }, [groupField, scope, filters, leadOptions, leadKeyName, folders.folders]);

  const groupHeading = useCallback(
    (field: ProjectGroupField, key: string): { label: string; icon: React.ReactNode } => {
      if (field === "status") {
        const status = key as ProjectStatus;
        return {
          label: t(($) => $.status[status]),
          icon: (
            <span
              className={cn("size-2 shrink-0 rounded-full", PROJECT_STATUS_CONFIG[status].dotColor)}
            />
          ),
        };
      }
      if (field === "priority") {
        const priority = key as ProjectPriority;
        return {
          label: t(($) => $.priority[priority]),
          icon: <PriorityIcon priority={priority} />,
        };
      }
      if (field === "folder") {
        const folder = folders.folders.find((f) => f.id === key);
        return folder
          ? { label: folder.name, icon: <Folder className="size-4 shrink-0 text-muted-foreground" /> }
          : {
              label: folders.labels.noFolder,
              icon: <FolderMinus className="size-4 shrink-0 text-muted-foreground" />,
            };
      }
      if (key === NO_LEAD_KEY) {
        return {
          label: t(($) => $.lead.no_lead),
          icon: (
            <span className="inline-flex size-[18px] shrink-0 rounded-full border border-dashed border-muted-foreground/30" />
          ),
        };
      }
      const [type, id] = key.split(":") as [string, string];
      return {
        label: leadKeyName(key),
        icon: <ActorAvatar actorType={type} actorId={id} size="sm" />,
      };
    },
    [t, leadKeyName, folders.folders, folders.labels.noFolder],
  );

  const buckets = useMemo(
    () => (groupField ? groupProjects(visible, groupField, groupKeys, folders.folderOf) : []),
    [visible, groupField, groupKeys, folders.folderOf],
  );

  const tableGroups = useMemo<CollectionGroup<Project>[] | undefined>(() => {
    if (!isTable || !groupField) return undefined;
    return buckets
      .filter((bucket) => bucket.projects.length > 0)
      .map((bucket) => {
        const { label, icon } = groupHeading(groupField, bucket.key);
        return {
          key: bucket.key,
          label: (
            <span className="inline-flex min-w-0 items-center gap-2">
              {icon}
              <span className="truncate">{label}</span>
            </span>
          ),
          count: bucket.projects.length,
          rows: bucket.projects,
        };
      });
  }, [isTable, groupField, buckets, groupHeading]);

  const boardColumns = useMemo<ProjectBoardColumn[]>(() => {
    if (isTable || !groupField) return [];
    return buckets.map((bucket) => ({
      key: bucket.key,
      ...groupHeading(groupField, bucket.key),
      projects: bucket.projects,
    }));
  }, [isTable, groupField, buckets, groupHeading]);

  const moveToGroup = (project: Project, key: string) => {
    if (!groupField) return;
    if (groupField === "folder") {
      if (key === NO_FOLDER_KEY || folders.canFile) folders.moveProject(project.id, key === NO_FOLDER_KEY ? null : key);
      return;
    }
    updateProject.mutate({ id: project.id, ...projectGroupUpdate(groupField, key) });
  };

  const changeLayout = (next: ProjectLayout) => {
    setLayout(next);
    setSelectedIds(new Set());
  };

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
            muted={isProjectArchived(project)}
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
        key: "start_date",
        header: t(($) => $.table.start_date),
        width: 96,
        sortKey: "start_date",
        hideable: true,
        cell: (project) => <ProjectDate value={project.start_date} />,
      },
      {
        key: "due_date",
        header: t(($) => $.table.due_date),
        width: 96,
        sortKey: "due_date",
        hideable: true,
        cell: (project) => (
          <ProjectDate value={project.due_date} dueState={projectDueState(project)} />
        ),
      },
      {
        key: "last_activity",
        header: t(($) => $.table.last_activity),
        width: 104,
        sortKey: "last_activity",
        hideable: true,
        cell: (project) => (
          <span className="tabular-nums">{formatRelativeDate(projectLastActivityAt(project))}</span>
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
      scope === "archived"
        ? {
            // Archived scope: when it was archived, and the way back.
            key: "archived",
            header: t(($) => $.table.archived),
            width: 160,
            core: true,
            interactive: true,
            cell: (project) => (
              <span className="flex w-full items-center justify-between gap-2">
                <span className="tabular-nums">
                  {project.archived_at ? formatRelativeDate(project.archived_at) : null}
                </span>
                <Button variant="outline" size="xs" onClick={() => restore(project)}>
                  <ArchiveRestore />
                  {t(($) => $.page.restore)}
                </Button>
              </span>
            ),
          }
        : {
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
    [t, formatRelativeDate, scope, restore],
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
    archived: t(($) => $.page.scope_archived),
  };
  const groupFieldLabels: Record<ProjectGroupField, string> = {
    status: t(($) => $.toolbar.section_status),
    priority: t(($) => $.toolbar.section_priority),
    lead: t(($) => $.toolbar.section_lead),
    folder: folders.labels.group,
  };
  const noMatches = (
    <CollectionNoMatches
      title={
        scope === "archived" && !hasActiveFilters && !hasSearch
          ? t(($) => $.page.no_archived)
          : t(($) => $.page.no_matches)
      }
      onReset={resetNarrowing}
      hasFilters={hasActiveFilters}
      hasSearch={hasSearch}
    />
  );

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
            <CollectionSearch
              value={search}
              onChange={setSearch}
              label={t(($) => $.page.search)}
            />
            {hasActiveFilters || hasSearch ? (
              <CollectionResetButton onClick={resetNarrowing} />
            ) : null}
          </>
        }
        actions={
          <>
            {/* Sort and grouping apply to both layouts; columns only to the table. */}
            <CollectionDisplayMenu
              columns={columns}
              hiddenColumns={hiddenColumns}
              onToggleColumn={(key) => toggleColumn(key as ProjectColumnKey)}
              sort={sort}
              showColumns={isTable}
              triggerIcon={isTable ? Rows3 : Columns3}
              grouping={{
                value: isTable ? shownGroupBy : boardGroupBy(shownGroupBy),
                options: [
                  ...(isTable ? [{ value: "none", label: t(($) => $.page.group_none) }] : []),
                  ...groupFields.map((field) => ({
                    value: field,
                    label: groupFieldLabels[field],
                  })),
                ],
                onChange: (value) => setGroupBy(value as ProjectGroupBy),
              }}
              extra={
                <div
                  role="group"
                  aria-label={t(($) => $.toolbar.view)}
                  className="grid grid-cols-2 gap-0.5 rounded-lg bg-muted p-0.5"
                >
                  {LAYOUTS.map(({ layout: option, Icon }) => (
                    <Toggle
                      key={option}
                      pressed={layout === option}
                      onPressedChange={() => changeLayout(option)}
                      className="h-auto min-w-0 flex-col gap-1 rounded-md px-0.5 py-1.5 text-caption font-normal text-muted-foreground hover:bg-transparent hover:text-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm aria-pressed:hover:bg-background"
                    >
                      <Icon className="size-4" />
                      <span className="max-w-full truncate">
                        {option === "table" ? t(($) => $.page.view_table) : t(($) => $.page.view_board)}
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
        <ProjectsIntro onCreate={openCreateProject} />
      ) : (
        <>
          {isTable ? (
            <CollectionList
              label={scopeLabels[scope]}
              columns={columns}
              hiddenColumns={hiddenColumns}
              rows={visible}
              groups={tableGroups}
              loading={isLoading}
              getRowId={(project) => project.id}
              getRowHref={(project) => wsPaths.projectDetail(project.id)}
              getRowTitle={(project) => project.title}
              rowMenu={(project) => (
                <ProjectRowActions
                  project={project}
                  pinned={pinnedProjectIds.has(project.id)}
                  canDelete={mayDelete(project)}
                />
              )}
              sort={sort}
              selection={{ selectedIds, onChange: setSelectedIds }}
              empty={noMatches}
            />
          ) : isLoading ? (
            <ProjectBoardLoading />
          ) : visible.length === 0 && (hasActiveFilters || hasSearch || scope === "archived") ? (
            noMatches
          ) : (
            <ProjectBoard
              columns={boardColumns}
              pinnedIds={pinnedProjectIds}
              mayDelete={mayDelete}
              onMove={moveToGroup}
              onUpdate={(project, data) => updateProject.mutate({ id: project.id, ...data })}
            />
          )}

          <ProjectBatchToolbar
            rows={selectedProjects}
            pinnedIds={pinnedProjectIds}
            canDelete={selectedProjects.length > 0 && selectedProjects.every(mayDelete)}
            onClear={() => setSelectedIds(new Set())}
          />
        </>
      )}
    </div>
  );
}
