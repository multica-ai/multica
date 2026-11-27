"use client";

import { useCallback, useMemo, useState, type MouseEvent } from "react";

/** A row menu's open state, and the cursor point it opened at when a
 *  right-click (not its trigger) opened it. */
export interface RowMenuState {
  open: boolean;
  point: { x: number; y: number } | null;
}

export const CLOSED_ROW_MENU: RowMenuState = { open: false, point: null };

/** A zero-size virtual anchor at the menu's cursor point; undefined when the
 *  menu opened from its trigger, so it anchors there instead. */
export function usePointAnchor(point: RowMenuState["point"]) {
  return useMemo(() => {
    if (!point) return undefined;
    return {
      getBoundingClientRect: () => ({
        x: point.x,
        y: point.y,
        top: point.y,
        left: point.x,
        right: point.x,
        bottom: point.y,
        width: 0,
        height: 0,
        toJSON: () => ({}),
      }),
    };
  }, [point]);
}

/**
 * Closes Base UI sends a menu its trigger did not press open, which it treats
 * as hover-opened: the pointer leaving the trigger, and a submenu opening
 * inside it (read as a sibling menu opening). A right-click menu is neither.
 */
const HOVER_MENU_CLOSE_REASONS = new Set(["trigger-hover", "sibling-open"]);

/**
 * The state a menu moves to when Base UI asks it to open or close, or null to
 * stay put.
 */
export function nextRowMenuState(
  menu: RowMenuState,
  open: boolean,
  reason: string,
): RowMenuState | null {
  if (!open && menu.point && HOVER_MENU_CLOSE_REASONS.has(reason)) return null;
  return open ? { open: true, point: null } : { open: false, point: menu.point };
}

/**
 * Whether a right-click on `target` belongs to the text menu: the target is
 * inside a field or an editor, or selected text runs through it. An inline
 * object with a menu of its own (a mention) yields to it then.
 */
export function yieldsToTextMenu(target: Element): boolean {
  if (target.closest("input, textarea, [contenteditable='true']")) return true;
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed) return false;
  for (let i = 0; i < selection.rangeCount; i++) {
    if (selection.getRangeAt(i).intersectsNode(target)) return true;
  }
  return false;
}

function isContentTarget(target: Element): boolean {
  return (
    !!target.closest("a[href], img, video, pre, table, [data-context-menu-content]") ||
    yieldsToTextMenu(target)
  );
}

/**
 * Whether a right-click on a row should open the row's menu. Not when
 * something inside the row already handled it, nor when the event bubbled up
 * the React tree from a portaled popup outside the row's DOM (the row's own
 * open menu included). With `deferToContent`, content that has a menu of its
 * own — a link, an image, a field, selected text, a code block, a table, an
 * attachment — keeps it.
 */
export function shouldOpenRowMenu(
  event: MouseEvent,
  { deferToContent = false }: { deferToContent?: boolean } = {},
): boolean {
  if (event.defaultPrevented) return false;
  const target = event.target;
  if (!(target instanceof Element) || !event.currentTarget.contains(target)) return false;
  return !(deferToContent && isContentTarget(target));
}

/** Open state for a menu that opens both from a trigger and at the cursor. */
export function useRowMenu() {
  const [menu, setMenu] = useState<RowMenuState>(CLOSED_ROW_MENU);
  const anchor = usePointAnchor(menu.point);

  const openAtPointer = useCallback((event: MouseEvent) => {
    event.preventDefault();
    setMenu({ open: true, point: { x: event.clientX, y: event.clientY } });
  }, []);

  const onOpenChange = useCallback(
    (open: boolean, details: { reason: string }) => {
      setMenu((current) => nextRowMenuState(current, open, details.reason) ?? current);
    },
    [],
  );

  return { menu, setMenu, anchor, openAtPointer, onOpenChange };
}
