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
      const captureEl = e.currentTarget as HTMLElement;
      const { pointerId } = e;
      captureEl.setPointerCapture(pointerId);

      dragRef.current = {
        startX: e.clientX,
        startY: e.clientY,
        startW: renderWidth,
        startH: renderHeight,
        dir,
      };
      setIsDragging(true);

      // Lock the resize cursor globally so it survives the pointer leaving the
      // narrow handle; per-direction rules live in packages/ui/styles/base.css.
      document.documentElement.setAttribute("data-chat-resizing", dir);

      // One idempotent cleanup wired to every way a pointer-capture drag can
      // end. A drag started with setPointerCapture may terminate via
      // pointercancel / lostpointercapture / window blur without a pointerup;
      // missing any of those would strand data-chat-resizing and freeze the
      // whole page in the resize cursor with text selection disabled.
      let finished = false;
      const finishDrag = () => {
        if (finished) return;
        finished = true;

        document.removeEventListener("pointermove", onPointerMove);
        document.removeEventListener("pointerup", onPointerUp);
        document.removeEventListener("pointercancel", onPointerCancel);
        captureEl.removeEventListener("lostpointercapture", onLostPointerCapture);
        window.removeEventListener("blur", finishDrag);

        dragRef.current = null;
        setIsDragging(false);
        document.documentElement.removeAttribute("data-chat-resizing");

        if (captureEl.hasPointerCapture?.(pointerId)) {
          captureEl.releasePointerCapture?.(pointerId);
        }
      };

      const onPointerMove = (ev: PointerEvent) => {
        const d = dragRef.current;
        if (!d || ev.pointerId !== pointerId) return;

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
      const onPointerUp = (ev: PointerEvent) => {
        if (ev.pointerId === pointerId) finishDrag();
      };
      const onPointerCancel = (ev: PointerEvent) => {
        if (ev.pointerId === pointerId) finishDrag();
      };
      const onLostPointerCapture = (ev: PointerEvent) => {
        if (ev.pointerId === pointerId) finishDrag();
      };

      document.addEventListener("pointermove", onPointerMove);
      document.addEventListener("pointerup", onPointerUp);
      document.addEventListener("pointercancel", onPointerCancel);
      captureEl.addEventListener("lostpointercapture", onLostPointerCapture);
      window.addEventListener("blur", finishDrag);
    },
    [renderWidth, renderHeight, setChatSize],
  );

  return { renderWidth, renderHeight, isAtMax, boundsReady, isDragging, toggleExpand, startDrag };
}
