// TECH-3541 #3 — the archived inbox as a VIEW reached from the dynamic inbox ⋯
// menu (not a box). Mirrors the classic inbox's archived view: paginated
// archived inbox items + archived chats, each row offering unarchive, opening
// into the same detail panel as the live inbox.
"use client";

import { ArrowLeft } from "lucide-react";
import { useUnarchiveInbox } from "@multica/cerebro-inbox";
import { MobileSidebarTrigger } from "@multica/views/layout/page-header";
import type { DynInboxEntry } from "../section-filter";
import { useArchivedInboxEntries } from "../use-archived-entries";
import { DynamicInboxRow } from "./dynamic-inbox-row";

export interface ArchivedInboxViewProps {
  wsId: string;
  selectedKey: string | null;
  onSelect: (entry: DynInboxEntry) => void;
  onBack: () => void;
}

function entryKey(entry: DynInboxEntry): string {
  return entry.kind === "notif" ? entry.item.issue_id ?? entry.item.id : entry.id;
}

export function ArchivedInboxView({ wsId, selectedKey, onSelect, onBack }: ArchivedInboxViewProps) {
  const { entries, isLoading, hasNextPage, isFetchingNextPage, fetchNextPage } =
    useArchivedInboxEntries(wsId);
  const unarchive = useUnarchiveInbox();

  const onUnarchive = (entry: DynInboxEntry) => {
    // Chats unarchive through their own row menu (CerebroChatSessionRowActions);
    // here we only handle inbox notifications.
    if (entry.kind === "notif") unarchive.mutate(entry.item.id);
  };

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-12 shrink-0 items-center gap-1 border-b border-border px-2">
        <MobileSidebarTrigger className="mr-0" />
        <button
          type="button"
          onClick={onBack}
          className="flex shrink-0 items-center gap-1.5 rounded px-2 py-1 text-sm text-muted-foreground hover:bg-muted"
        >
          <ArrowLeft className="size-4" /> Back
        </button>
        <span className="ml-1 text-sm font-semibold">Archived</span>
        <span className="text-xs text-muted-foreground">{entries.length}</span>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {isLoading ? (
          <p className="px-3 py-3 text-sm text-muted-foreground">Loading…</p>
        ) : entries.length === 0 ? (
          <p className="px-3 py-3 text-sm text-muted-foreground">No archived messages.</p>
        ) : (
          <div className="divide-y divide-border/60">
            {entries.map((entry) => (
              <DynamicInboxRow
                key={entry.id}
                entry={entry}
                isSelected={entryKey(entry) === selectedKey}
                isArchivedView
                onSelect={onSelect}
                onArchive={() => {}}
                onUnarchive={onUnarchive}
              />
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
    </div>
  );
}
