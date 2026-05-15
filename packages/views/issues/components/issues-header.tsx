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
  PRIORITY_ORDER,
} from "@multica/core/issues/config";
import { StatusIcon, PriorityIcon } from ".";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { labelListOptions } from "@multica/core/labels/queries";
import { ProjectIcon } from "../../projects/components/project-icon";
import { ActorAvatar } from "../../common/actor-avatar";
import { LabelChip } from "../../labels/label-chip";
import {
  SORT_OPTIONS,
  GROUPING_OPTIONS,
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
import { useT } from "../../i18n";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";

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

const SCOPE_VALUES: IssuesScope[] = ["all", "members", "agents"];

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
  showSquads = true,
}: {
  counts: Map<string, number>;
  selected: ActorFilterValue[];
  onToggle: (value: ActorFilterValue) => void;
  showNoAssignee?: boolean;
  includeNoAssignee?: boolean;
  onToggleNoAssignee?: () => void;
  noAssigneeCount?: number;
  showSquads?: boolean;
}) {
  const { t } = useT("issues");
  const [search, setSearch] = useState("");
  const wsId = useWorkspaceId();
  const { data: members = [] } = useQuery(memberListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const query = search.trim().toLowerCase();
  const filteredMembers = members.filter((m) =>
    m.name.toLowerCase().includes(query) || matchesPinyin(m.name, query),
  );
  const filteredAgents = agents.filter((a) =>
    !a.archived_at && (a.name.toLowerCase().includes(query) || matchesPinyin(a.name, query)),
  );
  const filteredSquads = squads.filter((s) =>
    !s.archived_at && (s.name.toLowerCase().includes(query) || matchesPinyin(s.name, query)),
  );

  const isSelected = (type: "member" | "agent" | "squad", id: string) =>
    selected.some((f) => f.type === type && f.id === id);

  return (
    <>
      <div className="px-2 py-1.5 border-b border-foreground/5">
        <input
          type="text"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder={t(($) => $.filters.placeholder)}
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
              {t(($) => $.filters.no_assignee)}
              {(noAssigneeCount ?? 0) > 0 && (
                <span className="ml-auto text-xs text-muted-foreground">
                  {noAssigneeCount}
                </span>
              )}
            </DropdownMenuCheckboxItem>
          )}

        {filteredMembers.length > 0 && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t(($) => $.filters.members_group)}</DropdownMenuLabel>
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
            <DropdownMenuLabel>{t(($) => $.filters.agents_group)}</DropdownMenuLabel>
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

        {showSquads && filteredSquads.length > 0 && (
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t(($) => $.filters.squads_group)}</DropdownMenuLabel>
            {filteredSquads.map((s) => {
              const checked = isSelected("squad", s.id);
              const count = counts.get(`squad:${s.id}`) ?? 0;
              return (
                <DropdownMenuCheckboxItem
                  key={s.id}
                  checked={checked}
                  onCheckedChange={() =>
                    onToggle({ type: "squad", id: s.id })
                  }
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={checked} />
                  <ActorAvatar actorType="squad" actorId={s.id} size={18} />
                  <span className="truncate">{s.name}</span>
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

        {filteredMembers.length === 0 && filteredAgents.length === 0 && (!showSquads || filteredSquads.length === 0) && search && (
          <div className="px-2 py-3 text-center text-sm text-muted-foreground">
            {t(($) => $.filters.no_results)}
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
  const { t } = useT("issues");
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
          placeholder={t(($) => $.filters.placeholder)}
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
            {t(($) => $.filters.no_project)}
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
            {t(($) => $.filters.no_results)}
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
  const { t } = useT("issues");
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
          placeholder={t(($) => $.filters.placeholder)}
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
            {search ? t(($) => $.filters.no_results) : t(($) => $.filters.no_labels)}
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
  const { t } = useT("issues");
  const scope = useIssuesScopeStore((s) => s.scope);
  const setScope = useIssuesScopeStore((s) => s.setScope);
  const SCOPE_LABEL_KEY: Record<IssuesScope, "all_label" | "members_label" | "agents_label"> = {
    all: "all_label",
    members: "members_label",
    agents: "agents_label",
  };
  const SCOPE_DESC_KEY: Record<IssuesScope, "all_description" | "members_description" | "agents_description"> = {
    all: "all_description",
    members: "members_description",
    agents: "agents_description",
  };

  return (
    <div className="flex h-12 shrink-0 items-center justify-between px-4">
      {/* Left: scope buttons */}
      <div className="flex items-center gap-1">
        {SCOPE_VALUES.map((s) => (
          <Tooltip key={s}>
            <TooltipTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  className={
                    scope === s
                      ? "bg-accent text-accent-foreground hover:bg-accent/80"
                      : "text-muted-foreground"
                  }
                  onClick={() => setScope(s)}
                >
                  {t(($) => $.scope[SCOPE_LABEL_KEY[s]])}
                </Button>
              }
            />
            <TooltipContent side="bottom">{t(($) => $.scope[SCOPE_DESC_KEY[s]])}</TooltipContent>
          </Tooltip>
        ))}
      </div>

      <IssueDisplayControls scopedIssues={scopedIssues} />
    </div>
  );
}

export function IssueDisplayControls({ scopedIssues }: { scopedIssues: Issue[] }) {
  const { t } = useT("issues");
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
  const grouping = useViewStore((s) => s.grouping);
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

  const SORT_LABEL_KEY: Record<typeof SORT_OPTIONS[number]["value"], "sort_manual" | "sort_priority" | "sort_due_date" | "sort_created" | "sort_title"> = {
    position: "sort_manual",
    priority: "sort_priority",
    due_date: "sort_due_date",
    created_at: "sort_created",
    title: "sort_title",
  };
  const GROUPING_LABEL_KEY: Record<typeof GROUPING_OPTIONS[number]["value"], "group_status" | "group_assignee"> = {
    status: "group_status",
    assignee: "group_assignee",
  };
  const CARD_PROPERTY_LABEL_KEY: Record<typeof CARD_PROPERTY_OPTIONS[number]["key"], "card_priority" | "card_description" | "card_assignee" | "card_due_date" | "card_project" | "card_labels" | "card_child_progress"> = {
    priority: "card_priority",
    description: "card_description",
    assignee: "card_assignee",
    dueDate: "card_due_date",
    project: "card_project",
    labels: "card_labels",
    childProgress: "card_child_progress",
  };
  const sortLabel = t(($) => $.display[SORT_LABEL_KEY[sortBy]]);
  const groupingLabel = t(($) => $.display[GROUPING_LABEL_KEY[grouping]]);

  return (
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
            <TooltipContent side="bottom">{t(($) => $.filters.tooltip)}</TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="end" className="w-auto">
            {/* Status */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <CircleDot className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_status)}</span>
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
                      {t(($) => $.status[s])}
                      {count > 0 && (
                        <span className="ml-auto text-xs text-muted-foreground">
                          {t(($) => $.filters.issue_count, { count })}
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
                <span className="flex-1">{t(($) => $.filters.section_priority)}</span>
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
                      {t(($) => $.priority[p])}
                      {count > 0 && (
                        <span className="ml-auto text-xs text-muted-foreground">
                          {t(($) => $.filters.issue_count, { count })}
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
                <span className="flex-1">{t(($) => $.filters.section_assignee)}</span>
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
                <span className="flex-1">{t(($) => $.filters.section_creator)}</span>
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
                  showSquads={false}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Project */}
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <FolderKanban className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_project)}</span>
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
                <span className="flex-1">{t(($) => $.filters.section_label)}</span>
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
                  {t(($) => $.filters.reset)}
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
            <TooltipContent side="bottom">{t(($) => $.display.tooltip)}</TooltipContent>
          </Tooltip>
          <PopoverContent align="end" className="w-64 p-0">
            {viewMode === "board" && (
              <div className="border-b px-3 py-2.5">
                <span className="text-xs font-medium text-muted-foreground">
                  {t(($) => $.display.grouping_section)}
                </span>
                <div className="mt-2">
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          variant="outline"
                          size="sm"
                          className="w-full justify-between text-xs"
                        >
                          {groupingLabel}
                          <ChevronDown className="size-3 text-muted-foreground" />
                        </Button>
                      }
                    />
                    <DropdownMenuContent align="start" className="w-auto">
                      {GROUPING_OPTIONS.map((opt) => (
                        <DropdownMenuItem
                          key={opt.value}
                          onClick={() => act.setGrouping(opt.value)}
                        >
                          {t(($) => $.display[GROUPING_LABEL_KEY[opt.value]])}
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
            )}

            <div className="border-b px-3 py-2.5">
              <span className="text-xs font-medium text-muted-foreground">
                {t(($) => $.display.ordering_section)}
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
                        {t(($) => $.display[SORT_LABEL_KEY[opt.value]])}
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
                  title={sortDirection === "asc" ? t(($) => $.display.ascending_title) : t(($) => $.display.descending_title)}
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
                {t(($) => $.display.card_properties_section)}
              </span>
              <div className="mt-2 space-y-2">
                {CARD_PROPERTY_OPTIONS.map((opt) => (
                  <label
                    key={opt.key}
                    className="flex cursor-pointer items-center justify-between"
                  >
                    <span className="text-sm">{t(($) => $.display[CARD_PROPERTY_LABEL_KEY[opt.key]])}</span>
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
              {viewMode === "board" ? t(($) => $.view.tooltip_board) : t(($) => $.view.tooltip_list)}
            </TooltipContent>
          </Tooltip>
          <DropdownMenuContent align="end" className="w-auto">
            <DropdownMenuGroup>
              <DropdownMenuLabel>{t(($) => $.view.section)}</DropdownMenuLabel>
              <DropdownMenuItem onClick={() => act.setViewMode("board")}>
                <Columns3 />
                {t(($) => $.view.board)}
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => act.setViewMode("list")}>
                <List />
                {t(($) => $.view.list)}
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
    </div>
  );
}
