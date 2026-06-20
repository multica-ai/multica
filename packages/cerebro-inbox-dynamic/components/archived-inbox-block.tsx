// FIR-1645 — the Archived block: the user's archived messages as a foldable
// box inside the dynamic inbox, instead of the full-screen ⋯ → "Show archived"
// view. Starts folded by default; offers an in-block search, sort
// (newest/oldest) and group-by-type, and gives each archived message row TWO
// restore actions — "unarchive" and "unarchive & mark unread" — via the
// per-row ArchivedRowActionsProvider that CerebroUnarchiveAction reads.
//
// Like the Chat block, this is rendered by a dedicated component over its own
// archived queries (useArchivedInboxEntries), not a slice of the live merged
// inbox feed.
"use client";

import { useMemo, useState } from "react";
import { ChevronDown, ChevronRight, Search, Settings2, X } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  ArchivedRowActionsProvider,
  useMarkInboxUnread,
  useUnarchiveInbox,
} from "@multica/cerebro-inbox";
import { sectionLabel, type InboxSectionConfig, type SectionSort } from "../layout";
import {
  entryMatchesQuery,
  sortEntries,
  type DynInboxEntry,
} from "../section-filter";
import { useArchivedInboxEntries } from "../use-archived-entries";
import { DynamicInboxRow } from "./dynamic-inbox-row";

export interface ArchivedInboxBlockProps {
  wsId: string;
  section: InboxSectionConfig;
  selectedKey: string | null;
  /** Free-text search from the inbox top bar — combined with the in-block search. */
  query?: string;
  dragHandle?: React.ReactNode;
  onSelect: (entry: DynInboxEntry) => void;
  onChange: (next: InboxSectionConfig) => void;
  onRemove: () => void;
}

const SORT_OPTIONS: { value: SectionSort; label: string }[] = [
  { value: "newest", label: "Newest first" },
  { value: "oldest", label: "Oldest first" },
];

// FIR-1686 — explicit grouping choices so "no grouping" is a first-class option,
// not just un-toggling "Type".
const GROUP_OPTIONS: { value: "none" | "type"; label: string }[] = [
  { value: "none", label: "None" },
  { value: "type", label: "Type" },
];

function entryKey(entry: DynInboxEntry): string {
  return entry.kind === "notif" ? entry.item.issue_id ?? entry.item.id : entry.id;
}

/** Group-by-type buckets, in a stable order; empty buckets are dropped. */
function groupByType(entries: DynInboxEntry[]): Array<{ label: string; items: DynInboxEntry[] }> {
  return [
    { label: "Issues", items: entries.filter((e) => e.kind === "notif") },
    { label: "Channels", items: entries.filter((e) => e.kind === "channel") },
    { label: "Chat", items: entries.filter((e) => e.kind === "chat") },
  ].filter((g) => g.items.length > 0);
}

