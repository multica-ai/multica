"use client";

import { cloneElement, useEffect, useMemo, useRef, useState } from "react";
import {
  Activity,
  Bot,
  CalendarClock,
  CalendarDays,
  ChartGantt,
  ChevronDown,
  CircleDashed,
  CircleDot,
  CircleUser,
  Columns3,
  Filter,
  FolderKanban,
  FolderMinus,
  List,
  ListTree,
  Search,
  SignalHigh,
  SlidersHorizontal,
  Tag,
  Table2,
  User,
  UserMinus,
  UserPen,
  Waves,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuCheckboxItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
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
import { Calendar } from "@multica/ui/components/ui/calendar";
import { Switch } from "@multica/ui/components/ui/switch";
import {
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectGroup,
  SelectItem,
} from "@multica/ui/components/ui/select";
import { Toggle } from "@multica/ui/components/ui/toggle";
import {
  PRIORITY_DISPLAY_ORDER,
} from "@multica/core/issues/config";
import { StatusIcon, PriorityIcon } from ".";
import {
  FilterOperator,
  RELATION_LABEL_KEY,
  useFilterChips,
  type FilterChip,
} from "./filter-chips-bar";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { PROJECT_STATUS_CONFIG, PROJECT_STATUS_ORDER } from "@multica/core/projects/config";
import { labelListOptions } from "@multica/core/labels/queries";
import { propertyListOptions } from "@multica/core/properties";
import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";
import type {
  Issue,
  IssueTableFacetSpec,
  IssueTableFacetsResponse,
  ProjectStatus,
} from "@multica/core/types";
import { isFilterablePropertyType, propertyFilterValueKey, type PropertyFilterValue } from "@multica/core/types";
import { ProjectIcon } from "../../projects/components/project-icon";
import { useProjectStatusLabels } from "../../projects/components/labels";
import { ActorAvatar } from "../../common/actor-avatar";
import { PropertyIcon } from "../../common/property-icon";
import { sortDirectionLabelKey } from "../utils/sort-direction";
import { LabelChip } from "../../labels/label-chip";
import {
  SORT_OPTIONS,
  GROUPING_OPTIONS,
  SWIMLANE_GROUPINGS,
  CARD_PROPERTY_OPTIONS,
  cardPropertyOptionsForView,
  sortOptionsForView,
  ISSUE_RELATION_FILTERS,
  type ActorFilterValue,
  type IssueDateField,
  type IssueDateFilter,
  type IssueRelationFilter,
  type NegatableFilterDimension,
  type SortField,
  type IssueGrouping,
  type SwimlaneGrouping,
  type TableGrouping,
  type ViewMode,
} from "@multica/core/issues/stores/view-store";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import {
  advancedFilterSignature,
  countAdvancedConditions,
  DUE_DATE_FILTERS,
  NEGATABLE_FILTER_DIMENSIONS,
  simpleFieldValues,
  type AdvancedFilterGroup,
} from "@multica/core/issues/stores/filter-tree";
import { actorFilterKey, type IssueViewBaseline } from "@multica/core/issue-views/baseline";
import { addDaysDateOnly, dateOnlyToLocalDate, formatDateOnly, toDateOnly, todayDateOnly } from "@multica/core/issues/date";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";
import { useStatusOptions } from "../utils/status-options";
import { useColumnStatuses } from "../surface/column-statuses-context";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { TableColumnPicker } from "./table-view";
import { DisplayChip, SortDirectionButton } from "./display-parts";
import { AdvancedFilterPanel, stopMenuSearchKeys } from "./advanced-filter-panel";
import { PropertyFilterOptions } from "./property-filter-options";
import {
  DUE_DATE_LABEL_KEY,
  FILTER_FIELD_ICONS,
  matchesFilterQuery,
  toggleSimpleFilterValue,
  useFilterFieldLabels,
  useFilterValueCatalog,
} from "./filter-field-options";

type LocalDateRange = {
  from: Date | undefined;
  to?: Date;
};

// Helpers

function getActiveFilterCount(
  state: {
    statusFilters: string[];
    priorityFilters: string[];
    assigneeFilters: ActorFilterValue[];
    includeNoAssignee: boolean;
    creatorFilters: ActorFilterValue[];
    projectFilters: string[];
    includeNoProject: boolean;
    projectStatusFilters: ProjectStatus[];
    labelFilters: string[];
    relationFilters: string[];
    dueDateFilters: string[];
    negatedFilters: NegatableFilterDimension[];
    propertyFilters?: Record<string, PropertyFilterValue[]>;
    advancedFilter?: AdvancedFilterGroup | null;
    dateFilter?: IssueDateFilter | null;
    agentRunningFilter?: boolean;
  },
  // Inside a saved view only the user's additions on top of the view's own
  // query count — the baseline is the view, not "active filtering".
  baseline?: IssueViewBaseline,
) {
  let count = 0;
  const negated = new Set(state.negatedFilters);
  // Flipping a view's own "is" to "is not" makes the whole dimension new.
  const fixedFor = (dimension: NegatableFilterDimension, fixed?: Set<string>) =>
    baseline && baseline.negated.has(dimension) === negated.has(dimension) ? fixed : undefined;
  const delta = (values: string[], fixed?: Set<string>) =>
    fixed ? values.filter((v) => !fixed.has(v)).length : values.length;
  if (delta(state.statusFilters, fixedFor("status", baseline?.status)) > 0) count++;
  if (delta(state.priorityFilters, fixedFor("priority", baseline?.priority)) > 0) count++;
  const assigneeFixed = fixedFor("assignee", baseline?.assignee);
  const assigneeDelta =
    delta(state.assigneeFilters.map(actorFilterKey), assigneeFixed) > 0 ||
    (state.includeNoAssignee && !(assigneeFixed && baseline?.includeNoAssignee));
  if (assigneeDelta) count++;
  if (delta(state.creatorFilters.map(actorFilterKey), fixedFor("creator", baseline?.creator)) > 0) count++;
  const projectFixed = fixedFor("project", baseline?.project);
  const projectDelta =
    delta(state.projectFilters, projectFixed) > 0 ||
    (state.includeNoProject && !(projectFixed && baseline?.includeNoProject));
  if (projectDelta) count++;
  if (delta(state.projectStatusFilters, fixedFor("projectStatus", baseline?.projectStatus)) > 0) count++;
  if (delta(state.labelFilters, fixedFor("label", baseline?.label)) > 0) count++;
  if (delta(state.relationFilters, fixedFor("relation", baseline?.relation)) > 0) count++;
  if (delta(state.dueDateFilters, fixedFor("dueDate", baseline?.dueDate)) > 0) count++;
  const advancedFilter = state.advancedFilter ?? null;
  if (
    !baseline ||
    advancedFilterSignature(advancedFilter) !== advancedFilterSignature(baseline.raw.advancedFilter)
  ) {
    count += countAdvancedConditions(advancedFilter);
  }
  for (const [id, selected] of Object.entries(state.propertyFilters ?? {})) {
    // Property members can be operator objects — compare through their
    // canonical keys so a view-fixed operator still cancels out.
    const fixed = baseline?.property.get(id);
    const deltaCount = fixed
      ? selected.filter((v) => !fixed.has(propertyFilterValueKey(v))).length
      : selected.length;
    if (deltaCount > 0) count++;
  }
  if (state.dateFilter) count++;
  if (state.agentRunningFilter && !baseline?.agentRunning) count++;
  return count;
}

function shortDateLabel(dateOnly: string) {
  return formatDateOnly(dateOnly, { month: "short", day: "numeric" }) || dateOnly;
}

function normalizeDateRange(from: Date, to: Date) {
  return from <= to ? [from, to] as const : [to, from] as const;
}

const DATE_FIELD_LABEL_KEY: Record<IssueDateField, "date_field_created" | "date_field_updated"> = {
  created_at: "date_field_created",
  updated_at: "date_field_updated",
};

/** Feeding this to useIssueCounts hides every per-option badge (badges only
 *  render at count > 0) without touching the option lists themselves. */
const NO_COUNT_ISSUES: Issue[] = [];

function useIssueCounts(
  allIssues: Issue[],
  serverFacets?: IssueTableFacetsResponse,
) {
  return useMemo(() => {
    const status = new Map<string, number>();
    const priority = new Map<string, number>();
    const assignee = new Map<string, number>();
    const creator = new Map<string, number>();
    const project = new Map<string, number>();
    const label = new Map<string, number>();
    // property definition id → option key → count. Checkbox values count
    // under the "true"/"false" pseudo-option keys the filter store uses.
    const property = new Map<string, Map<string, number>>();
    let noAssignee = 0;
    let noProject = 0;

    if (serverFacets) {
      for (const facet of serverFacets.facets) {
        const target =
          facet.kind === "status"
            ? status
            : facet.kind === "priority"
              ? priority
              : facet.kind === "assignee"
                ? assignee
                : facet.kind === "creator"
                  ? creator
                  : facet.kind === "project"
                    ? project
                    : facet.kind === "label"
                      ? label
                      : null;
        if (facet.kind === "property" && facet.property_id) {
          property.set(
            facet.property_id,
            new Map(facet.values.map((value) => [value.key, value.count])),
          );
          continue;
        }
        for (const value of facet.values) {
          if (facet.kind === "assignee" && value.key === "__none__") {
            noAssignee = value.count;
          } else if (facet.kind === "project" && value.key === "__none__") {
            noProject = value.count;
          } else {
            target?.set(value.key, value.count);
          }
        }
      }
      return { status, priority, assignee, creator, noAssignee, project, noProject, label, property };
    }

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

      for (const [propertyId, value] of Object.entries(issue.properties ?? {})) {
        const optionKeys =
          typeof value === "string"
            ? [value]
            : Array.isArray(value)
              ? value
              : typeof value === "boolean"
                ? [String(value)]
                : [];
        if (optionKeys.length === 0) continue;
        let perOption = property.get(propertyId);
        if (!perOption) {
          perOption = new Map<string, number>();
          property.set(propertyId, perOption);
        }
        for (const key of optionKeys) {
          perOption.set(key, (perOption.get(key) ?? 0) + 1);
        }
      }
    }

    return { status, priority, assignee, creator, noAssignee, project, noProject, label, property };
  }, [allIssues, serverFacets]);
}

