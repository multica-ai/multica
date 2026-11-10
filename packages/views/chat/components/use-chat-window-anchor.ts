"use client";

import { useLayoutEffect, useState } from "react";

/** The window's inset from its container, and its resting place with no anchor. */
export const CHAT_WINDOW_INSET = 8;

/** The quick-chat group in the bottom bar. */
const CHAT_BAR_SELECTOR = '[data-slot="chat-bar"]';
/** The bottom-bar control the open window belongs to. */
const PRESSED_ANCHOR_SELECTOR = '[data-chat-anchor][aria-pressed="true"]';
/** Anything in the bar that opens or switches the window. */
const ANCHOR_TRIGGER_SELECTOR = "[data-chat-anchor], [data-chat-anchor-trigger]";

/**
 * Where the floating chat window sits horizontally, as a `right` offset in its
 * container. The window opens above the bottom-bar control that opened it: its
 * left edge lines up with that control's, pulled back inside the container
 * when there is no room. With no bar (below `md`) it rests in the corner.
 *
 * The window is placed when it opens, when the bar opens another chat in it,
 * and when its container resizes; after that it follows its control if the
 * bar's chips reflow. It stays put otherwise: a chip appearing for the chat
 * just started, or the window being dragged wider, must not move it under the
 * person typing in it.
 */
export function useChatWindowAnchor(
  windowRef: React.RefObject<HTMLElement | null>,
  isOpen: boolean,
): number {
  const [right, setRight] = useState(CHAT_WINDOW_INSET);

  useLayoutEffect(() => {
    if (!isOpen) return;
    const win = windowRef.current;
    const container = win?.parentElement;
    if (!win || !container) return;

    // The control the window was last placed above.
    let placedAt: string | null = null;
    const pressedAnchor = () => document.querySelector<HTMLElement>(PRESSED_ANCHOR_SELECTOR);

    const measure = () => {
      const anchor = pressedAnchor();
      const anchorBox = anchor?.getBoundingClientRect();
      placedAt = anchor?.dataset.chatAnchor ?? null;
      // No bar, or a bar that is not laid out (display: none).
      if (!anchorBox || anchorBox.width === 0) {
        setRight(CHAT_WINDOW_INSET);
        return;
      }
      const box = container.getBoundingClientRect();
      const width = win.offsetWidth;
      const left = Math.min(
        Math.max(anchorBox.left, box.left + CHAT_WINDOW_INSET),
        box.right - CHAT_WINDOW_INSET - width,
      );
      setRight(Math.max(CHAT_WINDOW_INSET, Math.round(box.right - left - width)));
    };

    measure();

    // The click handler's state update has not rendered yet when this runs,
    // so measure on the next frame, once the pressed control has moved.
    let frame = 0;
    const onClick = (event: MouseEvent) => {
      if (!(event.target instanceof Element) || !event.target.closest(ANCHOR_TRIGGER_SELECTOR)) return;
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(measure);
    };
    document.addEventListener("click", onClick);

    if (typeof ResizeObserver === "undefined") {
      return () => {
        cancelAnimationFrame(frame);
        document.removeEventListener("click", onClick);
      };
    }
    const observer = new ResizeObserver(measure);
    observer.observe(container);
    // A chip leaving the bar (its chat was read) shifts the ones beside it.
    // Follow the control the window is above, but not a change of control.
    const barObserver = new ResizeObserver(() => {
      if (pressedAnchor()?.dataset.chatAnchor === placedAt) measure();
    });
    const bar = document.querySelector(CHAT_BAR_SELECTOR);
    if (bar) barObserver.observe(bar);

    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener("click", onClick);
      observer.disconnect();
      barObserver.disconnect();
    };
  }, [windowRef, isOpen]);

  return right;
}
