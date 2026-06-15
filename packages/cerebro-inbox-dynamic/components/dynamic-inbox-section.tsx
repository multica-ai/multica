// TECH-3413 — one configurable section inside the dynamic inbox box.
"use client";

import { useMemo, useState } from "react";
import { ChevronDown, ChevronRight, Settings2, X, Pencil, Filter, Star } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuLabel,
  DropdownMenuGroup,
} from "@multica/ui/components/ui/dropdown-menu";
import type { InboxActionCategory } from "@multica/cerebro-inbox";
import type { AgentRunState } from "@multica/views/inbox/components/inbox-list-item";
import type { Project } from "@multica/core/types";
import {
  selectSectionEntries,
  groupSectionEntries,
  type DynInboxEntry,
  type SectionGroup,
  type SectionGroupContext,
  sectionFilters,
  type SectionFilterContext,
} from "../section-filter";
import { FilterBuilder } from "./filter-builder";
import {
  sectionLabel,
  type InboxSectionConfig,
  type SectionBadgeColor,
  type SectionGroupBy,
  type SectionSortMethod,
  type SectionViewFilter,
} from "../layout";
import { DynamicInboxRow } from "./dynamic-inbox-row";

// TECH-3541 #2 — classic-inbox parity controls for the "All messages" box:
// the same group-by / sort / view-filter options and bulk actions the classic
// inbox ⋯ menu offers (minus the priorities panel).
const GROUP_BY_OPTIONS: { value: SectionGroupBy; label: string }[] = [
  { value: "none", label: "None" },
  { value: "action", label: "Action" },
  { value: "project", label: "Project" },
  { value: "agent", label: "Agent" },
  { value: "type", label: "Type" },
];

const SORT_METHOD_OPTIONS: { value: SectionSortMethod; label: string }[] = [
  { value: "last_message", label: "Last message" },
  { value: "last_other", label: "Last from another actor" },
  { value: "last_me", label: "Last from me" },
];

const VIEW_FILTER_OPTIONS: { value: SectionViewFilter; label: string }[] = [
  { value: "all", label: "All" },
  { value: "unread", label: "Unread" },
  { value: "mentioned", label: "Mentioned" },
  { value: "issues", label: "Issues only" },
  { value: "channels", label: "Channels only" },
  { value: "dms", label: "DMs only" },
  { value: "muted", label: "Muted" },
  { value: "pinned", label: "Pinned" },
];

/** TECH-3541 #2 — handlers + flags supplied to the "All messages" box so it
 *  exposes the classic inbox's view options and bulk maintenance actions. */
export interface SectionClassicControls {
  channelsEnabled: boolean;
  pinnedFilterEnabled: boolean;
  onMarkAllRead: () => void;
  onArchiveAll: () => void;
  onArchiveAllRead: () => void;
  onArchiveCompleted: () => void;
}

export interface DynamicInboxSectionProps {
  section: InboxSectionConfig;
  entries: DynInboxEntry[];
  filterContext: SectionFilterContext;
  actionLabels: Record<InboxActionCategory, string>;
  projects: Project[];
  /** TECH-3541 #2 — lookup tables for project/agent/type grouping. */
  groupContext?: SectionGroupContext;
  /** TECH-3541 #2 — present only for the "All messages" box; enables the full
   *  classic-inbox settings + bulk actions in the ⋯ menu. */
  classicControls?: SectionClassicControls;
  selectedKey: string | null;
  /** Free-text search from the inbox top bar (TECH-3413 #9). */
  query?: string;
  /** Drag-to-reorder handle injected by the sortable wrapper (TECH-3413 #1). */
  dragHandle?: React.ReactNode;
  /** TECH-3541 (Jesper) — the inbox search bar, rendered as part of the
   *  permanent inbox block (the default "All messages" box). */
  searchSlot?: React.ReactNode;
  /** TECH-3541 (Jesper) — the permanent inbox block cannot be removed; its X
   *  affordance is hidden. Defaults to removable. */
  removable?: boolean;
  /** TECH-3579 — show the favorite star on rows + the favorites-on-top section. */
  favoritesEnabled?: boolean;
  onSelect: (entry: DynInboxEntry) => void;
  onArchive: (entry: DynInboxEntry) => void;
  /** TECH-3579 — star / unstar a conversation. */
  onToggleFavorite?: (entry: DynInboxEntry) => void;
  onChange: (next: InboxSectionConfig) => void;
  onRemove: () => void;
}