// Actor sub-menu content (shared between Assignee and Creator)

function ActorSubContent({
  counts,
  selected,
  onToggle,
  showNoAssignee,
  includeNoAssignee,
  onToggleNoAssignee,
  noAssigneeCount,
  showSquads = true,
  fixedKeys,
  noAssigneeFixed = false,
  fixedTitle,
}: {
  counts: Map<string, number>;
  selected: ActorFilterValue[];
  onToggle: (value: ActorFilterValue) => void;
  showNoAssignee?: boolean;
  includeNoAssignee?: boolean;
  onToggleNoAssignee?: () => void;
  noAssigneeCount?: number;
  showSquads?: boolean;
  /** Actor keys (`type:id`) fixed by the open saved view. */
  fixedKeys?: Set<string>;
  noAssigneeFixed?: boolean;
  fixedTitle?: string;
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
          className="w-full bg-transparent text-body placeholder:text-muted-foreground outline-none"
          autoFocus
        />
      </div>

      <div className="max-h-64 overflow-y-auto p-1">
        {showNoAssignee &&
          (!query || "no assignee".includes(query) || "unassigned".includes(query)) && (
            <DropdownMenuCheckboxItem
              checked={includeNoAssignee ?? false}
              disabled={noAssigneeFixed}
              title={noAssigneeFixed ? fixedTitle : undefined}
              onCheckedChange={() => onToggleNoAssignee?.()}
              className={FILTER_ITEM_CLASS}
            >
              <HoverCheck checked={includeNoAssignee ?? false} />
              <UserMinus className="size-3.5 text-muted-foreground" />
              {t(($) => $.filters.no_assignee)}
              {(noAssigneeCount ?? 0) > 0 && (
                <span className="ml-auto text-caption text-muted-foreground">
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
                  disabled={fixedKeys?.has(`member:${m.user_id}`) === true}
                  title={fixedKeys?.has(`member:${m.user_id}`) === true ? fixedTitle : undefined}
                  onCheckedChange={() =>
                    onToggle({ type: "member", id: m.user_id })
                  }
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={checked} />
                  <ActorAvatar actorType="member" actorId={m.user_id} size="sm" />
                  <span className="truncate">{m.name}</span>
                  {count > 0 && (
                    <span className="ml-auto text-caption text-muted-foreground">
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
                  disabled={fixedKeys?.has(`agent:${a.id}`) === true}
                  title={fixedKeys?.has(`agent:${a.id}`) === true ? fixedTitle : undefined}
                  onCheckedChange={() =>
                    onToggle({ type: "agent", id: a.id })
                  }
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={checked} />
                  <ActorAvatar actorType="agent" actorId={a.id} size="sm" showStatusDot />
                  <span className="truncate">{a.name}</span>
                  {count > 0 && (
                    <span className="ml-auto text-caption text-muted-foreground">
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
                  disabled={fixedKeys?.has(`squad:${s.id}`) === true}
                  title={fixedKeys?.has(`squad:${s.id}`) === true ? fixedTitle : undefined}
                  onCheckedChange={() =>
                    onToggle({ type: "squad", id: s.id })
                  }
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={checked} />
                  <ActorAvatar actorType="squad" actorId={s.id} size="sm" />
                  <span className="truncate">{s.name}</span>
                  {count > 0 && (
                    <span className="ml-auto text-caption text-muted-foreground">
                      {count}
                    </span>
                  )}
                </DropdownMenuCheckboxItem>
              );
            })}
          </DropdownMenuGroup>
        )}

        {filteredMembers.length === 0 && filteredAgents.length === 0 && (!showSquads || filteredSquads.length === 0) && search && (
          <div className="px-2 py-3 text-center text-body text-muted-foreground">
            {t(($) => $.filters.no_results)}
          </div>
        )}
      </div>
    </>
  );
}

// Project sub-menu content

function ProjectSubContent({
  counts,
  selected,
  onToggle,
  includeNoProject,
  onToggleNoProject,
  noProjectCount,
  fixedIds,
  noProjectFixed = false,
  fixedTitle,
}: {
  counts: Map<string, number>;
  selected: string[];
  onToggle: (projectId: string) => void;
  includeNoProject: boolean;
  onToggleNoProject: () => void;
  noProjectCount: number;
  fixedIds?: Set<string>;
  noProjectFixed?: boolean;
  fixedTitle?: string;
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
          className="w-full bg-transparent text-body placeholder:text-muted-foreground outline-none"
          autoFocus
        />
      </div>

      <div className="max-h-64 overflow-y-auto p-1">
        {(!query || "no project".includes(query) || "unassigned".includes(query)) && (
          <DropdownMenuCheckboxItem
            checked={includeNoProject}
            disabled={noProjectFixed}
            title={noProjectFixed ? fixedTitle : undefined}
            onCheckedChange={() => onToggleNoProject()}
            className={FILTER_ITEM_CLASS}
          >
            <HoverCheck checked={includeNoProject} />
            <FolderMinus className="size-3.5 text-muted-foreground" />
            {t(($) => $.filters.no_project)}
            {noProjectCount > 0 && (
              <span className="ml-auto text-caption text-muted-foreground">
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
              disabled={fixedIds?.has(p.id) === true}
              title={fixedIds?.has(p.id) === true ? fixedTitle : undefined}
              onCheckedChange={() => onToggle(p.id)}
              className={FILTER_ITEM_CLASS}
            >
              <HoverCheck checked={checked} />
              <ProjectIcon project={p} size="sm" />
              <span className="truncate">{p.title}</span>
              {count > 0 && (
                <span className="ml-auto text-caption text-muted-foreground">
                  {count}
                </span>
              )}
            </DropdownMenuCheckboxItem>
          );
        })}

        {filtered.length === 0 && search && (
          <div className="px-2 py-3 text-center text-body text-muted-foreground">
            {t(($) => $.filters.no_results)}
          </div>
        )}
      </div>
    </>
  );
}

// Project status sub-menu content

function ProjectStatusSubContent({
  selected,
  onToggle,
  fixedStatuses,
  fixedTitle,
}: {
  selected: ProjectStatus[];
  onToggle: (status: ProjectStatus) => void;
  fixedStatuses?: Set<string>;
  fixedTitle?: string;
}) {
  const statusLabels = useProjectStatusLabels();
  return (
    <div className="p-1">
      {PROJECT_STATUS_ORDER.map((status) => {
        const checked = selected.includes(status);
        const fixed = fixedStatuses?.has(status) === true;
        return (
          <DropdownMenuCheckboxItem
            key={status}
            checked={checked}
            disabled={fixed}
            title={fixed ? fixedTitle : undefined}
            onCheckedChange={() => onToggle(status)}
            className={FILTER_ITEM_CLASS}
          >
            <HoverCheck checked={checked} />
            <span
              className={`size-2 rounded-full ${PROJECT_STATUS_CONFIG[status].dotColor}`}
            />
            {statusLabels[status]}
          </DropdownMenuCheckboxItem>
        );
      })}
    </div>
  );
}

// Label sub-menu content

function LabelSubContent({
  counts,
  selected,
  onToggle,
  fixedIds,
  fixedTitle,
}: {
  counts: Map<string, number>;
  selected: string[];
  onToggle: (labelId: string) => void;
  fixedIds?: Set<string>;
  fixedTitle?: string;
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
          className="w-full bg-transparent text-body placeholder:text-muted-foreground outline-none"
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
              disabled={fixedIds?.has(l.id) === true}
              title={fixedIds?.has(l.id) === true ? fixedTitle : undefined}
              onCheckedChange={() => onToggle(l.id)}
              className={FILTER_ITEM_CLASS}
            >
              <HoverCheck checked={checked} />
              <LabelChip label={l} />
              {count > 0 && (
                <span className="ml-auto text-caption text-muted-foreground">
                  {count}
                </span>
              )}
            </DropdownMenuCheckboxItem>
          );
        })}

        {filtered.length === 0 && (
          <div className="px-2 py-3 text-center text-body text-muted-foreground">
            {search ? t(($) => $.filters.no_results) : t(($) => $.filters.no_labels)}
          </div>
        )}
      </div>
    </>
  );
}

// Date sub-menu content

