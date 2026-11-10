"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Loader2, X } from "@multica/ui/icons";
import { useChatStore } from "@multica/core/chat";
import { chatSessionsOptions, pendingChatTasksOptions } from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { ChatSession } from "@multica/core/types";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { STAR_STROKE, starPath } from "@multica/ui/lib/multica-brand";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";
import { BOTTOM_BAR_ITEM_CLASS } from "../../layout/bottom-bar-styles";
import { ChatBarHistory } from "./chat-bar-history";
import { chatActivity, selectChatBarSessions, type ChatBarSession } from "./chat-bar-sessions";

const EMPTY_SESSIONS: ChatSession[] = [];
const STAR = starPath(STAR_STROKE);

/** The star from the Multica mark, in the text colour. */
function MulticaStar() {
  return (
    <svg aria-hidden viewBox="0 0 100 100" fill="currentColor">
      <path d={STAR} />
    </svg>
  );
}
const DEFAULT_MAX_CHIPS = 3;

/**
 * How many chips fit. The bar's other content (workspace status, Ask Multica,
 * history) takes about 560px and a chip up to 170px; a narrow window gives up
 * chips before it squeezes the status.
 */
function maxChipsForWidth(width: number): number {
  return Math.min(4, Math.max(1, Math.floor((width - 560) / 170)));
}

