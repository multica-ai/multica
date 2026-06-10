// CEREBRO: TECH-3298 — render an agent wakeup as a small, collapsible action
// note (like a status change) instead of a full comment card. The wakeup type
// is always visible; clicking the row expands the note the agent wrote to
// itself. Replaces the old "**Wakeup:** ..." comment that flooded the thread.
"use client";

import { useState } from "react";
import { AlarmClock, ChevronDown, ChevronRight, GitPullRequest, ListChecks, Moon } from "lucide-react";
import type { TimelineEntry } from "@multica/core/types/activity";

// content is "[wakeup:<trigger>] <note>". We parse the leading tag for the
// type label and strip it so only the note shows when expanded. Unknown /
// missing tags degrade to a generic label with the whole content as the note.
const WAKEUP_TAG = /^\[wakeup:([a-z_]+)\]\s*/i;

export function isWakeupEntry(entry: TimelineEntry): boolean {
  return entry.type === "comment" && entry.comment_type === "wakeup";
}

function parseWakeup(content: string): { trigger: string; note: string } {
  const match = content.match(WAKEUP_TAG);
  if (!match) return { trigger: "", note: content.trim() };
  return { trigger: match[1]!.toLowerCase(), note: content.slice(match[0].length).trim() };
}

function triggerLabel(trigger: string): string {
  switch (trigger) {
    case "time":
      return "Time-based wakeup";
    case "issue_status":
      return "Issue-status wakeup";
    case "github_ci":
      return "GitHub CI wakeup";
    default:
      return "Agent wakeup";
  }
}

function TriggerIcon({ trigger }: { trigger: string }) {
  const cls = "h-4 w-4 shrink-0 text-muted-foreground";
  switch (trigger) {
    case "time":
      return <AlarmClock className={cls} />;
    case "issue_status":
      return <ListChecks className={cls} />;
    case "github_ci":
      return <GitPullRequest className={cls} />;
    default:
      return <Moon className={cls} />;
  }
}

export function WakeupNote({
  entry,
  timeAgo,
}: {
  entry: TimelineEntry;
  timeAgo?: (dateStr: string) => string;
}) {
  const [expanded, setExpanded] = useState(false);
  const { trigger, note } = parseWakeup(entry.content ?? "");
  const label = triggerLabel(trigger);
  const hasNote = note.length > 0;

  return (
    <div className="pb-1 px-4">
      <button
        type="button"
        onClick={() => hasNote && setExpanded((v) => !v)}
        className="flex w-full items-center gap-2 text-xs text-muted-foreground transition-colors hover:text-foreground"
        aria-expanded={expanded}
        aria-label={label}
      >
        <TriggerIcon trigger={trigger} />
        <span className="font-medium">{label}</span>
        {timeAgo && entry.created_at && (
          <span className="text-muted-foreground/70">· {timeAgo(entry.created_at)}</span>
        )}
        {hasNote &&
          (expanded ? (
            <ChevronDown className="ml-auto h-3 w-3 shrink-0" />
          ) : (
            <ChevronRight className="ml-auto h-3 w-3 shrink-0" />
          ))}
      </button>
      {expanded && hasNote && (
        <p className="mt-1 ml-6 whitespace-pre-wrap break-words text-xs text-muted-foreground">
          {note}
        </p>
      )}
    </div>
  );
}
