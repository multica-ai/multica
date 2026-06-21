"use client";

// CEREBRO-PATCH(issues-header-cerebro): cerebro modification of upstream file

import { Fragment, useMemo, useState, type ReactNode } from "react";
import {
  ArrowDown,
  ArrowUp,
  CalendarDays,
  Check,
  ChevronDown,
  CircleDot,
  Columns3,
  Filter,
  FolderKanban,
  FolderMinus,
  List,
  Plus,
  SignalHigh,
  SlidersHorizontal,
  X,
  Tag,
  User,
  UserMinus,
  UserPen,
  Waves,
  ChartGantt,
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
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
import { PriorityIcon } from "./priority-icon";
import { StatusIcon } from "./status-icon";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { memberListOptions, agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import { projectListOptions } from "@multica/core/projects/queries";
import { labelListOptions } from "@multica/core/labels/queries";
import { ProjectIcon } from "../../projects/components/project-icon";
import { ActorAvatar } from "../../common/actor-avatar";
// CEREBRO-PATCH(issue-on-behalf-of-filter): MUL-2553 on-behalf-of member filter submenu.
import { OnBehalfOfFilterSub } from "./cerebro-on-behalf-of-filter";
// CEREBRO-PATCH(saved-filters-menu): FIR-1659 personal saved-filters section + save dialog host.
import { SavedFiltersMenuSection, SaveFilterDialogHost } from "@multica/cerebro-saved-filters/views";
import { LabelChip } from "../../labels/label-chip";
import {
  SORT_OPTIONS,
  GROUPING_OPTIONS,
  SWIMLANE_GROUPINGS,
  CARD_PROPERTY_OPTIONS,
  type ActorFilterValue,
  type IssueDateField,
  type IssueDateFilter,
  type IssueDatePreset,
  type SortField,
  type IssueGrouping,
  type SwimlaneGrouping,
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
import { WorkspaceAgentWorkingChip } from "./workspace-agent-working-chip";

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
  // CEREBRO-PATCH(issue-on-behalf-of-filter): MUL-2553 count on-behalf-of filter.
  onBehalfOfFilters: string[];
  dateFilter?: IssueDateFilter | null;
  // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — each stacked date condition counts.
  dateFilters?: IssueDateFilter[];
}) {
  let count = 0;
  if (state.statusFilters.length > 0) count++;
  if (state.priorityFilters.length > 0) count++;
  if (state.assigneeFilters.length > 0 || state.includeNoAssignee) count++;
  if (state.creatorFilters.length > 0) count++;
  if (state.projectFilters.length > 0 || state.includeNoProject) count++;
  if (state.labelFilters.length > 0) count++;
  if ((state.onBehalfOfFilters ?? []).length > 0) count++;
  if (state.dateFilter) count++;
  count += (state.dateFilters ?? []).length;
  return count;
}

function shortDateLabel(dateOnly: string) {
  return formatDateOnly(dateOnly, { month: "short", day: "numeric" }) || dateOnly;
}

function normalizeDateRange(from: Date, to: Date) {
  return from <= to ? [from, to] as const : [to, from] as const;
}

// CEREBRO-PATCH(issue-due-date-filter): FIR-1658 — due_date option in the date-field picker.
const DATE_FIELD_LABEL_KEY: Record<IssueDateField, "date_field_created" | "date_field_updated" | "date_field_due"> = {
  created_at: "date_field_created",
  updated_at: "date_field_updated",
  due_date: "date_field_due",
};

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
          className="w-full bg-transparent text-base md:text-sm placeholder:text-muted-foreground outline-none"
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
          className="w-full bg-transparent text-base md:text-sm placeholder:text-muted-foreground outline-none"
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
// Date sub-menu content
// ---------------------------------------------------------------------------

// CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — stacked date-condition
// builder for My Issues. Maps each preset id to an existing i18n label key.
const DATE_PRESET_LABEL_KEY: Record<
  Exclude<IssueDatePreset, "custom">,
  | "date_today"
  | "date_last_3_days"
  | "date_last_7_days"
  | "date_due_overdue"
  | "date_due_this_week"
  | "date_due_none"
> = {
  today: "date_today",
  last_3_days: "date_last_3_days",
  last_7_days: "date_last_7_days",
  overdue: "date_due_overdue",
  this_week: "date_due_this_week",
  none: "date_due_none",
};

const RELATIVE_PRESETS = ["today", "last_3_days", "last_7_days"] as const;
const DUE_PRESETS = ["overdue", "this_week", "none"] as const;

// Build a concrete IssueDateFilter for a preset. Due-date presets pin
// field=due_date by construction; relative presets honour the chosen field.
function buildDatePreset(
  field: IssueDateField,
  preset: IssueDatePreset,
): IssueDateFilter {
  switch (preset) {
    case "today":
      return { field, from: todayDateOnly(), to: todayDateOnly(), preset };
    case "last_3_days":
      return { field, from: addDaysDateOnly(-2), to: todayDateOnly(), preset };
    case "last_7_days":
      return { field, from: addDaysDateOnly(-6), to: todayDateOnly(), preset };
    case "overdue":
      return { field: "due_date", from: "1970-01-01", to: addDaysDateOnly(-1), preset };
    case "this_week":
      return { field: "due_date", from: todayDateOnly(), to: addDaysDateOnly(6), preset };
    case "none":
      return { field: "due_date", from: todayDateOnly(), to: todayDateOnly(), mode: "none", preset };
    default:
      return { field, from: todayDateOnly(), to: todayDateOnly(), preset: "custom" };
  }
}

// CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — editor for ONE stacked date
// condition (field + value). onChange replaces the condition; onRemove drops it.
function DateConditionEditor({
  value,
  onChange,
  onRemove,
}: {
  value: IssueDateFilter;
  onChange: (next: IssueDateFilter) => void;
  onRemove: () => void;
}) {
  const { t } = useT("issues");
  const [range, setRange] = useState<LocalDateRange | undefined>(() => {
    const from = dateOnlyToLocalDate(value.from);
    if (!from) return undefined;
    return { from, to: dateOnlyToLocalDate(value.to) };
  });

  const changeField = (nextField: IssueDateField) => {
    if (nextField === value.field) return;
    if (nextField === "due_date") {
      // Relative ranges don't translate to due-date semantics — default to This week.
      onChange(buildDatePreset("due_date", "this_week"));
    } else if (value.field === "due_date") {
      // Leaving due_date drops any due-specific preset — default to Last 7 days.
      onChange(buildDatePreset(nextField, "last_7_days"));
    } else {
      // created_at <-> updated_at: keep the same relative range, just swap field.
      onChange({ ...value, field: nextField });
    }
  };

  const applyCustom = () => {
    if (!range?.from) return;
    const [from, to] = normalizeDateRange(range.from, range.to ?? range.from);
    onChange({ field: value.field, from: toDateOnly(from), to: toDateOnly(to), preset: "custom" });
  };

  const valueOptions = value.field === "due_date" ? DUE_PRESETS : RELATIVE_PRESETS;

  return (
    <>
      <DropdownMenuGroup>
        <DropdownMenuLabel>{t(($) => $.filters.date_field)}</DropdownMenuLabel>
        <DropdownMenuRadioGroup
          value={value.field}
          onValueChange={(next) => changeField(next as IssueDateField)}
        >
          {(["created_at", "updated_at", "due_date"] as const).map((option) => (
            <DropdownMenuRadioItem key={option} value={option} closeOnClick={false}>
              {t(($) => $.filters[DATE_FIELD_LABEL_KEY[option]])}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuGroup>

      <DropdownMenuSeparator />
      <DropdownMenuRadioGroup
        value={value.preset && value.preset !== "custom" ? value.preset : ""}
        onValueChange={(preset) => onChange(buildDatePreset(value.field, preset as IssueDatePreset))}
      >
        {valueOptions.map((preset) => (
          <DropdownMenuRadioItem key={preset} value={preset} closeOnClick={false}>
            {t(($) => $.filters[DATE_PRESET_LABEL_KEY[preset]])}
          </DropdownMenuRadioItem>
        ))}
      </DropdownMenuRadioGroup>

      <div className="px-1.5 py-1">
        <Popover>
          <PopoverTrigger
            render={
              <Button
                variant="ghost"
                size="sm"
                className="h-7 w-full justify-start px-1.5 text-sm font-normal aria-expanded:bg-accent"
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

      <DropdownMenuSeparator />
      <DropdownMenuItem closeOnClick={false} onClick={onRemove} className="text-destructive">
        <X className="size-3.5" />
        Remove
      </DropdownMenuItem>
    </>
  );
}

// CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — the stacked builder: a list
// of date conditions (AND), each editable in its own submenu, plus add/clear.
function DateBuilderSubContent({
  value,
  onChange,
}: {
  value: IssueDateFilter[];
  onChange: (next: IssueDateFilter[]) => void;
}) {
  const { t } = useT("issues");

  const valueLabel = (c: IssueDateFilter): string => {
    if (c.preset && c.preset !== "custom") return t(($) => $.filters[DATE_PRESET_LABEL_KEY[c.preset as Exclude<IssueDatePreset, "custom">]]);
    if (c.mode === "none") return t(($) => $.filters.date_due_none);
    const from = formatDateOnly(c.from);
    const to = formatDateOnly(c.to);
    return from === to ? from : `${from} – ${to}`;
  };

  const updateAt = (i: number, next: IssueDateFilter) =>
    onChange(value.map((c, idx) => (idx === i ? next : c)));
  const removeAt = (i: number) => onChange(value.filter((_, idx) => idx !== i));
  const add = () => onChange([...value, buildDatePreset("created_at", "last_7_days")]);

  return (
    <>
      {value.length === 0 && (
        <DropdownMenuLabel className="font-normal text-muted-foreground">
          No date filters yet
        </DropdownMenuLabel>
      )}
      {value.map((c, i) => (
        <Fragment key={i}>
          {i > 0 && (
            <div className="px-1.5 py-0.5 text-[0.7rem] font-medium tracking-wide text-muted-foreground">
              AND
            </div>
          )}
          <DropdownMenuSub>
            <DropdownMenuSubTrigger>
              <CalendarDays className="size-3.5" />
              <span className="flex-1 truncate">
                {t(($) => $.filters[DATE_FIELD_LABEL_KEY[c.field]])}
              </span>
              <span className="max-w-28 truncate text-xs font-medium text-primary">
                {valueLabel(c)}
              </span>
            </DropdownMenuSubTrigger>
            <DropdownMenuSubContent className="w-56">
              <DateConditionEditor
                value={c}
                onChange={(next) => updateAt(i, next)}
                onRemove={() => removeAt(i)}
              />
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        </Fragment>
      ))}

      <DropdownMenuSeparator />
      <DropdownMenuItem closeOnClick={false} onClick={add}>
        <Plus className="size-3.5" />
        Add date filter
      </DropdownMenuItem>
      {value.length > 0 && (
        <DropdownMenuItem closeOnClick={false} onClick={() => onChange([])} className="text-muted-foreground">
          <X className="size-3.5" />
          Clear all dates
        </DropdownMenuItem>
      )}
    </>
  );
}

function DateSubContent({
  value,
  onChange,
  // CEREBRO-PATCH(my-issues-due-date-presets): FIR-1658 — due-date quick presets.
  dueDatePresets = false,
}: {
  value: IssueDateFilter | null;
  onChange: (filter: IssueDateFilter | null) => void;
  dueDatePresets?: boolean;
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

  // CEREBRO-PATCH(my-issues-due-date-presets): FIR-1658 — due-date quick filters.
  // Each forces field=due_date. Overdue = before today; This week = today..+6d;
  // No due date = the special "none" mode (issues with an empty due_date).
  const applyDuePreset = (preset: "overdue" | "this_week" | "none") => {
    setField("due_date");
    setRange(undefined);
    if (preset === "overdue") {
      onChange({ field: "due_date", from: "1970-01-01", to: addDaysDateOnly(-1) });
    } else if (preset === "this_week") {
      onChange({ field: "due_date", from: todayDateOnly(), to: addDaysDateOnly(6) });
    } else {
      onChange({ field: "due_date", from: todayDateOnly(), to: todayDateOnly(), mode: "none" });
    }
  };

  return (
    <>
      <DropdownMenuGroup>
        <DropdownMenuLabel>{t(($) => $.filters.date_field)}</DropdownMenuLabel>
        <DropdownMenuRadioGroup value={field} onValueChange={(next) => setFieldValue(next as IssueDateField)}>
          {/* CEREBRO-PATCH(issue-due-date-filter): FIR-1658 — offer due_date alongside created/updated. */}
          {(["created_at", "updated_at", "due_date"] as const).map((option) => (
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

      {/* CEREBRO-PATCH(my-issues-due-date-presets): FIR-1658 — due-date quick presets. */}
      {dueDatePresets && (
        <>
          <DropdownMenuSeparator />
          {/* CEREBRO-PATCH(my-issues-due-date-presets): GroupLabel must live inside a Group or Base UI throws (error #31). */}
          <DropdownMenuGroup>
            <DropdownMenuLabel>{t(($) => $.filters.date_due_section)}</DropdownMenuLabel>
            <DropdownMenuItem onClick={() => applyDuePreset("overdue")}>
              {t(($) => $.filters.date_due_overdue)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => applyDuePreset("this_week")}>
              {t(($) => $.filters.date_due_this_week)}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => applyDuePreset("none")}>
              {t(($) => $.filters.date_due_none)}
            </DropdownMenuItem>
          </DropdownMenuGroup>
        </>
      )}

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
// IssuesHeader
// ---------------------------------------------------------------------------

export function IssuesHeader({
  scopedIssues,
  referenceFilterControl,
  allowGantt = false,
  dateFilter = null,
  onDateFilterChange,
  // CEREBRO-PATCH(boards-date-filter): FIR-1724 forward due-date presets so boards match My Issues.
  dueDatePresets = false,
}: {
  scopedIssues: Issue[];
  referenceFilterControl?: ReactNode;
  /** Whether the Gantt view option is offered (project surface only). */
  allowGantt?: boolean;
  dateFilter?: IssueDateFilter | null;
  onDateFilterChange?: (filter: IssueDateFilter | null) => void;
  // CEREBRO-PATCH(boards-date-filter): FIR-1724 offer the My-Issues due-date presets on boards.
  dueDatePresets?: boolean;
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
  // Scope the chip to whatever issues this page is currently showing.
  // /issues uses the full workspace minus Members/Agents pill filtering;
  // passing the visible-issue id set lets the chip count match the list
  // length when the filter is on.
  const scopedIssueIds = useMemo(
    () => new Set(scopedIssues.map((i) => i.id)),
    [scopedIssues],
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
          {referenceFilterControl}
          {agentRunningFilter && (
            <span className="mr-1 hidden text-xs text-muted-foreground md:inline">
              {t(($) => $.agent_activity.filter_active_label)}
            </span>
          )}
          <WorkspaceAgentWorkingChip
            value={agentRunningFilter}
            onToggle={toggleAgentRunningFilter}
            scopedIssueIds={scopedIssueIds}
          />
          <IssueDisplayControls
            scopedIssues={scopedIssues}
            allowGantt={allowGantt}
            dateFilter={dateFilter}
            onDateFilterChange={onDateFilterChange}
            // CEREBRO-PATCH(boards-date-filter): FIR-1724 pass presets through to the date filter control.
            dueDatePresets={dueDatePresets}
          />
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
  dueDatePresets = false,
  // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — stacked date builder.
  dateFilters,
  onDateFiltersChange,
}: {
  scopedIssues: Issue[];
  /** Hide the board/list/swimlane/gantt view toggle entirely. */
  hideViewToggle?: boolean;
  dateFilter?: IssueDateFilter | null;
  onDateFilterChange?: (filter: IssueDateFilter | null) => void;
  // CEREBRO-PATCH(my-issues-due-date-presets): FIR-1658 — show Overdue/This week/No
  // due date quick presets in the date submenu (client-side My Issues filter only).
  dueDatePresets?: boolean;
  // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — when provided (My Issues),
  // the Date submenu renders the stacked multi-condition builder instead of the
  // single-range picker. The server-backed /issues page keeps onDateFilterChange.
  dateFilters?: IssueDateFilter[];
  onDateFiltersChange?: (filters: IssueDateFilter[]) => void;
  /** Whether the Gantt view option is offered (project surface only). */
  // Only Project Detail renders <GanttView>; other surfaces (global /issues,
  // /my-issues, actor panel) ignore viewMode === "gantt" and would silently
  // fall back to List if the option were exposed there. Keep Gantt opt-in.
  allowGantt?: boolean;
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
  // CEREBRO-PATCH(issue-on-behalf-of-filter): MUL-2553 read on-behalf-of filter state.
  const onBehalfOfFilters = useViewStore((s) => s.onBehalfOfFilters);
  const sortBy = useViewStore((s) => s.sortBy);
  const sortDirection = useViewStore((s) => s.sortDirection);
  const grouping = useViewStore((s) => s.grouping);
  const swimlaneGrouping = useViewStore((s) => s.swimlaneGrouping);
  const cardProperties = useViewStore((s) => s.cardProperties);
  const subIssueDisplay = useViewStore((s) => s.subIssueDisplay);
  const act = useViewStoreApi().getState();

  const counts = useIssueCounts(scopedIssues);
  const showDateFilter = !!onDateFilterChange;
  // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — My Issues passes the
  // stacked builder handler; the global page passes the single-range handler.
  const showDateBuilder = !!onDateFiltersChange;
  const activeDateFilters = dateFilters ?? [];

  const activeFilterCount = getActiveFilterCount({
    statusFilters,
    priorityFilters,
    assigneeFilters,
    includeNoAssignee,
    creatorFilters,
    projectFilters,
    includeNoProject,
    labelFilters,
    onBehalfOfFilters,
    dateFilter: showDateFilter ? dateFilter : null,
    dateFilters: showDateBuilder ? activeDateFilters : [],
  });
  const hasActiveFilters = activeFilterCount > 0;

  const SORT_LABEL_KEY: Record<typeof SORT_OPTIONS[number]["value"], "sort_manual" | "sort_priority" | "sort_start_date" | "sort_due_date" | "sort_created" | "sort_title"> = {
    position: "sort_manual",
    priority: "sort_priority",
    start_date: "sort_start_date",
    due_date: "sort_due_date",
    created_at: "sort_created",
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
        // CEREBRO-PATCH(my-issues-due-date-presets): FIR-1658 — "no due date" label.
        dateFilter.mode === "none"
          ? t(($) => $.filters.date_due_none)
          : dateFilter.from === dateFilter.to
            ? shortDateLabel(dateFilter.from)
            : `${shortDateLabel(dateFilter.from)} - ${shortDateLabel(dateFilter.to)}`
      }`
    : null;
  const sortLabel = t(($) => $.display[SORT_LABEL_KEY[sortBy]]);
  const groupingLabel = t(($) => $.display[GROUPING_LABEL_KEY[grouping]]);
  const swimlaneGroupingLabel = t(($) => $.display[SWIMLANE_GROUPING_LABEL_KEY[swimlaneGrouping]]);
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
                            onDateFiltersChange?.([]);
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
            {/* CEREBRO-PATCH(saved-filters-menu): FIR-1659 saved filters at top of the filter menu. */}
            <SavedFiltersMenuSection />
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

            {/* CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — My Issues gets the
                stacked multi-condition builder; the server-backed /issues page
                keeps the single-range picker. */}
            {showDateBuilder && onDateFiltersChange ? (
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <CalendarDays className="size-3.5" />
                  <span className="flex-1">{t(($) => $.filters.section_date)}</span>
                  {activeDateFilters.length > 0 && (
                    <span className="text-xs font-medium text-primary">
                      {activeDateFilters.length}
                    </span>
                  )}
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="w-64">
                  <DateBuilderSubContent
                    value={activeDateFilters}
                    onChange={onDateFiltersChange}
                  />
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            ) : showDateFilter && onDateFilterChange ? (
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
                    dueDatePresets={dueDatePresets}
                  />
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            ) : null}

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

            {/* CEREBRO-PATCH(issue-on-behalf-of-filter): MUL-2553 on-behalf-of member filter */}
            <OnBehalfOfFilterSub selected={onBehalfOfFilters} onToggle={act.toggleOnBehalfOfFilter} />

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
                <DropdownMenuItem
                  onClick={() => {
                    act.clearFilters();
                    onDateFilterChange?.(null);
                    onDateFiltersChange?.([]);
                  }}
                >
                  {t(($) => $.filters.reset)}
                </DropdownMenuItem>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
        {/* CEREBRO-PATCH(saved-filters-menu): FIR-1659 naming dialog lives outside the dropdown so it survives the menu closing. */}
        <SaveFilterDialogHost />

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
                          {t(($) => $.display[SORT_LABEL_KEY[opt.value]])}
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

            <div className="border-b px-3 py-2.5">
              <span className="text-xs font-medium text-muted-foreground">
                Sub-issues
              </span>
              <div className="mt-2 flex gap-1">
                {([
                  { value: "standalone" as const, label: "Separate" },
                  { value: "on-parent" as const, label: "On parent" },
                  { value: "hidden" as const, label: "Hidden" },
                ]).map((opt) => (
                  <Button
                    key={opt.value}
                    variant="outline"
                    size="sm"
                    className={`flex-1 text-xs ${
                      subIssueDisplay === opt.value
                        ? "bg-accent text-accent-foreground border-accent"
                        : ""
                    }`}
                    onClick={() => act.setSubIssueDisplay(opt.value)}
                  >
                    {opt.label}
                  </Button>
                ))}
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
