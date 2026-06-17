"use client";

// FIR-recent-chats: recent chat list shown at the top of the inbox new-chat
// screen. Lives in the cerebro zone; the upstream inbox-chat-panel mounts it
// behind the `cerebro_chat_recent_list` flag via a small marked import.

import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Bot, MessageSquare } from "lucide-react";
import { useFlagValue } from "@multica/cerebro-feature-flags";
import type { Agent, ChatSession } from "@multica/core/types";
import { Avatar, AvatarFallback, AvatarImage } from "@multica/ui/components/ui/avatar";

const COLLAPSED_COUNT = 3;

type Strings = {
  heading: string;
  see_earlier: (n: number) => string;
  collapse: string;
  untitled: string;
  archived: string;
};

function useStrings(): Strings {
  const { i18n } = useTranslation();
  // Danish-first fork; fall back to English for non-da locales.
  if (i18n.language?.startsWith("en")) {
    return {
      heading: "Recent chats",
      see_earlier: (n) => `See ${n} earlier`,
      collapse: "Show fewer",
      untitled: "Untitled",
      archived: "Archived",
    };
  }
  return {
    heading: "Seneste samtaler",
    see_earlier: (n) => `Se ${n} tidligere`,
    collapse: "Vis færre",
    untitled: "Uden titel",
    archived: "Arkiveret",
  };
}

/** Compact relative time ("5 min", "3 t", "2 d") in the active locale. */
function useTimeAgo(): (iso: string) => string {
  const { i18n } = useTranslation();
  const da = !i18n.language?.startsWith("en");
  return (iso: string) => {
    const then = new Date(iso).getTime();
    if (Number.isNaN(then)) return "";
    const sec = Math.max(0, Math.floor((Date.now() - then) / 1000));
    if (sec < 60) return da ? "nu" : "now";
    const min = Math.floor(sec / 60);
    if (min < 60) return `${min} min`;
    const hr = Math.floor(min / 60);
    if (hr < 24) return da ? `${hr} t` : `${hr}h`;
    const day = Math.floor(hr / 24);
    return da ? `${day} d` : `${day}d`;
  };
}

/**
 * Recent chat sessions list for the new-chat empty state. Shows the 3 most
 * recent by default with a "see earlier" expander that reveals the rest in a
 * scrollable area. Clicking a row resumes that session in place.
 *
 * `sessions` / `agents` are passed in from the panel (already loaded there) so
 * this component stays free of workspace-context coupling and adds no extra
 * network calls.
 */
export function RecentChatsList({
  sessions,
  agents,
  onOpenSession,
}: {
  sessions: ChatSession[];
  agents: Agent[];
  onOpenSession: (sessionId: string) => void;
}) {
  const enabled = useFlagValue("cerebro_chat_recent_list");
  const s = useStrings();
  const timeAgo = useTimeAgo();
  const [expanded, setExpanded] = useState(false);

  const sorted = useMemo(() => {
    return [...(sessions ?? [])].sort((a, b) => {
      const ta = new Date(b.updated_at).getTime();
      const tb = new Date(a.updated_at).getTime();
      return (Number.isNaN(ta) ? 0 : ta) - (Number.isNaN(tb) ? 0 : tb);
    });
  }, [sessions]);

  const agentById = useMemo(() => {
    const map = new Map<string, Agent>();
    for (const a of agents ?? []) map.set(a.id, a);
    return map;
  }, [agents]);

  if (!enabled || sorted.length === 0) return null;

  const visible = expanded ? sorted : sorted.slice(0, COLLAPSED_COUNT);
  const hiddenCount = sorted.length - COLLAPSED_COUNT;

  return (
    <div className="w-full max-w-sm">
      <p className="px-1 pb-1.5 text-xs font-medium text-muted-foreground">
        {s.heading}
      </p>
      <div
        className={
          expanded
            ? "max-h-64 overflow-y-auto rounded-md border"
            : "rounded-md border"
        }
      >
        {visible.map((session) => {
          const agent = agentById.get(session.agent_id) ?? null;
          const archived = session.status === "archived";
          return (
            <button
              key={session.id}
              type="button"
              onClick={() => onOpenSession(session.id)}
              className="flex w-full items-center gap-2.5 px-2.5 py-2 text-left transition-colors hover:bg-accent not-last:border-b"
            >
              <Avatar className="size-6 shrink-0">
                {agent?.avatar_url && <AvatarImage src={agent.avatar_url} />}
                <AvatarFallback className="bg-purple-100 text-purple-700 text-[10px]">
                  <Bot className="size-3" />
                </AvatarFallback>
              </Avatar>
              <span className="flex min-w-0 flex-1 flex-col">
                <span className="flex items-center gap-1.5">
                  <span className="truncate text-sm font-medium">
                    {session.title?.trim() || s.untitled}
                  </span>
                  {session.has_unread && (
                    <span className="size-1.5 shrink-0 rounded-full bg-primary" />
                  )}
                </span>
                <span className="truncate text-xs text-muted-foreground">
                  {agent?.name ?? ""}
                  {archived ? ` · ${s.archived}` : ""}
                </span>
              </span>
              <span className="shrink-0 text-xs text-muted-foreground">
                {timeAgo(session.updated_at)}
              </span>
            </button>
          );
        })}
      </div>
      {hiddenCount > 0 && (
        <button
          type="button"
          onClick={() => setExpanded((v) => !v)}
          className="mt-1.5 flex items-center gap-1 px-1 text-xs font-medium text-primary hover:underline"
        >
          <MessageSquare className="size-3" />
          {expanded ? s.collapse : s.see_earlier(hiddenCount)}
        </button>
      )}
    </div>
  );
}
