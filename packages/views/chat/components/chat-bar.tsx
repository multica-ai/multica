"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { History, Loader2, MessageCircle } from "@multica/ui/icons";
import { useChatStore } from "@multica/core/chat";
import { chatSessionsOptions, pendingChatTasksOptions } from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useShortcut } from "@multica/core/shortcuts";
import type { ChatSession } from "@multica/core/types";
import { TooltipHint } from "@multica/ui/components/common/tooltip-hint";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { ShortcutKeycaps } from "../../common/shortcut-keycaps";
import { useT } from "../../i18n";
import { BOTTOM_BAR_ICON_ITEM_CLASS, BOTTOM_BAR_ITEM_CLASS } from "../../layout/bottom-bar-styles";
import { AppLink } from "../../navigation";
import { selectChatBarSessions, type ChatBarSession } from "./chat-bar-sessions";

const EMPTY_SESSIONS: ChatSession[] = [];
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
 * Quick chat in the bottom bar: one chip per chat that is running, unread or
 * open, then Ask Multica and the way to every chat. The chat window opens
 * above whichever control is pressed: `useChatWindowAnchor` finds it by
 * `data-chat-anchor`, and re-places the window on a click that carries that
 * attribute or `data-chat-anchor-trigger`.
 */
export function ChatBar() {
  const { t } = useT("chat");
  const wsId = useWorkspaceId();
  const p = useWorkspacePaths();
  const isOpen = useChatStore((s) => s.isOpen);
  const activeSessionId = useChatStore((s) => s.activeSessionId);
  const selectedAgentId = useChatStore((s) => s.selectedAgentId);
  const setOpen = useChatStore((s) => s.setOpen);
  const toggle = useChatStore((s) => s.toggle);
  const setActiveSession = useChatStore((s) => s.setActiveSession);
  const setSelectedAgentId = useChatStore((s) => s.setSelectedAgentId);
  const shortcut = useShortcut("toggleChat");
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

  const { visible, overflow } = useMemo(
    () =>
      selectChatBarSessions({
        sessions,
        runningSessionIds,
        openSessionId: isOpen ? activeSessionId : null,
        maxVisible,
      }),
    [sessions, runningSessionIds, isOpen, activeSessionId, maxVisible],
  );
  const openOnChip = visible.some((item) => item.open);

  const openSession = (session: ChatSession) => {
    if (isOpen && activeSessionId === session.id) {
      setOpen(false);
      return;
    }
    // A chat is bound to its agent, so opening it selects that agent too.
    if (session.agent_id !== selectedAgentId) setSelectedAgentId(session.agent_id);
    setActiveSession(session.id);
    setOpen(true);
  };

  const titleOf = (session: ChatSession) => session.title || t(($) => $.window.untitled);
  const stateOf = (item: ChatBarSession) =>
    item.running ? t(($) => $.window.running) : item.unread ? t(($) => $.window.unread) : null;

  const askLabel = t(($) => $.fab.default);
  const allChatsLabel = t(($) => $.bar.all_chats);

  return (
    <div ref={rootRef} data-slot="chat-bar" className="flex min-w-0 shrink-0 items-center gap-0.5">
      {visible.map((item) => {
        const title = titleOf(item.session);
        const state = stateOf(item);
        return (
          <TooltipHint key={item.session.id} label={state ? `${title} · ${state}` : title}>
            <button
              type="button"
              data-chat-anchor={item.session.id}
              aria-pressed={item.open}
              aria-label={state ? `${title}, ${state}` : title}
              onClick={() => openSession(item.session)}
              className={cn(BOTTOM_BAR_ITEM_CLASS, "gap-1 pl-1.5")}
            >
              <SessionMark item={item} />
              <span className="max-w-32 truncate">{title}</span>
            </button>
          </TooltipHint>
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
      <TooltipHint
        label={askLabel}
        shortcut={shortcut ? <ShortcutKeycaps shortcut={shortcut} decorative className="ml-1.5" /> : null}
      >
        <button
          type="button"
          data-chat-anchor="new"
          aria-pressed={isOpen && !openOnChip}
          onClick={toggle}
          className={BOTTOM_BAR_ITEM_CLASS}
        >
          <MessageCircle aria-hidden />
          {askLabel}
        </button>
      </TooltipHint>
      <TooltipHint label={allChatsLabel}>
        <AppLink href={p.chat()} aria-label={allChatsLabel} className={BOTTOM_BAR_ICON_ITEM_CLASS}>
          <History aria-hidden />
        </AppLink>
      </TooltipHint>
    </div>
  );
}