export function ArchivedInboxBlock({
  wsId,
  section,
  selectedKey,
  query,
  dragHandle,
  onSelect,
  onChange,
  onRemove,
}: ArchivedInboxBlockProps) {
  // Collapse is ephemeral UI state seeded from the persisted defaultCollapsed
  // (true for a freshly added Archived block, so it starts folded).
  const collapsible = section.collapsible !== false;
  const [collapsed, setCollapsed] = useState<boolean>(section.defaultCollapsed !== false);
  const isCollapsed = collapsible && collapsed;

  const [searchOpen, setSearchOpen] = useState(false);
  const [search, setSearch] = useState("");

  const groupBy = section.groupBy === "type" ? "type" : "none";

  const { entries, isLoading, hasNextPage, isFetchingNextPage, fetchNextPage } =
    useArchivedInboxEntries(wsId);
  const unarchive = useUnarchiveInbox();
  const markUnread = useMarkInboxUnread();

  // Search (top-bar query AND in-block search) → sort. Grouping happens after.
  const shown = useMemo(() => {
    const filtered = entries.filter(
      (e) => entryMatchesQuery(e, query ?? "") && entryMatchesQuery(e, search),
    );
    return sortEntries(filtered, section);
  }, [entries, query, search, section]);

  const groups = useMemo(
    () => (groupBy === "type" ? groupByType(shown) : shown.length ? [{ label: "", items: shown }] : []),
    [groupBy, shown],
  );

  const onUnarchive = (entry: DynInboxEntry) => {
    // Chats unarchive through their own row menu (CerebroChatSessionRowActions);
    // here we only restore inbox notifications.
    if (entry.kind === "notif") unarchive.mutate(entry.item.id);
  };
  const onUnarchiveUnread = (entry: DynInboxEntry) => {
    if (entry.kind !== "notif") return;
    unarchive.mutate(entry.item.id);
    markUnread.mutate(entry.item.id);
  };

  const renderRow = (entry: DynInboxEntry) => {
    const row = (
      <DynamicInboxRow
        key={entry.id}
        entry={entry}
        isSelected={entryKey(entry) === selectedKey}
        isArchivedView
        onSelect={onSelect}
        onArchive={() => {}}
        onUnarchive={() => onUnarchive(entry)}
      />
    );
    // Only inbox-message rows get the second "unarchive & mark unread" action.
    if (entry.kind !== "notif") return row;
    return (
      <ArchivedRowActionsProvider
        key={entry.id}
        value={{ onUnarchiveUnread: () => onUnarchiveUnread(entry) }}
      >
        {row}
      </ArchivedRowActionsProvider>
    );
  };

  return (
    <section className="overflow-hidden rounded-xl border border-border bg-card text-sm">
      <header className="flex items-center gap-2 border-b border-border py-2 pl-8 pr-3">
        {dragHandle ? <div className="absolute left-2 top-2.5 z-10">{dragHandle}</div> : null}
        {collapsible && (
          <button
            type="button"
            className="-ml-1 rounded p-0.5 text-muted-foreground hover:bg-muted hover:text-foreground"
            onClick={() => setCollapsed((c) => !c)}
            title={isCollapsed ? "Expand" : "Collapse"}
            aria-label={isCollapsed ? "Expand" : "Collapse"}
          >
            {isCollapsed ? <ChevronRight className="size-4" /> : <ChevronDown className="size-4" />}
          </button>
        )}
        <span className="text-xs font-bold uppercase tracking-wide text-muted-foreground">
          {sectionLabel(section)}
        </span>
        <span className="text-xs text-muted-foreground">{shown.length}</span>
        <div className="ml-auto flex items-center gap-0.5 text-muted-foreground">
          <button
            type="button"
            className={`rounded p-1 hover:bg-muted ${searchOpen ? "bg-muted text-foreground" : ""}`}
            onClick={() => {
              setSearchOpen((open) => !open);
              if (searchOpen) setSearch("");
            }}
            aria-label="Search"
            title="Search"
          >
            <Search className="size-3.5" />
          </button>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={<button type="button" className="rounded p-1 hover:bg-muted" title="Settings" />}
            >
              <Settings2 className="size-3.5" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-56">
              <DropdownMenuGroup>
                <DropdownMenuLabel>Sort by</DropdownMenuLabel>
                {SORT_OPTIONS.map((opt) => (
                  <DropdownMenuItem
                    key={opt.value}
                    onClick={() => onChange({ ...section, sort: opt.value })}
                  >
                    {opt.label} {(section.sort ?? "newest") === opt.value ? "✓" : ""}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuLabel>Group by</DropdownMenuLabel>
                {GROUP_OPTIONS.map((opt) => (
                  <DropdownMenuItem
                    key={opt.value}
                    onClick={() => onChange({ ...section, groupBy: opt.value })}
                  >
                    {opt.label} {groupBy === opt.value ? "✓" : ""}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
          <button
            type="button"
            className="rounded p-1 hover:bg-muted"
            onClick={onRemove}
            title="Remove block"
            aria-label="Remove block"
          >
            <X className="size-3.5" />
          </button>
        </div>
      </header>

      {!isCollapsed && (
        <div className="flex flex-col">
          {searchOpen && (
            <div className="mx-3 mt-3 flex items-center gap-2 rounded-lg border border-border bg-muted/40 px-2.5 py-1.5">
              <Search className="size-3.5 flex-none text-muted-foreground" />
              <input
                type="search"
                value={search}
                onChange={(event) => setSearch(event.target.value)}
                autoComplete="off"
                autoCorrect="off"
                spellCheck={false}
                role="searchbox"
                placeholder="Search archived..."
                aria-label="Search archived"
                className="w-full bg-transparent text-sm outline-none placeholder:text-muted-foreground"
              />
            </div>
          )}

          {isLoading ? (
            <p className="px-3 py-3 text-sm text-muted-foreground">Loading…</p>
          ) : shown.length === 0 ? (
            <p className="px-3 py-3 text-sm text-muted-foreground">
              {search.trim() || (query ?? "").trim() ? "No matches." : "No archived messages."}
            </p>
          ) : (
            <div className="py-1">
              {groups.map((group) => (
                <div key={group.label || "all"}>
                  {group.label && (
                    <div className="px-3 pb-1 pt-2 text-xs font-semibold uppercase tracking-wide text-muted-foreground">
                      {group.label}
                    </div>
                  )}
                  <div className="divide-y divide-border/60">{group.items.map(renderRow)}</div>
                </div>
              ))}
            </div>
          )}

          {hasNextPage && (
            <div className="p-3">
              <button
                type="button"
                onClick={() => fetchNextPage()}
                disabled={isFetchingNextPage}
                className="w-full rounded-md border border-border py-1.5 text-xs font-medium text-muted-foreground hover:bg-muted disabled:opacity-50"
              >
                {isFetchingNextPage ? "Loading…" : "Load more"}
              </button>
            </div>
          )}
        </div>
      )}
    </section>
  );
}
