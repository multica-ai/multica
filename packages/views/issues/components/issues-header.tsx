"use client";

import { useMemo, useState } from "react";
import {
  ArrowDown,
  ArrowUp,
  Check,
  ChevronDown,
  CircleDot,
  Columns3,
  Filter,
  FolderKanban,
  FolderMinus,
  List,
  SignalHigh,
  SlidersHorizontal,
  Tag,
  User,
  UserMinus,
  UserPen,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuCheckboxItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubTrigger,
  DropdownMenuSubContent,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@multica/ui/components/ui/popover";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  ALL_STATUSES,
  STATUS_CONFIG,
  PRIORITY_ORDER,
  PRIORITY_CONFIG,
} from "@multica/core/issues/config";
import { StatusIcon, PriorityIcon } from ".";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { labelListOptions } from "@multica/core/labels/queries";
import { ProjectIcon } from "../../projects/components/project-icon";
import { ActorAvatar } from "../../common/actor-avatar";
import { LabelChip } from "../../labels/label-chip";
import {
  SORT_OPTIONS,
  CARD_PROPERTY_OPTIONS,
  type ActorFilterValue,
} from "@multica/core/issues/stores/view-store";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import {
  useIssuesScopeStore,
  type IssuesScope,
} from "@multica/core/issues/stores/issues-scope-store";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import type { Issue } from "@multica/core/types";

// ---------------------------------------------------------------------------
// HoverCheck — shadcn official pattern (PR #6862)
// ---------------------------------------------------------------------------

const FILTER_ITEM_CLASS =
  "group/fitem pr-1.5! [&>[data-slot=dropdown-menu-checkbox-item-indicator]]:hidden";

