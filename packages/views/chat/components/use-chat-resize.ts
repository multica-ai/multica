"use client";

import React, { useRef, useCallback, useState, useEffect } from "react";
import { CHAT_MIN_W, CHAT_MIN_H, useChatStore } from "@multica/core/chat";

type DragDir = "left" | "top" | "corner";

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
  const dragRef = useRef<{
    startX: number;
    startY: number;
    startW: number;
    startH: number;
    dir: DragDir;
  } | null>(null);

  const startDrag = useCallback(
    (e: React.PointerEvent, dir: DragDir) => {
      e.preventDefault();
      (e.target as HTMLElement).setPointerCapture(e.pointerId);

      dragRef.current = {
        startX: e.clientX,
        startY: e.clientY,
        startW: renderWidth,
        startH: renderHeight,
        dir,
      };
      setIsDragging(true);

      const onPointerMove = (ev: PointerEvent) => {
        const d = dragRef.current;
        if (!d) return;

        const { maxW: mw, maxH: mh } = boundsRef.current;

        const rawW =
          dir === "left" || dir === "corner"
            ? d.startW - (ev.clientX - d.startX)
            : d.startW;
        const rawH =
          dir === "top" || dir === "corner"
            ? d.startH - (ev.clientY - d.startY)
            : d.startH;

        setChatSize(clamp(rawW, CHAT_MIN_W, mw), clamp(rawH, CHAT_MIN_H, mh));
      };

      const onPointerUp = () => {
        dragRef.current = null;
        setIsDragging(false);
        document.removeEventListener("pointermove", onPointerMove);
        document.removeEventListener("pointerup", onPointerUp);
        document.body.style.cursor = "";
        document.body.style.userSelect = "";
      };

      document.addEventListener("pointermove", onPointerMove);
      document.addEventListener("pointerup", onPointerUp);

      const cursorMap: Record<DragDir, string> = {
        left: "col-resize",
        top: "row-resize",
        corner: "nw-resize",
      };
      document.body.style.cursor = cursorMap[dir];
      document.body.style.userSelect = "none";
    },
    [renderWidth, renderHeight, setChatSize],
  );

  return { renderWidth, renderHeight, isAtMax, boundsReady, isDragging, toggleExpand, startDrag };
}
