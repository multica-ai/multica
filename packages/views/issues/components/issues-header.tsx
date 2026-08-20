"use client";

import { cloneElement, useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowDown,
  ArrowUp,
  CalendarDays,
  ChartGantt,
  ChevronDown,
  CircleDot,
  Columns3,
  Filter,
  FolderKanban,
  FolderMinus,
  List,
  Rows3,
  SignalHigh,
  SlidersHorizontal,
  Tag,
  Table2,
  User,
  UserMinus,
  UserPen,
  Waves,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
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
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { labelListOptions } from "@multica/core/labels/queries";
import { propertyListOptions } from "@multica/core/properties";
import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";
import type {
  Issue,
  IssueProperty,
  IssueTableFacetSpec,
  IssueTableFacetsResponse,
  WorkingAgentSummary,
} from "@multica/core/types";
import { formatActorRef, isActorPropertyType } from "@multica/core/types";
import { ProjectIcon } from "../../projects/components/project-icon";
import { ActorAvatar } from "../../common/actor-avatar";
import { PropertyIcon } from "../../common/property-icon";
import { LabelChip } from "../../labels/label-chip";
import {
  SORT_OPTIONS,
  GROUPING_OPTIONS,
  SWIMLANE_GROUPINGS,
  CARD_PROPERTY_OPTIONS,
  type ActorFilterValue,
  type IssueDateField,
  type IssueDateFilter,
  type SortField,
  type IssueGrouping,
  type SwimlaneGrouping,
  type TableGrouping,
  type ViewMode,
} from "@multica/core/issues/stores/view-store";
import { useViewStore, useViewStoreApi } from "@multica/core/issues/stores/view-store-context";
import { FilterChipsBar } from "./filter-chips-bar";
import { SaveViewDialog, type SaveViewScope } from "./save-view-dialog";
import { ViewBar } from "./view-bar";
import { toast } from "sonner";
import { useActiveIssueView } from "@multica/core/issue-views/use-active-view";
import { useAuthStore } from "@multica/core/auth";
import type { IssueViewScope } from "@multica/core/issue-views/queries";
import { actorFilterKey, baselineFromQuery, type IssueViewBaseline } from "@multica/core/issue-views/baseline";
import type { IssueView } from "@multica/core/api/schemas";
import { addDaysDateOnly, dateOnlyToLocalDate, formatDateOnly, toDateOnly, todayDateOnly } from "@multica/core/issues/date";
import {
  useIssuesScope,
  useIssuesScopeStore,
  type IssuesScope,
  type IssuesScopePageKey,
} from "@multica/core/issues/stores/issues-scope-store";
import { actorKindForViewVariant } from "@multica/core/issues/surface/scope";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import { cn } from "@multica/ui/lib/utils";
import { PAGE_GUTTER } from "../../layout/page-header";
import { useT } from "../../i18n";
import { useStatusOptions } from "../utils/status-options";
import { NO_PROPERTY_VALUE } from "../utils/filter";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { WorkspaceAgentWorkingChip } from "./workspace-agent-working-chip";
import { TableColumnPicker } from "./table-view";

type LocalDateRange = {
  from: Date | undefined;
  to?: Date;
};

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function getActiveFilterCount(
  state: {
    statusFilters: string[];
    priorityFilters: string[];
    assigneeFilters: ActorFilterValue[];
    includeNoAssignee: boolean;
    creatorFilters: ActorFilterValue[];
    projectFilters: string[];
    includeNoProject: boolean;
    labelFilters: string[];
    propertyFilters?: Record<string, string[]>;
    dateFilter?: IssueDateFilter | null;
  },
  // Inside a saved view only the user's additions on top of the view's own
  // query count — the baseline is the view, not "active filtering".
  baseline?: IssueViewBaseline,
) {
  let count = 0;
  const delta = (values: string[], fixed?: Set<string>) =>
    fixed ? values.filter((v) => !fixed.has(v)).length : values.length;
  if (delta(state.statusFilters, baseline?.status) > 0) count++;
  if (delta(state.priorityFilters, baseline?.priority) > 0) count++;
  const assigneeDelta =
    delta(state.assigneeFilters.map(actorFilterKey), baseline?.assignee) > 0 ||
    (state.includeNoAssignee && !(baseline?.includeNoAssignee ?? false));
  if (assigneeDelta) count++;
  if (delta(state.creatorFilters.map(actorFilterKey), baseline?.creator) > 0) count++;
  const projectDelta =
    delta(state.projectFilters, baseline?.project) > 0 ||
    (state.includeNoProject && !(baseline?.includeNoProject ?? false));
  if (projectDelta) count++;
  if (delta(state.labelFilters, baseline?.label) > 0) count++;
  for (const [id, selected] of Object.entries(state.propertyFilters ?? {})) {
    if (delta(selected, baseline?.property.get(id)) > 0) count++;
  }
  if (state.dateFilter) count++;
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

// ---------------------------------------------------------------------------
// Label sub-menu content
// ---------------------------------------------------------------------------

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

/**
 * Option checkboxes for one custom property inside the Filter dropdown.
 * Select/multi_select list the definition's options (dot + name); checkbox
 * definitions expose the "true"/"false" pseudo-options with Yes/No labels;
 * actor definitions have no config options at all, so their candidates come
 * from the member directory instead, with the signed-in member first so
 * "this property is me" stays one click away.
 */
function PropertyFilterOptions({
  property,
  counts,
  selected,
  onToggle,
  fixedIds,
  fixedTitle,
}: {
  property: IssueProperty;
  counts: Map<string, number> | undefined;
  selected: string[];
  onToggle: (optionId: string) => void;
  fixedIds?: Set<string>;
  fixedTitle?: string;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const currentUserId = useAuthStore((s) => s.user?.id);
  const actorProperty = isActorPropertyType(property.type);
  const { data: actorMembers = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: actorProperty,
  });

  const actorOptions = useMemo(() => {
    if (!actorProperty) return [];
    return actorMembers
      .slice()
      .sort((a, b) => {
        if (a.user_id === currentUserId) return -1;
        if (b.user_id === currentUserId) return 1;
        return 0;
      })
      .map((m) => ({
        id: formatActorRef("member", m.user_id),
        name: m.name,
        actorType: "member" as const,
        actorId: m.user_id,
      }));
  }, [actorProperty, actorMembers, currentUserId]);

  // "No value" is offered for every property type: it selects issues where the
  // property is unset, the inverse of picking one of the options below.
  const noValueOption = {
    id: NO_PROPERTY_VALUE,
    name: t(($) => $.pickers.custom_property.none),
    color: undefined as string | undefined,
    actorType: undefined as string | undefined,
    actorId: undefined as string | undefined,
  };
  const options = [
    ...(actorProperty
      ? actorOptions.map((option) => ({
          id: option.id,
          name: option.name,
          color: undefined as string | undefined,
          actorType: option.actorType as string | undefined,
          actorId: option.actorId as string | undefined,
        }))
      : property.type === "checkbox"
        ? [
            { id: "true", name: t(($) => $.pickers.custom_property.true_label), color: undefined, actorType: undefined, actorId: undefined },
            { id: "false", name: t(($) => $.pickers.custom_property.false_label), color: undefined, actorType: undefined, actorId: undefined },
          ]
        : (property.config.options ?? []).map((option) => ({
            id: option.id,
            name: option.name,
            color: option.color as string | undefined,
            actorType: undefined as string | undefined,
            actorId: undefined as string | undefined,
          }))),
    noValueOption,
  ];

  return (
    <>
      {options.map((option) => {
        const checked = selected.includes(option.id);
        const count = counts?.get(option.id) ?? 0;
        return (
          <DropdownMenuCheckboxItem
            key={option.id}
            checked={checked}
            disabled={fixedIds?.has(option.id) === true}
            title={fixedIds?.has(option.id) === true ? fixedTitle : undefined}
            onCheckedChange={() => onToggle(option.id)}
            className={FILTER_ITEM_CLASS}
          >
            <HoverCheck checked={checked} />
            {option.actorType && option.actorId && (
              <ActorAvatar actorType={option.actorType} actorId={option.actorId} size="sm" />
            )}
            {option.color && (
              <span
                className="size-2.5 shrink-0 rounded-full"
                style={{ backgroundColor: option.color }}
              />
            )}
            <span className="truncate">{option.name}</span>
            {count > 0 && (
              <span className="ml-auto text-caption text-muted-foreground">
                {count}
              </span>
            )}
          </DropdownMenuCheckboxItem>
        );
      })}
    </>
  );
}

// ---------------------------------------------------------------------------
// Date sub-menu content
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// ViewRefreshIndicator
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// IssuesHeader
// ---------------------------------------------------------------------------

export function IssuesHeader({
  scopedIssues,
  workingAgents,
  allowGantt = false,
  dateFilter = null,
  onDateFilterChange,
  isRefreshing = false,
  facetCountsExact = true,
  tableFacetCounts,
  onTableFacetChange,
  saveViewScope = { kind: "workspace" },
}: {
  scopedIssues: Issue[];
  /** See IssueSurfaceController.workingAgents — the surface-scoped projection
   *  behind the agents-working chip. */
  workingAgents: WorkingAgentSummary[] | undefined;
  allowGantt?: boolean;
  dateFilter?: IssueDateFilter | null;
  onDateFilterChange?: (filter: IssueDateFilter | null) => void;
  isRefreshing?: boolean;
  /** See IssueDisplayControls.facetCountsExact. */
  facetCountsExact?: boolean;
  tableFacetCounts?: IssueTableFacetsResponse;
  onTableFacetChange?: (facet: IssueTableFacetSpec | null) => void;
  /** Where "save as view" files the view. /issues keeps the workspace
   *  default; the project-detail fallback passes its project scope; `null`
   *  hides the save affordance entirely. */
  saveViewScope?: SaveViewScope | null;
}) {
  const { t } = useT("issues");
  const [saveViewOpen, setSaveViewOpen] = useState(false);
  const headerWsId = useWorkspaceId();
  const viewListScope: IssueViewScope | null = saveViewScope
    ? saveViewScope.kind === "project"
      ? { scope_type: "project", scope_id: saveViewScope.projectId }
      : { scope_type: saveViewScope.kind }
    : null;
  const { activeView, views, viewsReady, setActive, missing } = useActiveIssueView(
    headerWsId,
    viewListScope,
  );
  // The open view vanished (deleted / access revoked): fall back to the
  // built-in default and say so — never a blank page (§7).
  useEffect(() => {
    if (missing) {
      setActive(null);
      toast.info(t(($) => $.view_selector.unavailable));
    }
  }, [missing, setActive, t]);
  const viewBaseline = useMemo(
    () => (activeView ? baselineFromQuery(activeView.query) : undefined),
    [activeView],
  );
  const [editTarget, setEditTarget] = useState<{
    view: IssueView;
    fromDefinition: boolean;
  } | null>(null);
  const currentUserId = useAuthStore((s) => s.user?.id ?? null);
  const isViewOwner = !!activeView && activeView.owner_id === currentUserId;
  // The tab belongs to THIS page (Issues vs each project) — see
  // IssuesScopePageKey. The header both renders the tabs and seeds the
  // save-view dialog's default variant from them.
  const scopePage: IssuesScopePageKey =
    saveViewScope?.kind === "project"
      ? `project:${saveViewScope.projectId}`
      : "issues";
  const scope = useIssuesScope(scopePage);
  const setScopeForPage = useIssuesScopeStore((s) => s.setScope);
  const setScope = useCallback(
    (next: IssuesScope) => setScopeForPage(scopePage, next),
    [setScopeForPage, scopePage],
  );
  // The save dialog's default variant: while a saved view is open the view's
  // own variant wins (the rows on screen ARE that variant — a copy must not
  // silently widen to the page tab); otherwise the page tab applies. Memoized
  // on primitives: the dialog resets its draft when this prop's identity
  // changes, so a fresh object per header render would wipe a half-typed
  // name on any background refetch.
  const dialogActorKind = activeView
    ? actorKindForViewVariant(activeView.scope_variant)
    : scope;
  const dialogProjectId =
    saveViewScope?.kind === "project" ? saveViewScope.projectId : null;
  const dialogMyVariant =
    saveViewScope?.kind === "my" ? saveViewScope.variant : null;
  const dialogScope = useMemo<SaveViewScope | null>(() => {
    if (dialogMyVariant) return { kind: "my", variant: dialogMyVariant };
    if (dialogProjectId)
      return {
        kind: "project",
        projectId: dialogProjectId,
        actorKind: dialogActorKind,
      };
    if (saveViewScope) return { kind: "workspace", actorKind: dialogActorKind };
    return null;
    // eslint-disable-next-line react-hooks/exhaustive-deps -- identity keyed on the primitive projections
  }, [dialogMyVariant, dialogProjectId, dialogActorKind, !!saveViewScope]);
  // Bind the workspace agents-working chip to the active view store so
  // shared IssuesHeader consumers (/issues and project detail) toggle the
  // same filter state as the rest of the display controls. /my-issues keeps
  // its own sibling header and passes chip state explicitly.
  const agentRunningFilter = useViewStore((s) => s.agentRunningFilter);
  const toggleAgentRunningFilter = useViewStore(
    (s) => s.toggleAgentRunningFilter,
  );
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

  const scopeLabel = t(($) => $.scope[SCOPE_LABEL_KEY[scope]]);

  return (
    <>
    <div className={cn("min-h-12 shrink-0 py-2 [-webkit-overflow-scrolling:touch]", PAGE_GUTTER)}>
      <div className="flex w-full min-w-0 items-start justify-between gap-2">
        {/* Left: the view bar — built-in tabs and saved views as one flat,
            per-user ordered row; wraps instead of overflowing. */}
        <div className="hidden min-w-0 flex-1 md:block">
          {saveViewScope && viewListScope && (
            <ViewBar
              wsId={headerWsId}
              scope={viewListScope}
              builtins={SCOPE_VALUES.map((s) => ({
                key: s,
                label: t(($) => $.scope[SCOPE_LABEL_KEY[s]]),
                description: t(($) => $.scope[SCOPE_DESC_KEY[s]]),
                active: !activeView && scope === s,
                onSelect: () => {
                  // Built-in tabs exit any open saved view; the surface
                  // returns to its untouched per-tab state.
                  if (activeView) setActive(null);
                  setScope(s);
                },
              }))}
              views={views}
              viewsReady={viewsReady}
              activeView={activeView}
              onSelectView={(view) => setActive(view ? view.id : null)}
              onNewView={() => {
                setEditTarget(null);
                setSaveViewOpen(true);
              }}
              onEditView={(view) => {
                setEditTarget({ view, fromDefinition: true });
                setSaveViewOpen(true);
              }}
            />
          )}
        </div>

        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <Button
                variant="outline"
                size="sm"
                className="shrink-0 gap-1 text-muted-foreground md:hidden"
              >
                <span className="truncate">{scopeLabel}</span>
                <ChevronDown className="size-3 text-muted-foreground" />
              </Button>
            }
          />
          <DropdownMenuContent align="start" className="w-auto">
            <DropdownMenuRadioGroup value={scope} onValueChange={(value) => setScope(value as IssuesScope)}>
              {SCOPE_VALUES.map((s) => (
                <DropdownMenuRadioItem key={s} value={s}>
                  {t(($) => $.scope[SCOPE_LABEL_KEY[s]])}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>

        <div className="flex shrink-0 items-center gap-1">
          {agentRunningFilter && (
            <span className="mr-1 hidden text-caption text-muted-foreground md:inline">
              {t(($) => $.agent_activity.filter_active_label)}
            </span>
          )}
          <WorkspaceAgentWorkingChip
            value={agentRunningFilter}
            onToggle={toggleAgentRunningFilter}
            agents={workingAgents}
          />
          <IssueDisplayControls
            scopedIssues={scopedIssues}
            allowGantt={allowGantt}
            dateFilter={dateFilter}
            onDateFilterChange={onDateFilterChange}
            facetCountsExact={facetCountsExact}
            tableFacetCounts={tableFacetCounts}
            onTableFacetChange={onTableFacetChange}
            viewBaseline={viewBaseline}
          />
          <ViewRefreshIndicator active={isRefreshing} />
        </div>
      </div>
    </div>
    <FilterChipsBar
      dateFilter={dateFilter}
      onDateFilterChange={onDateFilterChange}
      viewBaseline={viewBaseline}
      saveLabel={
        activeView
          ? isViewOwner
            ? t(($) => $.filters.chip_edit)
            : t(($) => $.filters.chip_save_as)
          : undefined
      }
      onSave={
        saveViewScope
          ? () => {
              setEditTarget(
                activeView && isViewOwner
                  ? { view: activeView, fromDefinition: false }
                  : null,
              );
              setSaveViewOpen(true);
            }
          : undefined
      }
    />
    {dialogScope && (
      <SaveViewDialog
        open={saveViewOpen}
        onOpenChange={setSaveViewOpen}
        scope={dialogScope}
        editView={editTarget?.view ?? null}
        seedFromDefinition={editTarget?.fromDefinition ?? false}
      />
    )}
    </>
  );
}

/**
 * The full filter menu (every dimension, search, counts) behind a caller-
 * supplied trigger. The toolbar wraps it in its labeled button; the
 * save-view dialog attaches it to a ghost "add filter" chip targeting the
 * draft store. Counts follow `scopedIssues` — pass nothing to hide them.
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
}: {
  trigger: React.ReactElement;
  tooltip?: string;
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
  const labelFilters = useViewStore((s) => s.labelFilters);
  const propertyFilters = useViewStore((s) => s.propertyFilters);
  const viewStoreApi = useViewStoreApi();
  const act = viewStoreApi.getState();
  const wsId = useWorkspaceId();
  const statusOptions = useStatusOptions(wsId);
  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(wsId));
  const filterableProperties = useMemo(
    () =>
      workspaceProperties.filter(
        (p) =>
          p.type === "select" ||
          p.type === "multi_select" ||
          p.type === "checkbox" ||
          isActorPropertyType(p.type),
      ),
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
        labelFilters,
        dateFilter: showDateFilter ? dateFilter : null,
      },
      viewBaseline,
    ) > 0;
  const fixedTitle = viewBaseline ? t(($) => $.filters.in_view) : undefined;
  const dateFilterLabel = showDateFilter && dateFilter
    ? `${t(($) => $.filters[DATE_FIELD_LABEL_KEY[dateFilter.field]])}: ${
        dateFilter.from === dateFilter.to
          ? shortDateLabel(dateFilter.from)
          : `${shortDateLabel(dateFilter.from)} - ${shortDateLabel(dateFilter.to)}`
      }`
    : null;

  // Base UI's render-prop merges its own ref with ours, so cloning the
  // caller's element only adds the ref we need for rect capture.
  const anchoredTrigger = freezeAnchor
    ? cloneElement(trigger, { ref: triggerRef } as Partial<React.HTMLAttributes<HTMLElement>>)
    : trigger;

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
        if (!open) onTableFacetChange?.(null);
        onOpenChange?.(open);
      }}
    >
      {tooltip ? (
        <Tooltip>
          <DropdownMenuTrigger render={<TooltipTrigger render={anchoredTrigger} />} />
          <TooltipContent side="bottom">{tooltip}</TooltipContent>
        </Tooltip>
      ) : (
        <DropdownMenuTrigger render={anchoredTrigger} />
      )}
          <DropdownMenuContent align="end" className="w-auto" anchor={frozenAnchor ?? undefined}>
            {/* Status */}
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "status" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <CircleDot className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_status)}</span>
                {statusFilters.length > 0 && (
                  <span className="text-caption text-primary font-medium">
                    {statusFilters.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-48">
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

            {/* Priority */}
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "priority" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <SignalHigh className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_priority)}</span>
                {priorityFilters.length > 0 && (
                  <span className="text-caption text-primary font-medium">
                    {priorityFilters.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-44">
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

            {showDateFilter && onDateFilterChange && (
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
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "assignee" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <User className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_assignee)}</span>
                {(assigneeFilters.length > 0 || includeNoAssignee) && (
                  <span className="text-caption text-primary font-medium">
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
                  fixedKeys={viewBaseline?.assignee}
                  noAssigneeFixed={viewBaseline?.includeNoAssignee === true}
                  fixedTitle={fixedTitle}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Creator */}
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "creator" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <UserPen className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_creator)}</span>
                {creatorFilters.length > 0 && (
                  <span className="text-caption text-primary font-medium">
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
                  fixedKeys={viewBaseline?.creator}
                  fixedTitle={fixedTitle}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Project */}
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "project" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <FolderKanban className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_project)}</span>
                {(projectFilters.length > 0 || includeNoProject) && (
                  <span className="text-caption text-primary font-medium">
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
                  fixedIds={viewBaseline?.project}
                  noProjectFixed={viewBaseline?.includeNoProject === true}
                  fixedTitle={fixedTitle}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Label */}
            <DropdownMenuSub
              onOpenChange={(open) =>
                onTableFacetChange?.(open ? { kind: "label" } : null)
              }
            >
              <DropdownMenuSubTrigger>
                <Tag className="size-3.5" />
                <span className="flex-1">{t(($) => $.filters.section_label)}</span>
                {labelFilters.length > 0 && (
                  <span className="text-caption text-primary font-medium">
                    {labelFilters.length}
                  </span>
                )}
              </DropdownMenuSubTrigger>
              <DropdownMenuSubContent className="w-auto min-w-52 p-0">
                <LabelSubContent
                  counts={counts.label}
                  selected={labelFilters}
                  onToggle={act.toggleLabelFilter}
                  fixedIds={viewBaseline?.label}
                  fixedTitle={fixedTitle}
                />
              </DropdownMenuSubContent>
            </DropdownMenuSub>

            {/* Custom properties — one sub per filterable definition */}
            {filterableProperties.length > 0 && <DropdownMenuSeparator />}
            {filterableProperties.map((property) => {
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
                    {selected.length > 0 && (
                      <span className="text-caption text-primary font-medium">
                        {selected.length}
                      </span>
                    )}
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent className="w-auto min-w-44 p-1">
                    <PropertyFilterOptions
                      property={property}
                      counts={counts.property.get(property.id)}
                      selected={selected}
                      onToggle={(optionId) => act.togglePropertyFilter(property.id, optionId)}
                      fixedIds={viewBaseline?.property.get(property.id)}
                      fixedTitle={fixedTitle}
                    />
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              );
            })}

            {/* Reset */}
            {hasActiveFilters && (
              <>
                <DropdownMenuSeparator />
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
              </>
            )}
          </DropdownMenuContent>
    </DropdownMenu>
  );
}

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
}: {
  scopedIssues: Issue[];
  hideViewToggle?: boolean;
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
  const [tableGroupMenuOpen, setTableGroupMenuOpen] = useState(false);
  const [viewMenuOpen, setViewMenuOpen] = useState(false);
  const viewMode = useViewStore((s) => s.viewMode);
  const statusFilters = useViewStore((s) => s.statusFilters);
  const priorityFilters = useViewStore((s) => s.priorityFilters);
  const assigneeFilters = useViewStore((s) => s.assigneeFilters);
  const includeNoAssignee = useViewStore((s) => s.includeNoAssignee);
  const creatorFilters = useViewStore((s) => s.creatorFilters);
  const projectFilters = useViewStore((s) => s.projectFilters);
  const includeNoProject = useViewStore((s) => s.includeNoProject);
  const labelFilters = useViewStore((s) => s.labelFilters);
  const propertyFilters = useViewStore((s) => s.propertyFilters);
  const cardPropertyIds = useViewStore((s) => s.cardPropertyIds);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const grouping = useViewStore((s) => s.grouping);
  const swimlaneGrouping = useViewStore((s) => s.swimlaneGrouping);
  const cardProperties = useViewStore((s) => s.cardProperties);
  const tableGrouping = useViewStore((s) => s.tableGrouping ?? "none");
  const tableHierarchy = useViewStore((s) => s.tableHierarchy ?? true);
  const showSubIssues = useViewStore((s) => s.showSubIssues);
  const act = useViewStoreApi().getState();
  const headerWsId = useWorkspaceId();
  // Active custom-property catalog: drives the filter sections, dynamic
  // sort/grouping options, and the card-property toggles below.
  const { data: workspaceProperties = [] } = useQuery(propertyListOptions(headerWsId));
  const propertyById = useMemo(
    () => new Map(workspaceProperties.map((p) => [p.id, p])),
    [workspaceProperties],
  );
  const filterableProperties = useMemo(
    () =>
      workspaceProperties.filter(
        (p) =>
          p.type === "select" ||
          p.type === "multi_select" ||
          p.type === "checkbox" ||
          isActorPropertyType(p.type),
      ),
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
      labelFilters,
      dateFilter: showDateFilter ? dateFilter : null,
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
  const GROUPING_LABEL_KEY: Record<typeof GROUPING_OPTIONS[number]["value"], "group_status" | "group_assignee"> = {
    status: "group_status",
    assignee: "group_assignee",
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
  const sortPropertyId = propertyIdFromViewKey(sortBy);
  const groupingPropertyId = propertyIdFromViewKey(grouping);
  const tableGroupingPropertyId = propertyIdFromViewKey(tableGrouping);
  // Property-backed sort/grouping label straight from the definition name;
  // a stale persisted id (archived/deleted definition) falls back to the
  // manual/status default label.
  const sortLabel = sortPropertyId
    ? propertyById.get(sortPropertyId)?.name ?? t(($) => $.display.sort_manual)
    : t(($) => $.display[SORT_LABEL_KEY[sortBy as keyof typeof SORT_LABEL_KEY]]);
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
        : t(($) => $.table.group_none);
  const controlButtonClass = "h-8 w-8 gap-1 px-0 text-muted-foreground md:h-7 md:w-auto md:px-2.5";

  return (
    <div className="flex shrink-0 items-center gap-1">
        <IssueFilterMenu
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
        />

        {viewMode === "table" && (
          <DropdownMenu
            open={tableGroupMenuOpen}
            onOpenChange={setTableGroupMenuOpen}
          >
            <DropdownMenuTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  className={controlButtonClass}
                >
                  <Rows3 className="size-3.5" />
                  <span className="hidden md:inline">
                    {effectiveTableGrouping === "none"
                      ? t(($) => $.table.group_label)
                      : t(($) => $.table.group_active, {
                          group: tableGroupingLabel,
                        })}
                  </span>
                  <ChevronDown className="size-3 text-muted-foreground" />
                </Button>
              }
            />
            <DropdownMenuContent align="end" className="w-auto min-w-48">
              <DropdownMenuRadioGroup
                value={effectiveTableGrouping}
                onValueChange={(value) => {
                  act.setTableGrouping(value as TableGrouping);
                  setTableGroupMenuOpen(false);
                }}
              >
                <DropdownMenuRadioItem value="none">
                  {t(($) => $.table.group_none)}
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="status">
                  {t(($) => $.table.columns.status)}
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="assignee">
                  {t(($) => $.table.columns.assignee)}
                </DropdownMenuRadioItem>
                {tableGroupableProperties.map((property) => (
                  <DropdownMenuRadioItem
                    key={property.id}
                    value={`property:${property.id}`}
                  >
                    <PropertyIcon
                      property={property}
                      className="size-3.5 text-caption"
                    />
                    <span>{property.name}</span>
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        )}

        {viewMode === "table" && (
          <TableColumnPicker
            properties={workspaceProperties}
            trigger={
              <Button
                variant="outline"
                size="sm"
                className={controlButtonClass}
              >
                <Columns3 className="size-3.5" />
                <span className="hidden md:inline">
                  {t(($) => $.table.columns.section)}
                </span>
                <ChevronDown className="size-3 text-muted-foreground" />
              </Button>
            }
          />
        )}

        {/* Display settings */}
        <Popover>
          <Tooltip>
            <PopoverTrigger
              render={
                <TooltipTrigger
                  render={
                    <Button variant="outline" size="sm" className={controlButtonClass}>
                      <SlidersHorizontal className="size-3.5" />
                      <span className="hidden md:inline">{t(($) => $.display.button)}</span>
                    </Button>
                  }
                />
              }
            />
            <TooltipContent side="bottom">{t(($) => $.display.tooltip)}</TooltipContent>
          </Tooltip>
          <PopoverContent align="end" className="w-64 p-3">
            <div className="space-y-3">
              {/* Uniform rows: caption label left, control right. Spacing
                  separates sections — no dividers (see UI rules). */}
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
                    <SelectTrigger size="sm" className="w-32" aria-label={t(($) => $.display.grouping_section)}>
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
                    <SelectTrigger size="sm" className="w-32" aria-label={t(($) => $.display.grouping_section)}>
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
              <div className="flex items-center justify-between gap-3">
                <span className="text-caption font-medium text-muted-foreground">
                  {t(($) => $.display.ordering_section)}
                </span>
                <div className="flex items-center gap-1.5">
                  <Select
                    items={[
                      ...SORT_OPTIONS.map((opt) => ({
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
                    <SelectTrigger size="sm" className="w-26" aria-label={t(($) => $.display.ordering_section)}>
                      <SelectValue>{sortLabel}</SelectValue>
                    </SelectTrigger>
                    <SelectContent align="end">
                      <SelectGroup>
                      {SORT_OPTIONS.map((opt) => (
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
                  {sortBy !== "position" && (
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
                  )}
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
              {viewMode !== "table" && (
                <div>
                  <span className="text-caption font-medium text-muted-foreground">
                    {t(($) => $.display.card_properties_section)}
                  </span>
                  {/* Chip toggles (pressed = shown on cards). Unpressed chips
                      dim so the active set reads at a glance. */}
                  <div className="mt-2 flex flex-wrap gap-1">
                    {CARD_PROPERTY_OPTIONS.map((opt) => (
                      <Toggle
                        key={opt.key}
                        size="sm"
                        variant="outline"
                        pressed={cardProperties[opt.key]}
                        onPressedChange={() => act.toggleCardProperty(opt.key)}
                        className="h-6 rounded-md px-2 text-caption text-muted-foreground hover:bg-muted/60 aria-pressed:border-transparent aria-pressed:bg-accent aria-pressed:text-accent-foreground aria-pressed:hover:bg-accent/80"
                      >
                        {t(($) => $.display[CARD_PROPERTY_LABEL_KEY[opt.key]])}
                      </Toggle>
                    ))}
                    {workspaceProperties.map((property) => (
                      <Toggle
                        key={property.id}
                        size="sm"
                        variant="outline"
                        pressed={cardPropertyIds.includes(property.id)}
                        onPressedChange={() => act.toggleCardPropertyId(property.id)}
                        className="h-6 max-w-40 rounded-md px-2 text-caption text-muted-foreground hover:bg-muted/60 aria-pressed:border-transparent aria-pressed:bg-accent aria-pressed:text-accent-foreground aria-pressed:hover:bg-accent/80"
                      >
                        <span className="truncate">{property.name}</span>
                      </Toggle>
                    ))}
                  </div>
                </div>
              )}
            </div>
          </PopoverContent>
        </Popover>

        {/* View toggle. If a store has `viewMode === "gantt"` persisted but
            this surface doesn't render Gantt, fall back to "list" so the
            trigger icon matches what's actually on screen. */}
        {!hideViewToggle && (
          <DropdownMenu open={viewMenuOpen} onOpenChange={setViewMenuOpen}>
            <Tooltip>
              <DropdownMenuTrigger
                render={
                  <TooltipTrigger
                    render={
                      <Button variant="outline" size="sm" className={controlButtonClass}>
                        {viewMode === "board" ? (
                          <Columns3 className="size-3.5" />
                        ) : viewMode === "table" ? (
                          <Table2 className="size-3.5" />
                        ) : viewMode === "swimlane" ? (
                          <Waves className="size-3.5" />
                        ) : viewMode === "gantt" && allowGantt ? (
                          <ChartGantt className="size-3.5" />
                        ) : (
                          <List className="size-3.5" />
                        )}
                        <span className="hidden md:inline">
                          {viewMode === "board"
                            ? t(($) => $.view.board)
                            : viewMode === "table"
                            ? t(($) => $.view.table)
                            : viewMode === "swimlane"
                            ? t(($) => $.view.swimlane)
                            : viewMode === "gantt" && allowGantt
                            ? t(($) => $.view.gantt)
                            : t(($) => $.view.list)}
                        </span>
                      </Button>
                    }
                  />
                }
              />
              <TooltipContent side="bottom">
                {viewMode === "board"
                  ? t(($) => $.view.tooltip_board)
                  : viewMode === "table"
                  ? t(($) => $.view.tooltip_table)
                  : viewMode === "swimlane"
                  ? t(($) => $.view.tooltip_swimlane)
                  : viewMode === "gantt" && allowGantt
                  ? t(($) => $.view.tooltip_gantt)
                  : t(($) => $.view.tooltip_list)}
              </TooltipContent>
            </Tooltip>
            <DropdownMenuContent align="end" className="w-auto">
              <DropdownMenuGroup>
                <DropdownMenuLabel>{t(($) => $.view.section)}</DropdownMenuLabel>
              </DropdownMenuGroup>
              <DropdownMenuRadioGroup
                value={viewMode}
                onValueChange={(v) => {
                  act.setViewMode(v as ViewMode);
                  setViewMenuOpen(false);
                }}
              >
                <DropdownMenuRadioItem value="board">
                  <Columns3 />
                  {t(($) => $.view.board)}
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="list">
                  <List />
                  {t(($) => $.view.list)}
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="table">
                  <Table2 />
                  {t(($) => $.view.table)}
                </DropdownMenuRadioItem>
                <DropdownMenuRadioItem value="swimlane">
                  <Waves />
                  {t(($) => $.view.swimlane)}
                </DropdownMenuRadioItem>
                {allowGantt && (
                  <DropdownMenuRadioItem value="gantt">
                    <ChartGantt />
                    {t(($) => $.view.gantt)}
                  </DropdownMenuRadioItem>
                )}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        )}

    </div>
  );
}