function DateSubContent({
  value,
  onChange,
}: {
  value: IssueDateFilter | null;
  onChange: (filter: IssueDateFilter | null) => void;
}) {
  const { t } = useT("issues");
  const [field, setField] = useState<IssueDateField>(value?.field ?? "created_at");
  // Controlled so Apply can dismiss the calendar while the filter menu
  // itself stays open (menu-wide rule: filter edits never close the menu).
  const [calendarOpen, setCalendarOpen] = useState(false);
  const [range, setRange] = useState<LocalDateRange | undefined>(() => {
    if (!value) return undefined;
    const from = dateOnlyToLocalDate(value.from);
    if (!from) return undefined;
    return { from, to: dateOnlyToLocalDate(value.to) };
  });

  const setFieldValue = (next: IssueDateField) => {
    setField(next);
    if (value) onChange({ ...value, field: next });
  };

  const applyPreset = (days: 1 | 3 | 7) => {
    onChange({
      field,
      from: addDaysDateOnly(1 - days),
      to: todayDateOnly(),
    });
  };

  const applyCustom = () => {
    if (!range?.from) return;
    const [from, to] = normalizeDateRange(range.from, range.to ?? range.from);
    onChange({
      field,
      from: toDateOnly(from),
      to: toDateOnly(to),
    });
    setCalendarOpen(false);
  };

  return (
    <>
      <DropdownMenuGroup>
        <DropdownMenuLabel>{t(($) => $.filters.date_field)}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={field} onValueChange={(next) => setFieldValue(next as IssueDateField)}>
          {(["created_at", "updated_at"] as const).map((option) => (
            // Picking the date field is a parameter for the presets below, not
            // the final action — keep the menu open so the user can continue
            // to a preset or the custom range.
            <DropdownMenuRadioItem key={option} value={option} closeOnClick={false}>
              {t(($) => $.filters[DATE_FIELD_LABEL_KEY[option]])}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuGroup>

      <DropdownMenuSeparator />
      {/* Presets set a filter value, same semantics as toggling a status
          checkbox — keep the menu open so multi-dimension refinement
          continues. Only Reset (terminal) closes. */}
      <DropdownMenuItem closeOnClick={false} onClick={() => applyPreset(1)}>
        {t(($) => $.filters.date_today)}
      </DropdownMenuItem>
      <DropdownMenuItem closeOnClick={false} onClick={() => applyPreset(3)}>
        {t(($) => $.filters.date_last_3_days)}
      </DropdownMenuItem>
      <DropdownMenuItem closeOnClick={false} onClick={() => applyPreset(7)}>
        {t(($) => $.filters.date_last_7_days)}
      </DropdownMenuItem>

      <div className="px-1.5 py-1">
        <Popover open={calendarOpen} onOpenChange={setCalendarOpen}>
          <PopoverTrigger
            render={
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-full justify-start px-0 text-body font-normal"
              >
                {t(($) => $.filters.date_custom_range)}
              </Button>
            }
          />
          <PopoverContent align="start" side="right" className="w-auto gap-0 p-0">
            <Calendar
              mode="range"
              selected={range}
              onSelect={(next) => setRange(next)}
              captionLayout="dropdown"
            />
            <div className="flex justify-end border-t p-2">
              <Button size="sm" onClick={applyCustom} disabled={!range?.from}>
                {t(($) => $.filters.date_apply)}
              </Button>
            </div>
          </PopoverContent>
        </Popover>
      </div>

      {value && (
        <>
          <DropdownMenuSeparator />
          <DropdownMenuItem
            closeOnClick={false}
            onClick={() => {
              setRange(undefined);
              onChange(null);
            }}
          >
            {t(($) => $.filters.date_clear)}
          </DropdownMenuItem>
        </>
      )}
    </>
  );
}

// ViewRefreshIndicator

/**
 * Neutral "view is revalidating" slot for the header controls cluster.
 * Driven by the surface's isRefreshing (placeholder-backed revalidation:
 * sort/date change, grouped-board filter change) — trigger-agnostic on
 * purpose, so any view change that puts a request in flight lights it.
 *
 * The slot is fixed-width so appearing/disappearing never shifts the
 * controls; the 300ms appear delay keeps fast networks indicator-free
 * (NN/g: sub-second responses need no feedback) while slow ones get a
 * "working on it" signal instead of a dead click.
 */
export function ViewRefreshIndicator({ active }: { active: boolean }) {
  return (
    <span className="flex w-4 shrink-0 items-center justify-center">
      {active && (
        <span className="animate-in fade-in fill-mode-backwards [animation-delay:300ms]">
          <Spinner className="size-3.5 text-muted-foreground" />
        </span>
      )}
    </span>
  );
}

/** What a dimension is filtering on, in its row of the filter menu: the
 *  relation word, the values, and a dot when it differs from the saved view. */
function FilterTriggerValue({
  chip,
  modified = false,
}: {
  chip: FilterChip | undefined;
  modified?: boolean;
}) {
  if (!chip) return null;
  return (
    <span className="ml-3 flex min-w-0 items-center gap-1 text-caption">
      <FilterOperator negated={chip.negated} />
      <span className="max-w-40 truncate font-medium text-primary">{chip.value}</span>
      {modified && <ModifiedDot />}
    </span>
  );
}

function ModifiedDot() {
  const { t } = useT("issues");
  const label = t(($) => $.filters.modified_hint);
  return <span role="img" aria-label={label} title={label} className="size-1.5 shrink-0 rounded-full bg-brand" />;
}

const OPERATOR_SEGMENT_CLASS =
  "h-6 flex-1 justify-center rounded-md px-3 py-0 text-caption text-muted-foreground focus:bg-background/60 data-checked:bg-background data-checked:font-medium data-checked:text-foreground data-checked:shadow-xs [&>[data-slot=dropdown-menu-radio-item-indicator]]:hidden";

/** "Is" / "is not" for one dimension, heading its submenu. Radio items keep
 *  it in the menu's keyboard order; the selection below is left as it is. */
function FilterOperatorSwitch({ dimension }: { dimension: NegatableFilterDimension }) {
  const { t } = useT("issues");
  const negated = useViewStore((s) => s.negatedFilters.includes(dimension));
  const setFilterNegated = useViewStore((s) => s.setFilterNegated);
  return (
    <>
      <DropdownMenuRadioGroup
        aria-label={t(($) => $.filters.operator_label)}
        value={negated ? "is_not" : "is"}
        onValueChange={(value) => setFilterNegated(dimension, value === "is_not")}
        className="m-1 flex gap-0.5 rounded-lg bg-muted p-0.5"
      >
        <DropdownMenuRadioItem value="is" closeOnClick={false} className={OPERATOR_SEGMENT_CLASS}>
          {t(($) => $.filters.operator_is)}
        </DropdownMenuRadioItem>
        <DropdownMenuRadioItem value="is_not" closeOnClick={false} className={OPERATOR_SEGMENT_CLASS}>
          {t(($) => $.filters.operator_is_not)}
        </DropdownMenuRadioItem>
      </DropdownMenuRadioGroup>
      <DropdownMenuSeparator />
    </>
  );
}

const RELATION_ICONS: Record<IssueRelationFilter, typeof User> = {
  assigned: CircleUser,
  created: UserPen,
  my_agents: Bot,
};

/** Read-only sentence form of the active filters, shown on hover so a closed
 *  menu still says what the surface is narrowed to. */
function FilterSummary({
  title,
  chips,
  modifiedKeys,
}: {
  title: string;
  chips: FilterChip[];
  modifiedKeys?: ReadonlySet<string>;
}) {
  const { t } = useT("issues");
  const modifiedCount = chips.filter((chip) => modifiedKeys?.has(chip.key)).length;
  return (
    <div className="flex w-72 flex-col gap-1 py-1">
      <div className="mb-1 flex items-center gap-1.5 font-medium">
        <Filter className="size-3.5" />
        <span className="truncate">{title}</span>
      </div>
      {chips.map((chip) => (
        <div key={chip.key} className="flex min-w-0 items-center gap-1.5">
          {chip.icon}
          <span className="shrink-0 text-muted-foreground">{chip.label}</span>
          <FilterOperator negated={chip.negated} />
          <span className="min-w-0 truncate">{chip.value}</span>
          {modifiedKeys?.has(chip.key) && <ModifiedDot />}
        </div>
      ))}
      {modifiedCount > 0 && (
        <div className="mt-1 flex items-center gap-1.5 border-t border-border pt-1.5 text-muted-foreground">
          <ModifiedDot />
          {t(($) => $.filters.unsaved_changes, { count: modifiedCount })}
        </div>
      )}
    </div>
  );
}

/**
 * The full filter menu (every dimension, search, counts) behind a caller-
 * supplied trigger. Counts follow `scopedIssues` — pass nothing to hide
 * them.
 */
export function IssueFilterMenu({
  trigger,
  tooltip,
  scopedIssues = NO_COUNT_ISSUES,
  facetCountsExact = true,
  tableFacetCounts,
  onTableFacetChange,
  dateFilter = null,
  onDateFilterChange,
  onOpenChange,
  freezeAnchor = false,
  viewBaseline,
  leadingSections,
  align = "end",
  showRelation = true,
  hiddenFields,
  modifiedKeys,
  summaryTitle,
}: {
  trigger: React.ReactElement;
  align?: "start" | "end";
  /** Rendered above the filter dimensions, e.g. the surface's scope axis. */
  leadingSections?: React.ReactNode;
  tooltip?: string;
  /** Offer "relation to me". A surface already scoped to me hides it. */
  showRelation?: boolean;
  /** Fields the scope already fixes, left out of search and advanced mode. */
  hiddenFields?: readonly NegatableFilterDimension[];
  /** Chip keys whose condition differs from the open saved view. */
  modifiedKeys?: ReadonlySet<string>;
  /** With active filters, hovering the trigger shows them as sentences under
   *  this title instead of `tooltip`. */
  summaryTitle?: string;
  scopedIssues?: Issue[];
  facetCountsExact?: boolean;
  tableFacetCounts?: IssueTableFacetsResponse;
  onTableFacetChange?: (facet: IssueTableFacetSpec | null) => void;
  dateFilter?: IssueDateFilter | null;
  onDateFilterChange?: (filter: IssueDateFilter | null) => void;
  /** Mirrors the menu's open state. */
  onOpenChange?: (open: boolean) => void;
  /** Pin the open menu to the trigger's position AT OPEN TIME (a virtual
   *  anchor). With it, chips growing or wrapping can reflow the trigger
   *  freely without the open menu ever moving under the pointer. */
  freezeAnchor?: boolean;
  /** Open saved view: values the view fixes render checked-and-disabled
   *  ("already in this view"); everything else stays selectable. */
  viewBaseline?: IssueViewBaseline;
}) {
  const { t } = useT("issues");
  const triggerRef = useRef<HTMLElement>(null);
  const [frozenAnchor, setFrozenAnchor] = useState<{ getBoundingClientRect: () => DOMRect } | null>(null);
  const statusFilters = useViewStore((s) => s.statusFilters);
  const priorityFilters = useViewStore((s) => s.priorityFilters);
  const assigneeFilters = useViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useViewStore((s) => s.creatorFilters);
  const projectFilters = useViewStore((s) => s.projectFilters);
  const includeNoProject = useViewStore((s) => s.includeNoProject);
  const projectStatusFilters = useViewStore((s) => s.projectStatusFilters);
  const labelFilters = useViewStore((s) => s.labelFilters);
  const relationFilters = useViewStore((s) => s.relationFilters);
  const dueDateFilters = useViewStore((s) => s.dueDateFilters);
  const advancedFilter = useViewStore((s) => s.advancedFilter);
  const negatedFilters = useViewStore((s) => s.negatedFilters);
  const propertyFilters = useViewStore((s) => s.propertyFilters);
  const agentRunningFilter = useViewStore((s) => s.agentRunningFilter);
  const viewStoreApi = useViewStoreApi();
  const act = viewStoreApi.getState();
  const advancedMode = advancedFilter !== null;
  const [panelOpen, setPanelOpen] = useState(false);
  // Leaving advanced mode from inside the panel unmounts it with no close
  // event; the next entry must start closed until asked.
  useEffect(() => {
    if (!advancedMode) setPanelOpen(false);
  }, [advancedMode]);
  const [search, setSearch] = useState("");
  const query = search.trim().toLowerCase();
  const fieldLabels = useFilterFieldLabels();
  const valueCatalog = useFilterValueCatalog(query.length > 0);
  const searchFields = NEGATABLE_FILTER_DIMENSIONS.filter(
    (field) => (showRelation || field !== "relation") && !hiddenFields?.includes(field),
  );
  const wsId = useWorkspaceId();
  const statusOptions = useStatusOptions(wsId);
  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(wsId));
  const filterableProperties = useMemo(
    () =>
      workspaceProperties.filter((p) => isFilterablePropertyType(p.type)),
    [workspaceProperties],
  );
  const counts = useIssueCounts(
    facetCountsExact ? scopedIssues : NO_COUNT_ISSUES,
    tableFacetCounts,
  );
  const showDateFilter = !!onDateFilterChange;
  const effectivePropertyFilters = useMemo(() => {
    const activeIds = new Set(filterableProperties.map((p) => p.id));
    return Object.fromEntries(
      Object.entries(propertyFilters).filter(([id, selected]) => selected.length > 0 && activeIds.has(id)),
    );
  }, [filterableProperties, propertyFilters]);
  const hasActiveFilters =
    getActiveFilterCount(
      {
        statusFilters,
        priorityFilters,
        propertyFilters: effectivePropertyFilters,
        assigneeFilters,
        includeNoAssignee,
        creatorFilters,
        projectFilters,
        includeNoProject,
        projectStatusFilters,
        labelFilters,
        relationFilters: showRelation ? relationFilters : [],
        dueDateFilters,
        negatedFilters,
        advancedFilter,
        dateFilter: showDateFilter ? dateFilter : null,
        agentRunningFilter,
      },
      viewBaseline,
    ) > 0;
  const fixedTitle = viewBaseline ? t(($) => $.filters.in_view) : undefined;
  const { chips: allChips } = useFilterChips(showDateFilter ? dateFilter : null, onDateFilterChange, viewBaseline);
  const chips = showRelation ? allChips : allChips.filter((chip) => chip.key !== "relation");
  const chipByKey = new Map(chips.map((chip) => [chip.key, chip]));
  const triggerValue = (key: string) => (
    <FilterTriggerValue chip={chipByKey.get(key)} modified={modifiedKeys?.has(key)} />
  );
  const [activeAtOpen, setActiveAtOpen] = useState<ReadonlySet<string> | null>(null);
  const activeKeys = activeAtOpen ?? new Set(chipByKey.keys());
  const dateFilterLabel = showDateFilter && dateFilter
    ? `${t(($) => $.filters[DATE_FIELD_LABEL_KEY[dateFilter.field]])}: ${
        dateFilter.from === dateFilter.to
          ? shortDateLabel(dateFilter.from)
          : `${shortDateLabel(dateFilter.from)} - ${shortDateLabel(dateFilter.to)}`
      }`
    : null;

  // One list of dimensions rendered in two passes: the ones already
  // filtering on first, then the rest. Membership is taken when the menu
  // opens, so a row never jumps out from under the pointer mid-edit.
  const inPass = (key: string, pass: "active" | "rest") =>
    activeKeys.has(key) === (pass === "active");
  const sectionLabel = (key: string) =>
    key === "date"
      ? t(($) => $.filters.section_date)
      : key === "agentRunning"
        ? `${t(($) => $.filters.section_agent_status)} ${t(($) => $.filters.agent_status_running)}`
        : key.startsWith("property:")
          ? filterableProperties.find((p) => `property:${p.id}` === key)?.name ?? ""
          : fieldLabels[key as NegatableFilterDimension] ?? "";
  const renderFieldSections = (pass: "active" | "rest" | "search") => {
    const show = (key: string) =>
      pass === "search" ? matchesFilterQuery(sectionLabel(key), query) : inPass(key, pass);
    return (
          <>
            {/* Status */}
            {show("status") && (
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "status" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <CircleDot className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_status)}</span>
                {triggerValue("status")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-48">
                <FilterOperatorSwitch dimension="status" />
                {/* Options come from the workspace catalog, so a custom status
                    is filterable — otherwise an issue moved onto one could not
                    be narrowed to. One flat list in category order: the icon
                    already carries the category, and a heading per category
                    doubled the menu's height for no added information
                    (MUL-6243, MUL-6399). */}
                {statusOptions.map((option) => {
                  const checked = statusFilters.includes(option.key);
                  const count = counts.status.get(option.key) ?? 0;
                  const fixed = viewBaseline?.status.has(option.key) === true;
                  return (
                    <DropdownMenuCheckboxItem
                      key={option.key}
                      checked={checked}
                      disabled={fixed}
                      title={fixed ? fixedTitle : undefined}
                      onCheckedChange={() => act.toggleStatusFilter(option.key)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={checked} />
                      <StatusIcon
                        status={option.key}
                        category={option.category}
                        color={option.color}
                        icon={option.icon}
                        className="h-3.5 w-3.5"
                      />
                      {option.label}
                      {count > 0 && (
                        <span className="ml-auto text-caption text-muted-foreground">
                          {t(($) => $.filters.issue_count, { count })}
                        </span>
                      )}
                    </DropdownMenuCheckboxItem>
                  );
                })}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            )}

            {/* Agent status */}
            {show("agentRunning") && (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <Bot className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_agent_status)}</span>
                {triggerValue("agentRunning")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-44">
                <DropdownMenuCheckboxItem
                  checked={agentRunningFilter}
                  disabled={viewBaseline?.agentRunning === true}
                  title={viewBaseline?.agentRunning === true ? fixedTitle : undefined}
                  onCheckedChange={() => act.toggleAgentRunningFilter()}
                  className={FILTER_ITEM_CLASS}
                >
                  <HoverCheck checked={agentRunningFilter} />
                  <Activity className="size-3.5 text-muted-foreground" />
                  {t(($) => $.filters.agent_status_running)}
                </DropdownMenuCheckboxItem>
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            )}

            {/* Priority */}
            {show("priority") && (
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "priority" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <SignalHigh className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_priority)}</span>
                {triggerValue("priority")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-44">
                <FilterOperatorSwitch dimension="priority" />
                {PRIORITY_DISPLAY_ORDER.map((p) => {
                  const checked = priorityFilters.includes(p);
                  const count = counts.priority.get(p) ?? 0;
                  const fixed = viewBaseline?.priority.has(p) === true;
                  return (
                    <DropdownMenuCheckboxItem
                      key={p}
                      checked={checked}
                      disabled={fixed}
                      title={fixed ? fixedTitle : undefined}
                      onCheckedChange={() => act.togglePriorityFilter(p)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={checked} />
                      <PriorityIcon priority={p} />
                      {t(($) => $.priority[p])}
                      {count > 0 && (
                        <span className="ml-auto text-caption text-muted-foreground">
                          {t(($) => $.filters.issue_count, { count })}
                        </span>
                      )}
                    </DropdownMenuCheckboxItem>
                  );
                })}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            )}

            {showDateFilter && onDateFilterChange && show("date") && (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <CalendarDays className="size-3.5" />
                  <span className="flex-1">{t(($) => $.filters.section_date)}</span>
                  {dateFilterLabel && (
                    <span className="max-w-36 truncate text-caption text-primary font-medium">
                      {dateFilterLabel}
                    </span>
                  )}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-56">
                  <DateSubContent
                    value={dateFilter}
                    onChange={onDateFilterChange}
                  />
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            )}

            {/* Assignee */}
            {show("assignee") && (
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "assignee" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <User className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_assignee)}</span>
                {triggerValue("assignee")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52 p-0">
                <FilterOperatorSwitch dimension="assignee" />
                <ActorSubContent
                  counts={counts.assignee}
                  selected={assigneeFilters}
                  onToggle={act.toggleAssigneeFilter}
                  showNoAssignee
                  includeNoAssignee={includeNoAssignee}
                  onToggleNoAssignee={act.toggleNoAssignee}
                  noAssigneeCount={counts.noAssignee}
                  fixedKeys={viewBaseline?.assignee}
                  noAssigneeFixed={viewBaseline?.includeNoAssignee === true}
                  fixedTitle={fixedTitle}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            )}

            {/* Creator */}
            {show("creator") && (
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "creator" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <UserPen className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_creator)}</span>
                {triggerValue("creator")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52 p-0">
                <FilterOperatorSwitch dimension="creator" />
                <ActorSubContent
                  counts={counts.creator}
                  selected={creatorFilters}
                  onToggle={act.toggleCreatorFilter}
                  showSquads={false}
                  fixedKeys={viewBaseline?.creator}
                  fixedTitle={fixedTitle}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            )}

            {/* Relation to me — OR across assigned / created / my agents,
                which no single actor dimension can express. */}
            {showRelation && show("relation") && (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <CircleUser className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_relation)}</span>
                {triggerValue("relation")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52">
                <FilterOperatorSwitch dimension="relation" />
                {ISSUE_RELATION_FILTERS.map((relation) => {
                  const checked = relationFilters.includes(relation);
                  const fixed = viewBaseline?.relation.has(relation) === true;
                  const Icon = RELATION_ICONS[relation];
                  return (
                    <DropdownMenuCheckboxItem
                      key={relation}
                      checked={checked}
                      disabled={fixed}
                      title={fixed ? fixedTitle : undefined}
                      onCheckedChange={() => act.toggleRelationFilter(relation)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={checked} />
                      <Icon className="size-3.5 text-muted-foreground" />
                      {t(($) => $.filters[RELATION_LABEL_KEY[relation]])}
                    </DropdownMenuCheckboxItem>
                  );
                })}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            )}

            {/* Project */}
            {show("project") && (
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "project" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <FolderKanban className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_project)}</span>
                {triggerValue("project")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52 p-0">
                <FilterOperatorSwitch dimension="project" />
                <ProjectSubContent
                  counts={counts.project}
                  selected={projectFilters}
                  onToggle={act.toggleProjectFilter}
                  includeNoProject={includeNoProject}
                  onToggleNoProject={act.toggleNoProject}
                  noProjectCount={counts.noProject}
                  fixedIds={viewBaseline?.project}
                  noProjectFixed={viewBaseline?.includeNoProject === true}
                  fixedTitle={fixedTitle}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            )}

            {/* Project status — a dimension of its own next to Project:
                "everything in the projects that are in progress", without
                naming them one by one. */}
            {show("projectStatus") && (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <CircleDashed className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_project_status)}</span>
                {triggerValue("projectStatus")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52 p-0">
                <FilterOperatorSwitch dimension="projectStatus" />
                <ProjectStatusSubContent
                  selected={projectStatusFilters}
                  onToggle={act.toggleProjectStatusFilter}
                  fixedStatuses={viewBaseline?.projectStatus}
                  fixedTitle={fixedTitle}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            )}

            {/* Label */}
            {show("label") && (
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "label" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <Tag className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_label)}</span>
                {triggerValue("label")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52 p-0">
                <FilterOperatorSwitch dimension="label" />
                <LabelSubContent
                  counts={counts.label}
                  selected={labelFilters}
                  onToggle={act.toggleLabelFilter}
                  fixedIds={viewBaseline?.label}
                  fixedTitle={fixedTitle}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            )}

            {/* Due date — relative buckets, so a saved view keeps meaning
                "overdue" tomorrow. */}
            {show("dueDate") && (
            <DropdownMenuSub>
              <DropdownMenuSubTrigger>
                <CalendarClock className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_due_date)}</span>
                {triggerValue("dueDate")}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52">
                <FilterOperatorSwitch dimension="dueDate" />
                {DUE_DATE_FILTERS.map((bucket) => {
                  const checked = dueDateFilters.includes(bucket);
                  const fixed = viewBaseline?.dueDate.has(bucket) === true;
                  return (
                    <DropdownMenuCheckboxItem
                      key={bucket}
                      checked={checked}
                      disabled={fixed}
                      title={fixed ? fixedTitle : undefined}
                      onCheckedChange={() => act.toggleDueDateFilter(bucket)}
                      className={FILTER_ITEM_CLASS}
                    >
                      <HoverCheck checked={checked} />
                      <CalendarClock className="size-3.5 text-muted-foreground" />
                      {t(($) => $.filters[DUE_DATE_LABEL_KEY[bucket]])}
                    </DropdownMenuCheckboxItem>
                  );
                })}
              </DropdownMenuSubContent>
            </DropdownMenuSub>
            )}

            {/* Custom properties — one sub per filterable definition */}
            {pass === "rest" && filterableProperties.some((p) => show(`property:${p.id}`)) && <DropdownMenuSeparator />}
            {filterableProperties.filter((property) => show(`property:${property.id}`)).map((property) => {
              const selected = propertyFilters[property.id] ?? [];
              return (
                <DropdownMenuSub
                  key={property.id}
                  onOpenChange={(open) =>
                    onTableFacetChange?.(
                      open
                        ? { kind: "property", property_id: property.id }
                        : null,
                    )
                  }
                >
                  <DropdownMenuSubTrigger>
                    {property.icon ? (
                      <PropertyIcon property={property} className="size-3.5 text-caption" />
                    ) : (
                      <SlidersHorizontal className="size-3.5" />
                    )}
                    <span className="flex-1 truncate">{property.name}</span>
                    {triggerValue(`property:${property.id}`)}
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent className="w-auto min-w-44 p-1">
                    <PropertyFilterOptions
                      property={property}
                      counts={counts.property.get(property.id)}
                      selected={selected}
                      onToggle={(optionId) => act.togglePropertyFilter(property.id, optionId)}
                      onSetValues={(optionIds) => act.setPropertyFilterValues(property.id, optionIds)}
                      fixedIds={viewBaseline?.property.get(property.id)}
                      fixedTitle={fixedTitle}
                    />
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              );
            })}

          </>
    );
  };

  // Base UI's render-prop merges its own ref with ours, so cloning the
  // caller's element only adds the ref we need for rect capture.
  const anchoredTrigger = freezeAnchor
    ? cloneElement(trigger, { ref: triggerRef } as Partial<React.HTMLAttributes<HTMLElement>>)
    : trigger;

  // Search: matching fields keep their submenus; matching values become
  // one-click rows that read as the condition they add.
  const simpleFields = {
    statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters,
    projectFilters, includeNoProject, projectStatusFilters, labelFilters, relationFilters,
    dueDateFilters,
  };
  const negatedSet = new Set(negatedFilters);
  const fixedValue = (field: NegatableFilterDimension, value: string) => {
    if (!viewBaseline) return false;
    if (value === "none" && field === "assignee") return viewBaseline.includeNoAssignee;
    if (value === "none" && field === "project") return viewBaseline.includeNoProject;
    return viewBaseline[field].has(value);
  };
  const sectionKeys = [
    ...searchFields.filter((field) => field !== "relation" || showRelation),
    "agentRunning",
    ...(showDateFilter ? ["date"] : []),
    ...filterableProperties.map((p) => `property:${p.id}`),
  ];
  const fieldHits = query ? sectionKeys.filter((key) => matchesFilterQuery(sectionLabel(key), query)) : [];
  const valueHits = query
    ? searchFields
        .flatMap((field) =>
          valueCatalog(field)
            .filter((option) => matchesFilterQuery(option.label, query))
            .map((option) => ({ field, option })),
        )
        .slice(0, 50)
    : [];

  const menuBody = query ? (
    <>
      {fieldHits.length > 0 && (
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t(($) => $.filters.search_fields)}</DropdownMenuLabel>
          {renderFieldSections("search")}
        </DropdownMenuGroup>
      )}
      {fieldHits.length > 0 && valueHits.length > 0 && <DropdownMenuSeparator />}
      {valueHits.length > 0 && (
        <DropdownMenuGroup>
          <DropdownMenuLabel>{t(($) => $.filters.search_values)}</DropdownMenuLabel>
          {valueHits.map(({ field, option }) => {
            const checked = simpleFieldValues(simpleFields, field).includes(option.value);
            const fixed = fixedValue(field, option.value);
            const FieldIcon = FILTER_FIELD_ICONS[field];
            return (
              <DropdownMenuCheckboxItem
                key={`${field}:${option.value}`}
                checked={checked}
                disabled={fixed}
                title={fixed ? fixedTitle : undefined}
                onCheckedChange={() => toggleSimpleFilterValue(act, field, option.value)}
                className={FILTER_ITEM_CLASS}
              >
                <HoverCheck checked={checked} />
                <FieldIcon className="size-3.5 text-muted-foreground" />
                <span className="shrink-0 text-muted-foreground">{fieldLabels[field]}</span>
                <FilterOperator negated={negatedSet.has(field)} />
                {option.icon}
                <span className="truncate">{option.label}</span>
              </DropdownMenuCheckboxItem>
            );
          })}
        </DropdownMenuGroup>
      )}
      {fieldHits.length === 0 && valueHits.length === 0 && (
        <div className="px-2 py-3 text-center text-body text-muted-foreground">
          {t(($) => $.filters.no_results)}
        </div>
      )}
    </>
  ) : (
    <>
      {leadingSections && (
        <>
          {leadingSections}
          <DropdownMenuSeparator />
        </>
      )}
      {activeKeys.size > 0 && (
        <>
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t(($) => $.filters.active_section)}</DropdownMenuLabel>
            {renderFieldSections("active")}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
        </>
      )}
      {renderFieldSections("rest")}

      <DropdownMenuSeparator />
      <DropdownMenuItem
        onClick={() => {
          act.enterAdvancedFilter();
          setPanelOpen(true);
        }}
      >
        <ListTree className="size-3.5" />
        {t(($) => $.filters.advanced_switch)}
      </DropdownMenuItem>

      {/* Reset */}
      {hasActiveFilters && (
        <DropdownMenuItem
          onClick={() => {
            if (viewBaseline) {
              // Inside a saved view, reset returns to the view's own
              // conditions, never to nothing.
              act.resetFiltersTo(viewBaseline.raw);
            } else {
              act.clearFilters();
            }
            onDateFilterChange?.(null);
          }}
        >
          {t(($) => $.filters.reset)}
        </DropdownMenuItem>
      )}
    </>
  );

  const tooltipContent =
    summaryTitle && chips.length > 0 ? (
      <TooltipContent side="bottom" align="start" className="max-w-sm">
        <FilterSummary title={summaryTitle} chips={chips} modifiedKeys={modifiedKeys} />
      </TooltipContent>
    ) : (
      <TooltipContent side="bottom">{tooltip}</TooltipContent>
    );
  const withTooltip = !!(summaryTitle || tooltip);

  // Advanced mode: the same trigger opens the rule panel instead of the menu.
  if (advancedMode) {
    return (
      <Popover
        open={panelOpen}
        onOpenChange={(open) => {
          setPanelOpen(open);
          onOpenChange?.(open);
        }}
      >
        {withTooltip ? (
          <Tooltip disabled={(!tooltip && chips.length === 0) || panelOpen}>
            <PopoverTrigger render={<TooltipTrigger render={trigger} />} />
            {tooltipContent}
          </Tooltip>
        ) : (
          <PopoverTrigger render={trigger} />
        )}
        <PopoverContent
          align={align}
          className="w-[680px] max-w-[calc(100vw-2rem)] gap-0 p-0"
          data-testid="advanced-filter-panel"
        >
          <AdvancedFilterPanel fields={searchFields} />
        </PopoverContent>
      </Popover>
    );
  }

  return (
    <DropdownMenu
      onOpenChange={(open) => {
        if (freezeAnchor) {
          if (open && triggerRef.current) {
            const rect = triggerRef.current.getBoundingClientRect();
            setFrozenAnchor({ getBoundingClientRect: () => rect });
          } else if (!open) {
            setFrozenAnchor(null);
          }
        }
        setActiveAtOpen(open ? new Set(chipByKey.keys()) : null);
        if (!open) {
          setSearch("");
          onTableFacetChange?.(null);
        }
        onOpenChange?.(open);
      }}
    >
      {/* One tree for both bodies: swapping wrappers when the first filter
          lands would remount the trigger and strand the open menu's anchor. */}
      {withTooltip ? (
        <Tooltip disabled={(!tooltip && chips.length === 0) || (!!summaryTitle && activeAtOpen !== null)}>
          <DropdownMenuTrigger render={<TooltipTrigger render={anchoredTrigger} />} />
          {tooltipContent}
        </Tooltip>
      ) : (
        <DropdownMenuTrigger render={anchoredTrigger} />
      )}
          <DropdownMenuContent align={align} className="w-auto min-w-64 p-0" anchor={frozenAnchor ?? undefined}>
            <div data-filter-search className="flex items-center gap-1.5 border-b border-foreground/5 px-2.5 py-2">
              <Search className="size-3.5 shrink-0 text-muted-foreground" />
              <input
                type="text"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                onKeyDown={stopMenuSearchKeys}
                placeholder={t(($) => $.filters.search_placeholder)}
                aria-label={t(($) => $.filters.search_placeholder)}
                className="w-full bg-transparent text-body outline-none placeholder:text-muted-foreground"
                autoFocus
              />
            </div>
            <div className="max-h-[min(70vh,36rem)] overflow-x-hidden overflow-y-auto p-1">{menuBody}</div>
          </DropdownMenuContent>
    </DropdownMenu>
  );
}

