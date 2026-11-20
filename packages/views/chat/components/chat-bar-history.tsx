"use client";

import { useMemo, useState } from "react";
import { History, Loader2 } from "@multica/ui/icons";
import { useWorkspacePaths } from "@multica/core/paths";
import type { ChatSession } from "@multica/core/types";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { BOTTOM_BAR_ICON_ITEM_CLASS } from "../../layout/bottom-bar-styles";
import { AppLink } from "../../navigation";
import { chatActivity } from "./chat-bar-sessions";
import { useFormatTimeAgo } from "./use-format-time-ago";

/** Recent chats only: the Chat page is the full history. */
const MAX_HISTORY = 30;
const DAY_MS = 86_400_000;

type HistoryGroup = "today" | "week" | "older";
const GROUPS: HistoryGroup[] = ["today", "week", "older"];

export function groupChatHistory(
  sessions: ChatSession[],
  now: Date,
): { group: HistoryGroup; sessions: ChatSession[] }[] {
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const recent = sessions
    .filter((session) => session.status !== "archived")
    .sort((a, b) => Date.parse(chatActivity(b)) - Date.parse(chatActivity(a)))
    .slice(0, MAX_HISTORY);
  const byGroup = new Map<HistoryGroup, ChatSession[]>();
  for (const session of recent) {
    const at = Date.parse(chatActivity(session));
    const group = at >= startOfToday ? "today" : at >= startOfToday - 7 * DAY_MS ? "week" : "older";
    byGroup.set(group, [...(byGroup.get(group) ?? []), session]);
  }
  return GROUPS.flatMap((group) => {
    const inGroup = byGroup.get(group);
    return inGroup ? [{ group, sessions: inGroup }] : [];
  });
}

/**
 * The chat history, opened from the bottom bar. Picking a chat opens it in
 * the quick-chat window; the rows carry `data-chat-anchor-trigger` so the
 * window is placed above the chip that chat gets.
 */
export function ChatBarHistory({
  sessions,
  runningSessionIds,
  onSelect,
}: {
  sessions: ChatSession[];
  runningSessionIds: ReadonlySet<string>;
  onSelect: (session: ChatSession) => void;
}) {
  const { t } = useT("chat");
  const p = useWorkspacePaths();
  const [open, setOpen] = useState(false);
  const formatTimeAgo = useFormatTimeAgo();
  // Grouped when the popover opens: "today" is read off the clock then.
  const groups = useMemo(
    () => (open ? groupChatHistory(sessions, new Date()) : []),
    [open, sessions],
  );
  const label = t(($) => $.bar.history);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <TooltipHint label={label}>
        <PopoverTrigger aria-label={label} className={BOTTOM_BAR_ICON_ITEM_CLASS}>
          <History aria-hidden />
        </PopoverTrigger>
      </TooltipHint>
      <PopoverContent
        align="end"
        side="top"
        sideOffset={8}
        aria-label={label}
        className="max-h-[min(28rem,var(--available-height))] w-80 gap-0 overflow-hidden p-1"
      >
        <div className="shrink-0 px-2 py-1.5 text-caption font-medium text-muted-foreground">
          {label}
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto">
          {groups.length === 0 && (
            <div className="px-2 py-1.5 text-caption text-muted-foreground">
              {t(($) => $.window.no_previous)}
            </div>
          )}
          {groups.map(({ group, sessions: inGroup }) => (
            <div
              key={group}
              role="group"
              aria-label={t(($) => $.bar.history_group[group])}
              className="border-t border-surface-border pt-1 pb-1 last:pb-0"
            >
              <div className="px-2 py-1 text-caption text-muted-foreground">
                {t(($) => $.bar.history_group[group])}
              </div>
              {inGroup.map((session) => {
                const running = runningSessionIds.has(session.id);
                return (
                  <button
                    key={session.id}
                    type="button"
                    data-chat-anchor-trigger
                    onClick={() => {
                      onSelect(session);
                      setOpen(false);
                    }}
                    className="flex h-8 w-full min-w-0 cursor-pointer items-center gap-2 rounded-md px-2 text-left text-body outline-none transition-colors hover:bg-accent/60 focus-visible:bg-accent/60"
                  >
                    <span aria-hidden className="flex size-3.5 shrink-0 items-center justify-center">
                      {running ? (
                        <Loader2 className="size-3.5 animate-spin text-muted-foreground" />
                      ) : session.has_unread ? (
                        <span className="size-1.5 rounded-full bg-brand" />
                      ) : null}
                    </span>
                    <span className={cn("min-w-0 flex-1 truncate", session.has_unread && "font-medium")}>
                      {session.title || t(($) => $.window.untitled)}
                    </span>
                    <span className="shrink-0 text-caption text-muted-foreground">
                      {formatTimeAgo(chatActivity(session))}
                    </span>
                  </button>
                );
              })}
            </div>
          ))}
        </div>
        <div className="shrink-0 border-t border-surface-border pt-1">
          <AppLink
            href={p.chat()}
            onClick={() => setOpen(false)}
            className="flex h-8 items-center rounded-md px-2 text-body text-muted-foreground outline-none transition-colors hover:bg-accent/60 hover:text-foreground focus-visible:bg-accent/60"
          >
            {t(($) => $.bar.all_chats)}
          </AppLink>
        </div>
      </PopoverContent>
    </Popover>
  );
}