function entryKey(entry: DynInboxEntry): string {
  return entry.kind === "notif" ? entry.item.issue_id ?? entry.item.id : entry.id;
}

const BADGE_COLOR_OPTIONS: Array<{
  value: SectionBadgeColor;
  label: string;
  className: string;
  swatchClassName: string;
}> = [
  {
    value: "brand",
    label: "Brand",
    className: "bg-primary/10 text-primary",
    swatchClassName: "bg-primary",
  },
  {
    value: "warning",
    label: "Warning",
    className: "bg-warning/15 text-warning",
    swatchClassName: "bg-warning",
  },
  {
    value: "success",
    label: "Success",
    className: "bg-success/10 text-success",
    swatchClassName: "bg-success",
  },
  {
    value: "destructive",
    label: "Destructive",
    className: "bg-destructive/10 text-destructive",
    swatchClassName: "bg-destructive",
  },
  {
    value: "muted",
    label: "Muted",
    className: "bg-muted text-muted-foreground",
    swatchClassName: "bg-muted-foreground",
  },
];

const DEFAULT_BADGE_COLOR_CLASS_NAME = "bg-primary/10 text-primary";

function badgeColorClassName(color: SectionBadgeColor | undefined): string {
  return (
    BADGE_COLOR_OPTIONS.find((option) => option.value === (color ?? "brand"))?.className ??
    DEFAULT_BADGE_COLOR_CLASS_NAME
  );
}

// Resolve the running/queued pip for a row from the same task state the
// classic inbox uses: issue rows key on issue_id, chat rows on session id.
function runStateFor(
  entry: DynInboxEntry,
  ctx: SectionFilterContext,
): AgentRunState | undefined {
  if (entry.kind === "notif" && entry.item.issue_id) {
    return ctx.action.issueRunStates.get(entry.item.issue_id) as AgentRunState | undefined;
  }
  if (entry.kind === "chat") {
    return ctx.action.chatRunStates.get(entry.session.id) as AgentRunState | undefined;
  }
  return undefined;
}