function HoverCheck({ checked }: { checked: boolean }) {
  return (
    <div
      className="border-input data-[selected=true]:border-primary data-[selected=true]:bg-primary data-[selected=true]:text-primary-foreground pointer-events-none size-4 shrink-0 rounded-[4px] border transition-all select-none *:[svg]:opacity-0 data-[selected=true]:*:[svg]:opacity-100 opacity-0 group-hover/fitem:opacity-100 group-focus/fitem:opacity-100 data-[selected=true]:opacity-100"
      data-selected={checked}
    >
      <Check className="size-3.5 text-current" />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function getActiveFilterCount(state: {
  statusFilters: string[];
  priorityFilters: string[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
}) {
  let count = 0;
  if (state.statusFilters.length > 0) count++;
  if (state.priorityFilters.length > 0) count++;
  if (state.assigneeFilters.length > 0 || state.includeNoAssignee) count++;
  if (state.creatorFilters.length > 0) count++;
  if (state.projectFilters.length > 0 || state.includeNoProject) count++;
  if (state.labelFilters.length > 0) count++;
  return count;
}

function useIssueCounts(allIssues: Issue[]) {
  return useMemo(() => {
    const status = new Map<string, number>();
    const priority = new Map<string, number>();
    const assignee = new Map<string, number>();
    const creator = new Map<string, number>();
    const project = new Map<string, number>();
    const label = new Map<string, number>();
    let noAssignee = 0;
    let noProject = 0;

    for (const issue of allIssues) {
      status.set(issue.status, (status.get(issue.status) ?? 0) + 1);
      priority.set(issue.priority, (priority.get(issue.priority) ?? 0) + 1);

      if (!issue.assignee_id) {
        noAssignee++;
      } else {
        const aKey = `${issue.assignee_type}:${issue.assignee_id}`;
        assignee.set(aKey, (assignee.get(aKey) ?? 0) + 1);
      }

      const cKey = `${issue.creator_type}:${issue.creator_id}`;
      creator.set(cKey, (creator.get(cKey) ?? 0) + 1);

      if (!issue.project_id) {
        noProject++;
      } else {
        project.set(issue.project_id, (project.get(issue.project_id) ?? 0) + 1);
      }

      if (issue.labels) {
        for (const l of issue.labels) {
          label.set(l.id, (label.get(l.id) ?? 0) + 1);
        }
      }
    }

    return { status, priority, assignee, creator, noAssignee, project, noProject, label };
  }, [allIssues]);
}

// ---------------------------------------------------------------------------
// Scope config
// ---------------------------------------------------------------------------

const SCOPES: { value: IssuesScope; label: string; description: string }[] = [
  { value: "all", label: "All", description: "All issues in this workspace" },
  { value: "members", label: "Members", description: "Issues assigned to team members" },
  { value: "agents", label: "Agents", description: "Issues assigned to AI agents" },
];

// ---------------------------------------------------------------------------
// Actor sub-menu content (shared between Assignee and Creator)
// ---------------------------------------------------------------------------

function ActorSubContent({
  counts,
  selected,
  onToggle,
  showNoAssignee,
  includeNoAssignee,
  onToggleNoAssignee,
  noAssigneeCount,
}: {
  counts: Map<string, number>;
  selected: ActorFilterValue[];
  onToggle: (value: ActorFilterValue) => void;
  showNoAssignee?: boolean;
  includeNoAssignee?: boolean;
  onToggleNoAssignee?: () => void;
  noAssigneeCount?: number;
}) {
  const [search, setSearch] = useState("");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const query = search.trim().toLowerCase();
  const filteredMembers = members.filter((m) =>
    m.name.toLowerCase().includes(query),
  );
  const filteredAgents = agents.filter((a) =>
    !a.archived_at && a.name.toLowerCase().includes(query),
  );

  const isSelected = (type: "member" | "agent", id: string) =>
    selected.some((f) => f.type === type && f.id === id);

  return (
    <>
      <div className="px-2 py-1.5 border-b border-foreground/5">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Filter..."
          className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          autoFocus
        />
      </div>

      <div className="max-h-64 overflow-y-auto p-1">
        {showNoAssignee &&
          (!query || "no assignee".includes(query) || "unassigned".includes(query)) && (
            <DropdownMenuCheckboxItem
              checked={includeNoAssignee ?? false}
              onCheckedChange={() => onToggleNoAssignee?.()}
              className={FILTER_ITEM_CLASS}
            >
              <HoverCheck checked={includeNoAssignee ?? false} />
              <UserMinus className="size-3.5 text-muted-foreground" />
              No assignee
              {(noAssigneeCount ?? 0) > 0 && (
                <span className="ml-auto text-xs text-muted-foreground">
                  {noAssigneeCount}
                </span>
              )}
            </DropdownMenuCheckboxItem>
          )}

        {filteredMembers.length > 0 && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>Members</DropdownMenuLabel>
            {filteredMembers.map((m) => {
              const checked = isSelected("member", m.user_id);
              const count = counts.get(`member:${m.user_id}`) ?? 0;
              return (
                <DropdownMenuCheckboxItem
                  key={m.user_id}
                  checked={checked}
                  onCheckedChange={() =>
                    onToggle({ type: "member", id: m.user_id })
                  }
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={checked} />
                  <ActorAvatar actorType="member" actorId={m.user_id} size={18} />
                  <span className="truncate">{m.name}</span>
                  {count > 0 && (
                    <span className="ml-auto text-xs text-muted-foreground">
                      {count}
                    </span>
                  )}
                </DropdownMenuCheckboxItem>
              );
            })}
          </DropdownMenuGroup>
        )}

        {filteredAgents.length > 0 && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>Agents</DropdownMenuLabel>
            {filteredAgents.map((a) => {
              const checked = isSelected("agent", a.id);
              const count = counts.get(`agent:${a.id}`) ?? 0;
              return (
                <DropdownMenuCheckboxItem
                  key={a.id}
                  checked={checked}
                  onCheckedChange={() =>
                    onToggle({ type: "agent", id: a.id })
                  }
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={checked} />
                  <ActorAvatar actorType="agent" actorId={a.id} size={18} showStatusDot />
                  <span className="truncate">{a.name}</span>
                  {count > 0 && (
                    <span className="ml-auto text-xs text-muted-foreground">
                      {count}
                    </span>
                  )}
                </DropdownMenuCheckboxItem>
              );
            })}
          </DropdownMenuGroup>
        )}

        {filteredMembers.length === 0 && filteredAgents.length === 0 && search && (
          <div className="px-2 py-3 text-center text-sm text-muted-foreground">
            No results
          </div>
        )}
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Project sub-menu content
// ---------------------------------------------------------------------------

function ProjectSubContent({
  counts,
  selected,
  onToggle,
  includeNoProject,
  onToggleNoProject,
  noProjectCount,
}: {
  counts: Map<string, number>;
  selected: string[];
  onToggle: (projectId: string) => void;
  includeNoProject: boolean;
  onToggleNoProject: () => void;
  noProjectCount: number;
}) {
  const [search, setSearch] = useState("");
  const wsId = useWorkspaceId();
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const query = search.trim().toLowerCase();
  const filtered = projects.filter((p) =>
    p.title.toLowerCase().includes(query),
  );

  return (
    <>
      <div className="px-2 py-1.5 border-b border-foreground/5">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Filter..."
          className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          autoFocus
        />
      </div>

      <div className="max-h-64 overflow-y-auto p-1">
        {(!query || "no project".includes(query) || "unassigned".includes(query)) && (
          <DropdownMenuCheckboxItem
            checked={includeNoProject}
            onCheckedChange={() => onToggleNoProject()}
            className={FILTER_ITEM_CLASS}
          >
            <HoverCheck checked={includeNoProject} />
            <FolderMinus className="size-3.5 text-muted-foreground" />
            No project
            {noProjectCount > 0 && (
              <span className="ml-auto text-xs text-muted-foreground">
                {noProjectCount}
              </span>
            )}
          </DropdownMenuCheckboxItem>
        )}

        {filtered.map((p) => {
          const checked = selected.includes(p.id);
          const count = counts.get(p.id) ?? 0;
          return (
            <DropdownMenuCheckboxItem
              key={p.id}
              checked={checked}
              onCheckedChange={() => onToggle(p.id)}
              className={FILTER_ITEM_CLASS}
            >
              <HoverCheck checked={checked} />
              <ProjectIcon project={p} size="sm" />
              <span className="truncate">{p.title}</span>
              {count > 0 && (
                <span className="ml-auto text-xs text-muted-foreground">
                  {count}
                </span>
              )}
            </DropdownMenuCheckboxItem>
          );
        })}

        {filtered.length === 0 && search && (
          <div className="px-2 py-3 text-center text-sm text-muted-foreground">
            No results
          </div>
        )}
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// Label sub-menu content
// ---------------------------------------------------------------------------

function LabelSubContent({
  counts,
  selected,
  onToggle,
}: {
  counts: Map<string, number>;
  selected: string[];
  onToggle: (labelId: string) => void;
}) {
  const [search, setSearch] = useState("");
  const wsId = useWorkspaceId();
  const { data: labels = [] } = useQuery(labelListOptions(wsId));
  const query = search.trim().toLowerCase();
  const filtered = labels.filter((l) => l.name.toLowerCase().includes(query));

  return (
    <>
      <div className="px-2 py-1.5 border-b border-foreground/5">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Filter..."
          className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
          autoFocus
        />
      </div>

      <div className="max-h-64 overflow-y-auto p-1">
        {filtered.map((l) => {
          const checked = selected.includes(l.id);
          const count = counts.get(l.id) ?? 0;
          return (
            <DropdownMenuCheckboxItem
              key={l.id}
              checked={checked}
              onCheckedChange={() => onToggle(l.id)}
              className={FILTER_ITEM_CLASS}
            >
              <HoverCheck checked={checked} />
              <LabelChip label={l} />
              {count > 0 && (
                <span className="ml-auto text-xs text-muted-foreground">
                  {count}
                </span>
              )}
            </DropdownMenuCheckboxItem>
          );
        })}

        {filtered.length === 0 && (
          <div className="px-2 py-3 text-center text-sm text-muted-foreground">
            {search ? "No results" : "No labels yet"}
          </div>
        )}
      </div>
    </>
  );
}

// ---------------------------------------------------------------------------
// IssuesHeader
// ---------------------------------------------------------------------------

export function IssuesHeader({ scopedIssues }: { scopedIssues: Issue[] }) {
  const scope = useIssuesScopeStore((s) => s.scope);
  const setScope = useIssuesScopeStore((s) => s.setScope);

  const viewMode = useViewStore((s) => s.viewMode);
  const statusFilters = useViewStore((s) => s.statusFilters);
  const priorityFilters = useViewStore((s) => s.priorityFilters);
  const assigneeFilters = useViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useViewStore((s) => s.creatorFilters);
  const projectFilters = useViewStore((s) => s.projectFilters);
  const includeNoProject = useViewStore((s) => s.includeNoProject);
  const labelFilters = useViewStore((s) => s.labelFilters);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const cardProperties = useViewStore((s) => s.cardProperties);
  const act = useViewStoreApi().getState();

  const counts = useIssueCounts(scopedIssues);

  const hasActiveFilters =
    getActiveFilterCount({
      statusFilters,
      priorityFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
      projectFilters,
      includeNoProject,
      labelFilters,
    }) > 0;

  const sortLabel =
    SORT_OPTIONS.find((o) => o.value === sortBy)?.label ?? "Manual";

  return (
    <div className="flex h-12 shrink-0 items-center justify-between px-4">
      {/* Left: scope buttons */}
      <div className="flex items-center gap-1">
        {SCOPES.map((s) => (
          <Tooltip key={s.value}>
            <TooltipTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  className={
                    scope === s.value
                      ? "bg-accent text-accent-foreground hover:bg-accent/80"
                      : "text-muted-foreground"
                  }
                  onClick={() => setScope(s.value)}
                >
                  {s.label}
                </Button>
              }
            />
            <TooltipContent side="bottom">{s.description}</TooltipContent>
          </Tooltip>
        ))}
      </div>

      {/* Right: filter + display + view toggle */}
      <div className="flex items-center gap-1">
        {/* Filter */}
        <DropdownMenu>
          <Tooltip>
            <DropdownMenuTrigger
              render={
                <TooltipTrigger
                  render={
                    <Button variant="outline" size="icon-sm" className="relative text-muted-foreground">
                      <Filter className="size-4" />
                      {hasActiveFilters && (
                        <span className="absolute top-0 right-0 size-1.5 rounded-full bg-brand" />
                      )}
                    </Button>
                  }
                />
              }
            />
            <TooltipContent side="bottom">Filter</TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="end" className="w-auto">
            {/* Status */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <CircleDot className="size-3.5" />
                <span className="flex-1">Status</span>
                {statusFilters.length > 0 && (
                  <span className="text-xs text-primary font-medium">
                    {statusFilters.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-48">
                {ALL_STATUSES.map((s) => {
                  const checked = statusFilters.includes(s);
                  const count = counts.status.get(s) ?? 0;
                  return (
                    <DropdownMenuCheckboxItem
                      key={s}
                      checked={checked}
                      onCheckedChange={() => act.toggleStatusFilter(s)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={checked} />
                      <StatusIcon status={s} className="h-3.5 w-3.5" />
                      {STATUS_CONFIG[s].label}
                      {count > 0 && (
                        <span className="ml-auto text-xs text-muted-foreground">
                          {count} {count === 1 ? "issue" : "issues"}
                        </span>
                      )}
                    </DropdownMenuCheckboxItem>
                  );
                })}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Priority */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <SignalHigh className="size-3.5" />
                <span className="flex-1">Priority</span>
                {priorityFilters.length > 0 && (
                  <span className="text-xs text-primary font-medium">
                    {priorityFilters.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-44">
                {PRIORITY_ORDER.map((p) => {
                  const checked = priorityFilters.includes(p);
                  const count = counts.priority.get(p) ?? 0;
                  return (
                    <DropdownMenuCheckboxItem
                      key={p}
                      checked={checked}
                      onCheckedChange={() => act.togglePriorityFilter(p)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={checked} />
                      <PriorityIcon priority={p} />
                      {PRIORITY_CONFIG[p].label}
                      {count > 0 && (
                        <span className="ml-auto text-xs text-muted-foreground">
                          {count} {count === 1 ? "issue" : "issues"}
                        </span>
                      )}
                    </DropdownMenuCheckboxItem>
                  );
                })}
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Assignee */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <User className="size-3.5" />
                <span className="flex-1">Assignee</span>
                {(assigneeFilters.length > 0 || includeNoAssignee) && (
                  <span className="text-xs text-primary font-medium">
                    {assigneeFilters.length + (includeNoAssignee ? 1 : 0)}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52 p-0">
                <ActorSubContent
                  counts={counts.assignee}
                  selected={assigneeFilters}
                  onToggle={act.toggleAssigneeFilter}
                  showNoAssignee
                  includeNoAssignee={includeNoAssignee}
                  onToggleNoAssignee={act.toggleNoAssignee}
                  noAssigneeCount={counts.noAssignee}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Creator */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <UserPen className="size-3.5" />
                <span className="flex-1">Creator</span>
                {creatorFilters.length > 0 && (
                  <span className="text-xs text-primary font-medium">
                    {creatorFilters.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52 p-0">
                <ActorSubContent
                  counts={counts.creator}
                  selected={creatorFilters}
                  onToggle={act.toggleCreatorFilter}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Project */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <FolderKanban className="size-3.5" />
                <span className="flex-1">Project</span>
                {(projectFilters.length > 0 || includeNoProject) && (
                  <span className="text-xs text-primary font-medium">
                    {projectFilters.length + (includeNoProject ? 1 : 0)}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52 p-0">
                <ProjectSubContent
                  counts={counts.project}
                  selected={projectFilters}
                  onToggle={act.toggleProjectFilter}
                  includeNoProject={includeNoProject}
                  onToggleNoProject={act.toggleNoProject}
                  noProjectCount={counts.noProject}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Label */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <Tag className="size-3.5" />
                <span className="flex-1">Label</span>
                {labelFilters.length > 0 && (
                  <span className="text-xs text-primary font-medium">
                    {labelFilters.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52 p-0">
                <LabelSubContent
                  counts={counts.label}
                  selected={labelFilters}
                  onToggle={act.toggleLabelFilter}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Reset */}
            {hasActiveFilters && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={act.clearFilters}>
                  Reset all filters
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>

        {/* Display settings */}
        <Popover>
          <Tooltip>
            <PopoverTrigger
              render={
                <TooltipTrigger
                  render={
                    <Button variant="outline" size="icon-sm" className="text-muted-foreground">
                      <SlidersHorizontal className="size-4" />
                    </Button>
                  }
                />
              }
            />
            <TooltipContent side="bottom">Display settings</TooltipContent>
          </Tooltip>
          <PopoverContent align="end" className="w-64 p-0">
            <div className="border-b px-3 py-2.5">
              <span className="text-xs font-medium text-muted-foreground">
                Ordering
              </span>
              <div className="mt-2 flex items-center gap-1.5">
                <DropdownMenu>
                  <DropdownMenuTrigger
                    render={
                      <Button
                        variant="outline"
                        size="sm"
                        className="flex-1 justify-between text-xs"
                      >
                        {sortLabel}
                        <ChevronDown className="size-3 text-muted-foreground" />
                      </Button>
                    }
                  />
                  <DropdownMenuContent align="start" className="w-auto">
                    {SORT_OPTIONS.map((opt) => (
                      <DropdownMenuItem
                        key={opt.value}
                        onClick={() => act.setSortBy(opt.value)}
                      >
                        {opt.label}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
                <Button
                  variant="outline"
                  size="icon-sm"
                  onClick={() =>
                    act.setSortDirection(sortDirection === "asc" ? "desc" : "asc")
                  }
                  title={sortDirection === "asc" ? "Ascending" : "Descending"}
                >
                  {sortDirection === "asc" ? (
                    <ArrowUp className="size-3.5" />
                  ) : (
                    <ArrowDown className="size-3.5" />
                  )}
                </Button>
              </div>
            </div>

            <div className="px-3 py-2.5">
              <span className="text-xs font-medium text-muted-foreground">
                Card properties
              </span>
              <div className="mt-2 space-y-2">
                {CARD_PROPERTY_OPTIONS.map((opt) => (
                  <label
                    key={opt.key}
                    className="flex cursor-pointer items-center justify-between"
                  >
                    <span className="text-sm">{opt.label}</span>
                    <Switch
                      size="sm"
                      checked={cardProperties[opt.key]}
                      onCheckedChange={() => act.toggleCardProperty(opt.key)}
                    />
                  </label>
                ))}
              </div>
            </div>
          </PopoverContent>
        </Popover>

        {/* View toggle */}
        <DropdownMenu>
          <Tooltip>
            <DropdownMenuTrigger
              render={
                <TooltipTrigger
                  render={
                    <Button variant="outline" size="icon-sm" className="text-muted-foreground">
                      {viewMode === "board" ? (
                        <Columns3 className="size-4" />
                      ) : (
                        <List className="size-4" />
                      )}
                    </Button>
                  }
                />
              }
            />
            <TooltipContent side="bottom">
              {viewMode === "board" ? "Board view" : "List view"}
            </TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="end" className="w-auto">
            <DropdownMenuGroup>
              <DropdownMenuLabel>View</DropdownMenuLabel>
              <DropdownMenuItem onClick={() => act.setViewMode("board")}>
                <Columns3 />
                Board
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => act.setViewMode("list")}>
                <List />
                List
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
    </div>
  );
}
