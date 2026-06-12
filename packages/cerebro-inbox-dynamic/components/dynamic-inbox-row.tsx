// TECH-3413 — renders one merged-inbox entry inside a dynamic section, reusing
// the upstream row components so rows look identical to the classic inbox.
"use client";

import { InboxListItem, ChannelListItem } from "@multica/views/inbox/components/inbox-list-item";
import { Bot } from "lucide-react";
import type { DynInboxEntry } from "../section-filter";

export interface DynamicInboxRowProps {
  entry: DynInboxEntry;
  isSelected: boolean;
  mentioned?: boolean;
  onSelect: (entry: DynInboxEntry) => void;
  onArchive: (entry: DynInboxEntry) => void;
}

export function DynamicInboxRow({ entry, isSelected, mentioned, onSelect, onArchive }: DynamicInboxRowProps) {
  if (entry.kind === "notif") {
    return (
      <InboxListItem
        item={entry.item}
        isSelected={isSelected}
        onClick={() => onSelect(entry)}
        onArchive={() => onArchive(entry)}
      />
    );
  }
  if (entry.kind === "channel") {
    return (
      <ChannelListItem
        channel={entry.channel}
        mentioned={mentioned}
        isSelected={isSelected}
        onClick={() => onSelect(entry)}
        onArchive={() => onArchive(entry)}
      />
    );
  }
  // Agent chat session — no exported detail row upstream yet (fase 1).
  const session = entry.session;
  return (
    <button
      type="button"
      onClick={() => onSelect(entry)}
      className={`flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm hover:bg-muted/60 ${
        isSelected ? "bg-muted" : ""
      }`}
    >
      <span className="flex size-6 items-center justify-center rounded-full bg-primary/10 text-primary">
        <Bot className="size-3.5" />
      </span>
      <span className="min-w-0 flex-1 truncate">{session.title || "Chat"}</span>
      {session.has_unread && <span className="size-2 flex-none rounded-full bg-blue-500" />}
    </button>
  );
}
