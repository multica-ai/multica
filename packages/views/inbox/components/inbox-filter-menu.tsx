"use client";

import { useEffect, useMemo, useState, type ReactNode } from "react";
import { Filter, RotateCcw, Search } from "@multica/ui/icons";
import { PRIORITY_DISPLAY_ORDER } from "@multica/core/issues/config";
import { DUE_DATE_FILTERS, FILTER_NONE_VALUE } from "@multica/core/issues/stores/filter-tree";
import {
  INBOX_NOTIFICATION_FILTER_KEYS,
  INBOX_RECEIVED_FILTERS,
  INBOX_TYPE_GROUPS,
  activeInboxFilterKeys,
  effectiveInboxFilters,
  inboxActorKey,
  inboxActorKeyParts,
  inboxFacetCounts,
  inboxFilterCount,
  inboxFilterValues,
  type InboxFilterKey,
  type InboxFilterSupport,
  type InboxListDimension,
  useInboxFilters,
  useInboxFilterStore,
} from "@multica/core/inbox/filter-store";
import { useQuery } from "@tanstack/react-query";
import { archivedInboxFacetsOptions } from "@multica/core/inbox/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import type { InboxItem } from "@multica/core/types";
import { ActorAvatar } from "@multica/ui/components/common/actor-avatar";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuCheckboxItem,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { FILTER_ITEM_CLASS, HoverCheck } from "../../common/hover-check";
import { PriorityIcon } from "../../issues/components/priority-icon";
import { StatusIcon } from "../../issues/components/status-icon";
import { FilterOperator } from "../../issues/components/filter-chips-bar";
import { matchesFilterQuery, useFilterValueCatalog } from "../../issues/components/filter-field-options";
import { stopMenuSearchKeys } from "../../issues/components/advanced-filter-panel";
import {
  ActorSubContent,
  FilterOperatorToggle,
  LabelSubContent,
  ProjectSubContent,
} from "../../issues/components/issues-header";
import { useStatusOptions } from "../../issues/utils/status-options";
import { useT } from "../../i18n";
import {
  INBOX_FILTER_ICONS,
  INBOX_TYPE_ICONS,
  useInboxFilterChips,
  useInboxFilterLabels,
} from "./inbox-filter-options";

const NO_COUNTS = new Map<string, number>();

const NOTIFICATION_KEYS = INBOX_NOTIFICATION_FILTER_KEYS;
const ISSUE_KEYS: readonly InboxFilterKey[] = [
  "status",
  "priority",
  "project",
  "assignee",
  "label",
  "dueDate",
];

function toMap(record: Record<string, number> | undefined): Map<string, number> {
  return new Map(Object.entries(record ?? {}));
}

/** A value row: hover checkbox, icon, label, and its faceted count. */
function ValueItem({
  checked,
  onToggle,
  icon,
  label,
  count,
}: {
  checked: boolean;
  onToggle: () => void;
  icon: ReactNode;
  label: string;
  count?: number;
}) {
  const { t } = useT("inbox");
  return (
    <DropdownMenuCheckboxItem checked={checked} onCheckedChange={onToggle} className={FILTER_ITEM_CLASS}>
      <HoverCheck checked={checked} />
      {icon}
      <span className="truncate">{label}</span>
      {count != null && count > 0 && (
        <span className="ml-auto pl-3 text-caption text-muted-foreground">
          {t(($) => $.filters.notification_count, { count })}
        </span>
      )}
    </DropdownMenuCheckboxItem>
  );
}

/**
 * The inbox filter, shaped like the issue filter: search, the dimensions in
 * use pinned on top, "is" / "is not" per field, and each field's current
 * value on its row.
 *
 * The main Inbox is narrowed by the issue only: its sections already say what
 * kind of notification a row is and whether it is read. The archive is one
 * flat list, so it also offers the notification fields.
 *
 * `items` are the view's notifications, every one rather than one per issue:
 * the notification fields match any notification of an issue.
 */
