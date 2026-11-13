import { useEffect } from "react";

/** A trusted `contextmenu` this soon after ours is Chromium's own for the same key. */
const NATIVE_ECHO_MS = 500;

function isEditable(el: Element): boolean {
  return (
    el instanceof HTMLInputElement ||
    el instanceof HTMLTextAreaElement ||
    el instanceof HTMLSelectElement ||
    (el instanceof HTMLElement && el.isContentEditable)
  );
}

/** Whether `event` asks for the focused element's context menu. */
export function isContextMenuKey(event: KeyboardEvent): boolean {
  if (event.key === "ContextMenu") return !event.metaKey && !event.ctrlKey && !event.altKey;
  return event.key === "F10" && event.shiftKey && !event.metaKey && !event.ctrlKey && !event.altKey;
}

/**
 * Shift+F10 and the menu key open the focused element's menu, as a
 * right-click on it would: every menu in the app listens for `contextmenu`,
 * so the keyboard gets the same menus with no per-menu wiring. Chromium does
 * this itself on Windows and Linux but not on macOS, and its event lands at
 * the element's corner; ours does it everywhere, near the element's start.
 * The key is only taken over when an app menu claims the event; otherwise
 * (a plain link, text fields) the platform's own path stays intact.
 */
export function useKeyboardContextMenu() {
  useEffect(() => {
    let dispatchedAt = -Infinity;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || !isContextMenuKey(event)) return;
      const target = document.activeElement;
      if (!target || target === document.body || isEditable(target)) return;
      const rect = target.getBoundingClientRect();
      const claimed = !target.dispatchEvent(
        new MouseEvent("contextmenu", {
          bubbles: true,
          cancelable: true,
          button: 2,
          clientX: rect.left + Math.min(16, rect.width / 2),
          clientY: rect.top + Math.min(rect.height, 24) / 2,
        }),
      );
      if (!claimed) return;
      event.preventDefault();
      dispatchedAt = performance.now();
    };
    const swallowEcho = (event: MouseEvent) => {
      if (!event.isTrusted || performance.now() - dispatchedAt > NATIVE_ECHO_MS) return;
      dispatchedAt = -Infinity;
      event.preventDefault();
      event.stopImmediatePropagation();
    };
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("contextmenu", swallowEcho, true);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("contextmenu", swallowEcho, true);
    };
  }, []);
}
