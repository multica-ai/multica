"use client";

import { useCallback, useEffect, useState } from "react";

/**
 * Longest the floating window waits for its message area before fading in
 * anyway. Covers a cold first open — a list that has to land and then settle —
 * so that one still appears composed; past it, a list that keeps moving opens
 * behind its own reveal rather than leaving the toggle looking dead.
 */
export const OPEN_REVEAL_DEADLINE_MS = 300;

/**
 * Holds the floating window's fade-in until its message area can paint.
 *
 * Closing unmounts the message list (MUL-8030), so each open mounts a fresh one,
 * and a fresh list stays invisible until it has landed on the newest message
 * (MUL-6879). Fading the window in on the keypress put that wait on screen as a
 * panel with a header and a composer and nothing between them, on every open
 * (MUL-8417). The window now starts its fade when the area reports ready.
 *
 * Readiness is per open: closing clears it, so the next open waits again.
 */
export function useChatOpenReveal(isOpen: boolean): {
  contentReady: boolean;
  markContentReady: () => void;
} {
  const [contentReady, setContentReady] = useState(false);
  const markContentReady = useCallback(() => setContentReady(true), []);

  useEffect(() => {
    if (!isOpen) return;
    const deadline = setTimeout(markContentReady, OPEN_REVEAL_DEADLINE_MS);
    return () => {
      clearTimeout(deadline);
      setContentReady(false);
    };
  }, [isOpen, markContentReady]);

  return { contentReady, markContentReady };
}
