"use client";

import { useQuery } from "@tanstack/react-query";
import {
  AtSign,
  Bell,
  Bot,
  CalendarClock,
  CircleDot,
  Clock,
  Eye,
  FolderKanban,
  MessageSquare,
  PencilLine,
  Settings,
  SignalHigh,
  Tag,
  User,
  UserPlus,
  UserRound,
  X,
  type IconComponent,
} from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { FILTER_NONE_VALUE } from "@multica/core/issues/stores/filter-tree";
import {
  activeInboxFilterKeys,
  inboxActorKeyParts,
  useInboxFilterStore,
  type InboxFilterKey,
  type InboxFilters,
  type InboxReceivedFilter,
  type InboxTypeGroup,
} from "@multica/core/inbox/filter-store";
import { useActorName } from "@multica/core/workspace/hooks";
import { projectListOptions } from "@multica/core/projects/queries";
import { labelListOptions } from "@multica/core/labels/queries";
import { ChipSpan, summarize, type FilterChip } from "../../issues/components/filter-chips-bar";
import { DUE_DATE_LABEL_KEY } from "../../issues/components/filter-field-options";
import { useStatusOptions } from "../../issues/utils/status-options";
import { useT } from "../../i18n";

export const INBOX_FILTER_ICONS: Record<InboxFilterKey, IconComponent> = {
  unread: Eye,
  type: Bell,
  actor: UserRound,
  received: Clock,
  status: CircleDot,
  priority: SignalHigh,
  project: FolderKanban,
  assignee: User,
  label: Tag,
  dueDate: CalendarClock,
};

export const INBOX_TYPE_ICONS: Record<InboxTypeGroup, IconComponent> = {
  mention: AtSign,
  comment: MessageSquare,
  assign: UserPlus,
  review: Eye,
  change: PencilLine,
  run: Bot,
  system: Settings,
};

/** Display names for every dimension and the values that have no directory. */
export function useInboxFilterLabels() {
  const { t } = useT("inbox");
  const { t: tIssues } = useT("issues");
  const dimension: Record<InboxFilterKey, string> = {
    unread: tIssues(($) => $.filters.section_read_state),
    type: t(($) => $.filters.type),
    actor: t(($) => $.filters.from),
    received: t(($) => $.filters.received),
    status: tIssues(($) => $.filters.section_status),
    priority: tIssues(($) => $.filters.section_priority),
    project: tIssues(($) => $.filters.section_project),
    assignee: tIssues(($) => $.filters.section_assignee),
    label: tIssues(($) => $.filters.section_label),
    dueDate: tIssues(($) => $.filters.section_due_date),
  };
  const type: Record<InboxTypeGroup, string> = {
    mention: t(($) => $.filters.type_mention),
    comment: t(($) => $.filters.type_comment),
    assign: t(($) => $.filters.type_assign),
    review: t(($) => $.filters.type_review),
    change: t(($) => $.filters.type_change),
    run: t(($) => $.filters.type_run),
    system: t(($) => $.filters.type_system),
  };
  const received: Record<InboxReceivedFilter, string> = {
    today: t(($) => $.filters.received_today),
    last_7_days: t(($) => $.filters.received_last_7_days),
    last_30_days: t(($) => $.filters.received_last_30_days),
  };
  return {
    dimension,
    type,
    received,
    unread: tIssues(($) => $.filters.read_state_unread),
    noProject: tIssues(($) => $.filters.no_project),
    noAssignee: tIssues(($) => $.filters.no_assignee),
    dueDate: (bucket: keyof typeof DUE_DATE_LABEL_KEY) => tIssues(($) => $.filters[DUE_DATE_LABEL_KEY[bucket]]),
  };
}

const CHIP_ICON_CLASS = "size-3 shrink-0 text-muted-foreground";

/** One chip per active dimension, in menu order; each clears its dimension. */
export function useInboxFilterChips(wsId: string, filters: InboxFilters): FilterChip[] {
  const labels = useInboxFilterLabels();
  const clearFilter = useInboxFilterStore((state) => state.clearFilter);
  const { getActorName } = useActorName();
  const statusOptions = useStatusOptions(wsId, filters.statuses);
  const { t: tIssues } = useT("issues");
  const keys = activeInboxFilterKeys(filters);
  const { data: projects = [] } = useQuery({ ...projectListOptions(wsId), enabled: keys.includes("project") });
  const { data: workspaceLabels = [] } = useQuery({ ...labelListOptions(wsId), enabled: keys.includes("label") });

  const actorName = (key: string) => {
    if (key === FILTER_NONE_VALUE) return labels.noAssignee;
    const { type, id } = inboxActorKeyParts(key);
    return getActorName(type, id);
  };
  const values = (key: InboxFilterKey): string[] => {
    switch (key) {
      case "unread":
        return [labels.unread];
      case "type":
        return filters.types.map((type) => labels.type[type]);
      case "actor":
        return filters.actors.map(actorName);
      case "received":
        return filters.received ? [labels.received[filters.received]] : [];
      case "status":
        return filters.statuses.map(
          (status) => statusOptions.find((option) => option.key === status)?.label ?? status,
        );
      case "priority":
        return filters.priorities.map((priority) => tIssues(($) => $.priority[priority]));
      case "project":
        return filters.projects.map((id) =>
          id === FILTER_NONE_VALUE ? labels.noProject : projects.find((p) => p.id === id)?.title ?? "",
        );
      case "assignee":
        return filters.assignees.map(actorName);
      case "label":
        return filters.labels.map((id) => workspaceLabels.find((l) => l.id === id)?.name ?? "");
      case "dueDate":
        return filters.dueDates.map(labels.dueDate);
    }
  };

  return keys.map((key) => {
    const Icon = INBOX_FILTER_ICONS[key];
    return {
      key,
      icon: <Icon className={CHIP_ICON_CLASS} />,
      label: labels.dimension[key],
      value: summarize(values(key)),
      negated: filters.negated.includes(key as never),
      onRemove: () => clearFilter(wsId, key),
    };
  });
}

/**
 * The active filters under the list header, compact enough for the list
 * column: icon and value only, the field name on hover.
 */
export function InboxFilterChipsBar({ wsId, filters }: { wsId: string; filters: InboxFilters }) {
  const { t } = useT("inbox");
  const chips = useInboxFilterChips(wsId, filters);
  const clearFilters = useInboxFilterStore((state) => state.clearFilters);
  if (chips.length === 0) return null;
  return (
    <div
      data-testid="inbox-filter-chips"
      className="mx-3 mb-2 flex shrink-0 flex-wrap items-center gap-1.5 rounded-lg bg-muted/50 px-1.5 py-1.5"
    >
      {chips.map((chip) => (
        <ChipSpan key={chip.key} chip={chip} compact />
      ))}
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label={t(($) => $.filters.clear)}
        title={t(($) => $.filters.clear)}
        className="ml-auto text-muted-foreground"
        onClick={() => clearFilters(wsId)}
      >
        <X className="size-3" />
      </Button>
    </div>
  );
}
