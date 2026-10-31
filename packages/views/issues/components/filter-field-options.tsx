"use client";

import { useMemo, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Bot,
  CalendarClock,
  CircleDashed,
  CircleDot,
  CircleUser,
  FolderKanban,
  FolderMinus,
  SignalHigh,
  Tag,
  User,
  UserMinus,
  UserPen,
  type IconComponent,
} from "@multica/ui/icons";
import { useWorkspaceId } from "@multica/core/hooks";
import { PRIORITY_DISPLAY_ORDER } from "@multica/core/issues/config";
import { PROJECT_STATUS_CONFIG, PROJECT_STATUS_ORDER } from "@multica/core/projects/config";
import { projectListOptions } from "@multica/core/projects/queries";
import { labelListOptions } from "@multica/core/labels/queries";
import { memberListOptions, agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import {
  DUE_DATE_FILTERS,
  FILTER_NONE_VALUE,
  ISSUE_RELATION_FILTERS,
  type DueDateFilter,
  type IssueRelationFilter,
  type NegatableFilterDimension,
} from "@multica/core/issues/stores/filter-tree";
import type { ActorFilterValue } from "@multica/core/issues/stores/view-store";
import { useT } from "../../i18n";
import { useStatusOptions } from "../utils/status-options";
import { matchesPinyin } from "../../editor/extensions/pinyin-match";
import { useProjectStatusLabels } from "../../projects/components/labels";
import { ActorAvatar } from "../../common/actor-avatar";
import { ProjectIcon } from "../../projects/components/project-icon";
import { StatusIcon } from "./status-icon";
import { PriorityIcon } from "./priority-icon";

export const FILTER_FIELD_ICONS: Record<NegatableFilterDimension, IconComponent> = {
  status: CircleDot,
  priority: SignalHigh,
  assignee: User,
  creator: UserPen,
  project: FolderKanban,
  projectStatus: CircleDashed,
  label: Tag,
  relation: CircleUser,
  dueDate: CalendarClock,
};

const RELATION_ICONS: Record<IssueRelationFilter, IconComponent> = {
  assigned: CircleUser,
  created: UserPen,
  my_agents: Bot,
};

export const DUE_DATE_LABEL_KEY: Record<
  DueDateFilter,
  "due_overdue" | "due_today" | "due_this_week" | "due_next_7_days" | "due_none"
> = {
  overdue: "due_overdue",
  today: "due_today",
  this_week: "due_this_week",
  next_7_days: "due_next_7_days",
  none: "due_none",
};

export const RELATION_LABEL_KEY: Record<
  IssueRelationFilter,
  "relation_assigned" | "relation_created" | "relation_my_agents"
> = {
  assigned: "relation_assigned",
  created: "relation_created",
  my_agents: "relation_my_agents",
};

export function useFilterFieldLabels(): Record<NegatableFilterDimension, string> {
  const { t } = useT("issues");
  return {
    status: t(($) => $.filters.section_status),
    priority: t(($) => $.filters.section_priority),
    assignee: t(($) => $.filters.section_assignee),
    creator: t(($) => $.filters.section_creator),
    project: t(($) => $.filters.section_project),
    projectStatus: t(($) => $.filters.section_project_status),
    label: t(($) => $.filters.section_label),
    relation: t(($) => $.filters.section_relation),
    dueDate: t(($) => $.filters.section_due_date),
  };
}

/** One value a condition on a field can take, in condition-value form. */
export interface FilterValueOption {
  value: string;
  label: string;
  icon: ReactNode;
  /** Heading the option sits under (members / agents / squads). */
  group?: string;
}

/** Case-insensitive substring or pinyin-initials match. */
export function matchesFilterQuery(text: string, query: string): boolean {
  if (!query) return true;
  return text.toLowerCase().includes(query) || matchesPinyin(text, query);
}

/**
 * Every value each field can be filtered on, for the menu's search and the
 * advanced panel. Directories load only while `enabled`; until then an
 * actor / project / label field simply lists nothing.
 */
export function useFilterValueCatalog(
  enabled: boolean,
): (field: NegatableFilterDimension) => FilterValueOption[] {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const statusOptions = useStatusOptions(wsId);
  const projectStatusLabels = useProjectStatusLabels();
  const { data: members = [] } = useQuery({ ...memberListOptions(wsId), enabled });
  const { data: agents = [] } = useQuery({ ...agentListOptions(wsId), enabled });
  const { data: squads = [] } = useQuery({ ...squadListOptions(wsId), enabled });
  const { data: projects = [] } = useQuery({ ...projectListOptions(wsId), enabled });
  const { data: labels = [] } = useQuery({ ...labelListOptions(wsId), enabled });
  const membersGroup = t(($) => $.filters.members_group);
  const agentsGroup = t(($) => $.filters.agents_group);
  const squadsGroup = t(($) => $.filters.squads_group);
  const noAssignee = t(($) => $.filters.no_assignee);
  const noProject = t(($) => $.filters.no_project);
  const priorityLabels = Object.fromEntries(
    PRIORITY_DISPLAY_ORDER.map((p) => [p, t(($) => $.priority[p])]),
  );
  const relationLabels = Object.fromEntries(
    ISSUE_RELATION_FILTERS.map((r) => [r, t(($) => $.filters[RELATION_LABEL_KEY[r]])]),
  );
  const dueDateLabels = Object.fromEntries(
    DUE_DATE_FILTERS.map((d) => [d, t(($) => $.filters[DUE_DATE_LABEL_KEY[d]])]),
  );
  // Labels are fresh strings per render; key the memo on their content.
  const labelKey = JSON.stringify([
    priorityLabels, relationLabels, dueDateLabels, projectStatusLabels,
    membersGroup, agentsGroup, squadsGroup, noAssignee, noProject,
  ]);

  return useMemo(() => {
    const actorOptions = (withSquads: boolean): FilterValueOption[] => [
      ...members.map((m) => ({
        value: `member:${m.user_id}`,
        label: m.name,
        group: membersGroup,
        icon: <ActorAvatar actorType="member" actorId={m.user_id} size="xs" profileLink={false} />,
      })),
      ...agents
        .filter((a) => !a.archived_at)
        .map((a) => ({
          value: `agent:${a.id}`,
          label: a.name,
          group: agentsGroup,
          icon: <ActorAvatar actorType="agent" actorId={a.id} size="xs" profileLink={false} />,
        })),
      ...(withSquads
        ? squads
            .filter((s) => !s.archived_at)
            .map((s) => ({
              value: `squad:${s.id}`,
              label: s.name,
              group: squadsGroup,
              icon: <ActorAvatar actorType="squad" actorId={s.id} size="xs" profileLink={false} />,
            }))
        : []),
    ];
    const byField: Record<NegatableFilterDimension, FilterValueOption[]> = {
      status: statusOptions.map((option) => ({
        value: option.key,
        label: option.label,
        icon: (
          <StatusIcon
            status={option.key}
            category={option.category}
            color={option.color}
            icon={option.icon}
            className="size-3.5"
          />
        ),
      })),
      priority: PRIORITY_DISPLAY_ORDER.map((p) => ({
        value: p,
        label: priorityLabels[p] ?? p,
        icon: <PriorityIcon priority={p} />,
      })),
      assignee: [
        {
          value: FILTER_NONE_VALUE,
          label: noAssignee,
          icon: <UserMinus className="size-3.5 text-muted-foreground" />,
        },
        ...actorOptions(true),
      ],
      creator: actorOptions(false),
      project: [
        {
          value: FILTER_NONE_VALUE,
          label: noProject,
          icon: <FolderMinus className="size-3.5 text-muted-foreground" />,
        },
        ...projects.map((p) => ({
          value: p.id,
          label: p.title,
          icon: <ProjectIcon project={p} size="sm" />,
        })),
      ],
      projectStatus: PROJECT_STATUS_ORDER.map((status) => ({
        value: status,
        label: projectStatusLabels[status],
        icon: <span className={`size-2 rounded-full ${PROJECT_STATUS_CONFIG[status].dotColor}`} />,
      })),
      label: labels.map((l) => ({
        value: l.id,
        label: l.name,
        icon: <span className="size-2 rounded-full" style={{ backgroundColor: l.color }} />,
      })),
      relation: ISSUE_RELATION_FILTERS.map((relation) => {
        const Icon = RELATION_ICONS[relation];
        return {
          value: relation,
          label: relationLabels[relation] ?? relation,
          icon: <Icon className="size-3.5 text-muted-foreground" />,
        };
      }),
      dueDate: DUE_DATE_FILTERS.map((bucket) => ({
        value: bucket,
        label: dueDateLabels[bucket] ?? bucket,
        icon: <CalendarClock className="size-3.5 text-muted-foreground" />,
      })),
    };
    return (field: NegatableFilterDimension) => byField[field];
    // eslint-disable-next-line react-hooks/exhaustive-deps -- translated labels are keyed by content
  }, [agents, labelKey, labels, members, projects, squads, statusOptions]);
}

function parseActorValue(value: string): ActorFilterValue | null {
  const separator = value.indexOf(":");
  if (separator <= 0) return null;
  const type = value.slice(0, separator);
  if (type !== "member" && type !== "agent" && type !== "squad") return null;
  return { type, id: value.slice(separator + 1) };
}

/** The simple-mode store actions the menu search needs. */
export interface SimpleFilterToggles {
  toggleStatusFilter: (status: string) => void;
  togglePriorityFilter: (priority: (typeof PRIORITY_DISPLAY_ORDER)[number]) => void;
  toggleAssigneeFilter: (value: ActorFilterValue) => void;
  toggleNoAssignee: () => void;
  toggleCreatorFilter: (value: ActorFilterValue) => void;
  toggleProjectFilter: (projectId: string) => void;
  toggleNoProject: () => void;
  toggleProjectStatusFilter: (status: (typeof PROJECT_STATUS_ORDER)[number]) => void;
  toggleLabelFilter: (labelId: string) => void;
  toggleRelationFilter: (relation: IssueRelationFilter) => void;
  toggleDueDateFilter: (bucket: DueDateFilter) => void;
}

/** Toggle one value of a simple-mode dimension, given in condition form. */
export function toggleSimpleFilterValue(
  act: SimpleFilterToggles,
  field: NegatableFilterDimension,
  value: string,
) {
  switch (field) {
    case "status":
      return act.toggleStatusFilter(value);
    case "priority":
      return act.togglePriorityFilter(value as (typeof PRIORITY_DISPLAY_ORDER)[number]);
    case "assignee": {
      if (value === FILTER_NONE_VALUE) return act.toggleNoAssignee();
      const actor = parseActorValue(value);
      return actor ? act.toggleAssigneeFilter(actor) : undefined;
    }
    case "creator": {
      const actor = parseActorValue(value);
      return actor ? act.toggleCreatorFilter(actor) : undefined;
    }
    case "project":
      return value === FILTER_NONE_VALUE ? act.toggleNoProject() : act.toggleProjectFilter(value);
    case "projectStatus":
      return act.toggleProjectStatusFilter(value as (typeof PROJECT_STATUS_ORDER)[number]);
    case "label":
      return act.toggleLabelFilter(value);
    case "relation":
      return act.toggleRelationFilter(value as IssueRelationFilter);
    case "dueDate":
      return act.toggleDueDateFilter(value as DueDateFilter);
  }
}
