"use client";

import { useMemo, useState } from "react";
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
  SignalHigh,
  SlidersHorizontal,
  X,
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
  ALL_STATUSES,
  PRIORITY_ORDER,
} from "@multica/core/issues/config";
import { StatusIcon, PriorityIcon } from ".";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { labelListOptions } from "@multica/core/labels/queries";
import { propertyListOptions } from "@multica/core/properties";
import { propertyIdFromViewKey } from "@multica/core/issues/stores/view-store";
import type { IssueProperty } from "@multica/core/types";
import { ProjectIcon } from "../../projects/components/project-icon";
import { ActorAvatar } from "../../common/actor-avatar";
import { PropertyIcon } from "../../common/property-icon";
import { LabelChip } from "../../labels/label-chip";
import {
  SORT_OPTIONS,
  GROUPING_OPTIONS,
  SWIMLANE_GROUPINGS,
  CARD_PROPERTY_OPTIONS,
  TABLE_SYSTEM_COLUMNS,
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
import { addDaysDateOnly, dateOnlyToLocalDate, formatDateOnly, toDateOnly, todayDateOnly } from "@multica/core/issues/date";
import {
  useIssuesScopeStore,
  type IssuesScope,
} from "@multica/core/issues/stores/issues-scope-store";
import { Tooltip, TooltipTrigger, TooltipContent } from "@multica/ui/components/ui/tooltip";
import type { Issue } from "@multica/core/types";
import { useT } from "../../i18n";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { WorkspaceAgentWorkingChip } from "./workspace-agent-working-chip";

type LocalDateRange = {
  from: Date | undefined;
  to?: Date;
};

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
  propertyFilters?: Record<string, string[]>;
  dateFilter?: IssueDateFilter | null;
}) {
  let count = 0;
  if (state.statusFilters.length > 0) count++;
  if (state.priorityFilters.length > 0) count++;
  if (state.assigneeFilters.length > 0 || state.includeNoAssignee) count++;
  if (state.creatorFilters.length > 0) count++;
  if (state.projectFilters.length > 0 || state.includeNoProject) count++;
  if (state.labelFilters.length > 0) count++;
  for (const selected of Object.values(state.propertyFilters ?? {})) {
    if (selected.length > 0) count++;
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

function useIssueCounts(allIssues: Issue[]) {
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
                  <ActorAvatar actorType="member" actorId={m.user_id} size="sm" />
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
                  <ActorAvatar actorType="agent" actorId={a.id} size="sm" showStatusDot />
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
                  <ActorAvatar actorType="squad" actorId={s.id} size="sm" />
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

/**
 * Option checkboxes for one custom property inside the Filter dropdown.
 * Select/multi_select list the definition's options (dot + name); checkbox
 * definitions expose the "true"/"false" pseudo-options with Yes/No labels.
 */
function PropertyFilterOptions({
  property,
  counts,
  selected,
  onToggle,
}: {
  property: IssueProperty;
  counts: Map<string, number> | undefined;
  selected: string[];
  onToggle: (optionId: string) => void;
}) {
  const { t } = useT("issues");
  const options =
    property.type === "checkbox"
      ? [
          { id: "true", name: t(($) => $.pickers.custom_property.true_label), color: undefined },
          { id: "false", name: t(($) => $.pickers.custom_property.false_label), color: undefined },
        ]
      : (property.config.options ?? []).map((option) => ({
          id: option.id,
          name: option.name,
          color: option.color as string | undefined,
        }));

  return (
    <>
      {options.map((option) => {
        const checked = selected.includes(option.id);
        const count = counts?.get(option.id) ?? 0;
        return (
          <DropdownMenuCheckboxItem
            key={option.id}
            checked={checked}
            onCheckedChange={() => onToggle(option.id)}
            className={FILTER_ITEM_CLASS}
          >
            <HoverCheck checked={checked} />
            {option.color && (
              <span
                className="size-2.5 shrink-0 rounded-full"
                style={{ backgroundColor: option.color }}
              />
            )}
            <span className="truncate">{option.name}</span>
            {count > 0 && (
              <span className="ml-auto text-xs text-muted-foreground">
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
  };

  return (
    <>
      <DropdownMenuGroup>
        <DropdownMenuLabel>{t(($) => $.filters.date_field)}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={field} onValueChange={(next) => setFieldValue(next as IssueDateField)}>
          {(["created_at", "updated_at"] as const).map((option) => (
            <DropdownMenuRadioItem key={option} value={option}>
              {t(($) => $.filters[DATE_FIELD_LABEL_KEY[option]])}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuGroup>

      <DropdownMenuSeparator />
      <DropdownMenuItem onClick={() => applyPreset(1)}>
        {t(($) => $.filters.date_today)}
      </DropdownMenuItem>
      <DropdownMenuItem onClick={() => applyPreset(3)}>
        {t(($) => $.filters.date_last_3_days)}
      </DropdownMenuItem>
      <DropdownMenuItem onClick={() => applyPreset(7)}>
        {t(($) => $.filters.date_last_7_days)}
      </DropdownMenuItem>

      <div className="px-1.5 py-1">
        <Popover>
          <PopoverTrigger
            render={
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-full justify-start px-0 text-sm font-normal"
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
  workingIssues,
  allowGantt = false,
  dateFilter = null,
  onDateFilterChange,
  isRefreshing = false,
  facetCountsExact = true,
}: {
  scopedIssues: Issue[];
  /** The rows the agents-working filter would leave on screen — undefined
   *  when the set is unknown (chip renders indeterminate). Scopes the chip:
   *  it counts the agents working on these rows. */
  workingIssues: Issue[] | undefined;
  allowGantt?: boolean;
  dateFilter?: IssueDateFilter | null;
  onDateFilterChange?: (filter: IssueDateFilter | null) => void;
  isRefreshing?: boolean;
  /** See IssueDisplayControls.facetCountsExact. */
  facetCountsExact?: boolean;
}) {
  const { t } = useT("issues");
  const scope = useIssuesScopeStore((s) => s.scope);
  const setScope = useIssuesScopeStore((s) => s.setScope);
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
    <div className="h-12 shrink-0 overflow-x-auto px-4 [-webkit-overflow-scrolling:touch]">
      <div className="flex h-full w-max min-w-full items-center justify-between gap-2">
        {/* Left: scope buttons */}
        <div className="hidden shrink-0 items-center gap-1 md:flex">
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
            <span className="mr-1 hidden text-xs text-muted-foreground md:inline">
              {t(($) => $.agent_activity.filter_active_label)}
            </span>
          )}
          <WorkspaceAgentWorkingChip
            value={agentRunningFilter}
            onToggle={toggleAgentRunningFilter}
            workingIssues={workingIssues}
          />
          <IssueDisplayControls
            scopedIssues={scopedIssues}
            allowGantt={allowGantt}
            dateFilter={dateFilter}
            onDateFilterChange={onDateFilterChange}
            facetCountsExact={facetCountsExact}
          />
          <ViewRefreshIndicator active={isRefreshing} />
        </div>
      </div>
    </div>
  );
}

export function IssueDisplayControls({
  scopedIssues,
  hideViewToggle = false,
  allowGantt = false,
  dateFilter = null,
  onDateFilterChange,
  facetCountsExact = true,
}: {
  scopedIssues: Issue[];
  hideViewToggle?: boolean;
  dateFilter?: IssueDateFilter | null;
  onDateFilterChange?: (filter: IssueDateFilter | null) => void;
  // Only Project Detail renders <GanttView>; other surfaces (global /issues,
  // /my-issues, actor panel) ignore viewMode === "gantt" and would silently
  // fall back to List if the option were exposed there. Keep Gantt opt-in.
  allowGantt?: boolean;
  /**
   * Whether `scopedIssues` covers the surface's full window. The table's
   * offset pagination hands us only the loaded pages — presenting counts
   * derived from a partial window as per-option totals under-reports
   * (round-2 review P2#3), so the badges are suppressed until the window is
   * complete. Filter OPTIONS are unaffected; they come from the
   * member/agent/project/label directories.
   */
  facetCountsExact?: boolean;
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
  const labelFilters = useViewStore((s) => s.labelFilters);
  const propertyFilters = useViewStore((s) => s.propertyFilters);
  const cardPropertyIds = useViewStore((s) => s.cardPropertyIds);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const grouping = useViewStore((s) => s.grouping);
  const swimlaneGrouping = useViewStore((s) => s.swimlaneGrouping);
  const cardProperties = useViewStore((s) => s.cardProperties);
  const tableColumns = useViewStore((s) => s.tableColumns);
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
      workspaceProperties.filter((p) =>
        p.type === "select" || p.type === "multi_select" || p.type === "checkbox",
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
        ["select", "multi_select", "checkbox"].includes(p.type),
      ),
    [workspaceProperties],
  );
  const visibleTableColumns = useMemo(
    () => new Set(tableColumns.map((column) => column.key)),
    [tableColumns],
  );

  const counts = useIssueCounts(
    facetCountsExact ? scopedIssues : NO_COUNT_ISSUES,
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

  const activeFilterCount = getActiveFilterCount({
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
  });
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
  const dateFilterLabel = showDateFilter && dateFilter
    ? `${t(($) => $.filters[DATE_FIELD_LABEL_KEY[dateFilter.field]])}: ${
        dateFilter.from === dateFilter.to
          ? shortDateLabel(dateFilter.from)
          : `${shortDateLabel(dateFilter.from)} - ${shortDateLabel(dateFilter.to)}`
      }`
    : null;
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
        {/* Filter */}
        <DropdownMenu>
          <Tooltip>
            <DropdownMenuTrigger
              render={
                <TooltipTrigger
                  render={
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
                      {hasActiveFilters && (
                        <span
                          role="button"
                          tabIndex={-1}
                          className="-mr-1 ml-0.5 hidden rounded-sm p-0.5 hover:bg-white/20 md:inline-flex"
                          onClick={(e) => {
                            e.preventDefault();
                            e.stopPropagation();
                            act.clearFilters();
                            onDateFilterChange?.(null);
                          }}
                          onPointerDown={(e) => e.stopPropagation()}
                        >
                          <X className="size-3" />
                        </span>
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

            {showDateFilter && onDateFilterChange && (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <CalendarDays className="size-3.5" />
                  <span className="flex-1">{t(($) => $.filters.section_date)}</span>
                  {dateFilterLabel && (
                    <span className="max-w-36 truncate text-xs text-primary font-medium">
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

            {/* Custom properties — one sub per filterable definition */}
            {filterableProperties.length > 0 && <DropdownMenuSeparator />}
            {filterableProperties.map((property) => {
              const selected = propertyFilters[property.id] ?? [];
              return (
                <DropdownMenuSub key={property.id}>
                  <DropdownMenuSubTrigger>
                    {property.icon ? (
                      <PropertyIcon property={property} className="size-3.5 text-xs" />
                    ) : (
                      <SlidersHorizontal className="size-3.5" />
                    )}
                    <span className="flex-1 truncate">{property.name}</span>
                    {selected.length > 0 && (
                      <span className="text-xs text-primary font-medium">
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
                    act.clearFilters();
                    onDateFilterChange?.(null);
                  }}
                >
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
                    <Button variant="outline" size="sm" className={controlButtonClass}>
                      {sortBy === "position" ? (
                        <SlidersHorizontal className="size-3.5" />
                      ) : sortDirection === "asc" ? (
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
                      <DropdownMenuRadioGroup value={grouping} onValueChange={(v) => act.setGrouping(v as IssueGrouping)}>
                        {GROUPING_OPTIONS.map((opt) => (
                          <DropdownMenuRadioItem key={opt.value} value={opt.value}>
                            {t(($) => $.display[GROUPING_LABEL_KEY[opt.value]])}
                          </DropdownMenuRadioItem>
                        ))}
                        {groupableProperties.map((property) => (
                          <DropdownMenuRadioItem key={property.id} value={`property:${property.id}`}>
                            <PropertyIcon property={property} className="size-3.5 text-xs" />
                            <span>{property.name}</span>
                          </DropdownMenuRadioItem>
                        ))}
                      </DropdownMenuRadioGroup>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
            )}
            {viewMode === "swimlane" && (
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
                          {swimlaneGroupingLabel}
                          <ChevronDown className="size-3 text-muted-foreground" />
                        </Button>
                      }
                    />
                    <DropdownMenuContent align="start" className="w-auto">
                      <DropdownMenuRadioGroup
                        value={swimlaneGrouping}
                        onValueChange={(v) => act.setSwimlaneGrouping(v as SwimlaneGrouping)}
                      >
                        {SWIMLANE_GROUPINGS.map((value) => (
                          <DropdownMenuRadioItem key={value} value={value}>
                            {t(($) => $.display[SWIMLANE_GROUPING_LABEL_KEY[value]])}
                          </DropdownMenuRadioItem>
                        ))}
                      </DropdownMenuRadioGroup>
                    </DropdownMenuContent>
                  </DropdownMenu>
                </div>
              </div>
            )}

            {viewMode === "table" && (
              <div className="border-b px-3 py-2.5">
                <label className="flex cursor-pointer items-center justify-between gap-3">
                  <span className="min-w-0">
                    <span className="block text-sm">
                      {t(($) => $.table.hierarchy)}
                    </span>
                    <span className="block text-xs text-muted-foreground">
                      {t(($) => $.table.hierarchy_description)}
                    </span>
                  </span>
                  <Switch
                    size="sm"
                    checked={tableHierarchy}
                    onCheckedChange={() => act.toggleTableHierarchy()}
                  />
                </label>
                <div className="mt-3">
                  <span className="text-xs font-medium text-muted-foreground">
                    {t(($) => $.table.group_label)}
                  </span>
                  <DropdownMenu>
                    <DropdownMenuTrigger
                      render={
                        <Button
                          variant="outline"
                          size="sm"
                          className="mt-1.5 w-full justify-between text-xs"
                        >
                          {tableGroupingLabel}
                          <ChevronDown className="size-3 text-muted-foreground" />
                        </Button>
                      }
                    />
                    <DropdownMenuContent align="start" className="w-auto min-w-48">
                      <DropdownMenuRadioGroup
                        value={effectiveTableGrouping}
                        onValueChange={(value) =>
                          act.setTableGrouping(value as TableGrouping)
                        }
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
                              className="size-3.5 text-xs"
                            />
                            <span>{property.name}</span>
                          </DropdownMenuRadioItem>
                        ))}
                      </DropdownMenuRadioGroup>
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
                    <DropdownMenuRadioGroup value={sortBy} onValueChange={(v) => act.setSortBy(v as SortField)}>
                      {SORT_OPTIONS.map((opt) => (
                        <DropdownMenuRadioItem key={opt.value} value={opt.value}>
                          {t(($) => $.display[SORT_LABEL_KEY[opt.value as keyof typeof SORT_LABEL_KEY]])}
                        </DropdownMenuRadioItem>
                      ))}
                      {sortableProperties.map((property) => (
                        <DropdownMenuRadioItem key={property.id} value={`property:${property.id}`}>
                          <PropertyIcon property={property} className="size-3.5 text-xs" />
                          <span>{property.name}</span>
                        </DropdownMenuRadioItem>
                      ))}
                    </DropdownMenuRadioGroup>
                  </DropdownMenuContent>
                </DropdownMenu>
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

            <label className="flex cursor-pointer items-center justify-between border-b px-3 py-2.5">
              <span className="text-sm">{t(($) => $.display.show_sub_issues)}</span>
              <Switch
                size="sm"
                checked={showSubIssues}
                onCheckedChange={() => act.toggleShowSubIssues()}
              />
            </label>

            {viewMode === "table" ? (
              <div className="max-h-80 overflow-y-auto px-3 py-2.5">
                <span className="text-xs font-medium text-muted-foreground">
                  {t(($) => $.table.columns.section)}
                </span>
                <p className="mt-2 text-xs text-muted-foreground">
                  {t(($) => $.table.columns.system_section)}
                </p>
                <div className="mt-1.5 space-y-2">
                  {TABLE_SYSTEM_COLUMNS.map((key) => (
                    <label
                      key={key}
                      className={
                        key === "title"
                          ? "flex items-center justify-between"
                          : "flex cursor-pointer items-center justify-between"
                      }
                    >
                      <span className="text-sm">
                        {t(($) => $.table.columns[key])}
                      </span>
                      <Switch
                        size="sm"
                        checked={visibleTableColumns.has(key)}
                        disabled={key === "title"}
                        onCheckedChange={() => act.toggleTableColumn(key)}
                      />
                    </label>
                  ))}
                </div>
                {workspaceProperties.length > 0 && (
                  <>
                    <p className="mt-3 text-xs text-muted-foreground">
                      {t(($) => $.table.columns.property_section)}
                    </p>
                    <div className="mt-1.5 space-y-2">
                      {workspaceProperties.map((property) => {
                        const key = `property:${property.id}` as const;
                        return (
                          <label
                            key={property.id}
                            className="flex cursor-pointer items-center justify-between gap-3"
                          >
                            <span className="flex min-w-0 items-center gap-1.5 truncate text-sm">
                              <PropertyIcon
                                property={property}
                                className="size-3.5 text-xs"
                              />
                              <span className="truncate">{property.name}</span>
                            </span>
                            <Switch
                              size="sm"
                              checked={visibleTableColumns.has(key)}
                              onCheckedChange={() =>
                                act.toggleTableColumn(key)
                              }
                            />
                          </label>
                        );
                      })}
                    </div>
                  </>
                )}
              </div>
            ) : (
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
                  {workspaceProperties.map((property) => (
                    <label
                      key={property.id}
                      className="flex cursor-pointer items-center justify-between"
                    >
                      <span className="flex min-w-0 items-center gap-1.5 truncate text-sm">
                        <PropertyIcon
                          property={property}
                          className="size-3.5 text-xs"
                        />
                        <span className="truncate">{property.name}</span>
                      </span>
                      <Switch
                        size="sm"
                        checked={cardPropertyIds.includes(property.id)}
                        onCheckedChange={() => act.toggleCardPropertyId(property.id)}
                      />
                    </label>
                  ))}
                </div>
              </div>
            )}
          </PopoverContent>
        </Popover>

        {/* View toggle. If a store has `viewMode === "gantt"` persisted but
            this surface doesn't render Gantt, fall back to "list" so the
            trigger icon matches what's actually on screen. */}
        {!hideViewToggle && (
          <DropdownMenu>
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
              <DropdownMenuRadioGroup value={viewMode} onValueChange={(v) => act.setViewMode(v as ViewMode)}>
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
