// TECH-3413 — one configurable section inside the dynamic inbox box.
"use client";

import { useMemo, useState } from "react";
import { ChevronUp, ChevronDown, ChevronRight, Settings2, X } from "lucide-react";
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
  type SectionFilterContext,
} from "../section-filter";
import { sectionLabel, type InboxSectionConfig } from "../layout";
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
  onSelect: (entry: DynInboxEntry) => void;
  onArchive: (entry: DynInboxEntry) => void;
  onChange: (next: InboxSectionConfig) => void;
  onRemove: () => void;
  onMove: (dir: -1 | 1) => void;
  isFirst: boolean;
  isLast: boolean;
}

function entryKey(entry: DynInboxEntry): string {
  return entry.kind === "notif" ? entry.item.issue_id ?? entry.item.id : entry.id;
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

  const projectName =
    section.kind === "project"
      ? projects.find((p) => p.id === section.projectId)?.title
      : undefined;
  const headerLabel =
    section.kind === "project" ? `Project · ${projectName ?? "select…"}` : sectionLabel(section);

  // #3: count as a plain number or a coloured circle badge.
  const countCircle = section.countStyle === "circle";

  return (
    <section className="overflow-hidden rounded-xl border border-border bg-card">
      <header className="flex items-center gap-2 border-b border-border px-3 py-2">
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
        <span className="text-xs font-bold uppercase tracking-wide text-muted-foreground">
          {headerLabel}
        </span>
        {countCircle ? (
          <span className="inline-flex min-w-5 items-center justify-center rounded-full bg-primary/10 px-1.5 text-[11px] font-semibold text-primary">
            {selected.length}
          </span>
        ) : (
          <span className="text-xs text-muted-foreground">{selected.length}</span>
        )}
        <div className="ml-auto flex items-center gap-0.5 text-muted-foreground">
          <button
            type="button"
            className="rounded p-1 hover:bg-muted disabled:opacity-30"
            disabled={props.isFirst}
            onClick={() => props.onMove(-1)}
            title="Move up"
          >
            <ChevronUp className="size-3.5" />
          </button>
          <button
            type="button"
            className="rounded p-1 hover:bg-muted disabled:opacity-30"
            disabled={props.isLast}
            onClick={() => props.onMove(1)}
            title="Move down"
          >
            <ChevronDown className="size-3.5" />
          </button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<button type="button" className="rounded p-1 hover:bg-muted" title="Section settings" />}
            >
              <Settings2 className="size-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
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
                <DropdownMenuItem
                  onClick={() => props.onChange({ ...section, excludeMuted: !section.excludeMuted })}
                >
                  Hide muted {section.excludeMuted ? "✓" : ""}
                </DropdownMenuItem>
              </DropdownMenuGroup>
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
