"use client";

import { useEffect, useRef } from "react";

/**
 * Ctrl+Shift keyboard shortcut for text direction toggle.
 * - Ctrl + Left Shift  → LTR
 * - Ctrl + Right Shift → RTL
 * Works on any focused editable element (input, textarea, contenteditable).
 *
 * Strategy:
 * - Track which Shift key is currently held (left vs right).
 * - Fire on ANY keydown when both Ctrl+Shift are active (order-independent).
 * - Use CSS class injection with !important to survive React re-renders.
 */
export function DirectionScript() {
  const shiftSideRef = useRef<"left" | "right" | null>(null);

  useEffect(() => {
    // Inject a <style> tag with direction classes that use !important.
    // This ensures our direction overrides survive React re-renders and
    // beat the global CSS rules that also use !important.
    const styleId = "__multica-direction-styles";
    if (!document.getElementById(styleId)) {
      const styleEl = document.createElement("style");
      styleEl.id = styleId;
      styleEl.textContent = `
        [data-multica-dir="ltr"], [data-multica-dir="ltr"] * {
          direction: ltr !important;
          text-align: left !important;
        }
        [data-multica-dir="rtl"], [data-multica-dir="rtl"] * {
          direction: rtl !important;
          text-align: right !important;
        }
      `;
      document.head.appendChild(styleEl);
    }

    function onKeyDown(e: KeyboardEvent) {
      // Track which shift key was pressed
      if (e.code === "ShiftLeft") {
        shiftSideRef.current = "left";
      } else if (e.code === "ShiftRight") {
        shiftSideRef.current = "right";
      }

      // We need BOTH Ctrl and Shift to be held.
      // But we only act on the event that COMPLETES the combo:
      // - If Shift was pressed first, then Ctrl → act on Ctrl keydown
      // - If Ctrl was pressed first, then Shift → act on Shift keydown
      // - If both already held and another key pressed → also act
      if (!e.ctrlKey || !e.shiftKey) return;

      // Determine direction from the tracked shift side
      const side = shiftSideRef.current;
      if (!side) return;

      const el = document.activeElement as HTMLElement | null;
      if (!el) return;

      const isEditable =
        el instanceof HTMLInputElement ||
        el instanceof HTMLTextAreaElement ||
        (el as HTMLElement).isContentEditable;

      if (!isEditable) return;

      const dir = side === "right" ? "rtl" : "ltr";

      // Use data attribute — the injected CSS handles the !important styling.
      el.setAttribute("data-multica-dir", dir);

      // Also set inline style as backup (survives even if CSS is blocked)
      el.style.setProperty("direction", dir, "important");
      el.style.setProperty("text-align", dir === "rtl" ? "right" : "left", "important");

      e.preventDefault();
      e.stopImmediatePropagation();
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