const LAYOUTS: { mode: ViewMode; Icon: typeof List }[] = [
  { mode: "board", Icon: Columns3 },
  { mode: "list", Icon: List },
  { mode: "table", Icon: Table2 },
  { mode: "swimlane", Icon: Waves },
  { mode: "gantt", Icon: ChartGantt },
];

export function IssueDisplayControls({
  scopedIssues,
  hideViewToggle = false,
  allowGantt = false,
  dateFilter = null,
  onDateFilterChange,
  facetCountsExact = true,
  tableFacetCounts,
  onTableFacetChange,
  viewBaseline,
  hideFilter = false,
  iconOnly = false,
}: {
  scopedIssues: Issue[];
  hideViewToggle?: boolean;
  /** The host renders its own filter trigger (the view header's chip row). */
  hideFilter?: boolean;
  /** A ghost icon button named only by its tooltip, for the issue view header. */
  iconOnly?: boolean;
  dateFilter?: IssueDateFilter | null;
  onDateFilterChange?: (filter: IssueDateFilter | null) => void;
  /** Open saved view: menu marks the view's values checked-and-disabled;
   *  the trigger count only counts additions on top. */
  viewBaseline?: IssueViewBaseline;
  // Only Project Detail renders <GanttView>; other surfaces (global /issues,
  // /my-issues, actor panel) ignore viewMode === "gantt" and would silently
  // fall back to List if the option were exposed there. Keep Gantt opt-in.
  allowGantt?: boolean;
  /**
   * Whether `scopedIssues` covers the surface's full window. Table does not
   * use loaded rows for counts; server-paged List, Board, and Swimlane follow
   * the same rule. They supply `tableFacetCounts` from the backend instead,
   * so badges remain exact without downloading all issues.
   */
  facetCountsExact?: boolean;
  tableFacetCounts?: IssueTableFacetsResponse;
  onTableFacetChange?: (facet: IssueTableFacetSpec | null) => void;
}) {
  const { t } = useT("issues");
  const viewMode = useViewStore((s) => s.viewMode);
  const statusFilters = useViewStore((s) => s.statusFilters);
  const priorityFilters = useViewStore((s) => s.priorityFilters);
  const assigneeFilters = useViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useViewStore((s) => s.creatorFilters);
  const projectFilters = useViewStore((s) => s.projectFilters);
  const includeNoProject = useViewStore((s) => s.includeNoProject);
  const projectStatusFilters = useViewStore((s) => s.projectStatusFilters);
  const labelFilters = useViewStore((s) => s.labelFilters);
  const propertyFilters = useViewStore((s) => s.propertyFilters);
  const relationFilters = useViewStore((s) => s.relationFilters);
  const dueDateFilters = useViewStore((s) => s.dueDateFilters);
  const advancedFilter = useViewStore((s) => s.advancedFilter);
  const negatedFilters = useViewStore((s) => s.negatedFilters);
  const agentRunningFilter = useViewStore((s) => s.agentRunningFilter);
  const cardPropertyIds = useViewStore((s) => s.cardPropertyIds);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const grouping = useViewStore((s) => s.grouping);
  const swimlaneGrouping = useViewStore((s) => s.swimlaneGrouping);
  const cardProperties = useViewStore((s) => s.cardProperties);
  const tableGrouping = useViewStore((s) => s.tableGrouping ?? "none");
  const tableHierarchy = useViewStore((s) => s.tableHierarchy ?? true);
  const tableColumnCount = useViewStore((s) => s.tableColumns.length);
  const showSubIssues = useViewStore((s) => s.showSubIssues);
  const hiddenStatuses = useViewStore((s) => s.hiddenStatuses);
  const act = useViewStoreApi().getState();
  const headerWsId = useWorkspaceId();
  const columnStatuses = useColumnStatuses();
  const statusOptions = useStatusOptions(headerWsId);
  const columnOptions = useMemo(() => {
    if (!columnStatuses) return [];
    const optionByKey = new Map(statusOptions.map((option) => [option.key, option]));
    return columnStatuses.flatMap((key) => optionByKey.get(key) ?? []);
  }, [columnStatuses, statusOptions]);
  const showColumnToggles =
    columnOptions.length > 0 &&
    (viewMode === "list" || viewMode === "swimlane" || (viewMode === "board" && grouping === "status"));
  // Active custom-property catalog: drives the filter sections, dynamic
  // sort/grouping options, and the card-property toggles below.
  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(headerWsId));
  const propertyById = useMemo(
    () => new Map(workspaceProperties.map((p) => [p.id, p])),
    [workspaceProperties],
  );
  const filterableProperties = useMemo(
    () =>
      workspaceProperties.filter((p) => isFilterablePropertyType(p.type)),
    [workspaceProperties],
  );
  const sortableProperties = useMemo(
    () => workspaceProperties.filter((p) => p.type === "number" || p.type === "date"),
    [workspaceProperties],
  );
  const groupableProperties = useMemo(
    () => workspaceProperties.filter((p) => p.type === "select"),
    [workspaceProperties],
  );
  const tableGroupableProperties = useMemo(
    () =>
      workspaceProperties.filter((p) =>
        ["select", "checkbox"].includes(p.type),
      ),
    [workspaceProperties],
  );

  const showDateFilter = !!onDateFilterChange;

  // Only count filters whose definition is still active — a filter pinned to
  // an archived definition is stripped by the surface controller and must
  // not light the badge for an effect that no longer exists.
  const effectivePropertyFilters = useMemo(() => {
    const activeIds = new Set(filterableProperties.map((p) => p.id));
    return Object.fromEntries(
      Object.entries(propertyFilters).filter(([id, selected]) => selected.length > 0 && activeIds.has(id)),
    );
  }, [filterableProperties, propertyFilters]);

  const activeFilterCount = getActiveFilterCount(
    {
      statusFilters,
      priorityFilters,
      propertyFilters: effectivePropertyFilters,
      assigneeFilters,
      includeNoAssignee,
      creatorFilters,
      projectFilters,
      includeNoProject,
      projectStatusFilters,
      labelFilters,
      relationFilters,
      dueDateFilters,
      negatedFilters,
      advancedFilter,
      dateFilter: showDateFilter ? dateFilter : null,
      agentRunningFilter,
    },
    viewBaseline,
  );
  const hasActiveFilters = activeFilterCount > 0;

  const SORT_LABEL_KEY: Record<typeof SORT_OPTIONS[number]["value"], "sort_manual" | "sort_status" | "sort_priority" | "sort_start_date" | "sort_due_date" | "sort_created" | "sort_updated" | "sort_title"> = {
    position: "sort_manual",
    status: "sort_status",
    priority: "sort_priority",
    start_date: "sort_start_date",
    due_date: "sort_due_date",
    created_at: "sort_created",
    updated_at: "sort_updated",
    title: "sort_title",
  };
  const GROUPING_LABEL_KEY: Record<typeof GROUPING_OPTIONS[number]["value"], "group_status" | "group_assignee" | "group_project"> = {
    status: "group_status",
    assignee: "group_assignee",
    project: "group_project",
  };
  const SWIMLANE_GROUPING_LABEL_KEY: Record<SwimlaneGrouping, "group_parent" | "group_project" | "group_assignee"> = {
    parent: "group_parent",
    project: "group_project",
    assignee: "group_assignee",
  };
  const CARD_PROPERTY_LABEL_KEY: Record<typeof CARD_PROPERTY_OPTIONS[number]["key"], "card_priority" | "card_description" | "card_assignee" | "card_start_date" | "card_due_date" | "card_project" | "card_labels" | "card_child_progress"> = {
    priority: "card_priority",
    description: "card_description",
    assignee: "card_assignee",
    startDate: "card_start_date",
    dueDate: "card_due_date",
    project: "card_project",
    labels: "card_labels",
    childProgress: "card_child_progress",
  };
  const availableCardPropertyOptions = cardPropertyOptionsForView(viewMode);
  const availableSortOptions = sortOptionsForView(viewMode, grouping);
  const sortPropertyId = propertyIdFromViewKey(sortBy);
  const groupingPropertyId = propertyIdFromViewKey(grouping);
  const tableGroupingPropertyId = propertyIdFromViewKey(tableGrouping);
  // Property-backed sort/grouping label straight from the definition name;
  // a stale persisted id (archived/deleted definition) falls back to the
  // manual/status default label.
  const sortLabel = sortPropertyId
    ? propertyById.get(sortPropertyId)?.name ?? t(($) => $.display.sort_manual)
    : t(($) => $.display[SORT_LABEL_KEY[sortBy as keyof typeof SORT_LABEL_KEY]]);
  const sortDirectionLabel = t(
    ($) => $.display[sortDirectionLabelKey(sortBy, sortDirection)],
  );
  const groupingLabel = groupingPropertyId
    ? propertyById.get(groupingPropertyId)?.name ?? t(($) => $.display.group_status)
    : t(($) => $.display[GROUPING_LABEL_KEY[grouping as keyof typeof GROUPING_LABEL_KEY]]);
  const swimlaneGroupingLabel = t(($) => $.display[SWIMLANE_GROUPING_LABEL_KEY[swimlaneGrouping]]);
  const effectiveTableGrouping =
    tableGroupingPropertyId && !propertyById.has(tableGroupingPropertyId)
      ? "none"
      : tableGrouping;
  const tableGroupingLabel = tableGroupingPropertyId
    ? propertyById.get(tableGroupingPropertyId)?.name ??
      t(($) => $.table.group_none)
    : effectiveTableGrouping === "status"
      ? t(($) => $.table.columns.status)
      : effectiveTableGrouping === "assignee"
        ? t(($) => $.table.columns.assignee)
        : effectiveTableGrouping === "project"
          ? t(($) => $.table.columns.project)
          : t(($) => $.table.group_none);
  // A Gantt layout persisted on a surface without Gantt renders as List.
  const layout: ViewMode = viewMode === "gantt" && !allowGantt ? "list" : viewMode;
  const layouts = LAYOUTS.filter(({ mode }) => mode !== "gantt" || allowGantt);
  const LayoutIcon = hideViewToggle
    ? SlidersHorizontal
    : LAYOUTS.find(({ mode }) => mode === layout)!.Icon;
  const hasLayoutOptions =
    showColumnToggles || viewMode === "table" || availableCardPropertyOptions.length > 0;
  const controlButtonClass = "h-8 w-8 gap-1 px-0 text-muted-foreground md:h-7 md:w-auto md:px-2.5";
  const controlLabelClass = "hidden md:inline @max-3xl/issue-header:hidden";

  return (
    <div className="flex shrink-0 items-center gap-1">
        {!hideFilter && <IssueFilterMenu
          trigger={
            <Button
              variant={hasActiveFilters ? "default" : "outline"}
              size="sm"
              className={
                hasActiveFilters
                  ? "h-8 w-8 gap-1 bg-brand px-0 text-white hover:bg-brand/90 md:h-7 md:w-auto md:px-2.5"
                  : controlButtonClass
              }
            >
              <Filter className="size-3.5" />
              <span className="hidden md:inline">
                {hasActiveFilters
                  ? t(($) => $.filters.active_count, { count: activeFilterCount })
                  : t(($) => $.filters.tooltip)}
              </span>
              {hasActiveFilters && (
                <span className="tabular-nums md:hidden">{activeFilterCount}</span>
              )}
            </Button>
          }
          tooltip={t(($) => $.filters.tooltip)}
          scopedIssues={scopedIssues}
          facetCountsExact={facetCountsExact}
          tableFacetCounts={tableFacetCounts}
          onTableFacetChange={onTableFacetChange}
          dateFilter={showDateFilter ? dateFilter : null}
          onDateFilterChange={onDateFilterChange}
          viewBaseline={viewBaseline}
        />}

        <Popover>
          <Tooltip>
            <PopoverTrigger
              render={
                <TooltipTrigger
                  render={
                    iconOnly ? (
                      <Button
                        variant="ghost"
                        size="icon-sm"
                        className="text-muted-foreground"
                        aria-label={t(($) => $.display.button)}
                      >
                        <LayoutIcon />
                      </Button>
                    ) : (
                      <Button
                        variant="outline"
                        size="sm"
                        className={controlButtonClass}
                        aria-label={t(($) => $.display.button)}
                      >
                        <LayoutIcon className="size-3.5" />
                        <span className={controlLabelClass}>{t(($) => $.display.button)}</span>
                        <ChevronDown className={cn("size-3 text-muted-foreground", controlLabelClass)} />
                      </Button>
                    )
                  }
                />
              }
            />
            <TooltipContent side="bottom">{t(($) => $.display.tooltip)}</TooltipContent>
          </Tooltip>
          <PopoverContent align="end" className="w-88 p-3">
            {/* Spacing separates sections — no dividers (see UI rules). */}
            {!hideViewToggle && (
              <div
                role="group"
                aria-label={t(($) => $.view.section)}
                className={cn(
                  "mb-3 grid gap-0.5 rounded-lg bg-muted p-0.5",
                  layouts.length === 5 ? "grid-cols-5" : "grid-cols-4",
                )}
              >
                {layouts.map(({ mode, Icon }) => (
                  <Toggle
                    key={mode}
                    pressed={layout === mode}
                    onPressedChange={() => act.setViewMode(mode)}
                    title={t(($) => $.view[mode])}
                    className="h-auto min-w-0 flex-col gap-1 rounded-md px-0.5 py-1.5 text-caption font-normal text-muted-foreground hover:bg-transparent hover:text-foreground aria-pressed:bg-background aria-pressed:text-foreground aria-pressed:shadow-sm aria-pressed:hover:bg-background"
                  >
                    <Icon className="size-4" />
                    <span className="max-w-full truncate">{t(($) => $.view[mode])}</span>
                  </Toggle>
                ))}
              </div>
            )}
            <div className="space-y-3">
              {viewMode === "board" && (
                <div className="flex items-center justify-between gap-3">
                  <span className="text-caption font-medium text-muted-foreground">
                    {t(($) => $.display.grouping_section)}
                  </span>
                  <Select
                    items={[
                      ...GROUPING_OPTIONS.map((opt) => ({
                        value: opt.value as string,
                        label: t(($) => $.display[GROUPING_LABEL_KEY[opt.value]]),
                      })),
                      ...groupableProperties.map((p) => ({
                        value: `property:${p.id}`,
                        label: p.name,
                      })),
                    ]}
                    value={grouping}
                    onValueChange={(v) => {
                      if (v) act.setGrouping(v as IssueGrouping);
                    }}
                  >
                    <SelectTrigger size="sm" className="w-36" aria-label={t(($) => $.display.grouping_section)}>
                      <SelectValue>{groupingLabel}</SelectValue>
                    </SelectTrigger>
                    <SelectContent align="end">
                      <SelectGroup>
                      {GROUPING_OPTIONS.map((opt) => (
                        <SelectItem key={opt.value} value={opt.value}>
                          {t(($) => $.display[GROUPING_LABEL_KEY[opt.value]])}
                        </SelectItem>
                      ))}
                      {groupableProperties.map((property) => (
                        <SelectItem key={property.id} value={`property:${property.id}`}>
                          {property.name}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
              )}
              {viewMode === "swimlane" && (
                <div className="flex items-center justify-between gap-3">
                  <span className="text-caption font-medium text-muted-foreground">
                    {t(($) => $.display.grouping_section)}
                  </span>
                  <Select
                    items={SWIMLANE_GROUPINGS.map((value) => ({
                      value: value as string,
                      label: t(($) => $.display[SWIMLANE_GROUPING_LABEL_KEY[value]]),
                    }))}
                    value={swimlaneGrouping}
                    onValueChange={(v) => {
                      if (v) act.setSwimlaneGrouping(v as SwimlaneGrouping);
                    }}
                  >
                    <SelectTrigger size="sm" className="w-36" aria-label={t(($) => $.display.grouping_section)}>
                      <SelectValue>{swimlaneGroupingLabel}</SelectValue>
                    </SelectTrigger>
                    <SelectContent align="end">
                      <SelectGroup>
                      {SWIMLANE_GROUPINGS.map((value) => (
                        <SelectItem key={value} value={value}>
                          {t(($) => $.display[SWIMLANE_GROUPING_LABEL_KEY[value]])}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
              )}
              {viewMode === "table" && (
                <div className="flex items-center justify-between gap-3">
                  <span className="text-caption font-medium text-muted-foreground">
                    {t(($) => $.display.grouping_section)}
                  </span>
                  <Select
                    items={[
                      { value: "none", label: t(($) => $.table.group_none) },
                      { value: "status", label: t(($) => $.table.columns.status) },
                      { value: "assignee", label: t(($) => $.table.columns.assignee) },
                      { value: "project", label: t(($) => $.table.columns.project) },
                      ...tableGroupableProperties.map((p) => ({
                        value: `property:${p.id}`,
                        label: p.name,
                      })),
                    ]}
                    value={effectiveTableGrouping}
                    onValueChange={(v) => {
                      if (v) act.setTableGrouping(v as TableGrouping);
                    }}
                  >
                    <SelectTrigger size="sm" className="w-36" aria-label={t(($) => $.display.grouping_section)}>
                      <SelectValue>{tableGroupingLabel}</SelectValue>
                    </SelectTrigger>
                    <SelectContent align="end">
                      <SelectGroup>
                        <SelectItem value="none">{t(($) => $.table.group_none)}</SelectItem>
                        <SelectItem value="status">{t(($) => $.table.columns.status)}</SelectItem>
                        <SelectItem value="assignee">{t(($) => $.table.columns.assignee)}</SelectItem>
                        <SelectItem value="project">{t(($) => $.table.columns.project)}</SelectItem>
                        {tableGroupableProperties.map((property) => (
                          <SelectItem key={property.id} value={`property:${property.id}`}>
                            <PropertyIcon property={property} className="size-3.5 text-caption" />
                            <span>{property.name}</span>
                          </SelectItem>
                        ))}
                      </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
              )}
              <div className="flex items-center justify-between gap-3">
                <span className="text-caption font-medium text-muted-foreground">
                  {t(($) => $.display.ordering_section)}
                </span>
                <div className="flex min-w-0 items-center gap-1">
                  {sortBy !== "position" && (
                    <SortDirectionButton
                      label={sortDirectionLabel}
                      onClick={() => act.setSortDirection(sortDirection === "asc" ? "desc" : "asc")}
                    />
                  )}
                  <Select
                    items={[
                      ...availableSortOptions.map((opt) => ({
                        value: opt.value as string,
                        label: t(($) => $.display[SORT_LABEL_KEY[opt.value as keyof typeof SORT_LABEL_KEY]]),
                      })),
                      ...sortableProperties.map((p) => ({
                        value: `property:${p.id}`,
                        label: p.name,
                      })),
                    ]}
                    value={sortBy}
                    onValueChange={(v) => {
                      if (v) act.setSortBy(v as SortField);
                    }}
                  >
                    <SelectTrigger size="sm" className="w-36" aria-label={t(($) => $.display.ordering_section)}>
                      <SelectValue>{sortLabel}</SelectValue>
                    </SelectTrigger>
                    <SelectContent align="end">
                      <SelectGroup>
                      {availableSortOptions.map((opt) => (
                        <SelectItem key={opt.value} value={opt.value}>
                          {t(($) => $.display[SORT_LABEL_KEY[opt.value as keyof typeof SORT_LABEL_KEY]])}
                        </SelectItem>
                      ))}
                      {sortableProperties.map((property) => (
                        <SelectItem key={property.id} value={`property:${property.id}`}>
                          {property.name}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <label className="flex cursor-pointer items-center justify-between gap-3">
                <span className="text-caption font-medium text-muted-foreground">
                  {t(($) => $.display.show_sub_issues)}
                </span>
                <Switch
                  size="sm"
                  checked={showSubIssues}
                  onCheckedChange={() => act.toggleShowSubIssues()}
                />
              </label>
            </div>
            {hasLayoutOptions && (
              <div className="mt-5 space-y-3">
                <p className="text-label font-medium text-foreground">
                  {t(($) => $.display.layout_options, { layout: t(($) => $.view[layout]) })}
                </p>
              {showColumnToggles && (
                <div>
                  <span className="text-caption font-medium text-muted-foreground">
                    {t(($) => $.display.columns_section)}
                  </span>
                  {/* Same chip grammar as Card properties: pressed = column shown.
                      A status filter picks the columns itself and overrides
                      these, so the chips lock rather than flip to no effect. */}
                  <div className="mt-2 flex flex-wrap gap-1">
                    {columnOptions.map((option) => (
                      <DisplayChip
                        key={option.key}
                        pressed={!hiddenStatuses.includes(option.key)}
                        disabled={statusFilters.length > 0}
                        onPressedChange={(pressed) =>
                          pressed ? act.showStatus(option.key) : act.hideStatus(option.key)
                        }
                      >
                        <StatusIcon
                          status={option.key}
                          category={option.category}
                          color={option.color}
                          icon={option.icon}
                          className="size-3 shrink-0"
                        />
                        <span className="truncate">{option.label}</span>
                      </DisplayChip>
                    ))}
                  </div>
                  {statusFilters.length > 0 && (
                    <p className="mt-1.5 text-caption text-faint-foreground">
                      {t(($) => $.display.columns_locked_by_filter)}
                    </p>
                  )}
                </div>
              )}
              {viewMode === "table" && (
                <label className="flex cursor-pointer items-center justify-between gap-3">
                  <span className="min-w-0">
                    <span className="block text-caption font-medium text-muted-foreground">
                      {t(($) => $.table.hierarchy)}
                    </span>
                    <span className="block text-caption text-faint-foreground">
                      {t(($) => $.table.hierarchy_description)}
                    </span>
                  </span>
                  <Switch
                    size="sm"
                    checked={tableHierarchy}
                    onCheckedChange={() => act.toggleTableHierarchy()}
                  />
                </label>
              )}
              {viewMode === "table" && (
                <div className="flex items-center justify-between gap-3">
                  <span className="text-caption font-medium text-muted-foreground">
                    {t(($) => $.table.columns.section)}
                  </span>
                  <TableColumnPicker
                    properties={workspaceProperties}
                    trigger={
                      <Button
                        variant="outline"
                        size="sm"
                        className="w-36 justify-between border-input pr-2 pl-2.5 text-body font-normal"
                      >
                        {t(($) => $.display.columns_shown, { count: tableColumnCount })}
                        <ChevronDown className="size-4 text-muted-foreground" />
                      </Button>
                    }
                  />
                </div>
              )}
              {availableCardPropertyOptions.length > 0 && (
                <div>
                  <span className="text-caption font-medium text-muted-foreground">
                    {t(($) => $.display.card_properties_section)}
                  </span>
                  {/* Chip toggles (pressed = shown on cards). Unpressed chips
                      dim so the active set reads at a glance. */}
                  <div className="mt-2 flex flex-wrap gap-1">
                    {availableCardPropertyOptions.map((opt) => (
                      <DisplayChip
                        key={opt.key}
                        pressed={cardProperties[opt.key]}
                        onPressedChange={() => act.toggleCardProperty(opt.key)}
                      >
                        {t(($) => $.display[CARD_PROPERTY_LABEL_KEY[opt.key]])}
                      </DisplayChip>
                    ))}
                    {workspaceProperties.map((property) => (
                      <DisplayChip
                        key={property.id}
                        pressed={cardPropertyIds.includes(property.id)}
                        onPressedChange={() => act.toggleCardPropertyId(property.id)}
                      >
                        <span className="truncate">{property.name}</span>
                      </DisplayChip>
                    ))}
                  </div>
                </div>
              )}
              </div>
            )}
          </PopoverContent>
        </Popover>

    </div>
  );
}
