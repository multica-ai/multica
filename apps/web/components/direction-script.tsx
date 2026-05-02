"use client";

import { useEffect, useRef } from "react";

/**
 * Ctrl+Shift keyboard shortcut for text direction toggle.
 * - Ctrl + Left Shift  → LTR
 * - Ctrl + Right Shift → RTL
 * Works on any focused editable element (input, textarea, contenteditable).
 *
 * Strategy:
 * - Fire ONLY on Shift keydown when Ctrl is already held (not on every
 *   Ctrl+Shift+X combination). This avoids breaking command palettes
 *   (Ctrl+Shift+K, Ctrl+Shift+P, etc.).
 * - Use HTML-native `dir` attribute (no injected CSS or !important needed).
 */
export function DirectionScript() {
  const shiftSideRef = useRef<"left" | "right" | null>(null);

  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      // Only react to the Shift key itself when Ctrl is held.
      // This avoids triggering on Ctrl+Shift+A, Ctrl+Shift+K, etc.
      if (e.key !== "Shift") return;
      if (!e.ctrlKey) return;

      // Track which shift was pressed (for LTR vs RTL)
      const side = e.code === "ShiftLeft" ? "left" : "right";
      shiftSideRef.current = side;

      const el = document.activeElement as HTMLElement | null;
      if (!el) return;

      const isEditable =
        el instanceof HTMLInputElement ||
        el instanceof HTMLTextAreaElement ||
        (el as HTMLElement).isContentEditable;

      if (!isEditable) return;

      const dir = side === "right" ? "rtl" : "ltr";

      // Use HTML-native dir attribute — browser handles styling without !important
      el.setAttribute("dir", dir);
    }

    function onKeyUp(e: KeyboardEvent) {
      if (e.code === "ShiftLeft" || e.code === "ShiftRight") {
        shiftSideRef.current = null;
      }
    }

    // Use capture phase to intercept before React's synthetic handlers
    document.addEventListener("keydown", onKeyDown, true);
    document.addEventListener("keyup", onKeyUp, true);

    return () => {
      document.removeEventListener("keydown", onKeyDown, true);
      document.removeEventListener("keyup", onKeyUp, true);
    };
  }, []);

  return null;
}
