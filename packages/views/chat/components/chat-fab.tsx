"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { MessageCircle } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { cn } from "@multica/ui/lib/utils";
import { useChatStore } from "@multica/core/chat";
import { chatSessionsOptions, pendingChatTasksOptions } from "@multica/core/chat/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { createLogger } from "@multica/core/logger";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";

const logger = createLogger("chat.ui");

const POSITION_STORAGE_KEY = "multica.chat.fab.position";
const DRAG_THRESHOLD_PX = 5;
const FAB_SIZE = 40;
const EDGE_PADDING = 8;

type FabPosition = { right: number; bottom: number };

const DEFAULT_POSITION: FabPosition = { right: EDGE_PADDING, bottom: EDGE_PADDING };

function loadPosition(): FabPosition {
  if (typeof window === "undefined") return DEFAULT_POSITION;
  try {
    const raw = window.localStorage.getItem(POSITION_STORAGE_KEY);
    if (!raw) return DEFAULT_POSITION;
    const parsed = JSON.parse(raw);
    if (typeof parsed?.right === "number" && typeof parsed?.bottom === "number") {
      return parsed;
    }
  } catch {
    // ignore
  }
  return DEFAULT_POSITION;
}

function clampToContainer(pos: FabPosition, container: HTMLElement | null): FabPosition {
  if (!container) return pos;
  const rect = container.getBoundingClientRect();
  const maxRight = Math.max(EDGE_PADDING, rect.width - FAB_SIZE - EDGE_PADDING);
  const maxBottom = Math.max(EDGE_PADDING, rect.height - FAB_SIZE - EDGE_PADDING);
  return {
    right: Math.min(Math.max(pos.right, EDGE_PADDING), maxRight),
    bottom: Math.min(Math.max(pos.bottom, EDGE_PADDING), maxBottom),
  };
}

export function ChatFab() {
  const wsId = useWorkspaceId();
  const isOpen = useChatStore((s) => s.isOpen);
  const toggle = useChatStore((s) => s.toggle);
  const { data: sessions = [] } = useQuery(chatSessionsOptions(wsId));
  const { data: pending } = useQuery(pendingChatTasksOptions(wsId));

  const [position, setPosition] = useState<FabPosition>(() => loadPosition());
  const [isDragging, setIsDragging] = useState(false);
  const buttonRef = useRef<HTMLButtonElement | null>(null);
  const dragStateRef = useRef<{
    pointerId: number;
    startX: number;
    startY: number;
    startPos: FabPosition;
    moved: boolean;
  } | null>(null);

  // Re-clamp on window resize so a previously-valid position stays in-bounds.
  useEffect(() => {
    const onResize = () => {
      setPosition((prev) => clampToContainer(prev, buttonRef.current?.parentElement ?? null));
    };
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

  const persist = useCallback((next: FabPosition) => {
    try {
      window.localStorage.setItem(POSITION_STORAGE_KEY, JSON.stringify(next));
    } catch {
      // ignore quota / disabled storage
    }
  }, []);

  const handlePointerDown = useCallback((e: React.PointerEvent<HTMLButtonElement>) => {
    if (e.button !== 0) return;
    const btn = e.currentTarget;
    btn.setPointerCapture(e.pointerId);
    dragStateRef.current = {
      pointerId: e.pointerId,
      startX: e.clientX,
      startY: e.clientY,
      startPos: position,
      moved: false,
    };
  }, [position]);

  const handlePointerMove = useCallback((e: React.PointerEvent<HTMLButtonElement>) => {
    const st = dragStateRef.current;
    if (!st || st.pointerId !== e.pointerId) return;
    const dx = e.clientX - st.startX;
    const dy = e.clientY - st.startY;
    if (!st.moved && Math.hypot(dx, dy) < DRAG_THRESHOLD_PX) return;
    if (!st.moved) {
      st.moved = true;
      setIsDragging(true);
    }
    // Container-relative: dragging right/down must *decrease* right/bottom.
    const next = clampToContainer(
      { right: st.startPos.right - dx, bottom: st.startPos.bottom - dy },
      buttonRef.current?.parentElement ?? null,
    );
    setPosition(next);
  }, []);

  const endDrag = useCallback((e: React.PointerEvent<HTMLButtonElement>) => {
    const st = dragStateRef.current;
    if (!st || st.pointerId !== e.pointerId) return;
    const wasDrag = st.moved;
    dragStateRef.current = null;
    if (wasDrag) {
      setIsDragging(false);
      setPosition((p) => {
        persist(p);
        return p;
      });
    }
    // If it was a click (no movement), onClick fires naturally after pointerup.
  }, [persist]);

  if (isOpen) return null;

  const unreadSessionCount = sessions.filter((s) => s.has_unread).length;
  const isRunning = (pending?.tasks ?? []).length > 0;

  const handleClick = () => {
    // Suppress click that follows a drag gesture.
    if (dragStateRef.current?.moved) return;
    logger.info("fab.click (open chat)", { unreadSessionCount, isRunning });
    toggle();
  };

  const tooltip = isRunning
    ? "Multica is working..."
    : unreadSessionCount > 0
      ? `${unreadSessionCount} unread ${unreadSessionCount === 1 ? "chat" : "chats"}`
      : "Ask Multica";

  return (
    <Tooltip>
      <TooltipTrigger
        ref={buttonRef}
        onClick={handleClick}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        style={{ right: position.right, bottom: position.bottom, touchAction: "none" }}
        className={cn(
          "absolute z-50 flex size-10 items-center justify-center rounded-full ring-1 ring-foreground/10 bg-card text-muted-foreground shadow-sm hover:text-accent-foreground",
          isDragging
            ? "cursor-grabbing scale-105 shadow-md transition-none select-none"
            : "cursor-grab transition-transform hover:scale-110 active:scale-95",
          isRunning && !isDragging && "animate-chat-impulse",
        )}
      >
        <MessageCircle className="pointer-events-none size-5" />
        {unreadSessionCount > 0 && (
          <span className="pointer-events-none absolute -top-0.5 -right-0.5 flex min-w-4 h-4 items-center justify-center rounded-full bg-brand px-1 text-xs font-semibold leading-none text-background">
            {unreadSessionCount > 9 ? "9+" : unreadSessionCount}
          </span>
        )}
      </TooltipTrigger>
      <TooltipContent side="top" sideOffset={10}>{tooltip}</TooltipContent>
    </Tooltip>
  );
}
