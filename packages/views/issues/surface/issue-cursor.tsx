"use client";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useModalStore } from "@multica/core/modals";
import { issueRouteRef, useWorkspacePaths } from "@multica/core/paths";
import { isEditableShortcutTarget, isPortalLayerShortcutTarget } from "@multica/core/shortcuts";
import type { Issue } from "@multica/core/types";
import { isImeComposing } from "@multica/core/utils";
import { useIntentNavigate } from "../../navigation";
import { useRegisterCommandBarContext } from "../../search/register-context";
import {
  PEEK_TARGET_ATTR,
  hasIssueInColumns,
  locateInColumns,
  useIssuePeekActions,
  useIssuePeekClick,
  useIssuePeekColumns,
  useIssuePeekId,
  type IssuePeekColumns,
  type IssuePeekPosition,
} from "./peek-context";
import { useIssueSurfaceSelection, type IssueSurfaceSelection } from "./selection-context";

/**
 * The row a list, table or board's keyboard cursor is on, while it shows.
 * Rows and cards paint `data-cursor` from it.
 */
const IssueCursorContext = createContext<string | null>(null);

export function useIsIssueCursor(issueId: string) {
  return useContext(IssueCursorContext) === issueId;
}

/** The cursor's row, for views that mark rows without a per-row hook. */
export function useIssueCursorId() {
  return useContext(IssueCursorContext);
}

/** The cursor on a row (list, table): a ring, over the row's hover or selected background. */
export const CURSOR_ROW_CLASS =
  "data-[cursor]:outline data-[cursor]:outline-1 data-[cursor]:-outline-offset-1 data-[cursor]:outline-ring/60";

type Step = keyof Pick<IssuePeekPosition, "prevId" | "nextId" | "leftId" | "rightId">;

interface Cursor {
  id: string;
  /**
   * False when only the pointer put it there: the row's hover already shows
   * it, and nothing but a key (or the peek) should act on it.
   */
  visible: boolean;
}

/**
 * Keyboard flow over the issues a view publishes to its peek host (see
 * `publishColumns`): J / K or ↑ / ↓ move the cursor, ← / → across board
 * columns, Enter opens the issue as a click would, Space peeks it. In views
 * with a selection, X toggles the row, Shift extends from the last toggled
 * one, ⌘A selects every row and Esc clears the selection, then the cursor.
 *
 * The cursor starts where the pointer last was, and follows the peek while
 * one is open (the peek owns these keys then). Its issue is the command
 * bar's, below a peek or a selection, so the issue verbs (S, A, P, ...) act
 * on it.
 */
export function IssueCursorProvider({
  issues,
  selectable,
  enabled,
  children,
}: {
  /** The issues behind the published ids, for the command bar context. */
  issues: readonly Issue[];
  /** Whether this view shows its selection (list, table). */
  selectable: boolean;
  /** Off for views that paint no cursor (swimlane, gantt). */
  enabled: boolean;
  children: ReactNode;
}) {
  const columns = useIssuePeekColumns();
  const peek = useIssuePeekActions();
  const peekId = useIssuePeekId();
  const selection = useIssueSurfaceSelection();
  const [cursor, setCursor] = useState<Cursor | null>(null);
  const rootRef = useRef<HTMLDivElement>(null);
  const anchorRef = useRef<string | null>(null);

  // A cursor left on an issue the view no longer shows starts over.
  const cursorId = enabled && cursor && hasIssueInColumns(columns, cursor.id) ? cursor.id : null;
  // The peek marks its own issue while it is open.
  const shownId = cursor?.visible && !peekId ? cursorId : null;

  useEffect(() => {
    if (peekId) setCursor({ id: peekId, visible: true });
  }, [peekId]);

  useEffect(() => {
    if (selection.selectedIds.size === 0) anchorRef.current = null;
  }, [selection.selectedIds]);

  useEffect(() => {
    if (!shownId) return;
    rootRef.current
      ?.querySelector(`[${PEEK_TARGET_ATTR}="${CSS.escape(shownId)}"]`)
      ?.scrollIntoView?.({ block: "nearest", inline: "nearest" });
  }, [shownId]);

  const cursorIssue = useMemo(
    () => (shownId ? (issues.find((issue) => issue.id === shownId) ?? null) : null),
    [issues, shownId],
  );
  useRegisterCommandBarContext(
    useMemo(() => (cursorIssue ? { kind: "issue", issue: cursorIssue, cursor: true } : null), [cursorIssue]),
  );

  // Enter opens the issue as a click on it would: peek or page, per the
  // reader's open mode. Not by clicking the row's DOM, where a table row's
  // first link can be a pull request's.
  const peekClick = useIssuePeekClick();
  const intentNavigate = useIntentNavigate();
  const paths = useWorkspacePaths();
  const openIssue = useCallback(
    (issue: Issue) => {
      if (!peekClick(issue)) intentNavigate(paths.issueDetail(issueRouteRef(issue)), "push", issue.identifier);
    },
    [intentNavigate, paths, peekClick],
  );

  // Captured, so a key the cursor takes reaches neither the hover peek's
  // Space, the focused row's own handlers nor its link's native Enter.
  const stateRef = useRef({ enabled, columns, cursorId, shownId, cursorIssue, peekId, peek, selection, selectable, openIssue });
  stateRef.current = { enabled, columns, cursorId, shownId, cursorIssue, peekId, peek, selection, selectable, openIssue };

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.altKey || isImeComposing(event)) return;
      if (!isCursorKeyTarget(event.target)) return;
      if (useModalStore.getState().modal) return;
      const state = stateRef.current;
      if (!state.enabled || state.peekId || !state.columns) return;
      const handled = handleKey(event, state, {
        moveTo: (id) => setCursor({ id, visible: true }),
        clear: () => setCursor(null),
        anchorRef,
      });
      if (!handled) return;
      event.preventDefault();
      event.stopPropagation();
    };
    document.addEventListener("keydown", onKeyDown, true);
    return () => document.removeEventListener("keydown", onKeyDown, true);
  }, []);

  const lastPointerRef = useRef<{ x: number; y: number } | null>(null);
  return (
    <IssueCursorContext.Provider value={shownId}>
      <div
        ref={rootRef}
        className="contents"
        onPointerMove={(event) => {
          // Content scrolling under a still pointer is not the user pointing.
          const last = lastPointerRef.current;
          lastPointerRef.current = { x: event.clientX, y: event.clientY };
          if (last && last.x === event.clientX && last.y === event.clientY) return;
          // Pointing takes over from the keys: the cursor moves to the row
          // under the pointer and hides, as the row's hover shows it.
          const id = (event.target as Element).closest(`[${PEEK_TARGET_ATTR}]`)?.getAttribute(PEEK_TARGET_ATTR);
          if (id) {
            if (id !== cursor?.id || cursor?.visible) setCursor({ id, visible: false });
          } else if (cursor?.visible) {
            setCursor({ ...cursor, visible: false });
          }
        }}
      >
        {children}
      </div>
    </IssueCursorContext.Provider>
  );
}

