import type { KeyboardEvent } from "react";

/**
 * Keyboard guard for a search box rendered inside a menu popup (the Filter
 * menu's Assignee / Creator / Project / Label lists, the Skills label filter).
 *
 * Base UI's menu merges typeahead and list-navigation handlers onto the
 * role="menu" element, and keydown bubbles from the input up to it. Typeahead
 * stopEvent()s every unmodified printable key, so the box swallowed every
 * character and the list could never be narrowed. List navigation claims
 * Home / End and, inside a submenu, closes the whole submenu on ArrowLeft,
 * whatever modifiers are held.
 *
 * Stop exactly those keys at the input and let every other key bubble, so the
 * guard never hides a key the menu or the app still needs: Escape closes the
 * menu, Tab moves focus on, ArrowDown / ArrowUp step from the search box into
 * the filtered list, and Enter, Backspace, F-keys and any Cmd/Ctrl/Alt chord
 * (Cmd/Ctrl+K opens global search) still reach document-level shortcut
 * handlers exactly as they did before.
 */
export function keepMenuSearchKeys(event: KeyboardEvent<HTMLInputElement>) {
  const typed =
    event.key.length === 1 && !event.ctrlKey && !event.metaKey && !event.altKey;
  if (typed || MENU_CARET_KEYS.has(event.key)) event.stopPropagation();
}

/** Caret keys Base UI's list navigation claims regardless of modifiers. */
const MENU_CARET_KEYS = new Set(["Home", "End", "ArrowLeft"]);
