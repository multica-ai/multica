"use client";

import { useCallback, useEffect, useLayoutEffect, useRef, useState, type RefObject } from "react";

/**
 * Owns the floating window's composer-focus nonce.
 *
 * `focusRequest` is handed to ChatInput, which pulls keyboard focus into the
 * editor every time the number changes; `0` is inert. Callers bump it for the
 * moments that mean "you are about to type something new" — a fresh chat, an
 * agent switch, a project-context change.
 *
 * Opening the window is one of those moments (MUL-5522): the whole point of the
 * toggle shortcut is reaching chat without a mouse, so landing in a window you
 * still have to click into would defeat it — and the same courtesy applies to
 * opening it from the FAB. So is Ask Multica (`newChatRequest`, bumped by the
 * store's `startNewChat`), which can start a new chat in a window already open.
 *
 * The refs are seeded with mount-time values so only a real closed → open
 * transition or a new bump focuses. ChatWindow stays mounted while closed and
 * `isOpen` is restored from storage, so treating mount as an open event would
 * let a persisted "open" preference steal focus from whatever page the user loaded.
 */
export function useChatInputFocus(
  isOpen: boolean,
  windowRef?: RefObject<HTMLElement | null>,
  newChatRequest = 0,
): {
  focusRequest: number;
  requestInputFocus: () => void;
} {
  const [focusRequest, setFocusRequest] = useState(0);
  const requestInputFocus = useCallback(() => setFocusRequest((n) => n + 1), []);

  // The window stays mounted to preserve drafts and its closing animation.
  // Release its focus before the next keystroke can enter a hidden composer.
  // Do not blur a dialog or page control the user has already moved to.
  useLayoutEffect(() => {
    if (isOpen) return;
    const container = windowRef?.current;
    const active = container?.ownerDocument.activeElement;
    if (active instanceof HTMLElement && container?.contains(active)) active.blur();
  }, [isOpen, windowRef]);

  const wasOpenRef = useRef(isOpen);
  useEffect(() => {
    const wasOpen = wasOpenRef.current;
    wasOpenRef.current = isOpen;
    if (isOpen && !wasOpen) requestInputFocus();
  }, [isOpen, requestInputFocus]);

  const seenNewChatRequestRef = useRef(newChatRequest);
  useEffect(() => {
    if (newChatRequest === seenNewChatRequestRef.current) return;
    seenNewChatRequestRef.current = newChatRequest;
    requestInputFocus();
  }, [newChatRequest, requestInputFocus]);

  return { focusRequest, requestInputFocus };
}