interface KeyState {
  columns: IssuePeekColumns | null;
  cursorId: string | null;
  shownId: string | null;
  cursorIssue: Issue | null;
  peek: ReturnType<typeof useIssuePeekActions>;
  selection: IssueSurfaceSelection;
  selectable: boolean;
  openIssue: (issue: Issue) => void;
}

function handleKey(
  event: KeyboardEvent,
  state: KeyState,
  ops: {
    moveTo: (id: string) => void;
    clear: () => void;
    anchorRef: { current: string | null };
  },
): boolean {
  const columns = state.columns ?? [];
  const mod = event.metaKey || event.ctrlKey;
  const { selection, selectable, shownId } = state;

  const move = (step: Step) => {
    const from = state.cursorId;
    const position = locateInColumns(columns, from);
    const next = position ? (position[step] ?? from) : firstId(columns, step);
    if (!next) return false;
    if (event.shiftKey && selectable && from && (step === "prevId" || step === "nextId")) {
      const anchor = ops.anchorRef.current ?? from;
      ops.anchorRef.current = anchor;
      const range = rangeInColumn(columns, anchor, next);
      if (range) selection.select(range);
    }
    ops.moveTo(next);
    return true;
  };

  if (mod) {
    if (event.key.toLowerCase() !== "a" || event.shiftKey || !selectable) return false;
    if (!shownId && selection.selectedIds.size === 0) return false;
    selection.select(columns.flat());
    return true;
  }

  // Held down, only the moves repeat.
  if (event.repeat && (event.key === "x" || event.key === "X" || event.key === "Enter" || event.key === " ")) {
    return shownId !== null;
  }

  switch (event.key) {
    case "j":
    case "J":
    case "ArrowDown":
      return move("nextId");
    case "k":
    case "K":
    case "ArrowUp":
      return move("prevId");
    case "ArrowLeft":
    case "ArrowRight":
      if (columns.length < 2 || event.shiftKey) return false;
      return move(event.key === "ArrowLeft" ? "leftId" : "rightId");
    case "x":
    case "X":
      if (!selectable || !shownId || event.shiftKey) return false;
      selection.toggle(shownId);
      ops.anchorRef.current = shownId;
      return true;
    case "Enter":
      if (!state.cursorIssue || event.shiftKey) return false;
      state.openIssue(state.cursorIssue);
      return true;
    case " ":
      if (!shownId || !state.peek || event.shiftKey) return false;
      state.peek.toggle(shownId);
      return true;
    case "Escape":
      if (selectable && selection.selectedIds.size > 0) {
        selection.clear();
        ops.anchorRef.current = null;
        return true;
      }
      if (!shownId) return false;
      ops.clear();
      return true;
    default:
      return false;
  }
}

/** Where a first step lands with no cursor yet: the top of the first column, or the bottom going up. */
function firstId(columns: IssuePeekColumns, step: Step): string | null {
  const column = columns.find((ids) => ids.length > 0);
  if (!column) return null;
  return step === "prevId" ? column[column.length - 1]! : column[0]!;
}

function rangeInColumn(columns: IssuePeekColumns, from: string, to: string): string[] | null {
  for (const column of columns) {
    const a = column.indexOf(from);
    const b = column.indexOf(to);
    if (a === -1 || b === -1) continue;
    return column.slice(Math.min(a, b), Math.max(a, b) + 1);
  }
  return null;
}

/**
 * The page itself, or a row's own element or link: a focused button, menu
 * trigger or field keeps its keys.
 */
function isCursorKeyTarget(target: EventTarget | null): boolean {
  // First: with a modal layer open, even the page itself is behind it.
  if (isPortalLayerShortcutTarget(target)) return false;
  if (!(target instanceof Element) || target === document.body) return true;
  if (isEditableShortcutTarget(target)) return false;
  const row = target.closest(`[${PEEK_TARGET_ATTR}]`);
  if (!row || row.closest("[data-issue-peek]")) return false;
  return target === row || target.tagName === "A";
}
