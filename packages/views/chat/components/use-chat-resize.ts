"use client";

import React, { useRef, useCallback, useState, useEffect } from "react";
import { CHAT_MIN_W, CHAT_MIN_H, useChatStore } from "@multica/core/chat";
import type { ResizeDelta } from "@multica/ui/hooks/use-resize-gesture";

export type DragDir = "left" | "top" | "corner";

const MAX_RATIO = 0.9;
const FALLBACK_MAX_W = 800;
const FALLBACK_MAX_H = 700;

function clamp(v: number, min: number, max: number) {
  return Math.max(min, Math.min(max, v));
}

export function useChatResize(
  windowRef: React.RefObject<HTMLDivElement | null>,
) {
  const chatWidth = useChatStore((s) => s.chatWidth);
  const chatHeight = useChatStore((s) => s.chatHeight);
  const isExpanded = useChatStore((s) => s.isExpanded);
  const setChatSize = useChatStore((s) => s.setChatSize);
  const setExpanded = useChatStore((s) => s.setExpanded);

  // ── Container bounds via ResizeObserver ────────────────────────────────
  const boundsRef = useRef({ maxW: FALLBACK_MAX_W, maxH: FALLBACK_MAX_H });
  const [boundsReady, setBoundsReady] = useState(false);
  const [isDragging, setIsDragging] = useState(false);
  const [, setRevision] = useState(0);

  useEffect(() => {
    const el = windowRef.current;
    const parent = el?.parentElement;
    if (!parent) return;

    const update = () => {
      const maxW = Math.floor(parent.clientWidth * MAX_RATIO);
      const maxH = Math.floor(parent.clientHeight * MAX_RATIO);
      setBoundsReady(true); // idempotent once true
      // Only trigger a re-render if the bounds actually changed. Without this
      // guard, any spurious ResizeObserver notification (including sub-pixel
      // layout jitter during mount) schedules a setState that feeds back into
      // the observer, producing "Maximum update depth exceeded".
      const prev = boundsRef.current;
      if (prev.maxW === maxW && prev.maxH === maxH) return;
      boundsRef.current = { maxW, maxH };
      setRevision((r) => r + 1);
    };

    // Measure immediately (parent is already in DOM at this point)
    update();

    const ro = new ResizeObserver(update);
    ro.observe(parent);
    return () => ro.disconnect();
  }, [windowRef]);

  // ── Derive rendered size ──────────────────────────────────────────────
  const { maxW, maxH } = boundsRef.current;

  const renderWidth = isExpanded ? maxW : clamp(chatWidth, CHAT_MIN_W, maxW);
  const renderHeight = isExpanded ? maxH : clamp(chatHeight, CHAT_MIN_H, maxH);

  // ── Expand / Restore ──────────────────────────────────────────────────
  const isAtMax = renderWidth >= maxW && renderHeight >= maxH;

  const toggleExpand = useCallback(() => {
    if (isExpanded || isAtMax) {
      setChatSize(CHAT_MIN_W, CHAT_MIN_H);
    } else {
      setExpanded(true);
    }
  }, [isExpanded, isAtMax, setChatSize, setExpanded]);

  // ── Drag ──────────────────────────────────────────────────────────────
  // The pointer gesture itself (capture, cursor lock, teardown) belongs to
  // useResizeGesture in the handles; this hook only owns the size maths.
  const dragRef = useRef<{ startW: number; startH: number } | null>(null);

  const handleResizeStart = useCallback(() => {
    dragRef.current = { startW: renderWidth, startH: renderHeight };
    setIsDragging(true);
  }, [renderWidth, renderHeight]);

  const handleResize = useCallback(
    (dir: DragDir, { dx, dy }: ResizeDelta) => {
      const d = dragRef.current;
      if (!d) return;

      const { maxW: mw, maxH: mh } = boundsRef.current;

      // The window is anchored bottom-right, so dragging the left/top edges
      // away from it (negative delta) grows the window.
      const rawW = dir === "left" || dir === "corner" ? d.startW - dx : d.startW;
      const rawH = dir === "top" || dir === "corner" ? d.startH - dy : d.startH;

      setChatSize(clamp(rawW, CHAT_MIN_W, mw), clamp(rawH, CHAT_MIN_H, mh));
    },
    [setChatSize],
  );

  const handleResizeEnd = useCallback(() => {
    dragRef.current = null;
    setIsDragging(false);
  }, []);

  return {
    renderWidth,
    renderHeight,
    isAtMax,
    boundsReady,
    isDragging,
    toggleExpand,
    handleResizeStart,
    handleResize,
    handleResizeEnd,
  };
}