function useMaxChips(ref: React.RefObject<HTMLElement | null>): number {
  const [max, setMax] = useState(DEFAULT_MAX_CHIPS);
  useEffect(() => {
    const bar = ref.current?.parentElement;
    if (!bar || typeof ResizeObserver === "undefined") return;
    const update = () => {
      // Unmeasured (hidden, or no layout): keep the default.
      if (bar.clientWidth > 0) setMax(maxChipsForWidth(bar.clientWidth));
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(bar);
    return () => observer.disconnect();
  }, [ref]);
  return max;
}

/**
 * A chat's state, in a slot of one size whatever the state: a chip must not
 * change width when its chat finishes or is read, or it would move the chips
 * beside it and the window above it.
 */
function SessionMark({ item }: { item: ChatBarSession }) {
  return (
    <span aria-hidden className="flex size-3.5 shrink-0 items-center justify-center">
      {item.running ? (
        <Loader2 className="animate-spin" />
      ) : (
        <span
          className={cn("size-1.5 rounded-full", item.unread ? "bg-brand" : "bg-faint-foreground")}
        />
      )}
    </span>
  );
}

/**
 * Quick chat in the bottom bar: a chip per chat that needs watching or was
 * opened here, then Ask Multica, which starts a new chat, and the history.
 * The chat window opens above whichever control is pressed:
 * `useChatWindowAnchor` finds it by `data-chat-anchor`, and re-places the
 * window on a click that carries that attribute or `data-chat-anchor-trigger`.
 */
export function ChatBar() {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const isOpen = useChatStore((s) => s.isOpen);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const quickChatAgentId = useChatStore((s) => s.quickChatAgentId);
  const kept = useChatStore((s) => s.chatBarKept);
  const dismissed = useChatStore((s) => s.chatBarDismissed);
  const setOpen = useChatStore((s) => s.setOpen);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);
  const setSelectedProjectId = useChatStore((s) => s.setSelectedProjectId);
  const keepChatInBar = useChatStore((s) => s.keepChatInBar);
  const dismissChatFromBar = useChatStore((s) => s.dismissChatFromBar);
  const rootRef = useRef<HTMLDivElement>(null);
  const maxVisible = useMaxChips(rootRef);

  const { data: sessions = EMPTY_SESSIONS } = useQuery(chatSessionsOptions(wsId));
  // Per-chat running state needs the task list, not the boolean the launcher
  // reads. It is refetched on task lifecycle events only, never per message.
  const { data: pending } = useQuery(pendingChatTasksOptions(wsId));
  const runningSessionIds = useMemo(
    () => new Set((pending?.tasks ?? []).map((task) => task.chat_session_id)),
    [pending],
  );

  // Whatever the window shows gets a chip: a chat opened from the shortcut or
  // the window's own history, or one that just got its first message.
  const openSessionId = isOpen ? activeSessionId : null;
  useEffect(() => {
    if (openSessionId) keepChatInBar(openSessionId);
  }, [openSessionId, keepChatInBar]);

  const { visible, overflow } = useMemo(
    () =>
      selectChatBarSessions({ sessions, runningSessionIds, openSessionId, kept, dismissed, maxVisible }),
    [sessions, runningSessionIds, openSessionId, kept, dismissed, maxVisible],
  );
  const openOnChip = visible.some((item) => item.open);
  const askPressed = isOpen && !openOnChip;

  const openSession = (session: ChatSession) => {
    // A chat is bound to its agent, so opening it selects that agent too.
    if (session.agent_id !== selectedAgentId) setSelectedAgentId(session.agent_id);
    // Kept here, not left to the effect, so the chip is there to anchor the
    // window by the time the click is measured.
    keepChatInBar(session.id);
    setActiveSession(session.id);
    setOpen(true);
  };

  const toggleSession = (session: ChatSession) => {
    if (isOpen && activeSessionId === session.id) setOpen(false);
    else openSession(session);
  };

  const closeChip = (item: ChatBarSession) => {
    if (item.open) setOpen(false);
    dismissChatFromBar(item.session.id, chatActivity(item.session));
  };

  const askMultica = () => {
    if (askPressed) {
      setOpen(false);
      return;
    }
    if (quickChatAgentId) setSelectedAgentId(quickChatAgentId);
    setSelectedProjectId(null);
    setActiveSession(null);
    setOpen(true);
  };

  const titleOf = (session: ChatSession) => session.title || t(($) => $.window.untitled);
  const stateOf = (item: ChatBarSession) =>
    item.running ? t(($) => $.window.running) : item.unread ? t(($) => $.window.unread) : null;

  const askLabel = t(($) => $.fab.default);

  return (
    <div ref={rootRef} data-slot="chat-bar" className="flex min-w-0 shrink-0 items-center gap-0.5">
      {visible.map((item) => {
        const title = titleOf(item.session);
        const state = stateOf(item);
        return (
          <span key={item.session.id} className="group/chip relative flex shrink-0">
            <TooltipHint label={state ? `${title} · ${state}` : title}>
              <button
                type="button"
                data-chat-anchor={item.session.id}
                aria-pressed={item.open}
                aria-label={state ? `${title}, ${state}` : title}
                onClick={() => toggleSession(item.session)}
                className={cn(BOTTOM_BAR_ITEM_CLASS, "gap-1 pl-1.5")}
              >
                <SessionMark item={item} />
                {/* The close button sits over the end of the title rather than
                    beside it, so showing it never changes the chip's width. */}
                <span className="max-w-32 truncate group-focus-within/chip:[mask-image:linear-gradient(to_right,black_calc(100%-26px),transparent_calc(100%-14px))] group-hover/chip:[mask-image:linear-gradient(to_right,black_calc(100%-26px),transparent_calc(100%-14px))]">
                  {title}
                </span>
              </button>
            </TooltipHint>
            <button
              type="button"
              aria-label={t(($) => $.bar.close_chip, { title })}
              onClick={() => closeChip(item)}
              className="absolute top-1 right-1 flex size-4 cursor-pointer items-center justify-center rounded-sm text-muted-foreground opacity-0 outline-none transition-opacity hover:bg-surface-hover hover:text-foreground focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-ring group-hover/chip:opacity-100 [&_svg]:size-3"
            >
              <X aria-hidden />
            </button>
          </span>
        );
      })}
      {overflow.length > 0 && (
        <DropdownMenu>
          <DropdownMenuTrigger
            aria-label={t(($) => $.bar.more, { count: overflow.length })}
            className={cn(BOTTOM_BAR_ITEM_CLASS, "px-1.5 tabular-nums")}
          >
            +{overflow.length}
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" side="top" sideOffset={8} className="max-w-72 min-w-48">
            {overflow.map((item) => (
              <DropdownMenuItem
                key={item.session.id}
                data-chat-anchor-trigger
                onClick={() => openSession(item.session)}
              >
                <span className="flex shrink-0 [&_svg]:size-3.5">
                  <SessionMark item={item} />
                </span>
                <span className="min-w-0 truncate">{titleOf(item.session)}</span>
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
      )}
      {(visible.length > 0 || overflow.length > 0) && (
        <span aria-hidden className="mx-1.5 h-3.5 w-px shrink-0 bg-surface-border" />
      )}
      <TooltipHint label={t(($) => $.window.new_chat_tooltip)}>
        <button
          type="button"
          data-chat-anchor="new"
          aria-pressed={askPressed}
          onClick={askMultica}
          className={BOTTOM_BAR_ITEM_CLASS}
        >
          <MulticaStar />
          {askLabel}
        </button>
      </TooltipHint>
      <ChatBarHistory sessions={sessions} runningSessionIds={runningSessionIds} onSelect={openSession} />
    </div>
  );
}
