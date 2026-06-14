// TECH-3413 — one configurable section inside the dynamic inbox box.
"use client";

import { useMemo, useState } from "react";
import { ChevronDown, ChevronRight, Settings2, X, Pencil, Filter } from "lucide-react";
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
  sectionFilters,
  type SectionFilterContext,
} from "../section-filter";
import { FilterBuilder } from "./filter-builder";
import { sectionLabel, type InboxSectionConfig, type SectionBadgeColor } from "../layout";
import { DynamicInboxRow } from "./dynamic-inbox-row";

export interface DynamicInboxSectionProps {
  section: InboxSectionConfig;
  entries: DynInboxEntry[];
  filterContext: SectionFilterContext;
  actionLabels: Record<InboxActionCategory, string>;
  projects: Project[];
  selectedKey: string | null;
  /** Free-text search from the inbox top bar (TECH-3413 #9). */
  query?: string;
  /** Drag-to-reorder handle injected by the sortable wrapper (TECH-3413 #1). */
  dragHandle?: React.ReactNode;
  onSelect: (entry: DynInboxEntry) => void;
  onArchive: (entry: DynInboxEntry) => void;
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
  const groups = useMemo(
    () => groupSectionEntries(selected, section, filterContext, actionLabels),
    [selected, section, filterContext, actionLabels],
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
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>Group by</DropdownMenuLabel>
                <DropdownMenuItem onClick={() => props.onChange({ ...section, groupBy: "none" })}>
                  None {section.groupBy !== "action" ? "✓" : ""}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => props.onChange({ ...section, groupBy: "action" })}>
                  Action {section.groupBy === "action" ? "✓" : ""}
                </DropdownMenuItem>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>Sort</DropdownMenuLabel>
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
                <DropdownMenuItem
                  onClick={() => props.onChange({ ...section, excludeMuted: !section.excludeMuted })}
                >
                  Hide muted {section.excludeMuted ? "✓" : ""}
                </DropdownMenuItem>
              </DropdownMenuGroup>
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
          <button
            type="button"
            className="rounded p-1 hover:bg-muted"
            onClick={props.onRemove}
            title="Remove section"
          >
            <X className="size-3.5" />
          </button>
        </div>
      </header>

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
            groups.map((group) => (
              <div key={group.key}>
                {group.label && (
                  <div className="px-3 pt-2 text-[10px] font-bold uppercase tracking-wide text-muted-foreground/80">
                    {group.label}
                  </div>
                )}
                {group.entries.map((entry) => (
                  <DynamicInboxRow
                    key={entry.id}
                    entry={entry}
                    isSelected={entryKey(entry) === selectedKey}
                    mentioned={
                      entry.kind === "channel" && filterContext.action.mentionedChannels.has(entry.channel.id)
                    }
                    agentRunState={runStateFor(entry, filterContext)}
                    onSelect={props.onSelect}
                    onArchive={props.onArchive}
                  />
                ))}
              </div>
            ))
          )}
        </div>
      )}
    </section>
  );
}