export function InboxFilterMenu({
  wsId,
  items,
  priorityFilterSupport,
  issueFieldsSupport = "supported",
  archived = false,
}: {
  wsId: string;
  items: InboxItem[];
  priorityFilterSupport: InboxFilterSupport;
  issueFieldsSupport?: InboxFilterSupport;
  archived?: boolean;
}) {
  const { t } = useT("inbox");
  const { t: tIssues } = useT("issues");
  const labels = useInboxFilterLabels();
  const filters = useInboxFilters(wsId);
  // Actions are stable; handlers read them without subscribing to the store.
  const store = useInboxFilterStore.getState();
  const clearPriorityFilters = useInboxFilterStore((state) => state.clearPriorityFilters);
  const clearIssueFieldFilters = useInboxFilterStore((state) => state.clearIssueFieldFilters);
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [activeAtOpen, setActiveAtOpen] = useState<ReadonlySet<InboxFilterKey> | null>(null);
  const query = search.trim().toLowerCase();
  const { getActorName, getActorInitials, getActorAvatarUrl } = useActorName();

  const effectiveFilters = useMemo(
    () => effectiveInboxFilters(filters, { priority: priorityFilterSupport, issueFields: issueFieldsSupport, archived }),
    [filters, priorityFilterSupport, issueFieldsSupport, archived],
  );
  const facetsQuery = useQuery({ ...archivedInboxFacetsOptions(wsId, effectiveFilters), enabled: archived && open });
  const facets = archived ? facetsQuery.data : undefined;
  const activeCount = inboxFilterCount(effectiveFilters);
  const chips = useInboxFilterChips(wsId, effectiveFilters);
  const chipByKey = new Map(chips.map((chip) => [chip.key, chip]));

  const issueFieldsSupported = issueFieldsSupport === "supported";
  // An older archive server ignores type, received and "is not"; offering
  // them there would answer a different question than the one asked.
  const serverOnlySupported = !archived || issueFieldsSupported;
  const available = (key: InboxFilterKey) => {
    switch (key) {
      case "priority":
        return priorityFilterSupport === "supported";
      case "unread":
        return archived;
      case "actor":
        return archived && actorOptions.length > 0;
      case "project":
      case "assignee":
      case "label":
      case "dueDate":
        return issueFieldsSupported;
      case "type":
      case "received":
        return archived && serverOnlySupported;
      default:
        return true;
    }
  };

  // A workspace keeps its filters while its backend changes (Desktop server
  // switch, self-hosted downgrade, rolling deployment). Drop a selection only
  // once incompatibility is confirmed; "unknown" keeps it dormant.
  const hasPriorities = filters.priorities.length > 0;
  const hasIssueFields = filters.projects.length + filters.assignees.length + filters.labels.length + filters.dueDates.length > 0;
  useEffect(() => {
    if (priorityFilterSupport === "unsupported" && hasPriorities) clearPriorityFilters(wsId);
  }, [clearPriorityFilters, hasPriorities, priorityFilterSupport, wsId]);
  useEffect(() => {
    if (issueFieldsSupport === "unsupported" && hasIssueFields) clearIssueFieldFilters(wsId);
  }, [clearIssueFieldFilters, hasIssueFields, issueFieldsSupport, wsId]);

  // Counts are faceted: each respects every other active dimension while
  // ignoring its own, so it says how many rows selecting that value shows.
  const localCounts = useMemo(
    () => (archived || !open ? null : inboxFacetCounts(items, effectiveFilters)),
    [archived, open, items, effectiveFilters],
  );
  const counts = {
    types: archived ? toMap(facets?.types) : localCounts?.types ?? NO_COUNTS,
    actors: archived ? toMap(facets?.actors) : localCounts?.actors ?? NO_COUNTS,
    statuses: archived ? toMap(facets?.statuses) : localCounts?.statuses ?? NO_COUNTS,
    priorities: archived ? toMap(facets?.priorities) : localCounts?.priorities ?? NO_COUNTS,
    projects: archived ? toMap(facets?.projects) : localCounts?.projects ?? NO_COUNTS,
    assignees: archived ? toMap(facets?.assignees) : localCounts?.assignees ?? NO_COUNTS,
    labels: archived ? toMap(facets?.labels) : localCounts?.labels ?? NO_COUNTS,
    unread: archived ? facets?.unreadCount ?? 0 : localCounts?.unread ?? 0,
  };

  const inboxStatusKeys = useMemo(
    () => [
      ...new Set([
        ...filters.statuses,
        ...Object.keys(facets?.statuses ?? {}),
        ...items.flatMap((item) => (item.issue_status == null ? [] : [item.issue_status])),
      ]),
    ],
    [items, facets, filters.statuses],
  );
  const statusOptions = useStatusOptions(wsId, inboxStatusKeys);
  // The sender list comes from every notification in the view rather than
  // the faceted subset: picking one sender must not remove the others from
  // the menu that offers them. Sorted by name so it does not reshuffle.
  const actorOptions = useMemo(() => {
    const keys = new Set<string>([...filters.actors, ...Object.keys(facets?.actors ?? {})]);
    for (const item of archived ? [] : items) {
      const key = inboxActorKey(item);
      if (key != null) keys.add(key);
    }
    return [...keys]
      .map((key) => {
        const { type, id } = inboxActorKeyParts(key);
        return { key, type, id, name: getActorName(type, id) };
      })
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [items, getActorName, archived, facets, filters.actors]);
  const valueCatalog = useFilterValueCatalog(query.length > 0);

  const isSelected = (dimension: InboxListDimension, value: string) =>
    inboxFilterValues(effectiveFilters, dimension).includes(value);
  const toggle = (dimension: InboxListDimension, value: string) =>
    store.toggleFilterValue(wsId, dimension, value);
  const operator = (dimension: InboxListDimension) =>
    serverOnlySupported ? (
      <FilterOperatorToggle
        negated={effectiveFilters.negated.includes(dimension)}
        onNegatedChange={(negated) => store.setFilterNegated(wsId, dimension, negated)}
      />
    ) : null;
  const actorAvatar = (option: { type: string; id: string; name: string }) => (
    <ActorAvatar
      size="xs"
      name={option.name}
      initials={getActorInitials(option.type, option.id)}
      avatarUrl={getActorAvatarUrl(option.type, option.id)}
      isAgent={option.type === "agent"}
      isSquad={option.type === "squad"}
      isSystem={option.type === "system"}
    />
  );
  const statusIcon = (key: string) => {
    const option = statusOptions.find((candidate) => candidate.key === key);
    return option ? (
      <StatusIcon status={option.key} category={option.category} color={option.color} icon={option.icon} className="size-3.5" />
    ) : null;
  };

  const submenuBody = (key: InboxFilterKey): ReactNode => {
    switch (key) {
      case "unread":
        return (
          <ValueItem
            checked={effectiveFilters.unreadOnly}
            onToggle={() => store.toggleUnreadOnly(wsId)}
            icon={<span className="flex size-3.5 items-center justify-center"><span className="size-1.5 rounded-full bg-brand" /></span>}
            label={labels.unread}
            count={counts.unread}
          />
        );
      case "type":
        return (
          <>
            {operator("type")}
            {INBOX_TYPE_GROUPS.map((type) => {
              const Icon = INBOX_TYPE_ICONS[type];
              return (
                <ValueItem
                  key={type}
                  checked={isSelected("type", type)}
                  onToggle={() => toggle("type", type)}
                  icon={<Icon className="size-3.5 text-muted-foreground" />}
                  label={labels.type[type]}
                  count={counts.types.get(type)}
                />
              );
            })}
          </>
        );
      case "actor":
        return (
          <>
            {operator("actor")}
            <div className="max-h-72 overflow-y-auto">
              {actorOptions.map((option) => (
                <ValueItem
                  key={option.key}
                  checked={isSelected("actor", option.key)}
                  onToggle={() => toggle("actor", option.key)}
                  icon={actorAvatar(option)}
                  label={option.name}
                  count={counts.actors.get(option.key)}
                />
              ))}
            </div>
          </>
        );
      case "received":
        return INBOX_RECEIVED_FILTERS.map((received) => (
          <ValueItem
            key={received}
            checked={effectiveFilters.received === received}
            onToggle={() => store.setReceived(wsId, received)}
            icon={<INBOX_FILTER_ICONS.received className="size-3.5 text-muted-foreground" />}
            label={labels.received[received]}
          />
        ));
      case "status":
        return (
          <>
            {operator("status")}
            {statusOptions.map((option) => (
              <ValueItem
                key={option.key}
                checked={isSelected("status", option.key)}
                onToggle={() => toggle("status", option.key)}
                icon={statusIcon(option.key)}
                label={option.label}
                count={counts.statuses.get(option.key)}
              />
            ))}
          </>
        );
      case "priority":
        return (
          <>
            {operator("priority")}
            {PRIORITY_DISPLAY_ORDER.map((priority) => (
              <ValueItem
                key={priority}
                checked={isSelected("priority", priority)}
                onToggle={() => toggle("priority", priority)}
                icon={<PriorityIcon priority={priority} />}
                label={tIssues(($) => $.priority[priority])}
                count={counts.priorities.get(priority)}
              />
            ))}
          </>
        );
      case "project":
        return (
          <>
            {operator("project")}
            <ProjectSubContent
              counts={counts.projects}
              selected={effectiveFilters.projects.filter((id) => id !== FILTER_NONE_VALUE)}
              onToggle={(id) => toggle("project", id)}
              includeNoProject={isSelected("project", FILTER_NONE_VALUE)}
              onToggleNoProject={() => toggle("project", FILTER_NONE_VALUE)}
              noProjectCount={counts.projects.get(FILTER_NONE_VALUE) ?? 0}
            />
          </>
        );
      case "assignee":
        return (
          <>
            {operator("assignee")}
            <ActorSubContent
              counts={counts.assignees}
              selected={effectiveFilters.assignees.flatMap((key) => {
                const { type, id } = inboxActorKeyParts(key);
                return type === "member" || type === "agent" || type === "squad" ? [{ type, id }] : [];
              })}
              onToggle={(actor) => toggle("assignee", `${actor.type}:${actor.id}`)}
              showNoAssignee
              includeNoAssignee={isSelected("assignee", FILTER_NONE_VALUE)}
              onToggleNoAssignee={() => toggle("assignee", FILTER_NONE_VALUE)}
              noAssigneeCount={counts.assignees.get(FILTER_NONE_VALUE) ?? 0}
            />
          </>
        );
      case "label":
        return (
          <>
            {operator("label")}
            <LabelSubContent
              counts={counts.labels}
              selected={[...effectiveFilters.labels]}
              onToggle={(id) => toggle("label", id)}
            />
          </>
        );
      case "dueDate":
        return (
          <>
            {operator("dueDate")}
            {DUE_DATE_FILTERS.map((bucket) => (
              <ValueItem
                key={bucket}
                checked={isSelected("dueDate", bucket)}
                onToggle={() => toggle("dueDate", bucket)}
                icon={<INBOX_FILTER_ICONS.dueDate className="size-3.5 text-muted-foreground" />}
                label={labels.dueDate(bucket)}
              />
            ))}
          </>
        );
    }
  };

  // Directory-backed submenus bring their own search and padding.
  const flushContent = (key: InboxFilterKey) => key === "project" || key === "assignee" || key === "label";
  const section = (key: InboxFilterKey) => {
    const Icon = INBOX_FILTER_ICONS[key];
    const chip = chipByKey.get(key);
    return (
      <DropdownMenuSub key={key}>
        <DropdownMenuSubTrigger>
          <Icon className="size-3.5" />
          <span className="flex-1">{labels.dimension[key]}</span>
          {chip && (
            <span className="ml-3 flex min-w-0 items-center gap-1 text-caption">
              <FilterOperator negated={chip.negated} />
              <span className="max-w-40 truncate font-medium text-primary">{chip.value}</span>
            </span>
          )}
        </DropdownMenuSubTrigger>
        <DropdownMenuSubContent className={cn("w-auto min-w-52", flushContent(key) && "p-0")}>
          {submenuBody(key)}
        </DropdownMenuSubContent>
      </DropdownMenuSub>
    );
  };

  // The dimensions in use when the menu opened go first; membership is
  // taken at open, so a row never jumps out from under the pointer.
  const activeKeys = activeAtOpen ?? new Set(activeInboxFilterKeys(effectiveFilters));
  const group = (title: string | null, keys: readonly InboxFilterKey[]) => {
    const shown = keys.filter((key) => available(key) && !activeKeys.has(key));
    if (shown.length === 0) return null;
    return (
      <DropdownMenuGroup>
        {title && <DropdownMenuLabel>{title}</DropdownMenuLabel>}
        {shown.map(section)}
      </DropdownMenuGroup>
    );
  };

  // Search: matching fields keep their submenus; matching values become
  // one-click rows that read as the condition they add.
  const allKeys = [...NOTIFICATION_KEYS, ...ISSUE_KEYS].filter(available);
  const fieldHits = query ? allKeys.filter((key) => matchesFilterQuery(labels.dimension[key], query)) : [];
  type ValueHit = { key: InboxFilterKey; value: string; label: string; icon: ReactNode; checked: boolean; onToggle: () => void };
  const valueHits: ValueHit[] = [];
  if (query) {
    const listHit = (key: InboxListDimension, value: string, label: string, icon: ReactNode) => ({
      key, value, label, icon, checked: isSelected(key, value), onToggle: () => toggle(key, value),
    });
    if (available("unread")) {
      valueHits.push({
        key: "unread", value: "unread", label: labels.unread, icon: null,
        checked: effectiveFilters.unreadOnly, onToggle: () => store.toggleUnreadOnly(wsId),
      });
    }
    if (available("type")) {
      for (const type of INBOX_TYPE_GROUPS) {
        const Icon = INBOX_TYPE_ICONS[type];
        valueHits.push(listHit("type", type, labels.type[type], <Icon className="size-3.5 text-muted-foreground" />));
      }
    }
    if (available("actor")) {
      for (const option of actorOptions) valueHits.push(listHit("actor", option.key, option.name, actorAvatar(option)));
    }
    if (available("received")) {
      for (const received of INBOX_RECEIVED_FILTERS) {
        valueHits.push({
          key: "received", value: received, label: labels.received[received], icon: null,
          checked: effectiveFilters.received === received, onToggle: () => store.setReceived(wsId, received),
        });
      }
    }
    for (const option of statusOptions) valueHits.push(listHit("status", option.key, option.label, statusIcon(option.key)));
    if (available("priority")) {
      for (const option of valueCatalog("priority")) valueHits.push(listHit("priority", option.value, option.label, option.icon));
    }
    for (const dimension of ["project", "assignee", "label", "dueDate"] as const) {
      if (!available(dimension)) continue;
      for (const option of valueCatalog(dimension)) valueHits.push(listHit(dimension, option.value, option.label, option.icon));
    }
  }
  const matchedValues = valueHits.filter((hit) => matchesFilterQuery(hit.label, query)).slice(0, 50);

  const menuBody = query ? (
    <>
      {fieldHits.length > 0 && (
        <DropdownMenuGroup>
          <DropdownMenuLabel>{tIssues(($) => $.filters.search_fields)}</DropdownMenuLabel>
          {fieldHits.map(section)}
        </DropdownMenuGroup>
      )}
      {fieldHits.length > 0 && matchedValues.length > 0 && <DropdownMenuSeparator />}
      {matchedValues.length > 0 && (
        <DropdownMenuGroup>
          <DropdownMenuLabel>{tIssues(($) => $.filters.search_values)}</DropdownMenuLabel>
          {matchedValues.map((hit) => {
            const FieldIcon = INBOX_FILTER_ICONS[hit.key];
            return (
              <DropdownMenuCheckboxItem
                key={`${hit.key}:${hit.value}`}
                checked={hit.checked}
                onCheckedChange={hit.onToggle}
                className={FILTER_ITEM_CLASS}
              >
                <HoverCheck checked={hit.checked} />
                <FieldIcon className="size-3.5 text-muted-foreground" />
                <span className="shrink-0 text-muted-foreground">{labels.dimension[hit.key]}</span>
                <FilterOperator negated={effectiveFilters.negated.includes(hit.key as InboxListDimension)} />
                {hit.icon}
                <span className="truncate">{hit.label}</span>
              </DropdownMenuCheckboxItem>
            );
          })}
        </DropdownMenuGroup>
      )}
      {fieldHits.length === 0 && matchedValues.length === 0 && (
        <div className="px-2 py-3 text-center text-body text-muted-foreground">
          {tIssues(($) => $.filters.no_results)}
        </div>
      )}
    </>
  ) : (
    <>
      {activeKeys.size > 0 && (
        <>
          <DropdownMenuGroup>
            <DropdownMenuLabel>{tIssues(($) => $.filters.active_section)}</DropdownMenuLabel>
            {[...NOTIFICATION_KEYS, ...ISSUE_KEYS].filter((key) => available(key) && activeKeys.has(key)).map(section)}
          </DropdownMenuGroup>
          <DropdownMenuSeparator />
        </>
      )}
      {group(t(($) => $.filters.section_notification), NOTIFICATION_KEYS)}
      {/* With no notification fields the heading has nothing to tell apart. */}
      {group(archived ? t(($) => $.filters.section_issue) : null, ISSUE_KEYS)}
      {activeCount > 0 && (
        <>
          <DropdownMenuSeparator />
          <DropdownMenuItem onClick={() => store.clearFilters(wsId)}>
            <RotateCcw className="size-3.5" />
            {tIssues(($) => $.filters.reset)}
          </DropdownMenuItem>
        </>
      )}
    </>
  );

  const triggerLabel =
    activeCount > 0
      ? t(($) => $.filters.active_count, { count: activeCount })
      : t(($) => $.filters.tooltip);

  return (
    <DropdownMenu
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        setActiveAtOpen(next ? new Set(activeInboxFilterKeys(effectiveFilters)) : null);
        if (!next) setSearch("");
      }}
    >
      <DropdownMenuTrigger
        render={
          <IconButton
            variant={activeCount > 0 ? "default" : "ghost"}
            size="icon-sm"
            label={triggerLabel}
            tooltipSide="bottom"
            className={cn(
              "text-muted-foreground",
              activeCount > 0 &&
                "w-auto gap-1 bg-brand px-2 text-white hover:bg-brand/90",
            )}
          />
        }
      >
        <Filter className="size-4" />
        {activeCount > 0 && (
          <span className="text-caption tabular-nums">{activeCount}</span>
        )}
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-auto min-w-64 p-0">
        <div data-filter-search className="flex items-center gap-1.5 border-b border-foreground/5 px-2.5 py-2">
          <Search className="size-3.5 shrink-0 text-muted-foreground" />
          <input
            type="text"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            onKeyDown={stopMenuSearchKeys}
            placeholder={tIssues(($) => $.filters.search_placeholder)}
            aria-label={tIssues(($) => $.filters.search_placeholder)}
            className="w-full bg-transparent text-body outline-none placeholder:text-muted-foreground"
            autoFocus
          />
        </div>
        <div className="max-h-[min(70vh,36rem)] overflow-x-hidden overflow-y-auto p-1">
          {archived && facetsQuery.isLoading && (
            <p role="status" className="px-2 py-1 text-caption text-muted-foreground">{t(($) => $.list.loading_more)}</p>
          )}
          {archived && facetsQuery.isError && (
            <div className="px-2 py-1">
              <p role="alert" className="text-caption text-destructive">{t(($) => $.errors.filters_load_failed)}</p>
              <Button variant="ghost" size="sm" onClick={() => { void facetsQuery.refetch(); }}>{t(($) => $.list.retry)}</Button>
            </div>
          )}
          {menuBody}
        </div>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