export function DynamicInboxSection(props: DynamicInboxSectionProps) {
  const { section, entries, filterContext, actionLabels, projects, selectedKey, query } = props;

  const selected = useMemo(
    () => selectSectionEntries(entries, section, filterContext, { query }),
    [entries, section, filterContext, query],
  );

  // TECH-3579 — "All messages" box only: pull starred conversations out of the
  // normal list and show them in a "Favorites" sub-section at the very top.
  // Default on; turn it off (per box) to keep favorites in their normal spot.
  // The standalone "favorites" box is all-favorites already, so it never splits.
  const favoritesOnTop =
    !!props.favoritesEnabled &&
    section.kind === "all" &&
    section.showFavoritesSection !== false;
  const favoriteEntries = useMemo(
    () => (favoritesOnTop ? selected.filter((e) => filterContext.isFavorite?.(e)) : []),
    [favoritesOnTop, selected, filterContext],
  );
  const restEntries = useMemo(
    () => (favoritesOnTop ? selected.filter((e) => !filterContext.isFavorite?.(e)) : selected),
    [favoritesOnTop, selected, filterContext],
  );

  const groups = useMemo(
    () => groupSectionEntries(restEntries, section, filterContext, actionLabels, props.groupContext),
    [restEntries, section, filterContext, actionLabels, props.groupContext],
  );

  // #2: foldable boxes. Collapsible unless explicitly turned off; initial fold
  // state from defaultCollapsed. The live toggle is ephemeral UI state.
  const collapsible = section.collapsible !== false;
  const [collapsed, setCollapsed] = useState<boolean>(section.defaultCollapsed === true);
  const isCollapsed = collapsible && collapsed;

  // TECH-3502 #4 — inline rename of the box header.
  const [editing, setEditing] = useState(false);
  // TECH-3502 #6 — the filter builder is configuration UI, so it only shows
  // while you're building/changing the box: open by default for a just-added
  // (empty) filter box, otherwise revealed from the ⋯ menu's "Edit filter".
  const [editFilter, setEditFilter] = useState(
    section.kind === "filter" && sectionFilters(section).length === 0,
  );

  const projectName =
    section.kind === "project"
      ? projects.find((p) => p.id === section.projectId)?.title
      : undefined;
  // A user-set title wins for every kind (TECH-3502 #4); otherwise project
  // boxes show their project, and the rest fall back to the catalog label.
  const headerLabel = section.title?.trim()
    ? section.title.trim()
    : section.kind === "project"
      ? `Project · ${projectName ?? "select…"}`
      : sectionLabel(section);

  const commitRename = (value: string) => {
    const trimmed = value.trim();
    props.onChange({ ...section, title: trimmed || undefined });
    setEditing(false);
  };

  // #3: count as a plain number or a coloured circle badge.
  const countCircle = section.countStyle === "circle";
  const badgeColor = section.badgeColor ?? "brand";
  const badgeClassName = badgeColorClassName(badgeColor);

  // TECH-3541 #2 — the "All messages" box gets the full classic-inbox settings.
  const isAllBox = section.kind === "all";
  const controls = props.classicControls;
  const viewFilter = section.viewFilter ?? "all";
  const sortMethod = section.sortMethod ?? "last_message";
  const groupPrimary = section.groupBy ?? "none";
  const groupSecondary = section.groupBySecondary ?? "none";
  // "Then by" only applies to the multi-level modes (not none/action).
  const showThenBy = groupPrimary === "project" || groupPrimary === "agent" || groupPrimary === "type";
  const viewFilterOptions = VIEW_FILTER_OPTIONS.filter(
    (o) =>
      (controls?.channelsEnabled || (o.value !== "channels" && o.value !== "dms")) &&
      (controls?.pinnedFilterEnabled || o.value !== "pinned"),
  );

  const renderEntry = (entry: DynInboxEntry) => (
    <DynamicInboxRow
      key={entry.id}
      entry={entry}
      isSelected={entryKey(entry) === selectedKey}
      mentioned={
        entry.kind === "channel" && filterContext.action.mentionedChannels.has(entry.channel.id)
      }
      agentRunState={runStateFor(entry, filterContext)}
      favoritesEnabled={props.favoritesEnabled}
      isFavorite={filterContext.isFavorite?.(entry) ?? false}
      onToggleFavorite={props.onToggleFavorite}
      onSelect={props.onSelect}
      onArchive={props.onArchive}
    />
  );

  // Render one group; nests one level when the group carries "Then by" children.
  const renderGroup = (group: SectionGroup, depth: number): React.ReactNode => (
    <div key={group.key}>
      {group.label && (
        <div
          className="px-3 pt-2 text-[10px] font-bold uppercase tracking-wide text-muted-foreground/80"
          style={depth > 0 ? { paddingLeft: 12 + depth * 12 } : undefined}
        >
          {group.label}
        </div>
      )}
      {group.children
        ? group.children.map((child) => renderGroup(child, depth + 1))
        : group.entries.map(renderEntry)}
    </div>
  );

  return (
    <section className="overflow-hidden rounded-xl border border-border bg-card">
      <header className="flex items-center gap-2 border-b border-border px-3 py-2">
        {props.dragHandle}
        {collapsible && (
          <button
            type="button"
            className="-ml-1 rounded p-0.5 text-muted-foreground hover:bg-muted"
            onClick={() => setCollapsed((c) => !c)}
            title={isCollapsed ? "Expand" : "Collapse"}
          >
            {isCollapsed ? <ChevronRight className="size-3.5" /> : <ChevronDown className="size-3.5" />}
          </button>
        )}
        {editing ? (
          <input
            autoFocus
            defaultValue={section.title ?? ""}
            placeholder={headerLabel}
            onBlur={(e) => commitRename(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") commitRename(e.currentTarget.value);
              else if (e.key === "Escape") setEditing(false);
            }}
            className="w-40 rounded border border-primary bg-transparent px-1 py-0.5 text-xs font-bold uppercase tracking-wide text-foreground outline-none"
            aria-label="Box name"
          />
        ) : (
          <button
            type="button"
            onDoubleClick={() => setEditing(true)}
            title="Double-click to rename"
            className="text-xs font-bold uppercase tracking-wide text-muted-foreground hover:text-foreground"
          >
            {headerLabel}
          </button>
        )}
        {countCircle ? (
          <span
            className={`inline-flex min-w-5 items-center justify-center rounded-full px-1.5 text-[11px] font-semibold ${badgeClassName}`}
          >
            {selected.length}
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">{selected.length}</span>
        )}
        <div className="ml-auto flex items-center gap-0.5 text-muted-foreground">
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<button type="button" className="rounded p-1 hover:bg-muted" title="Section settings" />}
            >
              <Settings2 className="size-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuGroup>
                <DropdownMenuItem onClick={() => setEditing(true)}>
                  <Pencil className="mr-2 size-3.5" /> Rename box
                </DropdownMenuItem>
                {section.title && (
                  <DropdownMenuItem onClick={() => props.onChange({ ...section, title: undefined })}>
                    Reset name
                  </DropdownMenuItem>
                )}
                {section.kind === "filter" && (
                  <DropdownMenuItem onClick={() => setEditFilter(true)}>
                    <Filter className="mr-2 size-3.5" /> Edit filter
                  </DropdownMenuItem>
                )}
              </DropdownMenuGroup>
              {/* TECH-3541 #2 — the "All messages" box mirrors the classic
                  inbox's view switch (All / Unread / Mentioned / …). */}
              {isAllBox && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>View</DropdownMenuLabel>
                    {viewFilterOptions.map((opt) => (
                      <DropdownMenuItem
                        key={`view-${opt.value}`}
                        onClick={() => props.onChange({ ...section, viewFilter: opt.value })}
                      >
                        {opt.label} {viewFilter === opt.value ? "✓" : ""}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuGroup>
                </>
              )}
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>Group by</DropdownMenuLabel>
                {isAllBox ? (
                  GROUP_BY_OPTIONS.map((opt) => (
                    <DropdownMenuItem
                      key={`group-${opt.value}`}
                      onClick={() =>
                        props.onChange({
                          ...section,
                          groupBy: opt.value,
                          // Drop "Then by" when the new primary takes no
                          // sub-grouping (none/action) or equals the secondary.
                          groupBySecondary:
                            opt.value === "none" ||
                            opt.value === "action" ||
                            groupSecondary === opt.value
                              ? "none"
                              : groupSecondary,
                        })
                      }
                    >
                      {opt.label} {groupPrimary === opt.value ? "✓" : ""}
                    </DropdownMenuItem>
                  ))
                ) : (
                  <>
                    <DropdownMenuItem onClick={() => props.onChange({ ...section, groupBy: "none" })}>
                      None {section.groupBy !== "action" ? "✓" : ""}
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={() => props.onChange({ ...section, groupBy: "action" })}>
                      Action {section.groupBy === "action" ? "✓" : ""}
                    </DropdownMenuItem>
                  </>
                )}
              </DropdownMenuGroup>
              {isAllBox && showThenBy && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>Then by</DropdownMenuLabel>
                    <DropdownMenuItem
                      onClick={() => props.onChange({ ...section, groupBySecondary: "none" })}
                    >
                      None {groupSecondary === "none" ? "✓" : ""}
                    </DropdownMenuItem>
                    {GROUP_BY_OPTIONS.filter(
                      (opt) => opt.value !== "none" && opt.value !== "action" && opt.value !== groupPrimary,
                    ).map((opt) => (
                      <DropdownMenuItem
                        key={`thenby-${opt.value}`}
                        onClick={() => props.onChange({ ...section, groupBySecondary: opt.value })}
                      >
                        {opt.label} {groupSecondary === opt.value ? "✓" : ""}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuGroup>
                </>
              )}
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>Sort</DropdownMenuLabel>
                {isAllBox &&
                  SORT_METHOD_OPTIONS.map((opt) => (
                    <DropdownMenuItem
                      key={`sortm-${opt.value}`}
                      onClick={() => props.onChange({ ...section, sortMethod: opt.value })}
                    >
                      {opt.label} {sortMethod === opt.value ? "✓" : ""}
                    </DropdownMenuItem>
                  ))}
                {isAllBox && <DropdownMenuLabel>Direction</DropdownMenuLabel>}
                <DropdownMenuItem onClick={() => props.onChange({ ...section, sort: "newest" })}>
                  Newest {section.sort !== "oldest" ? "✓" : ""}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => props.onChange({ ...section, sort: "oldest" })}>
                  Oldest {section.sort === "oldest" ? "✓" : ""}
                </DropdownMenuItem>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>Display</DropdownMenuLabel>
                {/* TECH-3579 — toggle the top Favorites sub-section on the "All
                    messages" box. Off keeps favorites in their normal position
                    (they can still be starred). */}
                {isAllBox && props.favoritesEnabled && (
                  <DropdownMenuItem
                    onClick={() =>
                      props.onChange({
                        ...section,
                        showFavoritesSection: section.showFavoritesSection === false,
                      })
                    }
                  >
                    <Star className="mr-2 size-3.5" /> Favorites on top{" "}
                    {section.showFavoritesSection !== false ? "✓" : ""}
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem
                  onClick={() => props.onChange({ ...section, collapsible: section.collapsible === false })}
                >
                  Foldable {section.collapsible !== false ? "✓" : ""}
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() => props.onChange({ ...section, defaultCollapsed: !section.defaultCollapsed })}
                >
                  Start collapsed {section.defaultCollapsed ? "✓" : ""}
                </DropdownMenuItem>
                <DropdownMenuItem
                  onClick={() =>
                    props.onChange({ ...section, countStyle: countCircle ? "plain" : "circle" })
                  }
                >
                  Count as circle badge {countCircle ? "✓" : ""}
                </DropdownMenuItem>
                {countCircle && (
                  <>
                    <DropdownMenuLabel>Badge color</DropdownMenuLabel>
                    {BADGE_COLOR_OPTIONS.map((option) => (
                      <DropdownMenuItem
                        key={option.value}
                        onClick={() => props.onChange({ ...section, badgeColor: option.value })}
                      >
                        <span
                          aria-hidden
                          className={`mr-2 inline-block size-2.5 rounded-full ${option.swatchClassName}`}
                        />
                        {option.label} {badgeColor === option.value ? "✓" : ""}
                      </DropdownMenuItem>
                    ))}
                  </>
                )}
              </DropdownMenuGroup>
              {/* TECH-3541 #2 — classic inbox bulk maintenance actions, on the
                  "All messages" box only. These operate on the whole inbox,
                  exactly like the classic ⋯ menu. */}
              {isAllBox && controls && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>Actions</DropdownMenuLabel>
                    <DropdownMenuItem onClick={controls.onMarkAllRead}>
                      Mark all as read
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={controls.onArchiveAll}>
                      Archive all
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={controls.onArchiveAllRead}>
                      Archive all read
                    </DropdownMenuItem>
                    <DropdownMenuItem onClick={controls.onArchiveCompleted}>
                      Archive completed
                    </DropdownMenuItem>
                  </DropdownMenuGroup>
                </>
              )}
              {/* TECH-3413 #5 — the "filter" kind is configured by the inline
                  FilterBuilder (chips) in the section body, not here. */}
              {section.kind === "project" && projects.length > 0 && (
                <>
                  <DropdownMenuSeparator />
                  <DropdownMenuGroup>
                    <DropdownMenuLabel>Project</DropdownMenuLabel>
                    {projects.map((p) => (
                      <DropdownMenuItem
                        key={p.id}
                        onClick={() => props.onChange({ ...section, projectId: p.id })}
                      >
                        {p.title} {section.projectId === p.id ? "✓" : ""}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuGroup>
                </>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
          {props.removable !== false && (
            <button
              type="button"
              className="rounded p-1 hover:bg-muted"
              onClick={props.onRemove}
              title="Remove section"
            >
              <X className="size-3.5" />
            </button>
          )}
        </div>
      </header>

      {/* TECH-3541 (Jesper) — the inbox search bar lives inside the permanent
          inbox block. Kept visible even when the box is collapsed so search is
          always reachable. */}
      {props.searchSlot}

      {!isCollapsed && section.kind === "filter" && editFilter && (
        <div className="border-b border-border/60">
          <FilterBuilder
            filters={sectionFilters(section)}
            projects={projects}
            onChange={(filters) => props.onChange({ ...section, filters })}
          />
          <div className="flex justify-end px-3 pb-2">
            <button
              type="button"
              onClick={() => setEditFilter(false)}
              className="rounded-md px-2 py-0.5 text-[11px] font-medium text-brand hover:bg-accent"
            >
              Done
            </button>
          </div>
        </div>
      )}

      {!isCollapsed && (
        <div className="divide-y divide-border/60">
          {selected.length === 0 ? (
            <p className="px-3 py-3 text-xs text-muted-foreground">Nothing here right now.</p>
          ) : (
            <>
              {/* TECH-3579 — starred conversations float to the top of the
                  "All messages" box in their own Favorites sub-section. */}
              {favoritesOnTop && favoriteEntries.length > 0 && (
                <div>
                  <div className="flex items-center gap-1 px-3 pt-2 text-[10px] font-bold uppercase tracking-wide text-warning">
                    <Star className="size-3 fill-warning" /> Favorites
                  </div>
                  {favoriteEntries.map(renderEntry)}
                </div>
              )}
              {groups.map((group) => renderGroup(group, 0))}
            </>
          )}
        </div>
      )}
    </section>
  );
}
