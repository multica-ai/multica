"use client";

import { useEffect, useRef } from "react";
import type { ShortcutActionId } from "@multica/core/shortcuts";
import { isInboxShortcut } from "./inbox-shortcut";
import { hasInboxUndo } from "./inbox-undo-toast";

const neverYields = () => false;

function useDocumentInboxShortcut(
  action: ShortcutActionId,
  handler: (() => void) | null,
  yields: () => boolean,
) {
  const handlerRef = useRef(handler);
  useEffect(() => {
    handlerRef.current = handler;
  });
  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      // Yielding leaves the event untouched, so a later listener can take it.
      if (yields()) return;
      if (!isInboxShortcut(event, action)) return;
      const run = handlerRef.current;
      if (!run) return;
      event.preventDefault();
      run();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [action, yields]);
}

/**
 * Runs `handler` on `action`'s Inbox shortcut while mounted; `null` leaves the
 * key alone. The latest `handler` answers, without re-subscribing.
 */
export function useInboxShortcut(action: ShortcutActionId, handler: (() => void) | null) {
  useDocumentInboxShortcut(action, handler, neverYields);
}

/**
 * Runs `undo` on the undo shortcut (Z), except while an undo toast shows: the
 * toast is the latest action, and the shortcut is its.
 */
export function useInboxUndoShortcut(undo: (() => void) | null) {
  useDocumentInboxShortcut("undoInboxAction", undo, hasInboxUndo);
}
